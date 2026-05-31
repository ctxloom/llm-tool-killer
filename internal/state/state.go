// Package state persists short-lived "confirm by repeating" tokens. When a
// command is denied, ltk remembers it briefly; running the exact same command
// again within the window counts as an explicit override and is allowed.
//
// This is an escape hatch, not a security control: a determined agent can just
// repeat the command. It exists so a human (or a deliberate agent) can get past
// a cooperative nudge without editing config, while every override is visible.
// Rules marked non-confirmable are never armed here, so repeating them never
// helps — see internal/rules.
//
// All file access goes through an afero.Fs so the store is testable against an
// in-memory filesystem.
package state

import (
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/spf13/afero"
)

// Store is a tiny on-disk map of command → unix expiry. It is best-effort and
// not concurrency-safe; hooks run effectively one at a time.
type Store struct {
	fs      afero.Fs
	path    string
	Pending map[string]int64 `json:"pending"` // command → unix expiry seconds
}

// Open loads the store at path from fs. A missing or corrupt file yields an
// empty store (best-effort: an unreadable state file must never break
// evaluation). A nil fs defaults to the real OS filesystem.
func Open(fs afero.Fs, path string) *Store {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	s := &Store{fs: fs, path: path, Pending: map[string]int64{}}
	if b, err := afero.ReadFile(fs, path); err == nil {
		_ = json.Unmarshal(b, s)
		if s.Pending == nil {
			s.Pending = map[string]int64{}
		}
	}
	return s
}

// Armed reports whether cmd has an unexpired pending entry — i.e. it was denied
// recently and a repeat now should be allowed.
func (s *Store) Armed(cmd string, now time.Time) bool {
	exp, ok := s.Pending[cmd]
	return ok && now.Unix() < exp
}

// Arm records cmd as pending until now+window, so the next identical command
// within the window is treated as a confirmation.
func (s *Store) Arm(cmd string, now time.Time, window time.Duration) {
	s.Pending[cmd] = now.Add(window).Unix()
}

// Clear removes cmd's pending entry (call after consuming a confirmation).
func (s *Store) Clear(cmd string) { delete(s.Pending, cmd) }

// Save prunes expired entries and writes the store, creating its directory.
func (s *Store) Save(now time.Time) error {
	for c, exp := range s.Pending {
		if now.Unix() >= exp {
			delete(s.Pending, c)
		}
	}
	if err := s.fs.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return afero.WriteFile(s.fs, s.path, b, 0o644)
}
