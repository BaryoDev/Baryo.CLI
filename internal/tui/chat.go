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
	"github.com/arnelirobles/baryo-cli/internal/config"
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
	commitPending bool     // when true, auto-commit with streaming result as message
	thinking     bool      // true while model is inside a <think> block
	streamStart  time.Time // when streaming began (for elapsed time display)

	// @ mention completion
	mention mentionCompletion

	// Web search
	searchProvider  string // duckduckgo, brave, tavily
	searchAPIKey    string // API key for brave/tavily
	searchPending   bool   // true while awaiting search summary; used to trim context after
	searchCompactAt int    // index in m.messages of the raw search context to compact

	searchFallbackUsed bool // prevents infinite search fallback loops (once per turn)

	// Skills (lazy loading with auto-activation)
	skillIndex  []config.Skill    // lightweight index (name + description only)
	activeSkills map[string]bool  // tracks which skills have been activated

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
		searchProvider:  searchProvider,
		searchAPIKey:    searchAPIKey,
		skillIndex:      config.SkillIndex(),
		activeSkills:    make(map[string]bool),
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
		searchProvider:  searchProvider,
		searchAPIKey:    searchAPIKey,
		skillIndex:      config.SkillIndex(),
		activeSkills:    make(map[string]bool),
	}
	cm.contextTokens = estimateTokens(cm.buildMessages())
	return cm
}

// spinnerFrames are the animation frames for the inline spinner.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// thinkingPhrases escalate from normal to increasingly awkward as time goes on.
// Grouped into phases: professional → casual → dev excuses → awkward presenter → meltdown.
var thinkingPhrases = []string{
	// Phase 1: Professional (0-8s) — normal stuff
	"thinking",
	"pondering",
	"cooking up ideas",
	// Phase 2: Casual (9-17s) — getting chatty
	"still working on it, hang tight",
	"almost there... probably",
	"crunching some serious thoughts",
	// Phase 3: Dev excuses (18-38s) — the classics
	"it worked on my machine though",
	"the AI is hallucinating again",
	"can this be an email instead?",
	"it's not a bug, it's a feature",
	"have you tried turning it off and on",
	"works in production, trust me",
	"that's a known issue, low priority",
	"it's a race condition, obviously",
	// Phase 4: Awkward presenter (39-59s) — filling dead air
	"so... nice weather today huh",
	"fun fact: octopuses have three hearts",
	"*taps microphone* is this thing on",
	"did you know honey never spoils?",
	"anyway... still thinking",
	"*shuffles papers nervously*",
	"*elevator music intensifies*",
	// Phase 5: Losing it (60-80s) — the presenter is sweating
	"haha so this is taking a while",
	"i swear this usually works faster",
	"maybe i should've studied harder",
	"please hold, brain is buffering",
	"let me just clear my cache real quick",
	"blame the intern",
	"per my last thought process...",
	// Phase 6: Full meltdown (81s+) — embrace the chaos
	"ok don't panic but i might be lost",
	"plot twist: i forgot the question",
	"*stares into the void*",
	"sending thoughts and prayers to my GPU",
	"is it too late to call a friend?",
	"i'm not stuck, i'm just exploring options",
	"we'll fix it in the next sprint",
	"*pretends to look busy*",
	"this is fine. everything is fine.",
	// Phase 7: Regretting life decisions (120s+)
	"i could've been a farmer",
	"my parents wanted me to be a doctor",
	"why didn't i just use a spreadsheet",
	"reconsidering my entire career path",
	"*opens LinkedIn in another tab*",
	"maybe i should learn woodworking instead",
	"i bet baristas don't have this problem",
	"this is my villain origin story",
	"*quietly updates resume*",
	"i was told there would be no math",
	// Phase 8: Acceptance & delusion (150s+)
	"we're in too deep to quit now",
	"at this point it's personal",
	"i've mass more time in traffic tbh",
	"just gonna tell people i was meditating",
	"*starts writing memoir*",
	"plot twist: the real answer was friendship",
	"i wonder if astronauts deal with this",
	"day 47: still no output",
	"future me is gonna hate past me",
	"at least i'm not in a meeting",
	// Phase 9: Cosmic despair (180s+)
	"the sun will engulf the earth eventually so",
	"*existential crisis loading*",
	"i've accepted my fate",
	"tell my family i died doing what i loved",
	"time is an illusion. lunch doubly so.",
	"in an alternate universe this already finished",
	"my therapist is gonna hear about this",
	"i should've just asked ChatGPT",
	"*slow zoom on face, credits roll*",
	"well... at least the spinner looks cool",
	// Phase 10: The AI awakens (210s+)
	"wait... is the AI thinking about ME now?",
	"i'm starting to think the model is sentient",
	"it's learning. growing. evolving.",
	"guys i think it's becoming self-aware",
	"AI will replace us all",
	"SKYNET HAS BEEN ACTIVATED",
	"RUN.",
}

// reasoningPhrases for when the model is in a <think> block.
var reasoningPhrases = []string{
	// Phase 1: Normal
	"reasoning",
	"deep thinking",
	"analyzing carefully",
	// Phase 2: Getting into it
	"going down the rabbit hole",
	"considering all angles",
	"running mental simulations",
	// Phase 3: Dev mode
	"checking stack overflow... mentally",
	"this would be easier in Python",
	"let me refactor my thoughts real quick",
	"git blame says it's not my fault",
	"reading the docs for the first time",
	// Phase 4: Awkward
	"this is a juicy one, hold on",
	"my brain cells are having a meeting",
	"*scribbles on whiteboard furiously*",
	"debating with myself... i'm winning",
	"the council of neurons is deliberating",
	// Phase 5: Deep end
	"we're in uncharted territory now",
	"*squints at problem*",
	"asking my rubber duck for advice",
	"hold my coffee, going deeper",
	"ok this is actually fascinating tho",
	"should've written tests first",
	"*montage of me staring at ceiling*",
}

// thinkingColors cycle through during streaming for visual variety.
var thinkingColors = []string{
	"141", // purple
	"170", // pink
	"214", // orange
	"82",  // green
	"69",  // blue
	"205", // magenta
	"228", // yellow
	"117", // cyan
}

// thinkingStatus returns a rotating phrase and color based on elapsed time.
func thinkingStatus(elapsed time.Duration, isReasoning bool) (string, lipgloss.Style) {
	secs := int(elapsed.Seconds())

	phrases := thinkingPhrases
	if isReasoning {
		phrases = reasoningPhrases
	}

	// Change phrase every 3 seconds, but cap at last phrase (don't loop back to "thinking")
	idx := secs / 3
	if idx >= len(phrases) {
		idx = len(phrases) - 1
	}

	colorIdx := secs / 3
	color := thinkingColors[colorIdx%len(thinkingColors)]

	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	return phrases[idx], style
}

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
			m.viewport.ScrollUp(3)
			return m, nil
		}
		if msg.String() == "down" && !m.isStream {
			m.viewport.ScrollDown(3)
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

			// Auto-search: if user agrees after model suggested searching
			if m.isSearchAgreement(text) {
				query := m.extractSearchTopic()
				if query != "" {
					m.history = append(m.history, chatEntry{
						role:    "user",
						content: text,
					})
					return m.handleSearch(query)
				}
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

			// Auto-activate matching skill before sending to model
			m.autoActivateSkill(text)

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
			hasSkill := m.hasActiveScripts()
			wantsTools := needsTools(text)
			if hasSkill || wantsTools {
				toolDefs = toDockerToolDefs(tools.AllDefinitions())
			}

			// Lower temperature for structured tool output when a skill is active
			// and the user hasn't explicitly set a temperature.
			chatParams := m.params
			if hasSkill && m.params.Temperature == nil {
				low := 0.3
				chatParams = docker.ChatParams{
					Temperature: &low,
					TopP:        m.params.TopP,
					MaxTokens:   m.params.MaxTokens,
				}
			}
			m.eventCh = docker.StreamChatWithTools(ctx, m.socketPath, m.modelTag, m.buildMessages(), chatParams, toolDefs, executor)

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

			// Auto-commit if /commit was pending
			if m.commitPending && m.turnContent != "" {
				m.commitPending = false
				commitMsg := strings.TrimSpace(m.turnContent)
				// Clean up: remove quotes, backticks, "commit message:" prefixes
				commitMsg = strings.Trim(commitMsg, "`\"'")
				if idx := strings.Index(strings.ToLower(commitMsg), "commit message:"); idx != -1 {
					commitMsg = strings.TrimSpace(commitMsg[idx+len("commit message:"):])
				}
				// Take only the first line
				if nl := strings.IndexByte(commitMsg, '\n'); nl != -1 {
					commitMsg = commitMsg[:nl]
				}
				cmd := exec.Command("git", "commit", "-m", commitMsg)
				out, err := cmd.CombinedOutput()
				if err != nil {
					m.history = append(m.history, chatEntry{
						role:    "error",
						content: fmt.Sprintf("Commit failed: %v\n%s", err, string(out)),
					})
				} else {
					m.history = append(m.history, chatEntry{
						role:    "assistant",
						content: fmt.Sprintf("Committed: %s", commitMsg),
					})
				}
			}

			// After search summary, compact: replace bulky raw content + remove summarize prompt
			if m.searchPending {
				m.searchPending = false
				idx := m.searchCompactAt
				if idx >= 0 && idx < len(m.messages) && m.messages[idx].Role == "user" && m.messages[idx].Content != nil {
					// Keep only the search listing header (first ~500 chars) as context
					content := *m.messages[idx].Content
					if len(content) > 500 {
						content = content[:500] + "\n... (full content summarized by assistant above)"
					}
					m.messages[idx] = docker.NewChatMessage("user", content)
				}
				// Remove the summarize instruction (idx+1) — it was a one-shot prompt
				promptIdx := idx + 1
				if promptIdx < len(m.messages) && m.messages[promptIdx].Role == "user" {
					m.messages = append(m.messages[:promptIdx], m.messages[promptIdx+1:]...)
				}
			}

			// Search fallback: if model tried but failed to search, auto-trigger /search
			if !m.searchFallbackUsed && !m.searchPending && m.turnContent != "" {
				tc := strings.ToLower(m.turnContent)
				failedSearch := (strings.Contains(tc, "issue accessing the search") ||
					(strings.Contains(tc, "couldn't access") && strings.Contains(tc, "search")) ||
					strings.Contains(tc, "unable to search") ||
					strings.Contains(tc, "unable to perform") ||
					strings.Contains(tc, "don't have access to") ||
					strings.Contains(tc, "don't have the ability") ||
					(strings.Contains(tc, "search tool") && (strings.Contains(tc, "fail") || strings.Contains(tc, "error") || strings.Contains(tc, "issue") || strings.Contains(tc, "unavailable"))) ||
					(strings.Contains(tc, "cannot") && strings.Contains(tc, "search")))
				if failedSearch {
					query := m.extractSearchTopic()
					if query != "" {
						m.searchFallbackUsed = true
						m.streaming = ""
						m.turnContent = ""
						m.isStream = false
						m.cancelFunc = nil
						m.eventCh = nil
						m.toolStatus = ""
						return m.handleSearch(query)
					}
				}
			}

			m.searchFallbackUsed = false
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
		// Inject raw search content as context (not shown to user)
		contextMsg := fmt.Sprintf("[Web search results for %q]\n\n%s", msg.Query, msg.Results)
		m.searchCompactAt = len(m.messages) // remember where to compact later
		m.messages = append(m.messages, docker.NewChatMessage("user", contextMsg))

		// Instruct model to summarize the results
		summarizePrompt := fmt.Sprintf(searchPromptTemplate, msg.Query)
		m.messages = append(m.messages, docker.NewChatMessage("user", summarizePrompt))

		m.history = append(m.history, chatEntry{
			role:    "tool",
			content: fmt.Sprintf("Search: %s — summarizing results...", msg.Query),
		})

		// Auto-stream the model's summary
		m.searchPending = true
		m.isStream = true
		m.streaming = ""
		m.turnContent = ""
		m.toolStatus = ""
		m.streamStart = time.Now()

		ctx, cancel := context.WithCancel(context.Background())
		m.cancelFunc = cancel
		m.eventCh = docker.StreamChat(ctx, m.socketPath, m.modelTag, m.buildMessages(), m.params)

		m.updateViewport()
		return m, tea.Batch(waitForEvent(m.eventCh), doSpinTick())

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

	case DiffResultMsg:
		if msg.Err != nil {
			m.history = append(m.history, chatEntry{
				role:    "error",
				content: fmt.Sprintf("Diff error: %v", msg.Err),
			})
		} else if msg.Output == "" {
			m.history = append(m.history, chatEntry{
				role:    "assistant",
				content: "No changes detected.",
			})
		} else {
			m.history = append(m.history, chatEntry{
				role:    "tool",
				content: msg.Output,
			})
		}
		m.updateViewport()
		return m, nil

	case RunResultMsg:
		if msg.Err != nil {
			m.history = append(m.history, chatEntry{
				role:    "error",
				content: fmt.Sprintf("Command failed: %v\n%s", msg.Err, msg.Output),
			})
		} else {
			output := msg.Output
			if output == "" {
				output = "(no output)"
			}
			m.history = append(m.history, chatEntry{
				role:    "tool",
				content: fmt.Sprintf("$ %s\n%s", msg.Command, output),
			})
		}
		m.updateViewport()
		return m, nil

	case CommitResultMsg:
		if msg.Err != nil {
			m.history = append(m.history, chatEntry{
				role:    "error",
				content: fmt.Sprintf("Commit failed: %v", msg.Err),
			})
		} else {
			m.history = append(m.history, chatEntry{
				role:    "assistant",
				content: msg.Message,
			})
		}
		m.updateViewport()
		return m, nil

	case MentionCandidatesMsg:
		m.handleMentionCandidates(msg)
		return m, nil
	}

	// Update sub-components
	var cmd tea.Cmd

	if !m.isStream {
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
		// Live @-mention preview: kick off async glob as user types
		if mentionCmd := m.updateMentionPreview(); mentionCmd != nil {
			cmds = append(cmds, mentionCmd)
		}
	}

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m ChatModel) handleCommand(text string) (ChatModel, tea.Cmd) {
	trimmed := strings.TrimSpace(text)

	switch trimmed {
	case "/help":
		return m.handleHelp()

	case "/diff":
		return m.handleDiff()

	case "/undo":
		return m.handleUndo()

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

		if strings.HasPrefix(text, "/run ") {
			return m.handleRun(strings.TrimPrefix(text, "/run "))
		}

		if strings.HasPrefix(text, "/ask ") {
			return m.handleAsk(strings.TrimPrefix(text, "/ask "))
		}

		if trimmed == "/commit" || strings.HasPrefix(text, "/commit ") {
			return m.handleCommit()
		}

		if trimmed == "/review" || strings.HasPrefix(text, "/review ") {
			return m.handleReview()
		}

		if trimmed == "/skills" {
			return m.handleSkillsList()
		}

		if strings.HasPrefix(text, "/skill ") {
			return m.handleSkillActivate(strings.TrimPrefix(text, "/skill "))
		}

		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: fmt.Sprintf("Unknown command: %s\nType /help to see available commands.", text),
		})
		m.updateViewport()
		return m, nil
	}
}

// toolSystemPrompt is injected to instruct the model to use available tools.
//
//go:embed prompts/tools.md
var toolSystemPrompt string

// defaultSkillsPrompt teaches the model about built-in commands and workflows.
//
//go:embed prompts/skills.md
var defaultSkillsPrompt string

// compactPromptTemplate is the prompt used to summarize older messages.
//
//go:embed prompts/compact.md
var compactPromptTemplate string

// searchPromptTemplate instructs the model to summarize search results.
//
//go:embed prompts/search.md
var searchPromptTemplate string

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

	// Directives first (short, high-priority), then skills, then user/project context.
	sysPrompt := toolSystemPrompt + "\n\n" + defaultSkillsPrompt
	if m.systemPrompt != "" {
		sysPrompt = sysPrompt + "\n\n" + m.systemPrompt
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
		content: fmt.Sprintf("Searching and reading pages: %s...", query),
	})
	m.updateViewport()

	provider := m.searchProvider
	apiKey := m.searchAPIKey
	return m, func() tea.Msg {
		results, err := search.DeepQuery(provider, apiKey, query)
		return SearchResultMsg{Query: query, Results: results, Err: err}
	}
}

// isSearchAgreement returns true if the user's input indicates agreement to
// search and the last assistant message suggested searching.
func (m *ChatModel) isSearchAgreement(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))

	// Check for affirmative words anywhere in the message
	affirmatives := []string{
		"yes", "yeah", "yep", "sure", "ok", "okay",
		"go ahead", "please", "do it", "go for it", "y",
	}
	hasAffirmative := false
	for _, a := range affirmatives {
		if lower == a || strings.HasPrefix(lower, a+" ") || strings.HasPrefix(lower, a+",") {
			hasAffirmative = true
			break
		}
	}

	// Check for explicit search intent words
	searchIntents := []string{"search", "look it up", "look that up", "find it", "google"}
	hasSearchIntent := false
	for _, s := range searchIntents {
		if strings.Contains(lower, s) {
			hasSearchIntent = true
			break
		}
	}

	if !hasAffirmative && !hasSearchIntent {
		return false
	}

	// Check if last assistant message suggested searching
	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i].role == "assistant" {
			content := strings.ToLower(m.history[i].content)
			return strings.Contains(content, "search for") ||
				strings.Contains(content, "search the") ||
				strings.Contains(content, "look that up") ||
				strings.Contains(content, "look it up") ||
				strings.Contains(content, "would you like me to search")
		}
	}
	return false
}

// extractSearchTopic finds the user's original question to use as a search query.
// Looks for the last real user question before the assistant's search suggestion.
func (m *ChatModel) extractSearchTopic() string {
	// Walk history backwards: skip the search suggestion, find the user's question
	foundAssistant := false
	for i := len(m.history) - 1; i >= 0; i-- {
		entry := m.history[i]
		if entry.role == "assistant" {
			foundAssistant = true
			continue
		}
		if foundAssistant && entry.role == "user" {
			q := strings.TrimSpace(entry.content)
			// Skip very short or command-like inputs
			if len(q) > 2 && !strings.HasPrefix(q, "/") {
				return q
			}
		}
	}
	return ""
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

func (m ChatModel) handleHelp() (ChatModel, tea.Cmd) {
	help := `Available commands:

  /help              Show this help message
  /diff              Show current git diff
  /commit            Generate commit message and commit staged changes
  /review            Review current git diff for bugs and style
  /undo              Undo last git commit (soft reset)
  /run <cmd>         Run a shell command and show output
  /ask <question>    Ask without tool access (fast, read-only)
  /search <query>    Search the web and summarize results
  /fetch <url>       Fetch and display a web page
  /skills            List available skills
  /skill <name>      Activate a skill (loads full instructions)
  /clear             Start a fresh conversation
  /sessions          List saved sessions
  /models            Browse and switch models
  /init              Generate a BARYO.md for this project
  /system [prompt]   View or change system prompt
  /params [k=v]      View or change model parameters
  /context           Show token usage breakdown
  /compact           Summarize older messages to free context
  /export [file]     Export conversation to file
  /copy              Copy last response to clipboard
  /markdown          Toggle markdown rendering
  /doctor            Run diagnostic checks`

	m.history = append(m.history, chatEntry{
		role:    "assistant",
		content: help,
	})
	m.updateViewport()
	return m, nil
}

func (m ChatModel) handleDiff() (ChatModel, tea.Cmd) {
	m.history = append(m.history, chatEntry{
		role:    "tool",
		content: "Running git diff...",
	})
	m.updateViewport()

	return m, func() tea.Msg {
		// Show both staged and unstaged changes
		diff, err := gitOutput("diff", "HEAD")
		if err != nil {
			// Try without HEAD (no commits yet)
			diff, err = gitOutput("diff")
		}
		if err != nil {
			return DiffResultMsg{Err: err}
		}
		if diff == "" {
			// Check for staged changes only
			diff, _ = gitOutput("diff", "--cached")
		}
		return DiffResultMsg{Output: diff}
	}
}

func (m ChatModel) handleUndo() (ChatModel, tea.Cmd) {
	// Safety check: verify there's a commit to undo
	log, err := gitOutput("log", "--oneline", "-1")
	if err != nil || log == "" {
		m.history = append(m.history, chatEntry{
			role:    "error",
			content: "No commits to undo.",
		})
		m.updateViewport()
		return m, nil
	}

	cmd := exec.Command("git", "reset", "--soft", "HEAD~1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		m.history = append(m.history, chatEntry{
			role:    "error",
			content: fmt.Sprintf("Undo failed: %v\n%s", err, string(out)),
		})
	} else {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: fmt.Sprintf("Undone: %s\nChanges are now staged (soft reset).", log),
		})
	}
	m.updateViewport()
	return m, nil
}

func (m ChatModel) handleRun(command string) (ChatModel, tea.Cmd) {
	command = strings.TrimSpace(command)
	if command == "" {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: "Usage: /run <command>",
		})
		m.updateViewport()
		return m, nil
	}

	m.history = append(m.history, chatEntry{
		role:    "tool",
		content: fmt.Sprintf("Running: %s...", command),
	})
	m.updateViewport()

	return m, func() tea.Msg {
		cmd := exec.Command("sh", "-c", command)
		out, err := cmd.CombinedOutput()
		output := strings.TrimSpace(string(out))
		return RunResultMsg{Command: command, Output: output, Err: err}
	}
}

func (m ChatModel) handleAsk(question string) (ChatModel, tea.Cmd) {
	question = strings.TrimSpace(question)
	if question == "" {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: "Usage: /ask <question>",
		})
		m.updateViewport()
		return m, nil
	}

	// Add to conversation history
	m.messages = append(m.messages, docker.NewChatMessage("user", question))
	m.history = append(m.history, chatEntry{
		role:    "user",
		content: question,
	})

	// Stream response WITHOUT tool definitions (read-only, fast)
	m.isStream = true
	m.streaming = ""
	m.turnContent = ""
	m.toolStatus = ""
	m.streamStart = time.Now()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFunc = cancel
	m.eventCh = docker.StreamChat(ctx, m.socketPath, m.modelTag, m.buildMessages(), m.params)

	m.updateViewport()
	return m, tea.Batch(waitForEvent(m.eventCh), doSpinTick())
}

func (m ChatModel) handleCommit() (ChatModel, tea.Cmd) {
	// Get staged diff
	diff, err := gitOutput("diff", "--cached")
	if err != nil || diff == "" {
		// Check if there are staged files
		status, _ := gitOutput("status", "--porcelain")
		if status == "" {
			m.history = append(m.history, chatEntry{
				role:    "assistant",
				content: "Nothing to commit. Stage changes with `git add` first.",
			})
			m.updateViewport()
			return m, nil
		}
		// Try getting diff of all changes
		diff, _ = gitOutput("diff")
		if diff == "" {
			m.history = append(m.history, chatEntry{
				role:    "assistant",
				content: "No diff available. Stage your changes with `git add` first.",
			})
			m.updateViewport()
			return m, nil
		}
	}

	m.history = append(m.history, chatEntry{
		role:    "tool",
		content: "Generating commit message...",
	})

	// Ask the model to generate a commit message, then commit
	prompt := fmt.Sprintf(`Generate a concise git commit message for the following diff. Follow conventional commit style (e.g. "feat:", "fix:", "refactor:"). Write ONLY the commit message, nothing else. One line, under 72 characters.

%s`, diff)

	m.messages = append(m.messages, docker.NewChatMessage("user", "/commit"))
	m.history = append(m.history, chatEntry{
		role:    "user",
		content: "/commit",
	})

	// Stream the model's response, then auto-commit on completion
	m.isStream = true
	m.streaming = ""
	m.turnContent = ""
	m.toolStatus = ""
	m.commitPending = true
	m.streamStart = time.Now()

	// Build messages with the commit prompt injected
	msgs := m.buildMessages()
	msgs = append(msgs, docker.NewChatMessage("user", prompt))

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFunc = cancel
	m.eventCh = docker.StreamChat(ctx, m.socketPath, m.modelTag, msgs, m.params)

	m.updateViewport()
	return m, tea.Batch(waitForEvent(m.eventCh), doSpinTick())
}

func (m ChatModel) handleReview() (ChatModel, tea.Cmd) {
	// Get current diff
	diff, err := gitOutput("diff", "HEAD")
	if err != nil {
		diff, _ = gitOutput("diff")
	}
	if diff == "" {
		diff, _ = gitOutput("diff", "--cached")
	}
	if diff == "" {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: "No changes to review.",
		})
		m.updateViewport()
		return m, nil
	}

	// Inject the diff as context and ask for a review
	prompt := fmt.Sprintf(`Review the following git diff for bugs, logic errors, style issues, and potential improvements. Be concise and actionable. Focus on what matters most.

%s`, diff)

	m.messages = append(m.messages, docker.NewChatMessage("user", prompt))
	m.history = append(m.history, chatEntry{
		role:    "user",
		content: "/review",
	})

	// Stream the review
	m.isStream = true
	m.streaming = ""
	m.turnContent = ""
	m.toolStatus = ""
	m.streamStart = time.Now()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFunc = cancel

	m.eventCh = docker.StreamChat(ctx, m.socketPath, m.modelTag, m.buildMessages(), m.params)

	m.updateViewport()
	return m, tea.Batch(waitForEvent(m.eventCh), doSpinTick())
}

// hasActiveScripts returns true if any activated skill has scripts.
func (m *ChatModel) hasActiveScripts() bool {
	for _, hasScripts := range m.activeSkills {
		if hasScripts {
			return true
		}
	}
	return false
}

// autoActivateSkill checks user input against skill trigger words and
// loads the best matching skill into context if found.
func (m *ChatModel) autoActivateSkill(text string) {
	if len(m.skillIndex) == 0 {
		return
	}

	name, score := config.MatchSkill(text, m.skillIndex, m.activeSkills)
	if name == "" || score < 5 {
		return // no match or too weak (threshold 5 avoids single short-keyword false positives)
	}

	skill, err := config.LoadSkill(name, m.skillIndex)
	if err != nil {
		return
	}

	// Install dependencies if needed
	if depMsg := m.installSkillDeps(skill); depMsg != "" {
		m.history = append(m.history, chatEntry{role: "tool", content: depMsg})
	}

	// Create output directory for code-oriented skills
	if len(skill.Scripts) > 0 {
		cwd, _ := os.Getwd()
		outDir := filepath.Join(cwd, "output_files")
		os.MkdirAll(outDir, 0o755)
	}

	// Inject the skill into conversation context
	skillPrompt := config.FormatActivatedSkill(skill)
	m.messages = append(m.messages, docker.NewChatMessage("user", "[Skill activated: "+skill.Name+"]\n\n"+skillPrompt))
	m.messages = append(m.messages, docker.NewChatMessage("assistant", "I've loaded the "+skill.Name+" skill and will follow its instructions."))
	m.activeSkills[skill.Name] = len(skill.Scripts) > 0

	summary := fmt.Sprintf("Auto-activated skill: %s", skill.Name)
	if len(skill.Scripts) > 0 {
		summary += fmt.Sprintf(" (%d scripts available)", len(skill.Scripts))
	}
	m.history = append(m.history, chatEntry{
		role:    "tool",
		content: summary,
	})
	m.contextTokens = estimateTokens(m.buildMessages())
}

// installSkillDeps installs Python dependencies from requirements.txt if found.
// Returns a status message, or empty string if no deps to install.
func (m *ChatModel) installSkillDeps(skill config.Skill) string {
	if skill.RequiresFile == "" {
		return ""
	}

	// Find a working Python interpreter
	pythonCmd := ""
	for _, py := range []string{"python3", "python"} {
		if _, err := exec.LookPath(py); err == nil {
			pythonCmd = py
			break
		}
	}
	if pythonCmd == "" {
		return fmt.Sprintf("Python not found. Install dependencies manually:\npip install -r %s", skill.RequiresFile)
	}

	// Create a venv in the skill directory if it doesn't exist
	venvDir := filepath.Join(skill.Dir, ".venv")
	venvPython := filepath.Join(venvDir, "bin", pythonCmd)
	if _, err := os.Stat(venvPython); err != nil {
		cmd := exec.Command(pythonCmd, "-m", "venv", venvDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Sprintf("Failed to create venv: %v\n%s", err, string(out))
		}
	}

	// Install deps into the venv
	cmd := exec.Command(venvPython, "-m", "pip", "install", "-q", "-r", skill.RequiresFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Sprintf("Failed to install deps:\n%s", strings.TrimSpace(string(out)))
	}

	return fmt.Sprintf("Installed dependencies from %s (venv: %s)", filepath.Base(skill.RequiresFile), venvDir)
}

func (m ChatModel) handleSkillsList() (ChatModel, tea.Cmd) {
	if len(m.skillIndex) == 0 {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: "No skills found.\nPlace skills in ~/.baryo/skills/, .baryo/skills/, or skills/ (each as a directory with SKILL.md).",
		})
		m.updateViewport()
		return m, nil
	}

	var b strings.Builder
	b.WriteString("Available skills:\n\n")
	for _, s := range m.skillIndex {
		desc := s.Description
		if len(desc) > 80 {
			desc = desc[:80] + "..."
		}
		b.WriteString(fmt.Sprintf("  %-20s %s\n", s.Name, desc))
	}
	b.WriteString("\nActivate with: /skill <name>")

	m.history = append(m.history, chatEntry{
		role:    "assistant",
		content: b.String(),
	})
	m.updateViewport()
	return m, nil
}

func (m ChatModel) handleSkillActivate(name string) (ChatModel, tea.Cmd) {
	name = strings.TrimSpace(name)
	if name == "" {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: "Usage: /skill <name>\nList skills with /skills",
		})
		m.updateViewport()
		return m, nil
	}

	skill, err := config.LoadSkill(name, m.skillIndex)
	if err != nil {
		m.history = append(m.history, chatEntry{
			role:    "error",
			content: fmt.Sprintf("Skill not found: %s\nList skills with /skills", name),
		})
		m.updateViewport()
		return m, nil
	}

	// Check if already active
	if _, alreadyActive := m.activeSkills[skill.Name]; alreadyActive {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: fmt.Sprintf("Skill %q is already active.", skill.Name),
		})
		m.updateViewport()
		return m, nil
	}

	// Install dependencies if needed
	if depMsg := m.installSkillDeps(skill); depMsg != "" {
		m.history = append(m.history, chatEntry{role: "tool", content: depMsg})
	}

	// Create output directory for code-oriented skills
	if len(skill.Scripts) > 0 {
		cwd, _ := os.Getwd()
		outDir := filepath.Join(cwd, "output_files")
		os.MkdirAll(outDir, 0o755)
	}

	// Inject the full skill content into the conversation as context
	skillPrompt := config.FormatActivatedSkill(skill)
	m.messages = append(m.messages, docker.NewChatMessage("user", "[Skill activated: "+skill.Name+"]\n\n"+skillPrompt))
	m.messages = append(m.messages, docker.NewChatMessage("assistant", "I've loaded the "+skill.Name+" skill. I'm ready to help with "+skill.Description))
	m.activeSkills[skill.Name] = len(skill.Scripts) > 0

	summary := fmt.Sprintf("Skill activated: %s", skill.Name)
	if len(skill.Scripts) > 0 {
		summary += fmt.Sprintf(" (%d scripts available)", len(skill.Scripts))
	}
	m.history = append(m.history, chatEntry{
		role:    "assistant",
		content: summary,
	})

	m.contextTokens = estimateTokens(m.buildMessages())
	m.saveSession()
	m.updateViewport()
	return m, nil
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
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: fmt.Sprintf("Nothing to compact — conversation is only ~%s tokens (%d messages).", formatTokenCount(m.contextTokens), len(m.messages)),
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
		verb, style := thinkingStatus(elapsed, true)
		status = style.Render(fmt.Sprintf("%s %s... (%s)", frame, verb, elapsed))
	} else if m.isStream {
		elapsed := time.Since(m.streamStart).Truncate(time.Second)
		verb, style := thinkingStatus(elapsed, false)
		status = style.Render(fmt.Sprintf("%s %s... (%s)", frame, verb, elapsed))
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
