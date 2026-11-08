//go:build unix

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/install"
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/policy"
	"github.com/mkusaka/gthok/internal/protocol"
	"github.com/mkusaka/gthok/internal/schema"
)

func compatibilityFixture(t *testing.T, version string) (real, calls string) {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)
	for key, value := range map[string]string{"HOME": root, "GTHOK_CONFIG_DIR": filepath.Join(root, "config"), "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "PATH": "/usr/bin:/bin", "GTHOK_DEPTH": "0", "GTHOK_NESTED": "", "GTHOK_NESTED_TOKEN": ""} {
		t.Setenv(key, value)
	}
	real, calls = filepath.Join(root, "real-git"), filepath.Join(root, "calls")
	body := "#!/bin/sh\nif [ \"$1\" = --version ]; then\n  printf 'probe\\n' >> '" + calls + "'\n  printf 'git version " + version + "\\n'\n  exit 0\nfi\nif [ \"$1\" = version ]; then\n  printf 'execution\\n' >> '" + calls + "'\n  printf 'original output\\n'\n  exit 23\nfi\nexit 1\n"
	if err := os.WriteFile(real, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GTHOK_REAL_GIT", real)
	return real, calls
}

func compatibilityConfig(t *testing.T, mode, extra string) {
	t.Helper()
	path := config.DefaultPath(environment())
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	body := "version: 1\nruntime: {gitCompatibility: " + mode + ", resolveAPI: never}\n" + extra
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGitCompatibilityClassification(t *testing.T) {
	for _, version := range []string{"2.56.0", "2.55.9", "2.57.0"} {
		for _, mode := range []string{"conservative", "verified-only"} {
			t.Run(version+"/"+mode, func(t *testing.T) {
				real, calls := compatibilityFixture(t, version)
				c := &config.Config{Runtime: config.Runtime{GitCompatibility: mode}}
				c.ApplyDefaults()
				inv := model.Invocation{Argv: []string{"version"}}
				raw := normalizeInvocation(c, inv)
				if raw.Registry.Git != "v"+install.GitBaseline {
					t.Fatalf("version probe baseline %s differs from registry %s", install.GitBaseline, raw.Registry.Git)
				}
				gate := checkGitCompatibility(c, real, environment())
				got := gate.apply(raw)
				if version == install.GitBaseline {
					if got.Registry.Compatibility != model.CompatibilityVerified || !reflect.DeepEqual(got.Classification, raw.Classification) || !reflect.DeepEqual(got.Operations, raw.Operations) {
						t.Fatalf("verified result changed: %+v", got)
					}
				} else {
					if got.Registry.Compatibility != model.CompatibilityUnverified || got.Classification.MutationFree || got.Classification.RetrySafe {
						t.Fatalf("unverified result marked safe: %+v", got)
					}
					op := got.Operations[0]
					if len(op.Unresolved) == 0 || !strings.Contains(op.Evidence, version) {
						t.Fatalf("missing version provenance: %+v", op)
					}
					if mode == "conservative" {
						if op.Action != raw.Operations[0].Action || op.Effect != model.EffectRead || op.Coverage != model.CoverageConservative || got.Classification.Coverage != model.CoverageConservative {
							t.Fatalf("conservative result=%+v", got)
						}
					} else if op.Family != model.FamilyUnknown || op.Action != "unknown" || op.Effect != model.EffectUnknown || op.UnknownReason != model.UnknownUnverified || op.Coverage != model.CoverageOpaque || op.Target.Refs.Kind != model.SetUnknown {
						t.Fatalf("verified-only result=%+v", got)
					}
				}
				if !reflect.DeepEqual(raw, normalizeInvocation(c, inv)) {
					t.Fatal("gate mutated syntax result")
				}
				// Applying the snapshot again never launches another child.
				gate.apply(raw)
				data, err := os.ReadFile(calls)
				if err != nil || string(data) != "probe\n" {
					t.Fatalf("probes=%q err=%v", data, err)
				}
			})
		}
	}
}

func TestGitCompatibilityNeverPromotesOpaqueCoverage(t *testing.T) {
	c := &config.Config{}
	c.ApplyDefaults()
	raw := model.NewUnknown(model.LevelL1, model.UnknownShellAlias, "arbitrary shell alias")
	raw.Operations = append(raw.Operations, normalizeInvocation(c, model.Invocation{Argv: []string{"version"}}).Operations...)
	got := (gitCompatibility{status: model.CompatibilityUnverified, detail: "version unavailable"}).apply(raw)
	if got.Classification.Coverage != model.CoverageOpaque || got.Operations[0].Coverage != model.CoverageOpaque || got.Operations[1].Coverage != model.CoverageConservative {
		t.Fatalf("coverage was promoted: %+v", got)
	}
	for _, op := range got.Operations {
		if len(op.Unresolved) == 0 {
			t.Fatalf("missing per-operation uncertainty: %+v", op)
		}
	}
}

func TestGitCompatibilityProbeFailure(t *testing.T) {
	for _, body := range []string{"exit 5", "printf unexpected"} {
		t.Run(body, func(t *testing.T) {
			real, _ := compatibilityFixture(t, "2.56.0")
			if err := os.WriteFile(real, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			c := &config.Config{}
			c.ApplyDefaults()
			gate := checkGitCompatibility(c, real, environment())
			raw := normalizeInvocation(c, model.Invocation{Argv: []string{"version"}})
			got := gate.apply(raw)
			if got.Registry.Compatibility != model.CompatibilityUnverified || got.Classification.RetrySafe || !strings.Contains(got.Operations[0].Evidence, "version unavailable") {
				t.Fatalf("failed probe trusted: %+v", got)
			}
		})
	}
}

func TestVerifiedOnlyCannotExcludeUnknownFromSyntax(t *testing.T) {
	c := &config.Config{Policy: config.Policy{Mode: config.ModeEnforce, UnknownMutation: config.UnknownAllow, Rules: []config.Rule{{
		ID: "unknown", Decision: "deny", Match: config.Match{Operations: []string{"unknown"}},
		Unless: []config.Match{{Refs: []string{"refs/heads/main"}}},
	}}}}
	c.ApplyDefaults()
	raw := normalizeInvocation(c, model.Invocation{Argv: []string{"branch", "-D", "main"}})
	raw.Operations[0].Execution = model.ExecutionSkipped
	raw.Operations[0].Target.Refs = model.Exact("refs/heads/main")
	got := (gitCompatibility{status: model.CompatibilityUnverified, mode: "verified-only", detail: "unverified version"}).apply(raw)
	if got.Operations[0].Execution != model.ExecutionMaybe || !evaluateConfig(c, policy.Subject{Result: got}).WouldDeny {
		t.Fatalf("syntax-derived exclusion bypassed unknown rule: %+v", got)
	}
}

func TestManagementCompatibilityAndOffline(t *testing.T) {
	for _, noResolve := range []bool{false, true} {
		for _, command := range []string{"normalize", "explain", "hooks"} {
			t.Run(fmt.Sprintf("%s/offline=%t", command, noResolve), func(t *testing.T) {
				_, calls := compatibilityFixture(t, "2.57.0")
				compatibilityConfig(t, "verified-only", "policy: {mode: enforce, unknownMutation: deny}\n")
				args := []string{command}
				switch command {
				case "hooks":
					args = append(args, "test", "--print-input")
				case "explain":
					args = append(args, "--json")
				}
				if noResolve {
					args = append(args, "--no-resolve")
				}
				code, out, errOut := hooksExpectRun(t, append(args, "--", "version")...)
				if code != 0 {
					t.Fatalf("code=%d stdout=%s stderr=%s", code, out, errOut)
				}
				var got model.Result
				switch command {
				case "explain":
					var report schema.Explain
					if err := json.Unmarshal([]byte(out), &report); err != nil {
						t.Fatal(err)
					}
					got = report.Normalization
					if report.Policy.WouldDeny == noResolve {
						t.Fatalf("policy ignored compatibility: %+v", report.Policy)
					}
				case "hooks":
					var input protocol.Input
					if err := json.Unmarshal([]byte(out), &input); err != nil {
						t.Fatal(err)
					}
					got.Classification, got.Operations = input.Classification, input.Operations
				default:
					if err := json.Unmarshal([]byte(out), &got); err != nil {
						t.Fatal(err)
					}
				}
				if (got.Operations[0].UnknownReason == model.UnknownUnverified) == noResolve {
					t.Fatalf("offline=%t result=%+v", noResolve, got)
				}
				if command != "hooks" && (got.Registry.Compatibility == "") != noResolve {
					t.Fatalf("compatibility=%s offline=%t", got.Registry.Compatibility, noResolve)
				}
				data, err := os.ReadFile(calls)
				if noResolve {
					if !os.IsNotExist(err) {
						t.Fatalf("offline analysis launched Git: %q %v", data, err)
					}
				} else if err != nil || string(data) != "probe\n" {
					t.Fatalf("probes=%q err=%v", data, err)
				}
			})
		}
	}
}

func TestCompatibilitySurvivesTransforms(t *testing.T) {
	for _, mode := range []string{"conservative", "verified-only"} {
		t.Run(mode, func(t *testing.T) {
			_, calls := compatibilityFixture(t, "2.57.0")
			inputPath := filepath.Join(t.TempDir(), "transform-input.json")
			response := "cat > '" + inputPath + "'\n" + `printf '{"schemaVersion":1,"env":{"COMPAT_TEST":"changed"}}'`
			compatibilityConfig(t, mode, fmt.Sprintf("transforms: {pre: [{id: edit-env, command: [/bin/sh, -c, %q]}]}\n", response))
			a, err := managementAnalyze([]string{"version"}, true, false, true, false)
			if err != nil {
				t.Fatal(err)
			}
			defer a.cleanup()
			if len(a.transformIDs) != 1 || a.env["COMPAT_TEST"] != "changed" || a.result.Registry.Compatibility != model.CompatibilityUnverified || a.result.Classification.RetrySafe {
				t.Fatalf("transform lost compatibility: %+v", a)
			}
			if (a.result.Operations[0].UnknownReason == model.UnknownUnverified) != (mode == "verified-only") {
				t.Fatalf("transform lost mode: %+v", a.result)
			}
			inputData, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatal(err)
			}
			var input protocol.Input
			if err := json.Unmarshal(inputData, &input); err != nil {
				t.Fatal(err)
			}
			if input.Classification.RetrySafe || input.Classification.MutationFree || (input.Operations[0].UnknownReason == model.UnknownUnverified) != (mode == "verified-only") {
				t.Fatalf("transform did not receive compatibility view: %+v", input)
			}
			data, err := os.ReadFile(calls)
			if err != nil || string(data) != "probe\n" {
				t.Fatalf("probes=%q err=%v", data, err)
			}
		})
	}
}

func TestCompatibilitySemanticTransform(t *testing.T) {
	for _, mode := range []string{"conservative", "verified-only"} {
		t.Run(mode, func(t *testing.T) {
			compatibilityFixture(t, "2.57.0")
			response := `printf '{"schemaVersion":1,"set":{"/operations/0/texts/body/value":"new"}}'`
			compatibilityConfig(t, mode, fmt.Sprintf("transforms: {pre: [{id: edit-message, command: [/bin/sh, -c, %q]}]}\n", response))
			a, err := managementAnalyze([]string{"commit", "-m", "old"}, true, false, true, false)
			if mode == "verified-only" {
				if err == nil {
					a.cleanup()
					t.Fatal("semantic edit accepted without a verified-version adapter")
				}
				if !strings.Contains(err.Error(), "unsupported edit") {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer a.cleanup()
			if a.result.Operations[0].Texts.Body.Value != "new" || a.result.Registry.Compatibility != model.CompatibilityUnverified || a.result.Classification.Coverage == model.CoverageSemantic {
				t.Fatalf("conservative edit failed roundtrip: %+v", a.result)
			}
		})
	}
}

func TestCompatibilityWrapperHelper(t *testing.T) {
	if os.Getenv("GTHOK_T12_WRAPPER_HELPER") == "1" {
		os.Exit(wrapper([]string{"version"}, "git", "shim"))
	}
}

func TestCompatibilityWrapperPassthroughAndPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, mode, extra, calls string
		want                     int
	}{
		{"no-config", "", "", "execution\n", 23},
		{"conservative", "conservative", "policy: {mode: enforce, unknownMutation: deny}\n", "probe\nexecution\n", 23},
		{"verified-only-allow", "verified-only", "policy: {mode: enforce, unknownMutation: allow}\n", "probe\nexecution\n", 23},
		{"verified-only-deny", "verified-only", "policy: {mode: enforce, unknownMutation: deny}\n", "probe\n", 77},
		{"unknown-rule-deny", "verified-only", "policy: {mode: enforce, unknownMutation: allow, rules: [{id: unknown, match: {operation: unknown}, decision: deny}]}\n", "probe\n", 77},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, calls := compatibilityFixture(t, "2.57.0")
			if tc.mode != "" {
				compatibilityConfig(t, tc.mode, tc.extra)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestCompatibilityWrapperHelper$")
			cmd.Env = append(os.Environ(), "GTHOK_T12_WRAPPER_HELPER=1")
			out, err := cmd.CombinedOutput()
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != tc.want {
				t.Fatalf("error=%v want code=%d output=%s", err, tc.want, out)
			}
			data, err := os.ReadFile(calls)
			if err != nil || string(data) != tc.calls {
				t.Fatalf("calls=%q err=%v, want %q", data, err, tc.calls)
			}
			if tc.want == 23 && string(out) != "original output\n" {
				t.Fatalf("passthrough output changed: %s", out)
			}
		})
	}
}

func TestVersionJSONAndText(t *testing.T) {
	for _, tc := range []struct{ version, commit string }{{"v0.0.1", "abc123"}, {"", ""}} {
		t.Run(tc.version, func(t *testing.T) {
			stdout, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stdout.Close() }()
			old := os.Stdout
			os.Stdout = stdout
			defer func() { os.Stdout = old }()
			if code := Management([]string{"version", "--json"}, tc.version, tc.commit); code != 0 {
				t.Fatalf("code=%d", code)
			}
			data, err := os.ReadFile(stdout.Name())
			if err != nil {
				t.Fatal(err)
			}
			var got schema.Version
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			wantVersion := tc.version
			if wantVersion == "" {
				wantVersion = "dev"
			}
			want := schema.Version{Version: wantVersion, Commit: tc.commit, Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
			if got != want {
				t.Fatalf("metadata=%+v, want %+v", got, want)
			}
			if tc.commit == "" && strings.Contains(string(data), `"commit"`) {
				t.Fatal("empty commit should be omitted")
			}
			code, out, errOut := hooksExpectRun(t, "version")
			if code != 0 || errOut != "" || !strings.HasPrefix(out, "gthok test\ncommit test\n") || !strings.Contains(out, runtime.GOOS+"/"+runtime.GOARCH) {
				t.Fatalf("text output changed: code=%d stdout=%s stderr=%s", code, out, errOut)
			}
		})
	}
}

func TestDocsSearchGitExample(t *testing.T) {
	code, out, errOut := hooksExpectRun(t, "docs", "search", "--help")
	if code != 0 || errOut != "" || !strings.Contains(out, `gthok docs search "force push lease"`) || strings.Contains(out, "GH_TOKEN") || strings.Contains(out, "gain") {
		t.Fatalf("help: code=%d stdout=%s stderr=%s", code, out, errOut)
	}
}
