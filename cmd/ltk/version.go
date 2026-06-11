package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// versionInfo is the machine-readable shape of `ltk version --format json`.
// ctxloom probes it at boot to report companion versions, so the field set
// is a cross-binary contract (taskloom and ctxloom emit the same shape).
type versionInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func newVersionCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the ltk version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return printVersion(cmd.OutOrStdout(), format)
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "output format: text or json ({name, version})")
	return cmd
}

func printVersion(w io.Writer, format string) error {
	switch format {
	case "text":
		_, err := fmt.Fprintln(w, Version)
		return err
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(versionInfo{Name: progName, Version: Version})
	default:
		return fmt.Errorf("unknown format %q (supported: text, json)", format)
	}
}
