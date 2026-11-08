//go:build unix

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/gitargs"
	"github.com/mkusaka/gthok/internal/model"
)

func gitContextFixture(t *testing.T) (string, string, map[string]string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"HOME": root, "PATH": filepath.Dir(real) + ":/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "GTHOK_CONFIG_DIR": filepath.Join(root, "config")}
	return real, root, env
}
func contextGit(t *testing.T, real, cwd string, env map[string]string, args ...string) {
	t.Helper()
	cmd := exec.Command(real, args...)
	cmd.Dir, cmd.Env = cwd, environmentList(env)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %q: %v\n%s", args, err, out)
	}
}

func TestEffectiveConfigRepository(t *testing.T) {
	real, root, env := gitContextFixture(t)
	repo, other, bare := filepath.Join(root, "repo"), filepath.Join(root, "other"), filepath.Join(root, "bare.git")
	for _, dir := range []string{repo, other} {
		contextGit(t, real, root, env, "init", "-q", dir)
	}
	contextGit(t, real, root, env, "init", "-q", "--bare", bare)
	if err := os.Mkdir(filepath.Join(other, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gthok.yaml"), []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		args      []string
		overrides map[string]string
		want      string
	}{
		{name: "cwd", want: repo},
		{name: "chdir-chain", args: []string{"-C", other, "-C", "sub", "status"}, want: other},
		{name: "git-dir", args: []string{"--git-dir", filepath.Join(other, ".git"), "status"}, want: other},
		{name: "work-tree", args: []string{"--git-dir=" + filepath.Join(repo, ".git"), "--work-tree=" + other, "status"}, want: other},
		{name: "env", overrides: map[string]string{"GIT_DIR": filepath.Join(other, ".git"), "GIT_WORK_TREE": other}, want: other},
		{name: "bare", args: []string{"-C", bare, "--bare", "status"}, want: bare},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := make(map[string]string)
			for k, v := range env {
				values[k] = v
			}
			for k, v := range tc.overrides {
				values[k] = v
			}
			got := effectiveConfigDir(tc.args, repo, values)
			if got != tc.want {
				t.Fatalf("directory=%q, want=%q", got, tc.want)
			}
			if got == other && config.HasConfig(got, values) {
				t.Fatal("caller repository config leaked into effective repository")
			}
		})
	}
}

func TestGitAliasQueryUsesGlobals(t *testing.T) {
	real, root, env := gitContextFixture(t)
	repo := filepath.Join(root, "repo")
	contextGit(t, real, root, env, "init", "-q", repo)
	contextGit(t, real, repo, env, "config", "alias.p", "push origin")
	g, err := gitargs.Parse([]string{"-C", repo, "-c", "alias.inline=status", "inline"})
	if err != nil {
		t.Fatal(err)
	}
	aliases := loadGitAliases(real, g, root, env)
	if aliases["inline.command"] != "status" {
		t.Fatalf("aliases: %v", aliases)
	}
}

func TestInvocationReadFileUsesChdir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "repo")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "message"), []byte("text"), 0600); err != nil {
		t.Fatal(err)
	}
	c := &config.Config{}
	c.ApplyDefaults()
	inv := invocation(c, []string{"-C", "repo", "commit", "-F", "message"}, nil, root, model.StdinData{}, false)
	data, err := inv.ReadFile("message")
	if err != nil || string(data) != "text" {
		t.Fatalf("effective file: %q %v", data, err)
	}
}

func TestAliasOutputIsBounded(t *testing.T) {
	var output aliasOutput
	if n, err := output.Write([]byte(strings.Repeat("x", 1<<20))); err != nil || n != 1<<20 {
		t.Fatal(n, err)
	}
	if _, err := output.Write([]byte("x")); err == nil {
		t.Fatal("unbounded alias output")
	}
}

func TestBuiltinAndOfflineAnalysisDoNotQueryAliases(t *testing.T) {
	_, root, env := gitContextFixture(t)
	marker, real := filepath.Join(root, "queried"), filepath.Join(root, "fake-git")
	if err := os.WriteFile(real, []byte("#!/bin/sh\nprintf queried > '"+marker+"'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	c := &config.Config{Runtime: config.Runtime{RealGit: real}}
	c.ApplyDefaults()
	invocation(c, []string{"-C", root, "status"}, env, root, model.StdinData{}, true)
	invocation(c, []string{"unknown"}, env, root, model.StdinData{}, false)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("builtin/offline analysis launched Git")
	}
}
