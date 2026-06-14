// Package hdhr implements the HDHomeRun HTTP discovery and lineup endpoints
// that Plex (and other HDHomeRun clients) use to discover and tune this
// server as a virtual tuner device.
package hdhr

import "encoding/xml"

// Discover is the response for /discover.json.
type Discover struct {
	FriendlyName    string `json:"FriendlyName"`
	ModelNumber     string `json:"ModelNumber"`
	FirmwareName    string `json:"FirmwareName"`
	FirmwareVersion string `json:"FirmwareVersion"`
	DeviceID        string `json:"DeviceID"`
	DeviceAuth      string `json:"DeviceAuth"`
	BaseURL         string `json:"BaseURL"`
	LineupURL       string `json:"LineupURL"`
	TunerCount      int    `json:"TunerCount"`
	Manufacturer    string `json:"Manufacturer,omitempty"`
}

// LineupStatus is the response for /lineup_status.json.
type LineupStatus struct {
	ScanInProgress int      `json:"ScanInProgress"`
	ScanPossible   int      `json:"ScanPossible"`
	Source         string   `json:"Source"`
	SourceList     []string `json:"SourceList"`
}

// LineupEntry is one entry in the /lineup.json array.
type LineupEntry struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
	Tags        string `json:"Tags,omitempty"`
}

// Capability is the UPnP device description served at /device.xml.
type Capability struct {
	XMLName     xml.Name `xml:"root"`
	Xmlns       string   `xml:"xmlns,attr"`
	SpecVersion struct {
		Major int `xml:"major"`
		Minor int `xml:"minor"`
	} `xml:"specVersion"`
	URLBase string `xml:"URLBase"`
	Device  struct {
		DeviceType   string `xml:"deviceType"`
		FriendlyName string `xml:"friendlyName"`
		Manufacturer string `xml:"manufacturer"`
		ModelName    string `xml:"modelName"`
		ModelNumber  string `xml:"modelNumber"`
		UDN          string `xml:"UDN"`
	} `xml:"device"`
}
