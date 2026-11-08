package git

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/registry"
)

func TestT7cWorktreeAdmin(t *testing.T) {
	for _, tt := range []struct {
		args   string
		action string
		effect model.Effect
		scopes []model.Scope
	}{
		{"add -b topic ../topic HEAD", "add", model.EffectWrite, []model.Scope{model.ScopeWorktreeAdmin, model.ScopeWorktree, model.ScopeIndex, model.ScopeLocalRef, model.ScopeConfig}},
		{"add --detach ../scratch HEAD", "add", model.EffectWrite, []model.Scope{model.ScopeWorktreeAdmin, model.ScopeWorktree, model.ScopeIndex, model.ScopeLocalRef, model.ScopeConfig}},
		{"list --porcelain -z", "list", model.EffectRead, []model.Scope{model.ScopeWorktreeAdmin}},
		{"lock --reason=busy ../topic", "lock", model.EffectWrite, []model.Scope{model.ScopeWorktreeAdmin}},
		{"unlock ../topic", "unlock", model.EffectWrite, []model.Scope{model.ScopeWorktreeAdmin}},
		{"move ../topic ../renamed", "move", model.EffectWrite, []model.Scope{model.ScopeWorktreeAdmin, model.ScopeWorktree}},
		{"prune", "prune", model.EffectWrite, []model.Scope{model.ScopeWorktreeAdmin}},
		{"prune -n", "prune.preview", model.EffectRead, []model.Scope{model.ScopeWorktreeAdmin}},
		{"remove -ff ../topic", "remove", model.EffectWrite, []model.Scope{model.ScopeWorktreeAdmin, model.ScopeWorktree}},
		{"repair ../topic", "repair", model.EffectWrite, []model.Scope{model.ScopeWorktreeAdmin}},
	} {
		t.Run(tt.args, func(t *testing.T) {
			r, op := adminTestOperation(t, append([]string{"worktree"}, strings.Fields(tt.args)...)...)
			if op.Family != "worktree.admin" || op.Action != "worktree.admin."+tt.action || op.Effect != tt.effect || !reflect.DeepEqual(op.Scopes, tt.scopes) || op.Coverage != model.CoverageSemantic || !strings.HasPrefix(op.Origin.Source, "builtin/worktree.c:") {
				t.Fatal(op)
			}
			if tt.action == "remove" && op.Parameters["forceLevel"] != 2 {
				t.Fatal(op.Parameters)
			}
			if tt.args == "add -b topic ../topic HEAD" && (!reflect.DeepEqual(op.Target.Refs, model.Exact("refs/heads/topic")) || !reflect.DeepEqual(op.Target.Paths, model.Exact("../topic")) || op.Target.Worktree != "../topic") {
				t.Fatal(op.Target)
			}
			if r.Classification.RetrySafe != (tt.effect == model.EffectRead) {
				t.Fatal(r.Classification)
			}
		})
	}
	_, op := adminTestOperation(t, "worktree", "add", "--no-checkout", "-Btopic", "--", "-directory", "HEAD")
	if op.Parameters["checkout"] != false || op.Parameters["resetBranch"] != true || slices.Contains(op.Capabilities, model.CapabilityExternalExec) || !reflect.DeepEqual(op.Target.Paths, model.Exact("-directory")) {
		t.Fatal(op)
	}
	_, op = adminTestOperation(t, "worktree", "add", "../auto")
	if !reflect.DeepEqual(op.Target.Refs, model.Exact("refs/heads/auto")) || len(op.Unresolved) == 0 {
		t.Fatal(op)
	}
}

func TestT7cSubmoduleModes(t *testing.T) {
	for _, tt := range []struct {
		args   string
		action string
		effect model.Effect
		scopes []model.Scope
	}{
		{"", "status", model.EffectRead, []model.Scope{model.ScopeSubmodule}},
		{"--quiet status --recursive -- lib", "status", model.EffectRead, []model.Scope{model.ScopeSubmodule}},
		{"summary --cached HEAD -- lib", "summary", model.EffectRead, []model.Scope{model.ScopeSubmodule}},
		{"add -bmain ../source lib", "add", model.EffectWrite, []model.Scope{model.ScopeSubmodule, model.ScopeIndex, model.ScopeWorktree, model.ScopeConfig, model.ScopeObjects, model.ScopeLocalRef}},
		{"init lib", "init", model.EffectWrite, []model.Scope{model.ScopeSubmodule, model.ScopeConfig}},
		{"deinit --all", "deinit", model.EffectWrite, []model.Scope{model.ScopeSubmodule, model.ScopeConfig, model.ScopeWorktree, model.ScopeWorktreeAdmin}},
		{"update --remote --recursive --merge lib", "update", model.EffectWrite, []model.Scope{model.ScopeSubmodule, model.ScopeWorktree, model.ScopeIndex, model.ScopeLocalRef, model.ScopeObjects}},
		{"update --init --rebase lib", "update", model.EffectWrite, []model.Scope{model.ScopeSubmodule, model.ScopeWorktree, model.ScopeIndex, model.ScopeLocalRef, model.ScopeObjects, model.ScopeConfig}},
		{"sync --recursive lib", "sync", model.EffectWrite, []model.Scope{model.ScopeSubmodule, model.ScopeConfig, model.ScopeRemoteConfig}},
		{"set-url lib ../new-source", "set-url", model.EffectWrite, []model.Scope{model.ScopeSubmodule, model.ScopeConfig, model.ScopeWorktree, model.ScopeRemoteConfig}},
		{"set-branch -bmain lib", "set-branch", model.EffectWrite, []model.Scope{model.ScopeSubmodule, model.ScopeConfig, model.ScopeWorktree}},
		{"absorbgitdirs lib", "absorbgitdirs", model.EffectWrite, []model.Scope{model.ScopeSubmodule, model.ScopeConfig, model.ScopeWorktree, model.ScopeWorktreeAdmin}},
	} {
		t.Run(tt.args, func(t *testing.T) {
			r, op := adminTestOperation(t, append([]string{"submodule"}, strings.Fields(tt.args)...)...)
			if op.Family != "submodule" || op.Action != "submodule."+tt.action || op.Effect != tt.effect || !reflect.DeepEqual(op.Scopes, tt.scopes) || op.Coverage != model.CoverageSemantic || !strings.HasPrefix(op.Origin.Source, "git-submodule.sh:cmd_") || r.Classification.RetrySafe != (tt.effect == model.EffectRead) {
				t.Fatal(r)
			}
			if strings.Contains(tt.args, "--recursive") && op.Parameters["recursive"] != true {
				t.Fatal(op.Parameters)
			}
			if tt.action == "set-url" && (!reflect.DeepEqual(op.Target.Paths, model.Exact("lib")) || op.Target.RemoteURL != "../new-source") {
				t.Fatal(op.Target)
			}
		})
	}
	_, op := adminTestOperation(t, "submodule", "update", "--no-fetch", "lib")
	if op.Parameters["strategy"] != "configured" || op.Parameters["noFetch"] != true || !slices.Contains(op.Capabilities, model.CapabilityExternalExec) || !slices.Contains(op.Capabilities, model.CapabilityNetwork) {
		t.Fatal(op)
	}
	found := false
	for _, u := range op.Unresolved {
		found = found || u.Field == "parameters.updateCommand" && u.Source == "git"
	}
	if !found {
		t.Fatal(op.Unresolved)
	}
	_, op = adminTestOperation(t, "submodule", "foreach", "--recursive", "git", "reset", "--hard")
	if op.Effect != model.EffectUnknown || op.Coverage != model.CoverageOpaque || op.UnknownReason != model.UnknownExternal || !reflect.DeepEqual(op.Parameters["command"], []string{"git", "reset", "--hard"}) {
		t.Fatal(op)
	}
}

func TestT7cInitAndHook(t *testing.T) {
	for _, args := range [][]string{{"init", "--bare", "-bmain", "repo.git"}, {"init-db", "-bmain", "repo.git"}} {
		r, op := adminTestOperation(t, args...)
		if op.Action != "repository.init" || op.Effect != model.EffectWrite || r.Classification.RetrySafe || !reflect.DeepEqual(op.Target.Refs, model.Exact("refs/heads/main")) || !reflect.DeepEqual(op.Target.Paths, model.Exact("repo.git")) || op.Origin.Source != "builtin/init-db.c:cmd_init_db" {
			t.Fatal(r)
		}
	}
	_, op := adminTestOperation(t, "hook", "run", "pre-commit", "--", "arg")
	if op.Action != "hook.run" || op.UnknownReason != model.UnknownExternal || op.Effect != model.EffectUnknown || !slices.Contains(op.Capabilities, model.CapabilityExternalExec) {
		t.Fatal(op)
	}
	r, op := adminTestOperation(t, "hook", "list")
	if op.Action != "hook.list" || !r.Classification.RetrySafe {
		t.Fatal(r)
	}
}

func TestT7cExplicitCommandInventory(t *testing.T) {
	globals := map[string]bool{"--exec-path": true, "--help": true, "--html-path": true, "--info-path": true, "--list-cmds": true, "--man-path": true, "--version": true}
	for name, entry := range registry.Commands() {
		t.Run(name, func(t *testing.T) {
			if entry.Implementation == "document" {
				// Documentation subjects are explicitly rejected by Normalize before parsing.
				r := Normalize(model.Invocation{Argv: []string{name}})
				if r.Operations[0].UnknownReason != model.UnknownCommand || r.Classification.RetrySafe {
					t.Fatal(r)
				}
				return
			}
			if entry.Implementation == "option-only" && globals[name] {
				return
			}
			if semantics[name] == nil {
				t.Fatal("command has neither semantics nor an explicit conservative/opaque entry")
			}
		})
	}
	for command, reason := range adminConservativeCommands {
		if reason == "" {
			t.Errorf("%s has no conservative reason", command)
		}
		entry, exists := commands[command]
		if !exists {
			t.Errorf("stale conservative entry %s", command)
			continue
		}
		p := ParsedCommand{Path: []string{entry.Name}}
		op := adminConservative(&Context{Parsed: p})[0]
		if op.Coverage != model.CoverageConservative || op.Effect != model.EffectUnknown || op.Detail != reason {
			t.Errorf("%s: %+v", command, op)
		}
	}
	for command, reason := range adminOpaqueCommands {
		if reason == "" {
			t.Errorf("%s has no opaque reason", command)
		}
		_, op := adminTestOperation(t, command)
		if op.Coverage != model.CoverageOpaque || op.UnknownReason == model.UnknownNone || op.Effect != model.EffectUnknown {
			t.Errorf("%s: %+v", command, op)
		}
	}
}
