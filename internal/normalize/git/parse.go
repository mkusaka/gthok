package git

import (
	"fmt"
	"strings"

	"github.com/mkusaka/gthok/internal/gitargs"
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/registry"
)

// Spans index the expanded analysis argv, half-open, including root options.
// Attached values retain their byte offset so adapters need not split argv.
type ParsedOption struct {
	Name        string
	Value       string
	HasValue    bool
	Negated     bool // relative to the registry spelling (including no-* names)
	Span        model.ArgSpan
	ValueIndex  int
	ValueOffset int
	Definition  registry.Option
}

type ParsedArgument struct {
	Value         string
	Span          model.ArgSpan
	AfterDashDash bool
}

type ParsedCommand struct {
	Path          []string
	Options       []ParsedOption // in invocation order, including repeated flags
	Positionals   []ParsedArgument
	Unknown       []ParsedArgument // KEEP_UNKNOWN_OPT, never silently discarded
	AfterDashDash []ParsedArgument
	Help          bool
}

func (p ParsedCommand) Option(name string) (ParsedOption, bool) {
	for i := len(p.Options) - 1; i >= 0; i-- {
		if p.Options[i].Name == name || p.Options[i].Definition.Short == name || p.Options[i].Definition.Long == name {
			return p.Options[i], true
		}
	}
	return ParsedOption{}, false
}

func (p ParsedCommand) Has(name string) bool {
	o, ok := p.Option(name)
	return ok && !o.Negated
}

func (p ParsedCommand) Values() []string {
	values := make([]string, len(p.Positionals))
	for i, p := range p.Positionals {
		values[i] = p.Value
	}
	return values
}

type syntaxError struct {
	reason model.UnknownReason
	detail string
}

func (e *syntaxError) Error() string { return e.detail }

func parseError(reason model.UnknownReason, format string, args ...any) error {
	return &syntaxError{reason: reason, detail: fmt.Sprintf(format, args...)}
}

// Parse parses root options and the command grammar without I/O or aliases.
func Parse(argv []string) (ParsedCommand, error) {
	g, err := gitargs.Parse(argv)
	if err != nil {
		return ParsedCommand{}, err
	}
	return parseCommand(g)
}

func parseCommand(g gitargs.Global) (ParsedCommand, error) {
	command := g.Command
	if command == "" {
		command = g.Early
	}
	p := ParsedCommand{Path: []string{command}}
	entry, ok := commands[command]
	if !ok {
		return p, parseError(model.UnknownCommand, "unknown Git command %q", command)
	}
	err := p.parseArgs(entry.Grammar, g.Args, len(g.Tokens)+1)
	p.retainBundles()
	if err == nil && !p.Help {
		err = p.checkExclusive(entry.Grammar)
	}
	return p, err
}

// checkExclusive rejects option groups Git refuses together (CMDMODE and
// die_for_incompatible_opt), along the parsed subcommand path. A member such
// as "-u/--update" lists spellings of one choice.
func (p ParsedCommand) checkExclusive(g registry.Grammar) error {
	path := p.Path[1:]
	for {
		for _, group := range g.Exclusive {
			var seen []string
			for _, member := range group {
				for _, spelling := range strings.Split(member, "/") {
					if p.given(spelling) {
						seen = append(seen, spelling)
						break
					}
				}
			}
			if len(seen) > 1 {
				return parseError(model.UnknownInvalidCombination, "options %s and %s cannot be used together", seen[0], seen[1])
			}
		}
		if len(path) == 0 {
			return nil
		}
		next := false
		for _, sub := range g.Subcommands {
			if sub.Name == path[0] {
				g, path, next = sub.Grammar, path[1:], true
				break
			}
		}
		if !next {
			return nil
		}
	}
}

// given reports whether the last occurrence of spelling ("-u", "--no-index",
// "--task=") leaves it in effect.
func (p ParsedCommand) given(spelling string) bool {
	name := strings.TrimSuffix(strings.TrimLeft(spelling, "-"), "=")
	if p.Has(name) {
		return true
	}
	base, ok := strings.CutPrefix(name, "no-")
	if !ok {
		return false
	}
	o, found := p.Option(base)
	return found && o.Negated
}

func hasFlag(flags []string, flag string) bool {
	for _, f := range flags {
		if f == flag {
			return true
		}
	}
	return false
}

func (p *ParsedCommand) positional(value string, index int, after bool) {
	a := ParsedArgument{Value: value, Span: model.ArgSpan{Start: index, End: index + 1}, AfterDashDash: after}
	p.Positionals = append(p.Positionals, a)
	if after {
		p.AfterDashDash = append(p.AfterDashDash, a)
	}
}

func (p *ParsedCommand) parseArgs(grammar registry.Grammar, args []string, offset int) error {
	options := grammarOptions(grammar, p.Path)
	keepUnknown := hasFlag(grammar.ParseFlags, "PARSE_OPT_KEEP_UNKNOWN_OPT")
	internalHelp := !hasFlag(grammar.ParseFlags, "PARSE_OPT_NO_INTERNAL_HELP")
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" || arg == "--end-of-options" {
			for j := i + 1; j < len(args); j++ {
				p.positional(args[j], offset+j, arg == "--")
			}
			return nil
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			if opt, ok := shortOption(options, arg); ok && hasFlag(opt.Flags, "PARSE_OPT_NODASH") {
				if err := p.addOption(opt, false, "", false, 0, args, &i, offset); err != nil {
					return err
				}
				continue
			}
			for _, sub := range grammar.Subcommands {
				if arg == sub.Name {
					p.Path = append(p.Path, sub.Name)
					return p.parseArgs(sub.Grammar, args[i+1:], offset+i+1)
				}
			}
			if len(grammar.Subcommands) > 0 {
				if !hasFlag(grammar.ParseFlags, "PARSE_OPT_SUBCOMMAND_OPTIONAL") {
					return parseError(model.UnknownParseError, "unknown subcommand %q", arg)
				}
				if fallback, ok := p.defaultGrammar(grammar); ok {
					return p.parseArgs(fallback, args[i:], offset+i)
				}
			}
			p.positional(arg, offset+i, false)
			if hasFlag(grammar.ParseFlags, "PARSE_OPT_STOP_AT_NON_OPTION") || len(grammar.Subcommands) > 0 {
				for j := i + 1; j < len(args); j++ {
					p.positional(args[j], offset+j, false)
				}
				return nil
			}
			continue
		}
		if internalHelp && (arg == "--help" || arg == "--help-all" || (len(args) == 1 && arg == "-h")) {
			p.Help = true
			return nil
		}
		start := i
		var err error
		if strings.HasPrefix(arg, "--") {
			name, value, attached := strings.Cut(arg[2:], "=")
			opt, negated, found := longOption(options, name, !keepUnknown)
			if !found {
				err = parseError(model.UnknownFlag, "unknown or ambiguous option %s", arg)
			} else {
				err = p.addOption(opt, negated, value, attached, len(name)+3, args, &i, offset)
			}
		} else {
			for j := 1; j < len(arg); j++ {
				opt, found := shortOption(options, arg[j:j+1])
				number, numeric := numberOption(options, grammar.Parser)
				if !found && numeric && arg[j] >= '0' && arg[j] <= '9' {
					end := j + 1
					for end < len(arg) && arg[end] >= '0' && arg[end] <= '9' {
						end++
					}
					opt = number
					err = p.addOption(opt, false, arg[j:end], true, j, args, &i, offset)
					j = end - 1
					continue
				}
				if !found {
					if internalHelp && arg[j] == 'h' {
						p.Help = true
						return nil
					}
					err = parseError(model.UnknownFlag, "unknown option -%c", arg[j])
					break
				}
				attached := opt.Arity != "none" && j+1 < len(arg)
				value := ""
				if attached {
					value = arg[j+1:]
				}
				err = p.addOption(opt, false, value, attached, j+1, args, &i, offset)
				if err != nil || opt.Arity != "none" {
					break
				}
			}
		}
		if err != nil {
			if e, ok := err.(*syntaxError); !ok || e.reason != model.UnknownFlag || !keepUnknown {
				return err
			}
			if len(grammar.Subcommands) > 0 {
				if fallback, ok := p.defaultGrammar(grammar); ok {
					return p.parseArgs(fallback, args[start:], offset+start)
				}
			}
			p.Unknown = append(p.Unknown, ParsedArgument{Value: arg, Span: model.ArgSpan{Start: offset + start, End: offset + start + 1}})
		}
	}
	return nil
}

func (p *ParsedCommand) addOption(opt registry.Option, negated bool, value string, attached bool, valueOffset int, args []string, i *int, offset int) error {
	start := *i
	name := opt.Long
	if name == "" {
		name = opt.Short
	}
	if attached && (negated || opt.Arity == "none") {
		return parseError(model.UnknownParseError, "option %s takes no value", name)
	}
	o := ParsedOption{Name: name, Negated: negated, Value: value, HasValue: attached, Definition: opt, ValueIndex: -1}
	if opt.Alias != "" {
		o.Name = opt.Alias
	}
	if attached {
		o.ValueIndex, o.ValueOffset = offset+start, valueOffset
	} else if !negated && opt.Arity == "required" {
		if opt.Type == "manual" && opt.Short == "" && hasFlag(opt.Flags, "ATTACHED_VALUE") {
			return parseError(model.UnknownParseError, "option %s requires an attached value", name)
		}
		if start+1 < len(args) {
			*i++
			o.Value, o.HasValue, o.ValueIndex = args[*i], true, offset+*i
		} else if !hasFlag(opt.Flags, "PARSE_OPT_LASTARG_DEFAULT") {
			return parseError(model.UnknownParseError, "option %s requires a value", name)
		}
	}
	// Low-level callbacks consume argv outside parse-options' normal arity.
	switch opt.Callback {
	case "cacheinfo_callback":
		if start+1 >= len(args) {
			return parseError(model.UnknownParseError, "cacheinfo requires a value")
		}
		count := 3
		if combinedCacheinfo(args[start+1]) {
			count = 1
		}
		if start+count >= len(args) {
			return parseError(model.UnknownParseError, "cacheinfo requires mode, object and path")
		}
		*i = start + count
		o.Value, o.HasValue, o.ValueIndex = strings.Join(args[start+1:*i+1], ","), true, offset+start+1
	case "stdin_callback", "stdin_cacheinfo_callback":
		if start != len(args)-1 {
			return parseError(model.UnknownParseError, "option %s must be last", name)
		}
	case "unresolve_callback", "reupdate_callback":
		for j := start + 1; j < len(args); j++ {
			p.positional(args[j], offset+j, false)
		}
		*i = len(args) - 1
	}
	o.Span = model.ArgSpan{Start: offset + start, End: offset + *i + 1}
	p.Options = append(p.Options, o)
	return nil
}

func shortOption(options []registry.Option, name string) (registry.Option, bool) {
	for _, opt := range options {
		if opt.Short == name {
			return opt, true
		}
	}
	return registry.Option{}, false
}

// parse-options normalizes both no-* option names and no-* requests. Thus
// --verify negates the declared --no-verify; NONEG forbids that inversion.
func longOption(options []registry.Option, name string, abbreviate bool) (registry.Option, bool, bool) {
	base := name
	unset, doubleNo := false, false
	if strings.HasPrefix(base, "no-") {
		base, unset = base[3:], true
		if strings.HasPrefix(base, "no-") {
			base, unset, doubleNo = base[3:], false, true
		}
	}
	var found registry.Option
	var negated, matched, ambiguous bool
	for _, opt := range options {
		if opt.Long == "" {
			continue
		}
		long := strings.TrimPrefix(opt.Long, "no-")
		optUnset := long != opt.Long
		neg := unset != optUnset
		if doubleNo && !optUnset || neg && !opt.Negatable {
			continue
		}
		if long == base {
			return opt, neg, true
		}
		if abbreviate && strings.HasPrefix(long, base) {
			// Revision's hand-written strcmp/skip_prefix options have no
			// parse-options abbreviation contract.
			if opt.Type == "manual" {
				continue
			}
			canonical := opt.Long
			if opt.Alias != "" {
				canonical = opt.Alias
			}
			previous := found.Long
			if found.Alias != "" {
				previous = found.Alias
			}
			if matched && (previous != canonical || negated != neg) {
				ambiguous = true
			}
			found, negated, matched = opt, neg, true
		}
	}
	return found, negated, matched && !ambiguous
}

func grammarOptions(g registry.Grammar, path []string) []registry.Option {
	out := make([]registry.Option, 0, len(g.Options))
	for _, opt := range g.Options {
		if (path[0] == "cherry-pick" && opt.Long == "reference") ||
			(path[0] == "revert" && opt.Source.File == "builtin/revert.c" && opt.Source.Line >= 152 && opt.Source.Line < 164) {
			continue
		}
		out = append(out, opt)
	}
	if g.Parser == "revision" || g.Parser == "diff" || (path[0] == "rev-parse" && len(path) == 1) {
		if g.Parser != "diff" {
			out = append(out, revisionOptions...)
			out = append(out, registry.Option{Long: "stdin", Type: "manual", Arity: "none"})
		}
		out = append(out, diffOptions...)
	}
	if len(path) == 2 && path[0] == "stash" && (path[1] == "show" || path[1] == "list") {
		out = append(out, revisionOptions...)
		out = append(out, diffOptions...)
	}
	return out
}

func (p *ParsedCommand) defaultGrammar(g registry.Grammar) (registry.Grammar, bool) {
	if len(p.Path) != 1 {
		return registry.Grammar{}, false
	}
	name := ""
	switch p.Path[0] {
	case "config":
		return configLegacyGrammar(g), true
	case "stash":
		name = "push"
	case "notes":
		name = "list"
	default:
		return registry.Grammar{}, false
	}
	for _, sub := range g.Subcommands {
		if sub.Name == name {
			p.Path = append(p.Path, name)
			return sub.Grammar, true
		}
	}
	return registry.Grammar{}, false
}
