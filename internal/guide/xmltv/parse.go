package xmltv

import (
	"encoding/xml"
	"time"
)

// timeLayout is the XMLTV timestamp format, e.g. "20240613180000 +0000".
const timeLayout = "20060102150405 -0700"

// Parse decodes an XMLTV document.
func Parse(data []byte) (*Document, error) {
	var doc Document
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// ParseTime parses an XMLTV timestamp such as "20240613180000 +0000".
func ParseTime(s string) (time.Time, error) {
	return time.Parse(timeLayout, s)
}

// FormatTime formats t as an XMLTV timestamp in UTC.
func FormatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}
