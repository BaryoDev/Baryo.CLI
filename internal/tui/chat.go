// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/arnelirobles/baryo-cli/internal/docker"
)

// ChatModel is the chat conversation screen.
type ChatModel struct {
	socketPath string // unix socket for Docker Model Runner
	modelName  string // display name (e.g. "ai/mistral")
	modelTag   string // full tag for API calls (e.g. "docker.io/ai/mistral:latest")
	messages   []docker.ChatMessage
	history    []chatEntry // rendered conversation history
	streaming  string      // current streaming text accumulator
	isStream   bool        // whether we are currently streaming

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
func NewChat(socketPath, modelName, modelTag string) ChatModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.Focus()
	ta.SetHeight(3)
	ta.SetWidth(80)
	ta.ShowLineNumbers = false
	ta.CharLimit = 4096

	return ChatModel{
		socketPath: socketPath,
		modelName:  modelName,
		modelTag:   modelTag,
		textarea:   ta,
	}
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
			m.tokenCh = docker.StreamChat(ctx, m.socketPath, m.modelTag, m.messages)

			m.updateViewport()
			return m, waitForToken(m.tokenCh)
		}

	case StreamTokenMsg:
		if msg.Done {
			// Streaming complete — render markdown and save
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
