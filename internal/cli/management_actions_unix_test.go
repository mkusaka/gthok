//go:build unix

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/install"
	"github.com/mkusaka/gthok/internal/schema"
)

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
		t.Fatal("non-Git actions remain exposed")
	}
}

func TestActionsDocumentedWithScopes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "action-catalogue.md"))
	if err != nil {
		t.Fatal(err)
	}
	catalogue := managementActionsCatalogue("")
	for _, family := range catalogue {
		for _, action := range family.Actions {
			if len(action.Git) == 0 || action.Scopes == nil {
				t.Fatalf("missing route/scopes for %s", action.Action)
			}
			if !strings.Contains(string(data), "`"+action.Action+"`") {
				t.Errorf("undocumented action %s", action.Action)
			}
			if action.Action == "remote.push.update" && len(action.Scopes) == 0 {
				t.Fatal("push scopes missing")
			}
		}
	}
	body, err := json.Marshal(catalogue)
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{`"native"`, `"rest"`, `"graphql"`} {
		if strings.Contains(string(body), old) {
			t.Fatal("retired column", old)
		}
	}
	if text := managementActionsText(managementActionsCatalogue("remote.push.*")); !strings.Contains(text, "scopes: [remote-ref]") || !strings.Contains(text, "git: push") {
		t.Fatal(text)
	}
}

func TestManagementJSONContractEnvelopes(t *testing.T) {
	hooksExpectConfig(t, "observe", "pre", ":")
	for _, tc := range []struct {
		args []string
		into any
	}{
		{[]string{"normalize", "--no-resolve", "--", "version"}, &schema.NormalizeResult{}},
		{[]string{"explain", "--no-resolve", "--no-transforms", "--json", "--", "version"}, &schema.Explain{}},
		{[]string{"hooks", "test", "--no-resolve", "--json", "--", "version"}, &schema.HooksTest{}},
		{[]string{"actions", "remote.push.*", "--json"}, &[]schema.ActionFamily{}},
		{[]string{"doctor", "--json"}, &[]install.Check{}},
	} {
		code, out, stderr := hooksExpectRun(t, tc.args...)
		if code != 0 && tc.args[0] != "doctor" {
			t.Fatalf("%v: code=%d stderr=%s", tc.args, code, stderr)
		}
		decoder := json.NewDecoder(strings.NewReader(out))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(tc.into); err != nil {
			t.Fatalf("%v: schema contract drift: %v\n%s", tc.args, err, out)
		}
	}
}

func TestAllSchemaCommands(t *testing.T) {
	for name := range schema.Files() {
		code, out, stderr := hooksExpectRun(t, "schema", name)
		want, err := schema.JSON(name)
		if err != nil || code != 0 || out != string(want) || stderr != "" {
			t.Fatalf("schema %s: code=%d, err=%v, stderr=%s", name, code, err, stderr)
		}
	}
	for _, name := range []string{"project-config", "local-config"} {
		code, out, _ := hooksExpectRun(t, "schema", name)
		if code != 0 || !json.Valid([]byte(out)) {
			t.Fatal(name, code, out)
		}
	}
	code, _, _ := hooksExpectRun(t, "schema", "not-a-contract")
	if code != 2 {
		t.Fatal("unknown schema did not produce a usage error", code)
	}
}
