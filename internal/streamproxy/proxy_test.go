package streamproxy

import (
	"net/http"
	"net/http/httptest"
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
