#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ATUIN_SRC="${ROOT_DIR}/vendor/atuin"
OUT_BIN="${1:-${HOME}/.atuin/bin/atuin}"

if [[ ! -d "${ATUIN_SRC}" ]]; then
    echo "vendor/atuin not found at ${ATUIN_SRC}" >&2
    exit 1
fi

mkdir -p "$(dirname "${OUT_BIN}")"

build_from_source() {
    command -v cargo >/dev/null 2>&1 || return 1
    (
        cd "${ATUIN_SRC}"
        cargo build --release --locked -p atuin
    )
}

build_from_image() {
    command -v docker >/dev/null 2>&1 || return 1
    docker image inspect pcm-kali-pentest >/dev/null 2>&1 || return 1

    local cid=""
    cid="$(docker create pcm-kali-pentest)"
    trap '[[ -n "${cid}" ]] && docker rm -f "${cid}" >/dev/null 2>&1 || true' EXIT
    docker cp "${cid}:/home/kali/.atuin/bin/atuin" "${OUT_BIN}"
    docker rm -f "${cid}" >/dev/null 2>&1 || true
    trap - EXIT
}

if build_from_source; then
    install -m 0755 "${ATUIN_SRC}/target/release/atuin" "${OUT_BIN}"
elif build_from_image; then
    chmod 0755 "${OUT_BIN}"
else
    echo "Failed to build atuin locally and could not extract it from pcm-kali-pentest" >&2
    exit 1
fi

if [[ "${OUT_BIN}" == "${HOME}/.atuin/bin/atuin" ]]; then
    printf 'export PATH="$HOME/.atuin/bin:$PATH"\n' > "${HOME}/.atuin/bin/env"
fi
echo "Installed patched atuin to ${OUT_BIN}"
