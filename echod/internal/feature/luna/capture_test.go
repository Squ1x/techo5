package luna

import (
	"bytes"
	"encoding/binary"
	"log/slog"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func captureFrame(n int16) []int16 {
	p := make([]int16, 320)
	for i := range p {
		p[i] = n
	}
	return p
}
func captureAck(c *Client) {
	c.receive(packet{websocket.TextMessage, []byte(`{"type":"state","state":"listening","turn":1}`)})
}

func TestHistoryAckAndLiveOrderOnWire(t *testing.T) {
	c, connections := testServer(t, Callbacks{})
	conn := take(t, connections)
	sendState(t, conn, "sleeping", 0)
	eventually(t, c.Ready)
	for i := int16(1); i <= 20; i++ {
		c.Audio(captureFrame(i))
	}
	c.Toggle()
	readCommand(t, conn, "wake")
	c.Audio(captureFrame(21))
	c.Audio(captureFrame(22))
	if b, _ := c.nextAudio(); b != nil {
		t.Fatal("audio before ack")
	}
	sendState(t, conn, "listening", 1)
	eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return !c.awaiting })
	c.Audio(captureFrame(23))
	start := time.Now()
	for i := int16(9); i <= 23; i++ {
		kind, b := readPacket(t, conn)
		if kind != websocket.BinaryMessage || len(b) != 640 {
			t.Fatal("bad frame")
		}
		for k := 0; k < len(b); k += 2 {
			if int16(binary.LittleEndian.Uint16(b[k:])) != i {
				t.Fatalf("order/sample: want %d", i)
			}
		}
	}
	if time.Since(start) < 200*time.Millisecond {
		t.Fatal("pre-roll burst would overrun server input queue")
	}
	eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.uplink.bytes == 15*640 })
}

func TestCaptureBoundedAndDiscarded(t *testing.T) {
	for _, cause := range []string{"stop", "timeout", "reconnect"} {
		t.Run(cause, func(t *testing.T) {
			c := New(Config{}, Callbacks{})
			c.ready = true
			for range 40 {
				c.Audio(captureFrame(1))
			}
			if len(c.pre) != 12 {
				t.Fatal("history not bounded")
			}
			c.Toggle()
			for range 300 {
				c.Audio(captureFrame(2))
			}
			if len(c.audio) != cap(c.audio) || c.uplink.dropped == 0 {
				t.Fatal("ack queue not bounded")
			}
			switch cause {
			case "stop":
				c.Stop()
			case "timeout":
				c.expireAck(time.Now().Add(3 * time.Second))
			case "reconnect":
				c.disconnected()
			}
			if len(c.pre) != 0 || len(c.audio) != 0 {
				t.Fatal("old capture survived", cause)
			}
			// Mimic a new sleeping state, then a fresh wake: only fresh history may survive.
			c.receive(packet{websocket.TextMessage, []byte(`{"type":"state","state":"sleeping","turn":0}`)})
			c.Audio(captureFrame(3))
			c.Toggle()
			captureAck(c)
			b, _ := c.nextAudio()
			if len(b) != 640 || int16(binary.LittleEndian.Uint16(b)) != 3 {
				t.Fatal("fresh wake reused old capture", cause)
			}
		})
	}
}

func TestPrivacyClearsSleepingAndAwaitingCapture(t *testing.T) {
	var allowed atomic.Bool
	allowed.Store(true)
	c := New(Config{}, Callbacks{AllowAudio: allowed.Load})
	c.ready = true
	c.Audio(captureFrame(1))
	allowed.Store(false)
	c.Audio(captureFrame(2))
	if len(c.pre) != 0 {
		t.Fatal("muted sleeping history retained")
	}
	allowed.Store(true)
	c.Audio(captureFrame(3))
	c.Stop() // Mute event also clears idle history.
	if len(c.pre) != 0 {
		t.Fatal("idle Stop retained history")
	}
	c.Audio(captureFrame(4))
	c.Toggle()
	c.Audio(captureFrame(5))
	allowed.Store(false)
	captureAck(c)
	if b, _ := c.nextAudio(); b != nil || len(c.audio) != 0 || len(c.pre) != 0 {
		t.Fatal("privacy change allowed queued audio")
	}
	allowed.Store(true)
	c.Audio(captureFrame(6))
	if len(c.pre) != 0 || len(c.audio) != 0 {
		t.Fatal("stopped room accepted capture before sleeping")
	}
}

func TestDisconnectSummaryRetainsTurn(t *testing.T) {
	var log bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	defer slog.SetDefault(previous)
	c := New(Config{}, Callbacks{})
	c.turn = 42
	c.uplink.add(make([]byte, 640))
	c.disconnected()
	if !strings.Contains(log.String(), "turn=42") || !strings.Contains(log.String(), "bytes=640") {
		t.Fatal("disconnect summary lost turn attribution", log.String())
	}
	if c.turn != 0 {
		t.Fatal("disconnect did not clear turn")
	}
}

func TestUplinkNumericRMS(t *testing.T) {
	var s uplinkStats
	b := make([]byte, 640)
	for i := 0; i < len(b); i += 2 {
		binary.LittleEndian.PutUint16(b[i:], uint16(1000))
	}
	s.add(b)
	s.add(b)
	if s.bytes != 1280 || math.Sqrt(s.squares/float64(s.bytes/2)) != 1000 {
		t.Fatal("numeric uplink summary wrong")
	}
}
