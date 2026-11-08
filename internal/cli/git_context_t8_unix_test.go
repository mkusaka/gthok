//go:build unix

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/model"
)

func TestBareContextReplaysRootOptions(t *testing.T) {
	real, root, env := gitContextFixture(t)
	bare, later := filepath.Join(root, "bare.git"), filepath.Join(root, "later")
	contextGit(t, real, root, env, "init", "-q", "--bare", bare)
	if err := os.Mkdir(later, 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args              []string
		gitDir, workTree  string
		wantDir, wantTree string
	}{
		{[]string{"-C", bare, "--bare", "-C", later}, "", "", bare, ""},
		{[]string{"--bare", "-C", later}, bare, root, bare, root},
		{[]string{"--work-tree", root, "-C", bare, "--bare", "-C", later}, "", "", bare, root},
		{[]string{"--bare", "--git-dir", bare, "-C", later}, "", "", bare, ""},
	} {
		values := make(map[string]string)
		for k, v := range env {
			values[k] = v
		}
		if tc.gitDir != "" {
			values["GIT_DIR"] = tc.gitDir
		}
		if tc.workTree != "" {
			values["GIT_WORK_TREE"] = tc.workTree
		}
		cwd, got := effectiveContext(tc.args, root, values)
		if cwd != later || got["GIT_DIR"] != tc.wantDir || got["GIT_WORK_TREE"] != tc.wantTree {
			t.Fatalf("%q: cwd=%s env=%v", tc.args, cwd, got)
		}
		cmd := exec.Command(real, append(append([]string(nil), tc.args...), "rev-parse", "--absolute-git-dir")...)
		cmd.Dir, cmd.Env = root, environmentList(values)
		out, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != got["GIT_DIR"] {
			t.Fatalf("git parity: %s %v", out, err)
		}
	}
}

func TestAliasProvidedOverrideRemainsConservative(t *testing.T) {
	real, root, env := gitContextFixture(t)
	contextGit(t, real, root, env, "init", "-q")
	contextGit(t, real, root, env, "config", "alias.start", "-c alias.inline=rev-parse inline HEAD")
	c := &config.Config{Runtime: config.Runtime{RealGit: real}}
	c.ApplyDefaults()
	inv := invocation(c, []string{"start"}, env, root, model.StdinData{}, true)
	r := normalizeInvocation(c, inv)
	if r.Classification.RetrySafe || r.Classification.Coverage != model.CoverageConservative || r.Operations[0].UnknownReason != model.UnknownParseError {
		t.Fatalf("unverified inner alias became retry-safe: %+v", r)
	}
}

func TestOrderedAliasesAndDashedExecutables(t *testing.T) {
	real, root, env := gitContextFixture(t)
	repo := filepath.Join(root, "repo")
	contextGit(t, real, root, env, "init", "-q", repo)
	c := &config.Config{Runtime: config.Runtime{RealGit: real}}
	c.ApplyDefaults()
	for _, body := range []string{
		"[alias]\n x = status\n[alias \"x\"]\n command = rev-parse HEAD\n",
		"[alias \"x\"]\n command = status\n[alias]\n x = rev-parse HEAD\n",
		"[alias \"X\"]\n command = status\n[alias]\n x = rev-parse HEAD\n",
		"[alias]\n start = X\n[alias \"X\"]\n command = status\n[alias]\n x = rev-parse HEAD\n",
	} {
		if err := os.WriteFile(filepath.Join(repo, ".git", "config"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		name := "x"
		if strings.Contains(body, "start =") {
			name = "start"
		} else if strings.Contains(body, "\"X\"") {
			name = "X"
		}
		inv := invocation(c, []string{name}, env, repo, model.StdinData{}, true)
		result := normalizeInvocation(c, inv)
		if len(result.Source.Argv) != 2 || result.Source.Argv[0] != "rev-parse" || result.Source.Argv[1] != "HEAD" {
			t.Fatalf("ordered aliases: %v %+v", inv.Aliases, result)
		}
	}
	contextGit(t, real, repo, env, "config", "alias.x", "status")
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "git-x"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, globals := range [][]string{nil, {"--exec-path=" + bin}, {"-c", "alias.x=status"}, {"-c", "alias.start=x", "-c", "alias.x=status"}, {"-c", "alias.start=-c alias.x=rev-parse x HEAD"}} {
		values := make(map[string]string)
		for k, v := range env {
			values[k] = v
		}
		if globals == nil || globals[0] == "-c" {
			values["PATH"] = bin + ":" + values["PATH"]
		}
		name := "x"
		if len(globals) > 2 || len(globals) > 1 && strings.Contains(globals[1], "alias.start=") {
			name = "start"
		}
		args := append(append([]string(nil), globals...), name)
		inv := invocation(c, args, values, repo, model.StdinData{}, true)
		r := normalizeInvocation(c, inv)
		if r.Operations[0].UnknownReason != model.UnknownExternal || r.Classification.RetrySafe {
			t.Fatalf("dashed precedence: %+v", r)
		}
	}
}
