package schema

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/mkusaka/gthok/internal/docsearch"
	"github.com/mkusaka/gthok/internal/install"
	"github.com/mkusaka/gthok/internal/logging"
	"github.com/mkusaka/gthok/internal/model"
	gitnormalize "github.com/mkusaka/gthok/internal/normalize/git"
	"github.com/mkusaka/gthok/internal/policy"
	"github.com/mkusaka/gthok/internal/protocol"
)

func TestPublicOutputSchemas(t *testing.T) {
	r := gitnormalize.Normalize(model.Invocation{Argv: []string{"version"}})
	p := policy.Evaluation{Mode: "observe", Decision: protocol.DecisionAllow}
	h := HooksTest{Phase: "pre", Synthetic: true, Results: []protocol.HookResult{}, Policy: p}
	h.Aggregate.Decision, h.Final.Decision = protocol.DecisionAbstain, protocol.DecisionAllow
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"normalize-result", r},
		{"explain", Explain{Normalization: r, Policy: p, Hooks: map[string][]string{"pre": {}, "post": {}}}},
		{"hooks-test", h},
		{"doctor", []install.Check{{Name: "git", Status: install.StatusOK, Detail: "installed"}}},
		{"actions", []ActionFamily{{Family: "git.version", Actions: []ActionEntry{{Action: "git.version.read", Effect: model.EffectRead, Scopes: []model.Scope{}, Git: []string{"version"}}}}}},
		{"docs", []docsearch.Result{{ID: "security", Title: "Security", Description: "Trust", URL: "https://example.test", Score: 1.5}}},
		{"docs", []docsearch.Section{{ID: "security", Title: "Security", File: "docs/security.md"}}},
		{"docs", []docsearch.Result{}},
		{"version", Version{Version: "v0.0.1", Go: "go1.26", OS: "linux", Arch: "arm64"}},
		{"log", logging.Record{Time: time.Now(), Argv: []string{"version"}, Classification: r.Classification, Operations: r.Operations}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			var doc any
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			if err := checkSchema(mustSchema(t, tc.name), doc, "$"); err != nil {
				t.Fatalf("%s: %v", data, err)
			}
		})
	}
}

func TestSchemaAliasesAndMetadata(t *testing.T) {
	for alias, canonical := range map[string]string{"project-config": "project", "local-config": "local"} {
		if !reflect.DeepEqual(mustSchema(t, alias), mustSchema(t, canonical)) {
			t.Errorf("%s differs from %s", alias, canonical)
		}
	}
	for name := range Files() {
		s := mustSchema(t, name)
		if s["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
			t.Fatal(name, s["$schema"])
		}
	}
	if _, err := Generate("not-a-contract"); err == nil {
		t.Fatal("accepted unknown schema")
	}
	s := mustSchema(t, "normalize-result")
	doc := hookDocument(t, gitnormalize.Normalize(model.Invocation{Argv: []string{"status"}}))
	doc["classification"].(map[string]any)["coverage"] = "safe"
	if checkSchema(s, doc, "$") == nil {
		t.Fatal("invalid coverage accepted")
	}
	doc = hookDocument(t, gitnormalize.Normalize(model.Invocation{Argv: []string{"status"}}))
	doc["operations"].([]any)[0].(map[string]any)["scopes"] = []any{"invalid"}
	if checkSchema(s, doc, "$") == nil {
		t.Fatal("invalid scope accepted")
	}
}
