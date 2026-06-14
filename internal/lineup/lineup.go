// Package lineup maintains the stable, config-defined channel lineup and a
// precomputed cache mapping each channel to its current best-match upstream
// stream URL.
package lineup

import (
	"fmt"
	"sync/atomic"

	"iptv2hdhr/internal/config"
	"iptv2hdhr/internal/playlist"
)

// Channel is one slot in the static channel lineup, built once at startup
// from config and never changed for the lifetime of the process.
type Channel struct {
	Number    string // GuideNumber, from config, stable
	Name      string // GuideName, from config, stable
	Logo      string
	TvgID     string
	NameMatch string
	ChannelID string // "slot-"+Number, stable XMLTV channel id
}

// Snapshot is the immutable, currently-active lineup and match cache.
// Replaced wholesale on each playlist refresh via atomic.Pointer.
type Snapshot struct {
	Channels  []Channel         // static, from config, in config order — never changes shape
	StreamURL map[string]string // Number -> matched upstream URL (only present if a match was found)
}

// Lineup holds the immutable channel list and the latest match snapshot.
type Lineup struct {
	channels    []Channel
	numberIndex map[string]int // Number -> index into channels, for O(1) lookup

	current atomic.Pointer[Snapshot]
}

// New builds a Lineup from the configured channels, validating that channel
// numbers are unique.
func New(cfg []config.Channel) (*Lineup, error) {
	channels := make([]Channel, 0, len(cfg))
	numberIndex := make(map[string]int, len(cfg))

	for _, ch := range cfg {
		if _, exists := numberIndex[ch.Number]; exists {
			return nil, fmt.Errorf("duplicate channel number %q", ch.Number)
		}
		numberIndex[ch.Number] = len(channels)
		channels = append(channels, Channel{
			Number:    ch.Number,
			Name:      ch.Name,
			Logo:      ch.Logo,
			TvgID:     ch.TvgID,
			NameMatch: ch.NameMatch,
			ChannelID: "slot-" + ch.Number,
		})
	}

	l := &Lineup{
		channels:    channels,
		numberIndex: numberIndex,
	}
	l.current.Store(&Snapshot{
		Channels:  channels,
		StreamURL: make(map[string]string),
	})
	return l, nil
}

// Channels returns the static channel list, for /lineup.json. Always the
// same length and order, regardless of playlist contents.
func (l *Lineup) Channels() []Channel {
	return l.channels
}

// Current returns the latest match snapshot. Safe for concurrent use; never
// blocks.
func (l *Lineup) Current() *Snapshot {
	return l.current.Load()
}

// StreamURL looks up the current matched upstream URL for a configured
// channel number. ok is false if the number isn't configured at all, or if
// it's configured but currently has no match.
func (l *Lineup) StreamURL(number string) (url string, configured bool, matched bool) {
	if _, exists := l.numberIndex[number]; !exists {
		return "", false, false
	}
	url, matched = l.current.Load().StreamURL[number]
	return url, true, matched
}

// Rebuild recomputes the match cache from the given playlist entries and
// atomically publishes the new snapshot. Called after every playlist
// refresh.
func (l *Lineup) Rebuild(entries []playlist.Entry) {
	idx := buildMatchIndex(entries)

	streamURL := make(map[string]string, len(l.channels))
	for _, ch := range l.channels {
		if url, ok := matchChannel(ch, idx); ok {
			streamURL[ch.Number] = url
		}
	}

	l.current.Store(&Snapshot{
		Channels:  l.channels,
		StreamURL: streamURL,
	})
}
