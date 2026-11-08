package git

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/protocol"
)

func TestHistoryCommitModes(t *testing.T) {
	write := []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeObjects}
	read := []model.Scope{model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects}
	for _, tc := range []whCase{
		{[]string{"commit", "-m", "message"}, "history.commit.create", write, map[string]any{"amend": false}},
		{[]string{"commit", "--amend", "--no-edit"}, "history.commit.amend", write, map[string]any{"amend": true}},
		{[]string{"commit", "--amend", "--no-amend", "-m", "message"}, "history.commit.create", write, nil},
		{[]string{"commit", "-ammessage", "--allow-empty", "--no-verify"}, "history.commit.create", write, map[string]any{"all": true, "allowEmpty": true, "noVerify": true}},
		{[]string{"commit", "--fixup=amend:HEAD"}, "history.commit.create", write, map[string]any{"fixup": "amend:HEAD"}},
		{[]string{"commit", "--squash=HEAD", "-m", "message"}, "history.commit.create", write, map[string]any{"squash": "HEAD"}},
		{[]string{"commit", "--dry-run"}, "history.commit.preview", read, map[string]any{"dryRun": true}},
		{[]string{"commit", "--short"}, "history.commit.preview", read, nil},
		{[]string{"commit", "--porcelain"}, "history.commit.preview", read, nil},
		{[]string{"commit", "--long"}, "history.commit.preview", read, nil},
		{[]string{"commit", "-z"}, "history.commit.preview", read, nil},
		{[]string{"commit", "--porcelain", "--no-short", "-m", "message"}, "history.commit.create", write, nil},
		{[]string{"commit", "--no-verify", "--verify", "-m", "message"}, "history.commit.create", write, map[string]any{"noVerify": false}},
		{[]string{"commit", "--amend", "--only", "-m", "message"}, "history.commit.amend", write, map[string]any{"only": true}},
		{[]string{"commit", "--include", "-m", "message", "file"}, "history.commit.create", write, map[string]any{"include": true}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) { whCheckCase(t, tc) })
	}
	for _, tc := range []struct {
		args   []string
		editor bool
	}{
		{[]string{"commit"}, true},
		{[]string{"commit", "-m", "message"}, false},
		{[]string{"commit", "-m", "message", "-e"}, true},
		{[]string{"commit", "--amend", "--no-edit"}, false},
		{[]string{"commit", "-CHEAD"}, false},
		{[]string{"commit", "-cHEAD"}, true},
		{[]string{"commit", "-cHEAD", "--no-edit"}, false},
		{[]string{"commit", "--fixup=HEAD"}, false},
		{[]string{"commit", "--fixup=amend:HEAD"}, true},
		{[]string{"commit", "--fixup=reword:HEAD"}, true},
		{[]string{"commit", "--fixup=amend:HEAD", "--no-edit"}, false},
		{[]string{"commit", "--dry-run"}, false},
	} {
		op := Normalize(model.Invocation{Argv: tc.args}).Operations[0]
		if slices.Contains(op.Capabilities, model.CapabilityEditor) != tc.editor {
			t.Errorf("%q editor=%v: %+v", tc.args, tc.editor, op)
		}
	}
	for _, args := range [][]string{{"commit", "-m", "message", "-n"}, {"merge", "-m", "message", "--no-verify", "topic"}} {
		op := Normalize(model.Invocation{Argv: args}).Operations[0]
		if args[0] == "commit" && slices.Contains(op.Capabilities, model.CapabilityExternalExec) {
			t.Fatalf("native hook classified as external execution: %+v", op)
		}
		withVerify := append(append([]string(nil), args...), "--verify")
		verified := Normalize(model.Invocation{Argv: withVerify}).Operations[0]
		if !reflect.DeepEqual(op.Capabilities, verified.Capabilities) {
			t.Fatalf("native hook verification changed capabilities: %+v / %+v", op, verified)
		}
	}
}

func TestHistoryMergeRebaseModes(t *testing.T) {
	write := []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects}
	for _, tc := range []whCase{
		{[]string{"merge", "topic"}, "history.merge.start", write, map[string]any{"commit": true, "fastForward": "allow"}},
		{[]string{"merge", "--abort"}, "history.merge.abort", write, nil},
		{[]string{"merge", "--continue"}, "history.merge.continue", write, nil},
		{[]string{"merge", "--quit"}, "history.merge.quit", []model.Scope{model.ScopeWorktreeAdmin, model.ScopeStash}, nil},
		{[]string{"merge", "--squash", "topic"}, "history.merge.squash", write, map[string]any{"squash": true, "commit": false}},
		{[]string{"merge", "--ff-only", "topic"}, "history.merge.start", write, map[string]any{"fastForward": "only"}},
		{[]string{"merge", "--ff-only", "--no-ff", "topic"}, "history.merge.start", write, map[string]any{"fastForward": "never"}},
		{[]string{"merge", "--no-ff", "--ff", "topic"}, "history.merge.start", write, map[string]any{"fastForward": "allow"}},
		{[]string{"merge", "--no-commit", "topic"}, "history.merge.start", write, map[string]any{"commit": false}},
		{[]string{"rebase", "main"}, "history.rebase.start", write, map[string]any{"root": false}},
		{[]string{"rebase", "--onto=main", "base", "topic"}, "history.rebase.start", write, map[string]any{"onto": "main"}},
		{[]string{"rebase", "--root", "--autosquash", "--no-update-refs"}, "history.rebase.start", write, map[string]any{"root": true, "autosquash": true, "updateRefs": false}},
		{[]string{"rebase", "--update-refs", "--no-update-refs", "--update-refs", "main"}, "history.rebase.start", write, map[string]any{"updateRefs": true}},
		{[]string{"rebase", "--exec", "echo one", "-xecho two", "main"}, "history.rebase.start", write, map[string]any{"exec": []string{"echo one", "echo two"}}},
		{[]string{"rebase", "--quit"}, "history.rebase.quit", []model.Scope{model.ScopeWorktreeAdmin, model.ScopeStash}, nil},
		{[]string{"rebase", "--edit-todo"}, "history.rebase.edit-todo", []model.Scope{model.ScopeWorktreeAdmin}, nil},
		{[]string{"rebase", "--show-current-patch"}, "history.rebase.show-current-patch", []model.Scope{model.ScopeObjects, model.ScopeWorktreeAdmin}, nil},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) { whCheckCase(t, tc) })
	}
	for _, mode := range []string{"continue", "abort", "skip"} {
		whCheckCase(t, whCase{[]string{"rebase", "--" + mode}, "history.rebase." + mode, write, nil})
	}
	for _, args := range [][]string{{"rebase", "-i", "main"}, {"rebase", "--edit-todo"}} {
		r := Normalize(model.Invocation{Argv: args})
		if r.Classification.Coverage != model.CoverageOpaque || r.Operations[0].UnknownReason != model.UnknownInteractive || !slices.Contains(r.Operations[0].Capabilities, model.CapabilityEditor) {
			t.Fatal(r)
		}
	}
	for _, args := range [][]string{{"rebase", "--exec=echo ok", "main"}, {"rebase", "--continue"}, {"rebase", "--skip"}} {
		r := Normalize(model.Invocation{Argv: args})
		if !slices.Contains(r.Operations[0].Capabilities, model.CapabilityExternalExec) || r.Classification.RetrySafe {
			t.Fatal(r)
		}
	}
}

func TestHistorySequencerAndAM(t *testing.T) {
	write := []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects}
	for _, command := range []string{"cherry-pick", "revert", "am"} {
		for _, mode := range []string{"start", "continue", "abort", "skip", "quit"} {
			args := []string{command}
			if mode != "start" {
				args = append(args, "--"+mode)
			} else if command != "am" {
				args = append(args, "HEAD~1")
			}
			scopes := write
			if mode == "quit" {
				scopes = []model.Scope{model.ScopeWorktreeAdmin}
			}
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				whCheckCase(t, whCase{args, "history." + command + "." + mode, scopes, nil})
			})
		}
	}
	for _, command := range []string{"cherry-pick", "revert"} {
		op := whCheckCase(t, whCase{[]string{command, "-n", "-m2", "HEAD"}, "history." + command + ".apply", []model.Scope{model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects}, map[string]any{"noCommit": true, "mainline": "2"}})
		if op.Target.Refs != nil || !reflect.DeepEqual(op.Target.Revisions, model.Exact("HEAD")) {
			t.Fatal(op)
		}
	}
	for _, mode := range []string{"retry", "allow-empty"} {
		whCheckCase(t, whCase{[]string{"am", "--" + mode}, "history.am." + mode, write, nil})
	}
	whCheckCase(t, whCase{[]string{"am", "-r"}, "history.am.continue", write, nil})
	whCheckCase(t, whCase{[]string{"am", "--show-current-patch=raw"}, "history.am.show-current-patch", []model.Scope{model.ScopeWorktreeAdmin}, nil})
	for _, command := range []string{"am", "apply"} {
		r := Normalize(model.Invocation{Argv: []string{command}})
		if r.Operations[0].UnknownReason != model.UnknownInputUnavailable || r.Classification.Coverage != model.CoverageOpaque {
			t.Fatal(r)
		}
		r = Normalize(model.Invocation{Argv: []string{command}, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte("patch input")}})
		if r.Operations[0].UnknownReason != model.UnknownNone || r.Operations[0].Target.Paths.Kind != model.SetUnknown {
			t.Fatal(r)
		}
	}
}

func TestHistoryNotesAndBisect(t *testing.T) {
	for _, mode := range []string{"add", "append", "copy", "edit", "remove", "prune", "merge", "list", "show", "get-ref"} {
		args := []string{"notes", "--ref=review", mode}
		scopes := []model.Scope{model.ScopeLocalRef, model.ScopeObjects}
		if mode == "merge" {
			scopes = append(scopes, model.ScopeWorktreeAdmin)
		}
		op := whCheckCase(t, whCase{args, "notes." + mode, scopes, nil})
		if !reflect.DeepEqual(op.Target.Refs, model.Exact("refs/notes/review")) {
			t.Fatal(op)
		}
	}
	whCheckCase(t, whCase{[]string{"notes"}, "notes.list", []model.Scope{model.ScopeLocalRef, model.ScopeObjects}, nil})
	whCheckCase(t, whCase{[]string{"notes", "prune", "-n"}, "notes.preview", []model.Scope{model.ScopeLocalRef, model.ScopeObjects}, map[string]any{"dryRun": true}})
	for _, ref := range []string{"notes/review", "refs/notes/review"} {
		op := Normalize(model.Invocation{Argv: []string{"notes", "list"}, Env: map[string]string{"GIT_NOTES_REF": ref}}).Operations[0]
		if !reflect.DeepEqual(op.Target.Refs, model.Exact("refs/notes/review")) {
			t.Fatal(op)
		}
	}
	for _, mode := range []string{"start", "good", "bad", "old", "new", "reset", "skip", "next", "run", "replay", "log", "terms", "visualize", "view"} {
		args := []string{"bisect", mode}
		action := mode
		switch mode {
		case "old":
			action = "good"
		case "new":
			action = "bad"
		case "view":
			action = "visualize"
		}
		scopes := []model.Scope{model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeWorktreeAdmin}
		if action == "log" || action == "terms" || action == "visualize" {
			scopes = []model.Scope{model.ScopeLocalRef, model.ScopeWorktreeAdmin}
		}
		if mode == "run" {
			args = append(args, "echo", "ok")
		}
		t.Run(strings.Join(args, " "), func(t *testing.T) { whCheckCase(t, whCase{args, "history.bisect." + action, scopes, nil}) })
	}
	op := Normalize(model.Invocation{Argv: []string{"bisect", "run", "echo", "ok"}}).Operations[0]
	if !slices.Contains(op.Capabilities, model.CapabilityExternalExec) || !reflect.DeepEqual(op.Parameters["command"], []string{"echo", "ok"}) {
		t.Fatal(op)
	}
}

func TestHistoryMessageSourcesAndLocators(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		value  string
		source string
		kind   string
		field  string
	}{
		{[]string{"commit", "-mone", "-m", "two"}, "one\n\ntwo", "inline", "argv", "body"},
		{[]string{"commit", "-mone", "--no-message", "-m", "two"}, "two", "inline", "argv", "body"},
		{[]string{"commit", "-F", "message"}, "file contents", "file", "file", "body"},
		{[]string{"commit", "-Fmessage", "-F", "last"}, "last contents", "file", "file", "body"},
		{[]string{"commit", "-F-"}, "stdin contents", "stdin", "stdin", "body"},
		{[]string{"merge", "-mone", "-mtwo", "topic"}, "one\n\ntwo", "inline", "argv", "body"},
		{[]string{"merge", "-F-", "topic"}, "file contents", "file", "file", "body"},
		{[]string{"notes", "add", "-mone", "-mtwo"}, "one\n\ntwo", "inline", "argv", "notes"},
		{[]string{"notes", "append", "-F-"}, "stdin contents", "stdin", "stdin", "notes"},
		{[]string{"stash", "push", "-mmessage"}, "message", "inline", "argv", "body"},
	} {
		inv := model.Invocation{Argv: tc.args, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte("stdin contents")}, ReadFile: func(path string) ([]byte, error) {
			if path == "last" {
				return []byte("last contents"), nil
			}
			return []byte("file contents"), nil
		}}
		op := Normalize(inv).Operations[0]
		var text *model.Text
		if op.Texts != nil {
			text = op.Texts.Body
			if tc.field == "notes" {
				text = op.Texts.Notes
			}
		}
		if text == nil || text.Value != tc.value || text.Source != tc.source || text.Bytes != int64(len(tc.value)) {
			t.Fatalf("%q: %+v, expected %q from %s", tc.args, op, tc.value, tc.source)
		}
		loc, ok := op.Editable["texts/"+tc.field+"/value"]
		if !ok || loc.Kind != tc.kind || len(loc.Spans) == 0 {
			t.Fatalf("%q locator: %+v", tc.args, op.Editable)
		}
		for key := range op.Editable {
			if err := protocol.ValidateTransformPath("/operations/0/" + key); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, args := range [][]string{{"commit", "-mmessage", "--edit"}, {"commit", "--fixup=HEAD", "-mmessage"}, {"notes", "add", "-mone", "-Fmessage"}, {"notes", "add", "-mone", "--separator=break"}} {
		op := Normalize(model.Invocation{Argv: args, ReadFile: func(string) ([]byte, error) { return []byte("message"), nil }}).Operations[0]
		for key := range op.Editable {
			if strings.HasPrefix(key, "texts/") {
				t.Fatalf("ambiguous/editor input is editable for %q: %+v", args, op)
			}
		}
	}
	for _, status := range []model.StdinStatus{model.StdinNotCaptured, model.StdinTTY, model.StdinTooLarge, model.StdinTimeout, model.StdinError} {
		op := Normalize(model.Invocation{Argv: []string{"commit", "-F-"}, Stdin: model.StdinData{Status: status}}).Operations[0]
		if op.Coverage != model.CoverageOpaque || op.Texts.Body.Source != "stdin" || op.Texts.Body.Status != status || len(op.Editable) != 0 {
			t.Fatal(op)
		}
		if status == model.StdinTooLarge && (!op.Texts.Body.Truncated || op.UnknownReason != model.UnknownInputTooLarge) {
			t.Fatal(op)
		}
	}
	op := Normalize(model.Invocation{Argv: []string{"commit", "-Fmessage"}, ReadFile: func(string) ([]byte, error) { return nil, errors.New("unavailable") }}).Operations[0]
	if op.Coverage != model.CoverageOpaque || op.Texts.Body.Source != "file" || op.Texts.Body.Path != "message" || len(op.Editable) != 0 {
		t.Fatal(op)
	}
}

func TestHistorySideChannels(t *testing.T) {
	for _, args := range [][]string{{"commit", "-mmessage", "-S"}, {"merge", "-mmessage", "-S", "topic"}, {"rebase", "-S", "main"}, {"cherry-pick", "-S", "HEAD"}, {"am", "-S", "mailbox"}, {"stash", "show"}, {"stash", "show", "--no-textconv", "--ext-diff"}} {
		r := Normalize(model.Invocation{Argv: args})
		if !slices.Contains(r.Operations[0].Capabilities, model.CapabilityExternalExec) || r.Classification.RetrySafe {
			t.Fatalf("%q: %+v", args, r)
		}
	}
	for _, args := range [][]string{{"stash", "show", "--output=out"}, {"apply", "--build-fake-ancestor=out", "patch"}, {"apply", "--reject", "patch"}} {
		r := Normalize(model.Invocation{Argv: args})
		if !slices.Contains(r.Operations[0].Capabilities, model.CapabilityFileWrite) || r.Classification.RetrySafe {
			t.Fatalf("%q: %+v", args, r)
		}
	}
	r := Normalize(model.Invocation{Argv: []string{"stash", "show", "--no-textconv"}})
	if !r.Classification.MutationFree || !r.Classification.RetrySafe {
		t.Fatal(r)
	}
}
