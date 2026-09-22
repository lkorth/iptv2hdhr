package lineup

import (
	"testing"

	"iptv2hdhr/internal/config"
	"iptv2hdhr/internal/playlist"
)

func sampleConfig() []config.Channel {
	return []config.Channel{
		{Number: "1001", Name: "ESPN HD", Logo: "http://example.com/espn.png", TvgID: "ESPN.us"},
		{Number: "1002", Name: "CNN", TvgID: "CNN.us"},
		{Number: "1003", Name: "Local News 5", NameMatch: "Local News"},
		{Number: "1004", Name: "Reserved Slot"}, // no tvg_id, no name_match: always unmatched
	}
}

func TestNew_BuildsChannelsWithSlotIDs(t *testing.T) {
	l, err := New(sampleConfig())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	channels := l.Channels()
	if len(channels) != 4 {
		t.Fatalf("len(channels) = %d, want 4", len(channels))
	}
	if channels[0].ChannelID != "slot-1001" {
		t.Errorf("channels[0].ChannelID = %q, want %q", channels[0].ChannelID, "slot-1001")
	}
	if channels[0].Name != "ESPN HD" {
		t.Errorf("channels[0].Name = %q, want %q", channels[0].Name, "ESPN HD")
	}
}

func TestNew_DuplicateChannelNumber(t *testing.T) {
	cfg := []config.Channel{
		{Number: "1001", Name: "A", TvgID: "A.us"},
		{Number: "1001", Name: "B", TvgID: "B.us"},
	}

	_, err := New(cfg)
	if err == nil {
		t.Fatal("New with duplicate channel numbers: error = nil, want non-nil")
	}
}

func TestCurrent_BeforeRebuild(t *testing.T) {
	l, err := New(sampleConfig())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	snap := l.Current()
	if len(snap.Channels) != 4 {
		t.Fatalf("snap.Channels len = %d, want 4", len(snap.Channels))
	}
	if len(snap.Streams) != 0 {
		t.Errorf("snap.Streams = %v, want empty before any Rebuild", snap.Streams)
	}
}

func TestRebuild_MatchByTvgID_CaseInsensitive(t *testing.T) {
	l, err := New(sampleConfig())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	entries := []playlist.Entry{
		{Name: "ESPN HD", TvgID: "espn.us", URL: "http://example.com/espn"},
	}
	l.Rebuild(entries)

	s, configured, matched := l.Stream("1001")
	if !configured {
		t.Fatal("channel 1001 should be configured")
	}
	if !matched {
		t.Fatal("channel 1001 should be matched (case-insensitive tvg-id)")
	}
	if s.URL != "http://example.com/espn" {
		t.Errorf("url = %q, want %q", s.URL, "http://example.com/espn")
	}
}

func TestRebuild_MatchByNameMatchFallback(t *testing.T) {
	l, err := New(sampleConfig())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	entries := []playlist.Entry{
		{Name: "Local News 5 HD", TvgID: "", URL: "http://example.com/local5"},
	}
	l.Rebuild(entries)

	s, configured, matched := l.Stream("1003")
	if !configured || !matched {
		t.Fatalf("channel 1003: configured=%v matched=%v, want true,true", configured, matched)
	}
	if s.URL != "http://example.com/local5" {
		t.Errorf("url = %q, want %q", s.URL, "http://example.com/local5")
	}
}

func TestRebuild_CarriesEntryHeaders(t *testing.T) {
	l, err := New(sampleConfig())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	entries := []playlist.Entry{
		{Name: "ESPN HD", TvgID: "ESPN.us", URL: "http://example.com/espn.m3u8",
			Headers: map[string]string{"Referer": "http://example.com/"}},
	}
	l.Rebuild(entries)

	s, _, matched := l.Stream("1001")
	if !matched {
		t.Fatal("channel 1001 should be matched")
	}
	if got := s.Headers["Referer"]; got != "http://example.com/" {
		t.Errorf("Headers[Referer] = %q, want %q", got, "http://example.com/")
	}
}

func TestStream_NotConfigured(t *testing.T) {
	l, err := New(sampleConfig())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, configured, _ := l.Stream("9999")
	if configured {
		t.Error("channel 9999 is not configured, want configured=false")
	}
}

func TestStream_ConfiguredButUnmatched(t *testing.T) {
	l, err := New(sampleConfig())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	l.Rebuild(nil)

	s, configured, matched := l.Stream("1004")
	if !configured {
		t.Fatal("channel 1004 should be configured")
	}
	if matched {
		t.Errorf("channel 1004 should be unmatched, got url %q", s.URL)
	}
}

// TestRebuild_LineupShapeNeverChanges is the stability-critical check: no
// matter what the playlist contains (empty, partial, full, or with extra
// unrelated entries), the lineup's channel count, order, numbers, and names
// must never change.
func TestRebuild_LineupShapeNeverChanges(t *testing.T) {
	l, err := New(sampleConfig())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	want := l.Channels()
	wantCopy := make([]Channel, len(want))
	copy(wantCopy, want)

	scenarios := [][]playlist.Entry{
		nil,
		{{Name: "ESPN HD", TvgID: "ESPN.us", URL: "http://example.com/espn"}},
		{
			{Name: "ESPN HD", TvgID: "ESPN.us", URL: "http://example.com/espn"},
			{Name: "CNN", TvgID: "CNN.us", URL: "http://example.com/cnn"},
			{Name: "Local News 5 HD", URL: "http://example.com/local5"},
			{Name: "Unrelated Channel", TvgID: "OTHER.us", URL: "http://example.com/other"},
		},
		{{Name: "Completely Different Lineup", TvgID: "XYZ.us", URL: "http://example.com/xyz"}},
	}

	for i, entries := range scenarios {
		l.Rebuild(entries)

		got := l.Channels()
		if len(got) != len(wantCopy) {
			t.Fatalf("scenario %d: len(Channels()) = %d, want %d", i, len(got), len(wantCopy))
		}
		for j := range got {
			if got[j] != wantCopy[j] {
				t.Errorf("scenario %d: Channels()[%d] = %+v, want %+v", i, j, got[j], wantCopy[j])
			}
		}

		snap := l.Current()
		if len(snap.Channels) != len(wantCopy) {
			t.Errorf("scenario %d: snap.Channels len = %d, want %d", i, len(snap.Channels), len(wantCopy))
		}
	}
}
