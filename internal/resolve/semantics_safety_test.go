package resolve

import (
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

func TestSemanticRemoteUpdateDefaults(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"origin", "backup", "skip"} {
		f.config("remote."+name+".url", "ssh://example.test/"+name)
	}
	f.config("remote.skip.skipDefaultUpdate", "true")
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"remote", "update"}, []string{"backup", "origin"}},
		{[]string{"fetch", "--all"}, []string{"backup", "origin"}},
		{[]string{"-c", "remotes.default=backup", "remote", "update"}, []string{"backup"}},
	} {
		res := normalizeFill(t, f, f.resolver(), tc.args...)
		var remotes []string
		for _, op := range res.Operations {
			if hasUnresolved(op, "target.remote") {
				t.Fatalf("default remote marker: %+v", op)
			}
			remotes = append(remotes, op.Target.Remote)
		}
		sort.Strings(remotes)
		if !reflect.DeepEqual(remotes, tc.want) {
			t.Fatalf("default remotes %q: %q, want %q", tc.args, remotes, tc.want)
		}
	}
}

func TestSemanticSubmodulePushKeepsAdditionalRemoteMarker(t *testing.T) {
	f := newFixture(t)
	f.commit()
	f.config("remote.origin.url", "ssh://example.test/unreachable")
	f.config("push.default", "current")
	op := normalizeFill(t, f, f.resolver(), "push", "--recurse-submodules=on-demand", "origin").Operations[0]
	if !hasUnresolved(op, "target.remote") || op.Target.RemoteURL == "" {
		t.Fatalf("resolving the parent remote hid unresolved submodule remotes: %+v", op)
	}
}

func TestSemanticEmptyNotesConfigurationStaysUnresolved(t *testing.T) {
	f := newFixture(t)
	f.env["GIT_NOTES_REF"] = ""
	op := normalizeFill(t, f, f.resolver(), "notes", "list").Operations[0]
	if !hasUnresolved(op, "target.refs") || op.Target.Refs.Kind != model.SetUnknown {
		t.Fatalf("empty notes ref was replaced with a default: %+v", op)
	}
}

func TestSemanticUnavailableConfigKeepsDefaultsUnresolved(t *testing.T) {
	f := newFixture(t)
	f.commit()
	if err := os.WriteFile(f.repo+"/.git/config", []byte("[invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	op := normalizeFill(t, f, f.resolver(), "clean").Operations[0]
	if !hasUnresolved(op, "parameters.requireForce") || op.Parameters["requireForce"] != nil || op.Effect != model.EffectWrite {
		t.Fatalf("failed configuration query became a safe default: %+v", op)
	}
}

func TestSemanticExplicitSquashIgnoresInteractiveDefault(t *testing.T) {
	f := newFixture(t)
	f.commit()
	f.config("pull.rebase", "interactive")
	res := normalizeFill(t, f, f.resolver(), "pull", "--squash")
	op := res.Operations[len(res.Operations)-1]
	if op.Parameters["mode"] != "merge" || op.UnknownReason == model.UnknownInteractive || hasUnresolved(op, "parameters.mode") {
		t.Fatalf("explicit squash used an interactive default: %+v", op)
	}
}
