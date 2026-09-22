package playlist

import (
	"bufio"
	"bytes"
	"errors"
	"net/textproto"
	"net/url"
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
	var pendingHeaders map[string]string
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
			pendingHeaders = nil
			haveExtinf = true
			continue
		}

		if strings.HasPrefix(line, "#EXTVLCOPT:") {
			if name, val, ok := parseVLCOpt(strings.TrimPrefix(line, "#EXTVLCOPT:")); ok {
				if pendingHeaders == nil {
					pendingHeaders = make(map[string]string)
				}
				pendingHeaders[name] = val
			}
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		// Non-# line: this is a URL. Only usable if we have a pending
		// #EXTINF to pair it with.
		if haveExtinf {
			if entry, ok := parseEntry(pendingExtinf, line, pendingHeaders); ok {
				entries = append(entries, entry)
			}
			haveExtinf = false
			pendingExtinf = ""
			pendingHeaders = nil
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

// parseEntry builds an Entry from an #EXTINF line, its associated URL line,
// and any headers collected from #EXTVLCOPT lines in between. It returns
// ok=false if the entry has no usable name.
func parseEntry(extinfLine, urlLine string, headers map[string]string) (Entry, bool) {
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

	streamURL, headers := splitURLHeaders(strings.TrimSpace(urlLine), headers)

	return Entry{
		Name:       name,
		TvgID:      attrs["tvg-id"],
		TvgName:    attrs["tvg-name"],
		TvgLogo:    attrs["tvg-logo"],
		GroupTitle: attrs["group-title"],
		URL:        streamURL,
		Headers:    headers,
	}, true
}

// vlcOptHeaders maps the #EXTVLCOPT options we honour to the HTTP header
// they set. Other options (e.g. network-caching) are ignored.
var vlcOptHeaders = map[string]string{
	"http-referrer":   "Referer",
	"http-user-agent": "User-Agent",
}

// parseVLCOpt parses the "key=value" body of an #EXTVLCOPT line, returning
// the HTTP header it corresponds to and its value, or ok=false if the option
// isn't one that maps to a request header.
func parseVLCOpt(opt string) (header, value string, ok bool) {
	key, val, found := strings.Cut(opt, "=")
	if !found {
		return "", "", false
	}
	header, ok = vlcOptHeaders[strings.ToLower(strings.TrimSpace(key))]
	if !ok {
		return "", "", false
	}
	return header, strings.TrimSpace(val), true
}

// splitURLHeaders strips a Kodi/TiviMate-style "|Header=value&Header2=value"
// suffix from a stream URL, merging the headers it carries into headers
// (the suffix wins over #EXTVLCOPT for the same header). Values in the
// suffix may be percent-encoded.
func splitURLHeaders(streamURL string, headers map[string]string) (string, map[string]string) {
	base, suffix, found := strings.Cut(streamURL, "|")
	if !found {
		return streamURL, headers
	}
	for _, kv := range strings.Split(suffix, "&") {
		key, val, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		if decoded, err := url.QueryUnescape(val); err == nil {
			val = decoded
		}
		if headers == nil {
			headers = make(map[string]string)
		}
		headers[textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(key))] = strings.TrimSpace(val)
	}
	return base, headers
}
