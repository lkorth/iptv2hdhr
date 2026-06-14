// Package guide fetches and merges XMLTV guide data, and builds the
// /guide.xml document served to Plex, filling gaps in the schedule with
// placeholder "no program information" blocks.
package guide

import (
	"context"
	"log"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"iptv2hdhr/internal/config"
	"iptv2hdhr/internal/fetchutil"
	"iptv2hdhr/internal/guide/xmltv"
)

// Index is the merged, queryable view of all configured guide sources.
type Index struct {
	// ProgrammesByChannelID maps XMLTV <channel id> (== tvg-id from the
	// guide source) -> programmes sorted by Start time.
	ProgrammesByChannelID map[string][]xmltv.Programme
	// ChannelMeta maps channel id -> display name/icon, for fallback
	// display-name lookups / debugging.
	ChannelMeta map[string]xmltv.Channel
}

// emptyIndex returns a non-nil, empty Index.
func emptyIndex() *Index {
	return &Index{
		ProgrammesByChannelID: make(map[string][]xmltv.Programme),
		ChannelMeta:           make(map[string]xmltv.Channel),
	}
}

// Fetcher periodically fetches and parses one or more configured XMLTV guide
// sources and merges them into a single, lock-free-readable Index.
type Fetcher struct {
	sources []config.GuideSource
	client  *http.Client

	mu            sync.Mutex
	lastGood      map[string]*xmltv.Document // by source name; preserved across failed refreshes
	etags         map[string]string          // by source name; for conditional refetches
	lastModifieds map[string]string          // by source name; for conditional refetches

	current atomic.Pointer[Index]
}

// NewFetcher creates a Fetcher for the given guide sources. Call RefreshNow
// once before Start so Current() returns useful data immediately.
func NewFetcher(sources []config.GuideSource) *Fetcher {
	f := &Fetcher{
		sources:       sources,
		client:        &http.Client{Timeout: 60 * time.Second},
		lastGood:      make(map[string]*xmltv.Document),
		etags:         make(map[string]string),
		lastModifieds: make(map[string]string),
	}
	f.current.Store(emptyIndex())
	return f
}

// Current returns the most recently merged guide index. Safe for concurrent
// use; never blocks. Never nil.
func (f *Fetcher) Current() *Index {
	return f.current.Load()
}

// RefreshNow synchronously fetches and parses every configured source once
// and updates the merged index. Per-source errors are logged; a failing
// source simply keeps its previous (possibly empty) data.
func (f *Fetcher) RefreshNow(ctx context.Context) {
	for _, src := range f.sources {
		f.refreshSource(ctx, src)
	}
	f.merge()
}

// Start launches one refresh goroutine per source, each on its own ticker
// using that source's configured refresh interval. Start returns
// immediately; goroutines stop when ctx is done.
func (f *Fetcher) Start(ctx context.Context) {
	for _, src := range f.sources {
		go func(src config.GuideSource) {
			ticker := time.NewTicker(src.RefreshInterval.Duration())
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					f.refreshSource(ctx, src)
					f.merge()
				case <-ctx.Done():
					return
				}
			}
		}(src)
	}
}

func (f *Fetcher) refreshSource(ctx context.Context, src config.GuideSource) {
	f.mu.Lock()
	etag, lastModified := f.etags[src.Name], f.lastModifieds[src.Name]
	f.mu.Unlock()

	result, err := fetchutil.GetConditional(ctx, f.client, src.URL, etag, lastModified)
	if err != nil {
		log.Printf("guide %q: fetch failed: %v", src.Name, err)
		return
	}
	if result.NotModified {
		log.Printf("guide %q: not modified, keeping previous data", src.Name)
		return
	}

	doc, err := xmltv.Parse(result.Data)
	if err != nil {
		log.Printf("guide %q: parse failed: %v", src.Name, err)
		return
	}

	f.mu.Lock()
	f.lastGood[src.Name] = doc
	f.etags[src.Name] = result.ETag
	f.lastModifieds[src.Name] = result.LastModified
	f.mu.Unlock()

	log.Printf("guide %q: loaded %d channels, %d programmes", src.Name, len(doc.Channels), len(doc.Programmes))
}

// merge rebuilds the combined Index from each source's last-known-good
// document and atomically publishes it.
func (f *Fetcher) merge() {
	f.mu.Lock()
	defer f.mu.Unlock()

	idx := emptyIndex()
	for _, src := range f.sources {
		doc, ok := f.lastGood[src.Name]
		if !ok {
			continue
		}
		for _, ch := range doc.Channels {
			if _, exists := idx.ChannelMeta[ch.ID]; !exists {
				idx.ChannelMeta[ch.ID] = ch
			}
		}
		for _, p := range doc.Programmes {
			idx.ProgrammesByChannelID[p.Channel] = append(idx.ProgrammesByChannelID[p.Channel], p)
		}
	}

	for id, programmes := range idx.ProgrammesByChannelID {
		sort.Slice(programmes, func(i, j int) bool {
			ti, erri := xmltv.ParseTime(programmes[i].Start)
			tj, errj := xmltv.ParseTime(programmes[j].Start)
			if erri != nil || errj != nil {
				return programmes[i].Start < programmes[j].Start
			}
			return ti.Before(tj)
		})
		idx.ProgrammesByChannelID[id] = programmes
	}

	f.current.Store(idx)
}
