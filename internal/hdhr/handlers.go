package hdhr

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"

	"iptv2hdhr/internal/config"
	"iptv2hdhr/internal/lineup"
)

// Deps holds everything the HDHomeRun handlers need.
type Deps struct {
	Cfg    *config.Config
	Lineup *lineup.Lineup
}

// baseURL returns the externally-visible base URL for this server, used to
// build absolute links in discovery responses.
func baseURL(cfg *config.Config, r *http.Request) string {
	if cfg.Device.BaseURL != "" {
		return cfg.Device.BaseURL
	}
	return "http://" + r.Host
}

// DiscoverHandler serves /discover.json.
func (d *Deps) DiscoverHandler(w http.ResponseWriter, r *http.Request) {
	base := baseURL(d.Cfg, r)
	resp := Discover{
		FriendlyName:    d.Cfg.Device.FriendlyName,
		ModelNumber:     d.Cfg.Device.ModelNumber,
		FirmwareName:    "bin_1.0",
		FirmwareVersion: "1.0",
		DeviceID:        d.Cfg.Device.DeviceID,
		DeviceAuth:      d.Cfg.Device.FriendlyName,
		BaseURL:         base,
		LineupURL:       base + "/lineup.json",
		TunerCount:      d.Cfg.Device.TunerCount,
		Manufacturer:    d.Cfg.Device.Manufacturer,
	}
	writeJSON(w, resp)
}

// LineupStatusHandler serves /lineup_status.json. Scanning is never
// "in progress" since the lineup is static and always ready.
func (d *Deps) LineupStatusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, LineupStatus{
		ScanInProgress: 0,
		ScanPossible:   1,
		Source:         "Cable",
		SourceList:     []string{"Cable"},
	})
}

// LineupHandler serves /lineup.json. The returned entries always have the
// same length, order, GuideNumber, and GuideName as the configured channel
// list, regardless of current playlist match state.
func (d *Deps) LineupHandler(w http.ResponseWriter, r *http.Request) {
	base := baseURL(d.Cfg, r)
	channels := d.Lineup.Channels()

	out := make([]LineupEntry, 0, len(channels))
	for _, ch := range channels {
		out = append(out, LineupEntry{
			GuideNumber: ch.Number,
			GuideName:   ch.Name,
			URL:         fmt.Sprintf("%s/stream/%s", base, ch.Number),
		})
	}
	writeJSON(w, out)
}

// LineupPostHandler serves /lineup.post. Scan requests are accepted as
// no-ops since the lineup is static and always up to date.
func (d *Deps) LineupPostHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// DeviceXMLHandler serves /device.xml, the UPnP device description.
func (d *Deps) DeviceXMLHandler(w http.ResponseWriter, r *http.Request) {
	base := baseURL(d.Cfg, r)

	var c Capability
	c.Xmlns = "urn:schemas-upnp-org:device-1-0"
	c.URLBase = base
	c.SpecVersion.Major = 1
	c.SpecVersion.Minor = 0
	c.Device.DeviceType = "urn:schemas-upnp-org:device:MediaServer:1"
	c.Device.FriendlyName = d.Cfg.Device.FriendlyName
	c.Device.Manufacturer = d.Cfg.Device.Manufacturer
	c.Device.ModelName = d.Cfg.Device.ModelNumber
	c.Device.ModelNumber = d.Cfg.Device.ModelNumber
	c.Device.UDN = "uuid:" + d.Cfg.Device.DeviceID

	writeXML(w, c)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(b)
}

func writeXML(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/xml")
	b, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write([]byte(xml.Header))
	w.Write(b)
}
