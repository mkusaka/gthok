package resolve

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

func TestClosedQueryTemplates(t *testing.T) {
	// Every legal kind is enumerated here. Unknown kinds cannot build argv;
	// operands cannot choose a command or introduce an option.
	cases := []struct {
		kind  queryKind
		value string
		want  []string
	}{
		{metadataQuery, "", []string{"rev-parse", "--git-dir", "--git-common-dir", "--is-bare-repository", "--is-inside-work-tree", "--show-prefix"}},
		{rootQuery, "", []string{"rev-parse", "--show-toplevel"}},
		{commitQuery, "HEAD", []string{"rev-parse", "--verify", "--quiet", "--end-of-options", "HEAD^{commit}"}},
		{symbolicQuery, "main", []string{"rev-parse", "--symbolic-full-name", "main"}},
		{headQuery, "", []string{"symbolic-ref", "-q", "HEAD"}},
		{configQuery, "", []string{"config", "--null", "--list"}},
		{configGetQuery, "push.default", []string{"config", "--null", "--get", "push.default"}},
		{configAllQuery, "remote.origin.url", []string{"config", "--null", "--get-all", "remote.origin.url"}},
		{configRegexpQuery, "^remote\\.", []string{"config", "--null", "--get-regexp", "^remote\\."}},
		{refsQuery, "refs/heads/", []string{"for-each-ref", "--format=%(refname)", "refs/heads/"}},
		{showRefsQuery, "", []string{"show-ref"}},
		{remoteQuery, "origin", []string{"remote", "get-url", "--all", "origin"}},
		{pushURLQuery, "origin", []string{"remote", "get-url", "--all", "--push", "origin"}},
		{filesQuery, "a", []string{"ls-files", "-z", "--", "a"}},
		{trackedFileQuery, "a", []string{"ls-files", "-z", "--error-unmatch", "--", "a"}},
		{objectTypeQuery, strings.Repeat("a", 40), []string{"cat-file", "-t", strings.Repeat("a", 40)}},
		{objectSizeQuery, strings.Repeat("a", 64), []string{"cat-file", "-s", strings.Repeat("a", 64)}},
	}
	for _, tc := range cases {
		args, err := (query{tc.kind, tc.value}).args()
		if err != nil || !reflect.DeepEqual(args, tc.want) {
			t.Fatalf("query %d: %q, %v; want %q", tc.kind, args, err, tc.want)
		}
		for _, arg := range args {
			for _, forbidden := range []string{"push", "fetch", "ls-remote", "ssh", "credential"} {
				if arg == forbidden {
					t.Fatalf("network/executable query: %q", args)
				}
			}
		}
	}
	if len(cases) != int(objectSizeQuery)+1 {
		t.Fatal("new query template lacks a safety assertion")
	}
	for kind := 0; kind <= 255; kind++ {
		for _, operand := range []string{"--output=unsafe", "--exec=unsafe", "bad\x00operand", "bad\noperand"} {
			if args, err := (query{queryKind(kind), operand}).args(); err == nil {
				if (queryKind(kind) != filesQuery && queryKind(kind) != trackedFileQuery) || args[len(args)-2] != "--" || strings.ContainsRune(operand, '\x00') {
					t.Fatalf("unsafe operand accepted: %q", args)
				}
			}
		}
		if kind > int(objectSizeQuery) {
			if _, err := (query{kind: queryKind(kind)}).args(); err == nil {
				t.Fatal("unknown query kind accepted")
			}
		}
	}
	for _, value := range []string{"HEAD", "abc", strings.Repeat("g", 40), strings.Repeat("a", 41)} {
		if _, err := (query{objectTypeQuery, value}).args(); err == nil {
			t.Fatal("non-OID cat-file operand accepted")
		}
	}
}

func TestSelectionGlobals(t *testing.T) {
	got, err := selectionGlobals([]string{"-p", "-Crepo", "-cname=value", "--git-dir=dir", "--work-tree", "work", "--namespace=ns", "--bare", "--config-env", "key=ENV", "--literal-pathspecs"})
	want := []string{"-C", "repo", "-c", "name=value", "--git-dir", "dir", "--work-tree", "work", "--namespace", "ns", "--bare", "--config-env=key=ENV"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("globals: %q, %v; want %q", got, err, want)
	}
	for _, args := range [][]string{{"-c"}, {"--exec-path=/tmp"}, {"push"}, {"--bare=yes"}, {"--git-dir", "bad\x00dir"}, {"--unknown"}} {
		if _, err := selectionGlobals(args); err == nil {
			t.Fatalf("unsafe globals accepted: %q", args)
		}
	}
	f := newFixture(t)
	r := f.resolver()
	r.Global = []string{"--exec-path=/must/not/use"}
	res := fill(t, r, pushOperation())
	if res.Operations[0].Target.GitDir != "" || len(r.cache) != 0 {
		t.Fatal("invalid global options were discarded and queries still ran")
	}
}

func TestQueryEnvironment(t *testing.T) {
	f := newFixture(t)
	keys := map[string]string{"GIT_TERMINAL_PROMPT": "0", "GIT_OPTIONAL_LOCKS": "0", "GIT_PAGER": "cat", "PAGER": "cat", "LC_ALL": "C", "GIT_NO_LAZY_FETCH": "1", "GTHOK_RESOLVER_QUERY": "1"}
	r := f.resolver()
	for key := range keys {
		r.Env[key] = "wrong"
		r.Global = append(r.Global, "--config-env=remote."+strings.ToLower(key)+".url="+key)
	}
	c := r.Local()
	for key, want := range keys {
		if got := c.Remotes[strings.ToLower(key)].URL; got != want {
			t.Fatalf("query %s=%q, want %q; context %+v", key, got, want, c)
		}
		if r.Env[key] != "wrong" {
			t.Fatal("resolver mutated caller environment")
		}
	}
}

func TestBoundedQueryOutput(t *testing.T) {
	var out boundedOutput
	if n, err := out.Write(bytes.Repeat([]byte("a"), maxQueryBytes)); n != maxQueryBytes || err != nil {
		t.Fatalf("valid output: %d, %v", n, err)
	}
	if _, err := out.Write([]byte("a")); err == nil || out.Len() != maxQueryBytes {
		t.Fatal("output cap ignored")
	}
	f := newFixture(t)
	f.config("resolver.large", strings.Repeat("x", maxQueryBytes/2))
	f.config("resolver.large", strings.Repeat("x", maxQueryBytes/2))
	r := f.resolver()
	r.Local()
	if _, err := r.run(context.Background(), query{kind: configQuery}); err == nil {
		t.Fatal("oversized git output was accepted")
	}
}

func TestPromisorObjectQueriesNeverFetch(t *testing.T) {
	f := newFixture(t)
	f.commit()
	f.config("remote.origin.url", "ssh://example.test/missing")
	f.config("remote.origin.promisor", "true")
	f.config("extensions.partialClone", "origin")
	marker, ssh := filepath.Join(f.root, "ssh-started"), filepath.Join(f.root, "deny-ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\n: > \"$GTHOK_TEST_SSH_RECORD\"\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	f.env["GIT_SSH"], f.env["GIT_SSH_VARIANT"], f.env["GTHOK_TEST_SSH_RECORD"] = ssh, "ssh", marker
	f.env["GIT_NO_LAZY_FETCH"] = "0"
	r := f.resolver()
	r.Local()
	for _, kind := range []queryKind{objectTypeQuery, objectSizeQuery, commitQuery} {
		if _, err := r.run(context.Background(), query{kind, strings.Repeat("a", 40)}); err == nil {
			t.Fatal("missing object unexpectedly resolved")
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("local object query started SSH: %v", err)
	}
}

func TestSkippedAndUnknownOperations(t *testing.T) {
	f := newFixture(t)
	r := f.resolver()
	op := pushOperation()
	op.Execution = model.ExecutionSkipped
	res := fill(t, r, op)
	if res.Operations[0].Target.GitDir != "" {
		t.Fatal("skipped operation was resolved")
	}
	unknown := model.NewUnknown(model.LevelL1, model.UnknownInteractive, "fixture")
	if diagnostics := r.Fill(context.Background(), &unknown, nil, false); len(diagnostics) != 0 || unknown.Classification.Resolution != model.ResolutionUnresolved {
		t.Fatal("local metadata erased unknown operation classification")
	}
}
