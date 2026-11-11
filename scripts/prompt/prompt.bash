# shellcheck shell=bash
[[ $- == *i* ]] || return
[[ ${POMDOCK_KIND:-} == container || -e /.dockerenv ]] || return
_pomdock_shell_listing() {
    local name value
    for name in ls la ll lt; do
        value=${BASH_ALIASES[$name]-}
        if [[ $value == eza\ * && $value != *--icons=never ]]; then
            # Freeze the existing alias text, rather than expanding it on use.
            # shellcheck disable=SC2139
            alias "$name=$value --icons=never"
        fi
    done
    return 0
}
_pomdock_prompt_start() { _pomdock_prompt_started=$EPOCHREALTIME; }
_pomdock_prompt_precmd() {
    local code=$? instance=${POMDOCK_MACHINE:-$(command hostname)} network exitip='' clock elevated='' duration=''
    instance=${instance//[^A-Za-z0-9_.-]/-}
    TZ=Europe/Berlin printf -v clock '%(%H:%M:%S)T' -1
    network=$(command pomdock-netcontext 2>/dev/null) || network=''
    if command -v pomdock-exitip >/dev/null 2>&1; then
        exitip=$(command pomdock-exitip show 2>/dev/null) || exitip=''
    fi
    if [[ -n ${_pomdock_prompt_started:-} ]]; then
        local seconds=$(( (${EPOCHREALTIME/./} - ${_pomdock_prompt_started/./}) / 1000000 ))
        if (( seconds >= 2 )); then
            if (( seconds >= 60 )); then duration=" · $((seconds / 60))m$((seconds % 60))s"
            else duration=" · ${seconds}s"
            fi
        fi
    fi
    _pomdock_prompt_started=''
    (( EUID == 0 )) && elevated='\[\e[1;31m\]root@'
    PS1="${clock} ${elevated}\\[\\e[0;36m\\]${instance}\\[\\e[0m\\] · \\w"
    [[ -n $network ]] && PS1+=" · \\[\\e[36m\\]${network}\\[\\e[0m\\]"
    [[ -n $exitip ]] && PS1+=" · exit \\[\\e[36m\\]${exitip}\\[\\e[0m\\]"
    (( code != 0 )) && PS1+=" \\[\\e[31m\\]· exit ${code}\\[\\e[0m\\]"
    [[ -n $duration ]] && PS1+="$duration"
    PS1+='\n\[\e[36m\]❯\[\e[0m\] '
    return 0
}
_pomdock_prompt_init() {
    local index
    _pomdock_shell_listing
    for index in "${!PROMPT_COMMAND[@]}"; do
        [[ ${PROMPT_COMMAND[index]} != _pomdock_prompt_init ]] || unset 'PROMPT_COMMAND[index]'
    done
    PROMPT_COMMAND+=(_pomdock_prompt_precmd)
    if (( BASH_VERSINFO[0] > 5 || (BASH_VERSINFO[0] == 5 && BASH_VERSINFO[1] >= 3) )); then
        # Parent-shell substitution preserves the timer without touching DEBUG.
        # shellcheck disable=SC2016
        PS0="${PS0:-}"'${ _pomdock_prompt_start; }'
    fi
    _pomdock_prompt_precmd
}
# A live shell can reload the definitions without adding prompt callbacks.
_pomdock_shell_listing
[[ -z ${_pomdock_prompt_scheduled:-} ]] || return 0
_pomdock_prompt_scheduled=1
PROMPT_COMMAND+=(_pomdock_prompt_init)
