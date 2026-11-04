#!/usr/bin/env bash
# Migrated from Common's bash-preexec-rearm.sh:
# https://github.com/projectbluefin/common/blob/main/system_files/shared/usr/share/ublue-os/bling/bash-preexec-rearm.sh
# bash-preexec's deferred installer can remain in PROMPT_COMMAND arrays and
# clear DEBUG every prompt. Rearm its original trap after all prompt hooks,
# preserving existing preexec_functions (including history consumers).
# https://github.com/projectbluefin/common/issues/869
if [[ $(type -t __bp_preexec_invoke_exec) == function ]] &&
    (unset PROMPT_COMMAND) 2>/dev/null; then
    __bling_rearm_bp_debug_trap() {
        trap '__bp_preexec_invoke_exec "$_"' DEBUG
    }
    if [[ ${PROMPT_COMMAND[*]-} != *__bling_rearm_bp_debug_trap* ]]; then
        if ((BASH_VERSINFO[0] > 5 || (BASH_VERSINFO[0] == 5 && BASH_VERSINFO[1] >= 1))); then
            PROMPT_COMMAND+=('__bling_rearm_bp_debug_trap')
        elif [[ -n ${PROMPT_COMMAND:-} ]]; then
            PROMPT_COMMAND="${PROMPT_COMMAND}"$'\n''__bling_rearm_bp_debug_trap'
        else
            PROMPT_COMMAND='__bling_rearm_bp_debug_trap'
        fi
    fi
fi
