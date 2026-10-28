package zim

import (
	"path/filepath"
	"regexp"
	"testing"
)

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// TestSearchRanksExactTitleFirst checks that a query naming an article puts
// that article first, ahead of longer titles and pages that merely mention it.
func TestSearchRanksExactTitleFirst(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	opts := []SearchOption{
		WithIndexPath(filepath.Join(t.TempDir(), "rank.bleve")),
		WithContentIndexing(func(b []byte) string { return htmlTagRe.ReplaceAllString(string(b), " ") }),
	}
	// Algeria vs. "Algiers (city)", America vs. "North America".
	for _, title := range []string{"Алжир", "Америкэ"} {
		results, err := a.Search(title, 5, opts...)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) == 0 || results[0].Entry.Title() != title {
			var got []string
			for _, r := range results {
				got = append(got, r.Entry.Title())
			}
			t.Errorf("Search(%q): want it first, got %q", title, got)
		}
	}
}

// TestSearchTitleBoostKeepsResultSet checks that the title bonus reorders
// results without adding any the query excludes.
func TestSearchTitleBoostKeepsResultSet(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	opts := []SearchOption{
		WithIndexPath(filepath.Join(t.TempDir(), "rank.bleve")),
		WithContentIndexing(func(b []byte) string { return htmlTagRe.ReplaceAllString(string(b), " ") }),
	}
	results, err := a.Search("+Алжир -къалэ", 50, opts...)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Entry.Title() == "Алжир (къалэ)" {
			t.Errorf("excluded term matched: %q", r.Entry.Title())
		}
	}
}
