//go:build unix

package cli

import "testing"

func TestGitActionsGlob(t *testing.T) {
	found := make(map[string]bool)
	for _, family := range managementActionsCatalogue("remote.push.*") {
		if family.Family != "remote.push" {
			t.Fatal(family.Family)
		}
		for _, action := range family.Actions {
			found[action.Action] = true
		}
	}
	for _, action := range []string{"remote.push.update", "remote.push.delete", "remote.push.mirror"} {
		if !found[action] {
			t.Fatalf("missing Git action %s", action)
		}
	}
	if len(managementActionsCatalogue("pr.merge")) != 0 {
		t.Fatal("GitHub actions remain exposed")
	}
}
