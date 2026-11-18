#!/usr/bin/env bash
# Refresh pomdock-owned shell/runtime assets without rebuilding pentest tools.
set -euo pipefail
if [[ $# != 2 ]]; then
    echo 'usage: update-runtime-image.sh BASE_IMAGE OUTPUT_IMAGE' >&2
    exit 2
fi
base_image=$1
output_image=$2
[[ $base_image =~ ^[A-Za-z0-9][A-Za-z0-9._:/@-]*$ ]] || { echo 'Invalid base image reference' >&2; exit 2; }
base_id=$(docker image inspect "$base_image" --format '{{.Id}}')
[[ $base_id =~ ^sha256:[a-f0-9]{64}$ ]] || { echo 'Could not resolve local base image ID' >&2; exit 1; }
root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
context_dir=$(mktemp -d)
trap 'rm -rf "$context_dir"' EXIT
mkdir -p "$context_dir/scripts/recording" "$context_dir/scripts/prompt"
for helper in pomdock-shell pomdock-tmux pomsession pomdock-help pomrdp pomdock-netcontext pomdock-exitip workspace-layout.py; do
    cp "$root_dir/scripts/$helper" "$context_dir/scripts/"
done
cp "$root_dir/scripts/recording/"*.sh "$root_dir/scripts/recording/"*.bash "$root_dir/scripts/recording/"*.zsh "$context_dir/scripts/recording/"
cp "$root_dir/scripts/prompt/"*.bash "$root_dir/scripts/prompt/"*.zsh "$root_dir/scripts/prompt/"*.py "$context_dir/scripts/prompt/"
printf 'FROM %s\n' "$base_id" > "$context_dir/Dockerfile"
cat >> "$context_dir/Dockerfile" <<'DOCKERFILE'
USER root
COPY scripts/pomdock-shell scripts/pomdock-tmux scripts/pomsession scripts/pomdock-help scripts/pomrdp scripts/pomdock-netcontext scripts/pomdock-exitip /usr/local/bin/
COPY scripts/workspace-layout.py /usr/local/lib/pomdock/workspace-layout.py
COPY scripts/recording/ /usr/local/lib/pomdock/recording/
COPY scripts/prompt/ /usr/local/lib/pomdock/prompt/
RUN chmod 0755 /usr/local/bin/pomdock-shell /usr/local/bin/pomdock-tmux /usr/local/bin/pomsession /usr/local/bin/pomdock-help /usr/local/bin/pomrdp /usr/local/bin/pomdock-netcontext /usr/local/bin/pomdock-exitip \
    && chmod -R a+rX /usr/local/lib/pomdock \
    && python3 /usr/local/lib/pomdock/workspace-layout.py kali \
    && python3 /usr/local/lib/pomdock/prompt/guard-dotfiles.py /home/kali/dotfiles/zsh/.zshrc /root/.zshrc \
    && ln -sfn pomsession /usr/local/bin/pdsession \
    && ln -sfn pomdock-help /usr/local/bin/help \
    && ln -sfn pomdock-help /usr/local/bin/pomhelp \
    && { grep -Fqx '. /usr/local/lib/pomdock/prompt/prompt.zsh' /etc/zsh/zshrc || printf '\n. /usr/local/lib/pomdock/prompt/prompt.zsh\n' >> /etc/zsh/zshrc; } \
    && { grep -Fqx '. /usr/local/lib/pomdock/prompt/prompt.bash' /etc/bash.bashrc || printf '\n. /usr/local/lib/pomdock/prompt/prompt.bash\n' >> /etc/bash.bashrc; }
LABEL io.pomdock.workspace="/home/kali/workspace" io.pomdock.prompt="local-tunnels-v1" io.pomdock.container-tmux="1"
USER kali
WORKDIR /home/kali/workspace
CMD ["zsh"]
DOCKERFILE
# Build with the classic (BuildKit-less) builder. The Dockerfile pins the exact
# local base image with `FROM <sha256:…>`, which buildx's default "docker" driver
# cannot resolve: it treats the bare digest as a registry reference and fails with
# "pull access denied". The classic builder reads it straight from the local image
# store. `--network none` keeps the added layer offline.
DOCKER_BUILDKIT=0 docker build -f "$context_dir/Dockerfile" -t "$output_image" --network none "$context_dir"
