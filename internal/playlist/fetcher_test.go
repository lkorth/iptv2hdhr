package playlist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"iptv2hdhr/internal/config"
)

const sourceAPlaylist = `#EXTM3U
#EXTINF:-1 tvg-id="A1.us" group-title="Group A",Channel A1
http://example.com/a1
`

const sourceAPlaylistUpdated = `#EXTM3U
#EXTINF:-1 tvg-id="A1.us" group-title="Group A",Channel A1 Updated
http://example.com/a1
#EXTINF:-1 tvg-id="A2.us" group-title="Group A",Channel A2
http://example.com/a2
`

const sourceBPlaylist = `#EXTM3U
#EXTINF:-1 tvg-id="B1.us" group-title="Group B",Channel B1
http://example.com/b1
`

func TestFetcher_Current_NilBeforeAnyRefresh(t *testing.T) {
	f := NewFetcher(nil)
	if got := f.Current(); got != nil {
		t.Errorf("Current() = %v, want nil before any refresh", got)
	}
}

func TestFetcher_RefreshNow_MergesMultipleSources(t *testing.T) {
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sourceAPlaylist))
	}))
	defer srvA.Close()

	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sourceBPlaylist))
	}))
	defer srvB.Close()

	f := NewFetcher([]config.PlaylistSource{
		{Name: "a", URL: srvA.URL},
		{Name: "b", URL: srvB.URL},
	})

	f.RefreshNow(context.Background())

	entries := f.Current()
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2: %+v", len(entries), entries)
	}
	if entries[0].Name != "Channel A1" {
		t.Errorf("entries[0].Name = %q, want %q", entries[0].Name, "Channel A1")
	}
	if entries[1].Name != "Channel B1" {
		t.Errorf("entries[1].Name = %q, want %q", entries[1].Name, "Channel B1")
	}
}

func TestFetcher_RefreshNow_PreservesLastGoodOnFailure(t *testing.T) {
	var failB atomic.Bool

	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sourceAPlaylistUpdated))
	}))
	defer srvA.Close()

	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failB.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(sourceBPlaylist))
	}))
	defer srvB.Close()

	f := NewFetcher([]config.PlaylistSource{
		{Name: "a", URL: srvA.URL},
		{Name: "b", URL: srvB.URL},
	})

	f.RefreshNow(context.Background())
	if len(f.Current()) != 3 {
		t.Fatalf("after first refresh: len(entries) = %d, want 3: %+v", len(f.Current()), f.Current())
	}

	// Now make source B start failing, and source A return different data.
	failB.Store(true)
	f.RefreshNow(context.Background())

	entries := f.Current()
	if len(entries) != 3 {
		t.Fatalf("after second refresh: len(entries) = %d, want 3 (B's last-good preserved): %+v", len(entries), entries)
	}

	var foundB1 bool
	for _, e := range entries {
		if e.TvgID == "B1.us" {
			foundB1 = true
		}
	}
	if !foundB1 {
		t.Errorf("entries missing B1.us from source B's last-good cache: %+v", entries)
	}
}

func TestFetcher_RefreshNow_ConditionalRequestSkipsUnchanged(t *testing.T) {
	var requests int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		if requests == 1 {
			w.Write([]byte(sourceAPlaylist)) // 1 entry
		} else {
			w.Write([]byte(sourceAPlaylistUpdated)) // 2 entries
		}
	}))
	defer srv.Close()

	f := NewFetcher([]config.PlaylistSource{{Name: "a", URL: srv.URL}})

	f.RefreshNow(context.Background())
	if len(f.Current()) != 1 {
		t.Fatalf("after first refresh: len(entries) = %d, want 1: %+v", len(f.Current()), f.Current())
	}

	// Second refresh: server reports 304 Not Modified for the etag we
	// learned from the first response, so the (different) body it would
	// otherwise serve must not be parsed.
	f.RefreshNow(context.Background())
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if len(f.Current()) != 1 {
		t.Errorf("after second (not-modified) refresh: len(entries) = %d, want 1 (unchanged): %+v", len(f.Current()), f.Current())
	}
}

func TestFetcher_RefreshNow_FetchFailureLeavesSourceEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := NewFetcher([]config.PlaylistSource{
		{Name: "a", URL: srv.URL},
	})

	f.RefreshNow(context.Background())

	entries := f.Current()
	if len(entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0: %+v", len(entries), entries)
	}
}
