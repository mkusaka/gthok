package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

type token struct {
	text string
	line int
	kind string // outermost option macro, retained through expansion
}

type macro struct {
	params   []string
	body     []token
	function bool
}

// lex keeps string/character literals intact and ignores comments/directives.
func lex(s string) ([]token, error) {
	var out []token
	line := 1
	for i := 0; i < len(s); {
		c := s[i]
		if c == '\n' {
			line++
			i++
			continue
		}
		if unicode.IsSpace(rune(c)) {
			i++
			continue
		}
		if c == '#' {
			for i < len(s) {
				if s[i] == '\n' {
					line++
					if i == 0 || s[i-1] != '\\' {
						i++
						break
					}
				}
				i++
			}
			continue
		}
		if i+1 < len(s) && s[i:i+2] == "//" {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(s) && s[i:i+2] == "/*" {
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return nil, fmt.Errorf("line %d: unterminated comment", line)
			}
			end += i + 4
			line += strings.Count(s[i:end], "\n")
			i = end
			continue
		}
		start, ln := i, line
		if c == '"' || c == '\'' {
			i++
			for i < len(s) && s[i] != c {
				if s[i] == '\\' {
					i++
				}
				i++
			}
			if i >= len(s) {
				return nil, fmt.Errorf("line %d: unterminated literal", ln)
			}
			i++
		} else if identByte(c) || c >= '0' && c <= '9' {
			i++
			for i < len(s) && (identByte(s[i]) || s[i] >= '0' && s[i] <= '9') {
				i++
			}
		} else {
			i++
		}
		out = append(out, token{text: s[start:i], line: ln})
	}
	return out, nil
}

func identByte(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func matching(ts []token, start int) (int, error) {
	pairs := map[string]string{"(": ")", "[": "]", "{": "}"}
	end, ok := pairs[ts[start].text]
	if !ok {
		return 0, fmt.Errorf("line %d: not an opening delimiter", ts[start].line)
	}
	for i := start + 1; i < len(ts); i++ {
		if ts[i].text == end {
			return i, nil
		}
		if _, ok := pairs[ts[i].text]; ok {
			j, err := matching(ts, i)
			if err != nil {
				return 0, err
			}
			i = j
		}
	}
	return 0, fmt.Errorf("line %d: unclosed %s", ts[start].line, ts[start].text)
}

func split(ts []token) ([][]token, error) {
	var out [][]token
	start := 0
	for i := 0; i < len(ts); i++ {
		switch ts[i].text {
		case "(", "[", "{":
			j, err := matching(ts, i)
			if err != nil {
				return nil, err
			}
			i = j
		case ",":
			out = append(out, ts[start:i])
			start = i + 1
		}
	}
	if start < len(ts) {
		out = append(out, ts[start:])
	}
	return out, nil
}

var defineRE = regexp.MustCompile(`^\s*#\s*define\s+([A-Za-z_][A-Za-z_0-9]*)(.*)$`)

func readMacros(s string, macros map[string]macro) error {
	s = strings.ReplaceAll(s, "\\\n", " ")
	for _, line := range strings.Split(s, "\n") {
		m := defineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		rest := m[2]
		def := macro{}
		if strings.HasPrefix(rest, "(") {
			end := strings.IndexByte(rest, ')')
			if end < 0 {
				return fmt.Errorf("invalid macro %s", m[1])
			}
			def.function = true
			if args := strings.TrimSpace(rest[1:end]); args != "" {
				for _, arg := range strings.Split(args, ",") {
					def.params = append(def.params, strings.TrimSpace(arg))
				}
			}
			rest = rest[end+1:]
		}
		// Only option constructors and option/parse-flag bundles are expanded.
		if !strings.Contains(rest, "OPT_") && !strings.Contains(rest, "OPTION_") && !strings.HasPrefix(m[1], "OPT_") && !strings.HasPrefix(strings.TrimSpace(rest), "\"") {
			continue
		}
		var err error
		def.body, err = lex(rest)
		if err != nil {
			return err
		}
		macros[m[1]] = def
	}
	return nil
}

func expand(ts []token, macros map[string]macro, depth int) ([]token, error) {
	if depth > 30 {
		return nil, fmt.Errorf("recursive option macro")
	}
	var out []token
	for i := 0; i < len(ts); i++ {
		t := ts[i]
		m, ok := macros[t.text]
		if !ok {
			out = append(out, t)
			continue
		}
		args := map[string][]token{}
		if m.function {
			if i+1 >= len(ts) || ts[i+1].text != "(" {
				out = append(out, t)
				continue
			}
			end, err := matching(ts, i+1)
			if err != nil {
				return nil, err
			}
			parts, err := split(ts[i+2 : end])
			if err != nil {
				return nil, err
			}
			if len(parts) != len(m.params) {
				return nil, fmt.Errorf("line %d: %s takes %d arguments, got %d", t.line, t.text, len(m.params), len(parts))
			}
			for j, p := range m.params {
				args[p] = parts[j]
			}
			i = end
		}
		kind := t.kind
		if kind == "" {
			kind = t.text
		}
		var body []token
		for _, b := range m.body {
			if a, ok := args[b.text]; ok {
				for _, x := range a {
					x.line = t.line
					x.kind = kind
					body = append(body, x)
				}
			} else {
				b.line = t.line
				b.kind = kind
				body = append(body, b)
			}
		}
		expanded, err := expand(body, macros, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, expanded...)
	}
	return out, nil
}

func text(ts []token) string {
	var b strings.Builder
	for _, t := range ts {
		b.WriteString(t.text)
	}
	return b.String()
}

func literal(ts []token) string {
	var value strings.Builder
	for _, t := range ts {
		if strings.HasPrefix(t.text, "\"") || strings.HasPrefix(t.text, "'") {
			s, err := strconv.Unquote(t.text)
			if err == nil {
				value.WriteString(s)
			}
		}
	}
	return value.String()
}

func symbol(ts []token) string {
	for _, t := range ts {
		if len(t.text) > 0 && identByte(t.text[0]) && t.text != "NULL" {
			return t.text
		}
	}
	return ""
}
