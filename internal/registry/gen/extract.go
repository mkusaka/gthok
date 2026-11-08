package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mkusaka/gthok/internal/registry"
)

var knownFlags = strings.Fields(`PARSE_OPT_KEEP_DASHDASH PARSE_OPT_STOP_AT_NON_OPTION PARSE_OPT_KEEP_ARGV0 PARSE_OPT_KEEP_UNKNOWN_OPT PARSE_OPT_NO_INTERNAL_HELP PARSE_OPT_ONE_SHOT PARSE_OPT_SHELL_EVAL PARSE_OPT_SUBCOMMAND_OPTIONAL PARSE_OPT_OPTARG PARSE_OPT_NOARG PARSE_OPT_NONEG PARSE_OPT_HIDDEN PARSE_OPT_LASTARG_DEFAULT PARSE_OPT_NODASH PARSE_OPT_LITERAL_ARGHELP PARSE_OPT_FROM_ALIAS PARSE_OPT_NOCOMPLETE PARSE_OPT_COMP_ARG PARSE_OPT_CMDMODE`)

var parseResults = strings.Fields(`PARSE_OPT_COMPLETE PARSE_OPT_HELP_ERROR PARSE_OPT_HELP PARSE_OPT_ERROR PARSE_OPT_DONE PARSE_OPT_NON_OPTION PARSE_OPT_SUBCOMMAND PARSE_OPT_UNKNOWN`)

var lowLevelAdapters = strings.Fields(`cacheinfo_callback stdin_callback stdin_cacheinfo_callback unresolve_callback reupdate_callback option_read_message diff_opt_output`)

var noArgTypes = strings.Fields(`OPTION_BIT OPTION_NEGBIT OPTION_BITOP OPTION_COUNTUP OPTION_SET_INT OPTION_NUMBER`)
var argTypes = strings.Fields(`OPTION_STRING OPTION_INTEGER OPTION_UNSIGNED OPTION_CALLBACK OPTION_LOWLEVEL_CALLBACK OPTION_FILENAME`)

type function struct {
	name       string
	start, end int
	line       int
	body       []token
	static     bool
}

type table struct {
	name, owner string
	source      registry.Source
	grammar     registry.Grammar
}

type sourceFile struct {
	name      string
	tokens    []token
	functions map[string]function
	tables    []table
}

func parseFunctions(ts []token) (map[string]function, error) {
	out := map[string]function{}
	declarationStart := 0
	for i := 0; i < len(ts); i++ {
		if ts[i].text == ";" {
			declarationStart = i + 1
		}
		if ts[i].text != "{" {
			continue
		}
		end, err := matching(ts, i)
		if err != nil {
			return nil, err
		}
		if i > 0 && ts[i-1].text == ")" {
			depth, j := 1, i-2
			for ; j >= 0; j-- {
				if ts[j].text == ")" {
					depth++
				}
				if ts[j].text == "(" {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			if j > 0 && identByte(ts[j-1].text[0]) {
				name := ts[j-1].text
				out[name] = function{name: name, start: i, end: end, line: ts[j-1].line, body: ts[i+1 : end], static: containsToken(ts[declarationStart:j], "static")}
			}
		}
		i = end
		declarationStart = end + 1
	}
	return out, nil
}

func flags(ts []token) ([]string, error) {
	var out []string
	for _, t := range ts {
		if strings.HasPrefix(t.text, "PARSE_OPT_") {
			if !slices.Contains(knownFlags, t.text) {
				return nil, fmt.Errorf("line %d: unmapped flag %s", t.line, t.text)
			}
			out = append(out, t.text)
		}
	}
	return sortedUnique(out), nil
}

func optionFlags(ts []token) ([]string, error) {
	for _, t := range ts {
		if identByte(t.text[0]) && !slices.Contains(knownFlags, t.text) {
			return nil, fmt.Errorf("line %d: unmapped flag %s", t.line, t.text)
		}
		if t.text[0] >= '0' && t.text[0] <= '9' && t.text != "0" {
			return nil, fmt.Errorf("line %d: unmapped numeric flag %s", t.line, t.text)
		}
		if !identByte(t.text[0]) && t.text != "0" && t.text != "|" && t.text != "(" && t.text != ")" {
			return nil, fmt.Errorf("line %d: unmapped flag expression %s", t.line, t.text)
		}
	}
	return flags(ts)
}

func sortedUnique(ss []string) []string { slices.Sort(ss); return slices.Compact(ss) }

func parseInitializer(ts []token, file, owner string) (registry.Grammar, error) {
	g := registry.Grammar{Parser: "parse-options"}
	parts, err := split(ts)
	if err != nil {
		return g, err
	}
	// Git only rejects CMDMODE options that set different values, so options
	// sharing a defval are one member: "--continue/--resolved".
	groups, modes := map[string][]string{}, map[string]map[string]int{}
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		if part[0].text != "{" || part[len(part)-1].text != "}" {
			return g, fmt.Errorf("%s:%d: unexpanded option constructor %s", file, part[0].line, text(part))
		}
		fields, err := split(part[1 : len(part)-1])
		if err != nil {
			return g, err
		}
		values := map[string][]token{}
		positional := []string{"type", "short_name", "long_name", "value", "precision", "argh", "help", "flags", "callback", "defval", "ll_callback", "extra", "subcommand_fn"}
		for i, f := range fields {
			if len(f) >= 3 && f[0].text == "." && f[2].text == "=" {
				values[f[1].text] = f[3:]
			} else {
				if i >= len(positional) {
					return g, fmt.Errorf("%s:%d: too many struct option fields", file, part[0].line)
				}
				values[positional[i]] = f
			}
		}
		typ := symbol(values["type"])
		if typ == "OPTION_END" || typ == "OPTION_GROUP" {
			continue
		}
		fs, err := optionFlags(values["flags"])
		if err != nil {
			return g, err
		}
		source := registry.Source{File: file, Line: part[0].line, Entrypoint: owner}
		long := literal(values["long_name"])
		if typ == "OPTION_SUBCOMMAND" {
			g.Subcommands = append(g.Subcommands, registry.Subcommand{Name: long, Entrypoint: symbol(values["subcommand_fn"]), Source: source, Flags: fs})
			continue
		}
		if typ != "OPTION_ALIAS" && !slices.Contains(noArgTypes, typ) && !slices.Contains(argTypes, typ) {
			return g, fmt.Errorf("%s:%d: unmapped option type %s", file, source.Line, typ)
		}
		arity := "required"
		if slices.Contains(noArgTypes, typ) || slices.Contains(fs, "PARSE_OPT_NOARG") {
			arity = "none"
		} else if slices.Contains(fs, "PARSE_OPT_OPTARG") {
			arity = "optional"
		}
		kind := part[0].kind
		if kind == "" {
			kind = typ
		}
		o := registry.Option{Long: long, Short: literal(values["short_name"]), Kind: kind, Type: typ, Arity: arity, Negatable: long != "" && !slices.Contains(fs, "PARSE_OPT_NONEG"), Flags: fs, Source: source, Value: text(values["value"]), Callback: symbol(values["callback"])}
		if typ == "OPTION_ALIAS" {
			o.Alias = literal(values["value"])
		}
		if o.Long == "" && o.Short == "" && typ != "OPTION_NUMBER" {
			return g, fmt.Errorf("%s:%d: nonliteral option spelling %s", file, source.Line, text(values["long_name"]))
		}
		if typ == "OPTION_LOWLEVEL_CALLBACK" || symbol(values["ll_callback"]) != "" {
			o.Callback = symbol(values["ll_callback"])
			if !slices.Contains(lowLevelAdapters, o.Callback) {
				return g, fmt.Errorf("%s:%d: unmapped low-level callback %s", file, source.Line, o.Callback)
			}
			g.Adapters = append(g.Adapters, "callback:"+o.Callback)
		}
		g.Options = append(g.Options, o)
		if slices.Contains(fs, "PARSE_OPT_CMDMODE") {
			defval := text(values["defval"])
			if modes[o.Value] == nil {
				modes[o.Value] = map[string]int{}
			}
			if i, ok := modes[o.Value][defval]; ok {
				groups[o.Value][i] += "/" + spelling(o)
			} else {
				modes[o.Value][defval] = len(groups[o.Value])
				groups[o.Value] = append(groups[o.Value], spelling(o))
			}
		}
	}
	for _, key := range sortedKeys(groups) {
		if len(groups[key]) > 1 {
			g.Exclusive = append(g.Exclusive, groups[key])
		}
	}
	return g, nil
}

func spelling(o registry.Option) string {
	if o.Long != "" {
		return "--" + o.Long
	}
	return "-" + o.Short
}

func parseSource(name, content string, macros map[string]macro) (*sourceFile, error) {
	ts, err := lex(content)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	for _, t := range ts {
		if !strings.HasPrefix(t.text, "PARSE_OPT_") || slices.Contains(knownFlags, t.text) || slices.Contains(parseResults, t.text) {
			continue
		}
		if _, ok := macros[t.text]; !ok {
			return nil, fmt.Errorf("%s:%d: unmapped flag %s", name, t.line, t.text)
		}
		expanded, err := expand([]token{t}, macros, 0)
		if err != nil {
			return nil, err
		}
		if _, err := flags(expanded); err != nil {
			return nil, err
		}
	}
	funcs, err := parseFunctions(ts)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	f := &sourceFile{name: name, tokens: ts, functions: funcs}
	for i := 0; i+6 < len(ts); i++ {
		if ts[i].text != "struct" || ts[i+1].text != "option" || ts[i+3].text != "[" {
			continue
		}
		j, err := matching(ts, i+3)
		if err != nil {
			return nil, err
		}
		if j+2 >= len(ts) || ts[j+1].text != "=" || ts[j+2].text != "{" {
			continue
		}
		end, err := matching(ts, j+2)
		if err != nil {
			return nil, err
		}
		owner := ""
		for _, fn := range funcs {
			if i > fn.start && i < fn.end {
				owner = fn.name
				break
			}
		}
		body, err := expand(ts[j+3:end], macros, 0)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		g, err := parseInitializer(body, name, owner)
		if err != nil {
			return nil, err
		}
		if owner != "" {
			depth := 0
			for _, tok := range ts[funcs[owner].start+1 : i] {
				if tok.text == "{" {
					depth++
				}
				if tok.text == "}" {
					depth--
				}
			}
			if depth > 0 {
				g.Adapters = append(g.Adapters, fmt.Sprintf("conditional-options:%s:%d", name, ts[i].line))
			}
		}
		t := table{name: ts[i+2].text, owner: owner, source: registry.Source{File: name, Line: ts[i].line, Entrypoint: owner}, grammar: g}
		g.Tables = []string{fmt.Sprintf("%s:%d:%s", name, ts[i].line, t.name)}
		t.grammar = g
		f.tables = append(f.tables, t)
		i = end
	}
	return f, nil
}

func mergeGrammar(dst *registry.Grammar, src registry.Grammar) {
	for _, o := range src.Options {
		found := false
		for _, p := range dst.Options {
			if o.Source == p.Source && o.Long == p.Long && o.Short == p.Short {
				found = true
				break
			}
		}
		if !found {
			dst.Options = append(dst.Options, o)
		}
	}
	for _, sub := range src.Subcommands {
		found := false
		for _, s := range dst.Subcommands {
			if s.Name == sub.Name {
				found = true
				break
			}
		}
		if !found {
			dst.Subcommands = append(dst.Subcommands, sub)
		}
	}
	dst.Tables = sortedUnique(append(dst.Tables, src.Tables...))
	dst.ParseFlags = sortedUnique(append(dst.ParseFlags, src.ParseFlags...))
	dst.Adapters = sortedUnique(append(dst.Adapters, src.Adapters...))
	for _, group := range src.Exclusive {
		found := false
		for _, existing := range dst.Exclusive {
			if slices.Equal(existing, group) {
				found = true
				break
			}
		}
		if !found {
			dst.Exclusive = append(dst.Exclusive, group)
		}
	}
}

// Follow local helpers, table references, and externally defined option helpers.
// Subcommand function pointers are not calls: their tables stay on the child.
func (e *extractor) functionGrammar(f *sourceFile, name string, seen map[string]bool) registry.Grammar {
	g := registry.Grammar{Parser: "parse-options"}
	key := f.name + ":" + name
	if seen[key] {
		return g
	}
	seen[key] = true
	defer delete(seen, key)
	fn, ok := f.functions[name]
	if !ok {
		return g
	}
	for _, t := range f.tables {
		if t.owner == name || t.owner == "" && containsToken(fn.body, t.name) {
			mergeGrammar(&g, t.grammar)
		}
	}
	// Calls after parse_options execute the request; their option tables are
	// not part of this command's argv grammar.
	parseEnd := len(fn.body)
	for i, t := range fn.body {
		if t.text == "parse_options" || t.text == "parse_options_start" {
			if i+1 < len(fn.body) && fn.body[i+1].text == "(" {
				parseEnd = i
			}
		}
		if strings.HasPrefix(t.text, "die_for_incompatible_opt") && i+1 < len(fn.body) && fn.body[i+1].text == "(" {
			end, err := matching(fn.body, i+1)
			if err == nil {
				args, err := split(fn.body[i+2 : end])
				if err == nil {
					var group []string
					for j := 1; j < len(args); j += 2 {
						if s := literal(args[j]); strings.HasPrefix(s, "-") {
							group = append(group, s)
						}
					}
					if len(group) > 1 {
						g.Exclusive = append(g.Exclusive, group)
					}
				}
			}
		}
	}
	for i, t := range fn.body {
		if i > parseEnd {
			break
		}
		if i+1 >= len(fn.body) || fn.body[i+1].text != "(" {
			continue
		}
		if strings.HasPrefix(t.text, "parse_options") && t.text != "parse_options_concat" && t.text != "parse_options_dup" {
			end, err := matching(fn.body, i+1)
			if err == nil {
				args, err := expand(fn.body[i+2:end], e.macros, 0)
				if err == nil {
					fs, err := flags(args)
					if err == nil {
						g.ParseFlags = append(g.ParseFlags, fs...)
					}
				}
			}
		}
		if _, ok := f.functions[t.text]; ok {
			mergeGrammar(&g, e.functionGrammar(f, t.text, seen))
		} else if other := e.entrypoints[t.text]; other != nil && !strings.HasPrefix(t.text, "cmd_") && ownsOptions(other, t.text) {
			mergeGrammar(&g, e.functionGrammar(other, t.text, seen))
		} else if strings.HasPrefix(t.text, "add_") && strings.HasSuffix(t.text, "_options") {
			g.Adapters = append(g.Adapters, "helper:"+t.text)
		}
	}
	g.ParseFlags = sortedUnique(g.ParseFlags)
	g.Adapters = sortedUnique(g.Adapters)
	if slices.Contains(g.ParseFlags, "PARSE_OPT_SUBCOMMAND_OPTIONAL") {
		g.Adapters = sortedUnique(append(g.Adapters, "default-subcommand:"+f.name))
	}
	for i := range g.Subcommands {
		sub := &g.Subcommands[i]
		childSeen := make(map[string]bool, len(seen))
		for key, value := range seen {
			childSeen[key] = value
		}
		sub.Grammar = e.functionGrammar(f, sub.Entrypoint, childSeen)
		if len(sub.Tables) == 0 {
			sub.Parser = "manual:" + f.name
		}
	}
	resolveAliases(g.Options)
	return g
}

func ownsOptions(f *sourceFile, name string) bool {
	fn := f.functions[name]
	for _, t := range f.tables {
		if t.owner == name || t.owner == "" && containsToken(fn.body, t.name) {
			return true
		}
	}
	return false
}

func containsToken(ts []token, name string) bool {
	for _, t := range ts {
		if t.text == name {
			return true
		}
	}
	return false
}

func resolveAliases(options []registry.Option) {
	for i := range options {
		if options[i].Alias == "" {
			continue
		}
		for _, target := range options {
			if target.Long == options[i].Alias {
				options[i].Arity = target.Arity
				options[i].Negatable = target.Negatable
				options[i].Flags = sortedUnique(append(append([]string{}, target.Flags...), "PARSE_OPT_FROM_ALIAS"))
				options[i].Callback = target.Callback
				break
			}
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
