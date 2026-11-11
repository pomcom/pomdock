# Bash command index. Preserve PROMPT_COMMAND slots and leave Atuin untouched.
case $- in *i*) ;; *) return ;; esac
[ -n "${POMDOCK_CAPTURE:-}" ] && [ -n "${POMDOCK_ENGAGEMENT:-}" ] || return
[ -z "${__pd_cmdlog:-}" ] || return
__pd_cmdlog=1
__pd_seq=0
__pd_armed=0
__pd_json() {
  local s=$1
  s=${s//\\/\\\\}; s=${s//\"/\\\"}
  s=${s//$'\n'/\\n}; s=${s//$'\t'/\\t}; s=${s//$'\r'/\\r}
  __pd_j=${s//[$'\001'-$'\037']/?}
}
__pd_append() {
  ( flock -x 9 && printf '%s\n' "$1" >> "$POMDOCK_SESSION_DIR/commands.jsonl" ) 9>"$POMDOCK_SESSION_DIR/commands.lock"
}
__pd_preexec() {
  __pd_start=$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)
  __pd_pos=$(stat -c %s "$POMDOCK_SESSION_LOG" 2>/dev/null || echo null)
  __pd_json "$PWD"; __pd_cwd=$__pd_j
  __pd_hist=$(HISTTIMEFORMAT='' history 1)
  __pd_armed=1
}
__pd_debug() {
  [ "$__pd_armed" = 1 ] || return 0
  __pd_armed=2
  local h n cmd record engagement sub
  h=${__pd_hist#"${__pd_hist%%[![:space:]]*}"}; n=${h%%[![:digit:]]*}
  if [ -n "$n" ] && [ "$n" != "$__pd_last_n" ]; then
    cmd=${h#"$n  "}; __pd_last_n=$n
  else
    cmd=$BASH_COMMAND
  fi
  __pd_seq=$((__pd_seq+1)); __pd_id="$POMDOCK_CAPTURE-$$-$__pd_seq"
  __pd_json "$POMDOCK_ENGAGEMENT"; engagement=$__pd_j
  __pd_json "$POMDOCK_CAPTURE"; sub=$__pd_j
  __pd_json "$cmd"
  printf -v record '{"v":2,"id":"%s","start":"%s","cwd":"%s","session":"%s","subid":"%s","pid":%d,"shell":"bash","pos":%s,"cmd":"%s"}' \
    "$__pd_id" "$__pd_start" "$__pd_cwd" "$engagement" "$sub" "$$" "$__pd_pos" "$__pd_j"
  __pd_append "$record"
  printf '\033]777;pomdock-start;%s\007' "$__pd_id"
}
__pd_precmd() {
  local ec=$? end size record
  if [ "$__pd_armed" = 2 ]; then
    end=$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)
    printf '\033]777;pomdock-end;%s\007' "$__pd_id"
    size=$(stat -c %s "$POMDOCK_SESSION_LOG" 2>/dev/null || echo null)
    printf -v record '{"v":2,"id":"%s","end":"%s","exit":%d,"end_pos":%s}' "$__pd_id" "$end" "$ec" "$size"
    __pd_append "$record"
  fi
  __pd_armed=0
  return 0
}
__pd_hist=$(HISTTIMEFORMAT='' history 1)
__pd_last_n=${__pd_hist#"${__pd_hist%%[![:space:]]*}"}
__pd_last_n=${__pd_last_n%%[![:digit:]]*}
# Kali bash >=5.3 supports parent-shell PS0 substitutions.
# shellcheck disable=SC2016
PS0="${PS0}"'${ __pd_preexec; }'
trap '__pd_debug' DEBUG
PROMPT_COMMAND[101]=__pd_precmd
