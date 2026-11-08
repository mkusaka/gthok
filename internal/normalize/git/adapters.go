package git

import (
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/mkusaka/gthok/internal/registry"
)

func combinedCacheinfo(value string) bool {
	parts := strings.SplitN(value, ",", 3)
	if len(parts) != 3 || (len(parts[1]) != 40 && len(parts[1]) != 64) {
		return false
	}
	_, modeErr := strconv.ParseUint(parts[0], 8, 32)
	_, oidErr := hex.DecodeString(parts[1])
	return modeErr == nil && oidErr == nil
}

func numberOption(options []registry.Option, parser string) (registry.Option, bool) {
	for _, opt := range options {
		if opt.Type == "OPTION_NUMBER" {
			opt.Long, opt.Arity = "context", "required"
			return opt, true
		}
	}
	if parser == "revision" {
		return registry.Option{Long: "max-count", Arity: "required"}, true
	}
	return registry.Option{}, false
}

// config's optional-subcommand fallback is cmd_config_actions, not the union of
// its modern subcommands. Shared location/display declarations come from get;
// only legacy action switches are supplied here (builtin/config.c:1371).
func configLegacyGrammar(g registry.Grammar) registry.Grammar {
	out := registry.Grammar{Parser: "parse-options", ParseFlags: []string{"PARSE_OPT_STOP_AT_NON_OPTION"}}
	for _, sub := range g.Subcommands {
		if sub.Name != "get" {
			continue
		}
		for _, opt := range sub.Options {
			switch opt.Long {
			case "all", "regexp", "value", "url":
				continue
			}
			out.Options = append(out.Options, opt)
		}
	}
	for _, name := range []string{"get", "get-all", "get-regexp", "get-urlmatch", "replace-all", "add", "unset", "unset-all", "rename-section", "remove-section", "list", "edit", "get-color", "get-colorbool"} {
		opt := registry.Option{Long: name, Arity: "none", Type: "OPTION_SET_INT", Kind: "OPT_CMDMODE",
			Flags:  []string{"PARSE_OPT_CMDMODE", "PARSE_OPT_NOARG", "PARSE_OPT_NONEG"},
			Source: registry.Source{File: "builtin/config.c", Line: 1374, Entrypoint: "cmd_config_actions"}}
		switch name {
		case "list":
			opt.Short = "l"
		case "edit":
			opt.Short = "e"
		}
		out.Options = append(out.Options, opt)
	}
	// comment is part of set, not get; reuse its declaration too.
	for _, sub := range g.Subcommands {
		if sub.Name == "set" {
			for _, opt := range sub.Options {
				if opt.Long == "comment" {
					out.Options = append(out.Options, opt)
				}
			}
		}
	}
	return out
}

func (p *ParsedCommand) retainBundles() {
	bundles := map[int][]int{}
	for i, o := range p.Options {
		bundles[o.Span.Start] = append(bundles[o.Span.Start], i)
	}
	for i := range p.Options {
		o := &p.Options[i]
		for _, j := range bundles[o.Span.Start] {
			other := p.Options[j]
			if i == j || other.Definition.Short == "" {
				continue
			}
			group := []string{"-" + other.Definition.Short}
			if other.HasValue {
				if other.ValueIndex == other.Span.Start {
					group[0] += other.Value
				} else {
					group = append(group, other.Value)
				}
			}
			o.Span.Keep = append(o.Span.Keep, group...)
			o.Span.KeepGroups = append(o.Span.KeepGroups, group)
			if other.Span.End > o.Span.End {
				o.Span.End = other.Span.End
			}
		}
	}
}
