// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/arnelirobles/baryo-cli/internal/llm"
	"github.com/arnelirobles/baryo-cli/internal/mcp"
	"github.com/arnelirobles/baryo-cli/internal/session"
	"github.com/arnelirobles/baryo-cli/internal/setup"
)

type screen int

const (
	screenLoading screen = iota
	screenModelSelect
	screenModelLoading // waiting for remote model to load into memory
	screenChat
	screenSessionSelect
	screenModelBrowser
)

// AppModel is the top-level bubbletea model that drives screen transitions.
type AppModel struct {
	screen       screen
	spinner      spinner.Model
	socketPath     string
	systemPrompt   string
	memoriesPrompt string
	params         llm.ChatParams

	searchProvider   string
	searchAPIKey     string
	permissionMode   string // "auto", "confirm", "suggest"
	providerKeys     map[string]string

	rewrite          bool               // prompt rewrite pass enabled
	mcpInReadOnly    bool               // allow MCP tools in read-only modes
	exportPath       string             // default directory for /export output

	mcpManager       MCPManager         // MCP server manager (nil if no servers configured)
	mcpConfigs       []mcp.ServerConfig // deferred MCP server configs for async startup

	preselectedModel *llm.Model
	resumeSession    *session.Session
	sessionList      []session.Summary

	modelSelect   ModelSelectModel
	sessionSelect SessionSelectModel
	modelBrowser  ModelBrowserModel
	chat          ChatModel

	pendingModel       *llm.Model // model waiting to be loaded
	loadStart          time.Time  // when model loading began
	pendingSetupPrompt bool       // true when first-run setup prompt should be shown

	err    error
	width  int
	height int
}

// AppOption configures the AppModel before it starts.
type AppOption func(*AppModel)

// WithSocketPath sets the Docker Model Runner socket path.
func WithSocketPath(path string) AppOption {
	return func(a *AppModel) {
		a.socketPath = path
	}
}

// WithPreselectedModel skips the model picker and goes straight to chat.
func WithPreselectedModel(model llm.Model) AppOption {
	return func(a *AppModel) {
		a.preselectedModel = &model
	}
}

// WithSession resumes a saved session, skipping model loading and picker.
func WithSession(sess *session.Session) AppOption {
	return func(a *AppModel) {
		a.resumeSession = sess
	}
}

// WithSystemPrompt sets the system prompt for chat sessions.
func WithSystemPrompt(prompt string) AppOption {
	return func(a *AppModel) {
		a.systemPrompt = prompt
	}
}

// WithMemories sets the formatted memories prompt for prominent injection.
func WithMemories(memories string) AppOption {
	return func(a *AppModel) {
		a.memoriesPrompt = memories
	}
}

// WithParams sets the model parameters for chat sessions.
func WithParams(params llm.ChatParams) AppOption {
	return func(a *AppModel) {
		a.params = params
	}
}

// WithSearchConfig sets the web search provider and API key.
func WithSearchConfig(provider, apiKey string) AppOption {
	return func(a *AppModel) {
		a.searchProvider = provider
		a.searchAPIKey = apiKey
	}
}

// WithPermissionMode sets the permission mode for destructive tool calls.
func WithPermissionMode(mode string) AppOption {
	return func(a *AppModel) {
		a.permissionMode = mode
	}
}

// WithProviderKeys sets API keys for cloud model providers.
func WithProviderKeys(keys map[string]string) AppOption {
	return func(a *AppModel) {
		a.providerKeys = keys
	}
}

// WithRewrite enables or disables the prompt rewrite pass.
func WithRewrite(enabled bool) AppOption {
	return func(a *AppModel) {
		a.rewrite = enabled
	}
}

// WithMCPInReadOnly enables or disables MCP tools in read-only modes.
func WithMCPInReadOnly(enabled bool) AppOption {
	return func(a *AppModel) {
		a.mcpInReadOnly = enabled
	}
}

// WithExportPath sets the default directory for /export output.
func WithExportPath(path string) AppOption {
	return func(a *AppModel) {
		a.exportPath = path
	}
}

// WithMCPManager sets an already-started MCP server manager.
func WithMCPManager(mgr MCPManager) AppOption {
	return func(a *AppModel) {
		a.mcpManager = mgr
	}
}

// WithMCPConfigs sets MCP server configs for async startup during TUI loading.
func WithMCPConfigs(configs []mcp.ServerConfig) AppOption {
	return func(a *AppModel) {
		a.mcpConfigs = configs
	}
}

// WithSessionList starts on the session picker screen.
func WithSessionList(summaries []session.Summary) AppOption {
	return func(a *AppModel) {
		a.sessionList = summaries
	}
}

// NewApp creates the initial application model.
func NewApp(opts ...AppOption) AppModel {
	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("183"))),
	)
	m := AppModel{
		screen:  screenLoading,
		spinner: s,
		rewrite:       true,
		mcpInReadOnly: true,
	}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

func (m AppModel) Init() tea.Cmd {
	// If resuming a specific session, skip everything
	if m.resumeSession != nil {
		return func() tea.Msg {
			return SessionLoadedMsg{Session: m.resumeSession}
		}
	}
	// If showing session picker, skip model loading
	if m.sessionList != nil {
		return func() tea.Msg {
			return ShowSessionsMsg{Sessions: m.sessionList}
		}
	}
	cmds := []tea.Cmd{m.spinner.Tick, m.loadModels(), m.checkFirstRunSetup()}
	if len(m.mcpConfigs) > 0 {
		cmds = append(cmds, m.startMCPServers())
	}
	return tea.Batch(cmds...)
}

// MCPReadyMsg signals that MCP servers have finished starting.
type MCPReadyMsg struct {
	Manager *mcp.Manager
	Errors  []error
}

// checkFirstRunSetup returns a Cmd that checks if first-run skill setup is needed.
func (m AppModel) checkFirstRunSetup() tea.Cmd {
	return func() tea.Msg {
		if setup.NeedsSetup() {
			return SetupPromptMsg{}
		}
		return nil
	}
}

// startMCPServers starts MCP server connections in the background.
func (m AppModel) startMCPServers() tea.Cmd {
	configs := m.mcpConfigs
	return func() tea.Msg {
		mgr := mcp.NewManager()
		errs := mgr.Start(context.Background(), configs)
		return MCPReadyMsg{Manager: mgr, Errors: errs}
	}
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

	// Global message handlers (can arrive from any screen)
	case SetupPromptMsg:
		if m.screen == screenChat {
			m.chat.setupPromptPending = true
			m.chat.showSetupPrompt()
		} else {
			m.pendingSetupPrompt = true
		}
		return m, nil

	case MCPReadyMsg:
		m.mcpManager = msg.Manager
		// If chat is already active, inject the MCP manager into it.
		if m.screen == screenChat {
			m.chat.mcpManager = msg.Manager
		}
		return m, nil

	case SessionLoadedMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		m.screen = screenChat
		m.chat = NewChatFromSession(m.socketPath, m.systemPrompt, m.memoriesPrompt, m.params, msg.Session, m.searchProvider, m.searchAPIKey, m.permissionMode, m.providerKeys, m.rewrite, m.mcpInReadOnly, m.mcpManager)
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(tea.WindowSizeMsg{
			Width:  m.width,
			Height: m.height,
		})
		return m, cmd

	case ShowSessionsMsg:
		m.screen = screenSessionSelect
		m.sessionSelect = NewSessionSelect(msg.Sessions)
		return m, nil

	case SessionCancelledMsg:
		m.screen = screenChat
		return m, nil

	case ShowModelsMsg:
		if msg.Err != nil {
			if m.screen == screenChat {
				m.chat.history = append(m.chat.history, chatEntry{
					role:    roleError,
					content: fmt.Sprintf("Models error: %v", msg.Err),
				})
				m.chat.updateViewport()
			} else {
				m.err = msg.Err
			}
			return m, nil
		}
		m.screen = screenModelBrowser
		m.modelBrowser = NewModelBrowser(msg.Downloaded, msg.Available)
		return m, nil

	case ModelBrowserCancelMsg:
		m.screen = screenChat
		return m, nil
	}

	switch m.screen {
	case screenLoading:
		return m.updateLoading(msg)
	case screenModelSelect:
		return m.updateModelSelect(msg)
	case screenModelLoading:
		return m.updateModelLoading(msg)
	case screenChat:
		return m.updateChat(msg)
	case screenSessionSelect:
		return m.updateSessionSelect(msg)
	case screenModelBrowser:
		return m.updateModelBrowser(msg)
	}

	return m, nil
}

func (m AppModel) updateLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ModelsLoadedMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		if m.preselectedModel != nil {
			return m.startModelLoading(*m.preselectedModel)
		}
		m.screen = screenModelSelect
		m.modelSelect = NewModelSelect(msg.Models)
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m AppModel) updateModelSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ModelSelectedMsg:
		return m.startModelLoading(msg.Model)
	default:
		var cmd tea.Cmd
		m.modelSelect, cmd = m.modelSelect.Update(msg)
		return m, cmd
	}
}

func (m AppModel) updateModelLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ModelPreloadedMsg:
		if msg.Err != nil {
			// Non-fatal: proceed to chat anyway, model may still work.
		}
		cmd := m.transitionToChat(*m.pendingModel)
		m.pendingModel = nil
		return m, cmd
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m AppModel) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.chat, cmd = m.chat.Update(msg)
	return m, cmd
}

func (m AppModel) updateSessionSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.sessionSelect, cmd = m.sessionSelect.Update(msg)
	return m, cmd
}

func (m AppModel) updateModelBrowser(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ModelSelectedMsg:
		return m.startModelLoading(msg.Model)
	default:
		var cmd tea.Cmd
		m.modelBrowser, cmd = m.modelBrowser.Update(msg)
		return m, cmd
	}
}

func (m AppModel) View() string {
	switch m.screen {
	case screenLoading:
		if m.err != nil {
			return ErrorStyle.Render("Error: "+m.err.Error()) + "\n\n" +
				HelpStyle.Render("ctrl+c to quit")
		}
		return m.spinner.View() + " " + HelpStyle.Render("loading models...")

	case screenModelSelect:
		return m.modelSelect.View()

	case screenModelLoading:
		elapsed := time.Since(m.loadStart).Truncate(time.Second)
		name := ""
		if m.pendingModel != nil {
			name = m.pendingModel.Name
		}
		return fmt.Sprintf("\n  %s %s (%s)\n\n  %s",
			m.spinner.View(),
			HelpStyle.Render("loading "+name+" into memory..."),
			HelpStyle.Render(elapsed.String()),
			DimStyle.Render("this may take a few minutes for large models"))

	case screenChat:
		return m.chat.View()

	case screenSessionSelect:
		return m.sessionSelect.View()

	case screenModelBrowser:
		return m.modelBrowser.View()
	}

	return ""
}

// loadModels returns a Cmd that fetches the model list.
// For TCP/remote connections, it queries the server API instead of docker CLI.
// Also appends models from configured cloud providers (Gemini, OpenRouter).
func (m AppModel) loadModels() tea.Cmd {
	socketPath := m.socketPath
	keys := m.providerKeys
	return func() tea.Msg {
		var models []llm.Model
		if llm.IsRemoteSocket(socketPath) {
			if m, err := llm.ListRemoteModels(socketPath); err == nil {
				models = append(models, m...)
			}
		} else {
			if m, err := llm.ListModels(); err == nil {
				models = append(models, m...)
			}
		}

		// Append cloud provider models (non-fatal on error).
		for provider, key := range keys {
			if key == "" {
				continue
			}
			if pm, e := llm.ListProviderModels(provider, key); e == nil {
				models = append(models, pm...)
			}
		}

		if len(models) == 0 {
			return ModelsLoadedMsg{Err: fmt.Errorf("no models available — configure a cloud provider or install Docker")}
		}

		return ModelsLoadedMsg{Models: models}
	}
}

// ModelPreloadedMsg signals that the remote model has been loaded.
type ModelPreloadedMsg struct {
	Err error
}

// preloadModel returns a Cmd that loads a model on a remote Ollama server.
func preloadModel(socketPath, modelTag string) tea.Cmd {
	return func() tea.Msg {
		err := llm.PreloadModel(socketPath, modelTag)
		return ModelPreloadedMsg{Err: err}
	}
}

// transitionToChat sets up the chat screen for the given model.
func (m *AppModel) transitionToChat(model llm.Model) tea.Cmd {
	m.screen = screenChat
	m.chat = NewChat(m.socketPath, m.systemPrompt, m.memoriesPrompt, m.params, model, m.searchProvider, m.searchAPIKey, m.permissionMode, m.providerKeys, m.rewrite, m.mcpInReadOnly, m.mcpManager)
	m.chat.exportPath = m.exportPath
	if m.pendingSetupPrompt {
		m.pendingSetupPrompt = false
		m.chat.setupPromptPending = true
		m.chat.showSetupPrompt()
	}
	var cmd tea.Cmd
	m.chat, cmd = m.chat.Update(tea.WindowSizeMsg{
		Width:  m.width,
		Height: m.height,
	})
	return cmd
}

// startModelLoading transitions to the loading screen for remote models,
// or directly to chat for local/cloud-provider models.
func (m *AppModel) startModelLoading(model llm.Model) (tea.Model, tea.Cmd) {
	// Cloud provider models don't need preloading.
	if model.Provider != "" {
		cmd := m.transitionToChat(model)
		return *m, cmd
	}
	if llm.IsRemoteSocket(m.socketPath) {
		m.screen = screenModelLoading
		m.pendingModel = &model
		m.loadStart = time.Now()
		return *m, tea.Batch(m.spinner.Tick, preloadModel(m.socketPath, model.Tag))
	}
	cmd := m.transitionToChat(model)
	return *m, cmd
}

