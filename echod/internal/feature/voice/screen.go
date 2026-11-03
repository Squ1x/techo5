package voice

import (
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/hook"
)

// State is what a turn looks like from outside: which phase it is in and the words so far. It is
// what a screen shows — "Listening…", then what was heard, then the answer — and it is a hook
// rather than a query because a screen wants to redraw the moment something changes, not poll.
//
// Phase is the conversation's own name for it: idle, listening, thinking, replying. Heard and Reply
// are kept through the idle that follows a turn, so whoever shows them can let them linger.
type State struct {
	Phase string
	Heard string
	Reply string

	// FollowUp is listening again after a reply, which remains in Reply. Zero times mean no
	// countdown is available; producers must not invent a timeout.
	FollowUp        bool
	FollowUpStarted time.Time
	FollowUpUntil   time.Time

	// Activity contains at most two display-ready tool summaries. Producers supply known MDI
	// icon names and short human labels, never raw tool names, arguments, IDs or results.
	// Entries with empty Status are unused. The fixed array keeps State comparable and copies
	// summaries by value on publication. No backend summaries means no inferred activity.
	Activity [2]ToolActivity
}

// ToolActivity is a display summary supplied by a trusted adapter, not a tool payload.
type ToolActivity struct {
	Icon   string
	Label  string
	Status string // running, ok, missing, error
}

// Changed fires on every phase change and whenever a transcript or a reply arrives. It runs on the
// conversation's goroutine, so listeners must not block.
var Changed hook.Hook[State]
