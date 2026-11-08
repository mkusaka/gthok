package resolve

import (
	"context"
	"sort"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
)

func isPush(op model.Operation) bool {
	return op.Family == "remote.push" || strings.HasPrefix(op.Action, "remote.push.")
}

func stringList(value any) []string {
	switch value := value.(type) {
	case string:
		return []string{value}
	case []string:
		return value
	case []any:
		var values []string
		for _, v := range value {
			if s, ok := v.(string); ok {
				values = append(values, s)
			}
		}
		return values
	}
	return nil
}

func flag(op model.Operation, key string) bool {
	value, _ := op.Parameters[key].(bool)
	return value
}

func configTrue(value string) bool {
	switch strings.ToLower(value) {
	case "true", "yes", "on", "1":
		return true
	}
	return false
}

func configuredFlag(op model.Operation, key, fallback string) bool {
	if value, ok := op.Parameters[key].(bool); ok {
		return value
	}
	return configTrue(fallback)
}

func (r *Resolver) pushRefs(ctx context.Context, op *model.Operation) {
	if configuredFlag(*op, "mirror", r.last("remote."+op.Target.Remote+".mirror")) || flag(*op, "matching") || flag(*op, "prune") {
		unresolvedRefs(op, "matching, mirror or pruning push needs remote refs", "remote")
		return
	}
	if configuredFlag(*op, "followTags", r.last("push.followtags")) {
		unresolvedRefs(op, "follow-tags push needs remote object reachability", "remote")
		return
	}
	if flag(*op, "all") || flag(*op, "tags") {
		if err := r.loadRefs(ctx); err != nil {
			unresolvedRefs(op, err.Error(), "git")
			return
		}
		var names []string
		for name := range r.refs {
			if (flag(*op, "all") && strings.HasPrefix(name, "refs/heads/")) || (flag(*op, "tags") && strings.HasPrefix(name, "refs/tags/")) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		setRefs(op, names)
		return
	}
	refspecs := stringList(op.Parameters["refspecs"])
	if len(refspecs) == 0 {
		refspecs = stringList(op.Parameters["refspec"])
	}
	if len(refspecs) == 0 && op.Target.Refs != nil && op.Target.Refs.Kind != model.SetUnknown {
		// An explicit fully-qualified destination needs no remote advertisement.
		for _, name := range op.Target.Refs.Values {
			if !strings.HasPrefix(name, "refs/") || strings.ContainsAny(name, "*?:") {
				unresolvedRefs(op, "short push destination needs a refspec or remote refs", "remote")
				return
			}
		}
		if len(op.Target.Refs.Values) > 0 || op.Target.Refs.Kind == model.SetNone {
			return
		}
	}
	if len(refspecs) == 0 {
		refspecs = r.config["remote."+op.Target.Remote+".push"]
	}
	if len(refspecs) > 0 {
		var names []string
		matchedPattern := make(map[string]bool)
		for _, spec := range refspecs {
			spec = strings.TrimPrefix(spec, "+")
			if spec == "" || spec == ":" || strings.HasPrefix(spec, "^") {
				unresolvedRefs(op, "matching refspec needs remote refs", "remote")
				return
			}
			source, dest, explicit := strings.Cut(spec, ":")
			if strings.Contains(spec, "*") {
				if !explicit {
					dest = source
				}
				if !strings.HasPrefix(source, "refs/") || !strings.HasPrefix(dest, "refs/") || strings.Count(source, "*") != 1 || strings.Count(dest, "*") != 1 {
					unresolvedRefs(op, "short wildcard refspec needs further ref resolution", "remote")
					return
				}
				if err := r.loadRefs(ctx); err != nil {
					unresolvedRefs(op, err.Error(), "git")
					return
				}
				prefix, suffix, _ := strings.Cut(source, "*")
				var expanded []string
				for ref := range r.refs {
					if !matchedPattern[ref] && len(ref) >= len(prefix)+len(suffix) && strings.HasPrefix(ref, prefix) && strings.HasSuffix(ref, suffix) {
						matchedPattern[ref] = true
						middle := ref[len(prefix) : len(ref)-len(suffix)]
						expanded = append(expanded, strings.Replace(dest, "*", middle, 1))
					}
				}
				sort.Strings(expanded)
				names = append(names, expanded...)
				continue
			}
			if !explicit {
				if source == "@" {
					source = "HEAD"
				}
				full, err := r.dwim(ctx, source)
				if err != nil {
					unresolvedRefs(op, err.Error(), "git")
					return
				}
				data, err := r.run(ctx, query{kind: symbolicQuery, value: full})
				if err != nil || !strings.HasPrefix(strings.TrimSpace(string(data)), "refs/") {
					unresolvedRefs(op, "source ref cannot be resolved locally", "git")
					return
				}
				dest = strings.TrimSpace(string(data))
			}
			if !strings.HasPrefix(dest, "refs/") {
				// Git first tries a short destination against advertised remote
				// refs, even when its local source is an unambiguous branch.
				unresolvedRefs(op, "short explicit destination needs remote refs", "remote")
				return
			}
			names = append(names, dest)
		}
		setRefs(op, names)
		return
	}
	branch := r.local.Branch
	if !strings.HasPrefix(branch, "refs/heads/") {
		unresolvedRefs(op, "default push requires an attached local branch", "git")
		return
	}
	mode := r.last("push.default")
	if mode == "" {
		mode = "simple"
	}
	key := "branch." + strings.TrimPrefix(branch, "refs/heads/")
	upstreams := r.config[key+".merge"]
	fetchRemote := r.defaultRemote(false)
	autoSetup := configTrue(r.last("push.autosetupremote"))
	switch mode {
	case "current":
		setRefs(op, []string{branch})
	case "simple", "upstream", "tracking":
		if mode == "simple" && op.Target.Remote != fetchRemote {
			setRefs(op, []string{branch})
			return
		}
		if mode != "simple" && op.Target.Remote != fetchRemote {
			unresolvedRefs(op, "upstream push remote differs from the branch remote", "git")
			return
		}
		if len(upstreams) == 0 && autoSetup {
			setRefs(op, []string{branch})
			return
		}
		if len(upstreams) != 1 || !strings.HasPrefix(upstreams[0], "refs/") {
			unresolvedRefs(op, "default push requires one configured upstream ref", "git")
			return
		}
		if mode == "simple" && upstreams[0] != branch {
			unresolvedRefs(op, "simple push requires the same upstream branch name", "git")
			return
		}
		setRefs(op, upstreams)
	case "matching":
		unresolvedRefs(op, "matching push needs remote refs", "remote")
	case "nothing":
		unresolvedRefs(op, "push.default=nothing requires an explicit refspec", "git")
	default:
		unresolvedRefs(op, "unsupported push.default value", "git")
	}
}

func setRefs(op *model.Operation, names []string) {
	values := make([]string, 0, len(names))
	seen := make(map[string]bool)
	for _, name := range names {
		if !seen[name] {
			seen[name] = true
			values = append(values, name)
		}
	}
	kind := model.SetSet
	if len(values) == 0 {
		kind = model.SetNone
	} else if len(values) == 1 {
		kind = model.SetExact
	}
	op.Target.Refs = &model.TargetSet{Kind: kind, Values: values, Complete: true}
}
