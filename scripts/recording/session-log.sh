# shellcheck shell=sh
# Capture each interactive terminal exactly once, including nested tmux panes.
# Valid in bash and zsh. Sourced before the user's shell configuration.
case $- in *i*) ;; *) return ;; esac
[ -n "${POMDOCK_ENGAGEMENT:-}" ] || return
__pd_tty=$(tty 2>/dev/null) || return
[ "$__pd_tty" = "${POMDOCK_PTY:-}" ] && return

case "$(ps -o comm= -p "$PPID" 2>/dev/null)" in
  script)
    if [ -n "${POMDOCK_CAPTURE:-}" ] && [ -f "${POMDOCK_SESSION_LOG:-}" ]; then
      export POMDOCK_PTY="$__pd_tty"
      export POMDOCK_CAPTURE_ACTIVE=1
      return
    fi ;;
esac

__pd_dir=${POMDOCK_SESSION_DIR:-$HOME/workspace/sessions}
umask 077
mkdir -p -- "$__pd_dir" || return
if ! [ -w "$__pd_dir" ]; then
  echo 'pomdock: recordings directory is not writable; this terminal is not recorded' >&2
  return
fi
__pd_context=${POMDOCK_SHELL_CONTEXT:-main}
if [ -n "${TMUX:-}" ]; then
  __pd_context="tmux-$(tmux display-message -p '#S_#I_#P' 2>/dev/null || echo unknown)"
fi
# Context is a filename component, never shell code or an unchecked path.
__pd_context=$(printf '%s' "$__pd_context" | tr -c 'A-Za-z0-9_-' '-')
POMDOCK_CAPTURE="$(date -u +%Y%m%dT%H%M%SZ)-${__pd_context}-pid$$"
export POMDOCK_CAPTURE
export POMDOCK_SESSION_LOG="$__pd_dir/$POMDOCK_CAPTURE.log"
export POMDOCK_SESSION_DIR="$__pd_dir"
date -u +%Y-%m-%dT%H:%M:%S.%3NZ > "$__pd_dir/$POMDOCK_CAPTURE.start"
__pd_shell=$(readlink /proc/$$/exe)
export SHELL="$__pd_shell"
exec script -q --flush --return --logging-format advanced \
  --log-out "$POMDOCK_SESSION_LOG" \
  --log-timing "$__pd_dir/$POMDOCK_CAPTURE.timing" \
  --command "exec $__pd_shell -l"
