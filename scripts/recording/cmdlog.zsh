# Structured command index independent of Atuin's history hooks.
[[ -o interactive && -n ${POMDOCK_CAPTURE:-} && -n ${POMDOCK_ENGAGEMENT:-} ]] || return
(( ${+__pd_cmdlog} )) && return
typeset -g __pd_cmdlog=1 __pd_seq=0 __pd_cmd= __pd_start= __pd_id= __pd_cwd=
autoload -Uz add-zsh-hook
zmodload -F zsh/stat b:zstat 2>/dev/null

__pd_capture_size() {
  local -a st
  if zstat -A st +size "$POMDOCK_SESSION_LOG" 2>/dev/null; then
    typeset -g __pd_size=$st[1]
  else
    typeset -g __pd_size=null
  fi
}
__pd_json() {
  local s=$1
  s=${s//\\/\\\\}; s=${s//\"/\\\"}
  s=${s//$'\n'/\\n}; s=${s//$'\t'/\\t}; s=${s//$'\r'/\\r}
  typeset -g __pd_j=${s//[$'\001'-$'\037']/?}
}
__pd_append() {
  # One complete line per append, serialized across all shells in an engagement.
  ( flock -x 9 && printf '%s\n' "$1" >> "$POMDOCK_SESSION_DIR/commands.jsonl" ) 9>"$POMDOCK_SESSION_DIR/commands.lock"
}
__pd_preexec() {
  __pd_cmd=$1
  __pd_start=$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)
  __pd_id="$POMDOCK_CAPTURE-$$-$((++__pd_seq))"
  __pd_capture_size
  __pd_json "$PWD"; __pd_cwd=$__pd_j
  __pd_json "$__pd_cmd"; local command=$__pd_j
  __pd_json "$POMDOCK_ENGAGEMENT"; local engagement=$__pd_j
  __pd_json "$POMDOCK_CAPTURE"; local sub=$__pd_j
  local record
  printf -v record '{"v":2,"id":"%s","start":"%s","cwd":"%s","session":"%s","subid":"%s","pid":%d,"shell":"zsh","pos":%s,"cmd":"%s"}' \
    "$__pd_id" "$__pd_start" "$__pd_cwd" "$engagement" "$sub" "$$" "$__pd_size" "$command"
  __pd_append "$record"
  printf '\033]777;pomdock-start;%s\007' "$__pd_id"
}
__pd_precmd() {
  local ec=$? end record
  [[ -n $__pd_start ]] || return 0
  end=$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)
  __pd_capture_size
  printf '\033]777;pomdock-end;%s\007' "$__pd_id"
  printf -v record '{"v":2,"id":"%s","end":"%s","exit":%d,"end_pos":%s}' "$__pd_id" "$end" "$ec" "$__pd_size"
  __pd_append "$record"
  __pd_cmd= __pd_start=
  return 0
}
add-zsh-hook preexec __pd_preexec
add-zsh-hook precmd __pd_precmd
