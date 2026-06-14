package playlist

import (
	"context"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"iptv2hdhr/internal/config"
	"iptv2hdhr/internal/fetchutil"
)

// Fetcher periodically fetches and parses one or more configured M3U
// playlists and merges their entries into a single, lock-free-readable
// snapshot.
type Fetcher struct {
	sources []config.PlaylistSource
	client  *http.Client

	mu            sync.Mutex
	lastGood      map[string][]Entry // by source name; preserved across failed refreshes
	etags         map[string]string  // by source name; for conditional refetches
	lastModifieds map[string]string  // by source name; for conditional refetches

	current atomic.Pointer[[]Entry]
}

// NewFetcher creates a Fetcher for the given playlist sources. Call
// RefreshNow once before Start so Current() returns data immediately.
func NewFetcher(sources []config.PlaylistSource) *Fetcher {
	return &Fetcher{
		sources:       sources,
		client:        &http.Client{Timeout: 60 * time.Second},
		lastGood:      make(map[string][]Entry),
		etags:         make(map[string]string),
		lastModifieds: make(map[string]string),
	}
}

// Current returns the most recently merged set of playlist entries. Safe
// for concurrent use; never blocks.
func (f *Fetcher) Current() []Entry {
	p := f.current.Load()
	if p == nil {
		return nil
	}
	return *p
}

// RefreshNow synchronously fetches and parses every configured source once
// and updates the merged snapshot. Per-source errors are logged; a failing
// source simply keeps its previous (possibly empty) entries.
func (f *Fetcher) RefreshNow(ctx context.Context) {
	for _, src := range f.sources {
		f.refreshSource(ctx, src)
	}
	f.merge()
}

// Start launches one refresh goroutine per source, each on its own ticker
// using that source's configured refresh interval. After each successful
// refresh+merge, onUpdate (if non-nil) is called with the new merged
// snapshot. Start returns immediately; goroutines stop when ctx is done.
func (f *Fetcher) Start(ctx context.Context, onUpdate func([]Entry)) {
	for _, src := range f.sources {
		go func(src config.PlaylistSource) {
			ticker := time.NewTicker(src.RefreshInterval.Duration())
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					f.refreshSource(ctx, src)
					f.merge()
					if onUpdate != nil {
						onUpdate(f.Current())
					}
				case <-ctx.Done():
					return
				}
			}
		}(src)
	}
}

func (f *Fetcher) refreshSource(ctx context.Context, src config.PlaylistSource) {
	f.mu.Lock()
	etag, lastModified := f.etags[src.Name], f.lastModifieds[src.Name]
	f.mu.Unlock()

	result, err := fetchutil.GetConditional(ctx, f.client, src.URL, etag, lastModified)
	if err != nil {
		log.Printf("playlist %q: fetch failed: %v", src.Name, err)
		return
	}
	if result.NotModified {
		log.Printf("playlist %q: not modified, keeping previous entries", src.Name)
		return
	}

	entries, err := Parse(result.Data)
	if err != nil {
		log.Printf("playlist %q: parse failed: %v", src.Name, err)
		return
	}

	f.mu.Lock()
	f.lastGood[src.Name] = entries
	f.etags[src.Name] = result.ETag
	f.lastModifieds[src.Name] = result.LastModified
	f.mu.Unlock()

	log.Printf("playlist %q: loaded %d entries", src.Name, len(entries))
}

// merge rebuilds the combined entry list from each source's last-known-good
// entries and atomically publishes it.
func (f *Fetcher) merge() {
	f.mu.Lock()
	defer f.mu.Unlock()

	var merged []Entry
	for _, src := range f.sources {
		merged = append(merged, f.lastGood[src.Name]...)
	}
	f.current.Store(&merged)
}
