package config

import (
	"strings"
	"testing"
)

func TestValidateGitMatch(t *testing.T) {
	for _, tc := range []struct {
		name, field, problem string
		m                    Match
	}{
		{"scope", "scopes", "must be one of", Match{Scopes: StringList{"remote"}}},
		{"capability", "capabilities", "must be one of", Match{Capabilities: StringList{"shell"}}},
		{"repo-root-glob", "repoRoots", "invalid glob", Match{RepoRoots: StringList{"/repos/["}}},
		{"worktree-glob", "worktrees", "invalid glob", Match{Worktrees: StringList{"/repos/["}}},
		{"common-dir-glob", "commonDirs", "invalid glob", Match{CommonDirs: StringList{"/repos/["}}},
		{"remote-url-glob", "remoteURLs", "invalid glob", Match{RemoteURLs: StringList{"https://example.test/["}}},
		{"ref-glob", "refs", "invalid glob", Match{Refs: StringList{"refs/heads/["}}},
		{"short-ref", "refs", "full ref-name glob", Match{Refs: StringList{"main"}}},
		{"short-ref-glob", "refs", "short names and refspecs", Match{Refs: StringList{"release/*"}}},
		{"empty-ref-prefix", "refs", "full ref-name glob", Match{Refs: StringList{"refs/"}}},
		{"refspec", "refs", "refspecs", Match{Refs: StringList{"refs/heads/main:refs/heads/other"}}},
		{"forced-refspec", "refs", "refspecs", Match{Refs: StringList{"+refs/heads/main"}}},
		{"path-glob", "paths", "invalid glob", Match{Paths: StringList{"src/["}}},
		{"absolute-path", "paths", "repository-relative", Match{Paths: StringList{"/src/**"}}},
		{"parent-path", "paths", "parent traversal", Match{Paths: StringList{"../src/**"}}},
		{"parent-only-path", "paths", "parent traversal", Match{Paths: StringList{".."}}},
		{"pathspec", "paths", "pathspec magic", Match{Paths: StringList{":(glob)src/**"}}},
		{"empty-root", "repoRoots", "non-empty", Match{RepoRoots: StringList{""}}},
		{"nul-root", "repoRoots", "NUL-free", Match{RepoRoots: StringList{"repo\x00"}}},
		{"empty-remote", "remotes", "non-empty", Match{Remotes: StringList{""}}},
		{"nul-remote", "remotes", "NUL-free", Match{Remotes: StringList{"origin\x00"}}},
	} {
		for _, location := range []string{"rule", "unless", "pre-hook", "post-hook", "transform", "delegate"} {
			t.Run(tc.name+"/"+location, func(t *testing.T) {
				c := validConfig()
				key := "policy.rules[0].match."
				switch location {
				case "rule":
					c.Policy.Rules = []Rule{{ID: "check", Decision: "deny", Match: tc.m}}
				case "unless":
					c.Policy.Rules = []Rule{{ID: "check", Decision: "deny", Unless: []Match{tc.m}}}
					key = "policy.rules[0].unless[0]."
				case "pre-hook":
					c.Hooks[0].Match = tc.m
					key = "hooks[0].match."
				case "post-hook":
					c.Hooks[1].Match = tc.m
					key = "hooks[1].match."
				case "transform":
					c.Transforms = TransformList{{ID: "transform", Match: tc.m, Command: []string{"check"}}}
					key = "transforms.pre[0].match."
				case "delegate":
					c.Delegates = []Delegate{{ID: "delegate", Match: tc.m, Command: []string{"check"}}}
					c.Delegates[0].Match.Effects = StringList{"read"}
					c.Delegates[0].Match.Levels = StringList{"L1"}
					key = "delegates[0].match."
				}
				c.ApplyDefaults()
				err := c.Validate()
				if err == nil || !strings.Contains(err.Error(), key+tc.field+"[0]") || !strings.Contains(err.Error(), tc.problem) {
					t.Fatalf("wanted %s at %s%s[0], got %v", tc.problem, key, tc.field, err)
				}
			})
		}
	}
}

func TestParseGitMatchers(t *testing.T) {
	body := `version: 1
policy:
  rules:
    - id: protect
      decision: deny
      match:
        scopes: [local-ref, remote-ref, index, worktree, config, objects, stash, reflog, remote-config, submodule, worktree-admin, hooks, file-output]
        capabilities: [external-exec, editor, network, file-write, index-refresh, interactive, pager]
        repoRoots: /repos/**
        worktrees: [/worktrees/*]
        commonDirs: /repos/**/.git
        remotes: [origin, backup]
        remoteURLs: ['https://*.example.test/team/**', 'git@example.test:team/*']
        refs: ['refs/heads/main', 'refs/heads/release/*', '**']
        paths: ['src/**/*.go', './docs/*']
      unless:
        - remotes: scratch
`
	c, err := Parse([]byte(body), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := c.Policy.Rules[0].Match
	if len(m.Scopes) != 13 || len(m.Capabilities) != 7 || len(m.RepoRoots) != 1 || len(m.Worktrees) != 1 || len(m.CommonDirs) != 1 || len(m.Remotes) != 2 || len(m.RemoteURLs) != 2 || len(m.Refs) != 3 || len(m.Paths) != 2 {
		t.Fatalf("Git criteria were not decoded: %+v", m)
	}
}
