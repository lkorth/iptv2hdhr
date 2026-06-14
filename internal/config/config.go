package config

import (
	"crypto/rand"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration so it can be unmarshaled from YAML strings
// like "30m" or "1h" via time.ParseDuration.
type Duration time.Duration

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

// Config is the top-level configuration file schema.
type Config struct {
	Device    DeviceConfig     `yaml:"device"`
	Playlists []PlaylistSource `yaml:"playlists"`
	Guides    []GuideSource    `yaml:"guides"`
	Channels  []Channel        `yaml:"channels"`
}

type DeviceConfig struct {
	FriendlyName string `yaml:"friendly_name"`
	Manufacturer string `yaml:"manufacturer"`
	ModelNumber  string `yaml:"model_number"`
	// DeviceID is Plex's stable identity for this tuner (HDHomeRun
	// "DeviceID"/UDN). Set it once and never change it, or Plex will treat
	// the tuner as a new device.
	DeviceID   string `yaml:"device_id"`
	TunerCount int    `yaml:"tuner_count"`
	HTTPPort   int    `yaml:"http_port"`
	BaseURL    string `yaml:"base_url"`
}

// deviceIDPattern matches the 8-hex-character format real HDHomeRun devices
// use for DeviceID.
var deviceIDPattern = regexp.MustCompile(`^[0-9A-Fa-f]{8}$`)

// generateDeviceID produces an 8-character uppercase hex string, matching
// the format real HDHomeRun devices use for DeviceID. Used only to suggest a
// value when device.device_id is missing from the config.
func generateDeviceID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%02X%02X%02X%02X", b[0], b[1], b[2], b[3]), nil
}

type PlaylistSource struct {
	Name            string   `yaml:"name"`
	URL             string   `yaml:"url"`
	RefreshInterval Duration `yaml:"refresh_interval"`
}

type GuideSource struct {
	Name            string   `yaml:"name"`
	URL             string   `yaml:"url"`
	RefreshInterval Duration `yaml:"refresh_interval"`
}

// Channel is one slot in the static channel lineup.
type Channel struct {
	Number    string `yaml:"number"`
	Name      string `yaml:"name"`
	Logo      string `yaml:"logo"`
	TvgID     string `yaml:"tvg_id"`
	NameMatch string `yaml:"name_match"`
}

const (
	DefaultFriendlyName    = "IPTV Tuner"
	DefaultModelNumber     = "HDTC-2US"
	DefaultManufacturer    = "Silicondust"
	DefaultTunerCount      = 4
	DefaultHTTPPort        = 8080
	DefaultRefreshInterval = 30 * time.Minute
	MinRefreshInterval     = 1 * time.Minute
)

// Load reads and parses the YAML config file at path, applies defaults, and
// validates it.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Device.FriendlyName == "" {
		c.Device.FriendlyName = DefaultFriendlyName
	}
	if c.Device.Manufacturer == "" {
		c.Device.Manufacturer = DefaultManufacturer
	}
	if c.Device.ModelNumber == "" {
		c.Device.ModelNumber = DefaultModelNumber
	}
	if c.Device.TunerCount == 0 {
		c.Device.TunerCount = DefaultTunerCount
	}
	if c.Device.HTTPPort == 0 {
		c.Device.HTTPPort = DefaultHTTPPort
	}

	for i := range c.Playlists {
		if c.Playlists[i].Name == "" {
			c.Playlists[i].Name = fmt.Sprintf("playlist-%d", i+1)
		}
		if c.Playlists[i].RefreshInterval == 0 {
			c.Playlists[i].RefreshInterval = Duration(DefaultRefreshInterval)
		}
	}

	for i := range c.Guides {
		if c.Guides[i].Name == "" {
			c.Guides[i].Name = fmt.Sprintf("guide-%d", i+1)
		}
		if c.Guides[i].RefreshInterval == 0 {
			c.Guides[i].RefreshInterval = Duration(DefaultRefreshInterval)
		}
	}
}

// Validate checks the config for structural problems. It accumulates all
// errors found rather than failing fast, so users can fix multiple issues at
// once.
func (c *Config) Validate() error {
	var errs []string

	if c.Device.HTTPPort < 1 || c.Device.HTTPPort > 65535 {
		errs = append(errs, fmt.Sprintf("device.http_port must be between 1 and 65535, got %d", c.Device.HTTPPort))
	}
	if c.Device.TunerCount < 1 {
		errs = append(errs, fmt.Sprintf("device.tuner_count must be >= 1, got %d", c.Device.TunerCount))
	}
	if c.Device.DeviceID == "" {
		msg := "device.device_id is required and must never change once Plex has discovered this tuner"
		if suggestion, err := generateDeviceID(); err == nil {
			msg += fmt.Sprintf(" (e.g. %q)", suggestion)
		}
		errs = append(errs, msg)
	} else if !deviceIDPattern.MatchString(c.Device.DeviceID) {
		errs = append(errs, fmt.Sprintf("device.device_id must be 8 hex characters, got %q", c.Device.DeviceID))
	}

	if len(c.Playlists) == 0 {
		errs = append(errs, "at least one playlist must be configured under 'playlists'")
	}
	for i, p := range c.Playlists {
		if p.URL == "" {
			errs = append(errs, fmt.Sprintf("playlists[%d] (%s): url is required", i, p.Name))
		}
		if p.RefreshInterval.Duration() < MinRefreshInterval {
			errs = append(errs, fmt.Sprintf("playlists[%d] (%s): refresh_interval must be >= %s, got %s", i, p.Name, MinRefreshInterval, p.RefreshInterval.Duration()))
		}
	}

	for i, g := range c.Guides {
		if g.URL == "" {
			errs = append(errs, fmt.Sprintf("guides[%d] (%s): url is required", i, g.Name))
		}
		if g.RefreshInterval.Duration() < MinRefreshInterval {
			errs = append(errs, fmt.Sprintf("guides[%d] (%s): refresh_interval must be >= %s, got %s", i, g.Name, MinRefreshInterval, g.RefreshInterval.Duration()))
		}
	}

	if len(c.Channels) == 0 {
		errs = append(errs, "at least one channel must be configured under 'channels'")
	}
	seenNumbers := make(map[string]bool, len(c.Channels))
	for i, ch := range c.Channels {
		if ch.Number == "" {
			errs = append(errs, fmt.Sprintf("channels[%d]: number is required", i))
		} else if seenNumbers[ch.Number] {
			errs = append(errs, fmt.Sprintf("channels[%d]: duplicate channel number %q", i, ch.Number))
		} else {
			seenNumbers[ch.Number] = true
		}
		if ch.Name == "" {
			errs = append(errs, fmt.Sprintf("channels[%d] (number %s): name is required", i, ch.Number))
		}
		if ch.TvgID == "" && ch.NameMatch == "" {
			// Not fatal: a channel with no match rule simply always returns
			// 503 on /stream/, which is valid for a reserved slot.
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}
