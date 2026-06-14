// Package logoproxy serves /logo/{number}, fetching each configured
// channel's logo from its upstream URL and caching it in memory so Plex
// never contacts upstream logo URLs directly.
package logoproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"iptv2hdhr/internal/lineup"
)

const upstreamUserAgent = "iptv2hdhr/1.0"

type cachedLogo struct {
	data        []byte
	contentType string
}

// Handler serves /logo/{number}.
type Handler struct {
	lineup *lineup.Lineup
	client *http.Client

	mu    sync.Mutex
	cache map[string]cachedLogo
}

// New creates a Handler that serves configured channel logos under
// /logo/{number}.
func New(l *lineup.Lineup) *Handler {
	return &Handler{
		lineup: l,
		client: &http.Client{Timeout: 30 * time.Second},
		cache:  make(map[string]cachedLogo),
	}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	number := strings.TrimPrefix(r.URL.Path, "/logo/")

	ch, ok := h.lineup.ChannelByNumber(number)
	if !ok || ch.Logo == "" {
		http.Error(w, "logo not found", http.StatusNotFound)
		return
	}

	h.mu.Lock()
	logo, cached := h.cache[number]
	h.mu.Unlock()

	if !cached {
		var err error
		logo, err = h.fetch(r.Context(), ch.Logo)
		if err != nil {
			http.Error(w, "fetching logo: "+err.Error(), http.StatusBadGateway)
			return
		}

		h.mu.Lock()
		h.cache[number] = logo
		h.mu.Unlock()
	}

	w.Header().Set("Content-Type", logo.contentType)
	w.Write(logo.data)
}

func (h *Handler) fetch(ctx context.Context, logoURL string) (cachedLogo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, logoURL, nil)
	if err != nil {
		return cachedLogo{}, err
	}
	req.Header.Set("User-Agent", upstreamUserAgent)

	resp, err := h.client.Do(req)
	if err != nil {
		return cachedLogo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return cachedLogo{}, fmt.Errorf("fetching %s: unexpected status %d", logoURL, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return cachedLogo{}, err
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return cachedLogo{data: data, contentType: contentType}, nil
}
