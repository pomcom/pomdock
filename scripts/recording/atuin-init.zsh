# System fallback: defer until personal .zshrc has had its chance to configure Atuin.
[[ -o interactive ]] || return
(( ${+_pomdock_atuin_scheduled} )) && return
typeset -g _pomdock_atuin_scheduled=1
autoload -Uz add-zsh-hook
_pomdock_atuin_init() {
    add-zsh-hook -d precmd _pomdock_atuin_init
    (( ${+functions[_atuin_preexec]} || ${+functions[_atuin_precmd]} )) && return
    (( $+commands[atuin] )) || return
    local init
    init=$(atuin init zsh --disable-up-arrow) || return
    eval "$init"
}
add-zsh-hook precmd _pomdock_atuin_init
