package git

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize/actions"
)

func adminTestOperation(t *testing.T, argv ...string) (model.Result, model.Operation) {
	t.Helper()
	r := Normalize(model.Invocation{Argv: argv})
	if len(r.Errors) != 0 || len(r.Operations) != 1 {
		t.Fatalf("%q: %+v", argv, r)
	}
	op := r.Operations[0]
	if op.Origin.Source == "" {
		t.Fatalf("%q: missing source: %+v", argv, op)
	}
	spec, ok := actions.Lookup(op.Action)
	if !ok || spec.Family != op.Family || spec.Effect != op.Effect {
		t.Fatalf("%q: unregistered/mismatched action: %+v; spec %+v", argv, op, spec)
	}
	return r, op
}

func TestT7cConfigModes(t *testing.T) {
	for _, tt := range []struct {
		args     string
		action   string
		effect   model.Effect
		location string
		keys     *model.TargetSet
		params   map[string]any
	}{
		{"get User.Name", "get", model.EffectRead, "all", model.Exact("user.name"), nil},
		{"get --global --all remote.Origin.URL", "get", model.EffectRead, "global", model.Exact("remote.Origin.url"), map[string]any{"all": true}},
		{"get --system core.editor", "get", model.EffectRead, "system", model.Exact("core.editor"), nil},
		{"get --blob=HEAD:.gitmodules submodule.x.url", "get", model.EffectRead, "blob", model.Exact("submodule.x.url"), nil},
		{"get --regexp ^remote", "get", model.EffectRead, "all", model.UnknownSet(), map[string]any{"keyPattern": "^remote"}},
		{"get --url=https://example.test http.proxy", "get", model.EffectRead, "all", model.UnknownSet(), nil},
		{"set --worktree --append core.editor vim", "set", model.EffectWrite, "worktree", model.Exact("core.editor"), map[string]any{"append": true}},
		{"set --all --value=old --fixed-value core.editor vim", "set", model.EffectWrite, "local", model.Exact("core.editor"), map[string]any{"all": true, "fixedValue": true, "valuePattern": "old"}},
		{"set --app --no-append core.editor vim", "set", model.EffectWrite, "local", model.Exact("core.editor"), map[string]any{"append": false}},
		{"unset --all core.editor", "unset", model.EffectWrite, "local", model.Exact("core.editor"), map[string]any{"all": true}},
		{"rename-section remote.old remote.new", "rename-section", model.EffectWrite, "local", &model.TargetSet{Kind: model.SetPattern, Values: []string{"remote.old.*", "remote.new.*"}, Complete: true}, nil},
		{"remove-section remote.old", "remove-section", model.EffectWrite, "local", &model.TargetSet{Kind: model.SetPattern, Values: []string{"remote.old.*"}, Complete: true}, nil},
		{"list --global", "list", model.EffectRead, "global", &model.TargetSet{Kind: model.SetPattern, Values: []string{"*"}, Complete: true}, nil},
		{"edit --local", "edit", model.EffectWrite, "local", model.UnknownSet(), nil},
		{"--get core.editor", "get", model.EffectRead, "all", model.Exact("core.editor"), nil},
		{"--get-all core.editor", "get", model.EffectRead, "all", model.Exact("core.editor"), map[string]any{"all": true}},
		{"--get-regexp ^core", "get", model.EffectRead, "all", model.UnknownSet(), nil},
		{"--global --add core.editor vim", "add", model.EffectWrite, "global", model.Exact("core.editor"), map[string]any{"append": true}},
		{"--unset core.editor old", "unset", model.EffectWrite, "local", model.Exact("core.editor"), map[string]any{"valuePattern": "old"}},
		{"--unset-all core.editor", "unset", model.EffectWrite, "local", model.Exact("core.editor"), map[string]any{"all": true}},
		{"--replace-all core.editor vim old", "set", model.EffectWrite, "local", model.Exact("core.editor"), map[string]any{"all": true, "valuePattern": "old"}},
		{"-e", "edit", model.EffectWrite, "local", model.UnknownSet(), nil},
		{"-l", "list", model.EffectRead, "all", &model.TargetSet{Kind: model.SetPattern, Values: []string{"*"}, Complete: true}, nil},
		{"core.editor", "get", model.EffectRead, "all", model.Exact("core.editor"), nil},
		{"core.editor vim", "set", model.EffectWrite, "local", model.Exact("core.editor"), nil},
		{"core.editor vim old", "set", model.EffectWrite, "local", model.Exact("core.editor"), map[string]any{"all": true, "valuePattern": "old"}},
	} {
		t.Run(tt.args, func(t *testing.T) {
			argv := append([]string{"config"}, strings.Fields(tt.args)...)
			r, op := adminTestOperation(t, argv...)
			if op.Family != "config" || op.Action != "config."+tt.action || op.Effect != tt.effect || !reflect.DeepEqual(op.Scopes, []model.Scope{model.ScopeConfig}) || op.Coverage != model.CoverageSemantic || !reflect.DeepEqual(op.Target.ConfigKeys, tt.keys) || op.Parameters["location"] != tt.location {
				t.Fatalf("%+v", op)
			}
			for key, want := range tt.params {
				if !reflect.DeepEqual(op.Parameters[key], want) {
					t.Errorf("%s: got %#v want %#v", key, op.Parameters[key], want)
				}
			}
			if tt.action == "edit" && !reflect.DeepEqual(op.Capabilities, []model.Capability{model.CapabilityEditor}) {
				t.Fatalf("editor: %+v", op)
			}
			if r.Classification.RetrySafe != (tt.effect == model.EffectRead) {
				t.Fatalf("safety: %+v", r.Classification)
			}
		})
	}
}

func TestT7cConfigFileAndBoundaries(t *testing.T) {
	_, op := adminTestOperation(t, "config", "set", "-ffirst", "--file=second", "--", "Core.Editor", "--global")
	if !reflect.DeepEqual(op.Target.Paths, model.Exact("second")) || op.Parameters["file"] != "second" || op.Parameters["location"] != "file" || !reflect.DeepEqual(op.Target.ConfigKeys, model.Exact("core.editor")) {
		t.Fatal(op)
	}
	r := Normalize(model.Invocation{Argv: []string{"config", "--get", "core.editor"}, Env: map[string]string{"GIT_CONFIG": "custom"}})
	if r.Operations[0].Parameters["location"] != "file" || !reflect.DeepEqual(r.Operations[0].Target.Paths, model.Exact("custom")) {
		t.Fatal(r)
	}
	_, op = adminTestOperation(t, "config", "remove-section", "Remote.a*b")
	if !reflect.DeepEqual(op.Target.ConfigKeys.Values, []string{"Remote.a\\*b.*"}) {
		t.Fatal(op.Target.ConfigKeys)
	}
	for _, args := range [][]string{{"config"}, {"config", "set", "core.editor"}, {"config", "--get", "--unset", "core.editor"}} {
		r := Normalize(model.Invocation{Argv: args})
		if r.Classification.RetrySafe || r.Operations[0].UnknownReason != model.UnknownParseError {
			t.Fatalf("%q: %+v", args, r)
		}
	}
}
