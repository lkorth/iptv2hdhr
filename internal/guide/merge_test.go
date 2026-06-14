package guide

import (
	"testing"
	"time"

	"iptv2hdhr/internal/guide/xmltv"
	"iptv2hdhr/internal/lineup"
)

func TestBuildGuideXML(t *testing.T) {
	snap := &lineup.Snapshot{
		Channels: []lineup.Channel{
			{Number: "1001", Name: "ESPN HD", Logo: "http://example.com/espn.png", TvgID: "ESPN.us", ChannelID: "slot-1001"},
			{Number: "1004", Name: "Reserved Slot", ChannelID: "slot-1004"},
		},
	}

	idx := emptyIndex()
	idx.ProgrammesByChannelID["ESPN.us"] = []xmltv.Programme{
		{
			Channel: "ESPN.us",
			Start:   xmltv.FormatTime(testNow),
			Stop:    xmltv.FormatTime(testNow.Add(1 * time.Hour)),
			Title:   []xmltv.Title{{Value: "SportsCenter"}},
		},
	}

	data, err := BuildGuideXML(snap, idx, testNow, "http://192.168.1.50:8080")
	if err != nil {
		t.Fatalf("BuildGuideXML returned error: %v", err)
	}

	doc, err := xmltv.Parse(data)
	if err != nil {
		t.Fatalf("re-parsing generated guide XML: %v\n%s", err, data)
	}

	if len(doc.Channels) != 2 {
		t.Fatalf("len(doc.Channels) = %d, want 2: %+v", len(doc.Channels), doc.Channels)
	}
	if doc.Channels[0].ID != "slot-1001" {
		t.Errorf("doc.Channels[0].ID = %q, want %q", doc.Channels[0].ID, "slot-1001")
	}
	if doc.Channels[0].Icon == nil || doc.Channels[0].Icon.Src != "http://192.168.1.50:8080/logo/1001" {
		t.Errorf("doc.Channels[0].Icon = %+v, want src %q", doc.Channels[0].Icon, "http://192.168.1.50:8080/logo/1001")
	}
	if doc.Channels[1].ID != "slot-1004" {
		t.Errorf("doc.Channels[1].ID = %q, want %q", doc.Channels[1].ID, "slot-1004")
	}
	if doc.Channels[1].Icon != nil {
		t.Errorf("doc.Channels[1].Icon = %+v, want nil (no logo configured)", doc.Channels[1].Icon)
	}

	// Each channel covers the same 25h window; slot-1001 has one real
	// programme plus filler, slot-1004 is all filler.
	var count1001, count1004 int
	for _, p := range doc.Programmes {
		switch p.Channel {
		case "slot-1001":
			count1001++
		case "slot-1004":
			count1004++
		default:
			t.Errorf("unexpected programme channel %q", p.Channel)
		}
	}
	if count1001 == 0 {
		t.Error("no programmes for slot-1001")
	}
	if count1004 != 5 {
		t.Errorf("count1004 = %d, want 5 (all filler, 25h / 6h blocks)", count1004)
	}
}
