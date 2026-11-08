package resolve

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

func TestLocalGitMetadata(t *testing.T) {
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"HOME": root, "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null"}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(real, args...)
		cmd.Env = []string{"HOME=" + root, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "PATH=" + os.Getenv("PATH")}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %q: %v\n%s", args, err, out)
		}
	}
	repo, linked, bare := filepath.Join(root, "repo"), filepath.Join(root, "linked"), filepath.Join(root, "bare.git")
	run("init", "-q", "-b", "main", repo)
	c := (&Resolver{Cwd: repo, Env: env}).Local()
	if c.RepoRoot != repo || c.GitDir != filepath.Join(repo, ".git") || c.CommonDir != c.GitDir || c.Branch != "refs/heads/main" || c.HeadSHA != "" || c.Bare {
		t.Fatalf("unborn context: %+v", c)
	}
	run("-C", repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "-c", "commit.gpgSign=false", "commit", "-q", "--allow-empty", "-m", "fixture")
	run("-C", repo, "worktree", "add", "-q", "-b", "linked", linked)
	for _, path := range []string{repo, linked} {
		r := &Resolver{Cwd: path, Env: env, RealGit: "/must/not/execute"}
		c := r.Local()
		if c.RepoRoot != path || c.CommonDir != filepath.Join(repo, ".git") || c.HeadSHA == "" {
			t.Fatalf("context: %+v", c)
		}
		c.Sources["gitDir"] = "changed"
		if r.Local().Sources["gitDir"] != "git" {
			t.Fatal("Local returned shared mutable data")
		}
		result := model.NewUnknown(model.LevelL1, model.UnknownCommand, "fixture")
		if diagnostics := r.Fill(context.Background(), &result, nil, true); len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
		if result.Operations[0].Target.Worktree != path || result.Operations[0].Target.GitDir != c.GitDir {
			t.Fatalf("target: %+v", result.Operations[0].Target)
		}
	}
	run("-C", repo, "checkout", "-q", "--detach")
	c = (&Resolver{Cwd: repo, Env: env}).Local()
	if c.Branch != "" || c.HeadSHA == "" {
		t.Fatalf("detached: %+v", c)
	}
	run("init", "-q", "--bare", bare)
	c = (&Resolver{Cwd: bare, Env: env}).Local()
	if !c.Bare || c.RepoRoot != "" || c.GitDir != bare || c.CommonDir != bare {
		t.Fatalf("bare: %+v", c)
	}
}

func TestMissingMetadataAndCancellation(t *testing.T) {
	r := &Resolver{Cwd: t.TempDir(), Env: map[string]string{}}
	if c := r.Local(); c.GitDir != "" || len(c.Unresolved) == 0 {
		t.Fatalf("missing metadata: %+v", c)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := model.Result{}
	if len(r.Fill(ctx, &result, nil, true)) != 1 {
		t.Fatal("cancellation ignored")
	}
}
