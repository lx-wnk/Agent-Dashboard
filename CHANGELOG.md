# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

From the first tagged release onward, release notes are generated automatically
from [Conventional Commits](https://www.conventionalcommits.org/) by GoReleaser.

## [Unreleased]

Preparing the first public release.

### Added

- Lean, front-door `README.md` and a structured `docs/` tree (configuration, MCP,
  agent control, security, statusline, architecture overview).
- Release tooling: GoReleaser config + `release` workflow producing cross-compiled
  binaries (macOS/Linux, amd64/arm64) with the SPA embedded, plus an `install.sh`
  one-liner installer.
- `agent-dashboard --version` (version injected at build time via ldflags).
- `task setup`, `task doctor`, `task build:frontend`, and `task build:all` to lower
  the contributor entry barrier.
- Community health files: `SECURITY.md`, `CODE_OF_CONDUCT.md`, Dependabot config,
  and `FUNDING.yml`.

### Fixed

- Production build now embeds the real Vue SPA. `vite build` writes to
  `server/frontend/dist` (the `go:embed` source); previously it emitted to the
  repo-root `./dist`, so `task build` silently shipped the placeholder frontend.

[Unreleased]: https://github.com/lx-wnk/Agent-Dashboard/commits/main
