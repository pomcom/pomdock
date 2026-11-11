#!/usr/bin/env bash
set -euo pipefail

usage() {
    echo 'usage: build-image.sh IMAGE [DOTFILES_DIR] [--toolset full|minimal] [--fresh]'
}
[[ $# -gt 0 ]] || { usage >&2; exit 2; }
if [[ "$1" == -h || "$1" == --help ]]; then usage; exit 0; fi
image="$1"
shift
dotfiles_dir="${PENTEST_DOTFILES_DIR:-${HOME}/pcm.dot}"
if [[ $# -gt 0 && "$1" != --* ]]; then
    dotfiles_dir="$1"
    shift
fi
toolset=full
fresh=()
while [[ $# -gt 0 ]]; do
    case "$1" in
        --toolset)
            [[ $# -gt 1 ]] || { usage >&2; exit 2; }
            toolset="$2"; shift 2 ;;
        --fresh) fresh=(--no-cache); shift ;;
        -h|--help) usage; exit 0 ;;
        *) printf 'Unknown build option: %s\n' "$1" >&2; usage >&2; exit 2 ;;
    esac
done
case "$toolset" in full|minimal) ;; *) echo 'Toolset must be full or minimal' >&2; exit 2 ;; esac
root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
build_context="$(mktemp -d)"
trap 'rm -rf "$build_context"' EXIT
mkdir -p "$build_context/.pomdock-dotfiles" "$build_context/scripts/recording" "$build_context/scripts/prompt" "$build_context/vendor/atuin"

# Deliberately allowlist inputs: never send the checkout, client data, binaries,
# reference clones, frontend dependencies or Rust build caches to the daemon.
cp "$root_dir/Dockerfile" "$root_dir/setup-pentest.sh" "$build_context/"
for helper in pomdock-shell pomdock-tmux pomsession pomdock-help pomrdp pomdock-netcontext pomdock-exitip workspace-layout.py; do
    cp "$root_dir/scripts/$helper" "$build_context/scripts/"
done
cp "$root_dir/scripts/recording/"*.sh "$root_dir/scripts/recording/"*.bash \
    "$root_dir/scripts/recording/"*.zsh "$build_context/scripts/recording/"
cp "$root_dir/scripts/prompt/"*.bash "$root_dir/scripts/prompt/"*.zsh "$root_dir/scripts/prompt/"*.py "$build_context/scripts/prompt/"
bash "$root_dir/scripts/stage-atuin.sh" "$root_dir/vendor/atuin" "$build_context/vendor/atuin"

# Personal shell setup is optional. Only known configuration components belong
# in an image; missing dotfiles never creates or modifies the user's directory.
if [[ -d "$dotfiles_dir" ]]; then
    dotfile_entries=()
    for entry in setup-shell.sh zsh starship atuin/.config tmux nvim git bat xxh; do
        [[ ! -e "$dotfiles_dir/$entry" ]] || dotfile_entries+=("$entry")
    done
    if [[ ${#dotfile_entries[@]} -gt 0 ]]; then
        tar -C "$dotfiles_dir" --exclude=.git --exclude=node_modules --exclude=target \
            --exclude=.cache --exclude=__pycache__ --exclude='*.zip' --exclude='.env*' \
            --exclude='*.key' --exclude='*.pem' --exclude='*.ovpn' \
            -cf - "${dotfile_entries[@]}" | tar -C "$build_context/.pomdock-dotfiles" -xf -
    fi
fi
revision="$(git -C "$root_dir" rev-parse HEAD 2>/dev/null || printf unknown)"
built_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
build_args=( -f "$build_context/Dockerfile" -t "$image" --build-arg "TOOLSET=$toolset"
    --label "org.opencontainers.image.revision=$revision"
    --label "org.opencontainers.image.created=$built_at"
    --label "io.pomdock.toolset=$toolset"
    --label 'io.pomdock.recording-format=3' )
# An explicit build-only override can use the host resolver when bridge DNS is
# unavailable. Never change the daemon or application/container network defaults.
if [[ -n "${POMDOCK_BUILD_NETWORK:-}" ]]; then
    build_args+=(--network "$POMDOCK_BUILD_NETWORK")
fi
if docker buildx version >/dev/null 2>&1; then
    docker buildx build "${build_args[@]}" "${fresh[@]}" --load "$build_context"
else
    echo '  [*] Docker Buildx not found; using the legacy builder'
    DOCKER_BUILDKIT=0 docker build "${build_args[@]}" "${fresh[@]}" "$build_context"
fi
