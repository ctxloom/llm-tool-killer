package scm

import (
	"reflect"
	"testing"

	"github.com/spf13/afero"
)

const gitmodules = `[submodule "libs/foo"]
	path = libs/foo
	url = https://example.com/foo.git
[submodule "vendor/bar"]
	path = vendor/bar
	branch = main
`

// SubmodulePaths reads the path keys, ignoring url/branch and other noise.
func TestSubmodulePaths(t *testing.T) {
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "/repo/.gitmodules", []byte(gitmodules), 0o644); err != nil {
		t.Fatal(err)
	}

	got := SubmodulePaths(fs, "/repo")
	want := []string{"libs/foo", "vendor/bar"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SubmodulePaths = %v, want %v", got, want)
	}
}

// The lookup walks up from a subdirectory to the repo root's .gitmodules.
func TestSubmodulePathsWalksUp(t *testing.T) {
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "/repo/.gitmodules", []byte("[submodule \"x\"]\n\tpath = x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := SubmodulePaths(fs, "/repo/a/b/c"); !reflect.DeepEqual(got, []string{"x"}) {
		t.Fatalf("walk-up = %v, want [x]", got)
	}
}

// No .gitmodules anywhere → nil (the rule then matches nothing).
func TestSubmodulePathsNone(t *testing.T) {
	fs := afero.NewMemMapFs()
	if got := SubmodulePaths(fs, "/repo"); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}
