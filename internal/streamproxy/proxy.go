// Package streamproxy implements the /stream/{number} endpoint, which
// proxies the matched upstream stream for a configured channel number,
// transparently remuxing HLS upstreams to MPEG-TS (via ffmpeg) since
// HDHomeRun clients expect a continuous MPEG-TS byte stream rather than an
// HLS manifest.
package streamproxy

import (
	"bytes"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"path"
	"strings"
	"time"

	"iptv2hdhr/internal/lineup"
)

// copyBufferSize is the chunk size used when copying stream data to the
// client. Each chunk is flushed immediately for low-latency live streaming.
const copyBufferSize = 64 * 1024

// upstreamUserAgent is sent both on our own probe request and (via
// ffmpeg's -user_agent) on the remux input request.
const upstreamUserAgent = "iptv2hdhr/1.0"

// ffmpegPath is the ffmpeg binary used to remux HLS upstreams. Overridable
// in tests.
var ffmpegPath = "ffmpeg"

// Handler serves /stream/{number} by proxying (or remuxing) the current
// best-match upstream URL for that channel.
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
// - If the upstream is an HLS playlist (by Content-Type or .m3u8/.m3u
//   extension), the response is remuxed to MPEG-TS via ffmpeg. Otherwise the
//   upstream body is passed through as-is.
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
	req.Header.Set("User-Agent", upstreamUserAgent)
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

	if isHLS(upstreamURL, resp.Header.Get("Content-Type")) {
		resp.Body.Close()
		remuxToMPEGTS(w, r, upstreamURL)
		return
	}

	passthrough(w, resp)
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

// passthrough copies resp's body directly to w, forwarding a small set of
// streaming-relevant headers.
func passthrough(w http.ResponseWriter, resp *http.Response) {
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

// remuxToMPEGTS runs ffmpeg to remux upstreamURL (an HLS playlist) into a
// continuous MPEG-TS stream and copies its stdout to w. ffmpeg is killed
// when r's context is done (e.g. the client disconnects).
//
// Video is passed through unmodified (-c:v copy). Audio is re-encoded to
// AC-3: Plex's HDHomeRun tuner emulation expects ATSC-style AC-3 audio, and
// fails to tune (codecpar sample_rate/channels never populate, "sample rate
// not set") when the audio is AAC, even when copied byte-for-byte from a
// well-formed source. -re paces ffmpeg's reads at the input's real-time
// rate so the output stream matches a real tuner's steady bitrate rather
// than arriving in bursts.
func remuxToMPEGTS(w http.ResponseWriter, r *http.Request, upstreamURL string) {
	cmd := exec.CommandContext(r.Context(), ffmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-user_agent", upstreamUserAgent,
		"-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5",
		"-re",
		"-i", upstreamURL,
		"-c:v", "copy",
		"-c:a", "ac3", "-b:a", "192k",
		"-f", "mpegts",
		"-",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "remux setup failed", http.StatusServiceUnavailable)
		return
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		http.Error(w, "remux unavailable", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "video/mp2t")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	buf := make([]byte, copyBufferSize)
	for {
		n, readErr := stdout.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				break
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			break
		}
	}

	if err := cmd.Wait(); err != nil && r.Context().Err() == nil {
		log.Printf("stream remux for %s: ffmpeg exited: %v: %s", upstreamURL, err, strings.TrimSpace(stderr.String()))
	}
}
