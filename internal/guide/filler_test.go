package guide

import (
	"testing"
	"time"

	"iptv2hdhr/internal/guide/xmltv"
)

var testNow = time.Date(2024, 6, 13, 12, 0, 0, 0, time.UTC)

// assertContiguous checks that programmes is non-empty, starts at
// windowStart, ends at windowEnd, and each programme's start equals the
// previous programme's stop (no gaps or overlaps).
func assertContiguous(t *testing.T, programmes []xmltv.Programme, windowStart, windowEnd time.Time) {
	t.Helper()

	if len(programmes) == 0 {
		t.Fatal("programmes is empty")
	}

	first, err := xmltv.ParseTime(programmes[0].Start)
	if err != nil {
		t.Fatalf("ParseTime(%q): %v", programmes[0].Start, err)
	}
	if !first.Equal(windowStart) {
		t.Errorf("first programme start = %v, want %v", first, windowStart)
	}

	cursor := first
	for i, p := range programmes {
		start, err := xmltv.ParseTime(p.Start)
		if err != nil {
			t.Fatalf("programme %d: ParseTime(start) error: %v", i, err)
		}
		stop, err := xmltv.ParseTime(p.Stop)
		if err != nil {
			t.Fatalf("programme %d: ParseTime(stop) error: %v", i, err)
		}
		if !start.Equal(cursor) {
			t.Errorf("programme %d: start = %v, want %v (contiguous with previous stop)", i, start, cursor)
		}
		if !stop.After(start) {
			t.Errorf("programme %d: stop %v not after start %v", i, stop, start)
		}
		cursor = stop
	}

	if !cursor.Equal(windowEnd) {
		t.Errorf("last programme stop = %v, want %v", cursor, windowEnd)
	}
}

func TestBuildChannelProgrammes_NoRealProgrammes(t *testing.T) {
	idx := emptyIndex()

	out := BuildChannelProgrammes(idx, "ESPN.us", "slot-1001", "ESPN HD", testNow)

	windowStart := testNow.Add(-GuideLookback)
	windowEnd := testNow.Add(GuideLookahead)
	assertContiguous(t, out, windowStart, windowEnd)

	for i, p := range out {
		if p.Channel != "slot-1001" {
			t.Errorf("programme %d: Channel = %q, want %q", i, p.Channel, "slot-1001")
		}
		if len(p.Title) != 1 || p.Title[0].Value != FillerTitle {
			t.Errorf("programme %d: Title = %+v, want [{Value: %q}]", i, p.Title, FillerTitle)
		}

		start, _ := xmltv.ParseTime(p.Start)
		stop, _ := xmltv.ParseTime(p.Stop)
		if d := stop.Sub(start); d > MaxFillerBlockDuration {
			t.Errorf("programme %d: duration = %v, want <= %v", i, d, MaxFillerBlockDuration)
		}
	}

	// 25h window / 6h max block = 5 blocks (6,6,6,6,1).
	if len(out) != 5 {
		t.Errorf("len(out) = %d, want 5", len(out))
	}
}

func TestBuildChannelProgrammes_RealProgrammeCoversWindow(t *testing.T) {
	idx := emptyIndex()
	idx.ProgrammesByChannelID["ESPN.us"] = []xmltv.Programme{
		{
			Channel: "ESPN.us",
			Start:   xmltv.FormatTime(testNow.Add(-10 * time.Hour)),
			Stop:    xmltv.FormatTime(testNow.Add(48 * time.Hour)),
			Title:   []xmltv.Title{{Value: "All Day Marathon"}},
		},
	}

	out := BuildChannelProgrammes(idx, "ESPN.us", "slot-1001", "ESPN HD", testNow)

	windowStart := testNow.Add(-GuideLookback)
	windowEnd := testNow.Add(GuideLookahead)
	assertContiguous(t, out, windowStart, windowEnd)

	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1 (no filler when real programme covers window): %+v", len(out), out)
	}
	if out[0].Channel != "slot-1001" {
		t.Errorf("out[0].Channel = %q, want %q", out[0].Channel, "slot-1001")
	}
	if len(out[0].Title) != 1 || out[0].Title[0].Value != "All Day Marathon" {
		t.Errorf("out[0].Title = %+v, want [{Value: All Day Marathon}]", out[0].Title)
	}
}

func TestBuildChannelProgrammes_GapBetweenProgrammes(t *testing.T) {
	idx := emptyIndex()
	idx.ProgrammesByChannelID["ESPN.us"] = []xmltv.Programme{
		{
			Channel: "ESPN.us",
			Start:   xmltv.FormatTime(testNow),
			Stop:    xmltv.FormatTime(testNow.Add(1 * time.Hour)),
			Title:   []xmltv.Title{{Value: "SportsCenter"}},
		},
	}

	out := BuildChannelProgrammes(idx, "ESPN.us", "slot-1001", "ESPN HD", testNow)

	windowStart := testNow.Add(-GuideLookback)
	windowEnd := testNow.Add(GuideLookahead)
	assertContiguous(t, out, windowStart, windowEnd)

	// Find the real programme and confirm filler surrounds it.
	var realIdx = -1
	for i, p := range out {
		if len(p.Title) == 1 && p.Title[0].Value == "SportsCenter" {
			realIdx = i
		}
	}
	if realIdx <= 0 || realIdx >= len(out)-1 {
		t.Fatalf("real programme at index %d, want it surrounded by filler in %d entries", realIdx, len(out))
	}
	if out[realIdx-1].Title[0].Value != FillerTitle {
		t.Errorf("entry before real programme = %+v, want filler", out[realIdx-1])
	}
	if out[realIdx+1].Title[0].Value != FillerTitle {
		t.Errorf("entry after real programme = %+v, want filler", out[realIdx+1])
	}
}

func TestBuildChannelProgrammes_NoTvgID(t *testing.T) {
	idx := emptyIndex()
	idx.ProgrammesByChannelID["ESPN.us"] = []xmltv.Programme{
		{
			Channel: "ESPN.us",
			Start:   xmltv.FormatTime(testNow),
			Stop:    xmltv.FormatTime(testNow.Add(1 * time.Hour)),
			Title:   []xmltv.Title{{Value: "SportsCenter"}},
		},
	}

	// No tvg-id configured for this channel: entire window is filler, even
	// though the index has data under a different channel id.
	out := BuildChannelProgrammes(idx, "", "slot-1004", "Reserved Slot", testNow)

	windowStart := testNow.Add(-GuideLookback)
	windowEnd := testNow.Add(GuideLookahead)
	assertContiguous(t, out, windowStart, windowEnd)

	for i, p := range out {
		if len(p.Title) != 1 || p.Title[0].Value != FillerTitle {
			t.Errorf("programme %d: Title = %+v, want filler", i, p.Title)
		}
	}
}

func TestBuildChannelProgrammes_ProgrammeOutsideWindowIgnored(t *testing.T) {
	idx := emptyIndex()
	idx.ProgrammesByChannelID["ESPN.us"] = []xmltv.Programme{
		{
			Channel: "ESPN.us",
			Start:   xmltv.FormatTime(testNow.Add(-48 * time.Hour)),
			Stop:    xmltv.FormatTime(testNow.Add(-47 * time.Hour)),
			Title:   []xmltv.Title{{Value: "Yesterday's Show"}},
		},
	}

	out := BuildChannelProgrammes(idx, "ESPN.us", "slot-1001", "ESPN HD", testNow)

	windowStart := testNow.Add(-GuideLookback)
	windowEnd := testNow.Add(GuideLookahead)
	assertContiguous(t, out, windowStart, windowEnd)

	for i, p := range out {
		if len(p.Title) == 1 && p.Title[0].Value == "Yesterday's Show" {
			t.Errorf("programme %d: out-of-window programme should have been dropped", i)
		}
	}
}
