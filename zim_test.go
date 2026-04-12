package zim

import (
	"log"
	"testing"
)

var A *Archive

func init() {
	var err error
	A, err = Open("test.zim")
	if err != nil {
		log.Panicf("Can't read %v", err)
	}
}

func TestOpen(t *testing.T) {
	if A.EntryCount() == 0 {
		t.Errorf("No entries found")
	}
}

func TestOpenMmap(t *testing.T) {
	a, err := Open("test.zim", WithMmap())
	if err != nil {
		t.Errorf("Can't read %v", err)
	}
	if a.EntryCount() == 0 {
		t.Errorf("No entries found")
	}
	a.Close()
}

func TestMime(t *testing.T) {
	if len(A.MimeTypes()) == 0 {
		t.Errorf("No mime types found")
	}
}

func TestDisplayInfost(t *testing.T) {
	info := A.String()
	if len(info) == 0 {
		t.Errorf("Can't read infos")
	}
	t.Log(info)
}

func TestURLAtIdx(t *testing.T) {
	e, err := A.GetEntryByIndex(5)
	if err != nil {
		t.Errorf("Can't find entry at index 5: %v", err)
	}
	t.Log(e)
}

func TestDisplayEntry(t *testing.T) {
	e, err := A.GetEntryByIndex(5)
	if err != nil {
		t.Errorf("Can't find entry at index 5: %v", err)
	}
	t.Log(e)
}

func TestPageNoIndex(t *testing.T) {
	_, err := A.GetEntryByFullPath("A/Dracula:Capitol_1.html")
	if err != nil {
		t.Errorf("Can't find existing url: %v", err)
	}
}

func TestListArticles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	var i uint32
	for _, _ = range A.Entries() {
		i++
	}

	if i == 0 {
		t.Errorf("Can't find any entries")
	}

	t.Logf("Found %d entries (EntryCount=%d)", i, A.EntryCount())
}

func TestMainPage(t *testing.T) {
	e, err := A.MainEntry()
	if err != nil {
		t.Errorf("Can't find the main entry: %v", err)
	}
	t.Log(e)
}

func TestData(t *testing.T) {
	e, err := A.GetEntryByIndex(2)
	if err != nil {
		t.Errorf("Can't find entry: %v", err)
	}
	if !e.IsRedirect() {
		data, err := e.Content()
		if err != nil {
			t.Errorf("Can't read content: %v", err)
		}
		if len(data) == 0 {
			t.Error("content is empty")
		}
	}
	t.Log(e)
}

func BenchmarkEntryContent(b *testing.B) {
	e, err := A.GetEntryByIndex(5)
	if err != nil {
		b.Fatalf("Can't find entry: %v", err)
	}
	data, err := e.Content()
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Content()
		A.r.blobCache.Purge()
	}
}
