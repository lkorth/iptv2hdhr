package playlist

import (
	"bufio"
	"bytes"
	"errors"
	"regexp"
	"strings"
)

// attrRegex matches key="value" attribute pairs in an #EXTINF line.
var attrRegex = regexp.MustCompile(`[A-Za-z0-9_-]+="[^"]*"`)

// ErrNotExtendedM3U is returned when the input is missing the #EXTM3U header
// or looks like an HLS media playlist rather than a channel list.
var ErrNotExtendedM3U = errors.New("invalid input: an extended M3U playlist (#EXTM3U) is required")

// Parse reads an extended M3U byte stream and returns the parsed entries.
//
// It returns an error only for structurally invalid input (missing #EXTM3U
// header, or an HLS media playlist). Individual malformed entries (e.g. an
// #EXTINF line with no following URL, or an entry with no usable name) are
// skipped silently.
func Parse(data []byte) ([]Entry, error) {
	if bytes.Contains(data, []byte("#EXT-X-TARGETDURATION")) || bytes.Contains(data, []byte("#EXT-X-MEDIA-SEQUENCE")) {
		return nil, ErrNotExtendedM3U
	}
	if !bytes.Contains(data, []byte("#EXTM3U")) {
		return nil, ErrNotExtendedM3U
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var entries []Entry
	var pendingExtinf string
	haveExtinf := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#EXTINF") {
			// A new #EXTINF line discards any previous one that never got a
			// matching URL line.
			pendingExtinf = line
			haveExtinf = true
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		// Non-# line: this is a URL. Only usable if we have a pending
		// #EXTINF to pair it with.
		if haveExtinf {
			if entry, ok := parseEntry(pendingExtinf, line); ok {
				entries = append(entries, entry)
			}
			haveExtinf = false
			pendingExtinf = ""
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

// parseEntry builds an Entry from an #EXTINF line and its associated URL
// line. It returns ok=false if the entry has no usable name.
func parseEntry(extinfLine, url string) (Entry, bool) {
	attrs := make(map[string]string)

	remainder := extinfLine
	for _, m := range attrRegex.FindAllString(extinfLine, -1) {
		eq := strings.IndexByte(m, '=')
		key := strings.ToLower(m[:eq])
		val := strings.Trim(m[eq+1:], `"`)
		attrs[key] = val
		remainder = strings.Replace(remainder, m, "", 1)
	}

	name := ""
	if comma := strings.IndexByte(remainder, ','); comma >= 0 {
		name = strings.TrimSpace(remainder[comma+1:])
	}
	if name == "" {
		name = attrs["tvg-name"]
	}
	if name == "" {
		return Entry{}, false
	}

	return Entry{
		Name:       name,
		TvgID:      attrs["tvg-id"],
		TvgName:    attrs["tvg-name"],
		TvgLogo:    attrs["tvg-logo"],
		GroupTitle: attrs["group-title"],
		URL:        strings.TrimSpace(url),
	}, true
}
