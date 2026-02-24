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
	"os/exec"
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
	"github.com/arnelirobles/baryo-cli/internal/search"
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

	eventCh      <-chan docker.StreamEvent
	cancelFunc   context.CancelFunc
	toolStatus   string    // shown in status bar during tool execution
	initPending  bool      // when true, write streaming result to BARYO.md on completion
	thinking     bool      // true while model is inside a <think> block
	streamStart  time.Time // when streaming began (for elapsed time display)

	// @ mention completion
	mention mentionCompletion

	// Web search
	searchProvider string // duckduckgo, brave, tavily
	searchAPIKey   string // API key for brave/tavily

	// Context window management
	contextTokens  int  // estimated token count after last turn
	contextLimit   int  // max context window (default 8192)
	compactPending bool // true during compaction streaming
	compactKeep    int  // number of messages to keep during compaction
}

// chatEntry is a rendered message in the history.
type chatEntry struct {
	role    string
	content string
}

// NewChat creates a new chat screen for the given model.
func NewChat(socketPath, systemPrompt string, params docker.ChatParams, modelName, modelTag, searchProvider, searchAPIKey string) ChatModel {
	ta := newTextarea()
	sess, _ := session.New(modelName, modelTag)
	return ChatModel{
		socketPath:     socketPath,
		systemPrompt:   systemPrompt,
		params:         params,
		modelName:      modelName,
		modelTag:       modelTag,
		textarea:       ta,
		markdown:       true,
		historyIdx:     -1,
		session:        sess,
		contextLimit:   8192,
		searchProvider: searchProvider,
		searchAPIKey:   searchAPIKey,
	}
}

// NewChatFromSession restores a chat screen from a saved session.
func NewChatFromSession(socketPath, systemPrompt string, params docker.ChatParams, sess *session.Session, searchProvider, searchAPIKey string) ChatModel {
	ta := newTextarea()
	history := make([]chatEntry, len(sess.Messages))
	for i, m := range sess.Messages {
		c := ""
		if m.Content != nil {
			c = *m.Content
		}
		history[i] = chatEntry{role: m.Role, content: c}
	}
	msgs := append([]docker.ChatMessage{}, sess.Messages...)
	cm := ChatModel{
		socketPath:     socketPath,
		systemPrompt:   systemPrompt,
		params:         params,
		modelName:      sess.ModelName,
		modelTag:       sess.ModelTag,
		messages:       msgs,
		history:        history,
		textarea:       ta,
		markdown:       true,
		historyIdx:     -1,
		session:        sess,
		contextLimit:   8192,
		searchProvider: searchProvider,
		searchAPIKey:   searchAPIKey,
	}
	cm.contextTokens = estimateTokens(cm.buildMessages())
	return cm
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

		// @-mention completion: Tab/Shift+Tab cycle, Enter selects, Esc cancels
		if m.mention.active && !m.isStream {
			switch msg.String() {
			case "tab", "shift+tab":
				m.handleMentionTab(msg.String() == "tab")
				return m, nil
			case "enter":
				m.handleMentionSelect()
				return m, nil
			case "escape":
				m.mention = mentionCompletion{}
				return m, nil
			}
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

			// Process @mentions — inject file contents as context
			_, fileContexts, mentionErrors := m.processAtMentions(text)
			for _, fc := range fileContexts {
				contextMsg := fmt.Sprintf("[File: %s]\n\n%s", fc.path, fc.content)
				m.messages = append(m.messages, docker.NewChatMessage("user", contextMsg))
				m.history = append(m.history, chatEntry{
					role:    "tool",
					content: fmt.Sprintf("Attached: %s (%d lines)", fc.path, fc.lines),
				})
			}
			for _, errMsg := range mentionErrors {
				m.history = append(m.history, chatEntry{
					role:    "error",
					content: errMsg,
				})
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
			m.streamStart = time.Now()

			ctx, cancel := context.WithCancel(context.Background())
			m.cancelFunc = cancel

			var toolDefs []docker.ToolDefinition
			executor := func(ctx context.Context, name, argsJSON string) (string, bool) {
				r := tools.Execute(ctx, name, argsJSON)
				return r.Content, r.IsError
			}
			if needsTools(text) {
				toolDefs = toDockerToolDefs(tools.AllDefinitions())
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
			// Compaction complete — replace old messages with summary + recent
			if m.compactPending {
				summary := m.streaming
				if summary == "" {
					summary = m.turnContent
				}
				m.compactPending = false
				m.streaming = ""
				m.turnContent = ""
				m.isStream = false
				m.cancelFunc = nil
				m.eventCh = nil
				m.toolStatus = ""

				if summary != "" {
					m.messages = append(
						[]docker.ChatMessage{
							docker.NewChatMessage("user", "[Conversation summary]\n\n"+summary),
							docker.NewChatMessage("assistant", "Understood, I have the context from our earlier conversation."),
						},
						m.messages[m.compactKeep:]...,
					)
				}
				m.contextTokens = estimateTokens(m.buildMessages())
				m.history = append(m.history, chatEntry{
					role:    "assistant",
					content: fmt.Sprintf("Context compacted. ~%s / %s tokens", formatTokenCount(m.contextTokens), formatTokenCount(m.contextLimit)),
				})
				m.saveSession()
				m.updateViewport()
				return m, nil
			}

			// Streaming complete — save to history and auto-save session
			if m.streaming != "" {
				cleaned, _ := stripThinkBlock(m.streaming)
				if cleaned != "" {
					m.history = append(m.history, chatEntry{
						role:    "assistant",
						content: cleaned,
					})
				}
				m.turnContent += cleaned
			}
			m.thinking = false
			// Commit the full turn as one assistant message (avoids consecutive assistant roles).
			if m.turnContent != "" {
				m.messages = append(m.messages, docker.NewChatMessage("assistant", m.turnContent))
			}

			// Write BARYO.md if /init was pending
			if m.initPending && m.turnContent != "" {
				m.initPending = false
				if err := os.WriteFile("BARYO.md", []byte(m.turnContent), 0644); err != nil {
					m.history = append(m.history, chatEntry{
						role:    "error",
						content: fmt.Sprintf("Failed to write BARYO.md: %v", err),
					})
				} else {
					m.history = append(m.history, chatEntry{
						role:    "assistant",
						content: "Saved to BARYO.md",
					})
				}
			}

			m.streaming = ""
			m.turnContent = ""
			m.isStream = false
			m.cancelFunc = nil
			m.eventCh = nil
			m.toolStatus = ""
			m.contextTokens = estimateTokens(m.buildMessages())
			m.saveSession()
			m.updateViewport()

			// Auto-compaction: trigger if over 85% of context limit
			if m.contextTokens > int(float64(m.contextLimit)*0.85) && len(m.messages) > 8 {
				return m.startCompaction()
			}

			return m, nil
		}

		if evt.Error != "" {
			// If we had partial content, keep it in history
			if m.streaming != "" {
				cleaned, _ := stripThinkBlock(m.streaming)
				if cleaned != "" {
					m.history = append(m.history, chatEntry{
						role:    "assistant",
						content: cleaned,
					})
				}
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
				cleaned, _ := stripThinkBlock(m.streaming)
				if cleaned != "" {
					m.history = append(m.history, chatEntry{
						role:    "assistant",
						content: cleaned,
					})
					m.turnContent += cleaned
				}
				m.streaming = ""
				m.thinking = false
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
			_, m.thinking = stripThinkBlock(m.streaming)
			m.updateViewport()
		}
		return m, waitForEvent(m.eventCh)

	case SearchResultMsg:
		if msg.Err != nil {
			m.history = append(m.history, chatEntry{
				role:    "error",
				content: fmt.Sprintf("Search error: %v", msg.Err),
			})
			m.updateViewport()
			return m, nil
		}
		// Inject into model context as a user message
		contextMsg := fmt.Sprintf("[Web search results for %q]\n\n%s", msg.Query, msg.Results)
		m.messages = append(m.messages, docker.NewChatMessage("user", contextMsg))
		// Display results with tool styling
		m.history = append(m.history, chatEntry{
			role:    "tool",
			content: fmt.Sprintf("Search: %s\n\n%s", msg.Query, msg.Results),
		})
		m.contextTokens = estimateTokens(m.buildMessages())
		m.saveSession()
		m.updateViewport()
		return m, nil

	case FetchResultMsg:
		if msg.Err != nil {
			m.history = append(m.history, chatEntry{
				role:    "error",
				content: fmt.Sprintf("Fetch error: %v", msg.Err),
			})
			m.updateViewport()
			return m, nil
		}
		// Inject into model context as a user message
		m.messages = append(m.messages, docker.NewChatMessage("user", msg.Content))
		// Display results with tool styling
		preview := msg.Content
		if len(preview) > 2000 {
			preview = preview[:2000] + "\n... (truncated in display)"
		}
		m.history = append(m.history, chatEntry{
			role:    "tool",
			content: preview,
		})
		m.contextTokens = estimateTokens(m.buildMessages())
		m.saveSession()
		m.updateViewport()
		return m, nil
	}

	// Update sub-components
	var cmd tea.Cmd

	if !m.isStream {
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
		// Live @-mention preview: update candidates as user types
		m.updateMentionPreview()
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
		m.contextTokens = estimateTokens(m.buildMessages())
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
		socketPath := m.socketPath
		return m, func() tea.Msg {
			var downloaded []docker.DockerModel
			var dlErr error
			if isRemoteSocket(socketPath) {
				downloaded, dlErr = docker.ListRemoteModels(socketPath)
			} else {
				downloaded, dlErr = docker.ListModels()
			}
			if dlErr != nil {
				return ShowModelsMsg{Err: dlErr}
			}
			if isRemoteSocket(socketPath) {
				// No Docker Hub search for remote servers
				return ShowModelsMsg{Downloaded: downloaded}
			}
			available, srErr := docker.SearchModels()
			if srErr != nil {
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

	case "/context":
		sysMsgs := m.buildMessages()[:1] // system prompt only
		sysTokens := estimateTokens(sysMsgs)
		convTokens := estimateTokens(m.messages)
		total := sysTokens + convTokens
		pct := 0
		if m.contextLimit > 0 {
			pct = total * 100 / m.contextLimit
		}
		m.contextTokens = total
		m.history = append(m.history, chatEntry{
			role: "assistant",
			content: fmt.Sprintf("System prompt:   ~%s tokens\nConversation:    ~%s tokens (%d messages)\nTotal estimated: ~%s / %s (%d%%)",
				formatTokenCount(sysTokens),
				formatTokenCount(convTokens),
				len(m.messages),
				formatTokenCount(total),
				formatTokenCount(m.contextLimit),
				pct,
			),
		})
		m.updateViewport()
		return m, nil

	case "/compact":
		return m.startCompaction()

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

		if strings.HasPrefix(text, "/search ") {
			return m.handleSearch(strings.TrimPrefix(text, "/search "))
		}

		if strings.HasPrefix(text, "/fetch ") {
			return m.handleFetch(strings.TrimPrefix(text, "/fetch "))
		}

		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: fmt.Sprintf("Unknown command: %s\nAvailable: /clear, /sessions, /resume, /models, /system, /params, /export, /copy, /markdown, /doctor, /init, /context, /compact, /search, /fetch", text),
		})
		m.updateViewport()
		return m, nil
	}
}

// toolSystemPrompt is injected to instruct the model to use available tools.
//
//go:embed prompts/tools.md
var toolSystemPrompt string

// compactPromptTemplate is the prompt used to summarize older messages.
//
//go:embed prompts/compact.md
var compactPromptTemplate string

// estimateTokens returns a rough token count for a set of messages.
// Uses the chars/4 heuristic plus per-message overhead.
func estimateTokens(messages []docker.ChatMessage) int {
	total := 0
	for _, m := range messages {
		if m.Content != nil {
			total += len(*m.Content)/4 + 4
		} else {
			total += 4 // per-message overhead even without content
		}
	}
	return total
}

// buildMessages prepends the system prompt to the conversation messages.
func (m *ChatModel) buildMessages() []docker.ChatMessage {
	msgs := make([]docker.ChatMessage, 0, len(m.messages)+2)

	// Directives first (short, high-priority), then user/project context.
	sysPrompt := toolSystemPrompt
	if m.systemPrompt != "" {
		sysPrompt = toolSystemPrompt + "\n\n" + m.systemPrompt
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
	// Check if BARYO.md already exists
	if _, err := os.Stat("BARYO.md"); err == nil {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: "BARYO.md already exists. Edit it directly or delete it and run /init again.",
		})
		m.updateViewport()
		return m, nil
	}

	// Gather project context for the model
	projectContext := gatherProjectContext()

	prompt := fmt.Sprintf(`Analyze this project and generate a BARYO.md file with project-specific instructions for an AI coding assistant.

The file should include:
- A heading with the project name
- A one-line description of the project
- The tech stack (languages, frameworks, build tools)
- Key directories and what they contain
- Coding guidelines specific to this project (style, patterns, conventions you observe)
- Build, test, and lint commands
- A Skills section with useful slash command definitions (e.g. /review, /commit, /test)

Write ONLY the markdown content for BARYO.md. No explanation before or after.

<project-context>
%s
</project-context>`, projectContext)

	// Inject as a user message and start streaming
	m.messages = append(m.messages, docker.NewChatMessage("user", prompt))
	m.history = append(m.history, chatEntry{
		role:    "user",
		content: "/init",
	})

	m.isStream = true
	m.streaming = ""
	m.turnContent = ""
	m.toolStatus = ""
	m.initPending = true

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

// gatherProjectContext collects project files and structure for the /init prompt.
func gatherProjectContext() string {
	var b strings.Builder

	// Directory listing
	b.WriteString("## Directory listing (top-level)\n\n")
	entries, _ := os.ReadDir(".")
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			b.WriteString(name + "/\n")
		} else {
			b.WriteString(name + "\n")
		}
	}

	// Subdirectories one level deep for key dirs
	keyDirs := []string{"internal", "cmd", "pkg", "src", "lib", "app", "api", "docs"}
	for _, dir := range keyDirs {
		if sub, err := os.ReadDir(dir); err == nil {
			b.WriteString(fmt.Sprintf("\n## %s/ contents\n\n", dir))
			for _, e := range sub {
				name := e.Name()
				if e.IsDir() {
					b.WriteString("  " + name + "/\n")
				} else {
					b.WriteString("  " + name + "\n")
				}
			}
		}
	}

	// Key project files
	projectFiles := []string{
		"README.md", "go.mod", "package.json", "Cargo.toml",
		"pyproject.toml", "Makefile", "Dockerfile",
		".goreleaser.yml", ".goreleaser.yaml",
	}

	for _, f := range projectFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		content := string(data)
		// Truncate large files
		if len(content) > 3000 {
			content = content[:3000] + "\n... (truncated)"
		}
		b.WriteString(fmt.Sprintf("\n## %s\n\n```\n%s\n```\n", f, content))
	}

	// Recent git log
	if out, err := gitOutput("log", "--oneline", "-10"); err == nil {
		b.WriteString("\n## Recent commits\n\n```\n" + out + "\n```\n")
	}

	return b.String()
}

// gitOutput runs a git command and returns stdout, or error.
func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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

func (m ChatModel) handleSearch(query string) (ChatModel, tea.Cmd) {
	query = strings.TrimSpace(query)
	if query == "" {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: "Usage: /search <query>",
		})
		m.updateViewport()
		return m, nil
	}

	m.history = append(m.history, chatEntry{
		role:    "tool",
		content: fmt.Sprintf("Searching: %s...", query),
	})
	m.updateViewport()

	provider := m.searchProvider
	apiKey := m.searchAPIKey
	return m, func() tea.Msg {
		results, err := search.Query(provider, apiKey, query)
		return SearchResultMsg{Query: query, Results: results, Err: err}
	}
}

func (m ChatModel) handleFetch(rawURL string) (ChatModel, tea.Cmd) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: "Usage: /fetch <url>",
		})
		m.updateViewport()
		return m, nil
	}

	m.history = append(m.history, chatEntry{
		role:    "tool",
		content: fmt.Sprintf("Fetching: %s...", rawURL),
	})
	m.updateViewport()

	return m, func() tea.Msg {
		content, err := search.Fetch(rawURL)
		return FetchResultMsg{URL: rawURL, Content: content, Err: err}
	}
}

// needsTools returns true if the user's message likely requires tool access.
// This prevents local models from calling tools for general questions.
func needsTools(text string) bool {
	lower := strings.ToLower(text)
	keywords := []string{
		"file", "folder", "directory", "dir", "path",
		"code", "read", "show", "open", "cat", "list",
		"find", "search", "grep", "glob",
		"git", "commit", "diff", "branch", "log", "status",
		"pr", "issue", "release",
		"project", "struct", "func ", "import", "package",
		"error", "bug", "fix", "test",
		"what's in", "what is in", "how many",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// stripThinkBlock removes <think>...</think> content from streamed text.
// Returns the cleaned text and whether we're currently inside a think block.
func stripThinkBlock(s string) (cleaned string, isThinking bool) {
	// Fast path: no think tag at all.
	if !strings.Contains(s, "<think>") {
		return s, false
	}

	result := s
	for {
		start := strings.Index(result, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "</think>")
		if end == -1 {
			// Still inside a think block — strip from <think> onward.
			return strings.TrimSpace(result[:start]), true
		}
		// Remove the complete think block.
		endPos := start + end + len("</think>")
		result = result[:start] + result[endPos:]
	}
	return strings.TrimSpace(result), false
}

// formatTokenCount formats a token count for display (e.g. 3160 → "3.2k").
func formatTokenCount(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// startCompaction initiates context compaction by summarizing older messages.
func (m ChatModel) startCompaction() (ChatModel, tea.Cmd) {
	if len(m.messages) <= 8 {
		convTokens := estimateTokens(m.messages)
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: fmt.Sprintf("Nothing to compact — conversation is only ~%s tokens (%d messages).", formatTokenCount(convTokens), len(m.messages)),
		})
		m.updateViewport()
		return m, nil
	}

	// Keep last 4 user/assistant pairs (up to 8 messages).
	keep := 8
	compactKeep := len(m.messages) - keep

	// Format older messages as text for summarization.
	var convo strings.Builder
	for _, msg := range m.messages[:compactKeep] {
		role := msg.Role
		content := ""
		if msg.Content != nil {
			content = *msg.Content
		}
		convo.WriteString(fmt.Sprintf("%s: %s\n\n", role, content))
	}

	prompt := fmt.Sprintf(compactPromptTemplate, convo.String())

	m.compactPending = true
	m.compactKeep = compactKeep
	m.isStream = true
	m.streaming = ""
	m.turnContent = ""
	m.toolStatus = ""
	m.streamStart = time.Now()

	m.history = append(m.history, chatEntry{
		role:    "tool",
		content: "Compacting context...",
	})

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFunc = cancel

	compactMsgs := []docker.ChatMessage{
		docker.NewChatMessage("user", prompt),
	}
	m.eventCh = docker.StreamChat(ctx, m.socketPath, m.modelTag, compactMsgs, m.params)

	m.updateViewport()
	return m, tea.Batch(waitForEvent(m.eventCh), doSpinTick())
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

	// Show streaming text (with think blocks stripped)
	if m.isStream && m.streaming != "" {
		displayText, _ := stripThinkBlock(m.streaming)
		if displayText != "" {
			b.WriteString(AssistantLabelStyle.Render("Assistant: "))
			if m.markdown {
				b.WriteString(RenderMarkdown(displayText, m.width))
			} else {
				b.WriteString(StreamingStyle.Render(displayText))
			}
			b.WriteString("\n")
		}
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
	} else if m.isStream && m.thinking {
		elapsed := time.Since(m.streamStart).Truncate(time.Second)
		status = HelpStyle.Render(fmt.Sprintf("%s reasoning... (%s)", frame, elapsed))
	} else if m.isStream {
		elapsed := time.Since(m.streamStart).Truncate(time.Second)
		status = HelpStyle.Render(fmt.Sprintf("%s thinking... (%s)", frame, elapsed))
	} else if m.mention.active && len(m.mention.candidates) > 0 {
		status = m.renderCompletionStatus()
	} else {
		help := "enter send • ↑↓ scroll • ctrl+p/n history • ctrl+c quit"
		tokenInfo := fmt.Sprintf("~%s / %s", formatTokenCount(m.contextTokens), formatTokenCount(m.contextLimit))

		// Color-code based on usage ratio.
		ratio := float64(m.contextTokens) / float64(m.contextLimit)
		var tokenStyled string
		switch {
		case ratio > 0.85:
			tokenStyled = TokenCritStyle.Render(tokenInfo)
		case ratio > 0.60:
			tokenStyled = TokenWarnStyle.Render(tokenInfo)
		default:
			tokenStyled = TokenDimStyle.Render(tokenInfo)
		}

		// Right-align the token count.
		helpWidth := lipgloss.Width(help)
		tokenWidth := lipgloss.Width(tokenInfo)
		gap := m.width - helpWidth - tokenWidth
		if gap < 2 {
			gap = 2
		}
		status = HelpStyle.Render(help) + strings.Repeat(" ", gap) + tokenStyled
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
		return fmt.Sprintf("(%d lines)\n%s", len(lines), strings.TrimSpace(content))
	}
	preview := strings.Join(lines[:maxPreviewLines], "\n")
	return fmt.Sprintf("(%d lines total)\n%s\n... (%d more lines)", len(lines), preview, len(lines)-maxPreviewLines)
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
