package streamproxy

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	"iptv2hdhr/internal/config"
	"iptv2hdhr/internal/lineup"
	"iptv2hdhr/internal/playlist"
)

func newTestLineup(t *testing.T) *lineup.Lineup {
	t.Helper()
	l, err := lineup.New([]config.Channel{
		{Number: "1001", Name: "ESPN HD", TvgID: "ESPN.us"},
		{Number: "1004", Name: "Reserved Slot"},
	})
	if err != nil {
		t.Fatalf("lineup.New: %v", err)
	}
	return l
}

func TestServeHTTP_UnknownChannel404(t *testing.T) {
	l := newTestLineup(t)
	h := New(l)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream/9999", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestServeHTTP_ConfiguredButUnmatched503(t *testing.T) {
	l := newTestLineup(t)
	l.Rebuild(nil) // no entries: 1001 stays unmatched
	h := New(l)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream/1001", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestServeHTTP_ReservedSlotWithNoMatchRule503(t *testing.T) {
	l := newTestLineup(t)
	l.Rebuild(nil)
	h := New(l)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream/1004", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestServeHTTP_StreamsUpstreamBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.Write([]byte("mpegts-data-chunk"))
	}))
	defer upstream.Close()

	l := newTestLineup(t)
	l.Rebuild([]playlist.Entry{
		{Name: "ESPN HD", TvgID: "ESPN.us", URL: upstream.URL},
	})
	h := New(l)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream/1001", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Body.String(); got != "mpegts-data-chunk" {
		t.Errorf("body = %q, want %q", got, "mpegts-data-chunk")
	}
	if ct := w.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Errorf("Content-Type = %q, want %q", ct, "video/mp2t")
	}
}

func TestServeHTTP_UpstreamErrorMapsTo503(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer upstream.Close()

	l := newTestLineup(t)
	l.Rebuild([]playlist.Entry{
		{Name: "ESPN HD", TvgID: "ESPN.us", URL: upstream.URL},
	})
	h := New(l)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream/1001", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d (upstream 404 must not become our 404)", w.Code, http.StatusServiceUnavailable)
	}
}

func TestIsHLS(t *testing.T) {
	tests := []struct {
		name        string
		upstreamURL string
		contentType string
		want        bool
	}{
		{"mpegts content-type", "https://example.com/stream", "video/mp2t", false},
		{"no content-type, raw url", "https://example.com/stream", "", false},
		{"x-mpegurl content-type", "https://example.com/stream", "application/x-mpegurl; charset=utf-8", true},
		{"apple mpegurl content-type", "https://example.com/stream", "application/vnd.apple.mpegurl", true},
		{"m3u8 extension, no content-type", "https://example.com/playlist.m3u8?x=1", "", true},
		{"m3u extension, no content-type", "https://example.com/playlist.m3u", "application/octet-stream", true},
		{"ts extension", "https://example.com/segment.ts", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHLS(tt.upstreamURL, tt.contentType); got != tt.want {
				t.Errorf("isHLS(%q, %q) = %v, want %v", tt.upstreamURL, tt.contentType, got, tt.want)
			}
		})
	}
}

func TestServeHTTP_NonHLSUpstreamIsPassedThroughWithoutFfmpeg(t *testing.T) {
	old := ffmpegPath
	ffmpegPath = "/nonexistent/ffmpeg"
	defer func() { ffmpegPath = old }()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.Write([]byte("mpegts-data-chunk"))
	}))
	defer upstream.Close()

	l := newTestLineup(t)
	l.Rebuild([]playlist.Entry{
		{Name: "ESPN HD", TvgID: "ESPN.us", URL: upstream.URL},
	})
	h := New(l)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream/1001", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Body.String(); got != "mpegts-data-chunk" {
		t.Errorf("body = %q, want %q", got, "mpegts-data-chunk")
	}
}

func TestServeHTTP_HLSUpstreamWithoutFfmpegReturns503(t *testing.T) {
	old := ffmpegPath
	ffmpegPath = "/nonexistent/ffmpeg"
	defer func() { ffmpegPath = old }()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-mpegurl")
		w.Write([]byte("#EXTM3U\n"))
	}))
	defer upstream.Close()

	l := newTestLineup(t)
	l.Rebuild([]playlist.Entry{
		{Name: "ESPN HD", TvgID: "ESPN.us", URL: upstream.URL},
	})
	h := New(l)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream/1001", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestServeHTTP_HLSUpstreamIsRemuxedToMPEGTS(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	dir := t.TempDir()
	playlistPath := filepath.Join(dir, "playlist.m3u8")
	generate := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=duration=1:size=64x64:rate=10",
		"-c:v", "libx264", "-f", "hls", "-hls_time", "1", "-hls_list_size", "0",
		"-hls_segment_filename", filepath.Join(dir, "segment%d.ts"),
		playlistPath,
	)
	if out, err := generate.CombinedOutput(); err != nil {
		t.Skipf("could not generate HLS fixture: %v: %s", err, out)
	}

	upstream := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer upstream.Close()

	l := newTestLineup(t)
	l.Rebuild([]playlist.Entry{
		{Name: "ESPN HD", TvgID: "ESPN.us", URL: upstream.URL + "/playlist.m3u8"},
	})
	h := New(l)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream/1001", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Errorf("Content-Type = %q, want %q", ct, "video/mp2t")
	}
	body := w.Body.Bytes()
	if len(body) == 0 {
		t.Fatal("remuxed body is empty")
	}
	if body[0] != 0x47 {
		t.Errorf("remuxed body does not start with MPEG-TS sync byte: got %#x", body[0])
	}
}
