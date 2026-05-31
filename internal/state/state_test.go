package state

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
)

func TestArmConfirmAndExpiry(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := filepath.Join(".ltk", "state.json")
	now := time.Unix(1_000_000, 0)

	s := Open(fs, path)
	if s.Armed("go test", now) {
		t.Fatal("nothing armed yet")
	}
	s.Arm("go test", now, 30*time.Second)
	if err := s.Save(now); err != nil {
		t.Fatal(err)
	}

	// Survives a process boundary (reloaded from the same fs).
	s2 := Open(fs, path)
	if !s2.Armed("go test", now.Add(10*time.Second)) {
		t.Error("should be armed within the window")
	}
	if s2.Armed("go build", now.Add(10*time.Second)) {
		t.Error("only the exact command is armed")
	}
	if s2.Armed("go test", now.Add(31*time.Second)) {
		t.Error("should have expired after the window")
	}

	s2.Clear("go test")
	if s2.Armed("go test", now) {
		t.Error("cleared entry must not be armed")
	}
}

func TestSavePrunesExpired(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "state.json"
	now := time.Unix(2_000_000, 0)

	s := Open(fs, path)
	s.Arm("old", now, 10*time.Second)
	if err := s.Save(now.Add(20 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if Open(fs, path).Armed("old", now.Add(20*time.Second)) {
		t.Error("expired entry should be pruned on save")
	}
}

func TestOpenMissingFileIsEmpty(t *testing.T) {
	s := Open(afero.NewMemMapFs(), "nope.json")
	if s.Armed("anything", time.Unix(1, 0)) {
		t.Error("missing file must yield an empty store")
	}
}
