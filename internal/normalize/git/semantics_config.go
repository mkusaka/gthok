package git

import (
	"strings"

	"github.com/mkusaka/gthok/internal/model"
)

func init() { register("config", configSemantics) }

// builtin/config.c:cmd_config_actions retains the legacy argc-based mode.
func configSemantics(c *Context) []model.Operation {
	p, args := c.Parsed, c.Parsed.Values()
	mode, legacy := "", len(p.Path) == 1
	if !legacy {
		mode = p.Path[1]
	} else {
		for _, o := range p.Options {
			switch o.Name {
			case "get", "get-all", "get-regexp", "get-urlmatch", "get-color", "get-colorbool", "list", "edit", "add", "replace-all", "unset", "unset-all", "rename-section", "remove-section":
				if mode != "" && mode != o.Name {
					return []model.Operation{adminInvalid(c, "conflicting config actions")}
				}
				mode = o.Name
			}
		}
		if mode == "" {
			switch len(args) {
			case 1:
				mode = "get"
			case 2, 3:
				mode = "set"
			default:
				return []model.Operation{adminInvalid(c, "config requires an action or key")}
			}
		}
	}
	action, effect := mode, model.EffectWrite
	switch mode {
	case "get", "get-all", "get-regexp", "get-urlmatch", "get-color", "get-colorbool", "list":
		effect = model.EffectRead
		if mode != "list" {
			action = "get"
		}
	case "replace-all":
		action = "set"
	case "unset-all":
		action = "unset"
	}
	if (mode != "list" && mode != "edit" && len(args) == 0) ||
		((mode == "set" || mode == "add" || mode == "replace-all" || mode == "rename-section") && len(args) < 2) {
		return []model.Operation{adminInvalid(c, "config action is missing an operand")}
	}
	op := adminOperation(c, "config", "config."+action, effect, model.ScopeConfig)
	op.Origin.Mode = mode
	if legacy {
		op.Origin.Source = "builtin/config.c:cmd_config_actions"
	}
	op.Parameters = map[string]any{"mode": mode, "all": p.Has("all") || mode == "get-all" || mode == "unset-all" || mode == "replace-all" || legacy && mode == "set" && len(args) == 3, "append": p.Has("append") || mode == "add"}
	location := "all"
	if effect == model.EffectWrite {
		location = "local"
	}
	if file := c.Invocation.Env["GIT_CONFIG"]; file != "" {
		location, op.Parameters["file"] = "file", file
	}
	for _, o := range p.Options {
		if o.Negated {
			continue
		}
		switch o.Name {
		case "global", "system", "local", "worktree":
			location = o.Name
		case "file", "blob":
			location, op.Parameters[o.Name] = o.Name, o.Value
		}
	}
	op.Parameters["location"] = location
	if location == "file" {
		op.Target.Paths = model.Exact(op.Parameters["file"].(string))
	}
	if location == "blob" {
		op.Target.Revisions = model.Exact(op.Parameters["blob"].(string))
	}
	switch mode {
	case "list":
		op.Target.ConfigKeys = &model.TargetSet{Kind: model.SetPattern, Values: []string{"*"}, Complete: true}
	case "edit":
		op.Capabilities = []model.Capability{model.CapabilityEditor}
		op.Target.ConfigKeys = model.UnknownSet()
		adminUnresolved(&op, "target.configKeys", "configuration keys are chosen in the editor", "editor")
	case "rename-section", "remove-section":
		keys := []string{adminGlobLiteral(args[0]) + ".*"}
		if mode == "rename-section" {
			keys = append(keys, adminGlobLiteral(args[1])+".*")
			op.Parameters["newSection"] = args[1]
		}
		op.Target.ConfigKeys = &model.TargetSet{Kind: model.SetPattern, Values: keys, Complete: true}
		op.Parameters["section"] = args[0]
	default:
		if mode == "get-regexp" || p.Has("regexp") || mode == "get-urlmatch" || p.Has("url") {
			op.Target.ConfigKeys = model.UnknownSet()
			op.Parameters["keyPattern"] = args[0]
			adminUnresolved(&op, "target.configKeys", "config regexp or URL matching needs local configuration", "git")
		} else {
			op.Target.ConfigKeys = model.Exact(configKey(args[0]))
		}
	}
	if o, ok := p.Option("value"); ok && !o.Negated {
		op.Parameters["valuePattern"] = o.Value
	} else if legacy && len(args) > 1 {
		i := 1
		if effect == model.EffectWrite && action != "unset" {
			i = 2
		}
		if i < len(args) {
			op.Parameters["valuePattern"] = args[i]
		}
	}
	op.Parameters["fixedValue"] = p.Has("fixed-value")
	return []model.Operation{op}
}

func configKey(key string) string {
	parts := strings.Split(key, ".")
	parts[0] = strings.ToLower(parts[0])
	parts[len(parts)-1] = strings.ToLower(parts[len(parts)-1])
	return strings.Join(parts, ".")
}

func adminGlobLiteral(s string) string {
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[", "]", "\\]").Replace(s)
}
