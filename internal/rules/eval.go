package rules

import "github.com/benjaminabbitt/llm-tool-killer/internal/ir"

// Decision is the result of evaluating a script against a config.
type Decision struct {
	Allowed bool
	Rule    *Rule            // the deny rule that fired, if any
	Command ir.SimpleCommand // the command that triggered a denial
	Reason  string           // human-facing explanation
	Suggest string           // suggested replacement command, if any
	// Confirmable reports whether this denial may be lifted by repeating the
	// command (the "confirm by repeating" override). ConfirmWindowSeconds is the
	// window for doing so. An inviolate rule yields Confirmable=false.
	Confirmable          bool
	ConfirmWindowSeconds int
}

// Evaluate matches every command in the script (nested included) against the
// rules in order. The first matching deny rule wins. A matching allow rule
// clears the current command without denying it; if nothing denies, the command
// is allowed.
func Evaluate(cfg *Config, script *ir.Script) Decision {
	if script == nil {
		return Decision{Allowed: true}
	}

	var decision Decision
	denied := false
	script.Walk(func(c ir.SimpleCommand) bool {
		for i := range cfg.Rules {
			r := &cfg.Rules[i]
			if !r.isEnabled() {
				continue // `enabled: false` keeps a rule in the file but inert
			}
			if !r.Match.matches(script.Shell, c) {
				continue
			}
			if r.action() == ActionDeny {
				repeatable, window := r.confirmPolicy(cfg.Defaults)
				decision = Decision{
					Allowed:              false,
					Rule:                 r,
					Command:              c,
					Reason:               r.Message,
					Suggest:              r.Suggest,
					Confirmable:          repeatable,
					ConfirmWindowSeconds: window,
				}
				denied = true
				return false // stop the walk; first deny wins
			}
			return true // explicit allow for this command; next command
		}
		return true
	})
	if denied {
		return decision
	}
	return Decision{Allowed: true}
}
