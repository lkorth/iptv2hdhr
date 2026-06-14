package guide

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"iptv2hdhr/internal/config"
)

const guideASource = `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="ESPN.us"><display-name>ESPN</display-name></channel>
  <programme channel="ESPN.us" start="20240613170000 +0000" stop="20240613180000 +0000">
    <title>SportsCenter</title>
  </programme>
  <programme channel="ESPN.us" start="20240613180000 +0000" stop="20240613190000 +0000">
    <title>NBA Tonight</title>
  </programme>
</tv>`

const guideBSource = `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="CNN.us"><display-name>CNN</display-name></channel>
  <programme channel="CNN.us" start="20240613170000 +0000" stop="20240613180000 +0000">
    <title>CNN Newsroom</title>
  </programme>
</tv>`

func TestFetcher_Current_NonNilBeforeAnyRefresh(t *testing.T) {
	f := NewFetcher(nil)
	idx := f.Current()
	if idx == nil {
		t.Fatal("Current() = nil, want non-nil empty Index")
	}
	if len(idx.ProgrammesByChannelID) != 0 || len(idx.ChannelMeta) != 0 {
		t.Errorf("Current() = %+v, want empty", idx)
	}
}

func TestFetcher_RefreshNow_MergesMultipleSources(t *testing.T) {
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(guideASource))
	}))
	defer srvA.Close()

	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(guideBSource))
	}))
	defer srvB.Close()

	f := NewFetcher([]config.GuideSource{
		{Name: "a", URL: srvA.URL},
		{Name: "b", URL: srvB.URL},
	})

	f.RefreshNow(context.Background())

	idx := f.Current()
	if _, ok := idx.ChannelMeta["ESPN.us"]; !ok {
		t.Errorf("ChannelMeta missing ESPN.us: %+v", idx.ChannelMeta)
	}
	if _, ok := idx.ChannelMeta["CNN.us"]; !ok {
		t.Errorf("ChannelMeta missing CNN.us: %+v", idx.ChannelMeta)
	}

	espn := idx.ProgrammesByChannelID["ESPN.us"]
	if len(espn) != 2 {
		t.Fatalf("len(ProgrammesByChannelID[ESPN.us]) = %d, want 2: %+v", len(espn), espn)
	}
	if espn[0].Title[0].Value != "SportsCenter" {
		t.Errorf("espn[0].Title = %v, want SportsCenter (sorted by start)", espn[0].Title)
	}
	if espn[1].Title[0].Value != "NBA Tonight" {
		t.Errorf("espn[1].Title = %v, want NBA Tonight", espn[1].Title)
	}

	cnn := idx.ProgrammesByChannelID["CNN.us"]
	if len(cnn) != 1 {
		t.Fatalf("len(ProgrammesByChannelID[CNN.us]) = %d, want 1: %+v", len(cnn), cnn)
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
			w.Write([]byte(guideASource)) // 2 programmes for ESPN.us
		} else {
			w.Write([]byte(guideBSource)) // CNN.us only
		}
	}))
	defer srv.Close()

	f := NewFetcher([]config.GuideSource{{Name: "a", URL: srv.URL}})

	f.RefreshNow(context.Background())
	if len(f.Current().ProgrammesByChannelID["ESPN.us"]) != 2 {
		t.Fatalf("after first refresh: ProgrammesByChannelID[ESPN.us] = %+v, want 2 entries", f.Current().ProgrammesByChannelID["ESPN.us"])
	}

	// Second refresh: server reports 304 Not Modified for the etag we
	// learned from the first response, so the (different) body it would
	// otherwise serve must not be parsed.
	f.RefreshNow(context.Background())
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if len(f.Current().ProgrammesByChannelID["ESPN.us"]) != 2 {
		t.Errorf("after second (not-modified) refresh: ProgrammesByChannelID[ESPN.us] = %+v, want last-good 2 entries preserved", f.Current().ProgrammesByChannelID["ESPN.us"])
	}
}

func TestFetcher_RefreshNow_PreservesLastGoodOnFailure(t *testing.T) {
	var fail atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(guideASource))
	}))
	defer srv.Close()

	f := NewFetcher([]config.GuideSource{{Name: "a", URL: srv.URL}})

	f.RefreshNow(context.Background())
	if len(f.Current().ProgrammesByChannelID["ESPN.us"]) != 2 {
		t.Fatalf("after first refresh: ProgrammesByChannelID[ESPN.us] = %+v, want 2 entries", f.Current().ProgrammesByChannelID["ESPN.us"])
	}

	fail.Store(true)
	f.RefreshNow(context.Background())

	if len(f.Current().ProgrammesByChannelID["ESPN.us"]) != 2 {
		t.Errorf("after failed refresh: ProgrammesByChannelID[ESPN.us] = %+v, want last-good 2 entries preserved", f.Current().ProgrammesByChannelID["ESPN.us"])
	}
}
