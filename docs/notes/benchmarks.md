# Benchmark notes

Run local microbenchmarks with `make bench` (`go test -run '^$' -bench . -benchmem ./...`).
Numbers describe that machine/build/input only, not a latency guarantee.

Separate no-config exec passthrough, configured pure normalization, local resolver
queries, hook scheduling, stdin replay and process supervision. Never compare a
networking Git request with offline syntax parsing as if they did equal work.
Keep fixtures credential-free, deterministic and local. Do not claim measured
results without recording the exact command, environment and observed output.
