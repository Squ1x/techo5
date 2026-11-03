// Package luna is the native PCM terminal transport. Hardware and UI stay in voice.
package luna

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Config struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

func (c Config) Enabled() bool { return c.URL != "" && c.Token != "" }
func (c Config) Validate() error {
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "/terminal" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "ws" && u.Scheme != "wss") || len(c.Token) < 32 {
		return errors.New("invalid Luna terminal URL or token (minimum 32 characters)")
	}
	return nil
}

// Event retains the wire event names; assistant text is a delta, not a full answer.
type Event struct {
	Type  string `json:"type"`
	State string `json:"state"`
	Text  string `json:"text"`
	Turn  uint64 `json:"turn"`
}
type command struct {
	Type      string `json:"type"`
	Assistant string `json:"assistant,omitempty"`
}

type packet struct {
	kind int
	data []byte
}

// Callbacks must be short. Play and Reset run under the session lock and must
// not call back into Client. Event runs outside it. Reset must discard queued audio.
type Callbacks struct {
	Event func(Event)
	Play  func([]int16)
	Reset func()
	// AllowAudio must be short and must not call Client. It gates history and writes.
	AllowAudio func() bool
}
type Client struct {
	config                           Config
	cb                               Callbacks
	mu                               sync.Mutex
	ready, active, blocked, awaiting bool
	lastWakeLog, lastStateLog        time.Time
	ackUntil                         time.Time
	turn                             uint64
	commands                         chan command
	lastDetection                    time.Time
	audio                            chan []byte
	pre                              [][]byte
	uplink                           uplinkStats
	lastUplinkLog                    time.Time
}

func New(c Config, cb Callbacks) *Client {
	return &Client{config: c, cb: cb, commands: make(chan command, 4), audio: make(chan []byte, 128)}
}
func (c *Client) Ready() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.ready }
func (c *Client) Busy() bool  { c.mu.Lock(); defer c.mu.Unlock(); return c.active }
func (c *Client) resetLocked() {
	c.logUplinkLocked()
	c.uplink = uplinkStats{}
	if c.cb.Reset != nil {
		c.cb.Reset()
	}
	c.clearCaptureLocked()
}
func (c *Client) clearCaptureLocked() {
	c.pre = nil
	for {
		select {
		case <-c.audio:
		default:
			return
		}
	}
}

// Wake carries a local detector's identity. Two slots can fire on the same
// utterance; match the detector's 800 ms refractory window without delaying touch.
func (c *Client) Wake(assistant string) {
	if assistant != "" && assistant != "Luna" && assistant != "Astra" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if !c.ready || c.blocked || now.Sub(c.lastDetection) < 800*time.Millisecond {
		return
	}
	c.lastDetection = now
	c.toggleLocked(assistant)
}

// Toggle is the existing action/touch button, independent of detector suppression.
func (c *Client) Toggle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toggleLocked("")
}

func (c *Client) toggleLocked(assistant string) {
	if !c.ready || c.blocked {
		return
	}
	name := "wake"
	if c.active {
		name = "stop"
		assistant = "" // Assistant selection is only meaningful on wake.
	}
	c.active, c.blocked, c.awaiting = name == "wake", name == "stop", name == "wake"
	c.ackUntil = time.Now().Add(2 * time.Second)
	pre := c.pre
	c.resetLocked()
	if name == "wake" && c.captureAllowedLocked() {
		for _, frame := range pre {
			c.audio <- frame // At most 12 frames into an empty 128-frame queue.
		}
	} else if name == "wake" {
		return
	}
	select {
	case c.commands <- command{Type: name, Assistant: assistant}:
	default:
		c.active = false
		c.blocked = true
	}
}
func (c *Client) Stop() { c.StopIf(nil) }

// StopIf guards a delayed speaker-preemption notification against a later reset.
// The predicate runs under the session lock and must not call back into Client.
func (c *Client) StopIf(stillCurrent func() bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if stillCurrent != nil && !stillCurrent() {
		return
	}
	c.stopLocked()
}
func (c *Client) stopLocked() {
	if !c.active {
		c.clearCaptureLocked() // Privacy must also discard sleeping history.
		return
	}
	c.active = false
	c.blocked = true
	c.awaiting = false
	c.ackUntil = time.Now().Add(2 * time.Second)
	c.resetLocked()
	select {
	case c.commands <- command{Type: "stop"}:
	default:
	}
}

// Audio accepts the existing post-AEC 20 ms mono frame without waiting on the network.
func (c *Client) Audio(pcm []int16) {
	if len(pcm) != 320 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ready || c.blocked || !c.captureAllowedLocked() {
		return
	}
	b := make([]byte, 640)
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(v))
	}
	if !c.active {
		// 240 ms matches mic.History (~250 ms): retain sentence onset, not
		// the whole wake phrase. Shifting twelve pointers is cheaper than a ring API.
		if len(c.pre) == 12 {
			copy(c.pre, c.pre[1:])
			c.pre = c.pre[:11]
		}
		c.pre = append(c.pre, b)
		return
	}
	select {
	case c.audio <- b: // Includes frames captured while awaiting the wake ack.
	default:
		c.uplink.dropped++
	}
}
func (c *Client) captureAllowedLocked() bool {
	if c.cb.AllowAudio != nil && !c.cb.AllowAudio() {
		c.stopLocked()
		return false
	}
	return true
}
func (c *Client) nextAudio() ([]byte, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.captureAllowedLocked() || !c.active || c.awaiting || c.blocked {
		return nil, c.turn
	}
	select {
	case b := <-c.audio:
		return b, c.turn
	default:
		return nil, c.turn
	}
}
func (c *Client) disconnected() {
	c.mu.Lock()
	defer func() {
		c.mu.Unlock()
		if c.cb.Event != nil {
			c.cb.Event(Event{Type: "state", State: "sleeping"})
		}
	}()
	c.ready = false
	c.active = false
	c.blocked = false
	c.awaiting = false
	c.ackUntil = time.Time{}
	c.resetLocked() // Attribute the final uplink summary to the disconnected turn.
	c.turn = 0
	for {
		select {
		case <-c.commands:
		default:
			return
		}
	}
}

// Run reconnects without resuming a canceled room. Errors deliberately contain no URL/token.
func (c *Client) Run(ctx context.Context) error {
	if !c.config.Enabled() {
		return nil
	}
	if err := c.config.Validate(); err != nil {
		return err
	}
	backoff := 250 * time.Millisecond
	for ctx.Err() == nil {
		started := time.Now()
		c.session(ctx)
		c.disconnected()
		if time.Since(started) > 10*time.Second {
			backoff = 250 * time.Millisecond
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		backoff = min(10*time.Second, backoff*2)
	}
	return nil
}
func (c *Client) session(ctx context.Context) {
	u, _ := url.Parse(c.config.URL)
	scheme := "http"
	if u.Scheme == "wss" {
		scheme = "https"
	}
	dial := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, resp, err := dial.DialContext(ctx, c.config.URL, http.Header{"Authorization": []string{"Bearer " + c.config.Token}, "Origin": []string{scheme + "://" + u.Host}})
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(65536)
	incoming := make(chan packet, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			// ReadDeadline belongs to the reader, like ReadMessage itself.
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			kind, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			select {
			case incoming <- packet{kind, data}:
			default:
				conn.Close()
				return
			}
		}
	}()
	defer func() { conn.Close(); <-done }()
	write := func(kind int, b []byte) bool {
		conn.SetWriteDeadline(time.Now().Add(time.Second))
		return conn.WriteMessage(kind, b) == nil
	}
	ackTick := time.NewTicker(50 * time.Millisecond)
	defer ackTick.Stop()
	audioTick := time.NewTicker(20 * time.Millisecond)
	defer audioTick.Stop()
	for {
		select {
		case now := <-ackTick.C:
			c.expireAck(now)
		case <-ctx.Done():
			return
		case <-done:
			return
		case cmd := <-c.commands:
			b, _ := json.Marshal(cmd)
			if !write(websocket.TextMessage, b) {
				return
			}
			if cmd.Type == "wake" && time.Since(c.lastWakeLog) >= time.Second {
				c.lastWakeLog = time.Now()
				slog.Info("Luna wake sent")
			}
		case <-audioTick.C:
			// The server consumes one frame/tick and rejects bursts over ten frames.
			// Drain history/ack-buffer before live PCM at exactly the normal cadence.
			b, turn := c.nextAudio()
			if b != nil {
				if !write(websocket.BinaryMessage, b) {
					return
				}
				c.mu.Lock()
				if c.active && !c.blocked && c.turn == turn {
					c.uplink.add(b)
				}
				c.mu.Unlock()
			}
		case p := <-incoming:
			if !c.receive(p) {
				return
			}
		}
	}
}

// PCM is a transport heartbeat, not an acknowledgement of wake/stop. The server
// may decline a command without emitting another unchanged sleeping state.
func (c *Client) expireAck(now time.Time) {
	c.mu.Lock()
	if c.ackUntil.IsZero() || now.Before(c.ackUntil) {
		c.mu.Unlock()
		return
	}
	c.ackUntil = time.Time{}
	c.active, c.blocked, c.awaiting = false, false, false
	c.resetLocked()
	for len(c.commands) > 0 {
		<-c.commands
	}
	turn := c.turn
	c.mu.Unlock()
	if c.cb.Event != nil {
		c.cb.Event(Event{Type: "state", State: "sleeping", Turn: turn})
	}
}

func (c *Client) receive(p packet) bool {
	var notify *Event
	c.mu.Lock()
	defer func() {
		c.mu.Unlock()
		if notify != nil && c.cb.Event != nil {
			c.cb.Event(*notify)
		}
	}()
	if p.kind == websocket.BinaryMessage {
		if len(p.data) != 960 {
			return false
		}
		if c.active && !c.blocked && !c.awaiting && c.cb.Play != nil {
			c.cb.Play(Playback(p.data))
		}
		return true
	}
	if p.kind != websocket.TextMessage {
		return false
	}
	var e Event
	if json.Unmarshal(p.data, &e) != nil {
		return false
	}
	if e.Type == "state" && e.State == "sleeping" {
		if c.awaiting || e.Turn < c.turn {
			return true
		}
		c.ackUntil = time.Time{}
		c.ready = true
		c.active = false
		c.blocked = false
		c.turn = e.Turn
		c.resetLocked()
	} else {
		if !c.active || c.blocked || e.Turn < c.turn {
			return true
		}
		wakeAck := c.awaiting
		if c.awaiting {
			if e.Type != "state" || e.State != "listening" || e.Turn <= c.turn {
				return true
			}
			c.awaiting = false
			c.ackUntil = time.Time{}
		}
		if e.Turn > c.turn {
			// A wake ack must NOT discard the pre-roll/ack-buffer it unlocks.
			if !wakeAck {
				c.resetLocked()
			}
			c.turn = e.Turn
		}
	}
	if e.Type == "state" && e.State == "processing" {
		c.logUplinkLocked()
	}
	if e.Type == "state" && time.Since(c.lastStateLog) >= 250*time.Millisecond {
		switch e.State {
		case "sleeping", "listening", "processing", "speaking", "followup", "ringing":
			c.lastStateLog = time.Now()
			slog.Info("Luna state received", "state", e.State, "turn", e.Turn)
		}
	}
	notify = &e
	return true
}

// Playback uses fixed 2x linear interpolation and duplicates mono to 48 kHz stereo.
// ponytail: hold the final sample at frame edges; use a streaming FIR if listening tests require it.
func Playback(pcm []byte) []int16 {
	out := make([]int16, len(pcm)*2)
	for i := 0; i < len(pcm)/2; i++ {
		a := int16(binary.LittleEndian.Uint16(pcm[2*i:]))
		b := a
		if 2*i+3 < len(pcm) {
			b = int16(binary.LittleEndian.Uint16(pcm[2*i+2:]))
		}
		j := i * 4
		out[j] = a
		out[j+1] = a
		out[j+2] = int16((int32(a) + int32(b)) / 2)
		out[j+3] = out[j+2]
	}
	return out
}
