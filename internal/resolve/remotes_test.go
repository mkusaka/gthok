package resolve

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

func TestConfigURLFallbackMatchesGit(t *testing.T) {
	f := newFixture(t)
	f.config("url.https://example.test/first/.insteadOf", "short:")
	f.config("url.https://example.test/second/.insteadOf", "short:")
	f.config("url.https://example.test/long/.insteadOf", "short:long/")
	f.config("url.ssh://example.test/write/.pushInsteadOf", "short:")
	for _, name := range []string{"implicit", "explicit", "reset", "unmatched"} {
		f.config("remote."+name+".url", "short:repo")
		f.config("remote."+name+".url", "other:repo")
	}
	f.config("remote.explicit.pushurl", "short:long/one")
	f.config("remote.explicit.pushurl", "other:two")
	f.config("remote.reset.url", "")
	f.config("remote.reset.url", "short:long/reset")
	f.config("remote.unmatched.url", "")
	f.config("remote.unmatched.url", "other:repo")
	r := f.resolver()
	r.Local()
	for _, name := range []string{"implicit", "explicit", "reset", "unmatched"} {
		for _, push := range []bool{false, true} {
			args := []string{"-C", f.repo, "remote", "get-url", "--all"}
			if push {
				args = append(args, "--push")
			}
			want := strings.Split(f.run(append(args, name)...), "\n")
			if got := r.configURLs(name, push); !reflect.DeepEqual(got, want) {
				t.Fatalf("fallback %s push=%t: %q; git: %q", name, push, got, want)
			}
		}
	}
}

func TestGlobalOnlyRemote(t *testing.T) {
	f := newFixture(t)
	r := f.resolver()
	r.Global = []string{"-c", "remote.session.url=short:repo", "-c", "url.https://example.test/read/.insteadOf=short:", "-c", "url.ssh://example.test/write/.pushInsteadOf=short:"}
	c := r.Local()
	if c.Remotes["session"].URL != "https://example.test/read/repo" || !reflect.DeepEqual(c.Remotes["session"].PushURLs, []string{"ssh://example.test/write/repo"}) {
		t.Fatalf("command-scope remote: %+v", c)
	}
	op := pushOperation()
	op.Target.Remote = "session"
	op.Parameters["refspec"] = "HEAD:refs/heads/main"
	got := fill(t, r, op).Operations[0]
	if got.Target.PushURL != "ssh://example.test/write/repo" || len(got.Unresolved) != 0 {
		t.Fatalf("command-scope remote target: %+v", got)
	}
}

func TestAmbiguousRemoteGroupRemainsUnknown(t *testing.T) {
	f := newFixture(t)
	op := pushOperation()
	op.Target.Remote = "missing-group"
	op.Unresolved = []model.Unresolved{{Field: "target.remote", Reason: "possible group", Source: "git"}}
	got := fill(t, f.resolver(), op).Operations[0]
	if !hasUnresolved(got, "target.remote") || got.Target.RemoteURL != "" || got.Target.PushURL != "" {
		t.Fatalf("unconfigured group guessed to be a URL: %+v", got)
	}
	f.config("remotes.empty", "")
	op.Target.Remote = "empty"
	got = fill(t, f.resolver(), op).Operations[0]
	if !hasUnresolved(got, "target.remote") || got.Target.RemoteURL != "" {
		t.Fatalf("empty group guessed: %+v", got)
	}
}

func TestURLLineBreakDoesNotInventMultipleTargets(t *testing.T) {
	f := newFixture(t)
	f.config("remote.origin.url", "https://example.test/one\nhttps://example.test/two")
	r := f.resolver()
	if c := r.Local(); c.Remotes["origin"].URL != "" || len(c.Remotes["origin"].PushURLs) != 0 {
		t.Fatalf("ambiguous URL output was split: %+v", c)
	}
	op := pushOperation()
	op.Parameters["refspec"] = "HEAD:refs/heads/main"
	res := fill(t, r, op)
	if len(res.Operations) != 1 || !hasUnresolved(res.Operations[0], "target.pushUrl") {
		t.Fatalf("ambiguous URL became known: %+v", res)
	}
}

func TestCleanURL(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://fixture:" + "password" + "@example.test/repo", "https://example.test/repo"},
		{"https://fixture:" + "%zz" + "@example.test/repo", "https://example.test/repo"},
		{"https://example.test/repo?token=" + "fixture-secret" + "&ordinary=ok", "https://example.test/repo?ordinary=ok"},
		{"git@example.test:repo", "git@example.test:repo"},
		{"../repo", "../repo"},
	} {
		if got := cleanURL(tc.raw); got != tc.want {
			t.Fatalf("URL sanitization: got %q; want %q", got, tc.want)
		}
	}
}
