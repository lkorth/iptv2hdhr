package hdhr

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"iptv2hdhr/internal/config"
	"iptv2hdhr/internal/lineup"
)

func testDeps(t *testing.T) *Deps {
	t.Helper()

	cfg := &config.Config{
		Device: config.DeviceConfig{
			FriendlyName: "IPTV Tuner",
			Manufacturer: "Silicondust",
			ModelNumber:  "HDTC-2US",
			DeviceID:     "ABCD1234",
			TunerCount:   4,
			HTTPPort:     8080,
		},
	}

	lin, err := lineup.New([]config.Channel{
		{Number: "1001", Name: "ESPN HD", TvgID: "ESPN.us"},
		{Number: "1002", Name: "CNN", TvgID: "CNN.us"},
	})
	if err != nil {
		t.Fatalf("lineup.New: %v", err)
	}

	return &Deps{
		Cfg:    cfg,
		Lineup: lin,
	}
}

func newRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, target, nil)
	r.Host = "192.168.1.50:8080"
	return r
}

func TestDiscoverHandler(t *testing.T) {
	d := testDeps(t)
	w := httptest.NewRecorder()
	d.DiscoverHandler(w, newRequest(t, "GET", "/discover.json"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp Discover
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, w.Body.String())
	}

	if resp.DeviceID != "ABCD1234" {
		t.Errorf("DeviceID = %q, want %q", resp.DeviceID, "ABCD1234")
	}
	if resp.BaseURL != "http://192.168.1.50:8080" {
		t.Errorf("BaseURL = %q, want %q", resp.BaseURL, "http://192.168.1.50:8080")
	}
	if resp.LineupURL != "http://192.168.1.50:8080/lineup.json" {
		t.Errorf("LineupURL = %q, want %q", resp.LineupURL, "http://192.168.1.50:8080/lineup.json")
	}
	if resp.TunerCount != 4 {
		t.Errorf("TunerCount = %d, want 4", resp.TunerCount)
	}
	if resp.ModelNumber != "HDTC-2US" {
		t.Errorf("ModelNumber = %q, want %q", resp.ModelNumber, "HDTC-2US")
	}
}

func TestDiscoverHandler_UsesConfiguredBaseURL(t *testing.T) {
	d := testDeps(t)
	d.Cfg.Device.BaseURL = "http://tuner.example.com"

	w := httptest.NewRecorder()
	d.DiscoverHandler(w, newRequest(t, "GET", "/discover.json"))

	var resp Discover
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.BaseURL != "http://tuner.example.com" {
		t.Errorf("BaseURL = %q, want %q", resp.BaseURL, "http://tuner.example.com")
	}
	if resp.LineupURL != "http://tuner.example.com/lineup.json" {
		t.Errorf("LineupURL = %q, want %q", resp.LineupURL, "http://tuner.example.com/lineup.json")
	}
}

func TestLineupStatusHandler(t *testing.T) {
	d := testDeps(t)
	w := httptest.NewRecorder()
	d.LineupStatusHandler(w, newRequest(t, "GET", "/lineup_status.json"))

	var resp LineupStatus
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ScanInProgress != 0 {
		t.Errorf("ScanInProgress = %d, want 0", resp.ScanInProgress)
	}
	if resp.ScanPossible != 1 {
		t.Errorf("ScanPossible = %d, want 1", resp.ScanPossible)
	}
}

func TestLineupHandler(t *testing.T) {
	d := testDeps(t)
	w := httptest.NewRecorder()
	d.LineupHandler(w, newRequest(t, "GET", "/lineup.json"))

	var entries []LineupEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, w.Body.String())
	}

	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].GuideNumber != "1001" || entries[0].GuideName != "ESPN HD" {
		t.Errorf("entries[0] = %+v, want GuideNumber 1001, GuideName ESPN HD", entries[0])
	}
	if entries[0].URL != "http://192.168.1.50:8080/stream/1001" {
		t.Errorf("entries[0].URL = %q, want %q", entries[0].URL, "http://192.168.1.50:8080/stream/1001")
	}
}

func TestLineupHandler_ShapeStableRegardlessOfMatches(t *testing.T) {
	d := testDeps(t)

	// Rebuild with no matching entries at all.
	d.Lineup.Rebuild(nil)

	w := httptest.NewRecorder()
	d.LineupHandler(w, newRequest(t, "GET", "/lineup.json"))

	var entries []LineupEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2 even with no playlist matches", len(entries))
	}
}

func TestLineupPostHandler(t *testing.T) {
	d := testDeps(t)
	w := httptest.NewRecorder()
	d.LineupPostHandler(w, newRequest(t, "POST", "/lineup.post?scan=start"))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestDeviceXMLHandler(t *testing.T) {
	d := testDeps(t)
	w := httptest.NewRecorder()
	d.DeviceXMLHandler(w, newRequest(t, "GET", "/device.xml"))

	if ct := w.Header().Get("Content-Type"); ct != "application/xml" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/xml")
	}
	if !strings.HasPrefix(w.Body.String(), xml.Header) {
		t.Errorf("body does not start with xml.Header: %s", w.Body.String())
	}

	var c Capability
	if err := xml.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("xml.Unmarshal: %v\nbody: %s", err, w.Body.String())
	}
	if c.Device.UDN != "uuid:ABCD1234" {
		t.Errorf("Device.UDN = %q, want %q", c.Device.UDN, "uuid:ABCD1234")
	}
	if c.URLBase != "http://192.168.1.50:8080" {
		t.Errorf("URLBase = %q, want %q", c.URLBase, "http://192.168.1.50:8080")
	}
}
