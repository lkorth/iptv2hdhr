package playlist

// Entry is one channel/stream entry parsed from an M3U playlist.
type Entry struct {
	Name       string // display name (from #EXTINF trailing name, or tvg-name fallback)
	TvgID      string // tvg-id attribute, as-is (matching is case-insensitive)
	TvgName    string // tvg-name attribute
	TvgLogo    string // tvg-logo attribute
	GroupTitle string // group-title attribute
	URL        string // stream URL

	// Headers are extra HTTP request headers to send when fetching URL (and,
	// for HLS, its segments), keyed by canonical header name. Populated
	// from #EXTVLCOPT:http-referrer / http-user-agent lines preceding the
	// URL and from a Kodi-style "url|Referer=...&User-Agent=..." suffix.
	// Nil when the entry has none.
	Headers map[string]string
}
