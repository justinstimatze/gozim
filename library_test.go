package zim

import (
	"os"
	"path/filepath"
	"testing"
)

// makeLibraryDir creates a temp directory with symlinks to test.zim
// under the given names. Returns the directory path.
func makeLibraryDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(cwd, "test.zim")
	for _, name := range names {
		if err := os.Symlink(src, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestOpenLibrary(t *testing.T) {
	dir := makeLibraryDir(t, "alpha.zim", "beta.zim")

	lib, err := OpenLibrary(dir)
	if err != nil {
		t.Fatalf("OpenLibrary: %v", err)
	}
	defer lib.Close()

	if lib.Len() != 2 {
		t.Fatalf("expected 2 archives, got %d", lib.Len())
	}

	slugs := lib.Slugs()
	if len(slugs) != 2 || slugs[0] != "alpha" || slugs[1] != "beta" {
		t.Errorf("unexpected slugs: %v", slugs)
	}

	if errs := lib.Errors(); len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
}

func TestLibraryGet(t *testing.T) {
	dir := makeLibraryDir(t, "wiki.zim")

	lib, err := OpenLibrary(dir)
	if err != nil {
		t.Fatalf("OpenLibrary: %v", err)
	}
	defer lib.Close()

	entry, ok := lib.Get("wiki")
	if !ok {
		t.Fatal("Get(wiki) not found")
	}
	if entry.Archive.EntryCount() == 0 {
		t.Error("archive has zero entries")
	}
	if entry.Slug != "wiki" {
		t.Errorf("slug = %q, want wiki", entry.Slug)
	}

	_, ok = lib.Get("nonexistent")
	if ok {
		t.Error("Get(nonexistent) should return false")
	}
}

func TestLibraryEntries(t *testing.T) {
	dir := makeLibraryDir(t, "charlie.zim", "alpha.zim")

	lib, err := OpenLibrary(dir)
	if err != nil {
		t.Fatalf("OpenLibrary: %v", err)
	}
	defer lib.Close()

	var slugs []string
	for slug, entry := range lib.Entries() {
		slugs = append(slugs, slug)
		if entry.Archive == nil {
			t.Errorf("nil archive for slug %s", slug)
		}
	}

	// Should be alphabetical regardless of filesystem order
	if len(slugs) != 2 || slugs[0] != "alpha" || slugs[1] != "charlie" {
		t.Errorf("unexpected iteration order: %v", slugs)
	}
}

func TestLibraryEmptyDir(t *testing.T) {
	dir := t.TempDir() // empty

	_, err := OpenLibrary(dir)
	if err == nil {
		t.Error("expected error for empty directory")
	}
}

func TestLibraryBadDir(t *testing.T) {
	_, err := OpenLibrary("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestLibrarySlugCollision(t *testing.T) {
	// Create two files that slugify to the same thing
	dir := makeLibraryDir(t, "my_file.zim", "my-file.zim")

	lib, err := OpenLibrary(dir)
	if err != nil {
		t.Fatalf("OpenLibrary: %v", err)
	}
	defer lib.Close()

	if lib.Len() != 2 {
		t.Fatalf("expected 2 archives, got %d", lib.Len())
	}

	// One should be "my-file", the other "my-file-2"
	slugs := lib.Slugs()
	t.Logf("slugs: %v", slugs)
	if len(slugs) != 2 {
		t.Fatalf("expected 2 slugs, got %d", len(slugs))
	}

	// Both should be retrievable
	for _, s := range slugs {
		if _, ok := lib.Get(s); !ok {
			t.Errorf("Get(%q) failed", s)
		}
	}
}

func TestLibraryWithMmap(t *testing.T) {
	dir := makeLibraryDir(t, "test.zim")

	lib, err := OpenLibrary(dir, WithMmap())
	if err != nil {
		t.Fatalf("OpenLibrary with mmap: %v", err)
	}
	defer lib.Close()

	entry, ok := lib.Get("test")
	if !ok {
		t.Fatal("Get(test) not found")
	}
	if entry.Archive.EntryCount() == 0 {
		t.Error("archive has zero entries with mmap")
	}
}

func TestLibraryIndexDiscovery(t *testing.T) {
	dir := makeLibraryDir(t, "wiki.zim")

	// Create a fake .bleve directory
	bleveDir := filepath.Join(dir, "wiki.bleve")
	if err := os.Mkdir(bleveDir, 0755); err != nil {
		t.Fatal(err)
	}

	lib, err := OpenLibrary(dir)
	if err != nil {
		t.Fatalf("OpenLibrary: %v", err)
	}
	defer lib.Close()

	entry, _ := lib.Get("wiki")
	if entry.IndexPath != bleveDir {
		t.Errorf("IndexPath = %q, want %q", entry.IndexPath, bleveDir)
	}
}

func TestLibraryPartialFailure(t *testing.T) {
	dir := makeLibraryDir(t, "good.zim")

	// Create a corrupt "ZIM" file
	corrupt := filepath.Join(dir, "bad.zim")
	os.WriteFile(corrupt, []byte("not a zim file"), 0644)

	lib, err := OpenLibrary(dir)
	if err != nil {
		t.Fatalf("OpenLibrary should succeed with partial failure: %v", err)
	}
	defer lib.Close()

	if lib.Len() != 1 {
		t.Errorf("expected 1 loaded archive, got %d", lib.Len())
	}
	if len(lib.Errors()) != 1 {
		t.Errorf("expected 1 error, got %d", len(lib.Errors()))
	}
	t.Logf("partial failure error: %v", lib.Errors()[0])
}

func TestLibraryClose(t *testing.T) {
	dir := makeLibraryDir(t, "a.zim", "b.zim")

	lib, err := OpenLibrary(dir)
	if err != nil {
		t.Fatalf("OpenLibrary: %v", err)
	}

	if err := lib.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"wikipedia_en_all_2024-01.zim", "wikipedia-en-all-2024-01"},
		{"simple.zim", "simple"},
		{"UPPER.zim", "upper"},
		{"a__b--c.zim", "a-b-c"},
		{"___.zim", ""},   // edge case: all non-alnum
		{"123.zim", "123"},
		{"hello world.zim", "hello-world"},
	}
	for _, tt := range tests {
		got := slugify(tt.in)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
