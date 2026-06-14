// Package xmltv defines the subset of the XMLTV schema this project reads
// and writes, plus helpers for XMLTV's timestamp format.
package xmltv

import "encoding/xml"

// Document is the root <tv> element of an XMLTV document.
type Document struct {
	XMLName    xml.Name    `xml:"tv"`
	Channels   []Channel   `xml:"channel"`
	Programmes []Programme `xml:"programme"`
}

// Channel is a <channel> element.
type Channel struct {
	ID          string   `xml:"id,attr"`
	DisplayName []string `xml:"display-name"`
	Icon        *Icon    `xml:"icon"`
}

// Icon is an <icon src="..."/> element.
type Icon struct {
	Src string `xml:"src,attr"`
}

// Programme is a <programme> element. Start and Stop are in XMLTV's
// timestamp format ("20060102150405 -0700"); use ParseTime to convert.
type Programme struct {
	Channel  string     `xml:"channel,attr"`
	Start    string     `xml:"start,attr"`
	Stop     string     `xml:"stop,attr"`
	Title    []Title    `xml:"title"`
	SubTitle []SubTitle `xml:"sub-title,omitempty"`
	Desc     []Desc     `xml:"desc"`
	Category []Category `xml:"category,omitempty"`
	Icon     *Icon      `xml:"icon,omitempty"`
}

// Title is a <title> element, optionally tagged with a language.
type Title struct {
	Lang  string `xml:"lang,attr,omitempty"`
	Value string `xml:",chardata"`
}

// SubTitle is a <sub-title> element, optionally tagged with a language.
type SubTitle struct {
	Lang  string `xml:"lang,attr,omitempty"`
	Value string `xml:",chardata"`
}

// Desc is a <desc> element, optionally tagged with a language.
type Desc struct {
	Lang  string `xml:"lang,attr,omitempty"`
	Value string `xml:",chardata"`
}

// Category is a <category> element, optionally tagged with a language.
type Category struct {
	Lang  string `xml:"lang,attr,omitempty"`
	Value string `xml:",chardata"`
}
