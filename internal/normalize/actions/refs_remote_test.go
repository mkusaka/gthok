package actions

import (
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

func TestRefsRemoteCatalogue(t *testing.T) {
	for _, action := range strings.Fields(`remote.push.update remote.push.delete remote.push.mirror remote.push.matching remote.push.tags
remote.fetch remote.pull remote.config.add remote.config.remove remote.config.rename remote.config.set-url remote.prune
ref.branch.create ref.branch.delete ref.branch.rename ref.branch.copy ref.branch.list ref.branch.upstream
ref.tag.create ref.tag.delete ref.tag.list ref.tag.verify ref.update ref.delete ref.symbolic.set ref.symbolic.delete ref.symbolic.read
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
	if !KnownPattern("remote.push.*") || !KnownPattern("ref.symbolic.*") {
		t.Fatal("catalogue patterns do not cover refs and remotes")
	}
}
