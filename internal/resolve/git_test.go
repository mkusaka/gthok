package resolve

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mkusaka/gthok/internal/model"
)

type fixture struct {
	t               *testing.T
	git, root, repo string
	env             map[string]string
}

func newFixture(t *testing.T, initArgs ...string) *fixture {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, git: real, root: root, repo: filepath.Join(root, "repo"), env: map[string]string{
		"HOME": root, "PATH": os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null",
	}}
	args := append([]string{"init", "-q", "-b", "main"}, initArgs...)
	f.run(append(args, f.repo)...)
	return f
}

func (f *fixture) run(args ...string) string {
	f.t.Helper()
	cmd := exec.Command(f.git, args...)
	for k, v := range f.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("fixture git failed: %v\n%s", err, out)
	}
	return strings.TrimSuffix(string(out), "\n")
}

func (f *fixture) config(key, value string) {
	f.t.Helper()
	f.run("-C", f.repo, "config", "--add", key, value)
}

func (f *fixture) commit() {
	f.t.Helper()
	f.run("-C", f.repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "-c", "commit.gpgSign=false", "commit", "-q", "--allow-empty", "-m", "fixture")
}

func (f *fixture) resolver() *Resolver {
	return &Resolver{Cwd: f.repo, Env: f.env, RealGit: f.git}
}

func pushOperation() model.Operation {
	return model.Operation{Family: "remote.push", Action: "remote.push.update", Effect: model.EffectWrite,
		Scopes: []model.Scope{model.ScopeRemoteRef}, Execution: model.ExecutionWillRun,
		Target: model.Target{Remote: "origin", Refs: model.UnknownSet()}, Parameters: map[string]any{}}
}

func fill(t *testing.T, r *Resolver, ops ...model.Operation) model.Result {
	t.Helper()
	res := model.Result{Operations: ops}
	if diagnostics := r.Fill(context.Background(), &res, nil, true); len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	return res
}

func TestLocalGitMetadata(t *testing.T) {
	for _, format := range []string{"files", "reftable"} {
		t.Run(format, func(t *testing.T) {
			f := newFixture(t, "--ref-format="+format)
			c := f.resolver().Local()
			if c.RepoRoot != f.repo || c.GitDir != filepath.Join(f.repo, ".git") || c.CommonDir != c.GitDir || c.Branch != "refs/heads/main" || c.HeadSHA != "" || c.Bare {
				t.Fatalf("unborn context: %+v", c)
			}
			f.commit()
			linked := filepath.Join(f.root, "linked")
			f.run("-C", f.repo, "worktree", "add", "-q", "-b", "linked", linked)
			for _, path := range []string{f.repo, linked} {
				r := f.resolver()
				r.Cwd = path
				c := r.Local()
				if c.RepoRoot != path || c.CommonDir != filepath.Join(f.repo, ".git") || c.HeadSHA == "" || len(c.Unresolved) != 0 {
					t.Fatalf("context: %+v", c)
				}
				c.Sources["gitDir"] = "changed"
				if r.Local().Sources["gitDir"] != "git" {
					t.Fatal("Local returned shared mutable sources")
				}
				res := fill(t, r, model.NewUnknown(model.LevelL1, model.UnknownCommand, "fixture").Operations[0])
				if res.Operations[0].Target.Worktree != path || res.Operations[0].Target.GitDir != c.GitDir {
					t.Fatalf("target: %+v", res.Operations[0].Target)
				}
			}
			f.run("-C", f.repo, "checkout", "-q", "--detach")
			c = f.resolver().Local()
			if c.Branch != "" || c.HeadSHA == "" || len(c.Unresolved) != 0 {
				t.Fatalf("detached: %+v", c)
			}
		})
	}
}

func TestBareGitMetadata(t *testing.T) {
	f := newFixture(t, "--bare")
	c := f.resolver().Local()
	if !c.Bare || c.RepoRoot != "" || c.GitDir != f.repo || c.CommonDir != f.repo || c.Branch != "refs/heads/main" {
		t.Fatalf("bare: %+v", c)
	}
	res := fill(t, f.resolver(), pushOperation())
	if res.Operations[0].Target.Worktree != "" {
		t.Fatal("bare repo acquired a worktree")
	}
}

func TestGlobalOptionsAndEnvironment(t *testing.T) {
	f := newFixture(t)
	f.commit()
	f.config("remote.origin.url", "https://example.test/fetch")
	if err := os.Mkdir(filepath.Join(f.repo, "subdir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.repo, "subdir"), filepath.Join(f.root, "link")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		cwd    string
		global []string
		env    map[string]string
	}{
		{"C chain", f.root, []string{"-C", "repo", "-C", "..", "-C", "repo"}, f.env},
		{"attached C", f.root, []string{"-Crepo"}, f.env},
		{"C chain through symlink", f.root, []string{"-C", "link", "-C", ".."}, f.env},
		{"git dir global", f.root, []string{"--git-dir=repo/.git", "--work-tree", "repo"}, f.env},
		{"git dir env", f.root, nil, cloneEnv(f.env, "GIT_DIR", filepath.Join(f.repo, ".git"), "GIT_WORK_TREE", f.repo)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Resolver{Cwd: tc.cwd, Env: tc.env, Global: tc.global, RealGit: f.git}
			c := r.Local()
			if c.RepoRoot != f.repo || c.GitDir != filepath.Join(f.repo, ".git") || c.HeadSHA == "" || c.Remotes["origin"].URL != "https://example.test/fetch" {
				t.Fatalf("context: %+v", c)
			}
		})
	}
	f.env["RESOLVER_TEST_URL"] = "https://example.test/config-env"
	r := f.resolver()
	r.Global = []string{"-c", "remote.origin.url=https://example.test/override", "--config-env=remote.origin.pushurl=RESOLVER_TEST_URL"}
	c := r.Local()
	if c.Remotes["origin"].PushURLs[0] != f.env["RESOLVER_TEST_URL"] {
		t.Fatalf("config-env was lost: %+v", c)
	}
	// --get-url selects the first URL; prove -c reaches config with a new remote.
	r = f.resolver()
	r.Global = []string{"-cremote.injected.url=https://example.test/injected"}
	if r.Local().Remotes["injected"].URL != "https://example.test/injected" {
		t.Fatal("inline config was lost")
	}
	bare := newFixture(t, "--bare")
	r = bare.resolver()
	r.Global = []string{"--bare"}
	if c := r.Local(); !c.Bare || c.GitDir != bare.repo {
		t.Fatalf("--bare lost: %+v", c)
	}
	dashDir := filepath.Join(f.root, "-C")
	f.run("init", "-q", "--bare", dashDir)
	r = f.resolver()
	r.Cwd, r.Global = f.root, []string{"--git-dir", "-C"}
	if c := r.Local(); c.GitDir != dashDir || !c.Bare {
		t.Fatalf("global option value was parsed as -C: %+v", c)
	}
}

func cloneEnv(env map[string]string, pairs ...string) map[string]string {
	copy := make(map[string]string)
	for k, v := range env {
		copy[k] = v
	}
	for i := 0; i < len(pairs); i += 2 {
		copy[pairs[i]] = pairs[i+1]
	}
	return copy
}

func TestSubmoduleGitdir(t *testing.T) {
	f := newFixture(t)
	f.commit()
	child := filepath.Join(f.root, "child")
	f.run("init", "-q", child)
	f.run("-C", child, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "-c", "commit.gpgSign=false", "commit", "-q", "--allow-empty", "-m", "fixture")
	f.run("-C", f.repo, "-c", "protocol.file.allow=always", "submodule", "add", "-q", child, "sub")
	r := f.resolver()
	r.Cwd = filepath.Join(f.repo, "sub")
	c := r.Local()
	if c.RepoRoot != r.Cwd || c.GitDir != filepath.Join(f.repo, ".git", "modules", "sub") || c.CommonDir != c.GitDir || c.HeadSHA == "" {
		t.Fatalf("submodule: %+v", c)
	}
}

func TestRemotesAndNoNetwork(t *testing.T) {
	f := newFixture(t)
	f.commit()
	f.env["GIT_SSH_COMMAND"] = "false"
	f.config("http.proxy", "http://127.0.0.1:1")
	f.config("url.https://example.test/rewritten/.insteadOf", "short:")
	f.config("url.ssh://example.test/push/.pushInsteadOf", "short:")
	f.config("remote.origin.url", "short:repo")
	f.config("remote.backup.url", "ssh://example.test/backup")
	f.config("remote.backup.pushurl", "https://example.test/one")
	f.config("remote.backup.pushurl", "https://example.test/two")
	f.config("remotes.everywhere", "origin backup")
	r := f.resolver()
	c := r.Local()
	if c.Remotes["origin"].URL != "https://example.test/rewritten/repo" || !reflect.DeepEqual(c.Remotes["origin"].PushURLs, []string{"ssh://example.test/push/repo"}) {
		t.Fatalf("rewrites: %+v", c)
	}
	info := c.Remotes["backup"]
	info.PushURLs[0] = "mutated"
	c.Remotes["backup"] = info
	delete(c.Remotes, "origin")
	if r.Local().Remotes["backup"].PushURLs[0] != "https://example.test/one" || len(r.Local().Remotes) != 2 {
		t.Fatal("Local returned shared remotes")
	}
	op := pushOperation()
	op.Target.Remote = "everywhere"
	op.Parameters["refspecs"] = []string{"main"}
	op.Unresolved = []model.Unresolved{{Field: "target.remote", Reason: "remote may be a group", Source: "git"}}
	res := fill(t, r, op)
	if len(res.Operations) != 3 {
		t.Fatalf("expected one operation per remote/push URL: %+v", res.Operations)
	}
	for _, op := range res.Operations {
		if op.Target.RemoteURL == "" || op.Target.PushURL == "" || op.Target.Refs.Values[0] != "refs/heads/main" || len(op.Unresolved) != 0 {
			t.Fatalf("resolved operation: %+v", op)
		}
	}
	if op.Target.Remote != "everywhere" || op.Target.Refs.Kind != model.SetUnknown || len(op.Unresolved) != 1 {
		t.Fatal("Fill mutated its input operation")
	}
	// Direct URL operands get the same local rewrite, without a helper/network.
	op = pushOperation()
	op.Target.Remote = "short:direct"
	op.Parameters["refspecs"] = []string{"main"}
	got := fill(t, r, op).Operations[0].Target
	if got.RemoteURL != "https://example.test/rewritten/direct" || got.PushURL != "ssh://example.test/push/direct" {
		t.Fatalf("direct URL: %+v", got)
	}
}

func TestCredentialsStripped(t *testing.T) {
	f := newFixture(t)
	secretURL := "https://fixture:" + "fixture-password" + "@example.test/repo"
	f.config("remote.origin.url", secretURL)
	f.config("remote.origin.pushurl", secretURL)
	r := f.resolver()
	c := r.Local()
	if c.Remotes["origin"].URL != "https://example.test/repo" || c.Remotes["origin"].PushURLs[0] != "https://example.test/repo" {
		t.Fatal("remote credentials not stripped")
	}
	op := pushOperation()
	op.Target.Remote = secretURL
	op.Parameters["refspecs"] = []string{"HEAD:refs/heads/main"}
	res := fill(t, r, op)
	data, err := json.Marshal(res)
	if err != nil || strings.Contains(string(data), "fixture-password") || strings.Contains(res.Operations[0].Target.Remote, "@") {
		t.Fatal("target credentials not stripped")
	}
}

func TestLocalRefDWIM(t *testing.T) {
	f := newFixture(t)
	f.commit()
	f.run("-C", f.repo, "tag", "release")
	f.run("-C", f.repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	f.run("-C", f.repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	for _, tc := range []struct{ input, want string }{
		{"main", "refs/heads/main"}, {"release", "refs/tags/release"}, {"heads/main", "refs/heads/main"},
		{"origin/main", "refs/remotes/origin/main"}, {"origin", "refs/remotes/origin/HEAD"},
		{"HEAD", "refs/heads/main"}, {"refs/heads/new", "refs/heads/new"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			op := model.Operation{Family: "ref", Action: "ref.update", Effect: model.EffectWrite, Scopes: []model.Scope{model.ScopeLocalRef}, Target: model.Target{Refs: model.Exact(tc.input)}, Unresolved: []model.Unresolved{{Field: "target.refs", Source: "git"}}}
			res := fill(t, f.resolver(), op)
			got := res.Operations[0]
			if !got.Target.Refs.Complete || got.Target.Refs.Values[0] != tc.want || len(got.Unresolved) != 0 || res.Classification.Resolution != model.ResolutionComplete {
				t.Fatalf("DWIM: %+v", res)
			}
		})
	}
	f.run("-C", f.repo, "tag", "main")
	for _, name := range []string{"main", "missing", "--output=unsafe"} {
		op := model.Operation{Scopes: []model.Scope{model.ScopeLocalRef}, Target: model.Target{Refs: model.Exact(name)}}
		got := fill(t, f.resolver(), op).Operations[0]
		if got.Target.Refs.Kind != model.SetUnknown || got.Target.Refs.Complete || len(got.Unresolved) == 0 {
			t.Fatalf("unresolvable ref: %+v", got)
		}
	}
}

func TestMissingMetadataAndCancellation(t *testing.T) {
	f := newFixture(t)
	r := f.resolver()
	r.Cwd = f.root
	if c := r.Local(); c.GitDir != "" || len(c.Unresolved) == 0 {
		t.Fatalf("missing metadata: %+v", c)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := model.Result{}
	if len(r.Fill(ctx, &result, nil, true)) != 1 {
		t.Fatal("cancellation ignored")
	}
	if got := r.Fill(context.Background(), nil, nil, false); len(got) != 0 {
		t.Fatal(got)
	}
	for _, real := range []string{"", "git", "/must/not/execute/git"} {
		r := f.resolver()
		r.RealGit = real
		if c := r.Local(); c.GitDir != "" || len(c.Unresolved) == 0 {
			t.Fatalf("real Git requirement ignored: %+v", c)
		}
	}
	r = f.resolver()
	r.Timeout = time.Nanosecond
	if c := r.Local(); len(c.Unresolved) == 0 {
		t.Fatal("query timeout ignored")
	}
}
