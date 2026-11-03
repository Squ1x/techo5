package voice

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/luna"
	"github.com/gorilla/websocket"
)

// No mic, speaker or display device is opened. This checks the actual transport
// event -> existing voice.Changed integration used by the display.
func TestLunaFeedsExistingVoiceState(t *testing.T) {
	accepted := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		accepted <- conn
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateChecked(func(c *config.Config) error {
		c.Luna = config.Luna{URL: "ws" + strings.TrimPrefix(srv.URL, "http") + "/terminal", Token: "test-luna-voice-token-longer-than-32-characters"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	config.Use(path)
	states := make(chan State, 30)
	unlisten := Changed.Listen(func(s State) { states <- s })
	defer unlisten()
	client := newLuna()
	if client == nil {
		t.Fatal("configured backend not selected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("client did not stop")
		}
	}()
	var conn *websocket.Conn
	select {
	case conn = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("no connection")
	}
	defer conn.Close()
	next := func() State {
		t.Helper()
		select {
		case s := <-states:
			return s
		case <-time.After(3 * time.Second):
			t.Fatal("no voice state")
			return State{}
		}
	}
	send := func(e luna.Event) {
		t.Helper()
		if err := conn.WriteJSON(e); err != nil {
			t.Fatal(err)
		}
	}
	send(luna.Event{Type: "state", State: "sleeping"})
	if next().Phase != "idle" {
		t.Fatal("sleeping not idle")
	}
	client.Toggle()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var wake luna.Event
	if err := conn.ReadJSON(&wake); err != nil || wake.Type != "wake" {
		t.Fatalf("wake: %v %v", wake, err)
	}
	send(luna.Event{Type: "state", State: "listening", Turn: 1})
	if next().Phase != "listening" {
		t.Fatal("listening not mapped")
	}
	send(luna.Event{Type: "transcript", Text: "Guten Morgen", Turn: 1})
	if next().Heard != "Guten Morgen" {
		t.Fatal("transcript missing")
	}
	send(luna.Event{Type: "state", State: "processing", Turn: 1})
	if next().Phase != "thinking" {
		t.Fatal("processing not thinking")
	}
	send(luna.Event{Type: "state", State: "speaking", Turn: 1})
	if next().Phase != "replying" {
		t.Fatal("speaking not replying")
	}
	send(luna.Event{Type: "assistant", Text: "Guten ", Turn: 1})
	next()
	send(luna.Event{Type: "assistant", Text: "Morgen!", Turn: 1})
	if next().Reply != "Guten Morgen!" {
		t.Fatal("assistant delta not accumulated")
	}
	send(luna.Event{Type: "state", State: "followup", Turn: 1})
	if next().Phase != "listening" {
		t.Fatal("follow-up not listening")
	}
	send(luna.Event{Type: "state", State: "sleeping", Turn: 1})
	if next().Phase != "idle" {
		t.Fatal("sleeping not idle")
	}
	if got := boundedLunaText("äöü", 3); got != "ä" || !utf8.ValidString(got) {
		t.Fatal("text limit split UTF-8")
	}
}
