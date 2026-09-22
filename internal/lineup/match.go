package lineup

import (
	"strings"

	"iptv2hdhr/internal/playlist"
)

// matchIndex indexes playlist entries for fast channel matching.
type matchIndex struct {
	byTvgID map[string]playlist.Entry // normalized tvg-id -> entry (first match wins on dupes)
	all     []playlist.Entry
}

// buildMatchIndex indexes playlist entries by normalized tvg-id for O(1)
// primary lookups, and keeps the full slice for name-substring fallback.
func buildMatchIndex(entries []playlist.Entry) matchIndex {
	idx := matchIndex{
		byTvgID: make(map[string]playlist.Entry, len(entries)),
		all:     entries,
	}
	for _, e := range entries {
		if e.TvgID == "" {
			continue
		}
		key := normalizeTvgID(e.TvgID)
		if _, exists := idx.byTvgID[key]; !exists {
			idx.byTvgID[key] = e
		}
	}
	return idx
}

// matchChannel finds the best current playlist entry for a config channel.
// Returns ok=false if nothing matches.
func matchChannel(ch Channel, idx matchIndex) (entry playlist.Entry, ok bool) {
	if ch.TvgID != "" {
		if e, found := idx.byTvgID[normalizeTvgID(ch.TvgID)]; found {
			return e, true
		}
	}
	if ch.NameMatch != "" {
		needle := strings.ToLower(ch.NameMatch)
		for _, e := range idx.all {
			if strings.Contains(strings.ToLower(e.Name), needle) ||
				strings.Contains(strings.ToLower(e.TvgName), needle) {
				return e, true
			}
		}
	}
	return playlist.Entry{}, false
}

// normalizeTvgID normalizes a tvg-id for case-insensitive comparison.
func normalizeTvgID(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
