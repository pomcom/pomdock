#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:?usage: build-image.sh IMAGE [DOTFILES_DIR]}"
DOTFILES_DIR="${2:-${PENTEST_DOTFILES_DIR:-${HOME}/pcm.dot}}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_CONTEXT="$(mktemp -d)"

cleanup() {
    rm -rf "${BUILD_CONTEXT}"
}
trap cleanup EXIT

mkdir -p "${DOTFILES_DIR}" "${BUILD_CONTEXT}/.pomdock-dotfiles"

# Stage a self-contained context so builds do not depend on Buildx named contexts.
tar -C "${ROOT_DIR}" \
    --exclude=.git \
    --exclude=.pomdock-dotfiles \
    --exclude=cli/pomdock \
    --exclude=vendor/atuin/target \
    -cf - . | tar -C "${BUILD_CONTEXT}" -xf -

tar -C "${DOTFILES_DIR}" \
    --exclude=.git \
    --exclude=docs-server/node_modules \
    --exclude='tools/*.jar' \
    --exclude='*.zip' \
    -cf - . \
    | tar -C "${BUILD_CONTEXT}/.pomdock-dotfiles" -xf -

if docker buildx version >/dev/null 2>&1; then
    docker buildx build \
        -f "${BUILD_CONTEXT}/Dockerfile" \
        -t "${IMAGE}" \
        --load \
        "${BUILD_CONTEXT}"
else
    echo "  [*] Docker Buildx not found; using the legacy builder"
    DOCKER_BUILDKIT=0 docker build \
        -f "${BUILD_CONTEXT}/Dockerfile" \
        -t "${IMAGE}" \
        "${BUILD_CONTEXT}"
fi
