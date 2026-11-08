# Hooks and policy implementation notes

Tri-state criteria are AND on one operation/target. Definite exclusions apply
only to that operation. Maybe-deny applies conservatively; maybe-hook selects;
maybe-transform/delegate never selects. Match refs as full names and paths as
repository-relative globs, never refspec/pathspec syntax.

Keep hook response decision separate from execution outcome/effective decision.
Continue-on-error converts the effective pre deny to abstain without erasing the
actual outcome. Post hooks are diagnostic and preserve Git status. Join started
background hooks, bound timeouts, and retain dependency results.
See [policy](../policy-and-matchers.md) and [protocol](../hook-protocol.md).
