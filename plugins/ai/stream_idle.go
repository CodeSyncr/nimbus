package ai

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// streamIdleLimit is how long a streamed reply may send nothing at all
// before it is given up as dead. Providers keep a live stream warm (Anthropic
// with pings) through the longest thinking, so silence this long is a
// connection that has gone without the socket noticing: a proxy that stopped
// forwarding, a network that changed underneath it.
var streamIdleLimit = 180 * time.Second

// idleReader is a response body that closes itself, and says why, when
// nothing has been read from it for the limit. Without it a read on a
// half-open connection waits for as long as the request's context lives,
// which for a long agent turn is as good as for ever.
type idleReader struct {
	rc      io.ReadCloser
	limit   time.Duration
	timer   *time.Timer
	stalled atomic.Bool
}

func newIdleReader(rc io.ReadCloser, limit time.Duration) *idleReader {
	r := &idleReader{rc: rc, limit: limit}
	r.timer = time.AfterFunc(limit, func() {
		r.stalled.Store(true)
		_ = rc.Close() // unblocks the read below
	})
	return r
}

func (r *idleReader) Read(p []byte) (int, error) {
	n, err := r.rc.Read(p)
	if n > 0 {
		r.timer.Reset(r.limit)
	}
	if err != nil && r.stalled.Load() {
		// worded so callers that retry a dropped line recognise it
		err = fmt.Errorf("ai: the stream stalled: nothing arrived for %s (i/o timeout)", r.limit)
	}
	return n, err
}

func (r *idleReader) Close() error {
	r.timer.Stop()
	return r.rc.Close()
}
