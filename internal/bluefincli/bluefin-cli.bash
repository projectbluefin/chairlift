#!/usr/bin/env bash
# ChairLift owns this Bash-only payload. Based on Common's bling.sh:
# https://github.com/projectbluefin/common/blob/main/system_files/shared/usr/share/ublue-os/bling/bling.sh
# Share Common's guard so an existing personal Common source cannot add hooks twice.
[[ ${_CHAIRLIFT_BLUEFIN_CLI_SOURCED:-0} == 1 || ${BLING_SOURCED:-0} == 1 ]] && return

_chairlift_bluefin_cli_dir=${BASH_SOURCE[0]%/*}
if [[ ! -r "$_chairlift_bluefin_cli_dir/homebrew.bash" ]]; then
    printf 'ChairLift Bluefin CLI: missing Homebrew environment; enable Bluefin CLI again.\n' >&2
    unset _chairlift_bluefin_cli_dir
    return 1
fi
source "$_chairlift_bluefin_cli_dir/homebrew.bash" || return
_CHAIRLIFT_BLUEFIN_CLI_SOURCED=1
BLING_SOURCED=1

if command -v eza >/dev/null 2>&1; then
    alias ls='eza'
    alias ll='eza -l --icons=auto --group-directories-first'
    alias l.='eza -d .*'
    alias l1='eza -1'
fi
if command -v bat >/dev/null 2>&1; then
    alias cat='bat --style=plain --pager=never'
fi
if command -v ug >/dev/null 2>&1; then
    alias grep='ug'
    alias egrep='ug -E'
    alias fgrep='ug -F'
    alias xzgrep='ug -z'
    alias xzegrep='ug -zE'
    alias xzfgrep='ug -zF'
fi
if command -v xdg-open >/dev/null 2>&1; then
    alias open='xdg-open >/dev/null 2>&1'
fi

# bash-preexec is optional; keep an already loaded history integration intact.
# Retain Common's historical search paths without requiring any of its files.
if [[ $(type -t __bp_preexec_invoke_exec) != function ]]; then
    for _chairlift_preexec in /etc/profile.d/bash-preexec.sh /usr/share/bash-prexec /usr/share/bash-prexec.sh /usr/share/bash-preexec.sh "$HOMEBREW_PREFIX/etc/profile.d/bash-preexec.sh"; do
        if [[ -r $_chairlift_preexec ]]; then
            source "$_chairlift_preexec"
            [[ $(type -t __bp_preexec_invoke_exec) == function ]] && break
        fi
    done
    unset _chairlift_preexec
fi

command -v direnv >/dev/null 2>&1 && eval "$(direnv hook bash)"
command -v starship >/dev/null 2>&1 && eval "$(starship init bash)"
command -v zoxide >/dev/null 2>&1 && eval "$(zoxide init bash)"
if command -v mise >/dev/null 2>&1 && [[ ${MISE_BASH_AUTO_ACTIVATE:-1} != 0 ]]; then
    eval "$(mise activate bash)"
fi

# This must remain after prompt integrations so the rearm hook is last.
if [[ -r "$_chairlift_bluefin_cli_dir/bash-preexec-rearm.bash" ]]; then
    source "$_chairlift_bluefin_cli_dir/bash-preexec-rearm.bash"
fi
unset _chairlift_bluefin_cli_dir
