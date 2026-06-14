package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTempConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return path
}

const validConfig = `
device:
  friendly_name: "Test Tuner"
  device_id: "ABCD1234"
  http_port: 9090
  tuner_count: 2

playlists:
  - name: "main"
    url: "http://example.com/playlist.m3u"
    refresh_interval: "15m"

guides:
  - name: "main-epg"
    url: "http://example.com/epg.xml"
    refresh_interval: "2h"

channels:
  - number: "1001"
    name: "ESPN"
    tvg_id: "ESPN.us"
  - number: "1002"
    name: "CNN"
    tvg_id: "CNN.us"
    name_match: "CNN"
`

func TestLoad_ValidConfig(t *testing.T) {
	path := writeTempConfig(t, validConfig)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Device.FriendlyName != "Test Tuner" {
		t.Errorf("FriendlyName = %q, want %q", cfg.Device.FriendlyName, "Test Tuner")
	}
	if cfg.Device.HTTPPort != 9090 {
		t.Errorf("HTTPPort = %d, want 9090", cfg.Device.HTTPPort)
	}
	if cfg.Device.ModelNumber != DefaultModelNumber {
		t.Errorf("ModelNumber = %q, want default %q", cfg.Device.ModelNumber, DefaultModelNumber)
	}

	if len(cfg.Playlists) != 1 {
		t.Fatalf("len(Playlists) = %d, want 1", len(cfg.Playlists))
	}
	if got := cfg.Playlists[0].RefreshInterval.Duration(); got != 15*time.Minute {
		t.Errorf("Playlists[0].RefreshInterval = %v, want 15m", got)
	}

	if len(cfg.Channels) != 2 {
		t.Fatalf("len(Channels) = %d, want 2", len(cfg.Channels))
	}
}

func TestLoad_AppliesDefaults(t *testing.T) {
	const minimal = `
device:
  device_id: "ABCD1234"

playlists:
  - url: "http://example.com/playlist.m3u"

channels:
  - number: "1"
    name: "Channel One"
    tvg_id: "ch1"
`
	path := writeTempConfig(t, minimal)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Device.FriendlyName != DefaultFriendlyName {
		t.Errorf("FriendlyName = %q, want default %q", cfg.Device.FriendlyName, DefaultFriendlyName)
	}
	if cfg.Device.HTTPPort != DefaultHTTPPort {
		t.Errorf("HTTPPort = %d, want default %d", cfg.Device.HTTPPort, DefaultHTTPPort)
	}
	if cfg.Device.TunerCount != DefaultTunerCount {
		t.Errorf("TunerCount = %d, want default %d", cfg.Device.TunerCount, DefaultTunerCount)
	}
	if cfg.Playlists[0].Name != "playlist-1" {
		t.Errorf("Playlists[0].Name = %q, want %q", cfg.Playlists[0].Name, "playlist-1")
	}
	if got := cfg.Playlists[0].RefreshInterval.Duration(); got != DefaultRefreshInterval {
		t.Errorf("Playlists[0].RefreshInterval = %v, want default %v", got, DefaultRefreshInterval)
	}
}

func TestLoad_DuplicateChannelNumbers(t *testing.T) {
	const dup = `
playlists:
  - url: "http://example.com/playlist.m3u"

channels:
  - number: "1001"
    name: "ESPN"
    tvg_id: "ESPN.us"
  - number: "1001"
    name: "ESPN HD"
    tvg_id: "ESPNHD.us"
`
	path := writeTempConfig(t, dup)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load returned no error, want duplicate channel number error")
	}
	if !strings.Contains(err.Error(), "duplicate channel number") {
		t.Errorf("error = %v, want it to mention 'duplicate channel number'", err)
	}
}

func TestLoad_MissingRequiredFields(t *testing.T) {
	const missing = `
playlists: []
channels: []
`
	path := writeTempConfig(t, missing)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load returned no error, want validation error for empty playlists/channels")
	}
	if !strings.Contains(err.Error(), "at least one playlist") {
		t.Errorf("error = %v, want it to mention missing playlist", err)
	}
	if !strings.Contains(err.Error(), "at least one channel") {
		t.Errorf("error = %v, want it to mention missing channel", err)
	}
}

func TestLoad_RefreshIntervalTooShort(t *testing.T) {
	const tooShort = `
device:
  device_id: "ABCD1234"

playlists:
  - url: "http://example.com/playlist.m3u"
    refresh_interval: "10s"

channels:
  - number: "1"
    name: "Channel One"
    tvg_id: "ch1"
`
	path := writeTempConfig(t, tooShort)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load returned no error, want validation error for too-short refresh_interval")
	}
	if !strings.Contains(err.Error(), "refresh_interval must be >=") {
		t.Errorf("error = %v, want it to mention refresh_interval", err)
	}
}

func TestLoad_MissingDeviceID(t *testing.T) {
	const missing = `
playlists:
  - url: "http://example.com/playlist.m3u"

channels:
  - number: "1"
    name: "Channel One"
    tvg_id: "ch1"
`
	path := writeTempConfig(t, missing)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load returned no error, want validation error for missing device_id")
	}
	if !strings.Contains(err.Error(), "device.device_id is required") {
		t.Errorf("error = %v, want it to mention device.device_id", err)
	}
}

func TestLoad_InvalidDeviceIDFormat(t *testing.T) {
	const invalid = `
device:
  device_id: "not-hex"

playlists:
  - url: "http://example.com/playlist.m3u"

channels:
  - number: "1"
    name: "Channel One"
    tvg_id: "ch1"
`
	path := writeTempConfig(t, invalid)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load returned no error, want validation error for invalid device_id format")
	}
	if !strings.Contains(err.Error(), "device.device_id must be 8 hex characters") {
		t.Errorf("error = %v, want it to mention device_id format", err)
	}
}


