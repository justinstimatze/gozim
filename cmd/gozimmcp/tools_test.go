package main

import (
	"encoding/base64"
	"testing"
	"unicode/utf8"
)

func TestIsTextual(t *testing.T) {
	textual := []string{"text/html", "text/plain", "text/css", "application/xml",
		"image/svg+xml", "application/xhtml+xml", "application/json", "application/javascript"}
	for _, m := range textual {
		if !isTextual(m) {
			t.Errorf("isTextual(%q) = false, want true", m)
		}
	}
	binary := []string{"image/png", "image/jpeg", "application/octet-stream", "font/woff2", "audio/mpeg"}
	for _, m := range binary {
		if isTextual(m) {
			t.Errorf("isTextual(%q) = true, want false", m)
		}
	}
}

func TestClipContentUTF8Boundary(t *testing.T) {
	// "日本語" is three 3-byte runes (9 bytes). Truncating at maxBytes that lands
	// mid-rune must back off to a complete rune, never emit a partial.
	s := "ab日本語" // bytes: a(1) b(1) 日(3) 本(3) 語(3) = 11 bytes
	content := []byte(s)

	for maxBytes := 1; maxBytes <= len(content); maxBytes++ {
		off, out, enc, trunc := clipContent(content, 0, maxBytes, true)
		if off != 0 {
			t.Errorf("maxBytes=%d: offset moved to %d", maxBytes, off)
		}
		if enc != "utf-8" {
			t.Errorf("maxBytes=%d: encoding = %q", maxBytes, enc)
		}
		if !utf8.ValidString(out) {
			t.Errorf("maxBytes=%d: result %q is not valid UTF-8", maxBytes, out)
		}
		// Result must be a rune-aligned prefix of the original.
		if len(out) > maxBytes {
			t.Errorf("maxBytes=%d: result len %d exceeds limit", maxBytes, len(out))
		}
		wantTrunc := len(out) < len(content)
		if trunc != wantTrunc {
			t.Errorf("maxBytes=%d: truncated=%v, want %v (out=%q)", maxBytes, trunc, wantTrunc, out)
		}
	}
}

func TestClipContentOffsetAlignment(t *testing.T) {
	// Offset landing inside the 3-byte 日 must advance to the next rune start.
	content := []byte("ab日本語")                         // 日 starts at byte 2
	off, out, _, _ := clipContent(content, 3, 0, true) // 3 is a continuation byte of 日
	if off != 5 {                                      // next rune start (本) is at byte 5
		t.Errorf("offset = %d, want 5", off)
	}
	if !utf8.ValidString(out) || out != "本語" {
		t.Errorf("out = %q, want %q", out, "本語")
	}
}

func TestClipContentBinary(t *testing.T) {
	content := []byte{0x89, 0x50, 0x4e, 0x47, 0xff, 0xfe, 0x00, 0x01} // PNG-ish bytes
	off, out, enc, trunc := clipContent(content, 0, 0, false)
	if off != 0 || enc != "base64" || trunc {
		t.Fatalf("got off=%d enc=%q trunc=%v", off, enc, trunc)
	}
	decoded, err := base64.StdEncoding.DecodeString(out)
	if err != nil {
		t.Fatalf("result is not valid base64: %v", err)
	}
	if string(decoded) != string(content) {
		t.Errorf("base64 round-trip mismatch")
	}
}

func TestClipContentNoLimit(t *testing.T) {
	content := []byte("hello world")
	off, out, enc, trunc := clipContent(content, 0, 0, true)
	if off != 0 || out != "hello world" || enc != "utf-8" || trunc {
		t.Errorf("unexpected: off=%d out=%q enc=%q trunc=%v", off, out, enc, trunc)
	}
}

func TestClipContentOffsetBeyondEnd(t *testing.T) {
	content := []byte("hi")
	off, out, _, trunc := clipContent(content, 100, 50, true)
	if off != 2 || out != "" || trunc {
		t.Errorf("unexpected: off=%d out=%q trunc=%v", off, out, trunc)
	}
}
