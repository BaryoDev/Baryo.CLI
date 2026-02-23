// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/arnelirobles/baryo-cli/internal/doctor"
	"github.com/arnelirobles/baryo-cli/internal/docker"
	"github.com/arnelirobles/baryo-cli/internal/session"
	"github.com/arnelirobles/baryo-cli/internal/tools"
)

// ChatModel is the chat conversation screen.
type ChatModel struct {
	socketPath   string            // unix socket for Docker Model Runner
	systemPrompt string            // active system prompt
	params       docker.ChatParams // model parameters
	modelName    string            // display name (e.g. "ai/mistral")
	modelTag     string            // full tag for API calls (e.g. "docker.io/ai/mistral:latest")
	messages     []docker.ChatMessage
	history      []chatEntry // rendered conversation history
	streaming    string      // current streaming text accumulator
	turnContent  string      // accumulates all assistant text for one turn (across tool rounds)
	isStream     bool        // whether we are currently streaming
	markdown     bool        // whether to render markdown in responses
	inputHistory []string    // previous user inputs
	historyIdx   int         // current position in input history (-1 = not browsing)
	session      *session.Session

	textarea    textarea.Model
	viewport    viewport.Model
	ready       bool
	width       int
	height      int
	spinFrame   int // current spinner animation frame

	eventCh    <-chan docker.StreamEvent
	cancelFunc context.CancelFunc
	toolStatus string // shown in status bar during tool execution
}

// chatEntry is a rendered message in the history.
type chatEntry struct {
	role    string
	content string
}

// NewChat creates a new chat screen for the given model.
func NewChat(socketPath, systemPrompt string, params docker.ChatParams, modelName, modelTag string) ChatModel {
	ta := newTextarea()
	sess, _ := session.New(modelName, modelTag)
	return ChatModel{
		socketPath:   socketPath,
		systemPrompt: systemPrompt,
		params:       params,
		modelName:    modelName,
		modelTag:     modelTag,
		textarea:     ta,
		markdown:     true,
		historyIdx:   -1,
		session:      sess,
	}
}

// NewChatFromSession restores a chat screen from a saved session.
func NewChatFromSession(socketPath, systemPrompt string, params docker.ChatParams, sess *session.Session) ChatModel {
	ta := newTextarea()
	history := make([]chatEntry, len(sess.Messages))
	for i, m := range sess.Messages {
		c := ""
		if m.Content != nil {
			c = *m.Content
		}
		history[i] = chatEntry{role: m.Role, content: c}
	}
	return ChatModel{
		socketPath:   socketPath,
		systemPrompt: systemPrompt,
		params:       params,
		modelName:    sess.ModelName,
		modelTag:     sess.ModelTag,
		messages:     append([]docker.ChatMessage{}, sess.Messages...),
		history:      history,
		textarea:     ta,
		markdown:     true,
		historyIdx:   -1,
		session:      sess,
	}
}

// spinnerFrames are the animation frames for the inline spinner.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinTickMsg drives the spinner animation during streaming.
type spinTickMsg struct{}

// doSpinTick returns a Cmd that sends a spinTickMsg after a short delay.
func doSpinTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return spinTickMsg{}
	})
}

func newTextarea() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.Focus()
	ta.SetHeight(3)
	ta.SetWidth(80)
	ta.ShowLineNumbers = false
	ta.CharLimit = 4096
	return ta
}

func (m ChatModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m ChatModel) Update(msg tea.Msg) (ChatModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		inputHeight := 5
		headerHeight := 2

		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-inputHeight-headerHeight)
			m.viewport.MouseWheelEnabled = true
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - inputHeight - headerHeight
		}
		m.textarea.SetWidth(msg.Width - 2)
		m.updateViewport()

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.cancelFunc != nil {
				m.cancelFunc()
			}
			return m, tea.Quit
		}

		// Viewport scrolling — up/down arrows scroll the conversation.
		if msg.String() == "up" && !m.isStream {
			m.viewport.ScrollUp(1)
			m.updateViewport()
			return m, nil
		}
		if msg.String() == "down" && !m.isStream {
			m.viewport.ScrollDown(1)
			m.updateViewport()
			return m, nil
		}

		// Input history navigation — ctrl+p/ctrl+n (readline-style).
		if msg.String() == "ctrl+p" && !m.isStream {
			if len(m.inputHistory) == 0 {
				break
			}
			if m.historyIdx == -1 {
				m.historyIdx = len(m.inputHistory) - 1
			} else if m.historyIdx > 0 {
				m.historyIdx--
			}
			m.textarea.SetValue(m.inputHistory[m.historyIdx])
			m.textarea.CursorEnd()
			return m, nil
		}

		if msg.String() == "ctrl+n" && !m.isStream {
			if m.historyIdx == -1 {
				break
			}
			if m.historyIdx < len(m.inputHistory)-1 {
				m.historyIdx++
				m.textarea.SetValue(m.inputHistory[m.historyIdx])
				m.textarea.CursorEnd()
			} else {
				m.historyIdx = -1
				m.textarea.Reset()
			}
			return m, nil
		}

		if msg.String() == "enter" && !m.isStream {
			text := strings.TrimSpace(m.textarea.Value())
			if text == "" {
				break
			}

			m.textarea.Reset()
			m.inputHistory = append(m.inputHistory, text)
			m.historyIdx = -1

			// Handle slash commands
			if strings.HasPrefix(text, "/") {
				return m.handleCommand(text)
			}

			// Add user message
			m.messages = append(m.messages, docker.NewChatMessage("user", text))
			m.history = append(m.history, chatEntry{
				role:    "user",
				content: text,
			})

			// Start streaming
			m.isStream = true
			m.streaming = ""
			m.turnContent = ""
			m.toolStatus = ""

			ctx, cancel := context.WithCancel(context.Background())
			m.cancelFunc = cancel

			toolDefs := toDockerToolDefs(tools.AllDefinitions())
			executor := func(ctx context.Context, name, argsJSON string) (string, bool) {
				r := tools.Execute(ctx, name, argsJSON)
				return r.Content, r.IsError
			}
			m.eventCh = docker.StreamChatWithTools(ctx, m.socketPath, m.modelTag, m.buildMessages(), m.params, toolDefs, executor)

			m.updateViewport()
			return m, tea.Batch(waitForEvent(m.eventCh), doSpinTick())
		}

	case spinTickMsg:
		if m.isStream {
			m.spinFrame = (m.spinFrame + 1) % len(spinnerFrames)
			m.updateViewport()
			return m, doSpinTick()
		}
		return m, nil

	case StreamTokenMsg:
		evt := msg.Event

		if evt.Done {
			// Streaming complete — save to history and auto-save session
			if m.streaming != "" {
				m.history = append(m.history, chatEntry{
					role:    "assistant",
					content: m.streaming,
				})
				m.turnContent += m.streaming
			}
			// Commit the full turn as one assistant message (avoids consecutive assistant roles).
			if m.turnContent != "" {
				m.messages = append(m.messages, docker.NewChatMessage("assistant", m.turnContent))
			}
			m.streaming = ""
			m.turnContent = ""
			m.isStream = false
			m.cancelFunc = nil
			m.eventCh = nil
			m.toolStatus = ""
			m.saveSession()
			m.updateViewport()
			return m, nil
		}

		if evt.Error != "" {
			// If we had partial content, keep it in history
			if m.streaming != "" {
				m.history = append(m.history, chatEntry{
					role:    "assistant",
					content: m.streaming,
				})
			}
			m.history = append(m.history, chatEntry{
				role:    "error",
				content: evt.Error,
			})
			m.streaming = ""
			m.isStream = false
			if m.cancelFunc != nil {
				m.cancelFunc()
			}
			m.cancelFunc = nil
			m.eventCh = nil
			m.toolStatus = ""
			// Remove the user message from conversation so it can be retried
			if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "user" {
				m.messages = m.messages[:len(m.messages)-1]
			}
			m.updateViewport()
			return m, nil
		}

		if evt.ContentReplace != nil {
			m.streaming = *evt.ContentReplace
			m.updateViewport()
			return m, waitForEvent(m.eventCh)
		}

		if evt.ToolStart != nil {
			// Flush any accumulated text before tool use.
			if m.streaming != "" {
				m.history = append(m.history, chatEntry{
					role:    "assistant",
					content: m.streaming,
				})
				m.turnContent += m.streaming
				m.streaming = ""
			}
			m.toolStatus = fmt.Sprintf("Running %s...", evt.ToolStart.Name)
			m.history = append(m.history, chatEntry{
				role:    "tool",
				content: fmt.Sprintf("Tool: %s(%s)", evt.ToolStart.Name, evt.ToolStart.Args),
			})
			m.updateViewport()
			return m, tea.Batch(waitForEvent(m.eventCh), doSpinTick())
		}

		if evt.ToolResult != nil {
			status := summarizeToolResult(evt.ToolResult.Content, evt.ToolResult.IsError)
			m.toolStatus = ""
			m.history = append(m.history, chatEntry{
				role:    "tool",
				content: fmt.Sprintf("Result: %s", status),
			})
			m.updateViewport()
			return m, waitForEvent(m.eventCh)
		}

		if evt.Token != "" {
			m.streaming += evt.Token
			m.updateViewport()
		}
		return m, waitForEvent(m.eventCh)
	}

	// Update sub-components
	var cmd tea.Cmd

	if !m.isStream {
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m ChatModel) handleCommand(text string) (ChatModel, tea.Cmd) {
	switch strings.TrimSpace(text) {
	case "/clear":
		sess, _ := session.New(m.modelName, m.modelTag)
		m.messages = nil
		m.history = nil
		m.session = sess
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: "Session cleared. Starting fresh.",
		})
		m.updateViewport()
		return m, nil

	case "/sessions", "/resume":
		return m, func() tea.Msg {
			summaries, err := session.List()
			if err != nil {
				return ShowSessionsMsg{}
			}
			return ShowSessionsMsg{Sessions: summaries}
		}

	case "/models":
		return m, func() tea.Msg {
			downloaded, dlErr := docker.ListModels()
			available, srErr := docker.SearchModels()
			if dlErr != nil {
				return ShowModelsMsg{Err: dlErr}
			}
			if srErr != nil {
				// Non-fatal: show downloaded models even if search fails
				return ShowModelsMsg{Downloaded: downloaded}
			}
			return ShowModelsMsg{Downloaded: downloaded, Available: available}
		}

	case "/doctor":
		results := doctor.RunChecks(m.socketPath)
		var b strings.Builder
		pass := lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Render("✓")
		fail := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render("✗")
		for _, r := range results {
			if r.Passed {
				b.WriteString(fmt.Sprintf("  %s %s", pass, r.Name))
				if r.Message != "" {
					b.WriteString(fmt.Sprintf(" — %s", r.Message))
				}
				b.WriteString("\n")
			} else {
				b.WriteString(fmt.Sprintf("  %s %s\n\n%s\n", fail, r.Name, r.Message))
			}
		}
		if doctor.AllPassed(results) {
			b.WriteString("\nAll checks passed.")
		}
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: b.String(),
		})
		m.updateViewport()
		return m, nil

	case "/markdown":
		m.markdown = !m.markdown
		msg := "Markdown rendering enabled."
		if !m.markdown {
			msg = "Markdown rendering disabled."
		}
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: msg,
		})
		m.updateViewport()
		return m, nil

	case "/init":
		return m.handleInit()

	case "/copy":
		var lastAssistant string
		for i := len(m.history) - 1; i >= 0; i-- {
			if m.history[i].role == "assistant" {
				lastAssistant = m.history[i].content
				break
			}
		}
		if lastAssistant == "" {
			m.history = append(m.history, chatEntry{
				role:    "assistant",
				content: "Nothing to copy.",
			})
		} else if err := clipboard.WriteAll(lastAssistant); err != nil {
			m.history = append(m.history, chatEntry{
				role:    "assistant",
				content: fmt.Sprintf("Clipboard error: %v", err),
			})
		} else {
			m.history = append(m.history, chatEntry{
				role:    "assistant",
				content: "Copied to clipboard.",
			})
		}
		m.updateViewport()
		return m, nil

	case "/system":
		prompt := m.systemPrompt
		if prompt == "" {
			prompt = "(none)"
		}
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: fmt.Sprintf("System prompt:\n%s\n\nTo change: /system <new prompt>", prompt),
		})
		m.updateViewport()
		return m, nil

	case "/params":
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: m.formatParams(),
		})
		m.updateViewport()
		return m, nil

	default:
		if strings.HasPrefix(text, "/system ") {
			m.systemPrompt = strings.TrimPrefix(text, "/system ")
			m.history = append(m.history, chatEntry{
				role:    "assistant",
				content: "System prompt updated.",
			})
			m.updateViewport()
			return m, nil
		}

		if text == "/export" || strings.HasPrefix(text, "/export ") {
			return m.handleExport(strings.TrimPrefix(text, "/export"))
		}

		if strings.HasPrefix(text, "/params ") {
			if err := m.parseParams(strings.TrimPrefix(text, "/params ")); err != nil {
				m.history = append(m.history, chatEntry{
					role:    "assistant",
					content: fmt.Sprintf("Error: %v", err),
				})
			} else {
				m.history = append(m.history, chatEntry{
					role:    "assistant",
					content: "Parameters updated.\n" + m.formatParams(),
				})
			}
			m.updateViewport()
			return m, nil
		}

		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: fmt.Sprintf("Unknown command: %s\nAvailable: /clear, /sessions, /resume, /models, /system, /params, /export, /copy, /markdown, /doctor, /init", text),
		})
		m.updateViewport()
		return m, nil
	}
}

// toolSystemPrompt is injected to instruct the model to use available tools.
//
//go:embed prompts/tools.md
var toolSystemPrompt string

// buildMessages prepends the system prompt to the conversation messages.
func (m *ChatModel) buildMessages() []docker.ChatMessage {
	msgs := make([]docker.ChatMessage, 0, len(m.messages)+2)

	// Combine user system prompt with tool instructions.
	sysPrompt := toolSystemPrompt
	if m.systemPrompt != "" {
		sysPrompt = m.systemPrompt + "\n\n" + toolSystemPrompt
	}
	msgs = append(msgs, docker.NewChatMessage("system", sysPrompt))
	msgs = append(msgs, m.messages...)
	return msgs
}

func (m *ChatModel) formatParams() string {
	temp := "(default)"
	if m.params.Temperature != nil {
		temp = strconv.FormatFloat(*m.params.Temperature, 'f', 2, 64)
	}
	topP := "(default)"
	if m.params.TopP != nil {
		topP = strconv.FormatFloat(*m.params.TopP, 'f', 2, 64)
	}
	maxTok := "(default)"
	if m.params.MaxTokens != nil {
		maxTok = strconv.Itoa(*m.params.MaxTokens)
	}
	return fmt.Sprintf("temperature: %s\ntop_p: %s\nmax_tokens: %s\n\nUsage: /params temperature=0.8 top_p=0.9 max_tokens=2048", temp, topP, maxTok)
}

func (m *ChatModel) parseParams(input string) error {
	for _, part := range strings.Fields(input) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return fmt.Errorf("invalid format %q, use key=value", part)
		}
		switch kv[0] {
		case "temperature":
			f, err := strconv.ParseFloat(kv[1], 64)
			if err != nil {
				return fmt.Errorf("invalid temperature: %v", err)
			}
			m.params.Temperature = &f
		case "top_p":
			f, err := strconv.ParseFloat(kv[1], 64)
			if err != nil {
				return fmt.Errorf("invalid top_p: %v", err)
			}
			m.params.TopP = &f
		case "max_tokens":
			n, err := strconv.Atoi(kv[1])
			if err != nil {
				return fmt.Errorf("invalid max_tokens: %v", err)
			}
			m.params.MaxTokens = &n
		default:
			return fmt.Errorf("unknown parameter %q (available: temperature, top_p, max_tokens)", kv[0])
		}
	}
	return nil
}

func (m *ChatModel) saveSession() {
	if m.session == nil {
		return
	}
	m.session.Messages = m.messages
	_ = m.session.Save() // best-effort, don't interrupt chat on save error
}

func (m ChatModel) handleInit() (ChatModel, tea.Cmd) {
	const filename = "BARYO.md"

	// Check if BARYO.md already exists
	if _, err := os.Stat(filename); err == nil {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: "BARYO.md already exists. Edit it directly to update your project instructions.",
		})
		m.updateViewport()
		return m, nil
	}

	content := generateBaryoMD()
	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: fmt.Sprintf("Failed to create BARYO.md: %v", err),
		})
		m.updateViewport()
		return m, nil
	}

	m.history = append(m.history, chatEntry{
		role:    "assistant",
		content: "Created BARYO.md with starter instructions.\nEdit it to customize how Baryo behaves in this project.",
	})
	m.updateViewport()
	return m, nil
}

// generateBaryoMD scans the project and builds a starter BARYO.md.
func generateBaryoMD() string {
	var b strings.Builder

	b.WriteString("# Project Instructions\n\n")
	b.WriteString("<!-- Baryo loads this file into the system prompt. -->\n")
	b.WriteString("<!-- Edit it to customize how the AI assistant behaves in this project. -->\n\n")

	// Detect project language/type from common files
	var hints []string
	langFiles := map[string]string{
		"go.mod":         "Go",
		"package.json":   "Node.js / JavaScript",
		"Cargo.toml":     "Rust",
		"pyproject.toml": "Python",
		"requirements.txt": "Python",
		"Gemfile":        "Ruby",
		"pom.xml":        "Java (Maven)",
		"build.gradle":   "Java (Gradle)",
		"CMakeLists.txt": "C/C++ (CMake)",
		"Makefile":       "Make",
		"Dockerfile":     "Docker",
		"docker-compose.yml": "Docker Compose",
	}

	for file, lang := range langFiles {
		if _, err := os.Stat(file); err == nil {
			hints = append(hints, lang)
		}
	}

	if len(hints) > 0 {
		b.WriteString("## Stack\n\n")
		b.WriteString("This project uses: " + strings.Join(hints, ", ") + "\n\n")
	}

	b.WriteString("## Guidelines\n\n")
	b.WriteString("- Follow the existing code style and conventions\n")
	b.WriteString("- Prefer editing existing files over creating new ones\n")
	b.WriteString("- Keep changes focused and minimal\n")

	// Add build/test hints if we can detect them
	if _, err := os.Stat("go.mod"); err == nil {
		b.WriteString("- Run `go vet ./...` to check for issues\n")
		b.WriteString("- Run `go build ./...` to verify compilation\n")
	}
	if _, err := os.Stat("package.json"); err == nil {
		b.WriteString("- Run `npm test` to verify changes\n")
	}
	if _, err := os.Stat("Cargo.toml"); err == nil {
		b.WriteString("- Run `cargo check` to verify compilation\n")
	}

	b.WriteString("\n## Skills\n\n")
	b.WriteString("<!-- Define reusable prompt snippets here, or create a separate skills.md file. -->\n")
	b.WriteString("<!-- Example:\n")
	b.WriteString("### /review\n")
	b.WriteString("Review the code changes for bugs, style issues, and potential improvements.\n")
	b.WriteString("-->\n")

	return b.String()
}

func (m ChatModel) handleExport(arg string) (ChatModel, tea.Cmd) {
	filename := strings.TrimSpace(arg)
	if filename == "" {
		filename = fmt.Sprintf("baryo-export-%s.md", time.Now().Format("20060102-150405"))
	}

	var data []byte
	var err error

	if filepath.Ext(filename) == ".json" {
		data, err = json.MarshalIndent(m.messages, "", "  ")
	} else {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("# Baryo — %s\n\n", m.modelName))
		for _, msg := range m.messages {
			switch msg.Role {
			case "user":
				b.WriteString("### User\n\n")
			case "assistant":
				b.WriteString("### Assistant\n\n")
			default:
				b.WriteString(fmt.Sprintf("### %s\n\n", msg.Role))
			}
			if msg.Content != nil {
				b.WriteString(*msg.Content)
			}
			b.WriteString("\n\n")
		}
		data = []byte(b.String())
	}

	if err == nil {
		err = os.WriteFile(filename, data, 0644)
	}

	if err != nil {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: fmt.Sprintf("Export failed: %v", err),
		})
	} else {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: fmt.Sprintf("Exported to %s", filename),
		})
	}
	m.updateViewport()
	return m, nil
}

func (m *ChatModel) updateViewport() {
	var b strings.Builder

	for _, entry := range m.history {
		switch entry.role {
		case "user":
			b.WriteString(UserLabelStyle.Render("You: "))
			b.WriteString(entry.content)
		case "error":
			b.WriteString(ErrorStyle.Render("Error: " + entry.content))
		case "tool":
			b.WriteString(ToolLabelStyle.Render(entry.content))
		case "assistant":
			b.WriteString(AssistantLabelStyle.Render("Assistant: "))
			if m.markdown {
				b.WriteString(RenderMarkdown(entry.content, m.width))
			} else {
				b.WriteString(StreamingStyle.Render(entry.content))
			}
		}
		b.WriteString("\n\n")
	}

	// Show spinner while a tool is running
	if m.toolStatus != "" {
		frame := spinnerFrames[m.spinFrame]
		b.WriteString(ToolLabelStyle.Render(frame+" "+m.toolStatus) + "\n")
	}

	// Show streaming text
	if m.isStream && m.streaming != "" {
		b.WriteString(AssistantLabelStyle.Render("Assistant: "))
		if m.markdown {
			b.WriteString(RenderMarkdown(m.streaming, m.width))
		} else {
			b.WriteString(StreamingStyle.Render(m.streaming))
		}
		b.WriteString("\n")
	}

	wrapped := lipgloss.NewStyle().Width(m.width).Render(b.String())
	m.viewport.SetContent(wrapped)
	m.viewport.GotoBottom()
}

func (m ChatModel) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}

	header := TitleStyle.Render(
		fmt.Sprintf("🐳 Baryo — chatting with %s", m.modelName))

	frame := spinnerFrames[m.spinFrame]
	var status string
	if m.toolStatus != "" {
		status = ToolLabelStyle.Render(frame+" "+m.toolStatus)
	} else if m.isStream {
		status = HelpStyle.Render(frame + " thinking...")
	} else {
		status = HelpStyle.Render("enter send • ↑↓ scroll • ctrl+p/n history • ctrl+c quit")
	}

	return fmt.Sprintf("%s\n%s\n%s\n%s",
		header,
		m.viewport.View(),
		m.textarea.View(),
		status,
	)
}

// waitForEvent returns a Cmd that waits for the next event from the channel.
func waitForEvent(ch <-chan docker.StreamEvent) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return StreamTokenMsg{Event: docker.StreamEvent{Done: true}}
		}
		return StreamTokenMsg{Event: evt}
	}
}

// summarizeToolResult returns a short preview of a tool result for the TUI.
func summarizeToolResult(content string, isError bool) string {
	if isError {
		return "error: " + content
	}
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) == 0 {
		return "done (empty)"
	}
	const maxPreviewLines = 6
	if len(lines) <= maxPreviewLines {
		return strings.TrimSpace(content)
	}
	preview := strings.Join(lines[:maxPreviewLines], "\n")
	return fmt.Sprintf("%s\n... (%d more lines)", preview, len(lines)-maxPreviewLines)
}

// toDockerToolDefs converts tools.Definition slice to docker.ToolDefinition slice.
func toDockerToolDefs(defs []tools.Definition) []docker.ToolDefinition {
	out := make([]docker.ToolDefinition, len(defs))
	for i, d := range defs {
		out[i] = docker.ToolDefinition{
			Type: d.Type,
			Function: docker.FunctionDefinition{
				Name:        d.Function.Name,
				Description: d.Function.Description,
				Parameters:  d.Function.Parameters,
			},
		}
	}
	return out
}
