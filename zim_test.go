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
		log.Panicf("Can't open: %v", err)
	}
}

func TestOpen(t *testing.T) {
	if A.EntryCount() == 0 {
		t.Error("no entries")
	}
}

func TestOpenMmap(t *testing.T) {
	a, err := Open("test.zim", WithMmap())
	if err != nil {
		t.Fatalf("Open mmap: %v", err)
	}
	defer a.Close()
	if a.EntryCount() == 0 {
		t.Error("no entries")
	}
}

func TestMimeTypes(t *testing.T) {
	if len(A.MimeTypes()) == 0 {
		t.Error("no mime types")
	}
}

func TestReaderString(t *testing.T) {
	s := A.String()
	if s == "" {
		t.Error("empty string")
	}
	t.Log(s)
}

func TestContent(t *testing.T) {
	e, err := A.GetEntryByIndex(5)
	if err != nil {
		t.Fatalf("GetEntryByIndex: %v", err)
	}
	if !e.IsRedirect() {
		data, err := e.Content()
		if err != nil {
			t.Fatalf("Content: %v", err)
		}
		if len(data) == 0 {
			t.Error("empty content")
		}
	}
}

func TestListEntries(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	var count uint32
	for range A.Entries() {
		count++
	}
	if count == 0 {
		t.Error("no entries")
	}
	t.Logf("%d entries (EntryCount=%d)", count, A.EntryCount())
}

func BenchmarkContent(b *testing.B) {
	e, err := A.GetEntryByIndex(5)
	if err != nil {
		b.Fatalf("GetEntryByIndex: %v", err)
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
