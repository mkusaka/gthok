package actions

import (
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

func TestRefsRemoteCatalogue(t *testing.T) {
	for _, action := range strings.Fields(`remote.push.update remote.push.delete remote.push.mirror remote.push.matching remote.push.tags
remote.fetch remote.pull remote.config.add remote.config.remove remote.config.rename remote.config.set-url remote.prune
ref.create ref.update ref.delete ref.rename ref.branch.list ref.branch.upstream
ref.tag.list ref.tag.verify ref.symbolic.read
ref.reflog.expire ref.reflog.delete ref.reflog.show`) {
		spec, ok := Lookup(action)
		if !ok || spec.Action != action || spec.Family == "" {
			t.Errorf("missing action %s", action)
		}
	}
	for _, action := range []string{"ref.branch.list", "ref.tag.list", "ref.tag.verify", "ref.symbolic.read", "ref.reflog.show", "remote.ls-remote"} {
		if Must(action).Effect != model.EffectRead {
			t.Errorf("%s must be read", action)
		}
	}
	for _, action := range []string{"ref.create", "ref.update", "ref.delete", "ref.rename"} {
		if spec := Must(action); spec.Family != "ref" || spec.Effect != model.EffectWrite {
			t.Errorf("noncanonical ref write: %+v", spec)
		}
	}
	for family, modes := range map[string][]string{"ref.branch": {"create", "delete", "rename", "copy"}, "ref.tag": {"create", "delete"}, "ref.symbolic": {"set", "delete"}} {
		for _, mode := range modes {
			if action := family + "." + mode; KnownPattern(action) {
				t.Errorf("retired action exposed: %s", action)
			}
		}
	}
	if !KnownPattern("remote.push.*") || !KnownPattern("ref.symbolic.*") {
		t.Fatal("catalogue patterns do not cover refs and remotes")
	}
}
