// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package cli

import "fmt"

// GenerateCompletion returns a shell completion script for the given shell.
func GenerateCompletion(shell string) (string, error) {
	switch shell {
	case "zsh":
		return generateZshCompletion(), nil
	case "bash":
		return generateBashCompletion(), nil
	case "fish":
		return generateFishCompletion(), nil
	case "powershell":
		return generatePowershellCompletion(), nil
	default:
		return "", fmt.Errorf("unsupported shell %q (supported: zsh, bash, fish, powershell)", shell)
	}
}

func generateZshCompletion() string {
	return `#compdef baryo

_baryo() {
    local -a commands flags
    commands=(
        'doctor:Run full diagnostic check'
        'completion:Generate shell completion script'
    )
    flags=(
        '-p[Send a prompt in non-interactive mode]:prompt:'
        '--model[Select a model by name]:model:'
        '--system-prompt[Override the system prompt]:prompt:'
        '--temperature[Sampling temperature (0.0-2.0)]:temp:'
        '--top-p[Nucleus sampling threshold]:top_p:'
        '--max-tokens[Maximum tokens to generate]:tokens:'
        '-c[Resume most recent session]'
        '--continue[Resume most recent session]'
        '-r[List and pick a saved session]'
        '--resume[List and pick a saved session]'
        '--resume-id[Resume a specific session by ID]:id:'
        '--tunnel[SSH tunnel as user@host]:tunnel:'
        '-y[Auto-approve destructive tool calls]'
        '--yolo[Auto-approve destructive tool calls]'
        '--max-turns[Max tool-call rounds in print mode]:turns:'
        '--output[Output format for print mode]:format:(text json)'
        '--no-tools[Disable tool calling]'
        '--debug[Enable debug logging]'
        '--strategy[Path to strategy JSON file]:file:_files'
        '--worktree[Run in isolated git worktree]'
        '--sandbox[Run code in Docker sandbox]'
        '--skip-checks[Skip startup health checks]'
        '--version[Print version]'
        '--help[Print help]'
    )

    _arguments -s $flags
    _describe -t commands 'baryo commands' commands
}

compdef _baryo baryo
`
}

func generateBashCompletion() string {
	return `# bash completion for baryo

_baryo_complete() {
    local cur prev opts commands
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    commands="doctor completion"
    opts="-p --model --system-prompt --temperature --top-p --max-tokens -c --continue -r --resume --resume-id --tunnel -y --yolo --max-turns --output --no-tools --debug --strategy --worktree --sandbox --skip-checks --version --help"

    case "${prev}" in
        --output)
            COMPREPLY=( $(compgen -W "text json" -- "${cur}") )
            return 0
            ;;
        --strategy)
            COMPREPLY=( $(compgen -f -- "${cur}") )
            return 0
            ;;
        completion)
            COMPREPLY=( $(compgen -W "zsh bash fish powershell" -- "${cur}") )
            return 0
            ;;
    esac

    if [[ "${cur}" == -* ]]; then
        COMPREPLY=( $(compgen -W "${opts}" -- "${cur}") )
    else
        COMPREPLY=( $(compgen -W "${commands}" -- "${cur}") )
    fi
}

complete -F _baryo_complete baryo
`
}

func generateFishCompletion() string {
	return `# fish completion for baryo

complete -c baryo -n '__fish_use_subcommand' -a 'doctor' -d 'Run diagnostic check'
complete -c baryo -n '__fish_use_subcommand' -a 'completion' -d 'Generate shell completion'

complete -c baryo -s p -d 'Send prompt (non-interactive)'
complete -c baryo -l model -d 'Select a model by name'
complete -c baryo -l system-prompt -d 'Override system prompt'
complete -c baryo -l temperature -d 'Sampling temperature (0.0-2.0)'
complete -c baryo -l top-p -d 'Nucleus sampling threshold'
complete -c baryo -l max-tokens -d 'Maximum tokens to generate'
complete -c baryo -s c -l continue -d 'Resume most recent session'
complete -c baryo -s r -l resume -d 'List saved sessions'
complete -c baryo -l resume-id -d 'Resume specific session'
complete -c baryo -l tunnel -d 'SSH tunnel as user@host'
complete -c baryo -s y -l yolo -d 'Auto-approve tool calls'
complete -c baryo -l max-turns -d 'Max tool-call rounds'
complete -c baryo -l output -d 'Output format' -xa 'text json'
complete -c baryo -l no-tools -d 'Disable tool calling'
complete -c baryo -l debug -d 'Enable debug logging'
complete -c baryo -l strategy -d 'Strategy JSON file' -rF
complete -c baryo -l worktree -d 'Run in isolated git worktree'
complete -c baryo -l sandbox -d 'Run code in Docker sandbox'
complete -c baryo -l skip-checks -d 'Skip startup health checks'
complete -c baryo -l version -d 'Print version'
complete -c baryo -l help -d 'Print help'

complete -c baryo -n '__fish_seen_subcommand_from completion' -xa 'zsh bash fish powershell'
`
}

func generatePowershellCompletion() string {
	return `# PowerShell completion for baryo

Register-ArgumentCompleter -CommandName baryo -ScriptBlock {
    param($commandName, $wordToComplete, $cursorPosition)

    $flags = @(
        [CompletionResult]::new('-p', '-p', 'ParameterName', 'Send prompt')
        [CompletionResult]::new('--model', '--model', 'ParameterName', 'Select model')
        [CompletionResult]::new('--system-prompt', '--system-prompt', 'ParameterName', 'Override system prompt')
        [CompletionResult]::new('--temperature', '--temperature', 'ParameterName', 'Sampling temperature')
        [CompletionResult]::new('--top-p', '--top-p', 'ParameterName', 'Nucleus sampling')
        [CompletionResult]::new('--max-tokens', '--max-tokens', 'ParameterName', 'Max tokens')
        [CompletionResult]::new('-c', '-c', 'ParameterName', 'Continue session')
        [CompletionResult]::new('-r', '-r', 'ParameterName', 'Resume session')
        [CompletionResult]::new('-y', '-y', 'ParameterName', 'Auto-approve')
        [CompletionResult]::new('--yolo', '--yolo', 'ParameterName', 'Auto-approve')
        [CompletionResult]::new('--debug', '--debug', 'ParameterName', 'Debug logging')
        [CompletionResult]::new('--worktree', '--worktree', 'ParameterName', 'Git worktree')
        [CompletionResult]::new('--sandbox', '--sandbox', 'ParameterName', 'Docker sandbox')
        [CompletionResult]::new('--version', '--version', 'ParameterName', 'Version')
        [CompletionResult]::new('--help', '--help', 'ParameterName', 'Help')
        [CompletionResult]::new('doctor', 'doctor', 'ParameterValue', 'Run diagnostics')
        [CompletionResult]::new('completion', 'completion', 'ParameterValue', 'Generate completion')
    )

    $flags | Where-Object { $_.CompletionText -like "$wordToComplete*" }
}
`
}
