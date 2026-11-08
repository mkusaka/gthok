# Lint notes

Run `make lint` before finishing a change. It installs the CI-pinned golangci-lint
version with the local Go toolchain, then analyzes ./.... Run gofmt first and fix
findings only within the files owned by the task; report unrelated findings to
the responsible owner rather than expanding scope.

Also run `gofmt -l .` (empty), `go build ./...`, `go vet ./...` and focused plus
full `go test ./...` when the tree builds. Generated schemas/registry JSON need
byte-identical regeneration tests. Lint is not a replacement for those checks.
