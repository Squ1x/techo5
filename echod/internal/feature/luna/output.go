package luna

import (
	"context"
	"sync"
)

// Output keeps slow speaker acquisition off the transport and capture threads.
// The shared gate bounds concurrent acquisitions, including across canceled turns.
// Acquire must return a short cancellation function; it may itself block.
type Output struct {
	ctx    context.Context
	cancel context.CancelFunc
	frames chan []int16
	mu     sync.Mutex
	stop   func()
}

func NewOutput(gate chan struct{}, acquire func(context.Context, <-chan []int16) func()) *Output {
	ctx, cancel := context.WithCancel(context.Background())
	o := &Output{ctx: ctx, cancel: cancel, frames: make(chan []int16, 3)}
	go func() {
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-gate }()
		if ctx.Err() != nil {
			return
		}
		stop := acquire(ctx, o.frames)
		o.mu.Lock()
		defer o.mu.Unlock()
		if ctx.Err() != nil {
			stop()
		} else {
			o.stop = stop
		}
	}()
	return o
}
func (o *Output) Play(pcm []int16) {
	if o.ctx.Err() != nil {
		return
	}
	select {
	case o.frames <- pcm:
	default:
	}
}

// Reset invalidates queued frames immediately, even while acquisition is blocked.
func (o *Output) Reset() {
	o.cancel()
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stop != nil {
		o.stop()
		o.stop = nil
	}
}
