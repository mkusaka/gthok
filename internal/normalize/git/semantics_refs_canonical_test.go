package git

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

func TestCanonicalRefWritesAcrossCommands(t *testing.T) {
	for _, tc := range []struct {
		args, action, kind, mode, extra string
		maybeCreate                     bool
	}{
		{"branch topic", "ref.create", "branch", "create", "", false},
		{"branch -c old topic", "ref.create", "branch", "copy", "", false},
		{"branch -c topic", "ref.create", "branch", "copy", "", false},
		{"tag topic", "ref.create", "tag", "create", "", false},
		{"tag -amrelease topic", "ref.create", "tag", "create", "", false},
		{"update-ref --no-deref refs/heads/topic HEAD " + strings.Repeat("0", 40), "ref.create", "branch", "create", "", false},
		{"update-ref --no-deref refs/tags/topic HEAD " + strings.Repeat("0", 64), "ref.create", "tag", "create", "", false},
		{"checkout -b topic", "ref.create", "branch", "create", "worktree.checkout.create", false},
		{"switch -c topic HEAD~1", "ref.create", "branch", "create", "worktree.switch.create", false},
		{"worktree add -b topic ../topic", "ref.create", "branch", "add", "worktree.admin.add", false},
		{"worktree add --no-checkout -b topic ../topic", "ref.create", "branch", "add", "worktree.admin.add", false},
		{"branch -f topic", "ref.update", "branch", "create", "", true},
		{"branch -C old topic", "ref.update", "branch", "copy", "", true},
		{"tag -f topic", "ref.update", "tag", "create", "", true},
		{"update-ref refs/heads/topic HEAD", "ref.update", "branch", "update", "", true},
		{"update-ref refs/notes/topic HEAD HEAD~1", "ref.update", "other", "update", "", false},
		{"symbolic-ref HEAD refs/heads/topic", "ref.update", "other", "set", "", true},
		{"checkout -B topic", "ref.update", "branch", "reset", "worktree.checkout.reset", true},
		{"switch -C topic HEAD~1", "ref.update", "branch", "reset", "worktree.switch.reset", true},
		{"worktree add -B topic ../topic HEAD~1", "ref.update", "branch", "add", "worktree.admin.add", true},
		{"branch -d topic", "ref.delete", "branch", "delete", "", false},
		{"branch -D topic", "ref.delete", "branch", "delete", "", false},
		{"tag -d topic", "ref.delete", "tag", "delete", "", false},
		{"update-ref -d refs/heads/topic", "ref.delete", "branch", "delete", "", false},
		{"update-ref refs/tags/topic " + strings.Repeat("0", 40) + " " + strings.Repeat("0", 40), "ref.delete", "tag", "delete", "", false},
		{"symbolic-ref -d refs/heads/alias", "ref.delete", "branch", "delete", "", false},
		{"branch -m old topic", "ref.rename", "branch", "rename", "", false},
		{"branch -M old topic", "ref.rename", "branch", "rename", "", false},
	} {
		t.Run(tc.args, func(t *testing.T) {
			argv := strings.Fields(tc.args)
			r := Normalize(model.Invocation{Argv: argv})
			count := 1
			if tc.extra != "" {
				count++
			}
			if len(r.Errors) != 0 || len(r.Operations) != count || tc.extra != "" && r.Operations[0].Action != tc.extra {
				t.Fatal(r)
			}
			op := r.Operations[count-1]
			if op.Family != "ref" || op.Action != tc.action || op.Effect != model.EffectWrite || op.Parameters["refKind"] != tc.kind || op.Origin.Mode != tc.mode || op.Origin.Command[0] != argv[0] || op.Origin.Source == "" {
				t.Fatal(op)
			}
			if !slices.Contains(op.Scopes, model.ScopeLocalRef) || !slices.Contains(op.Scopes, model.ScopeReflog) || r.Classification.MutationFree || r.Classification.RetrySafe {
				t.Fatal(r)
			}
			if got := slices.ContainsFunc(op.Unresolved, func(u model.Unresolved) bool { return u.Field == "action" && u.Source == "git" }); got != tc.maybeCreate {
				t.Fatalf("create/update uncertainty = %t, want %t: %+v", got, tc.maybeCreate, op)
			}
			if tc.extra != "" {
				if !reflect.DeepEqual(op.Target.Refs, model.Exact("refs/heads/topic")) || !reflect.DeepEqual(op.Target.Revisions, r.Operations[0].Target.Revisions) || op.Parameters["force"] != (tc.action == "ref.update") {
					t.Fatal(op)
				}
			}
		})
	}
}
