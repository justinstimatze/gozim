package zim

import (
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Library manages a collection of named ZIM archives loaded from a directory.
type Library struct {
	entries map[string]*LibraryEntry
	order   []string // stable alphabetical iteration order
	errs    []error  // per-file open failures (non-fatal)
}

// LibraryEntry holds a single archive and its discovery metadata.
type LibraryEntry struct {
	Archive   *Archive
	Slug      string
	IndexPath string // "" if no index auto-discovered
	Path      string // original file path
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// slugify derives a URL-safe slug from a ZIM filename.
// wikipedia_en_all_2024-01.zim → wikipedia-en-all-2024-01
func slugify(filename string) string {
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	slug := strings.ToLower(stem)
	slug = slugRe.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	return slug
}

// OpenLibrary scans dir for .zim files, opens each, and returns a Library.
// Options are forwarded to each Archive. Per-file open failures are non-fatal
// and accessible via Errors(). Returns an error only if the directory is
// unreadable or contains no loadable ZIM files.
func OpenLibrary(dir string, opts ...Option) (*Library, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	lib := &Library{entries: make(map[string]*LibraryEntry)}
	slugCount := make(map[string]int) // track collisions

	for _, de := range dirEntries {
		name := de.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".zim") {
			continue
		}
		// Skip non-regular files (symlinks to devices, pipes, etc.)
		if de.Type().IsDir() {
			continue
		}
		if !de.Type().IsRegular() && de.Type()&os.ModeSymlink == 0 {
			continue
		}

		fullPath := filepath.Join(dir, name)

		a, err := Open(fullPath, opts...)
		if err != nil {
			lib.errs = append(lib.errs, fmt.Errorf("%s: %w", name, err))
			continue
		}

		// Derive slug with collision handling
		base := slugify(name)
		if base == "" {
			base = "zim"
		}
		slugCount[base]++
		slug := base
		if slugCount[base] > 1 {
			slug = base + "-" + strconv.Itoa(slugCount[base])
		}

		// Auto-discover sibling index
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		indexPath := discoverIndex(dir, stem)

		lib.entries[slug] = &LibraryEntry{
			Archive:   a,
			Slug:      slug,
			IndexPath: indexPath,
			Path:      fullPath,
		}
		lib.order = append(lib.order, slug)
	}

	if len(lib.entries) == 0 {
		if len(lib.errs) > 0 {
			return nil, fmt.Errorf("no ZIM files loaded: %w", errors.Join(lib.errs...))
		}
		return nil, fmt.Errorf("no .zim files found in %s", dir)
	}

	slices.Sort(lib.order)
	return lib, nil
}

// discoverIndex probes for a Bleve index directory or .idx file next to the ZIM.
func discoverIndex(dir, stem string) string {
	bleve := filepath.Join(dir, stem+".bleve")
	if fi, err := os.Stat(bleve); err == nil && fi.IsDir() {
		return bleve
	}
	idx := filepath.Join(dir, stem+".idx")
	if fi, err := os.Stat(idx); err == nil && !fi.IsDir() {
		return idx
	}
	return ""
}

// Get returns the entry for the given slug, or false if not found.
func (l *Library) Get(slug string) (*LibraryEntry, bool) {
	e, ok := l.entries[slug]
	return e, ok
}

// Entries iterates over all entries in stable alphabetical order by slug.
func (l *Library) Entries() iter.Seq2[string, *LibraryEntry] {
	return func(yield func(string, *LibraryEntry) bool) {
		for _, slug := range l.order {
			if !yield(slug, l.entries[slug]) {
				return
			}
		}
	}
}

// Slugs returns a copy of the slug list in alphabetical order.
func (l *Library) Slugs() []string {
	return slices.Clone(l.order)
}

// Len returns the number of loaded archives.
func (l *Library) Len() int { return len(l.entries) }

// Errors returns per-file open failures encountered during OpenLibrary.
func (l *Library) Errors() []error {
	return slices.Clone(l.errs)
}

// Close releases all archives in the library.
func (l *Library) Close() error {
	var errs []error
	for _, slug := range l.order {
		if err := l.entries[slug].Archive.Close(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", slug, err))
		}
	}
	return errors.Join(errs...)
}
