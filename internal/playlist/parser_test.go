package playlist

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_SamplePlaylist(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "sample.m3u"))
	if err != nil {
		t.Fatalf("reading sample.m3u: %v", err)
	}

	entries, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	// NONAME.us (no name, no tvg-name) and ORPHAN.us (no following URL) are
	// both skipped, leaving 4 valid entries.
	if len(entries) != 4 {
		t.Fatalf("len(entries) = %d, want 4: %+v", len(entries), entries)
	}

	espn := entries[0]
	if espn.Name != "ESPN HD" {
		t.Errorf("entries[0].Name = %q, want %q", espn.Name, "ESPN HD")
	}
	if espn.TvgID != "ESPN.us" {
		t.Errorf("entries[0].TvgID = %q, want %q", espn.TvgID, "ESPN.us")
	}
	if espn.TvgLogo != "http://example.com/espn.png" {
		t.Errorf("entries[0].TvgLogo = %q, want %q", espn.TvgLogo, "http://example.com/espn.png")
	}
	if espn.GroupTitle != "Sports" {
		t.Errorf("entries[0].GroupTitle = %q, want %q", espn.GroupTitle, "Sports")
	}
	if espn.URL != "http://example.com/streams/espn" {
		t.Errorf("entries[0].URL = %q, want %q", espn.URL, "http://example.com/streams/espn")
	}

	cnn := entries[1]
	if cnn.TvgName != "CNN International" {
		t.Errorf("entries[1].TvgName = %q, want %q", cnn.TvgName, "CNN International")
	}

	local := entries[2]
	if local.TvgID != "" {
		t.Errorf("entries[2].TvgID = %q, want empty", local.TvgID)
	}
	if local.TvgName != "Local News, Channel 5" {
		t.Errorf("entries[2].TvgName = %q, want %q (with embedded comma)", local.TvgName, "Local News, Channel 5")
	}
	if local.Name != "Local News 5" {
		t.Errorf("entries[2].Name = %q, want %q", local.Name, "Local News 5")
	}

	last := entries[3]
	if last.Name != "Last Channel HD" {
		t.Errorf("entries[3].Name = %q, want %q", last.Name, "Last Channel HD")
	}
	if last.TvgID != "LASTCH.us" {
		t.Errorf("entries[3].TvgID = %q, want %q", last.TvgID, "LASTCH.us")
	}
}

func TestParse_MissingEXTM3UHeader(t *testing.T) {
	data := []byte("#EXTINF:-1,Channel\nhttp://example.com/stream\n")

	_, err := Parse(data)
	if err != ErrNotExtendedM3U {
		t.Errorf("Parse error = %v, want %v", err, ErrNotExtendedM3U)
	}
}

func TestParse_RejectsHLSPlaylist(t *testing.T) {
	data := []byte("#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXTINF:5,\nsegment0.ts\n")

	_, err := Parse(data)
	if err != ErrNotExtendedM3U {
		t.Errorf("Parse error = %v, want %v", err, ErrNotExtendedM3U)
	}
}

func TestParse_EmptyPlaylist(t *testing.T) {
	data := []byte("#EXTM3U\n")

	entries, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("len(entries) = %d, want 0", len(entries))
	}
}

func TestParse_EXTVLCOPTHeaders(t *testing.T) {
	data := []byte(`#EXTM3U
#EXTINF:-1 tvg-id="A.us",Channel A
#EXTVLCOPT:http-referrer=https://example.com/player
#EXTVLCOPT:http-user-agent=Mozilla/5.0
#EXTVLCOPT:network-caching=1000
http://example.com/a.m3u8
#EXTINF:-1 tvg-id="B.us",Channel B
http://example.com/b.m3u8
`)

	entries, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2: %+v", len(entries), entries)
	}

	a := entries[0]
	if a.URL != "http://example.com/a.m3u8" {
		t.Errorf("entries[0].URL = %q, want %q", a.URL, "http://example.com/a.m3u8")
	}
	if got := a.Headers["Referer"]; got != "https://example.com/player" {
		t.Errorf("entries[0].Headers[Referer] = %q, want %q", got, "https://example.com/player")
	}
	if got := a.Headers["User-Agent"]; got != "Mozilla/5.0" {
		t.Errorf("entries[0].Headers[User-Agent] = %q, want %q", got, "Mozilla/5.0")
	}
	if len(a.Headers) != 2 {
		t.Errorf("entries[0].Headers = %v, want only Referer and User-Agent (network-caching ignored)", a.Headers)
	}

	// Options must not leak into the next entry.
	if entries[1].Headers != nil {
		t.Errorf("entries[1].Headers = %v, want nil", entries[1].Headers)
	}
}

func TestParse_PipeSuffixHeaders(t *testing.T) {
	data := []byte(`#EXTM3U
#EXTINF:-1 tvg-id="A.us",Channel A
#EXTVLCOPT:http-referrer=https://vlcopt.example.com/
http://example.com/a.m3u8?token=x|Referer=https%3A%2F%2Fexample.com%2F&user-agent=Foo/1.0
`)

	entries, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1: %+v", len(entries), entries)
	}

	a := entries[0]
	if a.URL != "http://example.com/a.m3u8?token=x" {
		t.Errorf("URL = %q, want suffix stripped", a.URL)
	}
	// The pipe suffix wins over #EXTVLCOPT for the same header, and its
	// values are percent-decoded.
	if got := a.Headers["Referer"]; got != "https://example.com/" {
		t.Errorf("Headers[Referer] = %q, want %q", got, "https://example.com/")
	}
	// Header names are canonicalized regardless of how they were written.
	if got := a.Headers["User-Agent"]; got != "Foo/1.0" {
		t.Errorf("Headers[User-Agent] = %q, want %q", got, "Foo/1.0")
	}
}
