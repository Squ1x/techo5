package luna

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testToken = "test-terminal-token-at-least-32-characters"

func testServer(t *testing.T, cb Callbacks) (*Client, <-chan *websocket.Conn) {
	t.Helper()
	connections := make(chan *websocket.Conn, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/terminal" || r.Header.Get("Authorization") != "Bearer "+testToken || r.Header.Get("Origin") != "http://"+r.Host {
			t.Error("wrong terminal handshake")
			http.Error(w, "forbidden", 403)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		t.Cleanup(func() { conn.Close() })
		connections <- conn
	}))
	t.Cleanup(srv.Close)
	c := New(Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http") + "/terminal", Token: testToken}, cb)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("Run did not stop")
		}
	})
	return c, connections
}
func take[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
		var zero T
		return zero
	}
}
func eventually(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not reached")
}
func sendState(t *testing.T, conn *websocket.Conn, state string, turn uint64) {
	t.Helper()
	if err := conn.WriteJSON(Event{Type: "state", State: state, Turn: turn}); err != nil {
		t.Fatal(err)
	}
}
func readPacket(t *testing.T, conn *websocket.Conn) (int, []byte) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	kind, b, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	return kind, b
}
func readCommand(t *testing.T, conn *websocket.Conn, want string) {
	t.Helper()
	kind, b := readPacket(t, conn)
	var e Event
	if kind != websocket.TextMessage || json.Unmarshal(b, &e) != nil || e.Type != want {
		t.Fatalf("want %s, got %d %q", want, kind, b)
	}
}

func TestTerminalHandshakeWakeFramesStop(t *testing.T) {
	played := make(chan []int16, 10)
	events := make(chan Event, 30)
	var resets atomic.Int32
	c, connections := testServer(t, Callbacks{Play: func(p []int16) { played <- p }, Event: func(e Event) { events <- e }, Reset: func() { resets.Add(1) }})
	conn := take(t, connections)
	// Premature wake must not be sent: the first command below must be the only wake.
	c.Toggle()
	c.Audio(make([]int16, 320))
	if c.Ready() || c.Busy() {
		t.Fatal("ready before initial sleeping")
	}
	sendState(t, conn, "sleeping", 0)
	eventually(t, c.Ready)
	c.Toggle()
	readCommand(t, conn, "wake")
	// Ignore stale playback and a queued sleeping state until the listening acknowledgement.
	conn.WriteMessage(websocket.BinaryMessage, make([]byte, 960))
	sendState(t, conn, "sleeping", 0)
	sendState(t, conn, "listening", 1)
	eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return !c.awaiting && c.turn == 1 })
	frame := make([]int16, 320)
	for i := range frame {
		frame[i] = int16(i - 160)
	}
	c.Audio(frame)
	kind, b := readPacket(t, conn)
	if kind != websocket.BinaryMessage || len(b) != 640 {
		t.Fatalf("wrong uplink frame %d / %d", kind, len(b))
	}
	for i, v := range frame {
		if int16(binary.LittleEndian.Uint16(b[2*i:])) != v {
			t.Fatal("PCM sign/endianness")
		}
	}
	conn.WriteJSON(Event{Type: "transcript", Text: "hello", Turn: 1})
	conn.WriteJSON(Event{Type: "assistant", Text: "world", Turn: 1})
	sendState(t, conn, "speaking", 1)
	pcm := make([]byte, 960)
	for i := 0; i < 480; i++ {
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(i-240)))
	}
	conn.WriteMessage(websocket.BinaryMessage, pcm)
	out := take(t, played)
	if len(out) != 1920 || out[0] != -240 || out[1] != -240 || out[4] != -239 || out[1919] != 239 {
		t.Fatalf("wrong playback conversion: %d samples", len(out))
	}
	for {
		e := take(t, events)
		if e.State == "speaking" {
			break
		}
	}
	before := resets.Load()
	c.Toggle()
	readCommand(t, conn, "stop")
	if c.Busy() || resets.Load() <= before {
		t.Fatal("stop did not discard playback immediately")
	}
	conn.WriteMessage(websocket.BinaryMessage, pcm)
	conn.WriteJSON(Event{Type: "assistant", Text: "stale", Turn: 1})
	sendState(t, conn, "processing", 1)
	sendState(t, conn, "sleeping", 1)
	eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return !c.blocked })
	for len(events) > 0 {
		e := take(t, events)
		if e.Text == "stale" || e.State == "processing" {
			t.Fatal("event after stop")
		}
	}
	select {
	case <-played:
		t.Fatal("playback after stop")
	default:
	}
	c.Toggle()
	readCommand(t, conn, "wake")
	sendState(t, conn, "listening", 2)
	eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.turn == 2 })
	conn.WriteJSON(Event{Type: "assistant", Text: "old turn", Turn: 1})
	conn.WriteJSON(Event{Type: "assistant", Text: "new turn", Turn: 2})
	for {
		e := take(t, events)
		if e.Text == "old turn" {
			t.Fatal("stale turn accepted")
		}
		if e.Text == "new turn" {
			break
		}
	}
}

func TestReconnectNeedsSleepingAndNewWake(t *testing.T) {
	c, connections := testServer(t, Callbacks{})
	conn := take(t, connections)
	sendState(t, conn, "sleeping", 7)
	eventually(t, c.Ready)
	c.Toggle()
	readCommand(t, conn, "wake")
	sendState(t, conn, "listening", 8)
	eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.turn == 8 })
	conn.Close()
	eventually(t, func() bool { return !c.Ready() })
	next := take(t, connections)
	if c.Busy() || c.Ready() {
		t.Fatal("room resumed across reconnect")
	}
	c.Toggle()
	c.Audio(make([]int16, 320))
	sendState(t, next, "sleeping", 8)
	eventually(t, c.Ready)
	c.Toggle()
	readCommand(t, next, "wake")
	sendState(t, next, "listening", 9)
	eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.turn == 9 })
	// Malformed playback must disconnect, not misalign the ALSA stream.
	next.WriteMessage(websocket.BinaryMessage, make([]byte, 958))
	take(t, connections)
	if c.Busy() || c.Ready() {
		t.Fatal("malformed PCM left room active")
	}
}

func TestIgnoredWakeAndStopRecoverDespiteContinuousPCM(t *testing.T) {
	c, connections := testServer(t, Callbacks{})
	conn := take(t, connections)
	sendState(t, conn, "sleeping", 0)
	eventually(t, c.Ready)
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-quit:
				return
			case <-tick.C:
				conn.SetWriteDeadline(time.Now().Add(time.Second))
				if conn.WriteMessage(websocket.BinaryMessage, make([]byte, 960)) != nil {
					return
				}
			}
		}
	}()
	defer func() { close(quit); <-done }()
	c.Toggle()
	readCommand(t, conn, "wake")
	// Server ignores wake (muted/other owner) and does not resend unchanged state.
	eventually(t, func() bool { return !c.Busy() })
	c.Toggle()
	readCommand(t, conn, "wake")
	c.Stop()
	readCommand(t, conn, "stop")
	// Stop can likewise leave state unchanged, but must not wedge blocked forever.
	eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return !c.blocked })
	c.Toggle()
	readCommand(t, conn, "wake")
}

func TestStateCallbacksMayQueryReadiness(t *testing.T) {
	c := New(Config{}, Callbacks{})
	c.cb.Event = func(Event) { c.Ready(); c.Busy() }
	done := make(chan bool, 1)
	go func() {
		ok := c.receive(packet{websocket.TextMessage, []byte(`{"type":"state","state":"sleeping","turn":0}`)})
		c.disconnected()
		done <- ok
	}()
	if !take(t, done) {
		t.Fatal("state rejected")
	}
}

func TestBoundedAudioAndValidation(t *testing.T) {
	c := New(Config{}, Callbacks{})
	c.ready = true
	c.active = true
	for range cap(c.audio) + 10 {
		c.Audio(make([]int16, 320))
	}
	if len(c.audio) != cap(c.audio) {
		t.Fatal("queue not bounded")
	}
	c.Audio(make([]int16, 319))
	if len(c.audio) != cap(c.audio) {
		t.Fatal("bad frame accepted")
	}
	c.StopIf(func() bool { return false })
	if !c.Busy() {
		t.Fatal("stale playback preemption stopped a later room")
	}
	c.StopIf(func() bool { return true })
	if c.Busy() {
		t.Fatal("current playback preemption did not stop room")
	}
	if len(c.audio) != 0 {
		t.Fatal("stop retained uplink")
	}
	for _, url := range []string{"http://host/terminal", "ws://host/other", "ws://user:secret@host/terminal", "ws://host/terminal?token=secret", "ws://host/terminal#secret"} {
		if (Config{URL: url, Token: testToken}).Validate() == nil {
			t.Errorf("accepted %s", url)
		}
	}
	if err := (Config{URL: "wss://host/terminal", Token: testToken}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Config{URL: "ws://host/terminal", Token: "secret"}).Validate(); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("token validation leaked credential")
	}
	if err := New(Config{}, Callbacks{}).Run(context.Background()); err != nil {
		t.Fatal("disabled client must not dial")
	}
	// Interpolation, stereo duplication, signed PCM and held final sample.
	out := Playback([]byte{0x00, 0x80, 0xff, 0x7f})
	want := []int16{-32768, -32768, 0, 0, 32767, 32767, 32767, 32767}
	for i, v := range want {
		if out[i] != v {
			t.Fatalf("sample %d: got %d want %d", i, out[i], v)
		}
	}
}
