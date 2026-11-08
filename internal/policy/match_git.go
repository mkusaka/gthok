package policy

import (
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
)

// globMatch extends Unix filepath.Match with ** as a whole path component.
// Dynamic programming bounds work even for repeated globstars in policy input.
func globMatch(pattern, value string) bool {
	parts := strings.Split(value, "/")
	reachable := make([]bool, len(parts)+1)
	reachable[0] = true
	for _, component := range strings.Split(pattern, "/") {
		next := make([]bool, len(reachable))
		for i := range reachable {
			if component == "**" {
				next[i] = reachable[i] || i > 0 && next[i-1]
			} else if i > 0 && reachable[i-1] {
				next[i], _ = path.Match(component, parts[i-1])
			}
		}
		reachable = next
	}
	return reachable[len(parts)]
}

func labelsMatch[T ~string](patterns []string, values []T, effect model.Effect) (yes, maybe bool) {
	if len(patterns) == 0 {
		return true, false
	}
	for _, value := range values {
		if slices.Contains(patterns, string(value)) {
			return true, false
		}
	}
	maybe = len(values) == 0 && (effect == model.EffectUnknown || effect == "")
	return maybe, maybe
}

// canonicalPath resolves the longest existing, literal prefix so patterns and
// targets agree even when a symlink precedes a glob or a not-yet-created path.
func canonicalPath(value string, pattern bool) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	prefix := absolute
	for {
		if !pattern || !strings.ContainsAny(prefix, "*?[\\") {
			if resolved, err := filepath.EvalSymlinks(prefix); err == nil {
				suffix, err := filepath.Rel(prefix, absolute)
				if err != nil {
					return "", err
				}
				return filepath.Join(resolved, suffix), nil
			}
		}
		parent := filepath.Dir(prefix)
		if parent == prefix {
			return absolute, nil
		}
		prefix = parent
	}
}

func matchFilesystem(pattern, value string) bool {
	pattern, err := canonicalPath(pattern, true)
	if err != nil {
		return false
	}
	value, err = canonicalPath(value, false)
	return err == nil && globMatch(pattern, value)
}

// normalizeRemoteURL ignores userinfo, queries and fragments in actual URLs.
// Parse the authority separately so ?, [] and * remain usable in URL globs.
func normalizeRemoteURL(value string, pattern bool) (string, bool) {
	if value == "" || strings.ContainsRune(value, 0) {
		return "", false
	}
	if pattern && value == "**" {
		return value, true
	}
	scheme, rest, hasScheme := strings.Cut(value, "://")
	if !hasScheme {
		colon := strings.IndexByte(value, ':')
		if close := strings.IndexByte(value, ']'); strings.Contains(value, "[") && close >= 0 {
			if i := strings.IndexByte(value[close+1:], ':'); i >= 0 {
				colon = close + 1 + i
			}
		}
		if colon > 0 && !strings.Contains(value[:colon], "/") {
			scheme, rest = "ssh", value[:colon]+"/"+strings.TrimPrefix(value[colon+1:], "/")
		} else {
			local, err := canonicalPath(value, pattern)
			return "file://" + local, err == nil
		}
	}
	scheme = strings.ToLower(scheme)
	if !pattern {
		if i := strings.IndexAny(rest, "?#"); i >= 0 {
			rest = rest[:i]
		}
	}
	host, remotePath, _ := strings.Cut(rest, "/")
	if at := strings.LastIndexByte(host, '@'); at >= 0 {
		host = host[at+1:]
	}
	host = strings.ToLower(host)
	if host == "" && scheme != "file" {
		return "", false
	}
	switch scheme {
	case "http":
		host = strings.TrimSuffix(host, ":80")
	case "https":
		host = strings.TrimSuffix(host, ":443")
	case "ssh":
		host = strings.TrimSuffix(host, ":22")
	}
	decoded, err := url.PathUnescape(remotePath)
	if err != nil {
		return "", false
	}
	return scheme + "://" + host + path.Clean("/"+decoded), true
}

func matchRemoteURL(pattern, value string) bool {
	pattern, validPattern := normalizeRemoteURL(pattern, true)
	value, validValue := normalizeRemoteURL(value, false)
	return validPattern && validValue && globMatch(pattern, value)
}

func scalarTargetMatch(patterns []string, value string, match func(string, string) bool) (yes, maybe bool) {
	if len(patterns) == 0 {
		return true, false
	}
	if value == "" {
		return true, true
	}
	return anyMatch(patterns, value, match), false
}

func setTargetMatch(patterns []string, target *model.TargetSet, paths bool) (yes, maybe bool) {
	if len(patterns) == 0 {
		return true, false
	}
	if target == nil {
		return true, true
	}
	switch target.Kind {
	case model.SetNone:
		return false, false
	case model.SetExact, model.SetSet:
		for _, value := range target.Values {
			if paths && strings.HasPrefix(value, ":") {
				maybe = true
				continue
			}
			if paths {
				value = path.Clean(value)
			}
			for _, pattern := range patterns {
				if paths {
					pattern = path.Clean(pattern)
				}
				if globMatch(pattern, value) {
					return true, false
				}
			}
		}
		maybe = maybe || !target.Complete
		return maybe, maybe
	default: // Git patterns and unknown targets cannot prove disjointness.
		return true, true
	}
}
