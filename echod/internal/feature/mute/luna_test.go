package mute

import (
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/feature/luna"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/privacy"
)

type lunaPrivacyLine struct {
	privacy.Mute
	muted bool
}

func (l *lunaPrivacyLine) Get() (bool, error) { return l.muted, nil }

func TestLunaPrivacyAvailability(t *testing.T) {
	m := &Mute{}
	if muted, err := m.Muted(); muted || err != nil {
		t.Fatal("legacy missing-backend behavior changed")
	}
	if m.Available() || luna.MicAllowed(m) {
		t.Fatal("missing backend allowed Luna")
	}
	line := &lunaPrivacyLine{}
	m.line = line
	if !m.Available() || !m.CutHere() || !luna.MicAllowed(m) {
		t.Fatal("unmuted non-panel line blocked")
	}
	line.muted = true
	if luna.MicAllowed(m) {
		t.Fatal("muted non-panel line allowed")
	}
}
