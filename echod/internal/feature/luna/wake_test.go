package luna

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func assertWakeCommand(t *testing.T, conn *websocket.Conn, kind, assistant string) {
	t.Helper()
	wireKind, data := readPacket(t, conn)
	var got map[string]string
	if wireKind != websocket.TextMessage || json.Unmarshal(data, &got) != nil || got["type"] != kind {
		t.Fatalf("unexpected command: %s", data)
	}
	wantFields := 1
	if assistant != "" {
		wantFields++
	}
	if got["assistant"] != assistant || len(got) != wantFields {
		t.Fatalf("unexpected assistant fields: %s", data)
	}
}

func TestWakeAssistantWireAndLegacyTouch(t *testing.T) {
	for _, assistant := range []string{"", "Luna", "Astra", "touch"} {
		t.Run(assistant, func(t *testing.T) {
			c, connections := testServer(t, Callbacks{})
			conn := take(t, connections)
			sendState(t, conn, "sleeping", 0)
			eventually(t, c.Ready)
			c.Wake("Galileo") // Display names/unknown identities must fail closed.
			if c.Busy() {
				t.Fatal("unknown assistant started a turn")
			}
			if assistant == "touch" {
				c.Toggle()
				assistant = ""
			} else {
				c.Wake(assistant)
			}
			assertWakeCommand(t, conn, "wake", assistant)
			// Touch still stops immediately, even inside the detector's window.
			c.Toggle()
			assertWakeCommand(t, conn, "stop", "")
		})
	}
}

func TestConcurrentWakeSlotsOnlyOpenOneTurn(t *testing.T) {
	c, connections := testServer(t, Callbacks{})
	conn := take(t, connections)
	sendState(t, conn, "sleeping", 0)
	eventually(t, c.Ready)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, assistant := range []string{"Luna", "Astra"} {
		wg.Add(1)
		go func(assistant string) {
			defer wg.Done()
			<-start
			c.Wake(assistant)
		}(assistant)
	}
	close(start)
	wg.Wait()
	if !c.Busy() {
		t.Fatal("second slot canceled the first wake")
	}
	_, data := readPacket(t, conn)
	var got command
	if json.Unmarshal(data, &got) != nil || got.Type != "wake" || (got.Assistant != "Luna" && got.Assistant != "Astra") {
		t.Fatalf("wrong winning wake: %s", data)
	}
	sendState(t, conn, "listening", 1)
	eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.turn == 1 && !c.awaiting
	})
	c.Wake("Astra") // A second hit after a fast server ack must also be ignored.
	if !c.Busy() {
		t.Fatal("ack allowed a duplicate slot to cancel the turn")
	}
	c.mu.Lock()
	c.lastDetection = time.Now().Add(-time.Second)
	c.mu.Unlock()
	c.Wake("Luna") // A genuinely later wake still stops, without assistant on stop.
	assertWakeCommand(t, conn, "stop", "")
}
