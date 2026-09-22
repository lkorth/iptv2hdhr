package streamproxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"iptv2hdhr/internal/lineup"
)

// subscriberQueueSize is how many copyBufferSize chunks a subscriber's
// channel can buffer (~4MB) before it's considered too slow to keep up and
// is dropped, so one slow client can't hold up the others or grow memory
// without bound.
const subscriberQueueSize = 64

// ffmpegStartupTimeout bounds how long we wait for ffmpeg to produce its
// first output bytes when remuxing an HLS upstream. If exceeded, the
// channel is reported unavailable (503) rather than leaving the client
// waiting indefinitely on a stuck process.
const ffmpegStartupTimeout = 15 * time.Second

// idleSessionGrace is how long a session with no subscribers is kept alive
// (upstream connection / ffmpeg process still running) before being torn
// down, so a client that quickly reconnects (channel surf, brief blip)
// doesn't pay the cost of re-establishing the upstream.
const idleSessionGrace = 5 * time.Second

// session represents one shared upstream connection (and, for HLS sources,
// one ffmpeg remux process) for a single channel. Every current subscriber
// (an HTTP client tuned to that channel) receives the same byte stream, so
// an IPTV provider sees at most one connection per channel regardless of how
// many Plex tuners are watching it.
type session struct {
	number string
	stream lineup.Stream

	// contentType and err are written once by runSession before ready is
	// closed, and read by subscribers only after observing ready closed, so
	// no further synchronization is needed for them.
	contentType string
	err         error
	ready       chan struct{}

	mu        sync.Mutex
	subs      map[chan []byte]struct{}
	closed    bool
	idleTimer *time.Timer

	cancel context.CancelFunc
}

// getOrCreateSession returns the active session for number (creating one if
// none exists or if the matched upstream URL/headers have changed since the
// existing session was started) along with a subscription for the calling
// request.
// ok is false if an existing session ended right as we tried to join it; the
// caller should report the channel as temporarily unavailable and let the
// client retry.
//
// For a newly-created session, the subscription is registered before the
// session's goroutine starts, so the producer can never finish (or broadcast
// data) before the triggering request is listening.
func (h *Handler) getOrCreateSession(number string, stream lineup.Stream) (*session, chan []byte, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if s, ok := h.sessions[number]; ok && s.stream.Equal(stream) && !s.isClosed() {
		ch, ok := s.subscribe()
		return s, ch, ok
	}
	if s, ok := h.sessions[number]; ok {
		s.stop()
	}

	ctx, cancel := context.WithCancel(h.ctx)
	s := &session{
		number: number,
		stream: stream,
		ready:  make(chan struct{}),
		subs:   make(map[chan []byte]struct{}),
		cancel: cancel,
	}
	ch := make(chan []byte, subscriberQueueSize)
	s.subs[ch] = struct{}{}

	h.sessions[number] = s
	go h.runSession(ctx, s)
	return s, ch, true
}

func (s *session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// subscribe registers a new subscriber, returning ok=false if the session
// has already ended.
func (s *session) subscribe() (chan []byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, false
	}
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
	ch := make(chan []byte, subscriberQueueSize)
	s.subs[ch] = struct{}{}
	return ch, true
}

// unsubscribe removes ch from s's subscriber set. If s has no remaining
// subscribers, it's scheduled to be torn down after idleSessionGrace unless
// a new subscriber joins first.
func (h *Handler) unsubscribe(number string, s *session, ch chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	delete(s.subs, ch)
	if len(s.subs) == 0 {
		s.idleTimer = time.AfterFunc(idleSessionGrace, func() {
			h.endIfIdle(number, s)
		})
	}
}

// endIfIdle tears down s if it's still idle (no subscribers) when its grace
// period expires.
func (h *Handler) endIfIdle(number string, s *session) {
	s.mu.Lock()
	idle := !s.closed && len(s.subs) == 0
	s.mu.Unlock()
	if !idle {
		return
	}

	h.mu.Lock()
	if h.sessions[number] == s {
		delete(h.sessions, number)
	}
	h.mu.Unlock()

	s.stop()
}

// stop ends the session: it cancels the upstream connection/ffmpeg process
// and closes every subscriber's channel so their requests complete.
func (s *session) stop() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
	for ch := range s.subs {
		close(ch)
	}
	s.subs = nil
	s.mu.Unlock()
	s.cancel()
}

// fail marks startup as having failed with err and unblocks anything
// waiting on ready.
func (s *session) fail(err error) {
	s.err = err
	close(s.ready)
}

// broadcast sends a copy of chunk to every current subscriber. A subscriber
// whose queue is full (too slow to keep up) is dropped so it can't block the
// others.
func (s *session) broadcast(chunk []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		buf := make([]byte, len(chunk))
		copy(buf, chunk)
		select {
		case ch <- buf:
		default:
			log.Printf("stream %s: dropping subscriber that fell %d chunks behind", s.number, subscriberQueueSize)
			close(ch)
			delete(s.subs, ch)
		}
	}
}

// runSession establishes the shared upstream connection for s and, on
// success, pumps its data to all subscribers until the upstream ends, s is
// cancelled, or (for HLS) the ffmpeg remux process exits. It always removes
// s from the handler's session registry and stops it before returning.
func (h *Handler) runSession(ctx context.Context, s *session) {
	started := time.Now()
	log.Printf("stream %s: session starting for %s", s.number, s.stream.URL)
	defer func() {
		h.mu.Lock()
		if h.sessions[s.number] == s {
			delete(h.sessions, s.number)
		}
		h.mu.Unlock()
		s.stop()
		reason := "upstream ended"
		if s.err != nil {
			reason = "startup failed: " + s.err.Error()
		} else if ctx.Err() != nil {
			reason = "stopped (no subscribers or shutdown)"
		}
		log.Printf("stream %s: session ended after %s: %s", s.number, time.Since(started).Round(time.Second), reason)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.stream.URL, nil)
	if err != nil {
		s.fail(err)
		return
	}
	req.Header.Set("User-Agent", upstreamUserAgent)
	for k, v := range s.stream.Headers {
		req.Header.Set(k, v)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		s.fail(err)
		return
	}

	if resp.StatusCode >= 400 {
		resp.Body.Close()
		s.fail(fmt.Errorf("upstream returned status %d", resp.StatusCode))
		return
	}

	if isHLS(s.stream.URL, resp.Header.Get("Content-Type")) {
		resp.Body.Close()
		s.runRemux(ctx)
		return
	}

	s.contentType = resp.Header.Get("Content-Type")
	if s.contentType == "" {
		s.contentType = "video/mp2t"
	}
	close(s.ready)

	s.pump(resp.Body)
	resp.Body.Close()
}

// pump reads from r in copyBufferSize chunks, broadcasting each to every
// subscriber, until r returns an error (including io.EOF).
func (s *session) pump(r io.Reader) {
	buf := make([]byte, copyBufferSize)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			s.broadcast(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// runRemux runs ffmpeg to remux s.stream.URL (an HLS playlist) into a
// continuous MPEG-TS stream and pumps its stdout to subscribers. Any extra
// request headers on the stream are passed via -headers/-user_agent, which
// ffmpeg's HLS demuxer forwards to every playlist and segment request.
//
// Video is passed through unmodified (-c:v copy). Audio is re-encoded to
// AC-3: Plex's HDHomeRun tuner emulation expects ATSC-style AC-3 audio, and
// fails to tune (codecpar sample_rate/channels never populate, "sample rate
// not set") when the audio is AAC, even when copied byte-for-byte from a
// well-formed source. -re paces ffmpeg's reads at the input's real-time rate
// so the output stream matches a real tuner's steady bitrate rather than
// arriving in bursts.
//
// If ffmpeg doesn't produce any output within ffmpegStartupTimeout, it's
// killed and the channel is reported unavailable rather than leaving
// subscribers waiting on a stuck process.
func (s *session) runRemux(ctx context.Context) {
	args := []string{
		"-hide_banner", "-loglevel", "warning",
		"-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5",
	}
	args = append(args, ffmpegHeaderArgs(s.stream.Headers)...)
	args = append(args,
		"-re",
		"-i", s.stream.URL,
		"-c:v", "copy",
		"-c:a", "ac3", "-b:a", "192k",
		"-f", "mpegts",
		"-",
	)
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.fail(err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.fail(err)
		return
	}

	if err := cmd.Start(); err != nil {
		s.fail(err)
		return
	}

	// Relay ffmpeg's warnings/errors to our log as they happen, so stalls,
	// segment fetch failures and reconnects are visible while the stream is
	// still running rather than only once it exits.
	var lastErr string
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			lastErr = line
			log.Printf("stream %s: ffmpeg: %s", s.number, line)
		}
	}()

	buf := make([]byte, copyBufferSize)
	n, err := readWithTimeout(stdout, buf, ffmpegStartupTimeout)
	if err != nil {
		cmd.Process.Kill()
		<-stderrDone
		cmd.Wait()
		s.fail(fmt.Errorf("remux for %s produced no output: %w (last ffmpeg message: %q)", s.stream.URL, err, lastErr))
		return
	}

	s.contentType = "video/mp2t"
	close(s.ready)

	if n > 0 {
		s.broadcast(buf[:n])
	}
	s.pump(stdout)

	<-stderrDone
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		log.Printf("stream %s: ffmpeg exited: %v (last message: %q)", s.number, err, lastErr)
	}
}

// ffmpegHeaderArgs builds the ffmpeg input options that set the request
// User-Agent and any extra headers. User-Agent goes through -user_agent
// (defaulting to upstreamUserAgent); everything else is joined into a single
// CRLF-terminated -headers block, in sorted order so the argv is stable.
func ffmpegHeaderArgs(headers map[string]string) []string {
	userAgent := upstreamUserAgent
	var extra []string
	for k, v := range headers {
		if strings.EqualFold(k, "User-Agent") {
			userAgent = v
			continue
		}
		extra = append(extra, k+": "+v+"\r\n")
	}
	sort.Strings(extra)

	args := []string{"-user_agent", userAgent}
	if len(extra) > 0 {
		args = append(args, "-headers", strings.Join(extra, ""))
	}
	return args
}

// readWithTimeout reads from r into buf, returning an error if no read
// completes within timeout.
func readWithTimeout(r io.Reader, buf []byte, timeout time.Duration) (int, error) {
	type result struct {
		n   int
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		n, err := r.Read(buf)
		resCh <- result{n, err}
	}()
	select {
	case res := <-resCh:
		return res.n, res.err
	case <-time.After(timeout):
		return 0, fmt.Errorf("timed out after %s", timeout)
	}
}
