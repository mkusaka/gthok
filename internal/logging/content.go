package logging

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/redact"
)

func contentSummary(s string) string { return fmt.Sprintf("[content: %d bytes]", len(s)) }
func minimizeTexts(original, safe *model.Texts) {
	if original == nil {
		return
	}
	for _, pair := range [3][2]*model.Text{{original.Title, safe.Title}, {original.Body, safe.Body}, {original.Notes, safe.Notes}} {
		if pair[0] != nil {
			pair[1].Value = contentSummary(pair[0].Value)
		}
	}
}
func contentKey(key string) string {
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", ""), "_", ""))
}
func proseKey(key string) bool {
	switch contentKey(key) {
	case "body", "title", "message", "description", "subject", "comment", "notes":
		return true
	}
	return false
}
func minimizeString(s, key string) string {
	if proseKey(key) || len(s) > 64 || strings.ContainsFunc(s, unicode.IsSpace) {
		return contentSummary(s)
	}
	return s
}

// Minimize only a redacted copy; hooks and execution retain the original bytes.
func minimizeValue(r *redact.Redactor, original, safe any, key string) any {
	if _, ok := original.(json.Number); ok {
		return safe
	}
	value := reflect.ValueOf(original)
	if !value.IsValid() {
		return safe
	}
	if _, masked := safe.(string); masked && value.Kind() != reflect.String {
		return safe
	}
	switch value.Kind() {
	case reflect.String:
		if minimized := minimizeString(value.String(), key); minimized != value.String() {
			return r.String(minimized)
		}
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return safe
		}
		iter := value.MapRange()
		for iter.Next() {
			name, item := iter.Key().String(), iter.Value().Interface()
			switch out := safe.(type) {
			case map[string]any:
				out[r.String(name)] = minimizeValue(r, item, out[r.String(name)], name)
			case map[string]string:
				out[r.String(name)] = minimizeValue(r, item, out[r.String(name)], name).(string)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range value.Len() {
			switch out := safe.(type) {
			case []any:
				out[i] = minimizeValue(r, value.Index(i).Interface(), out[i], key)
			case []string:
				out[i] = minimizeValue(r, value.Index(i).Interface(), out[i], key).(string)
			}
		}
	}
	return safe
}

func minimizeArgv(r *redact.Redactor, argv []string) []string {
	out := r.Argv(argv)
	flags := true
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--" {
			flags = false
			continue
		}
		if !flags {
			continue
		}
		key, value, attached := strings.Cut(arg, "=")
		if key == "-m" || key == "--message" {
			if attached {
				out[i] = key + "=" + contentSummary(value)
			} else if i+1 < len(argv) {
				i++
				out[i] = contentSummary(argv[i])
			}
		} else if strings.HasPrefix(arg, "-m") && !strings.HasPrefix(arg, "--") {
			out[i] = "-m" + contentSummary(arg[2:])
		}
	}
	return out
}
