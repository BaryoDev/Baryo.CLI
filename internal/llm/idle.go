// SPDX-License-Identifier: MIT

package llm

import (
	"io"
	"sync/atomic"
	"time"
)

// StreamIdleTimeout is how long a provider may send nothing before the stream
// is abandoned. Headers arrive before a model starts generating, so
// ResponseHeaderTimeout does not cover this: a provider that sends headers and
// then stalls would otherwise hang until the process is killed.
//
// The default is generous because a local model can be slow to produce its
// first token on a cold prefill.
var StreamIdleTimeout = 5 * time.Minute

// idleWatchdog cancels a stream when nothing arrives within timeout. Call Tick
// on every byte or event received.
type idleWatchdog struct {
	timer   *time.Timer
	timeout time.Duration
	fired   atomic.Bool
}

// newIdleWatchdog starts the clock. cancel must belong to a context derived from
// the caller's, not the caller's own: cancelling the caller's context would make
// the error event unsendable.
func newIdleWatchdog(timeout time.Duration, cancel func()) *idleWatchdog {
	w := &idleWatchdog{timeout: timeout}
	w.timer = time.AfterFunc(timeout, func() {
		w.fired.Store(true)
		cancel()
	})
	return w
}

// Tick resets the clock.
func (w *idleWatchdog) Tick() {
	if w != nil && w.timer != nil {
		w.timer.Reset(w.timeout)
	}
}

// Stop releases the timer. Safe to call more than once.
func (w *idleWatchdog) Stop() {
	if w != nil && w.timer != nil {
		w.timer.Stop()
	}
}

// TimedOut reports whether the watchdog fired, which tells a stream it gave up
// on apart from one the caller cancelled.
func (w *idleWatchdog) TimedOut() bool {
	return w != nil && w.fired.Load()
}

// idleReader applies an idleWatchdog to a stream body: every read that returns
// data resets the clock.
type idleReader struct {
	r io.Reader
	w *idleWatchdog
}

func newIdleReader(r io.Reader, timeout time.Duration, cancel func()) *idleReader {
	return &idleReader{r: r, w: newIdleWatchdog(timeout, cancel)}
}

func (ir *idleReader) Read(p []byte) (int, error) {
	n, err := ir.r.Read(p)
	if n > 0 {
		ir.w.Tick()
	}
	return n, err
}

func (ir *idleReader) Stop()          { ir.w.Stop() }
func (ir *idleReader) TimedOut() bool { return ir.w.TimedOut() }

// idleError is the message shown when the watchdog fires.
func idleError(timeout time.Duration) string {
	return "no data from the provider for " + timeout.String() + ", giving up"
}
