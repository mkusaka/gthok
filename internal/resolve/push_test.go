package resolve

import (
	"reflect"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

func TestPushDestinations(t *testing.T) {
	f := newFixture(t)
	f.commit()
	f.config("remote.origin.url", "https://example.test/origin")
	f.config("remote.backup.url", "ssh://example.test/backup")
	f.config("branch.main.remote", "origin")
	f.run("-C", f.repo, "tag", "release")
	f.run("-C", f.repo, "symbolic-ref", "refs/heads/alias", "refs/heads/main")
	for _, tc := range []struct {
		name             string
		config           []string
		remote           string
		params           map[string]any
		refs             *model.TargetSet
		want             []string
		unresolvedSource string
	}{
		{name: "implicit current", config: []string{"push.default=current"}, want: []string{"refs/heads/main"}},
		{name: "simple with upstream", config: []string{"branch.main.merge=refs/heads/main"}, want: []string{"refs/heads/main"}},
		{name: "simple missing upstream", unresolvedSource: "git"},
		{name: "simple different upstream", config: []string{"branch.main.merge=refs/heads/other"}, unresolvedSource: "git"},
		{name: "upstream", config: []string{"push.default=upstream", "branch.main.merge=refs/heads/other"}, want: []string{"refs/heads/other"}},
		{name: "upstream wrong remote", config: []string{"push.default=upstream", "branch.main.merge=refs/heads/main"}, remote: "backup", unresolvedSource: "git"},
		{name: "triangular simple", config: []string{"branch.main.pushRemote=backup"}, want: []string{"refs/heads/main"}},
		{name: "push default remote", config: []string{"remote.pushDefault=backup"}, want: []string{"refs/heads/main"}},
		{name: "auto setup", config: []string{"push.autoSetupRemote=true"}, want: []string{"refs/heads/main"}},
		{name: "matching default", config: []string{"push.default=matching"}, unresolvedSource: "remote"},
		{name: "nothing default", config: []string{"push.default=nothing"}, unresolvedSource: "git"},
		{name: "configured refspec", config: []string{"remote.origin.push=refs/heads/main:refs/heads/configured"}, want: []string{"refs/heads/configured"}},
		{name: "implicit source", params: map[string]any{"refspecs": []string{"main"}}, want: []string{"refs/heads/main"}},
		{name: "HEAD source", params: map[string]any{"refspecs": []any{"HEAD"}}, want: []string{"refs/heads/main"}},
		{name: "at source", params: map[string]any{"refspecs": []string{"@"}}, want: []string{"refs/heads/main"}},
		{name: "symbolic source", params: map[string]any{"refspecs": []string{"alias"}}, want: []string{"refs/heads/main"}},
		{name: "tag source", params: map[string]any{"refspec": "release"}, want: []string{"refs/tags/release"}},
		{name: "force source", params: map[string]any{"refspecs": []string{"+main"}}, want: []string{"refs/heads/main"}},
		{name: "explicit destination", params: map[string]any{"refspecs": []string{"HEAD:refs/heads/new"}}, want: []string{"refs/heads/new"}},
		{name: "explicit target", refs: model.Exact("refs/heads/explicit"), want: []string{"refs/heads/explicit"}},
		{name: "delete full", params: map[string]any{"refspecs": []string{":refs/heads/main"}}, want: []string{"refs/heads/main"}},
		{name: "delete short", params: map[string]any{"refspecs": []string{":main"}}, unresolvedSource: "remote"},
		{name: "short destination", params: map[string]any{"refspecs": []string{"main:other"}}, unresolvedSource: "remote"},
		{name: "short target without source", refs: model.Exact("main"), unresolvedSource: "remote"},
		{name: "matching refspec", params: map[string]any{"refspecs": []string{":"}}, unresolvedSource: "remote"},
		{name: "wildcard refspec", params: map[string]any{"refspecs": []string{"refs/heads/*:refs/heads/review/*"}}, want: []string{"refs/heads/review/alias", "refs/heads/review/main"}},
		{name: "wildcard source only", params: map[string]any{"refspecs": []string{"refs/heads/*"}}, want: []string{"refs/heads/alias", "refs/heads/main"}},
		{name: "first wildcard wins", params: map[string]any{"refspecs": []string{"refs/heads/*:refs/heads/first/*", "refs/heads/*:refs/heads/second/*"}}, want: []string{"refs/heads/first/alias", "refs/heads/first/main"}},
		{name: "wildcard suffix", params: map[string]any{"refspecs": []string{"refs/heads/*ain:refs/heads/review/*-copy"}}, want: []string{"refs/heads/review/m-copy"}},
		{name: "short wildcard destination", params: map[string]any{"refspecs": []string{"refs/heads/*:review/*"}}, unresolvedSource: "remote"},
		{name: "missing source", params: map[string]any{"refspecs": []string{"missing"}}, unresolvedSource: "git"},
		{name: "all branches", params: map[string]any{"all": true}, want: []string{"refs/heads/alias", "refs/heads/main"}},
		{name: "all tags", params: map[string]any{"tags": true}, want: []string{"refs/tags/release"}},
		{name: "all branches and tags", params: map[string]any{"all": true, "tags": true}, want: []string{"refs/heads/alias", "refs/heads/main", "refs/tags/release"}},
		{name: "mirror flag", params: map[string]any{"mirror": true}, unresolvedSource: "remote"},
		{name: "mirror config", config: []string{"remote.origin.mirror=true"}, unresolvedSource: "remote"},
		{name: "mirror boolean yes", config: []string{"remote.origin.mirror=yes"}, unresolvedSource: "remote"},
		{name: "mirror negated", config: []string{"remote.origin.mirror=true"}, params: map[string]any{"mirror": false, "refspec": "main"}, want: []string{"refs/heads/main"}},
		{name: "prune flag", params: map[string]any{"prune": true}, unresolvedSource: "remote"},
		{name: "follow tags", params: map[string]any{"followTags": true}, unresolvedSource: "remote"},
		{name: "follow tags config", config: []string{"push.followTags=true"}, unresolvedSource: "remote"},
		{name: "follow tags valueless", config: []string{"push.followTags"}, unresolvedSource: "remote"},
		{name: "follow tags negated", config: []string{"push.followTags=true"}, params: map[string]any{"followTags": false, "refspec": "main"}, want: []string{"refs/heads/main"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := f.resolver()
			for _, config := range tc.config {
				r.Global = append(r.Global, "-c", config)
			}
			op := pushOperation()
			op.Target.Remote = tc.remote
			if tc.params != nil {
				op.Parameters = tc.params
			}
			if tc.refs != nil {
				op.Target.Refs = tc.refs
			}
			res := fill(t, r, op)
			got := res.Operations[0]
			if tc.unresolvedSource != "" {
				if got.Target.Refs.Kind != model.SetUnknown || got.Target.Refs.Complete {
					t.Fatalf("expected unresolved refs: %+v", got)
				}
				found := false
				for _, u := range got.Unresolved {
					found = found || (u.Field == "target.refs" && u.Source == tc.unresolvedSource)
				}
				if !found {
					t.Fatalf("missing %s provenance: %+v", tc.unresolvedSource, got.Unresolved)
				}
			} else if !reflect.DeepEqual(got.Target.Refs.Values, tc.want) || !got.Target.Refs.Complete || len(got.Unresolved) != 0 {
				t.Fatalf("refs: %+v, unresolved: %+v; want %q", got.Target.Refs, got.Unresolved, tc.want)
			}
		})
	}
}

func TestPushDetachedAndRemoteDefaults(t *testing.T) {
	f := newFixture(t)
	f.commit()
	f.config("remote.only.url", "https://example.test/only")
	op := pushOperation()
	op.Target.Remote = ""
	op.Parameters["refspecs"] = []string{"main"}
	if got := fill(t, f.resolver(), op).Operations[0]; got.Target.Remote != "only" || len(got.Unresolved) != 0 {
		t.Fatalf("single remote default: %+v", got)
	}
	f.run("-C", f.repo, "checkout", "-q", "--detach")
	op.Parameters = map[string]any{}
	got := fill(t, f.resolver(), op).Operations[0]
	if got.Target.Refs.Kind != model.SetUnknown || len(got.Unresolved) == 0 {
		t.Fatalf("detached default push guessed: %+v", got)
	}
	op.Parameters["refspecs"] = []string{"HEAD:refs/heads/new"}
	got = fill(t, f.resolver(), op).Operations[0]
	if got.Target.Refs.Values[0] != "refs/heads/new" || len(got.Unresolved) != 0 {
		t.Fatalf("detached explicit destination: %+v", got)
	}
}
