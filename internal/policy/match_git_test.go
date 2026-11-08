package policy

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/protocol"
)

func assertMatch(t *testing.T, m config.Match, op model.Operation, want string) {
	t.Helper()
	s := subject(op)
	yes, indices := Matches(m, nil, s)
	maybe, candidates := MatchMaybe(m, nil, s)
	if yes != (want == "yes") || maybe != (want == "maybe") {
		t.Fatalf("match=%+v target=%+v: yes=%v maybe=%v, want %s", m, op.Target, yes, maybe, want)
	}
	if yes && !reflect.DeepEqual(indices, []int{0}) || maybe && !reflect.DeepEqual(candidates, []int{0}) {
		t.Fatalf("indices=%v candidates=%v", indices, candidates)
	}
}

func TestGitMatchers(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    config.Match
		set  func(*model.Operation, bool)
	}{
		{"scopes", config.Match{Scopes: config.StringList{"remote-ref", "index"}}, func(op *model.Operation, hit bool) {
			op.Scopes = []model.Scope{model.ScopeWorktree}
			if hit {
				op.Scopes = append(op.Scopes, model.ScopeIndex)
			}
		}},
		{"capabilities", config.Match{Capabilities: config.StringList{"network", "external-exec"}}, func(op *model.Operation, hit bool) {
			op.Capabilities = []model.Capability{model.CapabilityPager}
			if hit {
				op.Capabilities = append(op.Capabilities, model.CapabilityExternalExec)
			}
		}},
		{"repoRoots", config.Match{RepoRoots: config.StringList{"/repos/**/project"}}, func(op *model.Operation, hit bool) {
			op.Target.RepoRoot = "/elsewhere/project"
			if hit {
				op.Target.RepoRoot = "/repos/team/project"
			}
		}},
		{"worktrees", config.Match{Worktrees: config.StringList{"/repos/project/wt-*"}}, func(op *model.Operation, hit bool) {
			op.Target.Worktree = "/repos/project/main"
			if hit {
				op.Target.Worktree = "/repos/project/wt-feature"
			}
		}},
		{"commonDirs", config.Match{CommonDirs: config.StringList{"/repos/**/.git"}}, func(op *model.Operation, hit bool) {
			op.Target.CommonDir = "/elsewhere/.git"
			if hit {
				op.Target.CommonDir = "/repos/project/.git"
			}
		}},
		{"remotes", config.Match{Remotes: config.StringList{"origin", "backup"}}, func(op *model.Operation, hit bool) {
			op.Target.Remote = "upstream"
			if hit {
				op.Target.Remote = "backup"
			}
		}},
		{"remoteURLs", config.Match{RemoteURLs: config.StringList{"ssh://git@example.test/team/**"}}, func(op *model.Operation, hit bool) {
			op.Target.RemoteURL = "https://example.test/team/repo.git"
			if hit {
				op.Target.RemoteURL = "git@EXAMPLE.test:team/repo.git"
			}
		}},
		{"refs", config.Match{Refs: config.StringList{"refs/heads/main", "refs/heads/release/*"}}, func(op *model.Operation, hit bool) {
			op.Target.Refs = model.Exact("refs/tags/v1")
			if hit {
				op.Target.Refs = model.Exact("refs/heads/release/v1")
			}
		}},
		{"paths", config.Match{Paths: config.StringList{"src/**/*.go"}}, func(op *model.Operation, hit bool) {
			op.Target.Paths = model.Exact("docs/README.md")
			if hit {
				op.Target.Paths = model.Exact("./src/internal/main.go")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, hit := range []bool{false, true} {
				op := operation("remote.push.update", "", model.EffectWrite)
				tc.set(&op, hit)
				want := "no"
				if hit {
					want = "yes"
				}
				assertMatch(t, tc.m, op, want)
			}
			op := operation("unknown", "", model.EffectUnknown)
			assertMatch(t, tc.m, op, "maybe")
			// A known effect without the requested side channel is a definite no.
			if tc.name == "scopes" || tc.name == "capabilities" {
				op.Effect = model.EffectRead
				assertMatch(t, tc.m, op, "no")
			}
			if yes, _ := Matches(tc.m, nil, subject()); yes {
				t.Fatal("target criterion selected invocation without operations")
			}
		})
	}
}

func TestGitTargetSets(t *testing.T) {
	for _, field := range []string{"refs", "paths"} {
		matching, missing := "refs/heads/main", "refs/tags/v1"
		m := config.Match{Refs: config.StringList{matching}}
		if field == "paths" {
			matching, missing = "src/main.go", "docs/readme.md"
			m = config.Match{Paths: config.StringList{matching}}
		}
		for _, tc := range []struct {
			name   string
			target *model.TargetSet
			want   string
		}{
			{"nil", nil, "maybe"},
			{"exact-hit", model.Exact(matching), "yes"},
			{"exact-miss", model.Exact(missing), "no"},
			{"exact-incomplete-hit", &model.TargetSet{Kind: model.SetExact, Values: []string{matching}}, "yes"},
			{"exact-incomplete-miss", &model.TargetSet{Kind: model.SetExact, Values: []string{missing}}, "maybe"},
			{"set-hit", &model.TargetSet{Kind: model.SetSet, Values: []string{missing, matching}, Complete: true}, "yes"},
			{"set-miss", &model.TargetSet{Kind: model.SetSet, Values: []string{missing}, Complete: true}, "no"},
			{"set-empty-complete", &model.TargetSet{Kind: model.SetSet, Complete: true}, "no"},
			{"set-incomplete-hit", &model.TargetSet{Kind: model.SetSet, Values: []string{matching}}, "yes"},
			{"set-incomplete-miss", &model.TargetSet{Kind: model.SetSet, Values: []string{missing}}, "maybe"},
			{"pattern-matching", &model.TargetSet{Kind: model.SetPattern, Values: []string{matching}, Complete: true}, "maybe"},
			{"pattern-disjoint", &model.TargetSet{Kind: model.SetPattern, Values: []string{missing}, Complete: true}, "maybe"},
			{"unknown", model.UnknownSet(), "maybe"},
			{"none", &model.TargetSet{Kind: model.SetNone}, "no"},
			{"unspecified", &model.TargetSet{}, "maybe"},
		} {
			t.Run(field+"/"+tc.name, func(t *testing.T) {
				op := operation("remote.push.update", "", model.EffectWrite)
				if field == "paths" {
					op.Target.Paths = tc.target
				} else {
					op.Target.Refs = tc.target
				}
				assertMatch(t, m, op, tc.want)
			})
		}
	}
}

func TestGitPathspecMagicMaybe(t *testing.T) {
	m := config.Match{Paths: config.StringList{"src/**"}}
	for _, magic := range []string{":(glob)src/**", ":(exclude)src/secret", ":!docs", ":/src"} {
		op := operation("index.add", "", model.EffectWrite)
		op.Target.Paths = model.Exact(magic)
		assertMatch(t, m, op, "maybe")
		op.Target.Paths = &model.TargetSet{Kind: model.SetSet, Values: []string{magic, "src/main.go"}, Complete: true}
		assertMatch(t, m, op, "yes")
	}
}

func TestGitCriteriaStayOnSameTarget(t *testing.T) {
	a := operation("remote.push.update", "/repos/a", model.EffectWrite)
	a.Target.Remote, a.Target.Refs = "A", model.Exact("refs/heads/X")
	b := operation("remote.push.update", "/repos/b", model.EffectWrite)
	b.Target.Remote, b.Target.Refs = "B", model.Exact("refs/heads/Y")
	s := subject(a, b)
	m := config.Match{Remotes: config.StringList{"A"}, Refs: config.StringList{"refs/heads/Y"}}
	if yes, _ := Matches(m, nil, s); yes {
		t.Fatal("remote A borrowed ref Y from remote B")
	}
	if maybe, _ := MatchMaybe(m, nil, s); maybe {
		t.Fatal("definite nonmatch became maybe")
	}
	m.Refs = config.StringList{"refs/heads/X"}
	if yes, indices := Matches(m, nil, s); !yes || !reflect.DeepEqual(indices, []int{0}) {
		t.Fatalf("same-target match: %v %v", yes, indices)
	}
	// A definite mismatch dominates an unknown criterion on the same op.
	a.Target.Refs = model.UnknownSet()
	assertMatch(t, config.Match{Remotes: config.StringList{"B"}, Refs: m.Refs}, a, "no")
}

func TestGitFilesystemNormalization(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(filepath.Join(real, "project"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, filepath.Join(real, "project"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ pattern, value string }{
		{filepath.Join(link, "**"), filepath.Join(real, "project")},
		{filepath.Join(real, "*"), filepath.Join(link, "project")},
		{filepath.Join(link, "**"), filepath.Join(real, "future", "project")},
		{relative, filepath.Join(link, "project") + "/../project/"},
	} {
		if !matchFilesystem(tc.pattern, tc.value) {
			t.Errorf("%q did not match %q", tc.pattern, tc.value)
		}
	}
}

func TestGitGlobs(t *testing.T) {
	for _, tc := range []struct {
		pattern, value string
		want           bool
	}{
		{"**", "refs/heads/main", true},
		{"refs/heads/*", "refs/heads/release/v1", false},
		{"refs/heads/**", "refs/heads/release/v1", true},
		{"src/**/*.go", "src/main.go", true},
		{"src/**/*.go", "src/deep/main.go", true},
		{"src/**/main.?o", "src/a/b/main.go", true},
		{"src/[ab].go", "src/a.go", true},
		{"src/[!a].go", "src/b.go", false}, // filepath.Match uses ^, not !, to negate.
		{"src/[^a].go", "src/b.go", true},
		{"src/**", "source/main.go", false},
		{"[", "anything", false},
	} {
		if got := globMatch(tc.pattern, tc.value); got != tc.want {
			t.Errorf("%q, %q: %v, want %v", tc.pattern, tc.value, got, tc.want)
		}
	}
}

func TestGitRemoteURLs(t *testing.T) {
	credentialURL := "https://user:" + "not-a-credential" + "@EXAMPLE.test:443/team/./repo.git?ignored=value#fragment"
	for _, tc := range []struct {
		pattern, value string
		want           bool
	}{
		{"https://example.test/team/repo.git", credentialURL, true},
		{"HTTPS://EXAMPLE.test/team/*", "https://example.test/team/repo.git", true},
		{"https://example.test/", "https://example.test?ignored=value#fragment", true},
		{"ssh://example.test/team/repo.git", "git@EXAMPLE.test:team/repo.git", true},
		{"git@example.test:team/*", "ssh://other@example.test:22/team/repo.git", true},
		{"ssh://example.test/team/**", "https://example.test/team/repo.git", false},
		{"https://*.test/team/repo.?it", "https://example.test/team/repo.git", true},
		{"https://example.test/team/*", "https://example.test/team/nested/repo.git", false},
		{"https://example.test/team/**", "https://example.test/team/nested/repo.git", true},
		{"https://example.test/team/**", "https://example.test/TEAM/repo.git", false},
		{"https://example.test/team/repo.git", "https://example.test/team/%72epo.git", true},
		{"**", "git@example.test:team/repo.git", true},
		{"file:///repos/**", "/repos/team/repo.git", true},
		{"ssh://example.test/team/**", "ssh:///team/repo.git", false},
	} {
		if got := matchRemoteURL(tc.pattern, tc.value); got != tc.want {
			t.Errorf("%q, %q: %v, want %v", tc.pattern, tc.value, got, tc.want)
		}
	}
	op := operation("remote.push.update", "", model.EffectWrite)
	op.Target.RemoteURL = "https://example.test/read/repo.git"
	op.Target.PushURL = "ssh://example.test/write/repo.git"
	assertMatch(t, config.Match{RemoteURLs: config.StringList{"ssh://example.test/write/**"}}, op, "yes")
	assertMatch(t, config.Match{RemoteURLs: config.StringList{"https://example.test/read/**"}}, op, "no")
	op.Target.PushURL = "ssh:///missing-host"
	assertMatch(t, config.Match{RemoteURLs: config.StringList{"ssh://example.test/write/**"}}, op, "maybe")
	assertMatch(t, config.Match{Remotes: config.StringList{"Origin"}}, model.Operation{Target: model.Target{Remote: "origin"}}, "no")
}

func TestGitConservativeRulesAndUnless(t *testing.T) {
	op := operation("remote.push.update", "", model.EffectWrite)
	op.Target.Remote = "origin"
	m := config.Match{Refs: config.StringList{"refs/heads/main"}}
	p := config.Policy{Mode: config.ModeEnforce, Rules: []config.Rule{{ID: "protected", Decision: "deny", Reason: "protected ref", Match: m}}}
	got := Evaluate(p, subject(op))
	if got.Decision != protocol.DecisionDeny || !strings.Contains(strings.Join(got.Reasons, " "), "matched conservatively") {
		t.Fatalf("unresolved deny did not apply: %+v", got)
	}
	p.Rules[0].OnUnresolved = config.OnErrorIgnore
	if got := Evaluate(p, subject(op)); got.Decision != protocol.DecisionAllow {
		t.Fatalf("explicit onUnresolved ignore: %+v", got)
	}
	p.Rules[0].OnUnresolved = ""
	p.Rules[0].Match = config.Match{Remotes: config.StringList{"origin"}}
	p.Rules[0].Unless = []config.Match{m}
	if yes, _ := Matches(p.Rules[0].Match, p.Rules[0].Unless, subject(op)); !yes {
		t.Fatal("maybe unless excluded definite match")
	}
	if got := Evaluate(p, subject(op)); got.Decision != protocol.DecisionDeny {
		t.Fatalf("maybe unless lifted deny: %+v", got)
	}
	op.Target.Refs = model.Exact("refs/heads/main")
	if yes, _ := Matches(p.Rules[0].Match, p.Rules[0].Unless, subject(op)); yes {
		t.Fatal("definite unless did not exclude")
	}
	if got := Evaluate(p, subject(op)); got.Decision != protocol.DecisionAllow {
		t.Fatalf("definite unless: %+v", got)
	}
}

func FuzzMatch(f *testing.F) {
	f.Add("refs/heads/**", "refs/heads/main", uint8(0))
	f.Add("**", ":(glob)src/**", uint8(2))
	f.Add("[", "", uint8(255))
	f.Add("https://*.test/**", "git@example.test:repo.git", uint8(3))
	f.Fuzz(func(t *testing.T, pattern, value string, kind uint8) {
		if len(pattern)+len(value) > 1024 {
			t.Skip()
		}
		kinds := []model.SetKind{model.SetExact, model.SetSet, model.SetPattern, model.SetUnknown, model.SetNone, ""}
		op := operation(value, value, model.EffectUnknown)
		op.Scopes = []model.Scope{model.Scope(value)}
		op.Capabilities = []model.Capability{model.Capability(value)}
		op.Target.Remote, op.Target.RemoteURL, op.Target.Worktree, op.Target.CommonDir = value, value, value, value
		op.Target.Refs = &model.TargetSet{Kind: kinds[int(kind)%len(kinds)], Values: []string{value}, Complete: kind%2 == 0}
		op.Target.Paths = op.Target.Refs
		op.Parameters = map[string]any{"value": value}
		for _, m := range []config.Match{
			{Operations: config.StringList{pattern}}, {Scopes: config.StringList{pattern}},
			{Capabilities: config.StringList{pattern}}, {RepoRoots: config.StringList{pattern}},
			{Worktrees: config.StringList{pattern}}, {CommonDirs: config.StringList{pattern}},
			{Remotes: config.StringList{pattern}}, {RemoteURLs: config.StringList{pattern}},
			{Refs: config.StringList{pattern}}, {Paths: config.StringList{pattern}},
			{Env: map[string]string{"VALUE": pattern}}, {Parameters: map[string]string{"value": pattern}},
		} {
			s := subject(op)
			s.Env = map[string]string{"VALUE": value}
			yes, _ := Matches(m, nil, s)
			maybe, _ := MatchMaybe(m, nil, s)
			if yes && maybe {
				t.Fatal("one operation matched both definitely and maybe")
			}
			Matches(m, []config.Match{m}, s)
			MatchMaybe(m, []config.Match{m}, s)
			Evaluate(config.Policy{Rules: []config.Rule{{Match: m, Unless: []config.Match{m}, Decision: "deny"}}}, s)
		}
	})
}
