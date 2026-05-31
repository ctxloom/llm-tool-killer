// Command ltk is the llm-tool-killer CLI.
//
//	ltk evaluate            # the hook: read a payload on stdin, emit a decision
//	ltk manage install      # add the hook to the most relevant LLM config
//	ltk manage uninstall    # remove it
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags (package main), e.g.
//
//	-X main.Version=v1.2.3
//
// It defaults to "dev"; the justfile stamps it from versionator.
var Version = "dev"

func main() {
	root := &cobra.Command{
		Use:           "ltk",
		Short:         "Gate an LLM agent's shell commands via a pre-tool hook",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newEvaluateCmd(), newManageCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ltk:", err)
		os.Exit(1)
	}
}
