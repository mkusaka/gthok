//go:build unix

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

func gitFixture(t *testing.T) (*sandbox, string) {
	t.Helper()
	s, real := realGitSandbox(t)
	s.env["GIT_TERMINAL_PROMPT"], s.env["GIT_SSH_COMMAND"] = "0", "false"
	return s, real
}

func normalizeGitFixture(t *testing.T, s *sandbox, args ...string) model.Result {
	t.Helper()
	out := s.management(nil, append([]string{"normalize", "--"}, args...)...)
	out.code(t, 0)
	var result model.Result
	if err := json.Unmarshal(out.stdout, &result); err != nil {
		t.Fatalf("normalize: %v\n%s", err, out.stdout)
	}
	if len(result.Errors) != 0 || len(result.Operations) == 0 {
		t.Fatalf("normalize: %+v; stderr=%s", result, out.stderr)
	}
	return result
}

func canonicalGitPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func checkGitContext(t *testing.T, result model.Result, root, gitDir, common string) {
	t.Helper()
	for _, op := range result.Operations {
		if op.Target.RepoRoot != root || op.Target.Worktree != root || op.Target.GitDir != gitDir || op.Target.CommonDir != common {
			t.Fatalf("context: %+v; want root=%q gitDir=%q common=%q", op.Target, root, gitDir, common)
		}
	}
}

func TestGitContextConfigIncludeIfAndAliases(t *testing.T) {
	s, real := gitFixture(t)
	root := canonicalGitPath(t, s.cwd)
	global, included := filepath.Join(s.env["HOME"], ".gitconfig"), filepath.Join(s.root, "included.gitconfig")
	s.env["GIT_CONFIG_GLOBAL"] = global
	for _, entry := range []struct{ file, key, value string }{
		{global, "alias.inspect", "branch --list"},
		{global, "alias.global-message", "commit -m 'global quoted message'"},
		{global, "includeIf.gitdir:" + root + "/.git.path", included},
		{included, "remote.origin.url", "file://" + filepath.Join(s.root, "included.git")},
		{included, "alias.publish", "push origin HEAD:refs/heads/included"},
		{filepath.Join(s.cwd, ".git", "config"), "alias.inspect", "push origin HEAD:refs/heads/local"},
	} {
		s.run(real, nil, "config", "--file", entry.file, entry.key, entry.value).code(t, 0)
	}
	for _, tc := range []struct {
		name string
		args []string
		ref  string
	}{
		{"conditional include", []string{"publish"}, "refs/heads/included"},
		{"local overrides global", []string{"inspect"}, "refs/heads/local"},
		{"inline overrides local", []string{"-c", "alias.inspect=push origin HEAD:refs/heads/inline", "inspect"}, "refs/heads/inline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizeGitFixture(t, s, tc.args...)
			checkGitContext(t, result, root, root+"/.git", root+"/.git")
			op := result.Operations[0]
			if len(result.Operations) != 1 || op.Action != "remote.push.update" || !reflect.DeepEqual(op.Target.Refs, model.Exact(tc.ref)) || op.Target.RemoteURL != "file://"+filepath.Join(s.root, "included.git") {
				t.Fatalf("include/alias: %+v", result)
			}
		})
	}
	result := normalizeGitFixture(t, s, "global-message")
	if result.Source.Alias != "global-message" || result.Operations[0].Texts.Body.Value != "global quoted message" {
		t.Fatalf("global alias: %+v", result)
	}
}

func TestGitContextGlobalsAndEnvironment(t *testing.T) {
	for _, kind := range []string{"chdir-chain", "git-dir", "environment"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := gitFixture(t)
			root := canonicalGitPath(t, s.cwd)
			if err := os.Mkdir(filepath.Join(s.cwd, "sub"), 0700); err != nil {
				t.Fatal(err)
			}
			var args []string
			switch kind {
			case "chdir-chain":
				args = []string{"-C", "sub", "-C", ".."}
			case "git-dir":
				s.cwd = s.root
				args = []string{"--git-dir=" + root + "/.git", "--work-tree=" + root}
			case "environment":
				s.cwd = s.root
				s.env["GIT_DIR"], s.env["GIT_WORK_TREE"] = root+"/.git", root
			}
			result := normalizeGitFixture(t, s, append(args, "reset", "--soft", "HEAD")...)
			checkGitContext(t, result, root, root+"/.git", root+"/.git")
			if !reflect.DeepEqual(result.Operations[0].Target.Refs, model.Exact("refs/heads/main")) {
				t.Fatalf("resolved HEAD: %+v", result)
			}
		})
	}
}

func TestGitContextLinkedWorktreeAndReftable(t *testing.T) {
	for _, format := range []string{"files", "reftable"} {
		t.Run(format, func(t *testing.T) {
			s, real := gitFixture(t)
			repo := filepath.Join(s.root, "repo")
			s.run(real, nil, "init", "-q", "-b", "main", "--ref-format="+format, repo).code(t, 0)
			s.cwd = repo
			s.run(real, nil, "commit", "-q", "--allow-empty", "-m", "initial").code(t, 0)
			linked := filepath.Join(s.root, "linked")
			s.run(real, nil, "worktree", "add", "-q", "-b", "linked", linked).code(t, 0)
			common := canonicalGitPath(t, filepath.Join(repo, ".git"))
			for _, path := range []string{repo, linked} {
				s.cwd = path
				root := canonicalGitPath(t, path)
				gitDir := common
				branch := "main"
				if path == linked {
					gitDir, branch = filepath.Join(common, "worktrees", "linked"), "linked"
				}
				result := normalizeGitFixture(t, s, "reset", "--soft", "HEAD")
				checkGitContext(t, result, root, gitDir, common)
				if !reflect.DeepEqual(result.Operations[0].Target.Refs, model.Exact("refs/heads/"+branch)) {
					t.Fatalf("resolved %s HEAD: %+v", format, result)
				}
			}
		})
	}
}

func TestGitContextBareExplicitGitDir(t *testing.T) {
	s, real := gitFixture(t)
	bare := filepath.Join(s.root, "bare.git")
	s.run(real, nil, "clone", "-q", "--bare", s.cwd, bare).code(t, 0)
	bare = canonicalGitPath(t, bare)
	s.cwd = s.root
	result := normalizeGitFixture(t, s, "--git-dir="+bare, "update-ref", "-d", "refs/heads/main")
	checkGitContext(t, result, "", bare, bare)
	if !reflect.DeepEqual(result.Operations[0].Target.Refs, model.Exact("refs/heads/main")) {
		t.Fatalf("bare refs: %+v", result)
	}
}

func TestGitContextRemoteGroupsAndMultiplePushURLs(t *testing.T) {
	s, real := gitFixture(t)
	urls := map[string]string{}
	for _, name := range []string{"origin", "backup"} {
		urls[name] = "file://" + filepath.Join(s.root, name+".git")
		s.run(real, nil, "init", "-q", "--bare", filepath.Join(s.root, name+".git")).code(t, 0)
		s.run(real, nil, "remote", "add", name, urls[name]).code(t, 0)
	}
	s.run(real, nil, "config", "remotes.fleet", "origin backup").code(t, 0)
	result := normalizeGitFixture(t, s, "push", "fleet", "main")
	if len(result.Operations) != 2 {
		t.Fatalf("group expansion: %+v", result)
	}
	for i, name := range []string{"origin", "backup"} {
		op := result.Operations[i]
		if op.Target.Remote != name || op.Target.PushURL != urls[name] || !reflect.DeepEqual(op.Target.Refs, model.Exact("refs/heads/main")) {
			t.Fatalf("group destination: %+v", op)
		}
	}
	for _, name := range []string{"origin", "backup"} {
		s.run(real, nil, "config", "--add", "remote.origin.pushurl", urls[name]).code(t, 0)
	}
	result = normalizeGitFixture(t, s, "push", "origin", "main")
	if len(result.Operations) != 2 {
		t.Fatalf("push URL expansion: %+v", result)
	}
	for i, name := range []string{"origin", "backup"} {
		if op := result.Operations[i]; op.Target.Remote != "origin" || op.Target.PushURL != urls[name] {
			t.Fatalf("push URL %d: %+v", i, op)
		}
	}
}

func TestGitContextURLRewrites(t *testing.T) {
	s, real := gitFixture(t)
	fetch, push := "file://"+filepath.Join(s.root, "fetch")+"/", "file://"+filepath.Join(s.root, "push")+"/"
	s.run(real, nil, "config", "url."+fetch+".insteadOf", "fixture:").code(t, 0)
	s.run(real, nil, "config", "url."+push+".pushInsteadOf", "fixture:").code(t, 0)
	s.run(real, nil, "remote", "add", "origin", "fixture:repo.git").code(t, 0)
	result := normalizeGitFixture(t, s, "push", "origin", "main")
	op := result.Operations[0]
	if op.Target.RemoteURL != fetch+"repo.git" || op.Target.PushURL != push+"repo.git" {
		t.Fatalf("rewritten URLs: %+v", op.Target)
	}
	// Explicit pushurl suppresses pushInsteadOf, but still applies insteadOf.
	s.run(real, nil, "config", "remote.origin.pushurl", "fixture:explicit.git").code(t, 0)
	result = normalizeGitFixture(t, s, "push", "origin", "main")
	if got := result.Operations[0].Target.PushURL; got != fetch+"explicit.git" {
		t.Fatalf("explicit pushurl: %q", got)
	}
}
