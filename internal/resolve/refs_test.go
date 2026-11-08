package resolve

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

func TestSemanticBranchCreationDefaults(t *testing.T) {
	f := newFixture(t)
	f.commit()
	f.config("remote.origin.url", "ssh://example.test/unreachable")
	f.config("remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	f.run("-C", f.repo, "update-ref", "refs/remotes/origin/topic", "HEAD")
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"switch", "main"}, false},
		{[]string{"switch", "topic"}, true},
		{[]string{"switch", "--no-guess", "topic"}, false},
		{[]string{"-c", "checkout.guess=false", "switch", "topic"}, false},
		{[]string{"worktree", "add", filepath.Join(f.root, "new")}, true},
		{[]string{"-c", "worktree.guessRemote=true", "worktree", "add", filepath.Join(f.root, "topic")}, true},
	} {
		op := normalizeFill(t, f, f.resolver(), tc.args...).Operations[0]
		if hasUnresolved(op, "parameters.createBranch") || op.Parameters["createBranch"] != tc.want {
			t.Fatalf("branch default %q: %+v", tc.args, op)
		}
	}
}

func TestSemanticSymbolicUpdateTargets(t *testing.T) {
	f := newFixture(t)
	f.commit()
	f.run("-C", f.repo, "symbolic-ref", "refs/heads/alias", "refs/heads/main")
	f.run("-C", f.repo, "symbolic-ref", "refs/heads/dangling", "refs/heads/future")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"update-ref", "HEAD", "HEAD"}, "refs/heads/main"},
		{[]string{"update-ref", "-d", "refs/heads/alias"}, "refs/heads/main"},
		{[]string{"update-ref", "-d", "refs/heads/dangling"}, "refs/heads/future"},
		{[]string{"update-ref", "refs/heads/new", "HEAD"}, "refs/heads/new"},
		{[]string{"update-ref", "--no-deref", "-d", "refs/heads/alias"}, "refs/heads/alias"},
		{[]string{"reflog", "show", "main"}, "refs/heads/main"},
		{[]string{"stash", "export", "--to-ref=refs/heads/exported"}, "refs/heads/exported"},
	} {
		op := normalizeFill(t, f, f.resolver(), tc.args...).Operations[0]
		if !reflect.DeepEqual(op.Target.Refs, model.Exact(tc.want)) || hasUnresolved(op, "target.refs") {
			t.Fatalf("refs %q: %+v", tc.args, op)
		}
	}
}
