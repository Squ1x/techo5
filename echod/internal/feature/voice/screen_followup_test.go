package voice

import (
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/wakeword"
)

func TestFollowUpDisplayLifecycle(t *testing.T) {
	w := newWatcher(t, config.OnHA{})
	c := w.conversation
	got := shownStates(t)
	c.shown = State{Reply: "The last answer.", Heard: "old question"}
	if !c.start(nextTurn{followUp: true}) {
		t.Fatal("follow-up did not start")
	}
	s := (*got)[0]
	if s.Phase != "listening" || !s.FollowUp || s.Reply != "The last answer." || s.Heard != "" {
		t.Fatalf("first follow-up snapshot: %+v", s)
	}
	if s.FollowUpStarted != w.at || s.FollowUpUntil != w.at.Add(c.listenFor(nextTurn{followUp: true})) {
		t.Fatalf("follow-up timestamps: %+v", s)
	}
	w.at = w.at.Add(time.Second)
	c.turnEvent(event{kind: evSpeaking})
	s = (*got)[len(*got)-1]
	if s.FollowUpUntil != w.at.Add(wakeword.MaxListen(c.slot)) || s.FollowUpStarted != (*got)[0].FollowUpStarted {
		t.Fatalf("speech did not publish its extended deadline: %+v", s)
	}
	c.turnEvent(event{kind: evCancel})
	s = (*got)[len(*got)-1]
	if s.Phase != "idle" || s.FollowUp || !s.FollowUpStarted.IsZero() || !s.FollowUpUntil.IsZero() || s.Activity != [2]ToolActivity{} {
		t.Fatalf("cancel left follow-up state: %+v", s)
	}
}

func TestListeningDisplayResetAndExit(t *testing.T) {
	c := idleTurn(t)
	c.now = func() time.Time { return time.Unix(100, 0) }
	t.Cleanup(c.disarm)
	for _, followUp := range []bool{true, false} {
		c.enter(phaseIdle)
		c.shown = State{Reply: "last answer", Heard: "old question", Activity: [2]ToolActivity{{Label: "old activity", Status: "ok"}}}
		c.listenShown(nextTurn{followUp: followUp})
		c.enter(phaseListening)
		if c.shown.FollowUp != followUp || c.shown.Heard != "" || c.shown.Activity != [2]ToolActivity{} {
			t.Fatalf("listening reset: %+v", c.shown)
		}
		if !followUp && (c.shown.Reply != "" || !c.shown.FollowUpStarted.IsZero() || !c.shown.FollowUpUntil.IsZero()) {
			t.Fatalf("ordinary listening retained follow-up: %+v", c.shown)
		}
		c.enter(phaseThinking)
		if c.shown.FollowUp || !c.shown.FollowUpStarted.IsZero() || !c.shown.FollowUpUntil.IsZero() {
			t.Fatalf("thinking retained follow-up: %+v", c.shown)
		}
	}
}

func TestToolActivityPublicationIsBoundedAndIndependent(t *testing.T) {
	c := &conversation{}
	got := shownStates(t)
	for _, status := range []string{"running", "ok", "missing", "error"} {
		c.shown.Activity = [2]ToolActivity{{Icon: "lightbulb", Label: "Living room light", Status: status}, {Label: "unsafe unknown status", Status: "unknown"}}
		c.emitShown()
		s := (*got)[len(*got)-1]
		c.shown.Activity[0].Label = "changed"
		if len(s.Activity) != 2 || s.Activity[0].Label != "Living room light" || s.Activity[0].Status != status || s.Activity[1] != (ToolActivity{}) {
			t.Fatalf("invalid activity snapshot: %+v", s)
		}
	}
	c.enter(phaseIdle)
	if s := (*got)[len(*got)-1]; s.Activity != [2]ToolActivity{} {
		t.Fatalf("idle retained activity: %+v", s)
	}
}
