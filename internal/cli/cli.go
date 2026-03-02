// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// Version is set at build time via ldflags.
var Version = "dev"

// Mode represents the execution mode for the CLI.
type Mode int

const (
	ModeInteractive Mode = iota
	ModePrint
	ModeVersion
	ModeHelp
	ModeDoctor
)

// Config holds the parsed CLI flags and stdin data.
type Config struct {
	Prompt       string
	Model        string
	SystemPrompt string
	Params       llm.ChatParams
	Tunnel       string // user@host for dynamic SSH tunnel
	Continue     bool
	Resume       bool
	ResumeID     string
	Yolo         bool
	SkipChecks   bool
	Doctor       bool
	ShowHelp     bool
	ShowVer      bool
	StdinData    string
	MaxTurns     int
	Output       string
	NoTools      bool
	Debug        bool
	Strategy     string // --strategy flag: path to strategy JSON file
}

// Parse parses CLI arguments and reads piped stdin if present.
func Parse() Config {
	var cfg Config
	var temperature, topP float64
	var maxTokens int

	fs := flag.NewFlagSet("baryo", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we handle errors ourselves

	fs.StringVar(&cfg.Prompt, "p", "", "prompt to send (non-interactive)")
	fs.StringVar(&cfg.Model, "model", "", "model name or substring to match")
	fs.StringVar(&cfg.SystemPrompt, "system-prompt", "", "override the system prompt")
	fs.Float64Var(&temperature, "temperature", -1, "sampling temperature (0.0-2.0)")
	fs.Float64Var(&topP, "top-p", -1, "nucleus sampling threshold (0.0-1.0)")
	fs.IntVar(&maxTokens, "max-tokens", -1, "maximum tokens to generate")
	fs.BoolVar(&cfg.Continue, "c", false, "resume most recent session")
	fs.BoolVar(&cfg.Continue, "continue", false, "resume most recent session")
	fs.BoolVar(&cfg.Resume, "r", false, "list and pick a saved session")
	fs.BoolVar(&cfg.Resume, "resume", false, "list and pick a saved session")
	fs.StringVar(&cfg.ResumeID, "resume-id", "", "resume a specific session by ID")
	fs.StringVar(&cfg.Tunnel, "tunnel", "", "SSH tunnel as user@host (ports default to 11434)")
	fs.BoolVar(&cfg.Yolo, "y", false, "auto-approve all destructive tool calls")
	fs.BoolVar(&cfg.Yolo, "yolo", false, "auto-approve all destructive tool calls")
	fs.BoolVar(&cfg.SkipChecks, "skip-checks", false, "skip startup health checks")
	fs.IntVar(&cfg.MaxTurns, "max-turns", 0, "max tool-call rounds in print mode (0 = default 5)")
	fs.StringVar(&cfg.Output, "output", "text", "output format for print mode: text or json")
	fs.BoolVar(&cfg.NoTools, "no-tools", false, "disable tool calling in print mode")
	fs.BoolVar(&cfg.Debug, "debug", false, "enable debug logging to ~/.baryo/debug.log")
	fs.StringVar(&cfg.Strategy, "strategy", "", "path to strategy JSON file (use with -p)")
	fs.BoolVar(&cfg.ShowVer, "version", false, "print version and exit")
	fs.BoolVar(&cfg.ShowHelp, "help", false, "print usage and exit")

	// Check for "doctor" subcommand — can appear as first arg or after flags.
	// Filter it out before parsing so flag.Parse doesn't choke on it.
	args := os.Args[1:]
	for i, a := range args {
		if a == "doctor" {
			cfg.Doctor = true
			args = append(args[:i], args[i+1:]...)
			break
		}
	}

	if err := fs.Parse(args); err != nil {
		// If parsing fails, show help
		cfg.ShowHelp = true
		return cfg
	}

	// Convert sentinel values to pointers (only set if explicitly provided)
	if temperature >= 0 {
		cfg.Params.Temperature = &temperature
	}
	if topP >= 0 {
		cfg.Params.TopP = &topP
	}
	if maxTokens >= 0 {
		cfg.Params.MaxTokens = &maxTokens
	}

	// BARYO_DEBUG=1 env var also enables debug mode.
	if !cfg.Debug {
		if v := os.Getenv("BARYO_DEBUG"); v == "1" || v == "true" {
			cfg.Debug = true
		}
	}

	// Read piped stdin (non-TTY)
	if info, err := os.Stdin.Stat(); err == nil {
		if info.Mode()&os.ModeCharDevice == 0 {
			data, err := io.ReadAll(os.Stdin)
			if err == nil && len(data) > 0 {
				cfg.StdinData = string(data)
			}
		}
	}

	return cfg
}

// Mode returns which execution mode applies based on the parsed config.
func (c Config) Mode() Mode {
	if c.ShowVer {
		return ModeVersion
	}
	if c.ShowHelp {
		return ModeHelp
	}
	if c.Doctor {
		return ModeDoctor
	}
	if c.Prompt != "" || c.StdinData != "" || c.Strategy != "" {
		return ModePrint
	}
	return ModeInteractive
}

// FullPrompt combines stdin data and the -p prompt into a single string.
func (c Config) FullPrompt() string {
	var parts []string
	if c.StdinData != "" {
		parts = append(parts, fmt.Sprintf("<context>\n%s\n</context>", strings.TrimRight(c.StdinData, "\n")))
	}
	if c.Prompt != "" {
		parts = append(parts, c.Prompt)
	}
	return strings.Join(parts, "\n\n")
}

// PrintVersion prints the version string.
func PrintVersion() {
	fmt.Printf("baryo version %s\n", Version)
}

// PrintHelp prints usage information.
func PrintHelp() {
	fmt.Print(`Usage: baryo [flags]
       baryo doctor

A local AI chat CLI powered by Docker Model Runner.

Flags:
  -p <prompt>           Send a prompt in non-interactive (print) mode
  --model <name>        Select a model by name or substring
  --system-prompt <s>   Override the system prompt
  --temperature <f>     Sampling temperature (0.0-2.0)
  --top-p <f>           Nucleus sampling threshold (0.0-1.0)
  --max-tokens <n>      Maximum tokens to generate
  -c, --continue        Resume the most recent session in this directory
  -r, --resume          List and pick a saved session to resume
  --resume-id <id>      Resume a specific session by ID
  --tunnel <user@host>  Auto-start SSH tunnel to remote Ollama server
  -y, --yolo            Auto-approve all destructive tool calls
  --max-turns <n>       Max tool-call rounds in print mode (default: 5)
  --output <fmt>        Output format for print mode: text or json
  --no-tools            Disable tool calling in print mode
  --strategy <file>     Path to strategy JSON file (use with -p)
  --debug               Enable debug logging to ~/.baryo/debug.log
  --skip-checks         Skip startup health checks
  --version             Print version and exit
  --help                Print this help message

Subcommands:
  doctor            Run full diagnostic check (Docker, Model Runner, models)

TUI Commands:
  /clear            Start a fresh conversation
  /sessions         List and pick a saved session to resume
  /resume           Alias for /sessions
  /models           Browse downloaded and available models
  /system           View or edit the active system prompt
  /params           View or adjust model parameters
  /export [file]    Export conversation to markdown or JSON
  /copy             Copy last assistant response to clipboard
  /markdown         Toggle markdown rendering on/off
  /doctor           Run diagnostic checks inside the TUI
  /search <query>   Search the web (DuckDuckGo, Brave, or Tavily)
  /strategy [file]  Analyze facts+constraints+goal → optimal steps
  /fetch <url>      Fetch a URL and inject its content into the conversation

Mentions:
  @filepath         Attach a file's contents as context (tab to complete)
                    Example: explain @main.go    compare @go.mod @go.sum

Examples:
  baryo                          Launch interactive TUI
  baryo doctor                   Run diagnostic checks
  baryo --model mistral          Launch TUI with a specific model
  baryo -p "what is 2+2"         Print mode: stream answer to stdout
  baryo --temperature 0.8 -p "write a poem"  Custom temperature
  baryo -c                       Resume last session in this directory
  baryo -r                       Pick a session to resume
  cat file.go | baryo -p "explain this"  Pipe stdin as context

Headless / CI:
  baryo -p "read main.go and summarize" --yolo      Tools with auto-approve
  baryo -p "list Go files" --yolo --output json      JSON structured output
  baryo -p "what is 2+2" --no-tools                  Simple Q&A, no tools
  baryo -p "edit main.go" --yolo --max-turns 2       Limited tool rounds
  cat main.go | baryo -p "review this" --yolo        Piped input with tools
`)
}
