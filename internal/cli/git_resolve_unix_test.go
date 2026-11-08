//go:build unix

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagementResolverUsesOriginalContextAndGlobals(t *testing.T) {
	real, root, env := gitContextFixture(t)
	repo := filepath.Join(root, "repo")
	contextGit(t, real, root, env, "init", "-q", "-b", "main", repo)
	if err := os.Mkdir(filepath.Join(repo, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	for key, value := range env {
		t.Setenv(key, value)
	}
	t.Setenv("GTHOK_REAL_GIT", real)
	t.Setenv("RESOLVE_PUSH_URL", "ssh://example.test/push")
	t.Setenv("RESOLVE_TEST_SECRET", "fixture-inline-value")
	args := []string{"-C", "repo", "-C", "sub", "-C", "..", "--git-dir=.git", "--work-tree=.", "--namespace=fixture", "-c", "remote.origin.url=https://example.test/fixture-inline-value", "--config-env=remote.origin.pushurl=RESOLVE_PUSH_URL", "status"}
	for _, legacyAPI := range []bool{false, true} {
		a, err := managementAnalyze(args, true, legacyAPI, false, false)
		if err != nil {
			t.Fatal(err)
		}
		defer a.cleanup()
		if a.real != real || a.context == nil || a.context.RepoRoot != repo || a.context.GitDir != filepath.Join(repo, ".git") {
			t.Fatalf("effective context: real=%q context=%+v", a.real, a.context)
		}
		remote := a.context.Remotes["origin"]
		if remote.URL != "https://example.test/fixture-inline-value" || len(remote.PushURLs) != 1 || remote.PushURLs[0] != "ssh://example.test/push" {
			t.Fatalf("raw config globals were lost: %+v", remote)
		}
		if _, ok := a.invocation.Env["GTHOK_RESOLVER_QUERY"]; ok {
			t.Fatal("resolver provenance leaked into invocation environment")
		}
	}
}

func TestManagementNoResolveDoesNotDiscoverGit(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", root)
	t.Setenv("GTHOK_CONFIG_DIR", root)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GTHOK_REAL_GIT", filepath.Join(root, "missing-git"))
	t.Setenv("PATH", root)
	a, err := managementAnalyze([]string{"status"}, false, false, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.cleanup()
	if a.real != "" || a.context != nil {
		t.Fatal("offline analysis discovered Git or invoked the resolver")
	}
	if a, err := managementAnalyze([]string{"status"}, true, false, false, false); err == nil {
		a.cleanup()
		t.Fatal("resolving analysis did not require real Git")
	}
}
