package resolve

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	gitnormalize "github.com/mkusaka/gthok/internal/normalize/git"
)

func normalizeFill(t *testing.T, f *fixture, r *Resolver, args ...string) model.Result {
	t.Helper()
	res := gitnormalize.Normalize(model.Invocation{Argv: args, Cwd: r.Cwd, Env: r.Env})
	if len(res.Errors) > 0 || len(res.Operations) == 0 || res.Operations[0].Family == model.FamilyUnknown {
		t.Fatalf("normalize %q: %+v", args, res)
	}
	r.Global = res.Source.Global
	original := res.Operations
	before, _ := json.Marshal(original)
	if diagnostics := r.Fill(context.Background(), &res, nil, false); len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	after, _ := json.Marshal(original)
	if string(before) != string(after) {
		t.Fatalf("Fill mutated input operations: before %s; after %s", before, after)
	}
	return res
}

func TestSemanticBooleanDefaults(t *testing.T) {
	for _, tc := range []struct {
		name, key, parameter, value string
		args                        []string
		want                        bool
		unresolved                  bool
	}{
		{"clean default", "", "requireForce", "", []string{"clean"}, true, false},
		{"clean permits delete", "clean.requireForce", "requireForce", "false", []string{"clean"}, false, false},
		{"clean numeric", "clean.requireForce", "requireForce", "2", []string{"clean"}, true, false},
		{"clean invalid", "clean.requireForce", "requireForce", "invalid", []string{"clean"}, false, true},
		{"sparse clean", "clean.requireForce", "requireForce", "off", []string{"sparse-checkout", "clean"}, false, false},
		{"submodule default", "", "recurseSubmodules", "", []string{"reset", "--hard"}, false, false},
		{"submodule enabled", "submodule.recurse", "recurseSubmodules", "yes", []string{"restore", "."}, true, false},
		{"rebase default", "", "updateRefs", "", []string{"rebase", "HEAD"}, false, false},
		{"rebase enabled", "rebase.updateRefs", "updateRefs", "true", []string{"rebase", "HEAD"}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.commit()
			if tc.key != "" {
				f.config(tc.key, tc.value)
			}
			op := normalizeFill(t, f, f.resolver(), tc.args...).Operations[0]
			if hasUnresolved(op, "parameters."+tc.parameter) != tc.unresolved {
				t.Fatalf("marker: %+v", op)
			}
			if !tc.unresolved && op.Parameters[tc.parameter] != tc.want {
				t.Fatalf("parameter: %+v", op.Parameters)
			}
			if tc.parameter == "requireForce" && op.Effect != model.EffectWrite {
				t.Fatalf("clean was weakened: %+v", op)
			}
			if tc.parameter == "recurseSubmodules" && tc.want && !slices.Contains(op.Scopes, model.ScopeSubmodule) {
				t.Fatalf("missing recursive scope: %+v", op)
			}
			if tc.parameter == "updateRefs" && tc.want && (!hasUnresolved(op, "target.refs") || op.Target.Refs.Complete) {
				t.Fatalf("rebase lost additional ref targets: %+v", op)
			}
		})
	}
}

func TestExplicitSemanticOptionsWin(t *testing.T) {
	f := newFixture(t)
	f.commit()
	f.config("submodule.recurse", "true")
	f.config("rebase.updateRefs", "true")
	for _, args := range [][]string{{"reset", "--hard", "--no-recurse-submodules"}, {"rebase", "--no-update-refs", "HEAD"}} {
		op := normalizeFill(t, f, f.resolver(), args...).Operations[0]
		parameter := "recurseSubmodules"
		if args[0] == "rebase" {
			parameter = "updateRefs"
		}
		if op.Parameters[parameter] != false || hasUnresolved(op, "parameters."+parameter) {
			t.Fatalf("explicit option overridden: %+v", op)
		}
	}
}

func TestSemanticHEAD(t *testing.T) {
	f := newFixture(t)
	for _, committed := range []bool{false, true} {
		if committed {
			f.commit()
		}
		for _, args := range [][]string{{"reset", "--soft"}, {"commit", "-m", "message"}, {"branch", "--unset-upstream"}} {
			op := normalizeFill(t, f, f.resolver(), args...).Operations[0]
			if !reflect.DeepEqual(op.Target.Refs, model.Exact("refs/heads/main")) || hasUnresolved(op, "target.refs") {
				t.Fatalf("HEAD %q: %+v", args, op)
			}
		}
	}
	op := normalizeFill(t, f, f.resolver(), "branch", "-m", "renamed").Operations[0]
	if !reflect.DeepEqual(op.Target.Refs.Values, []string{"refs/heads/main", "refs/heads/renamed"}) || !op.Target.Refs.Complete {
		t.Fatalf("rename omitted a ref: %+v", op)
	}
	f.run("-C", f.repo, "checkout", "-q", "--detach")
	op = normalizeFill(t, f, f.resolver(), "reset", "--soft").Operations[0]
	if !reflect.DeepEqual(op.Target.Refs, model.Exact("HEAD")) || hasUnresolved(op, "target.refs") {
		t.Fatalf("detached HEAD: %+v", op)
	}
	op = normalizeFill(t, f, f.resolver(), "branch", "--unset-upstream").Operations[0]
	if !hasUnresolved(op, "target.refs") {
		t.Fatal("detached HEAD was treated as a current branch")
	}
}

func TestSemanticNotesRef(t *testing.T) {
	for _, tc := range []struct{ config, env, explicit, want string }{
		{"", "", "", "refs/notes/commits"},
		{"refs/notes/review", "", "", "refs/notes/review"},
		{"refs/notes/review", "refs/notes/environment", "", "refs/notes/environment"},
		{"refs/notes/review", "refs/notes/environment", "explicit", "refs/notes/explicit"},
	} {
		f := newFixture(t)
		if tc.config != "" {
			f.config("core.notesRef", tc.config)
		}
		f.config("notes.displayRef", "refs/notes/display-only")
		f.config("notes.rewriteRef", "refs/notes/rewrite-only")
		f.env["GIT_NOTES_REF"] = tc.env
		if tc.env == "" {
			delete(f.env, "GIT_NOTES_REF")
		}
		args := []string{"notes"}
		if tc.explicit != "" {
			args = append(args, "--ref="+tc.explicit)
		}
		args = append(args, "add", "-m", "note")
		op := normalizeFill(t, f, f.resolver(), args...).Operations[0]
		if !reflect.DeepEqual(op.Target.Refs, model.Exact(tc.want)) || hasUnresolved(op, "target.refs") {
			t.Fatalf("notes ref: %+v", op)
		}
	}
}

func TestSemanticInitialBranchOutsideRepository(t *testing.T) {
	f := newFixture(t)
	for _, branch := range []string{"", "trunk"} {
		r := f.resolver()
		r.Cwd = f.root
		args := []string{"init", "new"}
		want := "master"
		if branch != "" {
			args = append([]string{"-c", "init.defaultBranch=" + branch}, args...)
			want = branch
		}
		op := normalizeFill(t, f, r, args...).Operations[0]
		if !reflect.DeepEqual(op.Target.Refs.Values, []string{"refs/heads/" + want}) || hasUnresolved(op, "target.refs") {
			t.Fatalf("initial branch: %+v", op)
		}
		if _, err := os.Stat(filepath.Join(f.root, "new")); !os.IsNotExist(err) {
			t.Fatalf("resolver executed init: %v", err)
		}
	}
}

func TestSemanticSigningDefaults(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		args       []string
		want       bool
	}{
		{"", "", []string{"tag", "new"}, false},
		{"tag.gpgSign", "yes", []string{"tag", "new"}, true},
		{"tag.gpgSign", "true", []string{"tag", "--no-sign", "new"}, false},
		{"tag.forceSignAnnotated", "true", []string{"tag", "-m", "message", "new"}, true},
		{"tag.forceSignAnnotated", "true", []string{"tag", "-a", "-m", "message", "new"}, false},
	} {
		f := newFixture(t)
		if tc.key != "" {
			f.config(tc.key, tc.value)
		}
		res := normalizeFill(t, f, f.resolver(), tc.args...)
		op := res.Operations[0]
		if op.Parameters["signed"] != tc.want || hasUnresolved(op, "parameters.signed") {
			t.Fatalf("signing %q: %+v", tc.args, op)
		}
		if tc.want && (!slices.Contains(op.Capabilities, model.CapabilityExternalExec) || !slices.Contains(op.Scopes, model.ScopeObjects) || res.Classification.RetrySafe) {
			t.Fatalf("signing capability: %+v", res)
		}
	}
}

func TestSemanticDriversNeverExecute(t *testing.T) {
	f := newFixture(t)
	f.commit()
	marker := filepath.Join(f.root, "executed")
	script := filepath.Join(f.root, "driver")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n: > \""+marker+"\"\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.repo, "file"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	f.run("-C", f.repo, "add", "file")
	f.config("core.fsmonitor", script)
	for _, tc := range []struct {
		args  []string
		field string
	}{
		{[]string{"diff"}, "parameters.externalDiff"},
		{[]string{"show"}, "parameters.filters"},
		{[]string{"add", "file"}, "capabilities"},
		{[]string{"hash-object", "file"}, "parameters.filters"},
		{[]string{"submodule", "update"}, "parameters.updateCommand"},
	} {
		op := normalizeFill(t, f, f.resolver(), tc.args...).Operations[0]
		if hasUnresolved(op, tc.field) {
			t.Fatalf("absent driver %q: %+v", tc.args, op)
		}
	}
	f.env["GIT_EXTERNAL_DIFF"] = script
	f.config("diff.external", script)
	f.config("diff.custom.command", script)
	f.config("diff.custom.textconv", script)
	f.config("filter.custom.clean", script)
	f.config("filter.custom.smudge", script)
	f.config("filter.custom.process", script)
	f.config("submodule.child.update", "!"+script)
	f.config("tar.custom.command", script)
	if err := os.WriteFile(filepath.Join(f.repo, ".gitattributes"), []byte("* diff=custom filter=custom\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args  []string
		field string
	}{
		{[]string{"diff"}, "parameters.externalDiff"},
		{[]string{"show"}, "parameters.filters"},
		{[]string{"add", "file"}, "capabilities"},
		{[]string{"submodule", "update"}, "parameters.updateCommand"},
	} {
		op := normalizeFill(t, f, f.resolver(), tc.args...).Operations[0]
		if !hasUnresolved(op, tc.field) || !slices.Contains(op.Capabilities, model.CapabilityExternalExec) {
			t.Fatalf("attribute-dependent driver %q: %+v", tc.args, op)
		}
	}
	op := normalizeFill(t, f, f.resolver(), "archive", "--format=custom", "HEAD").Operations[0]
	if hasUnresolved(op, "parameters.archiver") || op.Parameters["archiver"] != true {
		t.Fatalf("archiver: %+v", op)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("resolver executed a configured driver: %v", err)
	}
}

func TestSemanticDiffExternalConfiguration(t *testing.T) {
	for _, source := range []string{"config", "env"} {
		f := newFixture(t)
		if source == "env" {
			f.env["GIT_EXTERNAL_DIFF"] = "must-not-execute"
		} else {
			f.config("diff.external", "must-not-execute")
		}
		op := normalizeFill(t, f, f.resolver(), "diff").Operations[0]
		if op.Parameters["externalDiff"] != true || hasUnresolved(op, "parameters.externalDiff") || !slices.Contains(op.Capabilities, model.CapabilityExternalExec) {
			t.Fatalf("external diff: %+v", op)
		}
	}
}

func TestSemanticUpstreamAndPull(t *testing.T) {
	f := newFixture(t)
	f.commit()
	f.config("remote.origin.url", "ssh://example.test/unreachable")
	f.config("remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	f.config("branch.main.remote", "origin")
	f.config("branch.main.merge", "refs/heads/trunk")
	f.run("-C", f.repo, "update-ref", "refs/remotes/origin/trunk", "HEAD")
	for _, args := range [][]string{{"merge", "--no-edit"}, {"rebase"}} {
		op := normalizeFill(t, f, f.resolver(), args...).Operations[0]
		if hasUnresolved(op, "target.revisions") || !slices.Contains(op.Target.Revisions.Values, "refs/remotes/origin/trunk") {
			t.Fatalf("upstream: %+v", op)
		}
	}
	f.config("pull.rebase", "true")
	f.config("pull.ff", "only")
	res := normalizeFill(t, f, f.resolver(), "pull")
	op := res.Operations[len(res.Operations)-1]
	if op.Parameters["mode"] != "rebase" || op.Parameters["ffOnly"] != true || hasUnresolved(op, "parameters.mode") || hasUnresolved(op, "target.refs") {
		t.Fatalf("pull defaults: %+v", op)
	}
	f.config("branch.main.rebase", "interactive")
	res = normalizeFill(t, f, f.resolver(), "pull")
	op = res.Operations[len(res.Operations)-1]
	if op.UnknownReason != model.UnknownInteractive || !hasUnresolved(op, "parameters.todo") || res.Classification.Coverage != model.CoverageOpaque || res.Classification.RetrySafe {
		t.Fatalf("interactive default was weakened: %+v", res)
	}
}

func TestSemanticMaintenanceDefaults(t *testing.T) {
	for _, tc := range []struct {
		config []string
		args   []string
		want   []string
	}{
		{nil, []string{"maintenance", "run"}, []string{"geometric-repack", "commit-graph", "pack-refs", "reflog-expire", "worktree-prune", "rerere-gc"}},
		{[]string{"maintenance.strategy=gc"}, []string{"maintenance", "run"}, []string{"gc"}},
		{[]string{"maintenance.strategy=incremental"}, []string{"maintenance", "run", "--schedule=hourly"}, []string{"prefetch", "commit-graph"}},
		{[]string{"maintenance.strategy=gc", "maintenance.prefetch.enabled=true", "maintenance.gc.enabled=false"}, []string{"maintenance", "run"}, []string{"prefetch"}},
		{[]string{"maintenance.prefetch.enabled=true"}, []string{"maintenance", "run", "--task=gc"}, []string{"gc"}},
	} {
		f := newFixture(t)
		for _, config := range tc.config {
			key, value, _ := strings.Cut(config, "=")
			f.config(key, value)
		}
		op := normalizeFill(t, f, f.resolver(), tc.args...).Operations[0]
		if hasUnresolved(op, "parameters.tasks") || !reflect.DeepEqual(stringList(op.Parameters["tasks"]), tc.want) {
			t.Fatalf("tasks: %+v; want %q", op, tc.want)
		}
		if slices.Contains(tc.want, "prefetch") && !slices.Contains(op.Capabilities, model.CapabilityNetwork) {
			t.Fatalf("prefetch lost network capability: %+v", op)
		}
	}
}

func TestSemanticConfigKeyRegexp(t *testing.T) {
	f := newFixture(t)
	f.config("resolve.one", "first")
	f.config("resolve.two", "second")
	for _, args := range [][]string{{"config", "--get-regexp", "^resolve[.]"}, {"config", "get", "--regexp", "^resolve[.]"}} {
		op := normalizeFill(t, f, f.resolver(), args...).Operations[0]
		if hasUnresolved(op, "target.configKeys") || !reflect.DeepEqual(op.Target.ConfigKeys.Values, []string{"resolve.one", "resolve.two"}) {
			t.Fatalf("config keys: %+v", op)
		}
	}
	op := normalizeFill(t, f, f.resolver(), "config", "--file", "missing", "--get-regexp", "^resolve[.]").Operations[0]
	if !hasUnresolved(op, "target.configKeys") {
		t.Fatal("repository config was substituted for an explicit config file")
	}
}

func TestSemanticClearsOnlyResolvedMarker(t *testing.T) {
	f := newFixture(t)
	f.commit()
	op := gitnormalize.Normalize(model.Invocation{Argv: []string{"reset", "--soft"}}).Operations[0]
	op.Unresolved = append(op.Unresolved,
		model.Unresolved{Field: "target.refs", Reason: "separate unavailable input", Source: "stdin"},
		model.Unresolved{Field: "parameters.requireForce", Reason: "not a Git configuration marker", Source: "editor"})
	got := fill(t, f.resolver(), op).Operations[0]
	if len(got.Unresolved) != 2 || got.Target.Refs.Complete || got.Unresolved[0].Source != "stdin" || got.Unresolved[1].Source != "editor" {
		t.Fatalf("unrelated markers were cleared: %+v", got)
	}
}
