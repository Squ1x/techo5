package detect

import (
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/wake"
)

func TestDetectionCarriesLoadedModelIdentity(t *testing.T) {
	made := fakes(t)
	e := New(2, nil)
	defer e.Close()
	if err := e.Use(1, model("galileo", wake.KindMicroWakeWord)); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	got := make(chan string, 1)
	e.OnDetect = func(slot int, id string) {
		close(entered)
		<-release
		if slot != 1 {
			id = "wrong slot"
		}
		got <- id
	}
	made[wake.KindMicroWakeWord].loaded["galileo"] = 0.99
	e.score([]int16{0}, nil)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("no detection")
	}
	if err := e.Use(1, model("hey_odin", wake.KindMicroWakeWord)); err != nil {
		t.Fatal(err)
	}
	// The already captured ID must survive a slot replacement while routing waits.
	release <- struct{}{}
	select {
	case id := <-got:
		if id != "galileo" {
			t.Fatalf("captured wrong model: %s", id)
		}
	case <-time.After(time.Second):
		t.Fatal("callback did not finish")
	}
}
