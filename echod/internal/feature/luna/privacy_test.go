package luna

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
)

// Non-panel lines have CutHere=true regardless of whether they are currently muted.
type nonPanelMute struct{ muted, failed, missing atomic.Bool }

func (*nonPanelMute) CutHere() bool     { return true }
func (m *nonPanelMute) Available() bool { return !m.missing.Load() }
func (m *nonPanelMute) Muted() (bool, error) {
	if m.failed.Load() {
		return false, errors.New("privacy unavailable")
	}
	return m.muted.Load(), nil
}

func TestNonPanelCurrentMuteAllowsWakeAndPCM(t *testing.T) {
	line := &nonPanelMute{}
	if !line.CutHere() || !MicAllowed(line) {
		t.Fatal("unmuted non-panel line incorrectly blocked")
	}
	played := make(chan []int16, 4)
	c, connections := testServer(t, Callbacks{AllowAudio: func() bool { return MicAllowed(line) }, Play: func(p []int16) {
		if MicAllowed(line) {
			played <- p
		}
	}})
	conn := take(t, connections)
	sendState(t, conn, "sleeping", 0)
	eventually(t, c.Ready)
	wake := func() {
		if MicAllowed(line) {
			c.Toggle()
		}
	}
	audio := func() {
		if MicAllowed(line) {
			c.Audio(make([]int16, 320))
		} else {
			c.Stop()
		}
	}
	wake()
	readCommand(t, conn, "wake")
	sendState(t, conn, "listening", 1)
	eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return !c.awaiting })
	audio()
	kind, b := readPacket(t, conn)
	if kind != websocket.BinaryMessage || len(b) != 640 {
		t.Fatal("unmuted PCM did not flow")
	}
	conn.WriteMessage(websocket.BinaryMessage, make([]byte, 960))
	take(t, played)
	line.muted.Store(true)
	if MicAllowed(line) {
		t.Fatal("muted line allowed")
	}
	audio()
	readCommand(t, conn, "stop")
	sendState(t, conn, "sleeping", 1)
	eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return !c.blocked })
	wake()
	audio()
	if c.Busy() || len(c.audio) != 0 {
		t.Fatal("muted wake/PCM passed guard")
	}
	line.muted.Store(false)
	line.failed.Store(true)
	if MicAllowed(line) {
		t.Fatal("privacy error failed open")
	}
	wake()
	audio()
	if c.Busy() {
		t.Fatal("privacy error enabled wake")
	}
	line.failed.Store(false)
	line.missing.Store(true)
	if muted, err := line.Muted(); muted || err != nil {
		t.Fatal("regression requires Muted's existing (false,nil) fallback")
	}
	if MicAllowed(line) {
		t.Fatal("missing privacy backend failed open")
	}
	wake()
	audio()
	if c.Busy() || len(c.audio) != 0 {
		t.Fatal("missing backend enabled wake/PCM")
	}
}

// Older/unknown providers without an availability contract must also fail closed.
type unknownPrivacy struct{}

func (unknownPrivacy) Muted() (bool, error) { return false, nil }
func TestUnknownPrivacyFailsClosed(t *testing.T) {
	if MicAllowed(unknownPrivacy{}) {
		t.Fatal("unknown privacy provider allowed Luna")
	}
}
