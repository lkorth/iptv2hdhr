// Package streamproxy implements the /stream/{number} endpoint, which
// proxies the matched upstream stream for a configured channel number,
// transparently remuxing HLS upstreams to MPEG-TS (via ffmpeg) since
// HDHomeRun clients expect a continuous MPEG-TS byte stream rather than an
// HLS manifest.
//
// All clients currently tuned to the same channel number share a single
// upstream connection (and, for HLS, a single ffmpeg process): the first
// request for a channel starts a session, and subsequent requests for the
// same channel subscribe to that session's output. This keeps the number of
// upstream connections opened against an IPTV provider in line with the
// number of distinct channels being watched, rather than the number of Plex
// tuner sessions.
package streamproxy

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"iptv2hdhr/internal/lineup"
)

// copyBufferSize is the chunk size used when reading from the upstream (or
// ffmpeg) and broadcasting to subscribers. Each chunk is flushed immediately
// to subscribers for low-latency live streaming.
const copyBufferSize = 64 * 1024

// upstreamUserAgent is sent both on our own upstream request and (via
// ffmpeg's -user_agent) on the remux input request, unless the playlist
// entry specifies its own User-Agent.
const upstreamUserAgent = "iptv2hdhr/1.0"

// ffmpegPath is the ffmpeg binary used to remux HLS upstreams. Overridable
// in tests.
var ffmpegPath = "ffmpeg"

// Handler serves /stream/{number} by proxying (or remuxing) the current
// best-match upstream URL for that channel, sharing one session (and one
// upstream connection) across all subscribers to the same channel.
type Handler struct {
	lineup *lineup.Lineup
	client *http.Client

	// ctx bounds the lifetime of all sessions: when it's cancelled (e.g. on
	// server shutdown), every active upstream connection/ffmpeg process is
	// torn down.
	ctx context.Context

	mu       sync.Mutex
	sessions map[string]*session // channel number -> active session
}

// New creates a stream proxy handler backed by the given lineup. ctx bounds
// the lifetime of shared upstream connections and ffmpeg processes.
func New(l *lineup.Lineup, ctx context.Context) *Handler {
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
		ctx:      ctx,
		sessions: make(map[string]*session),
	}
}

// ServeHTTP handles GET /stream/{number}.
//
//   - If {number} is not a configured channel, responds 404 (genuinely
//     doesn't exist; a stale Plex scan or config error).
//   - If {number} is configured but currently has no matched upstream URL, or
//     the upstream is unreachable/erroring, responds 503 (temporarily
//     unavailable; normal/transient, does not imply a rescan is needed).
//   - If the upstream is an HLS playlist (by Content-Type or .m3u8/.m3u
//     extension), the response is remuxed to MPEG-TS via ffmpeg. Otherwise the
//     upstream body is passed through as-is.
//   - Extra request headers from the playlist entry (e.g. a Referer from an
//     #EXTVLCOPT:http-referrer line) are sent on the upstream request and, for
//     HLS, forwarded by ffmpeg to every playlist/segment fetch.
//
// The underlying upstream connection (and ffmpeg process, for HLS) is shared
// with any other request currently tuned to the same channel number.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	number := strings.TrimPrefix(r.URL.Path, "/stream/")

	stream, configured, matched := h.lineup.Stream(number)
	if !configured {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}
	if !matched {
		http.Error(w, "channel temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	s, sub, ok := h.getOrCreateSession(number, stream)
	if !ok {
		// Rare race: an existing session ended right as we tried to join
		// it. The client (or Plex) will retry, establishing a fresh
		// session.
		http.Error(w, "channel temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	select {
	case <-s.ready:
	case <-r.Context().Done():
		h.unsubscribe(number, s, sub)
		return
	}

	if s.err != nil {
		h.unsubscribe(number, s, sub)
		http.Error(w, "upstream error", http.StatusServiceUnavailable)
		return
	}
	defer h.unsubscribe(number, s, sub)

	w.Header().Set("Content-Type", s.contentType)
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	for {
		select {
		case <-sub.notify:
			data, closed := sub.take()
			if len(data) > 0 {
				if _, err := w.Write(data); err != nil {
					log.Printf("stream %s: client %s: write failed: %v", number, r.RemoteAddr, err)
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			if closed {
				log.Printf("stream %s: client %s: session ended", number, r.RemoteAddr)
				return
			}
		case <-r.Context().Done():
			log.Printf("stream %s: client %s disconnected", number, r.RemoteAddr)
			return
		}
	}
}

// isHLS reports whether the upstream response looks like an HLS playlist
// (M3U8) rather than a directly-streamable format, based on its
// Content-Type or, failing that, the URL's file extension.
func isHLS(upstreamURL, contentType string) bool {
	if strings.Contains(strings.ToLower(contentType), "mpegurl") {
		return true
	}
	if u, err := url.Parse(upstreamURL); err == nil {
		switch strings.ToLower(path.Ext(u.Path)) {
		case ".m3u8", ".m3u":
			return true
		}
	}
	return false
}
