//go:build unix

package resolve_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mkusaka/gthok/internal/cli"
)

func TestNormalizeNoResolveStartsNoGit(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", root)
	t.Setenv("GTHOK_CONFIG_DIR", root)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("PATH", root)
	t.Setenv("GTHOK_TEST_MARKER", filepath.Join(root, "started"))
	git := filepath.Join(root, "git")
	if err := os.WriteFile(git, []byte("#!/bin/sh\nprintf started > \"$GTHOK_TEST_MARKER\"\nexit 93\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GTHOK_REAL_GIT", git)
	output, err := os.CreateTemp(root, "output")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = output, output
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	if code := cli.Management([]string{"normalize", "--no-resolve", "--", "status"}, "test", "test"); code != 0 {
		data, _ := os.ReadFile(output.Name())
		t.Fatalf("normalize exited %d: %s", code, data)
	}
	if _, err := os.Stat(os.Getenv("GTHOK_TEST_MARKER")); !os.IsNotExist(err) {
		t.Fatalf("--no-resolve started Git: %v", err)
	}
}
