package guide

import (
	"fmt"
	"sort"
	"time"

	"iptv2hdhr/internal/guide/xmltv"
)

const (
	// GuideLookback is how far before "now" the generated guide window
	// starts.
	GuideLookback = 1 * time.Hour
	// GuideLookahead is how far after "now" the generated guide window
	// extends.
	GuideLookahead = 24 * time.Hour
	// MaxFillerBlockDuration caps the length of a single filler programme;
	// longer gaps are split into multiple blocks of at most this duration.
	MaxFillerBlockDuration = 6 * time.Hour
	// FillerTitle is the title used for synthetic "nothing scheduled"
	// programmes.
	FillerTitle = "No Program Information Available"
)

// BuildChannelProgrammes returns the full sequence of <programme> entries
// for one channel slot, covering [now-GuideLookback, now+GuideLookahead],
// combining real programmes (from idx, looked up by tvgID) with filler for
// gaps. channelID is the stable XMLTV <channel id> stamped onto every output
// programme (the lineup's per-slot stable id, e.g. "slot-1001" — not the
// upstream tvg-id).
func BuildChannelProgrammes(idx *Index, tvgID string, channelID string, channelName string, now time.Time) []xmltv.Programme {
	windowStart := now.Add(-GuideLookback)
	windowEnd := now.Add(GuideLookahead)

	var real []xmltv.Programme
	if idx != nil && tvgID != "" {
		real = idx.ProgrammesByChannelID[tvgID]
	}

	clipped := clipToWindow(real, windowStart, windowEnd)

	var out []xmltv.Programme
	cursor := windowStart
	for _, p := range clipped {
		start, _ := xmltv.ParseTime(p.Start)
		stop, _ := xmltv.ParseTime(p.Stop)

		if start.After(cursor) {
			out = append(out, fillerBlocks(cursor, start, channelID, channelName)...)
		}

		p.Channel = channelID
		out = append(out, p)

		if stop.After(cursor) {
			cursor = stop
		}
	}

	if windowEnd.After(cursor) {
		out = append(out, fillerBlocks(cursor, windowEnd, channelID, channelName)...)
	}

	return out
}

// clipToWindow filters programmes to those overlapping [windowStart,
// windowEnd), clipping start/stop to the window bounds for partial overlaps,
// and returns them sorted by (clipped) start time.
func clipToWindow(programmes []xmltv.Programme, windowStart, windowEnd time.Time) []xmltv.Programme {
	var out []xmltv.Programme
	for _, p := range programmes {
		start, err := xmltv.ParseTime(p.Start)
		if err != nil {
			continue
		}
		stop, err := xmltv.ParseTime(p.Stop)
		if err != nil {
			continue
		}

		if !stop.After(windowStart) || !start.Before(windowEnd) {
			continue
		}

		if start.Before(windowStart) {
			start = windowStart
		}
		if stop.After(windowEnd) {
			stop = windowEnd
		}
		if !start.Before(stop) {
			continue
		}

		clipped := p
		clipped.Start = xmltv.FormatTime(start)
		clipped.Stop = xmltv.FormatTime(stop)
		out = append(out, clipped)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Start < out[j].Start
	})

	// Drop entries that start before the previous (sorted) entry's stop, to
	// avoid negative-duration gaps from overlapping/out-of-order source data.
	var dedup []xmltv.Programme
	var prevStop time.Time
	for i, p := range out {
		start, _ := xmltv.ParseTime(p.Start)
		if i > 0 && !start.After(prevStop) && start.Before(prevStop) {
			continue
		}
		stop, _ := xmltv.ParseTime(p.Stop)
		dedup = append(dedup, p)
		prevStop = stop
	}

	return dedup
}

// fillerBlocks generates one or more filler programmes covering [start, end),
// splitting into chunks of at most MaxFillerBlockDuration. Returns nil if
// end is not after start.
func fillerBlocks(start, end time.Time, channelID, channelName string) []xmltv.Programme {
	if !end.After(start) {
		return nil
	}

	var out []xmltv.Programme
	for cursor := start; cursor.Before(end); {
		blockEnd := cursor.Add(MaxFillerBlockDuration)
		if blockEnd.After(end) {
			blockEnd = end
		}
		out = append(out, xmltv.Programme{
			Channel: channelID,
			Start:   xmltv.FormatTime(cursor),
			Stop:    xmltv.FormatTime(blockEnd),
			Title:   []xmltv.Title{{Value: FillerTitle}},
			Desc:    []xmltv.Desc{{Value: fmt.Sprintf("%s — no schedule data", channelName)}},
		})
		cursor = blockEnd
	}
	return out
}
