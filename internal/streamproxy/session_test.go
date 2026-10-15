package streamproxy

import (
	"bytes"
	"testing"
)

func newTestSession() (*session, *subscriber) {
	s := &session{
		number: "1001",
		ready:  make(chan struct{}),
		subs:   make(map[*subscriber]struct{}),
		cancel: func() {},
	}
	sub := newSubscriber()
	s.subs[sub] = struct{}{}
	return s, sub
}

// ffmpeg's pipe output arrives in small reads, so a subscriber that briefly
// stops reading can have thousands of tiny chunks queued. As long as their
// total size is under maxSubscriberBacklog, it must not be dropped, and it
// must receive every byte in order once it catches up.
func TestBroadcast_ManySmallChunksToSlowSubscriberAreNotDropped(t *testing.T) {
	s, sub := newTestSession()

	var want bytes.Buffer
	for i := 0; i < 10000; i++ {
		chunk := bytes.Repeat([]byte{byte(i)}, 188)
		want.Write(chunk)
		s.broadcast(chunk)
	}

	if _, ok := s.subs[sub]; !ok {
		t.Fatalf("subscriber with %d bytes queued was dropped, limit is %d", want.Len(), maxSubscriberBacklog)
	}
	select {
	case <-sub.notify:
	default:
		t.Fatal("subscriber was not notified of queued data")
	}
	got, closed := sub.take()
	if closed {
		t.Error("subscriber reported closed, want open")
	}
	if !bytes.Equal(got, want.Bytes()) {
		t.Errorf("took %d bytes, want the %d bytes broadcast, in order", len(got), want.Len())
	}
}

// A subscriber that falls more than maxSubscriberBacklog behind must stay
// connected and skip ahead to live, discarding only whole MPEG-TS packets so
// its demuxer stays aligned.
func TestBroadcast_SubscriberOverBacklogLimitSkipsAheadInsteadOfDropping(t *testing.T) {
	s, sub := newTestSession()

	// A partial packet already delivered before the backlog built up, so the
	// queued data doesn't start on a packet boundary.
	s.broadcast(make([]byte, 100))
	sub.take()

	chunk := make([]byte, copyBufferSize)
	queued := 0
	for queued+len(chunk) <= maxSubscriberBacklog {
		s.broadcast(chunk)
		queued += len(chunk)
	}

	last := bytes.Repeat([]byte{0x47}, len(chunk))
	s.broadcast(last)

	if _, ok := s.subs[sub]; !ok {
		t.Fatal("subscriber over the backlog limit was dropped, want it kept")
	}
	got, closed := sub.take()
	if closed {
		t.Error("subscriber reported closed, want open")
	}
	if !bytes.HasSuffix(got, last) {
		t.Error("backlog after skipping doesn't end with the newest chunk")
	}
	skipped := queued + len(last) - len(got)
	if skipped <= 0 {
		t.Fatalf("backlog is %d bytes, want old data discarded", len(got))
	}
	if skipped%tsPacketSize != 0 {
		t.Errorf("skipped %d bytes, want a multiple of %d", skipped, tsPacketSize)
	}
	if len(got) > maxSubscriberBacklog {
		t.Errorf("backlog is %d bytes, want at most %d", len(got), maxSubscriberBacklog)
	}
}

func TestSessionStop_ClosesSubscribers(t *testing.T) {
	s, sub := newTestSession()
	s.broadcast([]byte("abc"))
	s.stop()

	got, closed := sub.take()
	if string(got) != "abc" || !closed {
		t.Errorf("take after stop = (%q, %v), want (\"abc\", true)", got, closed)
	}
}

// A client joining a running session starts with the session's recent
// output as a cushion, followed by everything broadcast after it joined.
func TestSubscribe_JoinerGetsRecentOutputFirst(t *testing.T) {
	s, _ := newTestSession()
	s.broadcast([]byte("old"))

	joiner, ok := s.subscribe()
	if !ok {
		t.Fatal("subscribe failed on an open session")
	}
	s.broadcast([]byte("new"))

	got, _ := joiner.take()
	if string(got) != "oldnew" {
		t.Errorf("joiner got %q, want %q", got, "oldnew")
	}
}

// The join prebuffer stays bounded, keeps the newest output, and always
// starts on an MPEG-TS packet boundary of the session's output.
func TestBroadcast_JoinPrebufferIsBoundedAndPacketAligned(t *testing.T) {
	s, _ := newTestSession()

	// Write a stream of numbered 188-byte packets in chunks that don't line
	// up with packet boundaries.
	var stream []byte
	for i := 0; len(stream) < 3*joinPrebufferSize; i++ {
		pkt := make([]byte, tsPacketSize)
		pkt[0] = 0x47
		pkt[1] = byte(i)
		pkt[2] = byte(i >> 8)
		pkt[3] = byte(i >> 16)
		stream = append(stream, pkt...)
	}
	for off := 0; off < len(stream); off += 5000 {
		s.broadcast(stream[off:min(off+5000, len(stream))])
	}

	if len(s.recent) > joinPrebufferSize {
		t.Errorf("prebuffer is %d bytes, want at most %d", len(s.recent), joinPrebufferSize)
	}
	if len(s.recent) < joinPrebufferSize-tsPacketSize {
		t.Errorf("prebuffer is %d bytes, want close to %d", len(s.recent), joinPrebufferSize)
	}
	if !bytes.HasSuffix(stream, s.recent) {
		t.Error("prebuffer is not the newest output")
	}
	if s.recentStart%tsPacketSize != 0 || s.recent[0] != 0x47 {
		t.Errorf("prebuffer starts at offset %d (first byte %#x), want a packet boundary", s.recentStart, s.recent[0])
	}
}
