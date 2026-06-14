// Package streamproxy implements the /stream/{number} endpoint, which
// proxies a raw passthrough of the matched upstream stream for a configured
// channel number.
package streamproxy

import (
	"net"
	"net/http"
	"strings"
	"time"

	"iptv2hdhr/internal/lineup"
)

// copyBufferSize is the chunk size used when copying the upstream response
// body to the client. Each chunk is flushed immediately for low-latency
// live streaming.
const copyBufferSize = 64 * 1024

// Handler serves /stream/{number} by proxying the current best-match
// upstream URL for that channel.
type Handler struct {
	lineup *lineup.Lineup
	client *http.Client
}

// New creates a stream proxy handler backed by the given lineup.
func New(l *lineup.Lineup) *Handler {
	return &Handler{
		lineup: l,
		client: &http.Client{
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 15 * time.Second,
			},
			// No overall Timeout: streams are long-lived.
		},
	}
}

// ServeHTTP handles GET /stream/{number}.
//
// - If {number} is not a configured channel, responds 404 (genuinely
//   doesn't exist; a stale Plex scan or config error).
// - If {number} is configured but currently has no matched upstream URL, or
//   the upstream is unreachable/erroring, responds 503 (temporarily
//   unavailable; normal/transient, does not imply a rescan is needed).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	number := strings.TrimPrefix(r.URL.Path, "/stream/")

	upstreamURL, configured, matched := h.lineup.StreamURL(number)
	if !configured {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}
	if !matched {
		http.Error(w, "channel temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		http.Error(w, "bad upstream url", http.StatusServiceUnavailable)
		return
	}
	req.Header.Set("User-Agent", "iptv2hdhr/1.0")
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		http.Error(w, "upstream error", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		http.Error(w, "upstream error", http.StatusServiceUnavailable)
		return
	}

	for _, header := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := resp.Header.Get(header); v != "" {
			w.Header().Set(header, v)
		}
	}
	if resp.Header.Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "video/mp2t")
	}
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)
	buf := make([]byte, copyBufferSize)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			return
		}
	}
}
