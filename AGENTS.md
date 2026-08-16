# Repository Guidelines

## Project Structure & Module Organization

`cli/` contains the Go 1.24 Cobra CLI and Bubble Tea TUI. Commands delegate Docker work to `pentest.sh`, Kali VM work to `kali-vm/`, and Linux/Windows guest creation to `vm-profiles/`. Image provisioning lives in `Dockerfile` and `setup-pentest.sh`; the custom Tor sidecar is in `tor-gateway/`. Root-level `test*.sh` scripts provide integration checks. `scripts/` holds build helpers. `vendor/atuin/` is patched upstream source; avoid unrelated edits there.

## Build, Test, and Development Commands

- `cd cli && make build`: tidy Go modules and build `cli/pomdock`.
- `cd cli && go test ./...`: compile and run any Go package tests.
- `cd cli && go vet ./...`: run standard Go static checks.
- `./test-build.sh`: build the Kali image, verify installed tools, and tear down the test container.
- `./test-network.sh`: validate plain Docker networking; add `--vpn FILE`, `--whonix`, or `--vm NAME` for affected modes.
- `bash -n pentest.sh setup-pentest.sh test*.sh kali-vm/*.sh vm-profiles/*.sh`: syntax-check shell changes.

Docker tests require a working daemon. VM tests require libvirt and a running VM; Whonix modes may start supporting images.

## Coding Style & Naming Conventions

Format Go files with `gofmt`; use tabs, PascalCase for exported identifiers, and camelCase internally. Keep Cobra commands grouped by feature. Shell scripts target Bash: quote expansions, prefer `set -euo pipefail` where compatible, use uppercase configuration constants, and lowercase functions and locals. Name scripts by action in kebab case, such as `build-atuin.sh`.

## Testing Guidelines

Add colocated `*_test.go` files for new Go logic, following the table-driven patterns in `cli/main_test.go` and `cli/tui_test.go`. Run narrow checks first, then the integration script for every network mode changed. Tests must clean up containers by default; use `--keep` or `--no-teardown` only for diagnosis. Compare egress-IP and DNS warnings with the expectations in `README.md`.

## Commit & Pull Request Guidelines

Recent history favors short, imperative subjects, commonly `feat:` or `fix:` (for example, `fix: stale sidecar ID on restart`). Keep each commit scoped and mention the affected backend when helpful. Pull requests should explain behavior and risk, list commands run, link relevant issues, and include terminal output or TUI screenshots for user-visible changes.

## Security & Configuration

Do not commit VPN profiles, SSH keys, engagement data, credentials, or generated binaries. Use local paths such as `~/.ssh/kali` and `PENTEST_DOTFILES_DIR`; redact public IPs and provider details from logs shared in reviews.
