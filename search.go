package zim

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/blevesearch/bleve/v2"
)

// SearchResult represents a single fulltext search result.
type SearchResult struct {
	Entry Entry
	Score float64
}

type searchConfig struct {
	ctx       context.Context
	indexPath string
	offset    int
	extract   func([]byte) string
	analyzer  string
	batchSize int
}

// SearchOption configures fulltext search behavior.
type SearchOption func(*searchConfig)

// WithIndexPath overrides the default index location (<zimfile>.bleve/).
func WithIndexPath(path string) SearchOption {
	return func(c *searchConfig) { c.indexPath = path }
}

// WithOffset sets the starting offset for paginated results.
func WithOffset(n int) SearchOption {
	return func(c *searchConfig) { c.offset = n }
}

// WithContentIndexing enables indexing of article body text.
// The extract function converts raw content bytes to plain text for indexing.
func WithContentIndexing(extract func([]byte) string) SearchOption {
	return func(c *searchConfig) { c.extract = extract }
}

// WithAnalyzer sets the Bleve analyzer for indexing (e.g., "en", "ennostemm", "standard").
func WithAnalyzer(name string) SearchOption {
	return func(c *searchConfig) { c.analyzer = name }
}

// WithBatchSize sets the number of documents per Bleve batch during index building (default 1000).
func WithBatchSize(n int) SearchOption {
	return func(c *searchConfig) { c.batchSize = n }
}

// WithContext sets a context for cancellation of long-running index builds.
func WithContext(ctx context.Context) SearchOption {
	return func(c *searchConfig) { c.ctx = ctx }
}

type searchState struct {
	mu    sync.Mutex
	index bleve.Index
}

func (c *searchConfig) defaults() {
	if c.batchSize <= 0 {
		c.batchSize = 1000
	}
	if c.analyzer == "" {
		c.analyzer = "standard"
	}
}

// BuildIndex builds a fulltext search index without performing a search.
// The index is persisted to disk and reused by subsequent Search() calls.
func (a *Archive) BuildIndex(opts ...SearchOption) error {
	cfg := searchConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	cfg.defaults()
	_, err := a.ensureIndex(cfg)
	return err
}

// Search performs fulltext search on the archive. On the first call, it opens or builds
// a Bleve index persisted to disk as <zimfile>.bleve/ by default.
func (a *Archive) Search(query string, limit int, opts ...SearchOption) ([]SearchResult, error) {
	cfg := searchConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	cfg.defaults()

	idx, err := a.ensureIndex(cfg)
	if err != nil {
		return nil, err
	}

	q := bleve.NewQueryStringQuery(query)
	req := bleve.NewSearchRequestOptions(q, limit, cfg.offset, false)
	req.Fields = []string{"Title"}

	sr, err := idx.Search(req)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	results := make([]SearchResult, 0, len(sr.Hits))
	for _, hit := range sr.Hits {
		urlIdx, err := strconv.ParseUint(hit.ID, 10, 32)
		if err != nil {
			continue
		}
		entry, err := a.GetEntryByIndex(uint32(urlIdx))
		if err != nil {
			continue
		}
		results = append(results, SearchResult{Entry: entry, Score: hit.Score})
	}
	return results, nil
}

func (a *Archive) ensureIndex(cfg searchConfig) (bleve.Index, error) {
	a.search.mu.Lock()
	defer a.search.mu.Unlock()

	if a.search.index != nil {
		return a.search.index, nil
	}

	idxPath := cfg.indexPath
	if idxPath == "" {
		idxPath = a.r.f.Name() + ".bleve"
	}

	// Try to open existing index
	if _, err := os.Stat(idxPath); err == nil {
		idx, err := bleve.Open(idxPath)
		if err == nil {
			a.search.index = idx
			return idx, nil
		}
	}

	// Build new index
	idx, err := a.buildIndex(idxPath, cfg)
	if err != nil {
		return nil, err
	}
	a.search.index = idx
	return idx, nil
}

type indexDoc struct {
	Title   string
	Content string
}

func (a *Archive) buildIndex(path string, cfg searchConfig) (bleve.Index, error) {
	mapping := bleve.NewIndexMapping()
	mapping.DefaultType = "Article"

	articleMapping := bleve.NewDocumentMapping()
	mapping.AddDocumentMapping("Article", articleMapping)

	titleField := bleve.NewTextFieldMapping()
	titleField.Store = false
	titleField.Index = true
	titleField.Analyzer = cfg.analyzer
	articleMapping.AddFieldMappingsAt("Title", titleField)

	if cfg.extract != nil {
		contentField := bleve.NewTextFieldMapping()
		contentField.Store = false
		contentField.Index = true
		contentField.Analyzer = cfg.analyzer
		articleMapping.AddFieldMappingsAt("Content", contentField)
	}

	idx, err := bleve.New(path, mapping)
	if err != nil {
		return nil, fmt.Errorf("can't create index: %w", err)
	}

	batch := idx.NewBatch()
	batchCount := 0

	ctx := cfg.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	for urlIdx, entry := range a.Articles() {
		if err := ctx.Err(); err != nil {
			idx.Close()
			os.RemoveAll(path) // clean up partial index
			return nil, fmt.Errorf("index build cancelled: %w", err)
		}

		doc := indexDoc{Title: entry.Title()}

		if cfg.extract != nil {
			if data, err := entry.Content(); err == nil && len(data) > 0 {
				doc.Content = cfg.extract(data)
			}
		}

		batch.Index(strconv.FormatUint(uint64(urlIdx), 10), doc)
		batchCount++

		if batchCount >= cfg.batchSize {
			if err := idx.Batch(batch); err != nil {
				idx.Close()
				return nil, fmt.Errorf("batch index failed: %w", err)
			}
			batch = idx.NewBatch()
			batchCount = 0
		}
	}

	if batchCount > 0 {
		if err := idx.Batch(batch); err != nil {
			idx.Close()
			return nil, fmt.Errorf("batch index failed: %w", err)
		}
	}

	return idx, nil
}

func (a *Archive) closeSearch() error {
	a.search.mu.Lock()
	defer a.search.mu.Unlock()
	if a.search.index != nil {
		err := a.search.index.Close()
		a.search.index = nil
		return err
	}
	return nil
}
