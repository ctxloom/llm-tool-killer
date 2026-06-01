package main

import (
	"os"
	"path/filepath"
	"testing"
)

// scaffoldConfig must never silently clobber an existing rules file: without
// force it keeps the file (no backup); with force it backs the old one up first.
func TestScaffoldConfigPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".ltk", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	mine := "version: 1\nrules: []  # my edited rules\n"
	if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := scaffoldConfig(path, true, false); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != mine {
		t.Errorf("non-force scaffold overwrote the user's config:\n%s", got)
	}
	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Error("non-force scaffold should not write a .bak")
	}
}

func TestScaffoldConfigForceBacksUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".ltk", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	mine := "version: 1\nrules: []  # my edited rules\n"
	if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := scaffoldConfig(path, true, true); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("force scaffold should back up the old file: %v", err)
	}
	if string(bak) != mine {
		t.Errorf("backup does not match the original:\n%s", bak)
	}
	if got, _ := os.ReadFile(path); string(got) == mine {
		t.Error("force scaffold should have overwritten the config")
	}
}

func TestScaffoldConfigWritesWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".ltk", "config.yaml")
	if err := scaffoldConfig(path, false, false); err != nil { // minimal template
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("scaffold should create the file when absent: %v", err)
	}
}
