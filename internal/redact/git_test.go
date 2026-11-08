package redact

import (
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/protocol"
	"strings"
	"testing"
)

func TestGitURLsAndConfig(t *testing.T) {
	r := New(nil)
	argv := []string{"-c", "http.extraHeader=Authorization: Basic short", "-ccredential.password=tiny", "push", "https://user:password@example.test/repo"}
	got := strings.Join(r.Argv(argv), " ")
	if r.String("https://tiny-token@example.test/repo") != "https://[REDACTED]@example.test/repo" {
		t.Fatal("URL username credential leaked")
	}
	for _, secret := range []string{"short", "tiny", "password@example"} {
		if strings.Contains(got, secret) {
			t.Fatalf("leaked %s: %s", secret, got)
		}
	}
}
func TestGitTargetCopies(t *testing.T) {
	r := New(map[string]string{"API_SECRET": "credential"})
	input := model.Result{Source: model.Source{Global: []string{"-c", "password=credential"}}, Operations: []model.Operation{{Target: model.Target{RemoteURL: "https://user:" + "credential" + "@example.test", Refs: model.Exact("credential")}, Editable: map[string]model.EditLocator{"x": {Kind: "argv"}}}}}
	got := r.Result(input)
	got.Operations[0].Target.Refs.Values[0] = "changed"
	if input.Operations[0].Target.Refs.Values[0] != "credential" || got.Operations[0].Editable != nil || strings.Contains(got.Operations[0].Target.RemoteURL, "credential") || strings.Contains(strings.Join(got.Source.Global, " "), "credential") {
		t.Fatal("copy/redaction failed")
	}
	ctx := &model.ResolvedContext{RepoRoot: "credential", Remotes: map[string]model.RemoteInfo{"origin": {URL: "https://user:" + "credential" + "@example.test", PushURLs: []string{"credential"}}}}
	out := r.Input(protocol.Input{Context: ctx})
	out.Context.Remotes["origin"].PushURLs[0] = "changed"
	if ctx.Remotes["origin"].PushURLs[0] != "credential" || out.Context.RepoRoot == "credential" {
		t.Fatal("context copy/redaction failed")
	}
}
