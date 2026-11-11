# Bash resolves its help builtin before the executable on PATH. Keep its topic
# help available while using the shared Pomdock reference for bare `help`.
# shellcheck shell=bash
case $- in *i*) ;; *) return ;; esac
[ -n "${BASH_VERSION:-}" ] && [ -n "${POMDOCK_ENGAGEMENT:-}" ] || return

help() {
    if [ "$#" -eq 0 ] && command -v pomdock-help >/dev/null 2>&1; then
        command pomdock-help
    else
        builtin help "$@"
    fi
}
