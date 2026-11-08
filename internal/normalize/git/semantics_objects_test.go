package git

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

func TestT7cObjectAndIndexModes(t *testing.T) {
	for _, tt := range []struct {
		args   string
		action string
		effect model.Effect
		scopes []model.Scope
		safe   bool
	}{
		{"hash-object --no-filters file", "objects.hash.read", model.EffectRead, []model.Scope{model.ScopeObjects}, true},
		{"hash-object -w --stdin", "objects.hash.write", model.EffectWrite, []model.Scope{model.ScopeObjects}, false},
		{"hash-object file", "objects.hash.read", model.EffectRead, []model.Scope{model.ScopeObjects}, false},
		{"read-tree HEAD", "index.read-tree", model.EffectWrite, []model.Scope{model.ScopeIndex}, false},
		{"read-tree -mu HEAD", "index.read-tree", model.EffectWrite, []model.Scope{model.ScopeIndex, model.ScopeWorktree}, false},
		{"read-tree -mu --recurse-submodules HEAD", "index.read-tree", model.EffectWrite, []model.Scope{model.ScopeIndex, model.ScopeWorktree, model.ScopeSubmodule, model.ScopeLocalRef, model.ScopeReflog}, false},
		{"read-tree -mnu HEAD", "index.read-tree.preview", model.EffectRead, []model.Scope{model.ScopeIndex, model.ScopeWorktree}, true},
		{"read-tree --index-output=out HEAD", "index.read-tree", model.EffectWrite, []model.Scope{model.ScopeIndex, model.ScopeFileOutput}, false},
		{"update-index --chmod=+x file", "index.update", model.EffectWrite, []model.Scope{model.ScopeIndex, model.ScopeObjects}, false},
		{"update-index --add file", "index.update", model.EffectWrite, []model.Scope{model.ScopeIndex, model.ScopeObjects}, false},
		{"update-index --remove file", "index.update", model.EffectWrite, []model.Scope{model.ScopeIndex, model.ScopeObjects}, false},
		{"update-index --force-remove file", "index.update", model.EffectWrite, []model.Scope{model.ScopeIndex}, false},
		{"update-index --refresh", "index.update", model.EffectWrite, []model.Scope{model.ScopeIndex}, false},
		{"update-index --info-only --add file", "index.update", model.EffectWrite, []model.Scope{model.ScopeIndex}, false},
		{"write-tree", "objects.write-tree", model.EffectWrite, []model.Scope{model.ScopeObjects, model.ScopeIndex}, false},
		{"commit-tree -mmessage tree", "objects.commit-tree", model.EffectWrite, []model.Scope{model.ScopeObjects}, false},
		{"mktag", "objects.mktag", model.EffectWrite, []model.Scope{model.ScopeObjects}, false},
		{"mktree --batch", "objects.mktree", model.EffectWrite, []model.Scope{model.ScopeObjects}, false},
		{"fast-import", "objects.fast-import", model.EffectUnknown, []model.Scope{model.ScopeObjects, model.ScopeLocalRef, model.ScopeReflog}, false},
		{"fast-export --all", "objects.fast-export", model.EffectRead, []model.Scope{model.ScopeObjects}, true},
		{"fast-export --export-marks=marks HEAD", "objects.fast-export.output", model.EffectWrite, []model.Scope{model.ScopeObjects, model.ScopeFileOutput}, false},
		{"unpack-objects -q", "objects.unpack-objects", model.EffectWrite, []model.Scope{model.ScopeObjects}, false},
		{"unpack-objects -n", "objects.unpack-objects.preview", model.EffectRead, []model.Scope{model.ScopeObjects}, true},
		{"index-pack --stdin --fix-thin", "objects.index-pack", model.EffectWrite, []model.Scope{model.ScopeObjects}, false},
		{"index-pack --verify data.pack", "objects.index-pack.verify", model.EffectRead, []model.Scope{model.ScopeObjects}, true},
		{"index-pack data.pack", "objects.index-pack", model.EffectWrite, []model.Scope{model.ScopeObjects, model.ScopeFileOutput}, false},
		{"pack-objects --stdout", "objects.pack-objects.read", model.EffectRead, []model.Scope{model.ScopeObjects}, true},
		{"pack-objects --stdout --unpack-unreachable", "objects.pack-objects", model.EffectWrite, []model.Scope{model.ScopeObjects}, false},
		{"pack-objects packs", "objects.pack-objects", model.EffectWrite, []model.Scope{model.ScopeObjects, model.ScopeFileOutput}, false},
		{"prune --expire=now", "objects.prune", model.EffectWrite, []model.Scope{model.ScopeObjects}, false},
		{"prune -n", "objects.prune.preview", model.EffectRead, []model.Scope{model.ScopeObjects}, true},
		{"prune-packed", "objects.prune-packed", model.EffectWrite, []model.Scope{model.ScopeObjects}, false},
		{"prune-packed -n", "objects.prune-packed.preview", model.EffectRead, []model.Scope{model.ScopeObjects}, true},
		{"repack -ad", "objects.repack", model.EffectWrite, []model.Scope{model.ScopeObjects}, false},
		{"repack --dry-run", "objects.repack.preview", model.EffectRead, []model.Scope{model.ScopeObjects}, true},
		{"gc --prune=now", "objects.gc", model.EffectWrite, []model.Scope{model.ScopeObjects, model.ScopeLocalRef, model.ScopeReflog, model.ScopeWorktreeAdmin}, false},
		{"count-objects -v", "objects.count-objects", model.EffectRead, []model.Scope{model.ScopeObjects}, true},
		{"fsck", "objects.fsck", model.EffectRead, []model.Scope{model.ScopeObjects}, true},
		{"fsck --lost-found", "objects.fsck.lost-found", model.EffectWrite, []model.Scope{model.ScopeObjects, model.ScopeFileOutput}, false},
		{"fsck-objects", "objects.fsck", model.EffectRead, []model.Scope{model.ScopeObjects}, true},
		{"verify-pack -v pack.idx", "objects.verify-pack", model.EffectRead, []model.Scope{model.ScopeObjects}, true},
		{"commit-graph write --reachable", "objects.commit-graph.write", model.EffectWrite, []model.Scope{model.ScopeObjects}, false},
		{"commit-graph verify", "objects.commit-graph.verify", model.EffectRead, []model.Scope{model.ScopeObjects}, true},
		{"multi-pack-index write", "objects.multi-pack-index.write", model.EffectWrite, []model.Scope{model.ScopeObjects}, false},
		{"multi-pack-index verify", "objects.multi-pack-index.verify", model.EffectRead, []model.Scope{model.ScopeObjects}, true},
	} {
		t.Run(tt.args, func(t *testing.T) {
			r, op := adminTestOperation(t, strings.Fields(tt.args)...)
			if op.Action != tt.action || op.Effect != tt.effect || !reflect.DeepEqual(op.Scopes, tt.scopes) || r.Classification.RetrySafe != tt.safe || r.Classification.MutationFree != tt.safe {
				t.Fatal(r)
			}
			if op.Family == "objects" && tt.args != "fast-import" && !strings.HasPrefix(tt.args, "gc ") && (op.Target.Refs == nil || op.Target.Refs.Kind != model.SetNone) {
				t.Fatal("object-only command should not mutate refs", op.Target)
			}
		})
	}
}

func TestT7cObjectParametersAndInput(t *testing.T) {
	for _, tt := range []struct {
		args  string
		key   string
		value any
		cap   model.Capability
	}{
		{"hash-object -wt tree --stdin", "objectType", "tree", ""},
		{"read-tree -mu HEAD", "merge", true, model.CapabilityExternalExec},
		{"update-index --chmod=-x file", "chmod", "-x", ""},
		{"update-index --refresh", "refresh", true, model.CapabilityIndexRefresh},
		{"repack -ad", "deletePacks", true, ""},
		{"gc --prune=now", "prune", "now", ""},
		{"gc --prune", "prune", "configured", ""},
		{"gc --prune --no-prune", "prune", "none", ""},
	} {
		_, op := adminTestOperation(t, strings.Fields(tt.args)...)
		if !reflect.DeepEqual(op.Parameters[tt.key], tt.value) || tt.cap != "" && !slices.Contains(op.Capabilities, tt.cap) {
			t.Errorf("%q: %+v", tt.args, op)
		}
	}
	for _, tt := range []struct {
		argv []string
		data string
		want []string
	}{
		{[]string{"update-index", "-z", "--stdin"}, "a b\x00c\nd\x00", []string{"a b", "c\nd"}},
		{[]string{"update-index", "--index-info"}, "100644 oid\ta\n100755 oid\t\"b\\tc\"\n", []string{"a", "b\tc"}},
		{[]string{"hash-object", "--no-filters", "--stdin-paths"}, "a\n\"b\\tc\"\n", []string{"a", "b\tc"}},
	} {
		r := Normalize(model.Invocation{Argv: tt.argv, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte(tt.data)}})
		if !reflect.DeepEqual(r.Operations[0].Target.Paths.Values, tt.want) || r.Classification.Coverage != model.CoverageSemantic {
			t.Fatalf("%q: %+v", tt.argv, r)
		}
	}
	_, op := adminTestOperation(t, "update-index", "file", "--chmod=+x", "other")
	if !slices.Contains(op.Scopes, model.ScopeObjects) || !reflect.DeepEqual(op.Target.Paths.Values, []string{"file", "other"}) {
		t.Fatal(op)
	}
	for _, flag := range []string{"--info-only", "--force-remove", "--chmod=+x"} {
		_, op := adminTestOperation(t, "update-index", "first", flag, "second")
		if !slices.Contains(op.Scopes, model.ScopeObjects) {
			t.Fatalf("%s must not hide an earlier content update: %+v", flag, op)
		}
	}
	for _, flag := range []string{"--assume-unchanged", "--no-assume-unchanged", "--skip-worktree", "--no-skip-worktree", "--fsmonitor-valid", "--no-fsmonitor-valid"} {
		_, op := adminTestOperation(t, "update-index", flag, "file")
		if slices.Contains(op.Scopes, model.ScopeObjects) {
			t.Fatalf("metadata-only %s: %+v", flag, op)
		}
	}
	_, op = adminTestOperation(t, "update-index", "--cacheinfo", "100644,0123456789012345678901234567890123456789,file")
	if !reflect.DeepEqual(op.Target.Paths, model.Exact("file")) {
		t.Fatal(op)
	}
	for _, data := range []string{"invalid", "100644 oid\t\"bad\\q\"\n"} {
		r := Normalize(model.Invocation{Argv: []string{"update-index", "--index-info"}, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte(data)}})
		if r.Operations[0].UnknownReason != model.UnknownParseError || r.Classification.RetrySafe {
			t.Fatal(r)
		}
	}
	for _, captured := range []bool{false, true} {
		inv := model.Invocation{Argv: []string{"fast-import", "--export-marks=marks"}}
		if captured {
			inv.Stdin = model.StdinData{Status: model.StdinCaptured, Data: []byte("done\n")}
		}
		r := Normalize(inv)
		if r.Operations[0].UnknownReason != model.UnknownInputUnavailable || r.Operations[0].Effect != model.EffectUnknown || r.Classification.Coverage != model.CoverageOpaque || !slices.Contains(r.Operations[0].Capabilities, model.CapabilityFileWrite) {
			t.Fatal(r)
		}
	}
}

func TestT7cCommitTreeMessage(t *testing.T) {
	r := Normalize(model.Invocation{Argv: []string{"commit-tree", "-mone", "-Fmsg", "-mtwo", "tree"}, ReadFile: func(path string) ([]byte, error) {
		if path != "msg" {
			t.Fatal(path)
		}
		return []byte("from file\n"), nil
	}})
	op := r.Operations[0]
	if op.Texts == nil || op.Texts.Body.Value != "one\n\nfrom file\n\ntwo\n" || op.Effect != model.EffectWrite || !reflect.DeepEqual(op.Scopes, []model.Scope{model.ScopeObjects}) || len(op.Editable) != 0 {
		t.Fatal(op)
	}
	r = Normalize(model.Invocation{Argv: []string{"commit-tree", "tree"}, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte("stdin message\n")}})
	if r.Operations[0].Texts.Body.Source != "stdin" || r.Operations[0].Texts.Body.Value != "stdin message\n" {
		t.Fatal(r)
	}
}

func TestT7cArchiveEditableRemote(t *testing.T) {
	_, op := adminTestOperation(t, "archive", "--remote=old", "--remote=origin", "HEAD")
	loc, ok := op.Editable["/target/remote"]
	if !ok || loc.Kind != "argv" || loc.Flag != "remote" || len(loc.Spans) != 1 || loc.Spans[0].Start != 2 || loc.Spans[0].End != 3 {
		t.Fatal(op.Editable)
	}
	r := Normalize(model.Invocation{Argv: []string{"arc", "--remote=origin", "HEAD"}, Aliases: map[string]string{"arc": "archive"}})
	if len(r.Operations[0].Editable) != 0 {
		t.Fatal("alias-expanded spans must not be applied to execution argv", r)
	}
}

func TestT7cMaintenanceArchiveBundle(t *testing.T) {
	for _, tt := range []struct {
		args   string
		action string
		effect model.Effect
		cap    model.Capability
		safe   bool
	}{
		{"maintenance run --task=gc", "maintenance.run", model.EffectWrite, "", false},
		{"maintenance run --task=prefetch", "maintenance.run", model.EffectWrite, model.CapabilityNetwork, false},
		{"maintenance run", "maintenance.run", model.EffectWrite, model.CapabilityNetwork, false},
		{"maintenance start --scheduler=launchctl", "maintenance.start", model.EffectWrite, model.CapabilityExternalExec, false},
		{"maintenance stop", "maintenance.stop", model.EffectWrite, model.CapabilityExternalExec, false},
		{"maintenance register", "maintenance.register", model.EffectWrite, "", false},
		{"maintenance unregister --force", "maintenance.unregister", model.EffectWrite, "", false},
		{"maintenance is-needed --auto", "maintenance.is-needed", model.EffectRead, "", true},
		{"archive --format=tar HEAD", "archive.create", model.EffectRead, "", true},
		{"archive --format=custom HEAD", "archive.create", model.EffectRead, model.CapabilityExternalExec, false},
		{"archive --list", "archive.list", model.EffectRead, "", true},
		{"archive -oout.tar HEAD -- path", "archive.output", model.EffectWrite, model.CapabilityFileWrite, false},
		{"archive --remote=origin HEAD", "archive.create", model.EffectRead, model.CapabilityNetwork, false},
		{"bundle create out.bundle --all", "bundle.create", model.EffectWrite, model.CapabilityFileWrite, false},
		{"bundle create - HEAD", "bundle.stdout", model.EffectRead, "", true},
		{"bundle unbundle out.bundle", "bundle.unbundle", model.EffectWrite, "", false},
		{"bundle verify out.bundle", "bundle.verify", model.EffectRead, "", true},
		{"bundle list-heads out.bundle", "bundle.list-heads", model.EffectRead, "", true},
	} {
		t.Run(tt.args, func(t *testing.T) {
			r, op := adminTestOperation(t, strings.Fields(tt.args)...)
			if op.Action != tt.action || op.Effect != tt.effect || r.Classification.RetrySafe != tt.safe || tt.cap != "" && !slices.Contains(op.Capabilities, tt.cap) {
				t.Fatal(r)
			}
		})
	}
}
