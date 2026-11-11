# System fallback: personal .bashrc may supply its own Atuin/preexec integration.
# shellcheck shell=bash
[[ $- == *i* ]] || return
[[ -z ${_pomdock_atuin_scheduled:-} ]] || return
_pomdock_atuin_scheduled=1
_pomdock_atuin_init() {
    local index init
    for index in "${!PROMPT_COMMAND[@]}"; do
        [[ ${PROMPT_COMMAND[index]} != _pomdock_atuin_init ]] || unset 'PROMPT_COMMAND[index]'
    done
    if [[ ${__atuin_initialized:-} == true ]] || declare -F __atuin_preexec >/dev/null; then
        return
    fi
    command -v atuin >/dev/null || return
    init=$(atuin init bash --disable-up-arrow) || return
    eval "$init"
    # The upstream init registers arrays; it needs a preexec backend to run them.
    # Kali's Bash 5.3 PS0 runs in the parent shell and preserves the history ID.
    if [[ -z ${bash_preexec_imported:-}${__bp_imported:-}${BLE_VERSION:-} ]]; then
        if (( BASH_VERSINFO[0] > 5 || (BASH_VERSINFO[0] == 5 && BASH_VERSINFO[1] >= 3) )); then
            _pomdock_atuin_preexec() {
                local entry number
                entry=$(HISTTIMEFORMAT='' history 1)
                entry=${entry#"${entry%%[![:space:]]*}"}
                number=${entry%%[![:digit:]]*}
                __atuin_preexec "${entry#"$number  "}"
            }
            # Preserve the recorder's PS0 and DEBUG trap.
            # shellcheck disable=SC2016
            PS0="${PS0:-}"'${ _pomdock_atuin_preexec; }'
            PROMPT_COMMAND+=(__atuin_precmd)
        else
            printf 'pomdock: Atuin Bash history needs Bash 5.3+ or bash-preexec\n' >&2
        fi
    fi
}
PROMPT_COMMAND+=(_pomdock_atuin_init)
