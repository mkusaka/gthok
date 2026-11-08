package resolve

import (
	"context"
	"errors"
	"net/url"
	"os/exec"
	"sort"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
)

func exitCode(err error, code int) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == code
}

func cleanURL(raw string) string {
	if !strings.Contains(raw, "://") {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		// Even malformed URL userinfo must not escape to policy or hooks.
		scheme, rest, _ := strings.Cut(raw, "://")
		authority, path, hasPath := strings.Cut(rest, "/")
		if at := strings.LastIndex(authority, "@"); at >= 0 {
			authority = authority[at+1:]
		}
		if hasPath {
			authority += "/" + path
		}
		return scheme + "://" + authority
	}
	parsed.User = nil
	params := parsed.Query()
	for key := range params {
		lower := strings.ToLower(key)
		for _, sensitive := range []string{"token", "password", "passwd", "secret", "credential", "authorization", "api_key"} {
			if strings.Contains(lower, sensitive) {
				params.Del(key)
				break
			}
		}
	}
	parsed.RawQuery = params.Encode()
	return parsed.String()
}

func possibleGroup(op model.Operation) bool {
	for _, u := range op.Unresolved {
		if (u.Field == "target.remote" && strings.Contains(strings.ToLower(u.Reason), "group")) || u.Field == "target.remoteGroup" {
			return true
		}
	}
	return false
}

func (r *Resolver) expandGroup(original model.Operation) []model.Operation {
	op := cloneOperation(original)
	fetch := op.Action == "remote.fetch" || op.Family == "remote.fetch"
	if r.config != nil && fetch && op.Target.Remote == "" && (flag(op, "all") || flag(op, "remoteUpdate")) {
		var names []string
		if flag(op, "remoteUpdate") && !flag(op, "all") {
			for _, value := range r.config["remotes.default"] {
				names = append(names, strings.Fields(value)...)
			}
		}
		if len(names) == 0 {
			for name := range r.local.Remotes {
				if configTrue(r.last("remote."+name+".skipdefaultupdate")) || flag(op, "all") && configTrue(r.last("remote."+name+".skipfetchall")) {
					continue
				}
				names = append(names, name)
			}
			sort.Strings(names)
		}
		var result []model.Operation
		for _, name := range names {
			copy := cloneOperation(op)
			copy.Target.Remote = name
			result = append(result, copy)
		}
		if len(result) > 0 {
			return result
		}
	}
	if !hasUnresolved(op, "target.remote") && !hasUnresolved(op, "target.remoteGroup") {
		return []model.Operation{op}
	}
	values, exists := r.config["remotes."+op.Target.Remote]
	if !exists {
		return []model.Operation{op}
	}
	var result []model.Operation
	seen := make(map[string]bool)
	for _, value := range values {
		for _, name := range strings.Fields(value) {
			if seen[name] {
				continue
			}
			seen[name] = true
			copy := cloneOperation(op)
			copy.Target.Remote = name
			remaining := copy.Unresolved[:0]
			for _, u := range copy.Unresolved {
				if (u.Field != "target.remote" && u.Field != "target.remoteGroup") || u.Reason == "recursive submodule pushes require submodule configuration" {
					remaining = append(remaining, u)
				}
			}
			copy.Unresolved = remaining
			result = append(result, copy)
		}
	}
	if len(result) == 0 {
		setUnresolved(&op, "target.remote", "configured remote group is empty", "git")
		return []model.Operation{op}
	}
	return result
}

func (r *Resolver) remoteTargets(ctx context.Context, op model.Operation) []model.Operation {
	raw := op.Target.Remote
	op.Target.Remote = cleanURL(raw)
	if raw == "" {
		if isPush(op) || op.Family == "remote.fetch" || op.Family == "remote.pull" || op.Action == "remote.fetch" || op.Action == "remote.pull" {
			requireRemote(&op)
		}
		return []model.Operation{op}
	}
	info, named := r.local.Remotes[raw]
	if !named {
		if possibleGroup(op) {
			return []model.Operation{op}
		}
		for _, kind := range []queryKind{remoteQuery, pushURLQuery} {
			data, err := r.run(ctx, query{kind: kind, value: raw})
			var urls []string
			switch {
			case err == nil:
				urls = strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			case exitCode(err, 2):
				// Git treats an unconfigured remote operand as a URL/path.
				urls = []string{r.rewrite(raw, kind == pushURLQuery)}
			default:
				continue
			}
			for _, value := range urls {
				if value == "" {
					continue
				}
				if kind == remoteQuery && info.URL == "" {
					info.URL = cleanURL(value)
				} else if kind == pushURLQuery {
					info.PushURLs = append(info.PushURLs, cleanURL(value))
				}
			}
		}
	}
	if named || info.URL != "" {
		remaining := op.Unresolved[:0]
		for _, u := range op.Unresolved {
			if u.Field != "target.remote" || (u.Source != "git" && u.Source != "resolver") || u.Reason == "recursive submodule pushes require submodule configuration" {
				remaining = append(remaining, u)
			}
		}
		op.Unresolved = remaining
	}
	op.Target.Remote = cleanURL(raw)
	op.Target.RemoteURL = info.URL
	if info.URL == "" {
		setUnresolved(&op, "target.remoteUrl", "effective remote URL is unavailable from local config", "git")
	}
	if !isPush(op) {
		clearKnown(&op)
		return []model.Operation{op}
	}
	if len(info.PushURLs) == 0 {
		setUnresolved(&op, "target.pushUrl", "effective push URL is unavailable from local config", "git")
		return []model.Operation{op}
	}
	var result []model.Operation
	for _, pushURL := range info.PushURLs {
		copy := cloneOperation(op)
		copy.Target.PushURL = pushURL
		clearKnown(&copy)
		result = append(result, copy)
	}
	return result
}

// Named remotes always use get-url; this handles a URL/path given directly as
// the remote operand, for which get-url has no named remote to look up.
func (r *Resolver) rewrite(raw string, push bool) string {
	if push {
		if value, ok := r.rewriteURL(raw, ".pushinsteadof"); ok {
			return value
		}
	}
	value, _ := r.rewriteURL(raw, ".insteadof")
	return value
}

func (r *Resolver) rewriteURL(raw, suffix string) (string, bool) {
	best, replacement, found := "", "", false
	for _, key := range r.configOrder {
		if !strings.HasPrefix(key, "url.") || !strings.HasSuffix(key, suffix) {
			continue
		}
		for _, prefix := range r.config[key] {
			if (!found || len(prefix) > len(best)) && strings.HasPrefix(raw, prefix) {
				best, replacement, found = prefix, strings.TrimSuffix(strings.TrimPrefix(key, "url."), suffix), true
			}
		}
	}
	return replacement + strings.TrimPrefix(raw, best), found
}

func (r *Resolver) configURLs(name string, push bool) []string {
	values := func(key string) []string {
		var result []string
		for _, value := range r.config[key] {
			if value == "" {
				result = nil
			} else {
				result = append(result, value)
			}
		}
		return result
	}
	urls := values("remote." + name + ".url")
	if push {
		if explicit := values("remote." + name + ".pushurl"); len(explicit) > 0 {
			urls = explicit
		} else {
			var aliases []string
			for _, raw := range urls {
				if rewritten, ok := r.rewriteURL(raw, ".pushinsteadof"); ok {
					aliases = append(aliases, rewritten)
				}
			}
			if len(aliases) > 0 {
				return aliases
			}
		}
	}
	for i, raw := range urls {
		urls[i], _ = r.rewriteURL(raw, ".insteadof")
	}
	return urls
}
