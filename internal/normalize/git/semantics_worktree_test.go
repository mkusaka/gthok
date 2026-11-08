package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize/actions"
)

type whCase struct {
	args   []string
	action string
	scopes []model.Scope
	params map[string]any
}

func whCheckCase(t *testing.T, tc whCase) model.Operation {
	t.Helper()
	r := Normalize(model.Invocation{Argv: tc.args})
	count := 1
	if tc.action == "worktree.checkout.create" || tc.action == "worktree.checkout.reset" || tc.action == "worktree.switch.create" || tc.action == "worktree.switch.reset" {
		count = 2
	}
	if len(r.Operations) != count {
		t.Fatalf("%q operations: %+v", tc.args, r)
	}
	op := r.Operations[0]
	spec, ok := actions.Lookup(tc.action)
	if !ok || op.Action != tc.action || op.Family != spec.Family || op.Effect != spec.Effect || !reflect.DeepEqual(op.Scopes, tc.scopes) {
		t.Fatalf("%q: got %+v; want %s scopes %v", tc.args, op, tc.action, tc.scopes)
	}
	for key, want := range tc.params {
		if !reflect.DeepEqual(op.Parameters[key], want) {
			t.Errorf("%q parameter %s = %#v; want %#v", tc.args, key, op.Parameters[key], want)
		}
	}
	if !strings.Contains(op.Origin.Source, "builtin/") || !strings.Contains(op.Origin.Source, ":cmd_") {
		t.Errorf("source entrypoint missing: %+v", op.Origin)
	}
	if op.Effect == model.EffectWrite && (r.Classification.RetrySafe || r.Classification.MutationFree) {
		t.Errorf("write marked safe: %+v", r)
	}
	return op
}

func TestWorktreeResetModes(t *testing.T) {
	for _, mode := range []string{"soft", "mixed", "hard", "merge", "keep"} {
		t.Run(mode, func(t *testing.T) {
			scopes := []model.Scope{model.ScopeLocalRef}
			if mode != "soft" {
				scopes = append(scopes, model.ScopeIndex)
			}
			if mode == "hard" || mode == "merge" || mode == "keep" {
				scopes = append(scopes, model.ScopeWorktree)
			}
			op := whCheckCase(t, whCase{[]string{"reset", "--" + mode, "HEAD~2"}, "worktree.reset." + mode, scopes, map[string]any{"mode": mode}})
			if !reflect.DeepEqual(op.Target.Revisions, model.Exact("HEAD~2")) || op.Target.Refs.Kind != model.SetUnknown || op.Coverage != model.CoverageSemantic {
				t.Fatal(op)
			}
		})
	}
	for _, tc := range []whCase{
		{[]string{"reset"}, "worktree.reset.mixed", []model.Scope{model.ScopeLocalRef, model.ScopeIndex}, map[string]any{"refresh": true}},
		{[]string{"reset", "--hard", "--soft"}, "worktree.reset.soft", []model.Scope{model.ScopeLocalRef}, nil},
		{[]string{"reset", "--har"}, "worktree.reset.hard", []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree}, nil},
		{[]string{"reset", "--", "file"}, "worktree.reset.paths", []model.Scope{model.ScopeIndex}, map[string]any{"pathspecs": []string{"file"}}},
		{[]string{"reset", "HEAD~1", "--", "file"}, "worktree.reset.paths", []model.Scope{model.ScopeIndex}, nil},
		{[]string{"reset", "--mixed", "--no-refresh", "-N", "--", "file"}, "worktree.reset.paths", []model.Scope{model.ScopeIndex}, map[string]any{"refresh": false, "intentToAdd": true}},
		{[]string{"reset", "--no-refresh", "--refresh"}, "worktree.reset.mixed", []model.Scope{model.ScopeLocalRef, model.ScopeIndex}, map[string]any{"refresh": true}},
		{[]string{"reset", "-qp", "--", "file"}, "worktree.reset.patch", []model.Scope{model.ScopeIndex}, nil},
		{[]string{"reset", "--hard", "--recurse-submodules"}, "worktree.reset.hard", []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeSubmodule}, map[string]any{"recurseSubmodules": true}},
		{[]string{"reset", "--hard", "--recurse-submodules", "--no-recurse-submodules"}, "worktree.reset.hard", []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree}, map[string]any{"recurseSubmodules": false}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) { whCheckCase(t, tc) })
	}
	for _, args := range [][]string{{"reset", "file"}, {"reset", "tree", "file"}} {
		op := Normalize(model.Invocation{Argv: args}).Operations[0]
		if op.Coverage != model.CoverageConservative || len(op.Unresolved) == 0 || op.Target.Paths.Kind != model.SetUnknown {
			t.Fatalf("ambiguous operand guessed: %+v", op)
		}
	}
}

func TestWorktreeRestoreCheckoutSwitch(t *testing.T) {
	for _, tc := range []whCase{
		{[]string{"restore", "--", "a"}, "worktree.restore.paths", []model.Scope{model.ScopeWorktree}, map[string]any{"source": "index", "worktree": true, "staged": false}},
		{[]string{"restore", "--staged", "a"}, "worktree.restore.paths", []model.Scope{model.ScopeIndex}, map[string]any{"source": "HEAD", "worktree": false, "staged": true}},
		{[]string{"restore", "-SW", "-sHEAD~1", "a"}, "worktree.restore.paths", []model.Scope{model.ScopeIndex, model.ScopeWorktree}, map[string]any{"source": "HEAD~1"}},
		{[]string{"restore", "--staged", "--no-staged", "--worktree", "a"}, "worktree.restore.paths", []model.Scope{model.ScopeWorktree}, map[string]any{"staged": false}},
		{[]string{"restore", "--ours", "a"}, "worktree.restore.paths", []model.Scope{model.ScopeWorktree}, map[string]any{"stage": "ours"}},
		{[]string{"restore", "--ours", "--theirs", "a"}, "worktree.restore.paths", []model.Scope{model.ScopeWorktree}, map[string]any{"stage": "theirs"}},
		{[]string{"restore", "-p", "a"}, "worktree.restore.paths", []model.Scope{model.ScopeWorktree}, nil},
		{[]string{"checkout", "--", "a"}, "worktree.checkout.paths", []model.Scope{model.ScopeWorktree}, map[string]any{"source": "index"}},
		{[]string{"checkout", "HEAD~1", "--", "a"}, "worktree.checkout.paths", []model.Scope{model.ScopeIndex, model.ScopeWorktree}, map[string]any{"source": "HEAD~1"}},
		{[]string{"checkout", "--theirs", "--", "a"}, "worktree.checkout.paths", []model.Scope{model.ScopeWorktree}, map[string]any{"stage": "theirs"}},
		{[]string{"checkout", "-qf", "--", "a"}, "worktree.checkout.paths", []model.Scope{model.ScopeWorktree}, map[string]any{"force": true}},
		{[]string{"checkout", "-btopic"}, "worktree.checkout.create", []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeConfig}, map[string]any{"newBranch": "topic"}},
		{[]string{"checkout", "-Btopic"}, "worktree.checkout.reset", []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeConfig}, nil},
		{[]string{"checkout", "--orphan", "topic"}, "worktree.checkout.orphan", []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeConfig}, nil},
		{[]string{"checkout", "--detach", "HEAD~1"}, "worktree.checkout.detach", []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree}, nil},
		{[]string{"switch", "main"}, "worktree.switch.switch", []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeConfig}, nil},
		{[]string{"switch", "-c", "topic"}, "worktree.switch.create", []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeConfig}, nil},
		{[]string{"switch", "-C", "topic", "HEAD~1"}, "worktree.switch.reset", []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeConfig}, nil},
		{[]string{"switch", "--orphan", "topic"}, "worktree.switch.orphan", []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeConfig}, nil},
		{[]string{"switch", "--detach"}, "worktree.switch.detach", []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree}, nil},
		{[]string{"switch", "--discard-changes", "main"}, "worktree.switch.switch", []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeConfig}, map[string]any{"force": true}},
		{[]string{"switch", "-f", "--no-force", "main"}, "worktree.switch.switch", []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeConfig}, map[string]any{"force": false}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) { whCheckCase(t, tc) })
	}
	for _, args := range [][]string{{"checkout", "file"}, {"checkout", "tree", "file"}} {
		op := Normalize(model.Invocation{Argv: args}).Operations[0]
		if op.Coverage != model.CoverageConservative || op.Target.Paths.Kind != model.SetUnknown {
			t.Fatal(op)
		}
	}
	for _, command := range []string{"checkout", "switch"} {
		flag := "-b"
		if command == "switch" {
			flag = "-c"
		}
		op := Normalize(model.Invocation{Argv: []string{command, flag, "topic"}}).Operations[0]
		if !reflect.DeepEqual(op.Target.Refs, model.Exact("refs/heads/topic")) {
			t.Fatal(op)
		}
	}
}

func TestWorktreeCleanAndIndexModes(t *testing.T) {
	for _, tc := range []whCase{
		{[]string{"clean", "-n"}, "worktree.clean.preview", []model.Scope{model.ScopeWorktree}, map[string]any{"dryRun": true}},
		{[]string{"clean", "-fdx"}, "worktree.clean", []model.Scope{model.ScopeWorktree}, map[string]any{"forceCount": 1, "ignored": "include", "directories": true, "nestedRepositories": false}},
		{[]string{"clean", "-ffX"}, "worktree.clean", []model.Scope{model.ScopeWorktree}, map[string]any{"forceCount": 2, "ignored": "only", "nestedRepositories": true}},
		{[]string{"clean", "-ff", "--no-force", "-f"}, "worktree.clean", []model.Scope{model.ScopeWorktree}, map[string]any{"forceCount": 1}},
		{[]string{"clean", "-f", "-e*.o", "--exclude=keep", "dir"}, "worktree.clean", []model.Scope{model.ScopeWorktree}, map[string]any{"directories": true, "exclude": []string{"*.o", "keep"}}},
		{[]string{"clean", "-i"}, "worktree.clean", []model.Scope{model.ScopeWorktree}, nil},
		{[]string{"add", "-A", "--chmod=+x", "file"}, "index.add.update", []model.Scope{model.ScopeIndex, model.ScopeObjects}, map[string]any{"all": true, "chmod": "+x"}},
		{[]string{"add", "-uN"}, "index.add.update", []model.Scope{model.ScopeIndex, model.ScopeObjects}, map[string]any{"update": true, "intentToAdd": true}},
		{[]string{"add", "--all", "--ignore-removal", "file"}, "index.add.update", []model.Scope{model.ScopeIndex, model.ScopeObjects}, map[string]any{"all": false}},
		{[]string{"add", "--no-all", "--no-ignore-removal", "file"}, "index.add.update", []model.Scope{model.ScopeIndex, model.ScopeObjects}, map[string]any{"all": true}},
		{[]string{"add", "--renormalize", "file"}, "index.add.update", []model.Scope{model.ScopeIndex, model.ScopeObjects}, map[string]any{"update": true}},
		{[]string{"add", "-n", "file"}, "index.add.preview", []model.Scope{model.ScopeIndex}, map[string]any{"dryRun": true}},
		{[]string{"add", "--refresh", "file"}, "index.add.refresh", []model.Scope{model.ScopeIndex}, nil},
		{[]string{"add", "-p"}, "index.add.update", []model.Scope{model.ScopeIndex, model.ScopeObjects}, nil},
		{[]string{"add", "-i"}, "index.add.update", []model.Scope{model.ScopeIndex, model.ScopeObjects}, nil},
		{[]string{"add", "-e", "file"}, "index.add.update", []model.Scope{model.ScopeIndex, model.ScopeObjects}, nil},
		{[]string{"rm", "--cached", "-rf", "--", "dir"}, "index.rm.remove", []model.Scope{model.ScopeIndex}, map[string]any{"cached": true, "recursive": true, "force": true}},
		{[]string{"rm", "file"}, "index.rm.remove", []model.Scope{model.ScopeIndex, model.ScopeWorktree, model.ScopeSubmodule}, nil},
		{[]string{"rm", "-n", "--cached", "file"}, "index.rm.preview", []model.Scope{model.ScopeIndex}, nil},
		{[]string{"rm", "-n", "file"}, "index.rm.preview", []model.Scope{model.ScopeIndex, model.ScopeWorktree, model.ScopeSubmodule}, nil},
		{[]string{"mv", "-f", "a", "b"}, "worktree.mv.move", []model.Scope{model.ScopeIndex, model.ScopeWorktree, model.ScopeSubmodule, model.ScopeConfig}, map[string]any{"operands": []string{"a", "b"}, "force": true}},
		{[]string{"mv", "-n", "a", "b", "dir"}, "worktree.mv.preview", []model.Scope{model.ScopeIndex, model.ScopeWorktree}, map[string]any{"dryRun": true}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) { whCheckCase(t, tc) })
	}
	r := Normalize(model.Invocation{Argv: []string{"clean", "-n"}})
	if !r.Classification.MutationFree || !r.Classification.RetrySafe {
		t.Fatal(r)
	}
	op := Normalize(model.Invocation{Argv: []string{"clean"}}).Operations[0]
	if !slices.ContainsFunc(op.Unresolved, func(u model.Unresolved) bool { return u.Field == "parameters.requireForce" && u.Source == "git" }) {
		t.Fatal(op)
	}
	for _, args := range [][]string{{"clean", "-i"}, {"reset", "-p"}, {"restore", "-p", "file"}, {"checkout", "-p"}, {"add", "-p"}, {"add", "-i"}, {"add", "-e"}} {
		r := Normalize(model.Invocation{Argv: args})
		if r.Classification.Coverage != model.CoverageOpaque || r.Operations[0].UnknownReason != model.UnknownInteractive || !slices.Contains(r.Operations[0].Capabilities, model.CapabilityInteractive) || r.Classification.RetrySafe {
			t.Fatalf("%q: %+v", args, r)
		}
	}
}

func TestStageMatchesAdd(t *testing.T) {
	for _, args := range [][]string{{"file"}, {"-A"}, {"-p"}, {"-n", "file"}, {"--refresh", "file"}} {
		add := Normalize(model.Invocation{Argv: append([]string{"add"}, args...)})
		stage := Normalize(model.Invocation{Argv: append([]string{"stage"}, args...)})
		if !reflect.DeepEqual(stage.Classification, add.Classification) || len(stage.Operations) != len(add.Operations) {
			t.Fatalf("stage differs from add: %+v; add: %+v", stage, add)
		}
		for i, op := range stage.Operations {
			if !reflect.DeepEqual(op.Origin.Command, []string{"stage"}) {
				t.Fatal(op.Origin)
			}
			op.Origin.Command = add.Operations[i].Origin.Command
			if !reflect.DeepEqual(op, add.Operations[i]) {
				t.Fatalf("stage %q: %+v; add: %+v", args, op, add.Operations[i])
			}
		}
	}
}

func TestWorktreeStashModes(t *testing.T) {
	for _, mode := range []string{"push", "save", "pop", "apply", "drop", "clear", "branch", "store", "create", "list", "show", "export", "import"} {
		t.Run(mode, func(t *testing.T) {
			args := []string{"stash", mode}
			scopes := []model.Scope{model.ScopeStash}
			action := mode
			switch mode {
			case "save", "push":
				action = "push"
				scopes = append(scopes, model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects)
			case "pop", "branch":
				scopes = append(scopes, model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects)
				if mode == "branch" {
					args = append(args, "topic", "stash@{1}")
					scopes = append(scopes, model.ScopeLocalRef, model.ScopeConfig)
				}
			case "apply":
				scopes = []model.Scope{model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects}
			case "create":
				scopes = []model.Scope{model.ScopeObjects, model.ScopeIndex}
			case "export":
				scopes = []model.Scope{model.ScopeObjects}
			}
			whCheckCase(t, whCase{args, "stash." + action, scopes, nil})
		})
	}
	for _, args := range [][]string{{"stash"}, {"stash", "--", "file"}, {"stash", "-m", "message"}} {
		whCheckCase(t, whCase{args, "stash.push", []model.Scope{model.ScopeStash, model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects}, nil})
	}
	for _, tc := range []struct {
		args   []string
		params map[string]any
	}{
		{[]string{"stash", "push", "-aku"}, map[string]any{"untracked": "include", "keepIndex": true}},
		{[]string{"stash", "push", "-p", "--no-keep-index"}, map[string]any{"keepIndex": false}},
		{[]string{"stash", "push", "-S"}, map[string]any{"staged": true}},
		{[]string{"stash", "apply", "--index", "stash@{2}"}, map[string]any{"reinstateIndex": true, "stash": "stash@{2}"}},
	} {
		op := Normalize(model.Invocation{Argv: tc.args}).Operations[0]
		for k, want := range tc.params {
			if !reflect.DeepEqual(op.Parameters[k], want) {
				t.Fatalf("%q: %+v, want %s=%v", tc.args, op, k, want)
			}
		}
	}
	op := Normalize(model.Invocation{Argv: []string{"stash", "save", "one", "two"}}).Operations[0]
	if op.Texts.Body.Value != "one two" || op.Editable != nil {
		t.Fatal(op)
	}
}

func TestWorktreeApplyAndSparseModes(t *testing.T) {
	for _, tc := range []whCase{
		{[]string{"apply", "--check", "patch"}, "worktree.apply.check", []model.Scope{model.ScopeWorktree}, nil},
		{[]string{"apply", "--stat", "patch"}, "worktree.apply.stat", []model.Scope{model.ScopeWorktree}, nil},
		{[]string{"apply", "--numstat", "patch"}, "worktree.apply.stat", []model.Scope{model.ScopeWorktree}, nil},
		{[]string{"apply", "--summary", "patch"}, "worktree.apply.stat", []model.Scope{model.ScopeWorktree}, nil},
		{[]string{"apply", "--stat", "--apply", "patch"}, "worktree.apply.patch", []model.Scope{model.ScopeWorktree}, nil},
		{[]string{"apply", "--index", "patch"}, "worktree.apply.patch", []model.Scope{model.ScopeIndex, model.ScopeWorktree}, map[string]any{"index": true}},
		{[]string{"apply", "--cached", "patch"}, "worktree.apply.patch", []model.Scope{model.ScopeIndex}, map[string]any{"cached": true}},
		{[]string{"apply", "-3", "patch"}, "worktree.apply.patch", []model.Scope{model.ScopeIndex, model.ScopeWorktree}, map[string]any{"index": true}},
		{[]string{"apply", "--reject", "-R", "patch"}, "worktree.apply.patch", []model.Scope{model.ScopeWorktree}, map[string]any{"reject": true, "reverse": true}},
		{[]string{"sparse-checkout", "list"}, "worktree.sparse-checkout.list", []model.Scope{model.ScopeConfig, model.ScopeWorktree}, nil},
		{[]string{"sparse-checkout", "check-rules"}, "worktree.sparse-checkout.check-rules", []model.Scope{model.ScopeConfig, model.ScopeWorktree}, nil},
		{[]string{"sparse-checkout", "clean", "-n"}, "worktree.sparse-checkout.preview", []model.Scope{model.ScopeWorktree}, nil},
		{[]string{"sparse-checkout", "clean", "-f"}, "worktree.sparse-checkout.clean", []model.Scope{model.ScopeWorktree}, nil},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) { whCheckCase(t, tc) })
	}
	for _, mode := range []string{"init", "set", "add", "reapply", "disable"} {
		whCheckCase(t, whCase{[]string{"sparse-checkout", mode}, "worktree.sparse-checkout." + mode, []model.Scope{model.ScopeConfig, model.ScopeIndex, model.ScopeWorktree}, nil})
	}
	op := Normalize(model.Invocation{Argv: []string{"sparse-checkout", "set", "--no-cone", "--cone", "--no-sparse-index", "dir"}}).Operations[0]
	if op.Parameters["cone"] != true || op.Parameters["sparseIndex"] != false {
		t.Fatal(op)
	}
}

func TestWorktreePathspecInputsAndTerminators(t *testing.T) {
	for _, tc := range []struct {
		data string
		nul  bool
		want []string
	}{
		{"one\r\n\"two\\tthree\"\n", false, []string{"one", "two\tthree"}},
		{"one\x00two\nthree\x00", true, []string{"one", "two\nthree"}},
	} {
		args := []string{"add", "--pathspec-from-file=-"}
		if tc.nul {
			args = append(args, "--pathspec-file-nul")
		}
		op := Normalize(model.Invocation{Argv: args, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte(tc.data)}}).Operations[0]
		if !reflect.DeepEqual(op.Parameters["pathspecs"], tc.want) || op.Target.Paths.Kind != model.SetUnknown {
			t.Fatal(op)
		}
	}
	for _, args := range [][]string{{"add", "--", "--force"}, {"add", "--end-of-options", "--force"}} {
		op := Normalize(model.Invocation{Argv: args}).Operations[0]
		if !reflect.DeepEqual(op.Parameters["pathspecs"], []string{"--force"}) {
			t.Fatal(op)
		}
	}
	for _, inv := range []model.Invocation{
		{Argv: []string{"add", "--pathspec-from-file=-"}},
		{Argv: []string{"add", "--pathspec-from-file=file"}, ReadFile: func(string) ([]byte, error) { return nil, errors.New("unavailable") }},
	} {
		op := Normalize(inv).Operations[0]
		if op.Coverage != model.CoverageOpaque || op.UnknownReason != model.UnknownInputUnavailable || op.Target.Paths.Kind != model.SetUnknown {
			t.Fatal(op)
		}
	}
}

func TestWorktreeEmptyPathspecFileCanSelectWholeTree(t *testing.T) {
	for _, command := range []string{"reset", "checkout"} {
		for _, content := range []string{"", "file\n"} {
			args := []string{command, "--pathspec-from-file=paths"}
			if command == "reset" {
				args = append(args, "--hard")
			}
			op := Normalize(model.Invocation{Argv: args, ReadFile: func(string) ([]byte, error) { return []byte(content), nil }}).Operations[0]
			if content == "" && !slices.Contains(op.Scopes, model.ScopeLocalRef) {
				t.Fatalf("empty pathspec input can change refs: %+v", op)
			}
			if content != "" && slices.Contains(op.Scopes, model.ScopeLocalRef) {
				t.Fatalf("nonempty pathspec input must not select ref reset/switch: %+v", op)
			}
		}
		op := Normalize(model.Invocation{Argv: []string{command, "--pathspec-from-file=-"}}).Operations[0]
		if !slices.Contains(op.Scopes, model.ScopeLocalRef) || op.Coverage != model.CoverageOpaque {
			t.Fatalf("unavailable pathspec input must include an empty input's effects: %+v", op)
		}
	}
}

func TestWorktreeModesAgainstGit(t *testing.T) {
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	home := t.TempDir()
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.test", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.test"}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %q: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	path := filepath.Join(dir, "file")
	if err := os.WriteFile(path, []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "file")
	tree := git("write-tree")
	head := git("commit-tree", tree, "-m", "initial")
	git("update-ref", "HEAD", head)
	if err := os.WriteFile(path, []byte("modified\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "file")
	git("reset", "--", "file")
	if git("rev-parse", "HEAD") != head || git("show", ":file") != "original" {
		t.Fatal("path reset changed HEAD or did not restore index")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "modified\n" {
		t.Fatal("path reset changed worktree", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty-pathspec"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	git("reset", "--hard", "--pathspec-from-file=empty-pathspec")
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "original\n" {
		t.Fatal("hard reset did not restore worktree", err)
	}
	untracked := filepath.Join(dir, "untracked")
	if err := os.WriteFile(untracked, []byte("keep\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("clean", "-n")
	if _, err := os.Stat(untracked); err != nil {
		t.Fatal("clean dry-run removed file", err)
	}
	git("clean", "-f")
	if _, err := os.Stat(untracked); !os.IsNotExist(err) {
		t.Fatal("clean force did not remove file", err)
	}
}
