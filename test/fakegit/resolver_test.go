//go:build unix

package main

import (
	"strings"
	"testing"
)

func TestResolverQueriesAreClosedAndTagged(t *testing.T) {
	tag := []string{"GTHOK_RESOLVER_QUERY=1"}
	for _, args := range [][]string{
		{"rev-parse", "--git-dir", "--git-common-dir", "--is-bare-repository", "--is-inside-work-tree", "--show-prefix"},
		{"rev-parse", "--show-toplevel"},
		{"rev-parse", "--verify", "--quiet", "--end-of-options", "HEAD^{commit}"},
		{"rev-parse", "--symbolic-full-name", "main"},
		{"symbolic-ref", "-q", "HEAD"},
		{"config", "--null", "--list"},
		{"config", "--null", "--get", "push.default"},
		{"config", "--null", "--get-all", "remote.origin.url"},
		{"config", "--null", "--get-regexp", "^remote\\."},
		{"for-each-ref", "--format=%(refname)"},
		{"for-each-ref", "--format=%(refname)", "refs/heads/"},
		{"show-ref"},
		{"remote", "get-url", "--all", "origin"},
		{"remote", "get-url", "--all", "--push", "origin"},
		{"ls-files", "-z", "--", "path"},
		{"ls-files", "-z", "--error-unmatch", "--", "-path\n"},
		{"cat-file", "-t", strings.Repeat("a", 40)},
		{"cat-file", "-s", strings.Repeat("a", 64)},
	} {
		if !resolverQuery(args, tag) {
			t.Fatalf("fixed query not recognized: %q", args)
		}
		if resolverQuery(args, nil) {
			t.Fatalf("identical original request hidden: %q", args)
		}
		if resolverQuery(args, append(append([]string(nil), tag...), "GTHOK_DELEGATE_ID=delegate")) {
			t.Fatalf("delegate execution hidden: %q", args)
		}
		globals := []string{"-C", "repo", "-c", "core.bare=false", "--config-env=remote.origin.url=URL", "--namespace=fixture"}
		if !resolverQuery(append(globals, args...), tag) {
			t.Fatalf("query globals lost: %q", args)
		}
	}
	for _, args := range [][]string{
		nil, {"push", "origin"}, {"fetch"}, {"ls-remote"}, {"credential", "fill"}, {"ssh"},
		{"rev-parse", "--show-toplevel", "extra"}, {"rev-parse", "--symbolic-full-name", "--output=file"},
		{"config", "--null", "--list", "--file", "file"}, {"config", "--null", "--get", "--edit"},
		{"for-each-ref"}, {"for-each-ref", "--format=%(contents)"}, {"show-ref", "--verify", "HEAD"},
		{"remote", "get-url", "--all", "--upload-pack=other"}, {"cat-file", "--batch"}, {"cat-file", "-t", "HEAD"},
	} {
		if resolverQuery(args, tag) {
			t.Fatalf("non-template execution hidden: %q", args)
		}
	}
	if !resolverQuery([]string{"rev-parse", "--show-toplevel"}, append(tag, "FAKEGIT_MODE=pty")) {
		t.Fatal("resolver inherited request-only PTY behavior")
	}
}
