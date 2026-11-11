# Pomdock owns the container prompt; personal host prompts remain independent.
[[ -o interactive ]] || return
[[ ${POMDOCK_KIND:-} == container || -e /.dockerenv ]] || return
autoload -Uz add-zsh-hook
zmodload zsh/datetime 2>/dev/null
_pomdock_shell_listing() {
    local name value
    for name in ls la ll lt; do
        value=${aliases[$name]-}
        if [[ $value == eza\ * && $value != *--icons=never ]]; then
            alias "$name=$value --icons=never"
        fi
    done
    return 0
}
_pomdock_prompt_start() { _pomdock_prompt_started=$EPOCHREALTIME; }
_pomdock_prompt_precmd() {
    local code=$? instance=${POMDOCK_MACHINE:-$(command hostname)} network exitip='' clock duration='' elevated=''
    instance=${instance//[^A-Za-z0-9_.-]/-}
    TZ=Europe/Berlin strftime -s clock '%H:%M:%S' "$EPOCHSECONDS"
    network=$(command pomdock-netcontext 2>/dev/null) || network=''
    if command -v pomdock-exitip >/dev/null 2>&1; then
        exitip=$(command pomdock-exitip show 2>/dev/null) || exitip=''
    fi
    if [[ -n ${_pomdock_prompt_started:-} ]]; then
        local -F elapsed=$(( EPOCHREALTIME - _pomdock_prompt_started ))
        if (( elapsed >= 2 )); then
            local -i seconds=$elapsed
            if (( seconds >= 60 )); then
                duration=" · $(( seconds / 60 ))m$(( seconds % 60 ))s"
            else
                duration=" · ${seconds}s"
            fi
        fi
    fi
    _pomdock_prompt_started=''
    (( EUID == 0 )) && elevated='%F{red}%Broot@%b'
    PROMPT="${clock} ${elevated}%F{cyan}${instance}%f · %~"
    [[ -n $network ]] && PROMPT+=" · %F{cyan}${network}%f"
    [[ -n $exitip ]] && PROMPT+=" · exit %F{cyan}${exitip}%f"
    (( code != 0 )) && PROMPT+=" %F{red}· exit ${code}%f"
    [[ -n $duration ]] && PROMPT+="$duration"
    PROMPT+=$'\n''%F{cyan}❯%f '
    RPROMPT=''
    return 0
}
_pomdock_prompt_init() {
    add-zsh-hook -d precmd _pomdock_prompt_init
    # Personal dotfiles load after the system configuration. Retain their
    # listing options and colors, without font-dependent file icons.
    _pomdock_shell_listing
    # Retire old container-only prompt hooks if an older dotfile is sourced.
    add-zsh-hook -d precmd _pomdock_exitip
    add-zsh-hook -d precmd starship_precmd
    add-zsh-hook -d preexec starship_preexec
    precmd_functions=(_pomdock_prompt_precmd ${precmd_functions:#_pomdock_prompt_precmd})
    add-zsh-hook preexec _pomdock_prompt_start
    _pomdock_prompt_precmd
}
# First prompt runs after personal .zshrc, so its old PS1 cannot replace this.
# Definitions above can be sourced again without duplicating hooks.
_pomdock_shell_listing
(( ${+_pomdock_prompt_scheduled} )) && return 0
typeset -g _pomdock_prompt_scheduled=1
add-zsh-hook precmd _pomdock_prompt_init
