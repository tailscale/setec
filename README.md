# Leger - Podman Quadlet Manager with Secrets

[![CI](https://github.com/leger-labs/leger/actions/workflows/ci.yml/badge.svg)](https://github.com/leger-labs/leger/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

Leger manages Podman Quadlets from Git repositories with integrated secrets management via legerd (based on Tailscale's setec).

## Components

- **`leger`** - CLI for managing Podman Quadlets
- **`legerd`** - Secrets management daemon (fork of [tailscale/setec](https://github.com/tailscale/setec))

## Status

🚧 **Pre-release** - Active development towards v0.1.0

## Installation

Coming soon - RPM packages for Fedora 42+

## Architecture

- **Authentication:** Tailscale identity
- **Networking:** Tailscale MagicDNS
- **Secrets:** legerd (setec fork)
- **Containers:** Podman Quadlets (systemd integration)

## Attribution

legerd is a fork of [setec](https://github.com/tailscale/setec) by Tailscale Inc.
See [NOTICE](NOTICE) and [LICENSE.setec](LICENSE.setec) for full attribution.

## License

- Leger components: Apache License 2.0
- legerd (setec fork): BSD-3-Clause (see LICENSE.setec)

## Development

```bash
# Build both binaries
make build

# Run tests
make test

# Build RPM
make rpm
```

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for details.
