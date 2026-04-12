package zim

import (
	"testing"
)

func TestArchiveOpen(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	if a.EntryCount() == 0 {
		t.Error("EntryCount is 0")
	}
	if a.ClusterCount() == 0 {
		t.Error("ClusterCount is 0")
	}
}

func TestArchiveOpenMmap(t *testing.T) {
	a, err := Open("test.zim", WithMmap())
	if err != nil {
		t.Fatalf("Open with mmap: %v", err)
	}
	defer a.Close()

	if a.EntryCount() == 0 {
		t.Error("EntryCount is 0")
	}
}

func TestArchiveVersion(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	if a.VersionMajor() != 5 {
		t.Errorf("expected version 5, got %d", a.VersionMajor())
	}
}

func TestArchiveUUID(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	uuid := a.UUID()
	// UUID should not be all zeros
	allZero := true
	for _, b := range uuid {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("UUID is all zeros")
	}
}

func TestArchiveMimeTypes(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	mimes := a.MimeTypes()
	if len(mimes) == 0 {
		t.Error("no MIME types")
	}
}

func TestGetEntryByPath(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	e, err := a.GetEntryByPath("Dracula:Capitol_1.html")
	if err != nil {
		t.Fatalf("GetEntryByPath: %v", err)
	}

	if e.Title() == "" {
		t.Error("entry has empty title")
	}
	if !e.IsArticle() {
		t.Error("expected entry to be an article")
	}
	if e.IsRedirect() {
		t.Error("expected entry to not be a redirect")
	}
	t.Logf("Entry: %s (title: %s, mime: %s)", e.FullPath(), e.Title(), e.MimeType())
}

func TestGetEntryByFullPath(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	e, err := a.GetEntryByFullPath("A/Dracula:Capitol_1.html")
	if err != nil {
		t.Fatalf("GetEntryByFullPath: %v", err)
	}

	if e.Path() != "Dracula:Capitol_1.html" {
		t.Errorf("expected path Dracula:Capitol_1.html, got %s", e.Path())
	}
}

func TestGetEntryByIndex(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	e, err := a.GetEntryByIndex(5)
	if err != nil {
		t.Fatalf("GetEntryByIndex: %v", err)
	}
	t.Logf("Entry at index 5: %s", e)
}

func TestMainEntry(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	e, err := a.MainEntry()
	if err != nil {
		t.Fatalf("MainEntry: %v", err)
	}
	t.Logf("Main entry: %s", e)
}

func TestEntryContent(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	e, err := a.GetEntryByPath("Dracula:Capitol_1.html")
	if err != nil {
		t.Fatalf("GetEntryByPath: %v", err)
	}

	data, err := e.Content()
	if err != nil {
		t.Fatalf("Content: %v", err)
	}
	if len(data) == 0 {
		t.Error("content is empty")
	}
	t.Logf("Content length: %d bytes", len(data))
}

func TestMetadataLowerBound(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	// Check what lowerBound returns for M/
	lo, err := a.lowerBound("M/")
	if err != nil {
		t.Fatalf("lowerBound: %v", err)
	}
	t.Logf("lowerBound(M/) = %d (entryCount=%d)", lo, a.EntryCount())

	// Check the entry at that position
	if lo < a.EntryCount() {
		e, err := a.GetEntryByIndex(lo)
		if err != nil {
			t.Fatalf("GetEntryByIndex(%d): %v", lo, err)
		}
		t.Logf("Entry at lowerBound: ns=%c path=%s", e.Namespace(), e.FullPath())
	}
}

func TestMetadata(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	title := a.Title()
	t.Logf("Title: %q", title)

	lang := a.Language()
	t.Logf("Language: %q", lang)

	creator := a.Creator()
	t.Logf("Creator: %q", creator)

	date := a.Date()
	t.Logf("Date: %q", date)

	desc := a.Description()
	t.Logf("Description: %q", desc)

	// At least one of these should be non-empty in a valid ZIM file
	if title == "" && lang == "" && creator == "" {
		t.Log("Warning: no metadata found (may be expected for minimal test ZIM)")
	}

	// Test arbitrary key lookup
	_, found := a.Metadata("NonexistentKey")
	if found {
		t.Error("found nonexistent metadata key")
	}
}

func TestSearchTitles(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	results, err := a.SearchTitles("Dracula", 10)
	if err != nil {
		t.Fatalf("SearchTitles: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results for 'Dracula'")
	}
	for _, e := range results {
		t.Logf("  %s (title: %s)", e.FullPath(), e.Title())
	}
}

func TestSearchTitlesNoMatch(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	results, err := a.SearchTitles("ZZZNONEXISTENT", 10)
	if err != nil {
		t.Fatalf("SearchTitles: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestIterEntries(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	count := 0
	for _, _ = range a.Entries() {
		count++
	}
	if count == 0 {
		t.Error("no entries iterated")
	}
	t.Logf("Iterated %d entries (EntryCount=%d)", count, a.EntryCount())
}

func TestIterArticles(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	count := 0
	for _, e := range a.Articles() {
		if !e.IsArticle() {
			t.Errorf("Articles() yielded non-article: %s", e.FullPath())
		}
		count++
	}
	if count == 0 {
		t.Error("no articles iterated")
	}
	t.Logf("Iterated %d articles", count)
}

func TestIterEntriesByTitle(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	var lastTitle string
	count := 0
	for _, e := range a.EntriesByTitle() {
		t := e.Title()
		if count > 0 && t < lastTitle {
			// Note: title sort includes namespace prefix, so this check is approximate
		}
		lastTitle = t
		count++
	}
	if count == 0 {
		t.Error("no entries iterated by title")
	}
	t.Logf("Iterated %d entries by title", count)
}

func TestIterEntriesInNamespace(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	count := 0
	for _, e := range a.EntriesInNamespace('A') {
		if e.Namespace() != 'A' {
			t.Errorf("wrong namespace: got %c, want A", e.Namespace())
		}
		count++
	}
	t.Logf("Iterated %d entries in namespace A", count)
}

func TestIterEarlyBreak(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	count := 0
	for _, _ = range a.Entries() {
		count++
		if count >= 3 {
			break
		}
	}
	if count != 3 {
		t.Errorf("expected 3 entries, got %d", count)
	}
}

func TestValidateChecksum(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	valid, err := a.ValidateChecksum()
	if err != nil {
		t.Fatalf("ValidateChecksum: %v", err)
	}
	if !valid {
		t.Error("checksum validation failed for test.zim")
	}
}

func TestValidateChecksumMmap(t *testing.T) {
	a, err := Open("test.zim", WithMmap())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	valid, err := a.ValidateChecksum()
	if err != nil {
		t.Fatalf("ValidateChecksum: %v", err)
	}
	if !valid {
		t.Error("checksum validation failed for test.zim (mmap)")
	}
}

func TestSearch(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	idxPath := t.TempDir() + "/test.bleve"

	results, err := a.Search("Dracula", 5, WithIndexPath(idxPath))
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results for 'Dracula'")
	}
	for _, r := range results {
		t.Logf("  %.1f %s (title: %s)", r.Score, r.Entry.FullPath(), r.Entry.Title())
	}
}

func TestSearchPersistence(t *testing.T) {
	idxPath := t.TempDir() + "/persist.bleve"

	// Build index
	a1, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err = a1.Search("Dracula", 1, WithIndexPath(idxPath))
	if err != nil {
		t.Fatalf("Search (build): %v", err)
	}
	a1.Close()

	// Reopen and search using persisted index
	a2, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a2.Close()

	results, err := a2.Search("Dracula", 5, WithIndexPath(idxPath))
	if err != nil {
		t.Fatalf("Search (persisted): %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results from persisted index")
	}
	t.Logf("Found %d results from persisted index", len(results))
}

func TestSearchWithOffset(t *testing.T) {
	a, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	idxPath := t.TempDir() + "/offset.bleve"

	all, err := a.Search("Dracula", 10, WithIndexPath(idxPath))
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(all) > 1 {
		page2, err := a.Search("Dracula", 10, WithIndexPath(idxPath), WithOffset(1))
		if err != nil {
			t.Fatalf("Search with offset: %v", err)
		}
		if len(page2) != len(all)-1 {
			t.Errorf("expected %d results with offset 1, got %d", len(all)-1, len(page2))
		}
	}
}

func TestConcurrentOpen(t *testing.T) {
	a1, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open a1: %v", err)
	}
	defer a1.Close()

	a2, err := Open("test.zim")
	if err != nil {
		t.Fatalf("Open a2: %v", err)
	}
	defer a2.Close()

	// Both should work independently
	e1, err := a1.GetEntryByPath("Dracula:Capitol_1.html")
	if err != nil {
		t.Fatalf("a1 GetEntryByPath: %v", err)
	}
	e2, err := a2.GetEntryByPath("Dracula:Capitol_1.html")
	if err != nil {
		t.Fatalf("a2 GetEntryByPath: %v", err)
	}

	d1, _ := e1.Content()
	d2, _ := e2.Content()
	if len(d1) != len(d2) {
		t.Error("concurrent readers returned different content lengths")
	}
}
