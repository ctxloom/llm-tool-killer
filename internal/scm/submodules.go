// Package scm reads the source-control metadata the hook needs to evaluate
// rules. Today that is just the set of git submodule paths declared in
// .gitmodules, used to expand a `path: ["@submodules"]` rule into a concrete
// directory pattern per submodule (see rules.Config.ExpandSubmodules).
package scm

import (
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// SubmodulePaths returns the submodule paths declared in the nearest .gitmodules
// found at or above startDir (the repo root's, in practice). It returns nil when
// there is no .gitmodules or it declares none. Paths are returned exactly as
// written — slash-separated and repo-relative.
func SubmodulePaths(fsys afero.Fs, startDir string) []string {
	dir := filepath.Clean(startDir)
	for {
		data, err := afero.ReadFile(fsys, filepath.Join(dir, ".gitmodules"))
		if err == nil {
			return parseSubmodulePaths(string(data))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil // reached the filesystem root without finding one
		}
		dir = parent
	}
}

// parseSubmodulePaths extracts the `path = …` values from a .gitmodules document.
// .gitmodules is git-config (INI) format; each [submodule "name"] stanza carries
// a `path` key. We read only those keys, which is all the rule expansion needs.
func parseSubmodulePaths(content string) []string {
	var paths []string
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		key, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "path" {
			continue
		}
		if p := strings.TrimSpace(val); p != "" {
			paths = append(paths, filepath.ToSlash(p))
		}
	}
	return paths
}
