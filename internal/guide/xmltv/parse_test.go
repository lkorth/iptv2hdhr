package xmltv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParse_SampleGuide(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "sample-guide.xml"))
	if err != nil {
		t.Fatalf("reading sample-guide.xml: %v", err)
	}

	doc, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if len(doc.Channels) != 2 {
		t.Fatalf("len(doc.Channels) = %d, want 2: %+v", len(doc.Channels), doc.Channels)
	}
	if doc.Channels[0].ID != "ESPN.us" {
		t.Errorf("doc.Channels[0].ID = %q, want %q", doc.Channels[0].ID, "ESPN.us")
	}
	if len(doc.Channels[0].DisplayName) != 1 || doc.Channels[0].DisplayName[0] != "ESPN" {
		t.Errorf("doc.Channels[0].DisplayName = %v, want [%q]", doc.Channels[0].DisplayName, "ESPN")
	}
	if doc.Channels[0].Icon == nil || doc.Channels[0].Icon.Src != "http://example.com/espn.png" {
		t.Errorf("doc.Channels[0].Icon = %+v, want src %q", doc.Channels[0].Icon, "http://example.com/espn.png")
	}
	if doc.Channels[1].Icon != nil {
		t.Errorf("doc.Channels[1].Icon = %+v, want nil", doc.Channels[1].Icon)
	}

	if len(doc.Programmes) != 3 {
		t.Fatalf("len(doc.Programmes) = %d, want 3: %+v", len(doc.Programmes), doc.Programmes)
	}

	p0 := doc.Programmes[0]
	if p0.Channel != "ESPN.us" {
		t.Errorf("Programmes[0].Channel = %q, want %q", p0.Channel, "ESPN.us")
	}
	if len(p0.Title) != 1 || p0.Title[0].Value != "SportsCenter" {
		t.Errorf("Programmes[0].Title = %+v, want [{Value: SportsCenter}]", p0.Title)
	}
	if len(p0.Category) != 1 || p0.Category[0].Value != "Sports" {
		t.Errorf("Programmes[0].Category = %+v, want [{Value: Sports}]", p0.Category)
	}

	p1 := doc.Programmes[1]
	if len(p1.SubTitle) != 1 || p1.SubTitle[0].Value != "Lakers at Celtics" {
		t.Errorf("Programmes[1].SubTitle = %+v, want [{Value: Lakers at Celtics}]", p1.SubTitle)
	}

	start, err := ParseTime(p0.Start)
	if err != nil {
		t.Fatalf("ParseTime(%q) error: %v", p0.Start, err)
	}
	want := time.Date(2024, 6, 13, 18, 0, 0, 0, time.UTC)
	if !start.Equal(want) {
		t.Errorf("ParseTime(%q) = %v, want %v", p0.Start, start, want)
	}
}

func TestFormatTime_RoundTrip(t *testing.T) {
	in := time.Date(2024, 6, 13, 18, 30, 0, 0, time.FixedZone("", -5*3600))
	formatted := FormatTime(in)

	out, err := ParseTime(formatted)
	if err != nil {
		t.Fatalf("ParseTime(%q) error: %v", formatted, err)
	}
	if !out.Equal(in) {
		t.Errorf("round trip: got %v, want %v", out, in)
	}
	if !strings.HasSuffix(formatted, "+0000") {
		t.Errorf("FormatTime(%v) = %q, want suffix %q", in, formatted, "+0000")
	}
}

func TestParse_EmptyDocument(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?><tv></tv>`)

	doc, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(doc.Channels) != 0 {
		t.Errorf("len(doc.Channels) = %d, want 0", len(doc.Channels))
	}
	if len(doc.Programmes) != 0 {
		t.Errorf("len(doc.Programmes) = %d, want 0", len(doc.Programmes))
	}
}

func TestParseTime_InvalidFormat(t *testing.T) {
	_, err := ParseTime("not-a-time")
	if err == nil {
		t.Error("ParseTime(\"not-a-time\") error = nil, want non-nil")
	}
}
