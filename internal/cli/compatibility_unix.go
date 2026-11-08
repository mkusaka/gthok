//go:build unix

package cli

import (
	"fmt"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/install"
	"github.com/mkusaka/gthok/internal/model"
)

// A snapshot belongs to one pipeline invocation, including transform re-analysis.
type gitCompatibility struct {
	status model.Compatibility
	mode   string
	detail string
}

func checkGitCompatibility(c *config.Config, real string, env map[string]string) gitCompatibility {
	gate := gitCompatibility{status: model.CompatibilityUnverified, mode: c.Runtime.GitCompatibility}
	version, err := install.GitVersion(real, environmentList(env))
	if err != nil {
		gate.detail = "real Git version unavailable: " + err.Error()
	} else if version == install.GitBaseline {
		gate.status = model.CompatibilityVerified
	} else {
		gate.detail = fmt.Sprintf("real Git version %s differs from verified %s", version, install.GitBaseline)
	}
	return gate
}

// Return a view without mutating the syntax result used for transform validation.
func (g gitCompatibility) apply(result model.Result) model.Result {
	result.Registry.Compatibility = g.status
	if g.status != model.CompatibilityUnverified {
		return result
	}
	result.Classification.MutationFree, result.Classification.RetrySafe = false, false
	if result.Classification.Coverage == model.CoverageSemantic {
		result.Classification.Coverage = model.CoverageConservative
	}
	if result.Classification.Resolution == model.ResolutionComplete {
		result.Classification.Resolution = model.ResolutionPartial
	}
	result.Operations = append([]model.Operation(nil), result.Operations...)
	for i := range result.Operations {
		op := &result.Operations[i]
		if op.Coverage == model.CoverageSemantic {
			op.Coverage = model.CoverageConservative
		}
		op.Unresolved = append(append([]model.Unresolved(nil), op.Unresolved...), model.Unresolved{Field: "registry.compatibility", Reason: g.detail, Source: "git-version"})
		if op.Evidence != "" {
			op.Evidence += "; "
		}
		op.Evidence += g.detail
		if g.mode == "verified-only" {
			op.Family, op.Action, op.Effect = model.FamilyUnknown, model.FamilyUnknown, model.EffectUnknown
			op.Coverage, op.UnknownReason = model.CoverageOpaque, model.UnknownUnverified
			if op.Execution == model.ExecutionSkipped || op.Execution == model.ExecutionNotSelected {
				op.Execution = model.ExecutionMaybe
			}
			// Syntax-derived targets cannot prove exclusions on another Git version.
			op.Scopes, op.Capabilities, op.Parameters, op.Texts, op.Editable = nil, nil, nil, nil, nil
			op.Target = model.Target{Refs: model.UnknownSet(), Paths: model.UnknownSet()}
		}
	}
	if g.mode == "verified-only" {
		result.Classification.Confidence = model.ConfidenceNone
		result.Classification.Coverage, result.Classification.Resolution = model.CoverageOpaque, model.ResolutionUnresolved
	}
	return result
}
