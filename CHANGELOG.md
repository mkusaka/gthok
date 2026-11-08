# Changelog

All notable changes to gthok. Add entries under **Unreleased**;
`scripts/release.sh` turns that section into the next version's release notes.

## [Unreleased]

### Added

- Initial gthok 0.0.1: a transparent Git wrapper that normalizes Git operations
  before applying declarative policy and trusted pre/post hooks.
- Management commands for normalization, explanation, hooks, configuration,
  shims, diagnostics, and offline documentation.
- Linux and macOS releases for amd64 and arm64, SHA-256 checksums, build
  provenance, Linux deb/rpm packages, a Homebrew formula, and a GitHub Action.
- Installable through mise's GitHub backend: `mise use github:mkusaka/gthok`.

[Unreleased]: https://github.com/mkusaka/gthok/commits/main
