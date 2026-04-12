package zim

import "sync"

type metadataCache struct {
	once sync.Once
	data map[string]string
	err  error
}

func (a *Archive) loadMetadata() (map[string]string, error) {
	a.meta.once.Do(func() {
		a.meta.data = make(map[string]string)
		a.meta.err = a.parseMetadata()
	})
	return a.meta.data, a.meta.err
}

func (a *Archive) parseMetadata() error {
	lo, err := a.lowerBound("M/")
	if err != nil {
		return err
	}
	for idx := lo; idx < a.r.articleCount; idx++ {
		art, err := a.r.articleAtIdx(idx)
		if err != nil {
			continue
		}
		if art.namespace != 'M' {
			break
		}
		if art.entryType == RedirectEntry || art.entryType == LinkTargetEntry || art.entryType == DeletedEntry {
			continue
		}
		data, err := art.data()
		if err != nil {
			continue
		}
		a.meta.data[art.url] = string(data)
	}
	return nil
}

func (a *Archive) lowerBound(prefix string) (uint32, error) {
	var lo uint32
	hi := a.r.articleCount
	art := new(article)
	for lo < hi {
		mid := lo + (hi-lo)/2
		offset, err := a.r.OffsetAtURLIdx(mid)
		if err != nil {
			return 0, err
		}
		if err := a.r.fillArticle(art, offset); err != nil {
			return 0, err
		}
		if art.fullURL() < prefix {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo, nil
}

// Metadata returns the value for an arbitrary metadata key and whether it exists.
func (a *Archive) Metadata(key string) (string, bool) {
	m, err := a.loadMetadata()
	if err != nil {
		return "", false
	}
	v, ok := m[key]
	return v, ok
}

// Title returns the archive title from metadata.
func (a *Archive) Title() string { v, _ := a.Metadata("Title"); return v }

// Language returns the archive language code (ISO 639) from metadata.
func (a *Archive) Language() string { v, _ := a.Metadata("Language"); return v }

// Creator returns the archive creator from metadata.
func (a *Archive) Creator() string { v, _ := a.Metadata("Creator"); return v }

// Date returns the archive creation date from metadata.
func (a *Archive) Date() string { v, _ := a.Metadata("Date"); return v }

// Description returns the archive description from metadata.
func (a *Archive) Description() string { v, _ := a.Metadata("Description"); return v }
