package zim

import (
	"slices"
	"strings"
	"testing"
)

// TestSearchTitlesListingMatchesScan checks the listing search against the
// linear URL-index scan it replaces, restricted to entries the listing holds
// (the scan also returns non-article entries such as stylesheets).
func TestSearchTitlesListingMatchesScan(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	l := a.titleListing()
	if l.count == 0 {
		t.Fatal("test.zim should ship X/listing/titleOrdered/v1")
	}
	listed := make(map[string]bool, l.count)
	for i := uint32(0); i < l.count; i++ {
		idx, err := a.listingURLIdx(l, i)
		if err != nil {
			t.Fatal(err)
		}
		art, err := a.r.articleAtIdx(idx)
		if err != nil {
			t.Fatal(err)
		}
		listed[art.fullURL()] = true
	}

	ns := a.articleNamespace()
	for _, prefix := range []string{"", "A", "NBA", "The ", "Mi", "z", "ZZZNONEXISTENT"} {
		got, err := a.SearchTitles(prefix, 1<<20)
		if err != nil {
			t.Fatalf("%q: %v", prefix, err)
		}
		scan, err := a.searchTitlesByURL(ns, prefix, 1<<20)
		if err != nil {
			t.Fatalf("%q scan: %v", prefix, err)
		}

		var gotPaths, wantPaths []string
		for _, e := range got {
			if !strings.HasPrefix(e.article.title, prefix) && !strings.HasPrefix(e.article.url, prefix) {
				t.Errorf("%q: result %s doesn't match", prefix, e.FullPath())
			}
			gotPaths = append(gotPaths, e.FullPath())
		}
		for _, e := range scan {
			if listed[e.FullPath()] {
				wantPaths = append(wantPaths, e.FullPath())
			}
		}
		slices.Sort(gotPaths)
		slices.Sort(wantPaths)
		if !slices.Equal(gotPaths, wantPaths) {
			t.Errorf("%q: listing found %d, scan found %d listed entries", prefix, len(gotPaths), len(wantPaths))
		}
	}
}

func TestSearchTitlesListingLimit(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	got, err := a.SearchTitles("", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("got %d results, want 3", len(got))
	}
}

func TestSearchTitlesListingBadIndex(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.titleListing() // resolve once so the override below sticks
	// Offset 0 is the ZIM magic number, far beyond the entry count.
	a.listing = titleListing{off: 0, count: 1}
	if _, err := a.SearchTitles("A", 10); err == nil {
		t.Fatal("listing pointing past the entry table: expected error")
	}
}
