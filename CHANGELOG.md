# Changelog

All notable changes to gthok. Add entries under **Unreleased**;
`scripts/release.sh` turns that section into the next version's release notes.

## [Unreleased]

## [v0.0.1] - 2026-11-09

### Added

- Initial gthok 0.0.1: a transparent Git wrapper that normalizes Git operations
  before applying declarative policy and trusted pre/post hooks.
- Management commands for normalization, explanation, hooks, configuration,
  shims, diagnostics, and offline documentation.
- Linux and macOS releases for amd64 and arm64, SHA-256 checksums, build
  provenance, Linux deb/rpm packages, a Homebrew formula, and a GitHub Action.
- Installable through mise's GitHub backend: `mise use github:mkusaka/gthok`.

### Documentation and contracts

- Replaced inherited documentation and examples with Git operation, matcher,
  trust, hook, transform, delegate and local-resolution guidance.
- Listed every registered Git action with example routes/scopes; added public
  Draft 2020-12 schemas and checked-in regeneration checks.
- Added tested protected-force-push, history-audit and commit-trailer examples.
- Recognized fetch-pack hand options, safe reflog display options and bisect
  start/terms options while preserving arbitrary bisect-run child arguments.

[Unreleased]: https://github.com/mkusaka/gthok/compare/v0.0.1...HEAD
[v0.0.1]: https://github.com/mkusaka/gthok/releases/tag/v0.0.1
