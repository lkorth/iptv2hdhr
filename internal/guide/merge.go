package guide

import (
	"encoding/xml"
	"time"

	"iptv2hdhr/internal/guide/xmltv"
	"iptv2hdhr/internal/lineup"
)

// BuildGuideXML produces the full XMLTV document for /guide.xml: one
// <channel> per lineup slot (using the slot's stable channel id, name, and
// icon from config) plus BuildChannelProgrammes output for each. Channel
// icons are served through our own /logo/{number} endpoint (base + that
// path) rather than linking the upstream logo URL directly.
func BuildGuideXML(snap *lineup.Snapshot, idx *Index, now time.Time, base string) ([]byte, error) {
	doc := xmltv.Document{}

	for _, ch := range snap.Channels {
		xc := xmltv.Channel{
			ID:          ch.ChannelID,
			DisplayName: []string{ch.Name},
		}
		if ch.Logo != "" {
			xc.Icon = &xmltv.Icon{Src: base + "/logo/" + ch.Number}
		}
		doc.Channels = append(doc.Channels, xc)
		doc.Programmes = append(doc.Programmes, BuildChannelProgrammes(idx, ch.TvgID, ch.ChannelID, ch.Name, now)...)
	}

	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}

	return append([]byte(xml.Header), out...), nil
}
