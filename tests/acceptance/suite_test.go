// Package acceptance runs the human-readable Gherkin features in features/
// against the real decision pipeline (app.Decide) via godog.
package acceptance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"github.com/benjaminabbitt/llm-tool-killer/internal/app"
	"github.com/benjaminabbitt/llm-tool-killer/internal/engine"
	"github.com/benjaminabbitt/llm-tool-killer/internal/ir"
	"github.com/benjaminabbitt/llm-tool-killer/internal/rules"
)

// world is the per-scenario state: the configured tool and the last decision.
type world struct {
	app  *app.App
	resp engine.Response
}

// projectRedirects builds a rule set from the Background table. Columns:
// "when the agent runs" | "it should instead" | "because".
func (w *world) projectRedirects(table *godog.Table) error {
	cfg := &rules.Config{}
	for _, row := range table.Rows[1:] { // skip the header row
		command := strings.TrimSpace(row.Cells[0].Value)
		instead := strings.TrimSpace(row.Cells[1].Value)
		because := strings.TrimSpace(row.Cells[2].Value)
		cfg.Rules = append(cfg.Rules, rules.Rule{
			ID:      command,
			Match:   rules.Match{Command: rules.CommandPattern(strings.Fields(command))},
			Message: because,
			Suggest: instead,
		})
	}
	w.app = app.New(cfg)
	return nil
}

func (w *world) theAgentRuns(command string) error {
	if w.app == nil {
		return fmt.Errorf("no project configured (missing Background)")
	}
	w.resp = w.app.Decide(context.Background(), engine.Request{
		ToolName: "Bash",
		Command:  command,
		Shell:    ir.ShellBash,
	})
	return nil
}

func (w *world) theCommandIsBlocked() error {
	if w.resp.Allow {
		return fmt.Errorf("expected the command to be blocked, but it was allowed")
	}
	return nil
}

func (w *world) theCommandIsAllowed() error {
	if !w.resp.Allow {
		return fmt.Errorf("expected the command to be allowed, but it was blocked: %s", w.resp.Message())
	}
	return nil
}

func (w *world) theAgentIsTold(text string) error {
	if !strings.Contains(w.resp.Message(), text) {
		return fmt.Errorf("expected the agent to be told %q, got %q", text, w.resp.Message())
	}
	return nil
}

func (w *world) theAgentIsPointedAt(suggestion string) error {
	if w.resp.Suggest != suggestion {
		return fmt.Errorf("expected the agent to be pointed at %q, got %q", suggestion, w.resp.Suggest)
	}
	return nil
}

// InitializeScenario binds steps to a fresh world for each scenario.
func InitializeScenario(sc *godog.ScenarioContext) {
	w := &world{}
	sc.Step(`^the project asks agents to use:$`, w.projectRedirects)
	sc.Step(`^the agent runs "([^"]*)"$`, w.theAgentRuns)
	sc.Step(`^the command is turned away$`, w.theCommandIsBlocked)
	sc.Step(`^the command is allowed$`, w.theCommandIsAllowed)
	sc.Step(`^the agent is told "([^"]*)"$`, w.theAgentIsTold)
	sc.Step(`^the agent is pointed at "([^"]*)"$`, w.theAgentIsPointedAt)
}

func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: InitializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("acceptance scenarios failed")
	}
}
