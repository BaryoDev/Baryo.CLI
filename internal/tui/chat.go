// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/arnelirobles/baryo-cli/internal/docker"
	"github.com/arnelirobles/baryo-cli/internal/session"
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
	isStream     bool        // whether we are currently streaming
	session      *session.Session

	textarea textarea.Model
	viewport viewport.Model
	ready    bool
	width    int
	height   int

	tokenCh    <-chan string
	cancelFunc context.CancelFunc
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
		session:      sess,
	}
}

// NewChatFromSession restores a chat screen from a saved session.
func NewChatFromSession(socketPath, systemPrompt string, params docker.ChatParams, sess *session.Session) ChatModel {
	ta := newTextarea()
	history := make([]chatEntry, len(sess.Messages))
	for i, m := range sess.Messages {
		history[i] = chatEntry{role: m.Role, content: m.Content}
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
		session:      sess,
	}
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

		if msg.String() == "enter" && !m.isStream {
			text := strings.TrimSpace(m.textarea.Value())
			if text == "" {
				break
			}

			m.textarea.Reset()

			// Handle slash commands
			if strings.HasPrefix(text, "/") {
				return m.handleCommand(text)
			}

			// Add user message
			m.messages = append(m.messages, docker.ChatMessage{
				Role:    "user",
				Content: text,
			})
			m.history = append(m.history, chatEntry{
				role:    "user",
				content: text,
			})

			// Start streaming
			m.isStream = true
			m.streaming = ""

			ctx, cancel := context.WithCancel(context.Background())
			m.cancelFunc = cancel
			m.tokenCh = docker.StreamChat(ctx, m.socketPath, m.modelTag, m.buildMessages(), m.params)

			m.updateViewport()
			return m, waitForToken(m.tokenCh)
		}

	case StreamTokenMsg:
		if msg.Done {
			// Streaming complete — save to history and auto-save session
			rendered := m.streaming
			m.history = append(m.history, chatEntry{
				role:    "assistant",
				content: rendered,
			})
			m.messages = append(m.messages, docker.ChatMessage{
				Role:    "assistant",
				Content: m.streaming,
			})
			m.streaming = ""
			m.isStream = false
			m.cancelFunc = nil
			m.tokenCh = nil
			m.saveSession()
			m.updateViewport()
			return m, nil
		}

		m.streaming += msg.Token
		m.updateViewport()
		return m, waitForToken(m.tokenCh)
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
			content: fmt.Sprintf("Unknown command: %s\nAvailable: /clear, /sessions, /resume, /models, /system, /params", text),
		})
		m.updateViewport()
		return m, nil
	}
}

// buildMessages prepends the system prompt to the conversation messages.
func (m *ChatModel) buildMessages() []docker.ChatMessage {
	if m.systemPrompt == "" {
		return m.messages
	}
	msgs := make([]docker.ChatMessage, 0, len(m.messages)+1)
	msgs = append(msgs, docker.ChatMessage{Role: "system", Content: m.systemPrompt})
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

func (m *ChatModel) updateViewport() {
	var b strings.Builder

	for _, entry := range m.history {
		switch entry.role {
		case "user":
			b.WriteString(UserLabelStyle.Render("You: "))
			b.WriteString(entry.content)
		case "assistant":
			b.WriteString(AssistantLabelStyle.Render("Assistant: "))
			b.WriteString(StreamingStyle.Render(entry.content))
		}
		b.WriteString("\n\n")
	}

	// Show streaming text
	if m.isStream && m.streaming != "" {
		b.WriteString(AssistantLabelStyle.Render("Assistant: "))
		b.WriteString(StreamingStyle.Render(m.streaming))
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

	var status string
	if m.isStream {
		status = HelpStyle.Render("streaming...")
	} else {
		status = HelpStyle.Render("enter send • ctrl+c quit")
	}

	return fmt.Sprintf("%s\n%s\n%s\n%s",
		header,
		m.viewport.View(),
		m.textarea.View(),
		status,
	)
}

// waitForToken returns a Cmd that waits for the next token from the channel.
func waitForToken(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		token, ok := <-ch
		if !ok {
			return StreamTokenMsg{Done: true}
		}
		return StreamTokenMsg{Token: token}
	}
}
