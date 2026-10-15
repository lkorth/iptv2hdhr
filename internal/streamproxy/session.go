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
	"strconv"
	"strings"
	"sync"
	"time"

	"iptv2hdhr/internal/lineup"
)

// maxSubscriberBacklog is how many bytes a subscriber can have queued (not
// yet written to its client) before it's considered too slow to keep up.
// Rather than disconnecting it (which stops playback and makes the viewer
// retune), its backlog is discarded so it skips ahead to live: for live TV a
// brief glitch is better than an error. This also keeps one slow client from
// holding up the others or growing memory without bound.
//
// It's measured in bytes rather than chunks because chunk sizes vary widely:
// ffmpeg's pipe output arrives in reads of ~8KB on average, so a limit in
// chunks would allow only about a second of video. 16MB is ~40s of a
// typical 3Mbit/s stream, enough to absorb the bursts ffmpeg sends when
// catching up after a late HLS segment.
const maxSubscriberBacklog = 16 * 1024 * 1024

// tsPacketSize is the size of an MPEG-TS packet. Discarded backlog is always
// a whole number of packets, so the client's demuxer stays aligned to packet
// boundaries across the gap, and the join prebuffer always starts on a
// packet boundary.
const tsPacketSize = 188

// joinPrebufferSize is how much of a session's most recent output is kept so
// that a client joining an already-running session (e.g. Plex retuning
// within idleSessionGrace, or a second tuner on the same channel) starts with
// a cushion of data rather than at the live edge, where any late upstream
// segment would stall its playback. 4MB is ~10s of a typical 3Mbit/s stream.
// It must stay well under maxSubscriberBacklog, since a joiner's backlog
// starts at this size.
const joinPrebufferSize = 4 * 1024 * 1024

// ffmpegStartupTimeout bounds how long we wait for ffmpeg to produce its
// first output bytes when remuxing an HLS upstream. If exceeded, the
// channel is reported unavailable (503) rather than leaving the client
// waiting indefinitely on a stuck process.
const ffmpegStartupTimeout = 15 * time.Second

// hlsLiveStartSegments is how many segments back from the live edge of an
// HLS playlist ffmpeg starts, which sets how late an upstream segment can
// arrive before playback stalls (see runRemux): with typical 6s segments,
// 3 segments is ~18s.
const hlsLiveStartSegments = 3

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
	subs      map[*subscriber]struct{}
	closed    bool
	idleTimer *time.Timer

	// recent holds up to joinPrebufferSize bytes of the session's most recent
	// output, starting at byte offset recentStart of the session's output
	// (always a multiple of tsPacketSize). New subscribers are sent it first.
	recent      []byte
	recentStart int64

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
func (h *Handler) getOrCreateSession(number string, stream lineup.Stream) (*session, *subscriber, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if s, ok := h.sessions[number]; ok && s.stream.Equal(stream) && !s.isClosed() {
		sub, ok := s.subscribe()
		return s, sub, ok
	}
	if s, ok := h.sessions[number]; ok {
		s.stop()
	}

	ctx, cancel := context.WithCancel(h.ctx)
	s := &session{
		number: number,
		stream: stream,
		ready:  make(chan struct{}),
		subs:   make(map[*subscriber]struct{}),
		cancel: cancel,
	}
	sub := newSubscriber()
	s.subs[sub] = struct{}{}

	h.sessions[number] = s
	go h.runSession(ctx, s)
	return s, sub, true
}

func (s *session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// subscribe registers a new subscriber, returning ok=false if the session
// has already ended.
func (s *session) subscribe() (*subscriber, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, false
	}
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
	sub := newSubscriber()
	sub.push(s.recent)
	s.subs[sub] = struct{}{}
	return sub, true
}

// unsubscribe removes sub from s's subscriber set. If s has no remaining
// subscribers, it's scheduled to be torn down after idleSessionGrace unless
// a new subscriber joins first.
func (h *Handler) unsubscribe(number string, s *session, sub *subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	delete(s.subs, sub)
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
// and closes every subscriber so their requests complete.
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
	for sub := range s.subs {
		sub.close()
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

// broadcast queues a copy of chunk for every current subscriber, and keeps
// it in the join prebuffer for future subscribers. A subscriber whose
// backlog would exceed maxSubscriberBacklog (too slow to keep up) skips
// ahead to live rather than blocking the others.
func (s *session) broadcast(chunk []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.recent = append(s.recent, chunk...)
	if excess := len(s.recent) - joinPrebufferSize; excess > 0 {
		// Trim from the front, rounding up so the prebuffer still starts on
		// an MPEG-TS packet boundary of the session's output.
		end := s.recentStart + int64(excess)
		if rem := end % tsPacketSize; rem != 0 {
			end += tsPacketSize - rem
		}
		trim := int(end - s.recentStart)
		s.recent = s.recent[trim:]
		s.recentStart = end
	}

	for sub := range s.subs {
		if skipped := sub.push(chunk); skipped > 0 {
			log.Printf("stream %s: client fell more than %dMB behind, skipped %d bytes to catch up with live", s.number, maxSubscriberBacklog/(1024*1024), skipped)
		}
	}
}

// subscriber is one client's queue of session output that hasn't been
// written to it yet. Chunks broadcast while the client is busy writing are
// coalesced into a single buffer, so the backlog is bounded by bytes
// (maxSubscriberBacklog) regardless of how small the individual chunks are.
type subscriber struct {
	mu      sync.Mutex
	pending []byte
	closed  bool

	// notify is signalled (without blocking) whenever pending gains data or
	// the subscriber is closed.
	notify chan struct{}
}

func newSubscriber() *subscriber {
	return &subscriber{notify: make(chan struct{}, 1)}
}

// push appends a copy of chunk to the subscriber's backlog. If that would
// exceed maxSubscriberBacklog, the existing backlog (rounded down to whole
// MPEG-TS packets) is discarded first; push returns how many bytes were
// discarded.
func (sub *subscriber) push(chunk []byte) int {
	if len(chunk) == 0 {
		return 0
	}
	sub.mu.Lock()
	skipped := 0
	if len(sub.pending)+len(chunk) > maxSubscriberBacklog {
		skipped = len(sub.pending) - len(sub.pending)%tsPacketSize
		sub.pending = append([]byte(nil), sub.pending[skipped:]...)
	}
	sub.pending = append(sub.pending, chunk...)
	sub.mu.Unlock()
	sub.signal()
	return skipped
}

// close marks the subscriber as ended. Data already queued is still
// returned by take.
func (sub *subscriber) close() {
	sub.mu.Lock()
	sub.closed = true
	sub.mu.Unlock()
	sub.signal()
}

func (sub *subscriber) signal() {
	select {
	case sub.notify <- struct{}{}:
	default:
	}
}

// take removes and returns everything queued so far, and whether the
// subscriber has been closed (in which case nothing more will be queued).
func (sub *subscriber) take() ([]byte, bool) {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	data := sub.pending
	sub.pending = nil
	return data, sub.closed
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
// well-formed source.
//
// ffmpeg starts hlsLiveStartSegments segments back from the live edge, and
// -re paces its output at the input's real-time rate, so it stays that far
// behind the live edge. That gap is the cushion against late upstream
// segments: a segment that arrives late isn't needed until ffmpeg reaches
// it. Without -re, ffmpeg sends each segment as soon as it downloads, so the
// client receives the stream in segment-sized blocks at the live edge, and
// Plex (which plays close to the newest data it has) stutters whenever a
// segment is late.
//
// After a stall ffmpeg still sends the delayed data as a burst to catch up;
// maxSubscriberBacklog absorbs that.
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
		"-live_start_index", strconv.Itoa(-hlsLiveStartSegments),
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
