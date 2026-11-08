package git

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize/actions"
)

func TestRemotePushModes(t *testing.T) {
	for _, tt := range []struct {
		args    []string
		actions []string
		refs    *model.TargetSet
		params  map[string]any
	}{
		{[]string{"push", "origin", "HEAD:refs/heads/main"}, []string{"remote.push.update"}, model.Exact("refs/heads/main"), map[string]any{"forceMode": "none", "leaseMode": "none", "refspecs": []string{"HEAD:refs/heads/main"}}},
		{[]string{"push", "origin", "+HEAD:refs/heads/main"}, []string{"remote.push.update"}, nil, map[string]any{"forceMode": "unconditional", "refspecs": []string{"+HEAD:refs/heads/main"}}},
		{[]string{"push", "origin", "main"}, []string{"remote.push.update"}, model.UnknownSet(), map[string]any{"refspecs": []string{"main"}}},
		{[]string{"push", "-df", "origin", "refs/heads/old"}, []string{"remote.push.delete"}, model.Exact("refs/heads/old"), map[string]any{"forceMode": "unconditional", "refspecs": []string{":refs/heads/old"}}},
		{[]string{"push", "origin", ":refs/tags/old", "HEAD:refs/heads/new"}, []string{"remote.push.delete", "remote.push.update"}, model.Exact("refs/tags/old"), nil},
		{[]string{"push", "origin", ":"}, []string{"remote.push.matching"}, model.UnknownSet(), map[string]any{"matching": true}},
		{[]string{"push", "origin", "+:"}, []string{"remote.push.matching"}, nil, map[string]any{"forceMode": "unconditional"}},
		{[]string{"push", "--mirror", "origin"}, []string{"remote.push.mirror"}, model.UnknownSet(), map[string]any{"mirror": true, "forceMode": "unconditional"}},
		{[]string{"push", "--mirror", "--no-mirror", "origin", "HEAD:refs/heads/main"}, []string{"remote.push.update"}, nil, map[string]any{"mirror": false, "forceMode": "none"}},
		{[]string{"push", "--force", "--no-force", "origin", "HEAD:refs/heads/main"}, []string{"remote.push.update"}, nil, map[string]any{"forceMode": "none"}},
		{[]string{"push", "--branches", "origin"}, []string{"remote.push.update"}, model.UnknownSet(), map[string]any{"all": true}},
		{[]string{"push", "--all", "--no-all", "origin", "HEAD:refs/heads/main"}, []string{"remote.push.update"}, nil, map[string]any{"all": false}},
		{[]string{"push", "--tags", "origin"}, []string{"remote.push.tags"}, nil, map[string]any{"tags": true}},
		{[]string{"push", "--tags", "origin", "HEAD:refs/heads/main"}, []string{"remote.push.update", "remote.push.tags"}, nil, nil},
		{[]string{"push", "origin", "tag", "v1"}, []string{"remote.push.update"}, model.Exact("refs/tags/v1"), map[string]any{"refspecs": []string{"refs/tags/v1:refs/tags/v1"}}},
		{[]string{"push", "--force-with-lease", "origin", "HEAD:refs/heads/main"}, []string{"remote.push.update"}, nil, map[string]any{"forceMode": "lease", "leaseMode": "tracking", "leases": []string{""}}},
		{[]string{"push", "--force-with-lease=refs/heads/main", "--force-if-includes", "origin", "HEAD:refs/heads/main"}, []string{"remote.push.update"}, nil, map[string]any{"leaseMode": "tracking-includes"}},
		{[]string{"push", "--force-with-lease=refs/heads/main:expected", "--force-if-includes", "origin", "HEAD:refs/heads/main"}, []string{"remote.push.update"}, nil, map[string]any{"leaseMode": "explicit", "leases": []string{"refs/heads/main:expected"}}},
		{[]string{"push", "--force-with-lease=refs/heads/main:", "origin", "HEAD:refs/heads/main"}, []string{"remote.push.update"}, nil, map[string]any{"leaseMode": "explicit"}},
		{[]string{"push", "--force-with-lease", "--no-force-with-lease", "--force-if-includes", "origin", "HEAD:refs/heads/main"}, []string{"remote.push.update"}, nil, map[string]any{"leaseMode": "none", "forceMode": "none"}},
		{[]string{"push", "--atomic", "--no-follow-tags", "-o", "ci.skip", "--push-option=deploy", "origin", "HEAD:refs/heads/main"}, []string{"remote.push.update"}, nil, map[string]any{"atomic": true, "followTags": false, "pushOptions": []string{"ci.skip", "deploy"}}},
		{[]string{"push", "origin", "refs/heads/*:refs/heads/*", "^refs/heads/private/*"}, []string{"remote.push.update"}, &model.TargetSet{Kind: model.SetPattern, Values: []string{"refs/heads/*"}}, map[string]any{"refspecs": []string{"refs/heads/*:refs/heads/*", "^refs/heads/private/*"}}},
		{[]string{"push", "--rep=backup", "--", "origin", "HEAD:refs/heads/main"}, []string{"remote.push.update"}, nil, nil},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			r := Normalize(model.Invocation{Argv: tt.args})
			got := []string{}
			for _, op := range r.Operations {
				got = append(got, op.Action)
				if op.Family != "remote.push" || op.Effect != model.EffectWrite || op.Coverage != model.CoverageSemantic || op.Origin.Source != "builtin/push.c:cmd_push" || !slices.Contains(op.Capabilities, model.CapabilityNetwork) {
					t.Fatalf("operation: %+v", op)
				}
			}
			if !reflect.DeepEqual(got, tt.actions) || r.Classification.RetrySafe {
				t.Fatalf("result: %+v", r)
			}
			op := r.Operations[0]
			if tt.refs != nil && !reflect.DeepEqual(op.Target.Refs, tt.refs) {
				t.Errorf("refs %+v, want %+v", op.Target.Refs, tt.refs)
			}
			for key, want := range tt.params {
				if !reflect.DeepEqual(op.Parameters[key], want) {
					t.Errorf("parameter %s = %#v, want %#v", key, op.Parameters[key], want)
				}
			}
			if !slices.ContainsFunc(op.Unresolved, func(u model.Unresolved) bool { return u.Field == "target.remote" && u.Source == "git" }) {
				t.Fatal("remote group expansion must remain unresolved until local resolution")
			}
		})
	}
}

func TestRemotePushRemoteAndLocators(t *testing.T) {
	for _, tt := range []struct {
		args   []string
		remote string
		start  int
	}{
		{[]string{"push", "--repo=backup", "origin", "HEAD:refs/heads/main"}, "origin", 2},
		{[]string{"-C", "somewhere", "push", "--repo=origin"}, "origin", 3},
		{[]string{"push", "origin"}, "origin", 1},
	} {
		r := Normalize(model.Invocation{Argv: tt.args})
		op := r.Operations[0]
		loc, ok := op.Editable["target/remote"]
		if op.Target.Remote != tt.remote || !ok || loc.Spans[0].Start != tt.start {
			t.Errorf("%q: %+v, editable %+v", tt.args, op, op.Editable)
		}
	}
	r := Normalize(model.Invocation{Argv: []string{"publish"}, Aliases: map[string]string{"publish": "push origin HEAD:refs/heads/main"}})
	if len(r.Operations[0].Editable) != 0 {
		t.Fatal("alias-expanded spans exposed against execution argv")
	}
}

func TestRemoteModes(t *testing.T) {
	for _, tt := range []struct {
		args    []string
		actions []string
		effect  model.Effect
		network bool
		params  map[string]any
	}{
		{[]string{"fetch", "origin", "+refs/heads/main:refs/remotes/origin/main"}, []string{"remote.fetch"}, model.EffectWrite, true, map[string]any{"refspecs": []string{"+refs/heads/main:refs/remotes/origin/main"}, "writeFetchHead": true}},
		{[]string{"fetch", "--all", "--prune", "--prune-tags", "--depth=2"}, []string{"remote.fetch"}, model.EffectWrite, true, map[string]any{"all": true, "prune": true, "pruneTags": true, "depth": "2"}},
		{[]string{"fetch", "--multiple", "origin", "backup"}, []string{"remote.fetch", "remote.fetch"}, model.EffectWrite, true, nil},
		{[]string{"fetch", "--dry-run", "origin"}, []string{"remote.fetch"}, model.EffectRead, true, map[string]any{"dryRun": true, "writeFetchHead": false}},
		{[]string{"fetch", "--no-write-fetch-head", "--unshallow", "origin"}, []string{"remote.fetch"}, model.EffectWrite, true, map[string]any{"unshallow": true, "writeFetchHead": false}},
		{[]string{"fetch", "--prune", "--no-prune", "origin"}, []string{"remote.fetch"}, model.EffectWrite, true, map[string]any{"prune": false}},
		{[]string{"fetch", "-n", "--tags", "origin"}, []string{"remote.fetch"}, model.EffectWrite, true, map[string]any{"tags": true}},
		{[]string{"fetch", "--tags", "-n", "origin"}, []string{"remote.fetch"}, model.EffectWrite, true, map[string]any{"tags": false}},
		{[]string{"pull", "--no-rebase", "origin", "main"}, []string{"remote.fetch", "remote.pull"}, model.EffectWrite, true, nil},
		{[]string{"pull", "--rebase=merges", "origin", "main"}, []string{"remote.fetch", "remote.pull"}, model.EffectWrite, true, nil},
		{[]string{"pull", "--dry-run", "origin"}, []string{"remote.fetch", "remote.pull"}, model.EffectRead, true, nil},
		{[]string{"remote"}, []string{"remote.list"}, model.EffectRead, false, nil},
		{[]string{"remote", "add", "origin", "../upstream"}, []string{"remote.config.add"}, model.EffectWrite, false, map[string]any{"url": "../upstream"}},
		{[]string{"remote", "add", "-f", "origin", "../upstream"}, []string{"remote.config.add", "remote.fetch"}, model.EffectWrite, false, nil},
		{[]string{"remote", "rename", "old", "new"}, []string{"remote.config.rename"}, model.EffectWrite, false, map[string]any{"newName": "new"}},
		{[]string{"remote", "remove", "origin"}, []string{"remote.config.remove"}, model.EffectWrite, false, nil},
		{[]string{"remote", "rm", "origin"}, []string{"remote.config.remove"}, model.EffectWrite, false, nil},
		{[]string{"remote", "set-url", "--push", "--add", "origin", "../backup"}, []string{"remote.config.set-url"}, model.EffectWrite, false, map[string]any{"push": true, "add": true, "url": "../backup"}},
		{[]string{"remote", "set-head", "origin", "main"}, []string{"ref.update"}, model.EffectWrite, false, map[string]any{"newTarget": "refs/remotes/origin/main", "refKind": "other"}},
		{[]string{"remote", "set-head", "-d", "origin"}, []string{"ref.delete"}, model.EffectWrite, false, map[string]any{"refKind": "other"}},
		{[]string{"remote", "set-head", "-a", "origin"}, []string{"ref.update"}, model.EffectWrite, true, map[string]any{"refKind": "other"}},
		{[]string{"remote", "set-branches", "--add", "origin", "main", "topic"}, []string{"remote.config.set-branches"}, model.EffectWrite, false, map[string]any{"branches": []string{"main", "topic"}, "add": true}},
		{[]string{"remote", "prune", "origin"}, []string{"remote.prune"}, model.EffectWrite, true, nil},
		{[]string{"remote", "prune", "-n", "origin", "backup"}, []string{"remote.prune", "remote.prune"}, model.EffectRead, true, map[string]any{"dryRun": true}},
		{[]string{"remote", "update", "--prune", "group"}, []string{"remote.fetch"}, model.EffectWrite, true, map[string]any{"prune": true}},
		{[]string{"remote", "update", "origin", "backup"}, []string{"remote.fetch", "remote.fetch"}, model.EffectWrite, true, map[string]any{"remoteUpdate": true}},
		{[]string{"remote", "update"}, []string{"remote.fetch"}, model.EffectWrite, true, map[string]any{"remoteUpdate": true}},
		{[]string{"remote", "show", "-n", "origin"}, []string{"remote.show"}, model.EffectRead, false, nil},
		{[]string{"remote", "show", "origin"}, []string{"remote.show"}, model.EffectRead, true, nil},
		{[]string{"remote", "get-url", "--push", "--all", "origin"}, []string{"remote.get-url"}, model.EffectRead, false, map[string]any{"push": true, "all": true}},
		{[]string{"ls-remote", "origin", "main"}, []string{"remote.ls-remote"}, model.EffectRead, true, nil},
		{[]string{"ls-remote", "--get-url", "origin"}, []string{"remote.get-url"}, model.EffectRead, false, nil},
		{[]string{"clone", "../upstream", "new-repo"}, []string{"remote.clone"}, model.EffectWrite, true, map[string]any{"checkout": true, "bare": false}},
		{[]string{"clone", "--mirror", "../upstream", "new.git"}, []string{"remote.clone"}, model.EffectWrite, true, map[string]any{"checkout": false, "bare": true, "mirror": true}},
		{[]string{"clone", "--bare", "--no-naked", "../upstream", "new"}, []string{"remote.clone"}, model.EffectWrite, true, map[string]any{"bare": false, "checkout": true}},
		{[]string{"clone", "-n", "../upstream", "new"}, []string{"remote.clone"}, model.EffectWrite, true, map[string]any{"checkout": false}},
		{[]string{"send-pack", "origin", ":refs/heads/old"}, []string{"remote.push.delete"}, model.EffectWrite, true, nil},
		{[]string{"send-pack", "origin"}, []string{"remote.push.matching"}, model.EffectWrite, true, map[string]any{"matching": true}},
		{[]string{"upload-pack", "../upstream"}, []string{"remote.pack.upload"}, model.EffectRead, true, nil},
		{[]string{"receive-pack", "--advertise-refs", "../upstream"}, []string{"remote.pack.advertise"}, model.EffectRead, true, nil},
		{[]string{"fetch-pack", "../upstream", "refs/heads/main"}, []string{"remote.pack.fetch"}, model.EffectWrite, true, nil},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			r := Normalize(model.Invocation{Argv: tt.args})
			got := []string{}
			for _, op := range r.Operations {
				got = append(got, op.Action)
				if op.Coverage != model.CoverageSemantic || op.Origin.Source == "" {
					t.Fatalf("operation: %+v", op)
				}
				if spec, ok := actions.Lookup(op.Action); !ok || spec.Family != op.Family {
					t.Fatalf("uncatalogued action: %+v", op)
				}
				if op.Action == "remote.fetch" && slices.Contains(op.Scopes, model.ScopeRemoteRef) {
					t.Fatal("fetch mutates local, not remote refs")
				}
			}
			op := r.Operations[0]
			if !reflect.DeepEqual(got, tt.actions) || op.Effect != tt.effect || slices.Contains(op.Capabilities, model.CapabilityNetwork) != tt.network {
				t.Fatalf("result %+v", r)
			}
			for key, want := range tt.params {
				if !reflect.DeepEqual(op.Parameters[key], want) {
					t.Errorf("parameter %s = %#v, want %#v", key, op.Parameters[key], want)
				}
			}
		})
	}
}

func TestRemoteSafetyBoundaries(t *testing.T) {
	for _, args := range [][]string{{"push", "--dry-run", "origin", "HEAD:refs/heads/main"}, {"push", "-nu", "origin", "HEAD:refs/heads/main"}} {
		r := Normalize(model.Invocation{Argv: args})
		op := r.Operations[0]
		if op.Effect != model.EffectRead || op.Parameters["dryRun"] != true || slices.Contains(op.Scopes, model.ScopeConfig) {
			t.Fatalf("dry-run: %+v", r)
		}
		// Network retry safety is aggregated by shared git.go; see the T7b integration request.
	}
	r := Normalize(model.Invocation{Argv: []string{"push", "-u", "origin", "HEAD:refs/heads/main"}})
	if !slices.Contains(r.Operations[0].Scopes, model.ScopeConfig) {
		t.Fatal("push upstream config write missing")
	}
	for _, args := range [][]string{{"push", "--receive-pack=custom", "origin", "HEAD:refs/heads/main"}, {"fetch", "--upload-pack=custom", "origin"}} {
		if r := Normalize(model.Invocation{Argv: args}); !slices.Contains(r.Operations[0].Capabilities, model.CapabilityExternalExec) {
			t.Fatal(r)
		}
	}
	r = Normalize(model.Invocation{Argv: []string{"push", "--receive-pack=first", "--exec=last", "origin", "HEAD:refs/heads/main"}})
	if r.Operations[0].Parameters["exec"] != "last" {
		t.Fatal(r)
	}
	for _, args := range [][]string{{"push", "--prune", "origin", "HEAD:refs/heads/main"}, {"push", "--follow-tags", "origin", "HEAD:refs/heads/main"}} {
		r := Normalize(model.Invocation{Argv: args})
		if r.Operations[0].Target.Refs.Complete || r.Classification.Resolution == model.ResolutionComplete || len(r.Operations) != 2 {
			t.Fatal(r)
		}
		want := "remote.push.delete"
		if args[1] == "--follow-tags" {
			want = "remote.push.tags"
		}
		if r.Operations[1].Action != want {
			t.Fatal(r)
		}
	}
	r = Normalize(model.Invocation{Argv: []string{"pull", "--rebase=interactive", "origin"}})
	if len(r.Operations) != 2 || r.Operations[1].UnknownReason != model.UnknownInteractive || r.Classification.RetrySafe {
		t.Fatal(r)
	}
	r = Normalize(model.Invocation{Argv: []string{"receive-pack", "repo.git"}})
	if r.Operations[0].UnknownReason != model.UnknownInputUnavailable || r.Classification.RetrySafe || r.Classification.Coverage != model.CoverageOpaque {
		t.Fatal(r)
	}
	r = Normalize(model.Invocation{Argv: []string{"clone", "--bare", "repo", "new.git"}})
	if !reflect.DeepEqual(r.Operations[0].Target.Paths, model.Exact("new.git")) || slices.Contains(r.Operations[0].Scopes, model.ScopeWorktree) {
		t.Fatal(r)
	}
	r = Normalize(model.Invocation{Argv: []string{"fetch", "--multiple", "origin", "backup"}})
	if r.Operations[0].Target.Remote != "origin" || r.Operations[1].Target.Remote != "backup" {
		t.Fatal(r)
	}
}

func TestFetchCapturedRefspecs(t *testing.T) {
	argv := []string{"fetch", "--stdin", "origin"}
	r := Normalize(model.Invocation{Argv: argv})
	if r.Operations[0].UnknownReason != model.UnknownInputUnavailable || r.Classification.RetrySafe {
		t.Fatal(r)
	}
	r = Normalize(model.Invocation{Argv: argv, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte("+refs/heads/main:refs/remotes/origin/main\n")}})
	if !reflect.DeepEqual(r.Operations[0].Target.Refs.Values, []string{"refs/remotes/origin/main", "FETCH_HEAD"}) || slices.Contains(r.Operations[0].Scopes, model.ScopeRemoteRef) {
		t.Fatal(r)
	}
}

func TestRemoteLocalRepositories(t *testing.T) {
	git := rrRealGit(t)
	root, local, remote := t.TempDir(), t.TempDir(), t.TempDir()
	git(remote, "", "init", "-q", "--bare")
	git(local, "", "init", "-q", "-b", "main")
	git(local, "", "commit", "-q", "--allow-empty", "-m", "initial")
	git(local, "", "remote", "add", "origin", remote)
	for _, args := range [][]string{{"push", "--dry-run", "origin", "HEAD:refs/heads/main"}, {"push", "origin", "HEAD:refs/heads/main"}, {"fetch", "--prune", "origin"}, {"remote", "prune", "-n", "origin"}, {"ls-remote", "origin", "refs/heads/main"}} {
		git(local, "", args...)
		r := Normalize(model.Invocation{Argv: args})
		if r.Operations[0].Coverage != model.CoverageSemantic {
			t.Fatalf("%q: %+v", args, r)
		}
	}
	git(root, "", "clone", "-q", "--bare", remote, "copy.git")
}
