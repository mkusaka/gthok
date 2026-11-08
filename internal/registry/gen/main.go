// gen statically extracts Git's command inventory and option initializers.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/mkusaka/gthok/internal/registry"
)

const gitCommit = "a018953688f1b10bddf91bff8747068f5f4746a4"
const gitVersion = "2.56.0"
const generatorVersion = "1"

type extractor struct {
	macros      map[string]macro
	files       map[string]*sourceFile
	entrypoints map[string]*sourceFile
	commands    map[string]registry.Command
	grammars    map[string]registry.Grammar
	manifest    registry.ManifestInfo
}

func main() {
	src := flag.String("git-src", "", "pinned Git source directory")
	out := flag.String("out", "internal/registry/data", "output directory")
	verify := flag.Bool("verify", false, "cross-check the local pinned Git's completion helper (offline)")
	flag.Parse()
	if *src == "" {
		fmt.Fprintln(os.Stderr, "-git-src is required")
		os.Exit(2)
	}
	if err := generate(*src, *out, *verify); err != nil {
		fmt.Fprintln(os.Stderr, "registry:", err)
		os.Exit(1)
	}
}

func generate(src, out string, verify bool) error {
	cmd := exec.Command("git", "-C", src, "rev-parse", "HEAD")
	cmd.Env = isolatedEnv("")
	b, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("read source revision: %w", err)
	}
	if strings.TrimSpace(string(b)) != gitCommit {
		return fmt.Errorf("source must be Git v%s (%s), got %s", gitVersion, gitCommit, strings.TrimSpace(string(b)))
	}
	e := &extractor{files: map[string]*sourceFile{}, entrypoints: map[string]*sourceFile{}, commands: map[string]registry.Command{}, grammars: map[string]registry.Grammar{}, manifest: registry.ManifestInfo{Git: gitVersion, Tag: "v" + gitVersion, Commit: gitCommit, GeneratorVersion: generatorVersion, Exclusions: []registry.Exclusion{}}}
	contents := map[string]string{}
	macros := map[string]macro{}
	for _, pattern := range []string{"*.c", "*.h", "builtin/*.c", "builtin/*.h"} {
		paths, err := filepath.Glob(filepath.Join(src, pattern))
		if err != nil {
			return err
		}
		for _, path := range paths {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			name, _ := filepath.Rel(src, path)
			name = filepath.ToSlash(name)
			contents[name] = string(b)
			if err := readMacros(string(b), macros); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	e.macros = macros
	// Fail on new parse-option flags even if their first use is in a helper.
	flagHeader := contents["parse-options.h"]
	flagStart := strings.Index(flagHeader, "enum parse_opt_flags")
	flagEnd := strings.Index(flagHeader, "enum parse_opt_result")
	if flagStart < 0 || flagEnd < flagStart {
		return fmt.Errorf("parse-options.h flag enums not found")
	}
	header, err := lex(flagHeader[flagStart:flagEnd])
	if err != nil {
		return err
	}
	if _, err := flags(header); err != nil {
		return err
	}
	for _, name := range sortedKeys(contents) {
		if !strings.HasSuffix(name, ".c") {
			continue
		}
		f, err := parseSource(name, contents[name], macros)
		if err != nil {
			return err
		}
		e.files[name] = f
		for fn, definition := range f.functions {
			if !definition.static {
				e.entrypoints[fn] = f
			}
		}
		e.manifest.Coverage.Tables += len(f.tables)
		for _, t := range f.tables {
			e.manifest.Coverage.Options += len(t.grammar.Options)
		}
	}
	if err := e.inventory(src); err != nil {
		return err
	}
	for _, name := range sortedKeys(e.commands) {
		c := e.commands[name]
		g := registry.Grammar{}
		switch {
		case c.Implementation == "document":
			g.Parser = "document"
		case c.Implementation == "option-only":
			g.Parser = "global"
		case c.Builtin:
			f := e.entrypoints[c.Entrypoint]
			if f == nil {
				return fmt.Errorf("builtin %s has no source entrypoint %s", name, c.Entrypoint)
			}
			c.Source = registry.Source{File: f.name, Line: f.functions[c.Entrypoint].line, Entrypoint: c.Entrypoint}
			e.commands[name] = c
			g = e.functionGrammar(f, c.Entrypoint, map[string]bool{})
			if slices.Contains(c.Flags, "NO_PARSEOPT") || len(g.Tables) == 0 {
				g.Parser = "manual:" + f.name
			}
			if slices.Contains([]string{"log", "show", "whatchanged", "rev-list", "shortlog", "format-patch", "last-modified", "format-rev", "annotate", "blame", "pickaxe"}, name) {
				g.Parser = "revision"
			}
			if slices.Contains([]string{"diff", "diff-files", "diff-index", "diff-tree", "diff-pairs"}, name) {
				g.Parser = "diff"
			}
		default:
			g.Parser = "manual:" + c.Source.File
		}
		if g.Parser == "" {
			return fmt.Errorf("command %s is not covered by a table or adapter", name)
		}
		e.grammars[name] = g
		if g.Parser != "parse-options" {
			e.manifest.Coverage.Adapters++
			e.manifest.Exclusions = append(e.manifest.Exclusions, registry.Exclusion{Name: name, Reason: adapterReason(c, g)})
		}
		for _, adapter := range g.Adapters {
			e.manifest.Exclusions = append(e.manifest.Exclusions, registry.Exclusion{Name: name, Reason: "requires " + adapter + " adapter in addition to extracted tables"})
		}
	}
	globals, err := manualOptions(e.files["git.c"], "handle_options")
	if err != nil {
		return err
	}
	revisions, err := manualOptions(e.files["revision.c"], "handle_revision_opt", "handle_revision_pseudo_opt")
	if err != nil {
		return err
	}
	diff := registry.Grammar{Parser: "diff"}
	for _, t := range e.files["diff.c"].tables {
		mergeGrammar(&diff, t.grammar)
	}
	e.grammars["$global"] = registry.Grammar{Parser: "global", Options: globals}
	e.grammars["$revision"] = registry.Grammar{Parser: "revision", Options: revisions, Adapters: []string{"diff", "revision-number", "revision-pseudo-options"}}
	e.grammars["$diff"] = diff
	if err := e.manualGrammars(revisions); err != nil {
		return err
	}
	e.manifest.Coverage.Commands = len(e.commands)
	if verify {
		v, err := verifyCompletion(e.commands, e.grammars)
		if err != nil {
			return err
		}
		e.manifest.Verification = v
	}
	sourceMap := map[string]registry.Source{}
	for _, name := range sortedKeys(e.commands) {
		sourceMap[name] = e.commands[name].Source
	}
	for _, name := range sortedKeys(e.grammars) {
		addSources(sourceMap, name, e.grammars[name])
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	for _, item := range []struct {
		name  string
		value any
	}{{"commands", e.commands}, {"grammar", e.grammars}, {"source-map", sourceMap}, {"manifest", e.manifest}} {
		b, err := json.MarshalIndent(item.value, "", "  ")
		if err != nil {
			return err
		}
		b = append(b, '\n')
		if err := os.WriteFile(filepath.Join(out, item.name+".json"), b, 0644); err != nil {
			return err
		}
	}
	return nil
}

func adapterReason(c registry.Command, g registry.Grammar) string {
	switch c.Implementation {
	case "document":
		return "command-list documentation/interface entry, not an executable command"
	case "script":
		return "script parser requires " + g.Parser + " adapter"
	case "external":
		return "standalone executable requires " + g.Parser + " adapter"
	case "option-only":
		return "git.c option-only mode uses the global parser"
	}
	return "non-table or mixed parser requires " + g.Parser + " adapter"
}

func addSources(m map[string]registry.Source, name string, g registry.Grammar) {
	for _, o := range g.Options {
		if o.Long != "" {
			m[name+" --"+o.Long] = o.Source
		}
		if o.Short != "" {
			m[name+" -"+o.Short] = o.Source
		}
	}
	for _, sub := range g.Subcommands {
		m[name+" "+sub.Name] = sub.Source
		addSources(m, name+" "+sub.Name, sub.Grammar)
	}
}

var builtinRE = regexp.MustCompile(`\{\s*"([^"]+)"\s*,\s*(cmd_[A-Za-z_0-9]+)([^}]*)\}`)
var makeProgramRE = regexp.MustCompile(`^(SCRIPT_(?:SH|PERL|PYTHON|LIB)|PROGRAM_OBJS|EXTRA_PROGRAMS)\s*\+=\s*(\S+)`)

func (e *extractor) inventory(src string) error {
	gitSource, err := os.ReadFile(filepath.Join(src, "git.c"))
	if err != nil {
		return err
	}
	s := string(gitSource)
	start := strings.Index(s, "static struct cmd_struct commands[] = {")
	if start < 0 {
		return fmt.Errorf("git.c commands[] not found")
	}
	end := strings.Index(s[start:], "\n};") + start
	if end < start {
		return fmt.Errorf("git.c commands[] not found")
	}
	for _, loc := range builtinRE.FindAllStringSubmatchIndex(s[start:end], -1) {
		value := func(i int) string { return s[start+loc[i*2] : start+loc[i*2+1]] }
		name, entry := value(1), value(2)
		fs := strings.FieldsFunc(value(3), func(r rune) bool { return r == ',' || r == '|' || r == ' ' || r == '\t' || r == '\n' })
		e.commands[name] = registry.Command{Name: name, Builtin: true, Implementation: "builtin", Entrypoint: entry, Flags: fs, Source: registry.Source{File: "git.c", Line: 1 + strings.Count(s[:start+loc[0]], "\n"), Entrypoint: entry}}
		e.manifest.Coverage.Builtins++
	}
	list, err := os.Open(filepath.Join(src, "command-list.txt"))
	if err != nil {
		return err
	}
	defer func() { _ = list.Close() }()
	scanner := bufio.NewScanner(list)
	for line := 1; scanner.Scan(); line++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		name := strings.TrimPrefix(fields[0], "git-")
		c, exists := e.commands[name]
		if !exists {
			c = registry.Command{Name: name, Implementation: "external", Source: registry.Source{File: "command-list.txt", Line: line}}
		}
		c.Categories = fields[1:]
		if slices.Contains(fields[1:], "guide") || slices.Contains(fields[1:], "userinterfaces") || slices.Contains(fields[1:], "developerinterfaces") {
			c.Implementation = "document"
		}
		e.commands[name] = c
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	makefile, err := os.ReadFile(filepath.Join(src, "Makefile"))
	if err != nil {
		return err
	}
	for i, line := range strings.Split(string(makefile), "\n") {
		m := makeProgramRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil || strings.Contains(m[2], "$(") {
			continue
		}
		path := strings.TrimSuffix(m[2], "$X")
		if m[1] == "PROGRAM_OBJS" {
			path = strings.TrimSuffix(path, ".o") + ".c"
		}
		name := strings.TrimPrefix(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(path, ".c"), ".sh"), ".perl"), ".py"), "git-")
		c := e.commands[name]
		if c.Builtin {
			continue
		}
		c.Name = name
		c.Implementation = "external"
		if strings.HasPrefix(m[1], "SCRIPT_") {
			c.Implementation = "script"
		}
		c.Source = registry.Source{File: path, Line: 1}
		if _, err := os.Stat(filepath.Join(src, path)); err != nil {
			c.Source = registry.Source{File: "Makefile", Line: i + 1}
		}
		e.commands[name] = c
	}
	for _, name := range sortedKeys(e.commands) {
		c := e.commands[name]
		if c.Implementation != "external" {
			continue
		}
		if f := e.files[name+".c"]; f != nil {
			c.Source = registry.Source{File: f.name, Line: 1}
			if fn, ok := f.functions["cmd_main"]; ok {
				c.Entrypoint = fn.name
				c.Source.Line = fn.line
				c.Source.Entrypoint = fn.name
			}
		}
		for _, path := range []string{"git-" + name + ".sh", "git-" + name + ".perl", "git-" + name + ".py", "git-gui/git-gui.sh", "gitk-git/gitk", "gitweb/gitweb.perl"} {
			if (strings.HasPrefix(path, "git-gui/") && name != "gui" && name != "citool") || (strings.HasPrefix(path, "gitk-") && name != "gitk") || (strings.HasPrefix(path, "gitweb/") && name != "gitweb") {
				continue
			}
			if _, err := os.Stat(filepath.Join(src, path)); err == nil {
				c.Implementation = "script"
				c.Source = registry.Source{File: path, Line: 1}
				break
			}
		}
		e.commands[name] = c
	}
	// These aliases share remote-curl.c and are conditionally installed by Makefile.
	for _, name := range []string{"remote-http", "remote-https", "remote-ftp", "remote-ftps"} {
		e.commands[name] = registry.Command{Name: name, Implementation: "external", Source: registry.Source{File: "remote-curl.c", Line: 1}}
	}
	for _, name := range []string{"--help", "--version", "--exec-path", "--html-path", "--man-path", "--info-path", "--list-cmds"} {
		line := 1
		if i := strings.Index(s, "\""+name); i >= 0 {
			line += strings.Count(s[:i], "\n")
		}
		e.commands[name] = registry.Command{Name: name, Implementation: "option-only", Source: registry.Source{File: "git.c", Line: line, Entrypoint: "handle_options"}}
	}
	return nil
}
