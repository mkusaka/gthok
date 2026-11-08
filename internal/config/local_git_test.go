package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLocalTrackingSkipsWrapperAndFailsClosed(t *testing.T) {
	repo, env, _ := trustRepositoryFixture(t)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shim := t.TempDir()
	if err := os.Symlink(self, filepath.Join(shim, "git")); err != nil {
		t.Fatal(err)
	}
	env["PATH"] = shim + string(os.PathListSeparator) + filepath.Dir(real)
	env["GIT_CONFIG_NOSYSTEM"], env["GIT_CONFIG_GLOBAL"] = "1", "/dev/null"
	writeFixture(t, filepath.Join(repo, LocalName), "version: 1\n")
	if gitTracked(repo, LocalName, "", env) {
		t.Fatal("untracked local config was rejected")
	}
	trustGit(t, repo, "add", "-f", LocalName)
	if !gitTracked(repo, LocalName, "", env) {
		t.Fatal("tracked local config was trusted")
	}
	trustGit(t, repo, "rm", "--cached", LocalName)
	broken := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(broken, []byte("#!/bin/sh\nexit 2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if !gitTracked(repo, LocalName, broken, env) {
		t.Fatal("failed tracking query granted trust")
	}
	if !gitTracked(repo, LocalName, "/missing/git", env) {
		t.Fatal("missing Git granted trust")
	}
}
