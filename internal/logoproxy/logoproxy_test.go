package logoproxy

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"iptv2hdhr/internal/config"
	"iptv2hdhr/internal/lineup"
)

func newTestLineup(t *testing.T, logoURL string) *lineup.Lineup {
	t.Helper()
	l, err := lineup.New([]config.Channel{
		{Number: "1001", Name: "ESPN HD", Logo: logoURL},
		{Number: "1004", Name: "Reserved Slot"},
	})
	if err != nil {
		t.Fatalf("lineup.New: %v", err)
	}
	return l
}

func TestServeHTTP_FetchesAndServesLogo(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake-png-data"))
	}))
	defer upstream.Close()

	l := newTestLineup(t, upstream.URL)
	h := New(l)

	r := httptest.NewRequest("GET", "/logo/1001", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want %q", ct, "image/png")
	}
	if w.Body.String() != "fake-png-data" {
		t.Errorf("body = %q, want %q", w.Body.String(), "fake-png-data")
	}
}

func TestServeHTTP_CachesAfterFirstFetch(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake-png-data"))
	}))
	defer upstream.Close()

	l := newTestLineup(t, upstream.URL)
	h := New(l)

	for i := 0; i < 3; i++ {
		r := httptest.NewRequest("GET", "/logo/1001", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i, w.Code, http.StatusOK)
		}
	}

	if got := requests.Load(); got != 1 {
		t.Errorf("upstream requests = %d, want 1 (subsequent requests should be served from cache)", got)
	}
}

func TestServeHTTP_UnknownChannel404(t *testing.T) {
	l := newTestLineup(t, "http://example.com/logo.png")
	h := New(l)

	r := httptest.NewRequest("GET", "/logo/9999", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestServeHTTP_NoLogoConfigured404(t *testing.T) {
	l := newTestLineup(t, "http://example.com/logo.png")
	h := New(l)

	r := httptest.NewRequest("GET", "/logo/1004", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestServeHTTP_UpstreamErrorReturnsBadGateway(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	l := newTestLineup(t, upstream.URL)
	h := New(l)

	r := httptest.NewRequest("GET", "/logo/1001", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}
