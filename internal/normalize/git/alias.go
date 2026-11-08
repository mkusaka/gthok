package git

import (
	"fmt"
	"strings"

	"github.com/mkusaka/gthok/internal/gitargs"
	"github.com/mkusaka/gthok/internal/model"
)

// splitCmdline is alias.c split_cmdline, including its leading/trailing empty
// arguments and backslash handling inside double (but not single) quotes.
func splitCmdline(line string) ([]string, error) {
	out := []string{}
	var word strings.Builder
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote == 0 && asciiSpace(c):
			out = append(out, word.String())
			word.Reset()
			for i+1 < len(line) && asciiSpace(line[i+1]) {
				i++
			}
		case quote == 0 && (c == '\'' || c == '"'):
			quote = c
		case c == quote:
			quote = 0
		default:
			if c == '\\' && quote != '\'' {
				i++
				if i == len(line) {
					return nil, fmt.Errorf("cmdline ends with backslash")
				}
				c = line[i]
			}
			word.WriteByte(c)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed quote")
	}
	return append(out, word.String()), nil
}

func asciiSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f'
}

// ExpandAlias changes analysis argv only. Aliases use case-insensitive legacy
// keys, or case-sensitive subsection names in <name>.command keys.
func ExpandAlias(argv []string, aliases map[string]string) ([]string, string, model.UnknownReason) {
	return expandAlias(argv, aliases, nil)
}

func expandAlias(argv []string, aliases map[string]string, env map[string]string) ([]string, string, model.UnknownReason) {
	hops, alias, reason := ExpandAliasHops(argv, aliases, env)
	return hops[len(hops)-1], alias, reason
}

// ExpandAliasHops includes the original argv and each accepted expansion, with
// that hop's global options. The last hop is ExpandAlias's analysis argv; env
// supplies values for --config-env without performing executable lookup.
func ExpandAliasHops(argv []string, aliases map[string]string, env map[string]string) ([][]string, string, model.UnknownReason) {
	out := append([]string(nil), argv...)
	hops := [][]string{out}
	first := ""
	seen := map[string]bool{}
	for depth := 0; depth < 128; depth++ {
		g, err := gitargs.Parse(out)
		if err != nil || g.Command == "" {
			return hops, first, model.UnknownNone
		}
		entry, known := commands[g.Command]
		if known && entry.Implementation != "document" && entry.Implementation != "option-only" && !hasFlag(entry.Flags, "DEPRECATED") {
			// run_argv tries builtins and dashed executables before aliases.
			return hops, first, model.UnknownNone
		}
		value, ok, ambiguous := lookupAlias(g.Command, aliasesWithGlobals(g, aliases, env))
		if ambiguous {
			if first == "" {
				first = g.Command
			}
			return hops, first, model.UnknownParseError
		}
		if !ok {
			return hops, first, model.UnknownNone
		}
		if first == "" {
			first = g.Command
		}
		if seen[g.Command] {
			return hops, first, model.UnknownAliasLoop
		}
		seen[g.Command] = true
		if strings.HasPrefix(value, "!") {
			return hops, first, model.UnknownShellAlias
		}
		words, err := splitCmdline(value)
		if err != nil {
			return hops, first, model.UnknownParseError
		}
		aliasGlobal, err := gitargs.Parse(words)
		if err != nil {
			return hops, first, model.UnknownParseError
		}
		next := append([]string(nil), g.Tokens...)
		next = append(next, words...)
		next = append(next, g.Args...)
		if aliasGlobal.Early != "" && aliasGlobal.Early != "version" && aliasGlobal.Early != "help" {
			return append(hops, next), first, model.UnknownNone
		}
		// Only cmd_main rewrites -v/--version and -h/--help into commands;
		// handle_alias leaves those command words verbatim and lookup fails.
		if n := len(aliasGlobal.Tokens); n < len(words) && (words[n] == "--version" || words[n] == "-v" || words[n] == "--help" || words[n] == "-h") {
			return append(hops, next), first, model.UnknownCommand
		}
		if aliasGlobal.Command == "" || aliasChangesEnvironment(aliasGlobal) {
			return hops, first, model.UnknownParseError
		}
		out = next
		hops = append(hops, out)
	}
	return hops, first, model.UnknownAliasLoop
}

func lookupAlias(name string, aliases map[string]string) (value string, found, ambiguous bool) {
	// Invocation.Aliases has no config ordering. Conflicting legacy/subsection
	// spellings must fail closed rather than guess which Git would run last.
	for key := range aliases {
		match := key == name+".command"
		if !strings.HasSuffix(key, ".command") {
			match = strings.EqualFold(key, name)
		} else if key == ".command" {
			match = strings.EqualFold(name, "command")
		}
		if match {
			if found && value != aliases[key] {
				return "", true, true
			}
			value, found = aliases[key], true
		}
	}
	return value, found, false
}

func aliasesWithGlobals(g gitargs.Global, aliases map[string]string, env map[string]string) map[string]string {
	out := make(map[string]string, len(aliases))
	for key, value := range aliases {
		out[key] = value
	}
	for i := 0; i < len(g.Tokens); i++ {
		key, value, attached := strings.Cut(g.Tokens[i], "=")
		switch key {
		case "-c", "--config-env":
			if !attached && i+1 < len(g.Tokens) {
				i++
				value = g.Tokens[i]
			}
			configKey, configValue, _ := strings.Cut(value, "=")
			if key == "--config-env" {
				eq := strings.LastIndexByte(value, '=')
				configKey, configValue = value[:eq], env[value[eq+1:]]
			}
			if strings.HasPrefix(strings.ToLower(configKey), "alias.") {
				name := configKey[len("alias."):]
				if dot := strings.LastIndexByte(name, '.'); dot >= 0 && strings.EqualFold(name[dot+1:], "command") {
					name = name[:dot] + ".command"
					if strings.TrimSuffix(name, ".command") == g.Command {
						for existing := range out {
							if existing == name || (!strings.HasSuffix(existing, ".command") && strings.EqualFold(existing, g.Command)) {
								delete(out, existing)
							}
						}
					}
				} else {
					name = strings.ToLower(name)
					for existing := range out {
						if strings.EqualFold(strings.TrimSuffix(existing, ".command"), name) {
							delete(out, existing)
						}
					}
				}
				if name == ".command" {
					name = "command"
				}
				out[name] = configValue
			}
		case "-C", "--git-dir", "--work-tree", "--namespace", "--attr-source", "--shallow-file":
			if !attached {
				i++
			}
		}
	}
	return out
}

func aliasChangesEnvironment(g gitargs.Global) bool {
	for i := 0; i < len(g.Tokens); i++ {
		arg := g.Tokens[i]
		switch {
		case arg == "-c" || arg == "--config-env":
			i++
		case arg == "-C":
			i++
			if g.Tokens[i] != "" {
				return true
			}
		case arg == "-p" || arg == "--paginate" || strings.HasPrefix(arg, "--exec-path=") || strings.HasPrefix(arg, "--config-env="):
		default:
			return true
		}
	}
	return false
}
