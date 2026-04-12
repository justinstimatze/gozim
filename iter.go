package zim

import (
	"iter"
	"sync/atomic"
)

// Entries returns an iterator over all entries in URL index order.
func (a *Archive) Entries() iter.Seq2[uint32, Entry] {
	return func(yield func(uint32, Entry) bool) {
		for idx := uint32(0); idx < a.r.articleCount; idx++ {
			art, err := a.r.articleAtIdx(idx)
			if err != nil {
				continue
			}
			if !yield(idx, Entry{article: art, archive: a}) {
				return
			}
		}
	}
}

// Articles returns an iterator over front articles only (navigable content, not redirects).
func (a *Archive) Articles() iter.Seq2[uint32, Entry] {
	return func(yield func(uint32, Entry) bool) {
		for idx := uint32(0); idx < a.r.articleCount; idx++ {
			art, err := a.r.articleAtIdx(idx)
			if err != nil {
				continue
			}
			e := Entry{article: art, archive: a}
			if !e.IsArticle() {
				continue
			}
			if !yield(idx, e) {
				return
			}
		}
	}
}

// EntriesByTitle returns an iterator over all entries in title-sorted order.
// For v6 ZIM files (which lack a title pointer list), this falls back to
// URL index order (which is already sorted by namespace+path).
func (a *Archive) EntriesByTitle() iter.Seq2[uint32, Entry] {
	// v6 ZIM files have no title pointer list
	if a.r.titlePtrPos == 0xFFFFFFFFFFFFFFFF {
		return a.Entries()
	}
	return func(yield func(uint32, Entry) bool) {
		for i := uint32(0); i < a.r.articleCount; i++ {
			pos := a.r.titlePtrPos + uint64(i)*4
			b, err := a.r.bytesRangeAt(pos, pos+4)
			if err != nil {
				continue
			}
			idx := le32(b)
			art, err := a.r.articleAtIdx(idx)
			if err != nil {
				continue
			}
			if !yield(idx, Entry{article: art, archive: a}) {
				return
			}
		}
	}
}

// EntriesWithErrors returns an iterator like Entries but tracks skipped entries.
// After iteration completes, call errs.Load() to get the count of entries that
// failed to read and were silently skipped.
func (a *Archive) EntriesWithErrors() (iter.Seq2[uint32, Entry], *atomic.Int64) {
	errs := &atomic.Int64{}
	return func(yield func(uint32, Entry) bool) {
		for idx := uint32(0); idx < a.r.articleCount; idx++ {
			art, err := a.r.articleAtIdx(idx)
			if err != nil {
				errs.Add(1)
				continue
			}
			if !yield(idx, Entry{article: art, archive: a}) {
				return
			}
		}
	}, errs
}

// EntriesInNamespace returns an iterator over entries in the given namespace.
func (a *Archive) EntriesInNamespace(ns byte) iter.Seq2[uint32, Entry] {
	return func(yield func(uint32, Entry) bool) {
		lo, err := a.lowerBound(string(ns) + "/")
		if err != nil {
			return
		}
		for idx := lo; idx < a.r.articleCount; idx++ {
			art, err := a.r.articleAtIdx(idx)
			if err != nil {
				continue
			}
			if art.namespace != ns {
				break
			}
			if !yield(idx, Entry{article: art, archive: a}) {
				return
			}
		}
	}
}
