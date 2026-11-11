#!/usr/bin/env bash
# Copy only the vendored Cargo workspace needed to compile the patched client.
set -euo pipefail
source_dir="${1:?usage: stage-atuin.sh SOURCE DESTINATION}"
destination="${2:?usage: stage-atuin.sh SOURCE DESTINATION}"
mkdir -p "$destination"
# Crate resource symlinks refer back to these workspace-level files. Preserve
# their targets as well as the complete crates tree, including extensionless data.
source_entries=(Cargo.toml Cargo.lock README.md LICENSE CONTRIBUTORS CHANGELOG.md rust-toolchain.toml crates)
for entry in "${source_entries[@]}"; do
    if [[ ! -e "$source_dir/$entry" ]]; then
        printf 'Missing Atuin build source: %s\n' "$source_dir/$entry" >&2
        exit 1
    fi
done
tar -C "$source_dir" --exclude=.git --exclude=target --exclude=node_modules \
    --exclude=.cache --exclude=__pycache__ \
    -cf - "${source_entries[@]}" \
    | tar -C "$destination" -xf -
