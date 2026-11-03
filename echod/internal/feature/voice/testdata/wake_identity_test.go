package luna

// Adapted from the independent review's missing-first-model reproduction.
// The runner supplies exact production activeWakeWords/Voice.open functions;
// config and Client are real, only hardware/native-model dependencies are stubbed.
import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	wake "WAKE_STUB_IMPORT"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/gorilla/websocket"
)

func TestReviewLoadedModelRoutesIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, first, second, loaded, want, entry string
		slot                                     int
	}{
		{"missing Odin compacts Galileo", "hey_odin", "galileo", "galileo", "Astra", "Woke", 0},
		{"missing Galileo compacts Odin", "galileo", "hey_odin", "hey_odin", "Luna", "WokeAt", 0},
		{"reload leaves physical slot hole", "hey_odin", "galileo", "galileo", "Astra", "open", 1},
		{"legacy caller has no model snapshot", "hey_odin", "galileo", "", "", "legacy", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			cfg := config.Defaults()
			cfg.Wake.Words = []config.WakeWord{{ID: tc.first}, {ID: tc.second}}
			cfg.Luna = config.Luna{URL: "ws://host/terminal", Token: "synthetic-owner-only-token-for-test", WakeAssistants: map[string]string{"hey_odin": "Luna", "galileo": "Astra"}}
			raw, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			config.Use(path)
			if tc.loaded != "" {
				active := activeWakeWords([]wake.Model{{ID: tc.loaded}}, 2)
				if len(active) != 1 || active[0] != tc.loaded {
					t.Fatalf("active=%v", active)
				}
			}
			c := New(Config{}, Callbacks{})
			c.receive(packet{kind: websocket.TextMessage, data: []byte(`{"type":"state","state":"sleeping","turn":0}`)})
			v := Voice{luna: c}
			// Real entry points must retain the engine's ID across a nearest wait too.
			switch tc.entry {
			case "Woke":
				v.Woke(tc.slot, tc.loaded)
			case "WokeAt":
				v.WokeAt(tc.slot, time.Now(), tc.loaded)
			case "legacy":
				v.Woke(tc.slot)
			default:
				v.open(tc.slot, true, time.Time{}, tc.loaded)
			}
			select {
			case cmd := <-c.commands:
				if cmd.Type != "wake" || cmd.Assistant != tc.want {
					t.Fatalf("wrong identity: %+v, want %s", cmd, tc.want)
				}
			default:
				t.Fatal("no wake")
			}
		})
	}
}
