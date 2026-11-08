package redact_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/protocol"
	"github.com/mkusaka/gthok/internal/redact"
)

const replacement = "[REDACTED]"

func TestAddedSecretsSurviveReplacement(t *testing.T) {
	original, added := "original-"+"credential", "short"
	r := redact.New(map[string]string{"GH_TOKEN": original})
	r.AddSecrets("", added, "long-"+added)
	r.AddSecrets("replacement-" + "credential")
	for _, secret := range []string{original, added, "long-" + added, "replacement-" + "credential"} {
		if got := r.String(secret); got != replacement {
			t.Fatalf("learned value not fully masked: %q", got)
		}
	}
	r.AddSecrets(replacement)
	if got := r.String(added); strings.Contains(got, added) || strings.Contains(got, replacement) {
		t.Fatalf("replacement marker exposes learned value: %q", got)
	}
}

func TestRedactionCounts(t *testing.T) {
	secret := "learned-" + "count-credential"
	pattern := "ghp_" + strings.Repeat("c", 36)
	r := redact.New(map[string]string{"GH_TOKEN": secret})
	for _, tc := range []struct {
		name string
		in   any
		want any
		n    int
	}{
		{"repeated-learned", secret + " " + secret, replacement + " " + replacement, 2},
		{"pattern-and-learned", pattern + " " + secret, replacement + " " + replacement, 2},
		{"already-masked", replacement, replacement, 0},
		{"authorization", "Bearer " + secret, "Bearer " + replacement, 1},
		{"sensitive-map", map[string]any{"password": "short-" + "password", "nested": map[string]any{"token": []any{secret}}}, map[string]any{"password": replacement, "nested": map[string]any{"token": replacement}}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out any
			n := r.WithCount(func(r *redact.Redactor) { out = r.Value(tc.in) })
			if n != tc.n || !reflect.DeepEqual(out, tc.want) {
				t.Fatalf("redacted=%#v count=%d; want %#v count=%d", out, n, tc.want, tc.n)
			}
			n = r.WithCount(func(r *redact.Redactor) { _ = r.Value(out) })
			if n != 0 {
				t.Fatalf("redacting an already masked value counted %d substitutions", n)
			}
		})
	}
	t.Run("contextual-argv", func(t *testing.T) {
		args := []string{"secret", "set", "NAME", "--body", secret}
		var out []string
		n := r.WithCount(func(r *redact.Redactor) { out = r.Argv(args) })
		if n != 1 || out[4] != replacement {
			t.Fatalf("argv=%q count=%d", out, n)
		}
	})
	t.Run("overlap", func(t *testing.T) {
		long := "abcdefgh" + "ij"
		r := redact.New(map[string]string{"GH_TOKEN": long, "OTHER_TOKEN": long[2:]})
		var out string
		n := r.WithCount(func(r *redact.Redactor) { out = r.String(long + long[2:]) })
		if n != 2 || out != replacement+replacement {
			t.Fatalf("overlapping credentials: %q count=%d", out, n)
		}
	})
}

func TestHookInputDisplayPreservesPayloadCounters(t *testing.T) {
	secret := "display-only-" + "credential"
	r := redact.New(map[string]string{"GH_TOKEN": secret})
	in := protocol.Input{
		Redactions: 7,
		Invocation: protocol.InvocationInfo{Argv: []string{"pr", "create", "-b", secret}},
		Operations: []model.Operation{{Texts: &model.Texts{
			Title: &model.Text{Value: "ordinary", Redactions: 2},
			Body:  &model.Text{Value: secret, Bytes: int64(len(secret)), Redactions: 3},
			Notes: &model.Text{Value: secret, Redactions: 1},
		}}},
	}
	var out protocol.Input
	count := r.WithCount(func(r *redact.Redactor) { out = r.HookInput(in) })
	if count != 3 || out.Redactions != 7 || out.Operations[0].Texts.Title.Redactions != 2 || out.Operations[0].Texts.Body.Redactions != 3 || out.Operations[0].Texts.Notes.Redactions != 1 {
		t.Fatalf("display count=%d payload=%+v texts=%+v", count, out, out.Operations[0].Texts)
	}
	if out.Invocation.Argv[3] != replacement || out.Operations[0].Texts.Body.Value != replacement || out.Operations[0].Texts.Notes.Value != replacement {
		t.Fatalf("display redaction missed credential: %+v", out)
	}
	if in.Invocation.Argv[3] != secret || in.Operations[0].Texts.Body.Value != secret || out.Operations[0].Texts.Body.Bytes != int64(len(secret)) {
		t.Fatal("display masking mutated source input or original byte count")
	}
}

func TestStringPatterns(t *testing.T) {
	ghToken := "ghp_" + strings.Repeat("a", 36)
	r := redact.New(map[string]string{"CUSTOM_SECRET": "learned-value", "GH_TOKEN": "tiny"})
	tests := []struct{ name, input, want string }{
		{"learned anywhere", "prelearned-valuepost", "pre" + replacement + "post"},
		{"short github env", "tiny", replacement},
		{"github classic", "token=" + ghToken, "token=" + replacement},
		{"github oauth", "gho_" + strings.Repeat("b", 36), replacement},
		{"github user", "ghu_" + strings.Repeat("c", 36), replacement},
		{"github server", "ghs_" + strings.Repeat("d", 36), replacement},
		{"github refresh", "ghr_" + strings.Repeat("e", 36), replacement},
		{"fine grained", "github_pat_11_" + strings.Repeat("a", 70), replacement},
		{"v1 token", "v1." + strings.Repeat("a", 40), replacement},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9" + ".eyJzdWIiOiJmYWtlIn0.signature_fake", replacement},
		{"bearer", "Bearer short", "Bearer " + replacement},
		{"token authorization", "TOKEN short", "TOKEN " + replacement},
		{"basic", "Basic dXNlcjpwYXNz", "Basic " + replacement},
		{"authorization header", "Authorization: unusual short value", "Authorization: " + replacement},
		{"proxy authorization", "Proxy-Authorization: Basic short", "Proxy-Authorization: " + replacement},
		{"url auth", "visit https://user:p%40ss@host/path", "visit https://" + replacement + "@host/path"},
		{"private key", "before\n-----BEGIN RSA PRIVATE " + "KEY-----\nnot-a-key\n-----END RSA PRIVATE KEY-----\nafter", "before\n" + replacement + "\nafter"},
		{"private key unfinished", "-----BEGIN OPENSSH PRIVATE " + "KEY-----\nnot-a-key", replacement},
		{"ordinary prose", "keyboard tokenizer monkey key tokenization", "keyboard tokenizer monkey key tokenization"},
		{"URL username credential", "https://user@host/path", "https://" + replacement + "@host/path"},
		{"short pattern", "ghp_short", "ghp_short"},
		{"empty", "", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := r.String(test.input); got != test.want {
				t.Fatalf("String(%q) = %q; want %q", test.input, got, test.want)
			}
			if got := r.String(test.want); got != test.want {
				t.Fatalf("redaction is not idempotent: %q -> %q", test.want, got)
			}
		})
	}
}

func TestEnvironmentLearning(t *testing.T) {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "SERVICE_TOKEN", "MY_SECRET", "APP_PASSWORD", "APP_PASSWD", "SERVICE_API_KEY", "SSH_PRIVATE_KEY", "CREDENTIALS"} {
		t.Run(name, func(t *testing.T) {
			env := map[string]string{name: "fake-credential-value"}
			r := redact.New(env)
			env[name] = "changed-environment"
			if got := r.String("fake-credential-value"); got != replacement {
				t.Fatalf("secret not learned: %q", got)
			}
		})
	}
	r := redact.New(map[string]string{"APP_TOKEN": "short", "ORDINARY": "ordinary-long", "EMPTY_SECRET": ""})
	if got := r.String("short ordinary-long"); got != "short ordinary-long" {
		t.Fatalf("unexpected environment learning: %q", got)
	}
	var zero redact.Redactor
	if got := zero.String("Bearer fake"); got != "Bearer "+replacement {
		t.Fatalf("zero-value redactor: %q", got)
	}
}

func TestLearnedOverlapsAndMarkerBoundaries(t *testing.T) {
	tests := []struct {
		secrets []string
		input   string
	}{
		{[]string{"abcdefgh", "cdefghij"}, "abcdefghijabcdefgh"},
		{[]string{"abcdefgh", "x[REDACTED]y"}, "xabcdefghy"},
		{[]string{"[REDACTED]"}, "[REDACTED] ghp_" + strings.Repeat("a", 36)},
		{[]string{"REDACTED"}, "REDACTED Bearer secret"},
		{[]string{"aa"}, "aaaaaaa"},
		{[]string{"x", "y"}, "xxyyBearer xxx"},
	}
	for _, test := range tests {
		env := make(map[string]string)
		for i, value := range test.secrets {
			if i == 0 {
				env["GH_TOKEN"] = value
			} else {
				env["GITHUB_TOKEN"] = value
			}
		}
		got := redact.New(env).String(test.input)
		for _, value := range test.secrets {
			if strings.Contains(got, value) {
				t.Fatalf("output %q contains learned value %q", got, value)
			}
		}
	}
}

func TestValueSensitiveNamesAndIndependentCopy(t *testing.T) {
	names := []string{"token", "accessToken", "refresh_token", "id-token", "authToken", "GH_TOKEN", "githubToken", "ghEnterpriseToken", "github_enterprise_token", "password", "PASSWD", "secret", "clientSecret", "api_key", "private-key", "credential", "credentials", "Authorization", "proxy_authorization"}
	input := map[string]any{"key": "keep", "keyboard": "keep", "tokenizer": "keep", "publicKey": "keep", "nested": []any{map[string]any{"ordinary": "learned-value"}}}
	for _, name := range names {
		input[name] = map[string]any{"anything": "short"}
	}
	before := mustJSON(t, input)
	out := redact.New(map[string]string{"APP_SECRET": "learned-value"}).Value(input).(map[string]any)
	for _, name := range names {
		if out[name] != replacement {
			t.Errorf("sensitive name %q not redacted", name)
		}
	}
	for _, name := range []string{"key", "keyboard", "tokenizer", "publicKey"} {
		if out[name] != "keep" {
			t.Errorf("ordinary name %q redacted", name)
		}
	}
	if out["nested"].([]any)[0].(map[string]any)["ordinary"] != replacement {
		t.Fatal("nested learned value survived")
	}
	out["nested"].([]any)[0].(map[string]any)["ordinary"] = "changed"
	if !bytes.Equal(before, mustJSON(t, input)) {
		t.Fatal("Value shares mutable input")
	}
	typed := map[string]string{"token": "short", "ordinary": "learned-value"}
	typedOut := redact.New(map[string]string{"APP_SECRET": "learned-value"}).Value(typed).(map[string]string)
	typedOut["ordinary"] = "changed"
	if typed["ordinary"] != "learned-value" || typedOut["token"] != replacement {
		t.Fatal("typed map copied incorrectly")
	}
}

func TestConcurrentRedactor(t *testing.T) {
	r := redact.New(map[string]string{"GH_TOKEN": "learned-credential"})
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				if got := r.String("prefix learned-credential suffix"); got != "prefix "+replacement+" suffix" {
					t.Errorf("concurrent String = %q", got)
				}
			}
		}()
	}
	wg.Wait()
}

func TestValueByteBodiesAndNilContainers(t *testing.T) {
	r := redact.New(nil)
	input := json.RawMessage(`{"token":"short","key":"keep"}`)
	before := append(json.RawMessage(nil), input...)
	out := r.Value(input).(json.RawMessage)
	if strings.Contains(string(out), "short") || !strings.Contains(string(out), "keep") {
		t.Fatalf("raw JSON body: %q", out)
	}
	out[0] = 'x'
	if !bytes.Equal(input, before) {
		t.Fatal("RawMessage shares input")
	}
	for _, value := range []any{map[string]any(nil), map[string]string(nil), []any(nil), []string(nil), []byte(nil), json.RawMessage(nil), nil, true, 42, json.Number("123")} {
		if !reflect.DeepEqual(value, r.Value(value)) {
			t.Fatalf("nil/scalar changed: %#v", value)
		}
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestValueTypedJSONContainers(t *testing.T) {
	type text string
	input := map[text][]map[string]any{
		"nested": {{"token": "short", "ordinary": "learned-credential", "numbers": []int{1, 2}}},
	}
	before := mustJSON(t, input)
	out := redact.New(map[string]string{"GH_TOKEN": "learned-credential"}).Value(input).(map[string]any)
	nested := out["nested"].([]any)[0].(map[string]any)
	if nested["token"] != replacement || nested["ordinary"] != replacement {
		t.Fatal("typed nested JSON credentials survived")
	}
	nested["numbers"].([]any)[0] = 99
	nested["token"] = "changed"
	if !bytes.Equal(before, mustJSON(t, input)) {
		t.Fatal("typed JSON container shares input")
	}
	words := []string{"learned-credential", "keep"}
	copied := redact.New(map[string]string{"GH_TOKEN": "learned-credential"}).Value(words).([]string)
	copied[1] = "changed"
	if words[1] != "keep" || copied[0] != replacement {
		t.Fatal("typed string slice shares input or leaks")
	}
}

func FuzzRedactString(f *testing.F) {
	for _, seed := range []struct{ input, secret string }{
		{"before learned-credential after", "learned-credential"},
		{"[REDACTED]", "[REDACTED]"},
		{"Bearer short", "REDACTED"},
		{"xabcdefghy", "abcdefgh"},
		{"aaaaa", "a"},
		{"\x00\xffsecret\x00", "\xffsecret"},
		{"plain text", ""},
	} {
		f.Add(seed.input, seed.secret)
	}
	f.Fuzz(func(t *testing.T, input, secret string) {
		r := redact.New(map[string]string{"GH_TOKEN": secret, "SECOND_SECRET": "fuzz-fixed-credential", "THIRD_SECRET": "x[REDACTED]y"})
		out := r.String(input + secret + "fuzz-fixed-credential")
		for _, value := range []string{secret, "fuzz-fixed-credential", "x[REDACTED]y"} {
			if value != "" && strings.Contains(out, value) {
				t.Fatalf("output %q contains learned value %q", out, value)
			}
		}
	})
}

func BenchmarkRedactorResult(b *testing.B) {
	r := redact.New(map[string]string{"GH_TOKEN": "fake-learned-github-credential"})
	input := model.Result{
		SchemaVersion:  model.SchemaVersion,
		Classification: model.Classification{Level: model.LevelL1, Confidence: model.ConfidenceExact, Resolution: model.ResolutionComplete},
		Source:         model.Source{Kind: "command", Argv: []string{"push", "https://user:" + "fake-learned-github-credential" + "@example.test/repo"}},
		Operations:     []model.Operation{{Family: "ref", Action: "ref.update", Effect: model.EffectWrite, Execution: model.ExecutionWillRun, Target: model.Target{RepoRoot: "/repo", Refs: model.Exact("refs/heads/main")}, Parameters: map[string]any{"variables": map[string]any{"token": "fake-learned-github-credential", "ref": "refs/heads/main"}, "nested": []any{map[string]any{"ordinary": "keep"}}}}},
	}
	b.ReportAllocs()
	for b.Loop() {
		benchmarkResult = r.Result(input)
	}
}

var benchmarkResult model.Result

func TestPreviousWarningRedactedCopy(t *testing.T) {
	secret := "warning-" + "credential"
	in := protocol.Input{Previous: []protocol.HookResult{{Warning: secret}}}
	got := redact.New(map[string]string{"GH_TOKEN": secret}).Input(in)
	if got.Previous[0].Warning != replacement || in.Previous[0].Warning != secret {
		t.Fatalf("warning redaction mutated input or leaked: got=%+v input=%+v", got.Previous, in.Previous)
	}
}
