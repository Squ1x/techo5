package luna

import (
	"context"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestBlockedAcquisitionDoesNotBlockTransportOrStop(t *testing.T) {
	gate := make(chan struct{}, 1)
	entered, release, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var output *Output
	c, connections := testServer(t, Callbacks{
		Play: func(pcm []int16) {
			if output == nil {
				output = NewOutput(gate, func(ctx context.Context, frames <-chan []int16) func() {
					close(entered)
					<-release
					return func() { close(stopped) }
				})
			}
			output.Play(pcm)
		},
		Reset: func() {
			if output != nil {
				output.Reset()
			}
		},
	})
	conn := take(t, connections)
	sendState(t, conn, "sleeping", 0)
	eventually(t, c.Ready)
	c.Toggle()
	readCommand(t, conn, "wake")
	sendState(t, conn, "listening", 1)
	pcm := make([]byte, 960)
	pcm[0] = 1
	conn.WriteMessage(websocket.BinaryMessage, pcm)
	take(t, entered)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	// More packets than the incoming queue can hold must continue being consumed.
	for range 12 {
		conn.WriteMessage(websocket.BinaryMessage, pcm)
		time.Sleep(20 * time.Millisecond)
	}
	done := make(chan struct{})
	go func() { c.Audio(make([]int16, 320)); c.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("slow claim blocked Audio/Stop")
	}
	for {
		kind, b := readPacket(t, conn)
		if kind == websocket.TextMessage {
			if string(b) != `{"type":"stop"}` {
				t.Fatalf("unexpected command %s", b)
			}
			break
		}
	}
	sendState(t, conn, "sleeping", 1)
	eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return !c.blocked })
	select {
	case <-connections:
		t.Fatal("blocked claim overflowed transport/reconnected")
	default:
	}
	// A newer generation waits at the gate instead of acquiring in parallel.
	nextEntered := make(chan struct{})
	next := NewOutput(gate, func(context.Context, <-chan []int16) func() { close(nextEntered); return func() {} })
	defer next.Reset()
	select {
	case <-nextEntered:
		t.Fatal("concurrent speaker acquisitions")
	case <-time.After(20 * time.Millisecond):
	}
	// Release the simulated blocked driver and verify its canceled acquisition is disposed.
	close(release)
	take(t, stopped)
	take(t, nextEntered)
}
