//go:build unix

package cli

import (
	"fmt"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize/actions"
	gitnormalize "github.com/mkusaka/gthok/internal/normalize/git"
	"github.com/mkusaka/gthok/internal/schema"
	"github.com/spf13/cobra"
)

type managementActionFamily = schema.ActionFamily

type managementActionEntry = schema.ActionEntry

func managementActionsCommand(code *int) *cobra.Command {
	var asJSON bool
	cmd := managementCommand("actions [glob]", "List Git actions, effects, example scopes and command routes", code, func(_ *cobra.Command, args []string) int {
		prefix := ""
		if len(args) != 0 {
			prefix = args[0]
		}
		families := managementActionsCatalogue(prefix)
		if asJSON {
			return managementJSON(families, nil)
		}
		if _, err := fmt.Fprint(os.Stdout, managementActionsText(families)); err != nil {
			return managementFailure(err)
		}
		return 0
	})
	cmd.Args = cobra.MaximumNArgs(1)
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func managementActionsCatalogue(prefix string) []managementActionFamily {
	routes := gitnormalize.ActionRoutes()
	byFamily := make(map[string][]managementActionEntry)
	for _, spec := range actions.All() {
		glob, _ := path.Match(prefix, spec.Action)
		if !strings.HasPrefix(spec.Action, prefix) && !glob {
			continue
		}
		byFamily[spec.Family] = append(byFamily[spec.Family], managementActionEntry{
			Action: spec.Action, Effect: spec.Effect,
			Scopes: managementActionScopes(spec, routes[spec.Action]),
			Git:    managementActionRoutes(routes[spec.Action]),
		})
	}
	families := make([]string, 0, len(byFamily))
	for family := range byFamily {
		families = append(families, family)
	}
	slices.Sort(families)
	catalogue := make([]managementActionFamily, 0, len(families))
	for _, family := range families {
		catalogue = append(catalogue, managementActionFamily{Family: family, Actions: byFamily[family]})
	}
	return catalogue
}

// Scopes describe the examples, not a safety guarantee for every command mode.
func managementActionScopes(spec actions.Spec, routes []string) []model.Scope {
	scopes := []model.Scope{}
	for _, route := range routes {
		result := gitnormalize.Normalize(model.Invocation{Argv: strings.Fields(route)})
		for _, op := range result.Operations {
			if op.Action == spec.Action {
				scopes = append(scopes, op.Scopes...)
			}
		}
	}
	// Transaction/symref verification is selected by stdin, not these argv.
	if spec.Family == "ref.transaction" || spec.Action == "ref.verify" || spec.Action == "ref.symbolic.verify" {
		scopes = append(scopes, model.ScopeLocalRef)
	}
	slices.Sort(scopes)
	return slices.Compact(scopes)
}

func managementActionRoutes(routes []string) []string {
	if routes == nil {
		return []string{}
	}
	slices.Sort(routes)
	return slices.Compact(routes)
}

func managementActionsText(families []managementActionFamily) string {
	var text strings.Builder
	for _, family := range families {
		fmt.Fprintln(&text, family.Family)
		for _, action := range family.Actions {
			fmt.Fprintf(&text, "  %s (%s; scopes: %v)\n", action.Action, action.Effect, action.Scopes)
			for _, route := range action.Git {
				fmt.Fprintf(&text, "    git: %s\n", route)
			}
		}
	}
	return text.String()
}
