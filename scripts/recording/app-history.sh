# shellcheck shell=sh
# Persist application-maintained REPL histories with engagement recordings.
# No raw input, password prompts or terminal keystrokes are captured here.
case $- in *i*) ;; *) return ;; esac
[ -n "${POMDOCK_ENGAGEMENT:-}" ] || return
__pd_apphist="${POMDOCK_SESSION_DIR:-$HOME/workspace/sessions}/apphist"
(umask 077 && mkdir -p -- "$__pd_apphist") || return
export PYTHON_HISTORY="$__pd_apphist/python_history"
export SQLITE_HISTORY="$__pd_apphist/sqlite_history"
export PSQL_HISTORY="$__pd_apphist/psql_history"
export MYSQL_HISTFILE="$__pd_apphist/mysql_history"
# Metasploit uses ~/.msf4/history. Keep an existing user history untouched.
if [ ! -e "$HOME/.msf4/history" ] && [ ! -L "$HOME/.msf4/history" ]; then
    mkdir -p "$HOME/.msf4" && ln -s "$__pd_apphist/msfconsole_history" "$HOME/.msf4/history"
fi
unset __pd_apphist
