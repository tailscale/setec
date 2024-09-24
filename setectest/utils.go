package setectest

import "time"

// NewFakeTicker constructs a test-controled instance of the [setec.Ticker]
// interface that can be used by tests to control the advancement of updates to
// a [setec.Store].
func NewFakeTicker() *Ticker {
	return &Ticker{ch: make(chan time.Time), done: make(chan struct{})}
}

// Ticker implements the [setec.Ticker] interface allowing a test to control
// the advancement of time to exercise polling.
type Ticker struct {
	ch   chan time.Time
	done chan struct{}
}

func (Ticker) Stop()                    {}
func (f Ticker) Chan() <-chan time.Time { return f.ch }
func (f *Ticker) Done()                 { f.done <- struct{}{} }

// Poll signals the ticker, then waits for Done to be invoked.
func (f *Ticker) Poll() {
	f.ch <- time.Now()
	<-f.done
}
