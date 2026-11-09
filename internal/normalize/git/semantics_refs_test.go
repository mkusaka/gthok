package git

import (
	"errors"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize/actions"
)

func TestRefsModes(t *testing.T) {
	for _, tt := range []struct {
		args   []string
		action string
		effect model.Effect
		refs   *model.TargetSet
		params map[string]any
	}{
		{[]string{"branch"}, "ref.branch.list", model.EffectRead, rrSet([]string{"refs/heads/*"}, true), nil},
		{[]string{"branch", "-av"}, "ref.branch.list", model.EffectRead, rrSet([]string{"refs/heads/*", "refs/remotes/*"}, true), nil},
		{[]string{"branch", "-ar"}, "ref.branch.list", model.EffectRead, rrSet([]string{"refs/remotes/*"}, true), nil},
		{[]string{"branch", "-ra"}, "ref.branch.list", model.EffectRead, rrSet([]string{"refs/heads/*", "refs/remotes/*"}, true), nil},
		{[]string{"branch", "-r", "--list", "origin/*"}, "ref.branch.list", model.EffectRead, rrSet([]string{"refs/remotes/origin/*"}, true), nil},
		{[]string{"branch", "--contains", "HEAD"}, "ref.branch.list", model.EffectRead, nil, nil},
		{[]string{"branch", "--merged"}, "ref.branch.list", model.EffectRead, nil, nil},
		{[]string{"branch", "--no-merged", "HEAD"}, "ref.branch.list", model.EffectRead, nil, nil},
		{[]string{"branch", "--show-current"}, "ref.branch.list", model.EffectRead, model.UnknownSet(), nil},
		{[]string{"branch", "topic", "HEAD~1"}, "ref.create", model.EffectWrite, model.Exact("refs/heads/topic"), map[string]any{"force": false, "refKind": "branch"}},
		{[]string{"branch", "-f", "topic"}, "ref.update", model.EffectWrite, nil, map[string]any{"force": true}},
		{[]string{"branch", "-D", "topic"}, "ref.delete", model.EffectWrite, model.Exact("refs/heads/topic"), map[string]any{"force": true}},
		{[]string{"branch", "-rd", "origin/topic"}, "ref.delete", model.EffectWrite, model.Exact("refs/remotes/origin/topic"), map[string]any{"force": false, "refKind": "other"}},
		{[]string{"branch", "-df", "topic", "other"}, "ref.delete", model.EffectWrite, rrSet([]string{"refs/heads/topic", "refs/heads/other"}, false), map[string]any{"force": true}},
		{[]string{"branch", "-M", "old", "new"}, "ref.rename", model.EffectWrite, rrSet([]string{"refs/heads/old", "refs/heads/new"}, false), map[string]any{"force": true, "source": "old", "destination": "new"}},
		{[]string{"branch", "-c", "old", "new"}, "ref.create", model.EffectWrite, model.Exact("refs/heads/new"), map[string]any{"force": false}},
		{[]string{"branch", "-C", "new"}, "ref.update", model.EffectWrite, model.Exact("refs/heads/new"), map[string]any{"force": true}},
		{[]string{"branch", "-m", "new"}, "ref.rename", model.EffectWrite, nil, map[string]any{"force": false}},
		{[]string{"branch", "--set-upstream-to=origin/main", "topic"}, "ref.branch.upstream", model.EffectWrite, model.Exact("refs/heads/topic"), map[string]any{"upstream": "origin/main", "unset": false}},
		{[]string{"branch", "--unset-upstream"}, "ref.branch.upstream", model.EffectWrite, model.UnknownSet(), map[string]any{"unset": true}},
		{[]string{"branch", "--edit-description", "topic"}, "ref.branch.description", model.EffectWrite, nil, nil},
		{[]string{"branch", "--delete-merged=origin/*", "--dry-run"}, "ref.delete", model.EffectRead, model.UnknownSet(), map[string]any{"dryRun": true}},
		{[]string{"tag"}, "ref.tag.list", model.EffectRead, rrSet([]string{"refs/tags/*"}, true), nil},
		{[]string{"tag", "-l", "v*"}, "ref.tag.list", model.EffectRead, rrSet([]string{"refs/tags/v*"}, true), nil},
		{[]string{"tag", "-n2", "v*"}, "ref.tag.list", model.EffectRead, nil, nil},
		{[]string{"tag", "--points-at", "HEAD"}, "ref.tag.list", model.EffectRead, nil, nil},
		{[]string{"tag", "--ver", "v1"}, "ref.tag.verify", model.EffectRead, model.Exact("refs/tags/v1"), nil},
		{[]string{"tag", "v1"}, "ref.create", model.EffectWrite, model.Exact("refs/tags/v1"), map[string]any{"annotated": false, "force": false, "refKind": "tag"}},
		{[]string{"tag", "-af", "v1", "HEAD"}, "ref.update", model.EffectWrite, nil, map[string]any{"annotated": true, "force": true}},
		{[]string{"tag", "-s", "-m", "release", "v1"}, "ref.create", model.EffectWrite, nil, map[string]any{"signed": true, "annotated": true}},
		{[]string{"tag", "-mrelease", "v1"}, "ref.create", model.EffectWrite, nil, map[string]any{"annotated": true}},
		{[]string{"tag", "-d", "v1", "v2"}, "ref.delete", model.EffectWrite, rrSet([]string{"refs/tags/v1", "refs/tags/v2"}, false), nil},
		{[]string{"tag", "-f", "--no-force", "v1"}, "ref.create", model.EffectWrite, nil, map[string]any{"force": false}},
		{[]string{"tag", "--", "-v"}, "ref.create", model.EffectWrite, model.Exact("refs/tags/-v"), nil},
		{[]string{"update-ref", "--no-deref", "refs/heads/topic", "HEAD"}, "ref.update", model.EffectWrite, model.Exact("refs/heads/topic"), map[string]any{"noDeref": true, "newOid": "HEAD"}},
		{[]string{"update-ref", "-d", "refs/heads/topic"}, "ref.delete", model.EffectWrite, nil, map[string]any{"noDeref": false}},
		{[]string{"update-ref", "--no-deref", "refs/heads/topic", strings.Repeat("0", 40)}, "ref.delete", model.EffectWrite, nil, nil},
		{[]string{"symbolic-ref", "HEAD"}, "ref.symbolic.read", model.EffectRead, model.Exact("HEAD"), nil},
		{[]string{"symbolic-ref", "--no-recurse", "HEAD"}, "ref.symbolic.read", model.EffectRead, nil, nil},
		{[]string{"symbolic-ref", "HEAD", "refs/heads/topic"}, "ref.update", model.EffectWrite, model.Exact("HEAD"), map[string]any{"newTarget": "refs/heads/topic", "refKind": "other"}},
		{[]string{"symbolic-ref", "-d", "refs/remotes/origin/HEAD"}, "ref.delete", model.EffectWrite, nil, nil},
		{[]string{"pack-refs", "--all", "--no-prune", "--include=refs/heads/*", "--exclude=refs/heads/main"}, "ref.pack", model.EffectWrite, model.UnknownSet(), map[string]any{"all": true, "prune": false}},
		{[]string{"reflog"}, "ref.reflog.show", model.EffectRead, model.Exact("HEAD"), nil},
		{[]string{"reflog", "list"}, "ref.reflog.list", model.EffectRead, nil, nil},
		{[]string{"reflog", "exists", "refs/heads/main"}, "ref.reflog.exists", model.EffectRead, nil, nil},
		{[]string{"reflog", "expire", "--all", "--expire=now"}, "ref.reflog.expire", model.EffectWrite, nil, map[string]any{"expire": "now", "all": true}},
		{[]string{"reflog", "delete", "--updateref", "refs/heads/main@{1}"}, "ref.reflog.delete", model.EffectWrite, model.Exact("refs/heads/main"), map[string]any{"updateref": true}},
		{[]string{"reflog", "delete", "-n", "refs/heads/main@{1}"}, "ref.reflog.delete", model.EffectRead, nil, map[string]any{"dryRun": true}},
		{[]string{"reflog", "drop", "--all"}, "ref.reflog.drop", model.EffectWrite, nil, nil},
		{[]string{"reflog", "write", "refs/heads/main", "old", "new", "reason"}, "ref.reflog.write", model.EffectWrite, model.Exact("refs/heads/main"), nil},
		{[]string{"replace"}, "ref.replace.list", model.EffectRead, nil, nil},
		{[]string{"replace", "old", "new"}, "ref.replace.create", model.EffectWrite, model.UnknownSet(), nil},
		{[]string{"replace", "-d", "old"}, "ref.replace.delete", model.EffectWrite, nil, nil},
		{[]string{"replace", "--edit", "old"}, "ref.replace.edit", model.EffectWrite, nil, nil},
		{[]string{"replace", "--graft", "old", "parent"}, "ref.replace.graft", model.EffectWrite, nil, nil},
		{[]string{"replace", "--convert-graft-file"}, "ref.replace.convert", model.EffectWrite, nil, nil},
		{[]string{"show-ref", "--verify", "--end-of-options", "refs/heads/main"}, "ref.list", model.EffectRead, model.Exact("refs/heads/main"), nil},
		{[]string{"for-each-ref", "--shell", "--format=%(refname)", "refs/heads/"}, "ref.list", model.EffectRead, rrSet([]string{"refs/heads/"}, true), nil},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			r := Normalize(model.Invocation{Argv: tt.args})
			if len(r.Operations) != 1 {
				t.Fatalf("operations: %+v", r)
			}
			op := r.Operations[0]
			if op.Action != tt.action || op.Effect != tt.effect || op.Coverage != model.CoverageSemantic || op.Origin.Source == "" {
				t.Fatalf("operation: %+v", op)
			}
			if spec, exists := actions.Lookup(op.Action); !exists || spec.Family != op.Family {
				t.Fatalf("uncatalogued identity: %+v", op)
			}
			if tt.refs != nil && !reflect.DeepEqual(op.Target.Refs, tt.refs) {
				t.Errorf("refs = %+v, want %+v", op.Target.Refs, tt.refs)
			}
			for key, want := range tt.params {
				if !reflect.DeepEqual(op.Parameters[key], want) {
					t.Errorf("parameter %s = %#v, want %#v", key, op.Parameters[key], want)
				}
			}
			if tt.effect == model.EffectWrite && (r.Classification.MutationFree || r.Classification.RetrySafe) {
				t.Fatal("write classified as safe")
			}
		})
	}
}

func TestTagNoNames(t *testing.T) {
	for _, flag := range []string{"--delete", "-d", "--verify", "-v"} {
		t.Run(flag, func(t *testing.T) {
			r := Normalize(model.Invocation{Argv: []string{"tag", flag}})
			if len(r.Errors) != 0 || len(r.Operations) != 1 || r.Classification.Resolution != model.ResolutionComplete {
				t.Fatal(r)
			}
			op := r.Operations[0]
			action, effect := "ref.delete", model.EffectWrite
			if flag == "--verify" || flag == "-v" {
				action, effect = "ref.tag.verify", model.EffectRead
			}
			if op.Action != action || op.Effect != effect || op.Coverage != model.CoverageSemantic || op.UnknownReason != model.UnknownNone || !reflect.DeepEqual(op.Target.Refs, &model.TargetSet{Kind: model.SetNone, Complete: true}) || len(op.Capabilities) != 0 || len(op.Unresolved) != 0 {
				t.Fatal(r)
			}
		})
	}
	for _, flag := range []string{"--delete", "-d"} {
		r := Normalize(model.Invocation{Argv: []string{"branch", flag}})
		if r.Operations[0].UnknownReason != model.UnknownParseError || r.Operations[0].Detail != "branch name required" {
			t.Fatal("branch must still require a name:", r)
		}
	}
}

func TestRefsScopesAndCapabilities(t *testing.T) {
	for _, tt := range []struct {
		args   []string
		scopes []model.Scope
		caps   []model.Capability
		safe   bool
	}{
		{[]string{"branch", "-D", "topic"}, []model.Scope{model.ScopeLocalRef, model.ScopeConfig, model.ScopeReflog}, nil, false},
		{[]string{"branch", "--unset-upstream"}, []model.Scope{model.ScopeConfig}, nil, false},
		{[]string{"branch", "--edit-description"}, []model.Scope{model.ScopeConfig}, []model.Capability{model.CapabilityEditor}, false},
		{[]string{"tag", "--no-sign", "v1"}, []model.Scope{model.ScopeLocalRef, model.ScopeReflog}, nil, false},
		{[]string{"tag", "-a", "v1"}, []model.Scope{model.ScopeLocalRef, model.ScopeReflog, model.ScopeObjects}, []model.Capability{model.CapabilityEditor}, false},
		{[]string{"tag", "-s", "-mrelease", "v1"}, []model.Scope{model.ScopeLocalRef, model.ScopeReflog, model.ScopeObjects}, []model.Capability{model.CapabilityExternalExec}, false},
		{[]string{"tag", "-v", "v1"}, []model.Scope{model.ScopeLocalRef}, []model.Capability{model.CapabilityExternalExec}, false},
		{[]string{"reflog", "expire", "--updateref", "HEAD"}, []model.Scope{model.ScopeReflog, model.ScopeLocalRef}, nil, false},
		{[]string{"show-ref"}, []model.Scope{model.ScopeLocalRef}, nil, true},
		{[]string{"for-each-ref"}, []model.Scope{model.ScopeLocalRef}, nil, true},
		{[]string{"symbolic-ref", "HEAD"}, []model.Scope{model.ScopeLocalRef}, nil, true},
	} {
		r := Normalize(model.Invocation{Argv: tt.args})
		op := r.Operations[0]
		if !reflect.DeepEqual(op.Scopes, tt.scopes) || !reflect.DeepEqual(op.Capabilities, tt.caps) || r.Classification.RetrySafe != tt.safe {
			t.Errorf("%q: %+v, classification %+v", tt.args, op, r.Classification)
		}
	}
}

func TestTagMessageSources(t *testing.T) {
	for _, tt := range []struct {
		inv      model.Invocation
		text     model.Text
		editable bool
	}{
		{model.Invocation{Argv: []string{"tag", "-amfirst", "-m", "second", "v1"}}, model.Text{Source: "inline", Value: "first\n\nsecond", Bytes: 13}, true},
		{model.Invocation{Argv: []string{"tag", "-F", "msg", "v1"}, ReadFile: func(path string) ([]byte, error) { return []byte("body\n"), nil }}, model.Text{Source: "file", Path: "msg", Value: "body\n", Bytes: 5}, true},
		{model.Invocation{Argv: []string{"tag", "-F-", "v1"}, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte("body")}}, model.Text{Source: "stdin", Path: "-", Value: "body", Bytes: 4}, true},
		{model.Invocation{Argv: []string{"tag", "-F-", "v1"}}, model.Text{Source: "stdin", Path: "-", Status: model.StdinNotCaptured}, false},
		{model.Invocation{Argv: []string{"tag", "-F-", "v1"}, Stdin: model.StdinData{Status: model.StdinTooLarge}}, model.Text{Source: "stdin", Path: "-", Status: model.StdinTooLarge, Truncated: true}, false},
		{model.Invocation{Argv: []string{"tag", "-F", "missing", "v1"}, ReadFile: func(string) ([]byte, error) { return nil, errors.New("unreadable") }}, model.Text{Source: "file", Path: "missing"}, false},
		{model.Invocation{Argv: []string{"tag", "-eminitial", "v1"}}, model.Text{Source: "editor"}, false},
	} {
		r := Normalize(tt.inv)
		op := r.Operations[0]
		if op.Texts == nil || !reflect.DeepEqual(*op.Texts.Body, tt.text) {
			t.Errorf("%q: text %+v, want %+v", tt.inv.Argv, op.Texts, tt.text)
		}
		loc, editable := op.Editable["texts/body/value"]
		if editable != tt.editable || editable && len(loc.Spans) == 0 {
			t.Errorf("%q: editable %+v", tt.inv.Argv, op.Editable)
		}
	}
}

func TestUpdateRefCapturedTransactions(t *testing.T) {
	for _, tt := range []struct {
		name, input string
		nul         bool
		actions     []string
		skipped     []int
	}{
		{"implicit", "update refs/heads/a HEAD\ncreate refs/heads/b HEAD\ndelete refs/heads/c\nverify refs/heads/d\n", false, []string{"ref.update", "ref.create", "ref.delete", "ref.verify"}, nil},
		{"committed", "start\noption no-deref\nupdate \"refs/heads/a\" HEAD\nprepare\ncommit\n", false, []string{"ref.transaction.start", "ref.transaction.option", "ref.update", "ref.transaction.prepare", "ref.transaction.commit"}, nil},
		{"abort", "start\ndelete refs/heads/a\nabort\nstart\nupdate refs/heads/b HEAD\ncommit\n", false, []string{"ref.transaction.start", "ref.delete", "ref.transaction.abort", "ref.transaction.start", "ref.update", "ref.transaction.commit"}, []int{1}},
		{"eof-aborts", "start\ndelete refs/heads/a\n", false, []string{"ref.transaction.start", "ref.delete"}, []int{1}},
		{"symrefs", "symref-create refs/heads/alias refs/heads/main\nsymref-update HEAD refs/heads/main ref refs/heads/old\nsymref-delete refs/heads/alias refs/heads/main\nsymref-verify HEAD\n", false, []string{"ref.create", "ref.update", "ref.delete", "ref.symbolic.verify"}, nil},
		{"nul", "start\x00update refs/heads/a\x00HEAD\x00\x00delete refs/heads/b\x00\x00verify refs/heads/c\x00\x00prepare\x00commit\x00", true, []string{"ref.transaction.start", "ref.update", "ref.delete", "ref.verify", "ref.transaction.prepare", "ref.transaction.commit"}, nil},
		{"nul-symrefs", "symref-update HEAD\x00refs/heads/main\x00ref\x00refs/heads/old\x00symref-create refs/heads/alias\x00refs/heads/main\x00symref-delete refs/heads/alias\x00\x00symref-verify HEAD\x00\x00", true, []string{"ref.update", "ref.create", "ref.delete", "ref.symbolic.verify"}, nil},
		{"empty-new-deletes", "update refs/heads/a \"\"\n", false, []string{"ref.delete"}, nil},
		{"zero-old-creates", "update refs/heads/a HEAD " + strings.Repeat("0", 40) + "\nupdate refs/tags/a HEAD " + strings.Repeat("0", 64) + "\ncreate refs/notes/a HEAD\n", false, []string{"ref.create", "ref.create", "ref.create"}, nil},
		{"nul-zero-old-creates", "update refs/heads/a\x00HEAD\x00" + strings.Repeat("0", 40) + "\x00create refs/tags/a\x00HEAD\x00", true, []string{"ref.create", "ref.create"}, nil},
		{"nul-optional-at-eof", "symref-update refs/heads/alias\x00refs/heads/main\x00", true, []string{"ref.update"}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			argv := []string{"update-ref", "--stdin", "--no-deref"}
			if tt.nul {
				argv = append(argv, "-z")
			}
			r := Normalize(model.Invocation{Argv: argv, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte(tt.input)}})
			got := []string{}
			for i, op := range r.Operations {
				got = append(got, op.Action)
				if op.Coverage != model.CoverageSemantic || op.UnknownReason != model.UnknownNone {
					t.Fatalf("unexpected unknown: %+v", op)
				}
				if op.Action == "ref.create" || op.Action == "ref.update" || op.Action == "ref.delete" {
					if op.Family != "ref" || op.Parameters["refKind"] != rrRefKind(op.Target.Refs.Values[0]) || op.Origin.Mode != op.Parameters["command"] {
						t.Fatalf("noncanonical transaction write: %+v", op)
					}
				}
				if (op.Execution == model.ExecutionSkipped) != slices.Contains(tt.skipped, i) {
					t.Errorf("operation %d execution = %s", i, op.Execution)
				}
			}
			if !reflect.DeepEqual(got, tt.actions) {
				t.Fatalf("actions %v, want %v", got, tt.actions)
			}
		})
	}
}

func TestUpdateRefMalformedAndUnavailable(t *testing.T) {
	for _, input := range []string{" delete refs/heads/a\n", "delete\n", "create refs/heads/a\n", "start\nstart\n", "start\nprepare\ndelete refs/heads/a\n", "abort\ndelete refs/heads/a\n", "option other\n", "update \"unterminated HEAD\n", "symref-update HEAD refs/heads/main other old\n", "delete refs/heads/a extra extra\n", "symref-delete refs/heads/alias\n", "symref-verify refs/heads/alias\n", "\n"} {
		r := Normalize(model.Invocation{Argv: []string{"update-ref", "--stdin"}, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte(input)}})
		last := r.Operations[len(r.Operations)-1]
		if last.UnknownReason != model.UnknownParseError || last.Coverage != model.CoverageConservative || r.Classification.RetrySafe {
			t.Errorf("%q: %+v", input, r)
		}
	}
	for _, input := range []string{"update refs/heads/a\x00", "commit", "delete refs/heads/a"} {
		r := Normalize(model.Invocation{Argv: []string{"update-ref", "--stdin", "-z"}, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte(input)}})
		if r.Operations[len(r.Operations)-1].UnknownReason != model.UnknownParseError {
			t.Errorf("%q: %+v", input, r)
		}
	}
	for _, status := range []model.StdinStatus{model.StdinNotCaptured, model.StdinTTY, model.StdinTimeout, model.StdinError, model.StdinTooLarge} {
		r := Normalize(model.Invocation{Argv: []string{"update-ref", "--stdin"}, Stdin: model.StdinData{Status: status}})
		if r.Classification.Coverage != model.CoverageOpaque || r.Classification.RetrySafe || r.Operations[0].Effect != model.EffectUnknown {
			t.Errorf("%s: %+v", status, r)
		}
	}
}

func TestUpdateRefDerefAndNextOption(t *testing.T) {
	input := "option no-deref\nupdate HEAD main\nupdate HEAD next\nsymref-update HEAD refs/heads/main\n"
	r := Normalize(model.Invocation{Argv: []string{"update-ref", "--stdin"}, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte(input)}})
	if len(r.Operations) != 4 || r.Operations[1].Parameters["noDeref"] != true || !r.Operations[1].Target.Refs.Complete {
		t.Fatal(r)
	}
	for _, op := range r.Operations[2:] {
		if op.Parameters["noDeref"] != false || op.Target.Refs.Complete || len(op.Unresolved) == 0 {
			t.Fatalf("symbolic dereferencing hidden: %+v", op)
		}
	}
}

func rrRealGit(t *testing.T) func(string, string, ...string) string {
	t.Helper()
	home := t.TempDir()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	return func(dir, input string, args ...string) string {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir, cmd.Stdin = dir, strings.NewReader(input)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.test", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.test"}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %q: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
}

func TestRefsNoNamesAgainstRealGit(t *testing.T) {
	run, dir, home := rrRealGit(t), t.TempDir(), t.TempDir()
	run(dir, "", "init", "-q", "-b", "main")
	run(dir, "", "commit", "-q", "--allow-empty", "-m", "initial")
	run(dir, "", "tag", "--no-sign", "keep")
	before := run(dir, "", "show-ref", "--tags")
	for _, args := range [][]string{
		{"tag", "--delete"}, {"tag", "-d"}, {"tag", "--verify"}, {"tag", "-v"},
		{"branch", "--delete"}, {"branch", "-d"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
			out, err := cmd.CombinedOutput()
			if args[0] == "tag" {
				if err != nil || len(out) != 0 {
					t.Fatalf("git %q: %v\n%s", args, err, out)
				}
			} else {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 128 || !strings.Contains(string(out), "branch name required") {
					t.Fatalf("git %q must reject missing branch names: %v\n%s", args, err, out)
				}
			}
			if after := run(dir, "", "show-ref", "--tags"); after != before {
				t.Fatalf("no-name invocation changed tags: before %s, after %s", before, after)
			}
		})
	}
}

func TestRefsAgainstRealGit(t *testing.T) {
	git := rrRealGit(t)
	dir := t.TempDir()
	git(dir, "", "init", "-q", "-b", "main")
	git(dir, "", "commit", "-q", "--allow-empty", "-m", "initial")
	head := git(dir, "", "rev-parse", "HEAD")
	for _, args := range [][]string{{"branch", "topic"}, {"branch", "-C", "topic", "copy"}, {"branch", "-M", "copy", "renamed"}, {"branch", "-D", "renamed"}, {"tag", "--no-sign", "v1"}, {"tag", "-d", "v1"}} {
		r := Normalize(model.Invocation{Argv: args})
		if r.Operations[0].Effect != model.EffectWrite {
			t.Fatalf("%q: %+v", args, r)
		}
		git(dir, "", args...)
	}
	input := "start\ncreate refs/heads/created " + head + "\nsymref-create refs/heads/alias refs/heads/created\nprepare\ncommit\n"
	r := Normalize(model.Invocation{Argv: []string{"update-ref", "--stdin"}, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte(input)}})
	git(dir, input, "update-ref", "--stdin")
	if len(r.Operations) != 5 || git(dir, "", "rev-parse", "refs/heads/alias") != head {
		t.Fatal(r)
	}
	nulInput := "delete refs/heads/topic\x00\x00"
	git(dir, nulInput, "update-ref", "--stdin", "-z")
	abort := "start\ndelete refs/heads/created\nabort\n"
	git(dir, abort, "update-ref", "--stdin")
	if git(dir, "", "rev-parse", "refs/heads/created") != head {
		t.Fatal("aborted ref was deleted")
	}
}
