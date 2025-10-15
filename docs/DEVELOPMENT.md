# Leger Development Guide

## Repository Structure

```
leger/
├── cmd/
│   ├── leger/          # CLI (Leger-specific)
│   └── legerd/         # Daemon (setec fork)
├── internal/           # Leger-specific internals
├── client/             # setec client library (upstream)
├── server/             # setec server (upstream)
├── db/                 # setec database (upstream)
├── acl/                # setec ACL (upstream)
├── audit/              # setec audit (upstream)
├── types/              # setec types (upstream)
└── setectest/          # setec testing (upstream)
```

## Building

```bash
# Build both binaries
make build

# Build individually
make build-leger
make build-legerd

# Development build with version
make dev
```

## Testing

```bash
# All tests
make test

# Specific packages
go test ./internal/...
go test ./client/...

# With coverage
go test -coverprofile=coverage.txt ./...
```

## Attribution

- `cmd/legerd/` and all `*ec*` packages: BSD-3-Clause (Tailscale)
- `cmd/leger/` and `internal/`: Apache-2.0 (Leger Labs, Inc.)

See [NOTICE](../NOTICE) for full attribution.
