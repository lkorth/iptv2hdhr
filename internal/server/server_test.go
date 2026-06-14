package server

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"

	"iptv2hdhr/internal/config"
	"iptv2hdhr/internal/guide"
	"iptv2hdhr/internal/guide/xmltv"
	"iptv2hdhr/internal/hdhr"
	"iptv2hdhr/internal/lineup"
	"iptv2hdhr/internal/playlist"
)

func newTestServer(t *testing.T) *Server {
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
		{Number: "1004", Name: "Reserved Slot"},
	})
	if err != nil {
		t.Fatalf("lineup.New: %v", err)
	}
	lin.Rebuild([]playlist.Entry{
		{Name: "ESPN HD", TvgID: "ESPN.us", URL: "http://example.com/espn"},
	})

	gf := guide.NewFetcher(nil)

	return New(context.Background(), cfg, lin, gf)
}

func TestServer_Discover(t *testing.T) {
	srv := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/discover.json", nil)
	r.Host = "192.168.1.50:8080"
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp hdhr.Discover
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.DeviceID != "ABCD1234" {
		t.Errorf("DeviceID = %q, want %q", resp.DeviceID, "ABCD1234")
	}
}

func TestServer_LineupJSON(t *testing.T) {
	srv := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/lineup.json", nil)
	r.Host = "192.168.1.50:8080"
	srv.ServeHTTP(w, r)

	var entries []hdhr.LineupEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, w.Body.String())
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
}

func TestServer_LineupStatus(t *testing.T) {
	srv := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/lineup_status.json", nil)
	srv.ServeHTTP(w, r)

	var status hdhr.LineupStatus
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if status.ScanInProgress != 0 {
		t.Errorf("ScanInProgress = %d, want 0", status.ScanInProgress)
	}
}

func TestServer_LineupPost(t *testing.T) {
	srv := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/lineup.post?scan=start", nil)
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestServer_DeviceXML(t *testing.T) {
	srv := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/device.xml", nil)
	r.Host = "192.168.1.50:8080"
	srv.ServeHTTP(w, r)

	var c hdhr.Capability
	if err := xml.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("xml.Unmarshal: %v\nbody: %s", err, w.Body.String())
	}
	if c.Device.UDN != "uuid:ABCD1234" {
		t.Errorf("Device.UDN = %q, want %q", c.Device.UDN, "uuid:ABCD1234")
	}
}

func TestServer_GuideXML(t *testing.T) {
	srv := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/guide.xml", nil)
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/xml" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/xml")
	}

	doc, err := xmltv.Parse(w.Body.Bytes())
	if err != nil {
		t.Fatalf("xmltv.Parse: %v\nbody: %s", err, w.Body.String())
	}
	if len(doc.Channels) != 2 {
		t.Fatalf("len(doc.Channels) = %d, want 2", len(doc.Channels))
	}
	if doc.Channels[0].ID != "slot-1001" {
		t.Errorf("doc.Channels[0].ID = %q, want %q", doc.Channels[0].ID, "slot-1001")
	}
}

func TestServer_StreamUnknownChannel404(t *testing.T) {
	srv := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream/9999", nil)
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestServer_StreamConfiguredUnmatched503(t *testing.T) {
	srv := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream/1004", nil)
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}
