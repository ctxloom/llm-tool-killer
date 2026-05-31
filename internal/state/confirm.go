package state

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/afero"

	"github.com/ctxloom/llm-tool-killer/internal/engine"
)

// ConfirmByRepeat implements the "run it again to permit" override against the
// store at stateFile on fs. On the first denial of command it arms a pending
// entry and returns the denial with an added hint; if command is already armed
// (an identical re-run within the window) it clears the entry and returns an
// allow. The bool reports whether this call consumed a confirmation (allowed).
//
// Callers must only invoke this for a *confirmable* denial; an inviolate rule is
// gated out upstream (engine.Response.Confirmable), so repeating it never
// reaches here. now is injected so callers (and tests) control the clock. This
// is an escape hatch, not a security control.
func ConfirmByRepeat(fs afero.Fs, resp engine.Response, command, stateFile string, now time.Time, window time.Duration) (engine.Response, bool) {
	key := strings.TrimSpace(command)
	st := Open(fs, stateFile)
	if st.Armed(key, now) {
		st.Clear(key)
		_ = st.Save(now)
		return engine.Response{Allow: true}, true
	}
	st.Arm(key, now, window)
	_ = st.Save(now)
	resp.Reason = strings.TrimRight(resp.Reason, "\n") +
		fmt.Sprintf("\n\nIf you really mean it, run the exact same command again within %ds to proceed.", int(window.Seconds()))
	return resp, false
}
