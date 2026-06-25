#!/bin/bash
# test-build.sh — full build → test → teardown
#
# Usage:
#   ./test-build.sh            # build fresh, run tests, teardown
#   ./test-build.sh --no-build # skip build (use existing image)
#   ./test-build.sh --keep     # don't teardown on failure (inspect container)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE="pcm-kali-pentest:test"
CONTAINER="pcm-kali-pentest-test-$$"
DOTFILES_DIR="${HOME}/dotfiles"

NO_BUILD=false
KEEP_ON_FAIL=false

for arg in "$@"; do
    case "$arg" in
        --no-build)   NO_BUILD=true ;;
        --keep)       KEEP_ON_FAIL=true ;;
        -h|--help)
            echo "Usage: $(basename "$0") [--no-build] [--keep]"
            echo "  --no-build   skip docker build, use existing image"
            echo "  --keep       don't remove container on test failure"
            exit 0
            ;;
        *) echo "Unknown argument: $arg"; exit 1 ;;
    esac
done

GREEN='\033[0;32m'; RED='\033[0;31m'; BOLD='\033[1m'; RESET='\033[0m'
ok()   { echo -e "  ${GREEN}[+]${RESET} $*"; }
err()  { echo -e "  ${RED}[!]${RESET} $*"; }
info() { echo -e "  [*] $*"; }

teardown() {
    local exit_code=$?
    if docker inspect "$CONTAINER" &>/dev/null; then
        if [[ $exit_code -ne 0 && "$KEEP_ON_FAIL" == true ]]; then
            err "Tests failed. Container kept for inspection: $CONTAINER"
            err "  docker exec -it $CONTAINER zsh"
            err "  docker rm -f $CONTAINER  # when done"
        else
            info "Removing test container..."
            docker rm -f "$CONTAINER" >/dev/null
            ok "Teardown complete"
        fi
    fi
}

trap teardown EXIT

echo ""
echo -e "${BOLD}════════════════════════════════════════════════${RESET}"
echo -e "${BOLD}  kali-pentest — build + test${RESET}"
echo -e "${BOLD}════════════════════════════════════════════════${RESET}"
echo ""

# ── Build ────────────────────────────────────────────────────────

if [[ "$NO_BUILD" == false ]]; then
    info "Building image: $IMAGE"
    info "(this takes ~10 min on first run)"
    echo ""
    docker buildx build \
        -f "${SCRIPT_DIR}/Dockerfile" \
        --build-context dotfiles="${DOTFILES_DIR}" \
        -t "$IMAGE" \
        --load \
        "${SCRIPT_DIR}"
    echo ""
    ok "Image built: $IMAGE"
else
    if ! docker image inspect "$IMAGE" &>/dev/null; then
        err "Image $IMAGE not found and --no-build was set"
        exit 1
    fi
    info "Using existing image: $IMAGE"
fi

# ── Start container ──────────────────────────────────────────────

info "Starting test container: $CONTAINER"

docker run -d \
    --name "$CONTAINER" \
    "$IMAGE" \
    sleep infinity >/dev/null

ok "Container started"

# ── Run tests ────────────────────────────────────────────────────

echo ""
info "Running test.sh inside container..."
echo ""

docker cp "${SCRIPT_DIR}/test.sh" "$CONTAINER:/tmp/test.sh"
docker exec -i "$CONTAINER" bash /tmp/test.sh
TEST_EXIT=$?

echo ""
if [[ $TEST_EXIT -eq 0 ]]; then
    ok "All tests passed"
else
    err "Tests failed (exit $TEST_EXIT)"
    exit $TEST_EXIT
fi
