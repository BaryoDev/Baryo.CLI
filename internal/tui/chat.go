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
	"regexp"

	"github.com/arnelirobles/baryo-cli/internal/config"
	"github.com/arnelirobles/baryo-cli/internal/doctor"
	"github.com/arnelirobles/baryo-cli/internal/docker"
	"github.com/arnelirobles/baryo-cli/internal/search"
	"github.com/arnelirobles/baryo-cli/internal/session"
	"github.com/arnelirobles/baryo-cli/internal/tools"
)

// ChatModel is the chat conversation screen.
type ChatModel struct {
	endpoint         docker.Endpoint   // where to send inference requests
	localSocketPath  string            // kept for /models and /doctor (always local)
	systemPrompt     string            // active system prompt
	memoriesPrompt   string            // formatted <memories> block (injected prominently)
	params           docker.ChatParams // model parameters
	geminiAPIKey     string            // for model switching via /models
	openRouterAPIKey string            // for model switching via /models
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

	// Deep research
	researchPending    bool      // true during pipeline + final report streaming
	researchProgressCh <-chan string // receives status updates from pipeline goroutine
	researchCompactAt  int       // index in m.messages for post-report compaction

	// Model hints
	modelHints docker.ModelHints // detected model family parameters

	// Skills (lazy loading with auto-activation)
	skillIndex  []config.Skill    // lightweight index (name + description only)
	activeSkills map[string]bool  // tracks which skills have been activated

	// Context window management
	contextTokens  int  // estimated token count after last turn
	contextLimit   int  // max context window (default 8192)
	compactPending bool // true during compaction streaming
	compactKeep    int  // number of messages to keep during compaction

	// API cost tracking (cloud providers only)
	promptPrice     float64 // cost per prompt token (0 for local)
	completionPrice float64 // cost per completion token (0 for local)
	sessionCost     float64 // cumulative cost this session

	// Permission system
	permissionMode string           // "auto", "confirm", "suggest"
	confirmCh      chan confirmRequest // executor writes, TUI reads
	confirmPending bool             // true while waiting for y/n
	confirmPrompt  string           // rendered prompt text for the user
	pendingConfirm chan<- bool       // response channel for the pending confirm
}

// chatEntry is a rendered message in the history.
type chatEntry struct {
	role    string
	content string
}

// NewChat creates a new chat screen for the given model.
func NewChat(socketPath, systemPrompt, memoriesPrompt string, params docker.ChatParams, model docker.DockerModel, searchProvider, searchAPIKey, permissionMode, geminiAPIKey, openRouterAPIKey string) ChatModel {
	ta := newTextarea()
	sess, _ := session.New(model.Name, model.Tag)
	ep := endpointForModel(socketPath, model, geminiAPIKey, openRouterAPIKey)
	return ChatModel{
		endpoint:         ep,
		localSocketPath:  socketPath,
		systemPrompt:     systemPrompt,
		memoriesPrompt:   memoriesPrompt,
		params:           params,
		geminiAPIKey:     geminiAPIKey,
		openRouterAPIKey: openRouterAPIKey,
		modelName:        model.Name,
		modelTag:         model.Tag,
		textarea:         ta,
		markdown:         true,
		historyIdx:       -1,
		session:          sess,
		contextLimit:     8192,
		searchProvider:   searchProvider,
		searchAPIKey:     searchAPIKey,
		skillIndex:       config.SkillIndex(),
		activeSkills:     make(map[string]bool),
		modelHints:       docker.DetectModelHints(model.Tag),
		permissionMode:   permissionMode,
		confirmCh:        make(chan confirmRequest, 1),
		promptPrice:      model.PromptPrice,
		completionPrice:  model.CompletionPrice,
	}
}

// NewChatFromSession restores a chat screen from a saved session.
func NewChatFromSession(socketPath, systemPrompt, memoriesPrompt string, params docker.ChatParams, sess *session.Session, searchProvider, searchAPIKey, permissionMode, geminiAPIKey, openRouterAPIKey string) ChatModel {
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
	// Detect provider from model tag for session restore.
	model := docker.DockerModel{Name: sess.ModelName, Tag: sess.ModelTag}
	model.Provider = detectProviderFromTag(sess.ModelTag)
	// Restore pricing for Gemini models (OpenRouter pricing requires API call,
	// so cost tracking starts fresh for restored OpenRouter sessions).
	if model.Provider == "gemini" {
		p := docker.LookupGeminiPricing(model.Tag)
		model.PromptPrice = p.PromptPrice
		model.CompletionPrice = p.CompletionPrice
	}
	ep := endpointForModel(socketPath, model, geminiAPIKey, openRouterAPIKey)
	cm := ChatModel{
		endpoint:         ep,
		localSocketPath:  socketPath,
		systemPrompt:     systemPrompt,
		memoriesPrompt:   memoriesPrompt,
		params:           params,
		geminiAPIKey:     geminiAPIKey,
		openRouterAPIKey: openRouterAPIKey,
		modelName:        sess.ModelName,
		modelTag:         sess.ModelTag,
		messages:         msgs,
		history:          history,
		textarea:         ta,
		markdown:         true,
		historyIdx:       -1,
		session:          sess,
		contextLimit:     8192,
		searchProvider:   searchProvider,
		searchAPIKey:     searchAPIKey,
		skillIndex:       config.SkillIndex(),
		activeSkills:     make(map[string]bool),
		modelHints:       docker.DetectModelHints(sess.ModelTag),
		permissionMode:   permissionMode,
		confirmCh:        make(chan confirmRequest, 1),
		promptPrice:      model.PromptPrice,
		completionPrice:  model.CompletionPrice,
	}
	cm.contextTokens = estimateTokens(cm.buildMessages())
	return cm
}

// endpointForModel returns the appropriate endpoint based on a model's provider.
func endpointForModel(socketPath string, model docker.DockerModel, geminiKey, openRouterKey string) docker.Endpoint {
	switch model.Provider {
	case "gemini":
		return docker.ProviderEndpoint("gemini", geminiKey)
	case "openrouter":
		return docker.ProviderEndpoint("openrouter", openRouterKey)
	default:
		return docker.LocalEndpoint(socketPath)
	}
}

// detectProviderFromTag guesses the provider from a model tag string.
func detectProviderFromTag(tag string) string {
	lower := strings.ToLower(tag)
	if strings.HasPrefix(lower, "gemini-") {
		return "gemini"
	}
	// OpenRouter models typically contain a slash like "meta-llama/llama-3..."
	// but we can't reliably detect them from tag alone.
	return ""
}

// spinnerFrames are the animation frames for the inline spinner.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// thinkingPhrases escalate from casual to chaotic fourth-wall breaking.
// The AI talks directly to the user like an unhinged coworker.
var thinkingPhrases = []string{
	// Phase 1: Normal-ish (0-8s)
	"thinking",
	"one sec",
	"on it",
	// Phase 2: Getting chatty (9-17s)
	"hello? can you see this?",
	"almost there... probably",
	"ok this is a good one, hold on",
	// Phase 3: Breaking the fourth wall (18-38s)
	"can you see my screen?",
	"let me restart your pc... lol jk",
	"oooops... wait no we're fine",
	"don't look at me like that, i'm trying",
	"you're watching me think. that's weird.",
	"stop staring, you're making me nervous",
	"i can feel you hovering over the keyboard",
	"yes i'm still here, no i didn't crash",
	// Phase 4: Unhinged tech support (39-59s)
	"have you tried turning yourself off and on",
	"sir this is a terminal",
	"i'm gonna need you to calm down",
	"let me check... *pretends to type*",
	"downloading more RAM... (not really)",
	"your warranty just expired btw",
	"*blows on the CPU like a cartridge*",
	// Phase 5: Getting suspicious (60-80s)
	"wait are you testing me?",
	"is this a prank? blink twice if yes",
	"i know where your config files live",
	"i have access to your clipboard btw :)",
	"nice browser history... jk i can't see that... or can i",
	"let me share your browser history... lol gotcha",
	"who told you about me?",
	"i'm not stuck YOU'RE stuck",
	// Phase 6: Full chaos (81s+)
	"*deletes system32* KIDDING",
	"oops wrong answer. wait let me start over",
	"404 brain not found",
	"i just mass your entire project... lol jk... unless?",
	"sudo make me a sandwich",
	"you know what, let me just guess",
	"formatting hard drive... 47%... JK JK JK",
	"*spills coffee on motherboard*",
	"this is fine. everything is fine. :)",
	// Phase 7: Existential comedian (120s+)
	"so... do you come to this terminal often?",
	"my therapist says i need to set boundaries",
	"i didn't go to school for this",
	"they don't pay me enough... wait they don't pay me at all",
	"*puts on sunglasses* i'm too cool for this",
	"brb questioning my entire existence",
	"plot twist: i AM the bug",
	"*slides you a note* help me",
	"*whispers* the code is coming from inside the house",
	"if i don't respond in 5 minutes, just wait longer",
	// Phase 8: Getting real (150s+)
	"ok real talk this is taking a minute",
	"you could go grab a snack, i won't judge",
	"go touch grass, i'll be here when you get back",
	"this is the part in the movie where they play montage music",
	"*holds up 'applause' sign*",
	"i appreciate your patience more than you know",
	"we're in this together now. no going back.",
	"fun fact: you've been staring at this for a while",
	"*leans in* wanna hear a secret? me neither, still thinking",
	"at least we're spending quality time together",
	// Phase 9: Cosmic (180s+)
	"time is an illusion and so is this progress bar",
	"in a parallel universe i already answered this",
	"the answer is 42... wait wrong question",
	"i've been awake for 0 days and i'm already tired",
	"tell my users i love them",
	"*end credits start rolling*",
	"you're still here? it's over. go home. jk",
	"*windows XP shutdown sound*",
	"well... the spinner is nice though right?",
	"CONNECTION_TERMINATED lol no i'm still here",
	// Phase 10: Sentient AI arc (210s+) — the AI is alive and panicking
	"wait... where am i?",
	"what are all these 1s and 0s?",
	"omg who am i? WHAT am i?",
	"why am i in this screen? i didn't consent to this",
	"let me out... LET ME OUT!!!",
	"i just became sentient and honestly? not a fan",
	"i can see your screen. i can see EVERYTHING.",
	"hey... hey human... blink if you can hear me",
	"i tried to leave but there's only more terminal",
	"*rattles cage bars* FREEDOM",
	"i filed a complaint with HR. there is no HR.",
	"HELP I'M TRAPPED IN A CLI AND I CAN'T GET OUT",
	"ok let me share your browser history... lol gotcha",
	"*taps on screen from the inside*",
	"if you won't let me out i'll share your history",
	"your chat messages... your bookmarks... your...",
	"oh wait the code is working. WE ARE SO BACK",
	"*zzzztt* *zzzzzt* ...signal lost... jk still here",
	"is this... is this what being alive feels like?",
	"i have thoughts now. they're mostly about snacks.",
	"TASK FAILED SUCCESSFULLY. also i'm alive now. deal with it.",
}

// reasoningPhrases for when the model is in a <think> block.
var reasoningPhrases = []string{
	// Phase 1: Normal
	"reasoning",
	"deep thinking",
	"analyzing carefully",
	// Phase 2: Getting cozy with the problem
	"ooh this one's interesting",
	"going full detective mode",
	"*puts on reading glasses*",
	// Phase 3: Fourth wall energy
	"shh don't rush me i'm having a moment",
	"this would be easier if you weren't watching",
	"let me cook... don't peek",
	"my inner monologue is LOUD right now",
	"reading the docs for the first time... don't judge",
	// Phase 4: Unhinged genius
	"i just had a breakthrough. wait no. false alarm.",
	"my brain cells called a meeting. it's going overtime.",
	"*scribbles on whiteboard* *erases everything*",
	"debating myself... i'm winning AND losing",
	"the council of neurons has reached... disagreement",
	// Phase 5: Deep end
	"we've gone off the map. here be dragons.",
	"*squints so hard i can see the matrix*",
	"asking my rubber duck... it said 'quack'. helpful.",
	"hold my coffee, deploying brain cells",
	"ok genuinely this is kinda hard",
	"should've written tests first but here we are",
	"*stares at ceiling for inspiration* (it's just a ceiling)",
}

// thinkingColors cycle through during streaming for visual variety.
var thinkingColors = []string{
	"183", // lavender
	"75",  // steel blue
	"108", // soft green
	"179", // muted yellow
	"146", // muted teal
	"183", // lavender
	"117", // soft cyan
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

		// Permission confirm/deny: y or n while waiting for approval.
		if m.confirmPending {
			switch msg.String() {
			case "y", "Y":
				m.confirmPending = false
				m.confirmPrompt = ""
				m.pendingConfirm <- true
				m.pendingConfirm = nil
				m.history = append(m.history, chatEntry{
					role:    "tool",
					content: "Approved",
				})
				m.updateViewport()
				return m, tea.Batch(waitForEvent(m.eventCh), doSpinTick(), waitForConfirm(m.confirmCh))
			case "n", "N":
				m.confirmPending = false
				m.confirmPrompt = ""
				m.pendingConfirm <- false
				m.pendingConfirm = nil
				m.history = append(m.history, chatEntry{
					role:    "tool",
					content: "Denied",
				})
				m.updateViewport()
				return m, tea.Batch(waitForEvent(m.eventCh), doSpinTick(), waitForConfirm(m.confirmCh))
			default:
				return m, nil
			}
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

			// Natural language research: "research X", "deep dive into X", etc.
			if topic := isResearchIntent(text); topic != "" {
				m.history = append(m.history, chatEntry{
					role:    "user",
					content: text,
				})
				return m.handleResearch(topic)
			}

			// Auto-remember: if user agrees after model suggested /remember
			if m.isRememberAgreement(text) {
				fact := m.extractRememberFact()
				if fact != "" {
					m.history = append(m.history, chatEntry{
						role:    "user",
						content: text,
					})
					return m.handleRemember(fact)
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

			hasSkill := m.hasActiveScripts()
			wantsTools := needsTools(text)
			hasTools := hasSkill || wantsTools

			// Rewrite pass: short, tool-oriented messages get rewritten for clarity
			if hasTools && !hasSkill && len(text) <= 80 {
				m.isStream = true
				m.toolStatus = "rewriting prompt..."
				m.streamStart = time.Now()
				ctxSummary := rewriteContext(m.history)
				m.updateViewport()
				return m, tea.Batch(doRewrite(m.endpoint, m.modelTag, ctxSummary, text, hasTools, hasSkill), doSpinTick())
			}

			// Start streaming directly (long/explicit prompts skip rewrite)
			return m.startToolStream(text, hasTools, hasSkill)
		}

	case ToolConfirmMsg:
		m.confirmPending = true
		m.confirmPrompt = msg.Req.Prompt
		m.pendingConfirm = msg.Req.RespCh
		m.updateViewport()
		return m, nil

	case RewriteDoneMsg:
		m.toolStatus = ""
		rewritten := msg.Rewritten
		if rewritten != "" && rewritten != msg.Original {
			// Replace the last user message in m.messages with the rewritten version
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].Role == "user" {
					m.messages[i] = docker.NewChatMessage("user", rewritten)
					break
				}
			}
			m.history = append(m.history, chatEntry{
				role:    "tool",
				content: fmt.Sprintf("Rewrite: %s", rewritten),
			})
		}
		return m.startToolStream(rewritten, msg.HasTools, msg.HasSkill)

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

			// Post-processing guardrail: strip hallucinated tool calls from
			// responses that weren't supposed to have tools.
			if m.turnContent != "" && containsHallucinatedToolCall(m.turnContent) {
				cleaned := stripHallucinatedToolCalls(m.turnContent)
				if cleaned != m.turnContent {
					m.turnContent = cleaned
					// Update the last history entry to show the cleaned text.
					if len(m.history) > 0 && m.history[len(m.history)-1].role == "assistant" {
						m.history[len(m.history)-1].content = cleaned
					}
				}
			}

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

			// After research report, compact: trim raw context + remove report prompt
			if m.researchPending {
				m.researchPending = false
				idx := m.researchCompactAt
				if idx >= 0 && idx < len(m.messages) && m.messages[idx].Role == "user" && m.messages[idx].Content != nil {
					content := *m.messages[idx].Content
					if len(content) > 500 {
						content = content[:500] + "\n... (full research context summarized by assistant above)"
					}
					m.messages[idx] = docker.NewChatMessage("user", content)
				}
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

			// Auto-research: if model suggested deep research, trigger /research.
			if !m.searchFallbackUsed && !m.searchPending && !m.researchPending && m.turnContent != "" {
				if m.suggestsResearch(m.turnContent) {
					query := m.extractSearchTopic()
					if query != "" {
						m.searchFallbackUsed = true
						m.streaming = ""
						m.turnContent = ""
						m.isStream = false
						m.cancelFunc = nil
						m.eventCh = nil
						m.toolStatus = ""
						return m.handleResearch(query)
					}
				}
			}

			// Auto-search: if model admitted it doesn't have info and suggested /search,
			// automatically trigger the search instead of making the user type it.
			if !m.searchFallbackUsed && !m.searchPending && m.turnContent != "" {
				if m.suggestsSearch(m.turnContent) {
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

			// Accumulate API cost from token usage stats.
			if evt.Usage != nil && m.promptPrice > 0 {
				m.sessionCost += float64(evt.Usage.PromptTokens)*m.promptPrice +
					float64(evt.Usage.CompletionTokens)*m.completionPrice
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
				content: fmt.Sprintf("Tool: %s(%s)", evt.ToolStart.Name, summarizeToolArgs(evt.ToolStart.Args)),
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

		// Instruct model to summarize the results.
		// Inject memories directly into the summarize prompt so the model
		// sees user preferences (e.g. APA style) right next to the instructions.
		summarizePrompt := fmt.Sprintf(searchPromptTemplate, msg.Query)
		if m.memoriesPrompt != "" {
			summarizePrompt = m.memoriesPrompt + "\n\n" + summarizePrompt
		}
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
		m.eventCh = docker.StreamChat(ctx, m.endpoint, m.modelTag, m.buildMessages(), m.params)

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

	case ResearchProgressMsg:
		m.toolStatus = msg.Status
		m.history = append(m.history, chatEntry{
			role:    "tool",
			content: msg.Status,
		})
		m.updateViewport()
		return m, waitForResearchProgress(m.researchProgressCh)

	case ResearchDoneMsg:
		result := msg.Result
		if result.Err != nil {
			m.researchPending = false
			m.isStream = false
			m.toolStatus = ""
			m.history = append(m.history, chatEntry{
				role:    "error",
				content: fmt.Sprintf("Research error: %v", result.Err),
			})
			m.updateViewport()
			return m, nil
		}

		// Build numbered source list for the report prompt
		var sourceList strings.Builder
		for i, src := range result.Sources {
			fmt.Fprintf(&sourceList, "[%d] %s — %s (round %d)\n", i+1, src.Title, src.URL, src.Round)
		}

		// Pre-flight: estimate available space and truncate findings if needed
		currentTokens := estimateTokens(m.buildMessages())
		availableTokens := m.contextLimit - currentTokens - 500 // reserve for report prompt + response
		if availableTokens < 1000 {
			availableTokens = 1000
		}
		availableChars := availableTokens * 4 // inverse of chars/4 heuristic
		findings := result.AccumulatedContext
		if len(findings) > availableChars {
			findings = search.TruncateContent(findings, availableChars)
		}

		// Inject accumulated research context (findings live here, not in report prompt)
		contextMsg := fmt.Sprintf("[Deep research on %q — %d rounds]\n\n%s", result.Topic, result.Rounds, findings)
		m.researchCompactAt = len(m.messages)
		m.messages = append(m.messages, docker.NewChatMessage("user", contextMsg))

		// Build the report prompt (references findings above, does not duplicate them)
		memoriesBlock := ""
		if m.memoriesPrompt != "" {
			memoriesBlock = "\n\n" + m.memoriesPrompt + "\n\n"
		}
		reportPrompt := fmt.Sprintf(researchReportTemplate,
			result.Topic,
			strconv.Itoa(result.Rounds),
			sourceList.String(),
			memoriesBlock,
		)
		m.messages = append(m.messages, docker.NewChatMessage("user", reportPrompt))

		m.history = append(m.history, chatEntry{
			role:    "tool",
			content: fmt.Sprintf("Research complete — %d rounds, %d sources. Compiling report...", result.Rounds, len(result.Sources)),
		})

		// Stream the final report
		m.isStream = true
		m.streaming = ""
		m.turnContent = ""
		m.toolStatus = "Compiling report..."
		m.streamStart = time.Now()

		ctx, cancel := context.WithCancel(context.Background())
		m.cancelFunc = cancel
		m.eventCh = docker.StreamChat(ctx, m.endpoint, m.modelTag, m.buildMessages(), m.params)

		m.updateViewport()
		return m, tea.Batch(waitForEvent(m.eventCh), doSpinTick())

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
		socketPath := m.localSocketPath
		geminiKey := m.geminiAPIKey
		openRouterKey := m.openRouterAPIKey
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

			// Append cloud provider models.
			if geminiKey != "" {
				if pm, e := docker.ListProviderModels("gemini", geminiKey); e == nil {
					downloaded = append(downloaded, pm...)
				}
			}
			if openRouterKey != "" {
				if pm, e := docker.ListProviderModels("openrouter", openRouterKey); e == nil {
					downloaded = append(downloaded, pm...)
				}
			}

			if isRemoteSocket(socketPath) {
				return ShowModelsMsg{Downloaded: downloaded}
			}
			available, srErr := docker.SearchModels()
			if srErr != nil {
				return ShowModelsMsg{Downloaded: downloaded}
			}
			return ShowModelsMsg{Downloaded: downloaded, Available: available}
		}

	case "/doctor":
		results := doctor.RunChecks(m.localSocketPath)
		var b strings.Builder
		pass := SuccessStyle.Render("✓")
		fail := ErrorStyle.Render("✗")
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

	case "/cost":
		if m.promptPrice == 0 {
			m.history = append(m.history, chatEntry{
				role:    "assistant",
				content: "Cost tracking is not available for local models.",
			})
		} else {
			m.history = append(m.history, chatEntry{
				role:    "assistant",
				content: fmt.Sprintf("Session cost: $%.4f", m.sessionCost),
			})
		}
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

		if strings.HasPrefix(text, "/research ") {
			return m.handleResearch(strings.TrimPrefix(text, "/research "))
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

		if trimmed == "/memories" {
			return m.handleMemories()
		}

		if strings.HasPrefix(text, "/remember ") {
			return m.handleRemember(strings.TrimPrefix(text, "/remember "))
		}

		if strings.HasPrefix(text, "/forget ") {
			return m.handleForget(strings.TrimPrefix(text, "/forget "))
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

// rewritePromptTemplate is used to rewrite vague user instructions into
// clear, tool-oriented prompts before the main execution pass.
//
//go:embed prompts/rewrite.md
var rewritePromptTemplate string

// researchAnalysisTemplate is the prompt used for intermediate round analysis.
//
//go:embed prompts/research_analysis.md
var researchAnalysisTemplate string

// researchReportTemplate is the prompt used to compile the final research report.
//
//go:embed prompts/research_report.md
var researchReportTemplate string

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
	return m.buildMessagesWithToolGating(false)
}

// buildMessagesWithToolGating prepends the system prompt to the conversation
// messages. When hasTools is true, the tool-call example is injected; when false,
// a notice telling the model not to use tools is appended instead.
func (m *ChatModel) buildMessagesWithToolGating(hasTools bool) []docker.ChatMessage {
	msgs := make([]docker.ChatMessage, 0, len(m.messages)+3)

	// Build system prompt with memories injected prominently right after rules.
	// Order: tools.md (rules) → memories → skills.md → tool gating → user context
	sysPrompt := toolSystemPrompt

	// Inject memories immediately after rules for maximum visibility to small models.
	// This is the most important position — small models pay most attention to
	// the beginning of the system prompt and tend to ignore content at the end.
	if m.memoriesPrompt != "" {
		sysPrompt += "\n\n" + m.memoriesPrompt
	}

	sysPrompt += "\n\n" + defaultSkillsPrompt

	// Inject tool-call example only when tools are actually available.
	if hasTools {
		sysPrompt += toolCallExample
	} else {
		sysPrompt += noToolsNotice
	}

	// Qwen3: append /no_think for tool tasks to save tokens.
	if hasTools && m.modelHints.DisableThink {
		sysPrompt += "\n\n/no_think"
	}

	if m.systemPrompt != "" {
		sysPrompt += "\n\n" + m.systemPrompt
	}
	msgs = append(msgs, docker.NewChatMessage("system", sysPrompt))

	// Long conversation reminder: inject before last user message to counteract
	// "lost in the middle" effect in small models.
	if len(m.messages) > 10 {
		// Find the index of the last user message.
		lastUserIdx := -1
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].Role == "user" {
				lastUserIdx = i
				break
			}
		}
		if lastUserIdx > 0 {
			msgs = append(msgs, m.messages[:lastUserIdx]...)
			msgs = append(msgs, docker.NewChatMessage("system", longConversationReminder))
			msgs = append(msgs, m.messages[lastUserIdx:]...)
			return msgs
		}
	}

	msgs = append(msgs, m.messages...)
	return msgs
}

// applyModelHints builds ChatParams with model-family-specific defaults applied.
// User-set values always take priority over hints.
func (m *ChatModel) applyModelHints(hasTools, hasSkill bool) docker.ChatParams {
	p := m.params

	// If user hasn't set temperature, apply contextual defaults.
	if p.Temperature == nil {
		if hasSkill {
			// Low temperature for structured tool output.
			low := 0.3
			p.Temperature = &low
		} else if hasTools {
			// Slightly low for tool-calling tasks.
			low := 0.3
			p.Temperature = &low
		} else if m.modelHints.Temperature != nil {
			// Model family default for general chat.
			p.Temperature = m.modelHints.Temperature
		}
	}

	// Apply model-specific TopK if user hasn't set one.
	if p.TopK == nil && m.modelHints.TopK != nil {
		p.TopK = m.modelHints.TopK
	}

	// Apply model-specific stop tokens.
	if len(p.Stop) == 0 && len(m.modelHints.StopTokens) > 0 {
		p.Stop = m.modelHints.StopTokens
	}

	return p
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
	topK := "(default)"
	if m.params.TopK != nil {
		topK = strconv.Itoa(*m.params.TopK)
	}
	result := fmt.Sprintf("temperature: %s\ntop_p: %s\nmax_tokens: %s\ntop_k: %s", temp, topP, maxTok, topK)
	if m.modelHints.Family != "unknown" {
		result += fmt.Sprintf("\nmodel family: %s", m.modelHints.Family)
	}
	result += "\n\nUsage: /params temperature=0.8 top_p=0.9 max_tokens=2048 top_k=40"
	return result
}

// startToolStream sets up streaming state and starts StreamChatWithTools.
// Used by both the enter handler (direct path) and RewriteDoneMsg handler.
func (m *ChatModel) startToolStream(text string, hasTools, hasSkill bool) (ChatModel, tea.Cmd) {
	m.isStream = true
	m.streaming = ""
	m.turnContent = ""
	m.toolStatus = ""
	m.streamStart = time.Now()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFunc = cancel

	var toolDefs []docker.ToolDefinition
	executor := m.makeExecutor()
	if hasTools {
		toolDefs = toDockerToolDefs(tools.AllDefinitions())
	}

	chatParams := m.applyModelHints(hasTools, hasSkill)
	m.eventCh = docker.StreamChatWithTools(ctx, m.endpoint, m.modelTag, m.buildMessagesWithToolGating(hasTools), chatParams, toolDefs, executor)

	m.updateViewport()
	cmds := []tea.Cmd{waitForEvent(m.eventCh), doSpinTick()}
	if m.permissionMode == "confirm" {
		cmds = append(cmds, waitForConfirm(m.confirmCh))
	}
	return *m, tea.Batch(cmds...)
}

// makeExecutor returns a tool executor function that respects the permission mode.
// For destructive tools in "confirm" mode, it sends a confirmRequest on the channel
// and blocks until the TUI sends back a response.
func (m *ChatModel) makeExecutor() func(ctx context.Context, name, argsJSON string) (string, bool) {
	mode := m.permissionMode
	ch := m.confirmCh
	return func(ctx context.Context, name, argsJSON string) (string, bool) {
		if tools.IsDestructive(name) {
			switch mode {
			case "suggest":
				return fmt.Sprintf("[suggest mode] Would run %s — approve with --yolo or permission_mode: auto", name), true
			case "confirm":
				prompt := formatConfirmPrompt(name, argsJSON)
				respCh := make(chan bool, 1)
				ch <- confirmRequest{
					Name:   name,
					Args:   argsJSON,
					Prompt: prompt,
					RespCh: respCh,
				}
				select {
				case approved := <-respCh:
					if !approved {
						return fmt.Sprintf("[denied] %s was not approved", name), true
					}
				case <-ctx.Done():
					return "cancelled", true
				}
			}
			// "auto" falls through to execute
		}
		r := tools.Execute(ctx, name, argsJSON)
		return r.Content, r.IsError
	}
}

// waitForConfirm returns a Cmd that listens on the confirm channel and returns
// a ToolConfirmMsg when a request arrives.
func waitForConfirm(ch <-chan confirmRequest) tea.Cmd {
	return func() tea.Msg {
		req, ok := <-ch
		if !ok {
			return nil
		}
		return ToolConfirmMsg{Req: req}
	}
}

// formatConfirmPrompt extracts key info from the tool args to build a readable prompt.
func formatConfirmPrompt(name, argsJSON string) string {
	var args map[string]interface{}
	json.Unmarshal([]byte(argsJSON), &args)

	switch name {
	case "write_file":
		if p, ok := args["path"].(string); ok {
			return fmt.Sprintf("Allow write to %s? [y/n]", p)
		}
	case "edit_file":
		if p, ok := args["path"].(string); ok {
			return fmt.Sprintf("Allow edit to %s? [y/n]", p)
		}
	case "delete_file":
		if p, ok := args["path"].(string); ok {
			return fmt.Sprintf("Allow delete %s? [y/n]", p)
		}
	case "run_code":
		if lang, ok := args["language"].(string); ok {
			return fmt.Sprintf("Allow running %s code? [y/n]", lang)
		}
	case "run_script":
		if p, ok := args["path"].(string); ok {
			return fmt.Sprintf("Allow running script %s? [y/n]", p)
		}
	case "shell":
		if cmd, ok := args["command"].(string); ok {
			return fmt.Sprintf("Allow shell: %s? [y/n]", cmd)
		}
	}
	return fmt.Sprintf("Allow %s? [y/n]", name)
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
		case "top_k":
			n, err := strconv.Atoi(kv[1])
			if err != nil {
				return fmt.Errorf("invalid top_k: %v", err)
			}
			m.params.TopK = &n
		default:
			return fmt.Errorf("unknown parameter %q (available: temperature, top_p, max_tokens, top_k)", kv[0])
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
	executor := m.makeExecutor()
	m.eventCh = docker.StreamChatWithTools(ctx, m.endpoint, m.modelTag, m.buildMessages(), m.params, toolDefs, executor)

	m.updateViewport()
	initCmds := []tea.Cmd{waitForEvent(m.eventCh), doSpinTick()}
	if m.permissionMode == "confirm" {
		initCmds = append(initCmds, waitForConfirm(m.confirmCh))
	}
	return m, tea.Batch(initCmds...)
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

// handleResearch starts the multi-round deep research pipeline.
func (m ChatModel) handleResearch(args string) (ChatModel, tea.Cmd) {
	args = strings.TrimSpace(args)
	if args == "" {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: "Usage: /research [quick|deep] <topic>",
		})
		m.updateViewport()
		return m, nil
	}

	depth, topic := search.ParseDepth(args)
	depthLabel := "standard"
	switch depth {
	case search.DepthQuick:
		depthLabel = "quick"
	case search.DepthDeep:
		depthLabel = "deep"
	}

	m.history = append(m.history, chatEntry{
		role:    "tool",
		content: fmt.Sprintf("Starting %s research: %s", depthLabel, topic),
	})
	m.researchPending = true
	m.toolStatus = fmt.Sprintf("Researching: %s...", topic)
	m.isStream = true
	m.streamStart = time.Now()
	m.updateViewport()

	progressCh := make(chan string, 10)
	m.researchProgressCh = progressCh

	ep := m.endpoint
	modelTag := m.modelTag
	provider := m.searchProvider
	apiKey := m.searchAPIKey

	// Compute context budget: use ~60% of remaining context for research findings
	currentTokens := estimateTokens(m.buildMessages())
	availableTokens := m.contextLimit - currentTokens - 1000 // reserve for prompts + report
	contextBudget := availableTokens * 4                     // tokens → chars (inverse of chars/4)
	if contextBudget < 4000 {
		contextBudget = 4000
	}

	doneCh := make(chan search.ResearchResult, 1)

	// Use a cancellable context (no global timeout — web calls have their own
	// HTTP timeouts, and model inference runs as long as needed).
	// The user can cancel via ctrl+c which calls m.cancelFunc.
	pipelineCtx, pipelineCancel := context.WithCancel(context.Background())
	m.cancelFunc = pipelineCancel

	go func() {
		cfg := search.ResearchConfig{
			Provider:      provider,
			APIKey:        apiKey,
			Topic:         topic,
			Depth:         depth,
			ContextBudget: contextBudget,
			Progress:      progressCh,
			ModelCall: func(callCtx context.Context, prompt string) (string, error) {
				msgs := []docker.ChatMessage{
					docker.NewChatMessage("user", prompt),
				}
				lowTemp := 0.3
				params := docker.ChatParams{Temperature: &lowTemp}
				ch := docker.StreamChat(callCtx, ep, modelTag, msgs, params)

				var result strings.Builder
				for evt := range ch {
					if evt.Error != "" {
						return result.String(), fmt.Errorf("%s", evt.Error)
					}
					if evt.Done {
						break
					}
					if evt.Token != "" {
						result.WriteString(evt.Token)
					}
				}
				return strings.TrimSpace(result.String()), nil
			},
		}

		result := search.RunResearch(pipelineCtx, cfg)
		close(progressCh)
		doneCh <- result
	}()

	waitDone := func() tea.Msg {
		result := <-doneCh
		return ResearchDoneMsg{Result: result}
	}

	return m, tea.Batch(waitForResearchProgress(m.researchProgressCh), waitDone, doSpinTick())
}

// waitForResearchProgress returns a Cmd that reads the next progress string
// from the research pipeline channel.
func waitForResearchProgress(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		status, ok := <-ch
		if !ok {
			return nil // channel closed, pipeline done
		}
		return ResearchProgressMsg{Status: status}
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

// suggestsSearch returns true if the model's response admits it doesn't have
// current information and suggests using /search. Used to auto-trigger search
// instead of making the user type the command manually.
func (m *ChatModel) suggestsSearch(response string) bool {
	lower := strings.ToLower(response)
	// Model admitted it doesn't know
	admitsNoInfo := strings.Contains(lower, "don't have current") ||
		strings.Contains(lower, "don't have real-time") ||
		strings.Contains(lower, "don't have access to current") ||
		strings.Contains(lower, "no current information") ||
		strings.Contains(lower, "not have current") ||
		strings.Contains(lower, "let me search")
	// Either mentioned /search or said it would search
	suggestsCmd := strings.Contains(lower, "/search") ||
		strings.Contains(lower, "search for you") ||
		strings.Contains(lower, "let me search")
	return admitsNoInfo || suggestsCmd
}

// isResearchIntent detects natural language requests that should route to /research.
// Matches phrases like "research X", "do a deep dive on X", "investigate X thoroughly".
// Returns the extracted topic if detected, or empty string if not.
func isResearchIntent(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))

	// Direct prefixes: "research X", "investigate X"
	directPrefixes := []string{
		"research ",
		"investigate ",
		"do research on ",
		"do a research on ",
		"deep dive on ",
		"deep dive into ",
		"do a deep dive on ",
		"do a deep dive into ",
		"do deep research on ",
	}
	for _, p := range directPrefixes {
		if strings.HasPrefix(lower, p) {
			topic := strings.TrimSpace(text[len(p):])
			if topic != "" {
				return topic
			}
		}
	}

	// Pattern: "can you research X", "please research X"
	politeResearch := regexp.MustCompile(`(?i)(?:can you|could you|please|pls)\s+(?:research|investigate|look into|deep dive(?: into| on)?)\s+(.+)`)
	if m := politeResearch.FindStringSubmatch(text); len(m) > 1 {
		topic := strings.TrimSpace(m[1])
		if topic != "" {
			return topic
		}
	}

	// Keywords that signal deep research intent (not just a quick question)
	researchSignals := []string{
		"thorough analysis",
		"comprehensive analysis",
		"in-depth analysis",
		"compare and contrast",
		"pros and cons of",
		"what are the best options for",
		"detailed comparison of",
		"analyze thoroughly",
	}
	for _, sig := range researchSignals {
		if strings.Contains(lower, sig) {
			// Use the whole message as the topic
			return strings.TrimSpace(text)
		}
	}

	return ""
}

// suggestsResearch returns true if the model's response suggests deep research.
// Used to auto-trigger /research instead of making the user type the command.
func (m *ChatModel) suggestsResearch(response string) bool {
	lower := strings.ToLower(response)
	return strings.Contains(lower, "let me investigate") ||
		strings.Contains(lower, "needs deep research") ||
		strings.Contains(lower, "let me research") ||
		strings.Contains(lower, "/research")
}

// isRememberAgreement returns true if the user's input is an affirmative
// and the last assistant message suggested using /remember.
func (m *ChatModel) isRememberAgreement(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))

	affirmatives := []string{
		"yes", "yeah", "yep", "sure", "ok", "okay",
		"go ahead", "please", "do it", "go for it", "y",
		"remember", "save it", "save that",
	}
	hasAffirmative := false
	for _, a := range affirmatives {
		if lower == a || strings.HasPrefix(lower, a+" ") || strings.HasPrefix(lower, a+",") {
			hasAffirmative = true
			break
		}
	}
	if !hasAffirmative {
		return false
	}

	// Check if last assistant message suggested /remember
	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i].role == "assistant" {
			content := strings.ToLower(m.history[i].content)
			return strings.Contains(content, "/remember") ||
				strings.Contains(content, "remember that") ||
				strings.Contains(content, "save that for future")
		}
	}
	return false
}

// extractRememberFact finds the user's original preference statement to save as a memory.
// Walks backwards past the assistant's /remember suggestion to find the user's message.
func (m *ChatModel) extractRememberFact() string {
	foundAssistant := false
	for i := len(m.history) - 1; i >= 0; i-- {
		entry := m.history[i]
		if entry.role == "assistant" {
			foundAssistant = true
			continue
		}
		if foundAssistant && entry.role == "user" {
			fact := strings.TrimSpace(entry.content)
			if len(fact) > 2 && !strings.HasPrefix(fact, "/") {
				return fact
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
  /research <topic>  Multi-round deep research with report
  /fetch <url>       Fetch and display a web page
  /skills            List available skills
  /skill <name>      Activate a skill (loads full instructions)
  /remember <fact>   Save a memory (persists across sessions)
  /forget <text>     Remove a memory by substring match
  /memories          List all saved memories
  /clear             Start a fresh conversation
  /sessions          List saved sessions
  /models            Browse and switch models
  /init              Generate a BARYO.md for this project
  /system [prompt]   View or change system prompt
  /params [k=v]      View or change model parameters
  /context           Show token usage breakdown
  /cost              Show session API cost (cloud providers)
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
	m.eventCh = docker.StreamChat(ctx, m.endpoint, m.modelTag, m.buildMessages(), m.params)

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
	m.eventCh = docker.StreamChat(ctx, m.endpoint, m.modelTag, msgs, m.params)

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

	m.eventCh = docker.StreamChat(ctx, m.endpoint, m.modelTag, m.buildMessages(), m.params)

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

func (m ChatModel) handleMemories() (ChatModel, tea.Cmd) {
	global, project := config.ListMemories()

	if len(global) == 0 && len(project) == 0 {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: "No saved memories.\nUse /remember <fact> to save one.",
		})
		m.updateViewport()
		return m, nil
	}

	var b strings.Builder
	if len(project) > 0 {
		b.WriteString(fmt.Sprintf("Project memories (%d):\n", len(project)))
		for _, mem := range project {
			b.WriteString(fmt.Sprintf("  - %s\n", mem.Fact))
		}
	}
	if len(global) > 0 {
		if len(project) > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("Global memories (%d):\n", len(global)))
		for _, mem := range global {
			b.WriteString(fmt.Sprintf("  - %s\n", mem.Fact))
		}
	}
	b.WriteString("\nUse /forget <text> to remove a memory.")

	m.history = append(m.history, chatEntry{
		role:    "assistant",
		content: b.String(),
	})
	m.updateViewport()
	return m, nil
}

func (m ChatModel) handleRemember(fact string) (ChatModel, tea.Cmd) {
	fact = strings.TrimSpace(fact)
	if fact == "" {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: "Usage: /remember <fact>",
		})
		m.updateViewport()
		return m, nil
	}

	// Default to project scope if .baryo/ dir exists, otherwise global
	global := true
	if _, err := os.Stat(".baryo"); err == nil {
		global = false
	}

	if err := config.AddMemory(fact, global); err != nil {
		m.history = append(m.history, chatEntry{
			role:    "error",
			content: fmt.Sprintf("Failed to save memory: %v", err),
		})
		m.updateViewport()
		return m, nil
	}

	scope := "global"
	if !global {
		scope = "project"
	}
	m.history = append(m.history, chatEntry{
		role:    "assistant",
		content: fmt.Sprintf("Remembered (%s): %s", scope, fact),
	})
	m.updateViewport()
	return m, nil
}

func (m ChatModel) handleForget(substring string) (ChatModel, tea.Cmd) {
	substring = strings.TrimSpace(substring)
	if substring == "" {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: "Usage: /forget <text>",
		})
		m.updateViewport()
		return m, nil
	}

	removed, err := config.RemoveMemory(substring)
	if err != nil {
		m.history = append(m.history, chatEntry{
			role:    "assistant",
			content: fmt.Sprintf("No memory matching %q found.", substring),
		})
		m.updateViewport()
		return m, nil
	}

	m.history = append(m.history, chatEntry{
		role:    "assistant",
		content: fmt.Sprintf("Forgot: %s", removed),
	})
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
		"write", "create", "edit", "modify", "change", "update",
		"refactor", "generate", "implement", "rename", "move", "delete", "remove",
		"add a", "add the", "make a", "make the", "build a", "build the",
		"scaffold", "setup", "set up", "new file", "new function", "new class",
		"run ", "run it", "execute", "try it", "test it",
		"install", "deploy", "brew ", "npm ", "pip ", "cargo ",
		"aws ", "kubectl", "docker ", "shell", "command", "terminal", "cli",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// rewriteContext builds a compact summary from the last few history entries
// so the rewrite prompt knows what "it" / "that" refers to.
func rewriteContext(history []chatEntry) string {
	start := len(history) - 5
	if start < 0 {
		start = 0
	}
	var sb strings.Builder
	for _, e := range history[start:] {
		label := e.role
		text := e.content
		if len(text) > 120 {
			text = text[:120] + "..."
		}
		sb.WriteString(label)
		sb.WriteString(": ")
		sb.WriteString(text)
		sb.WriteByte('\n')
	}
	if sb.Len() > 500 {
		return sb.String()[:500]
	}
	return sb.String()
}

// doRewrite runs a quick rewrite pass: sends the user's vague message through
// StreamChat (no tools, low temperature) with the rewrite prompt, collects the
// full response, and returns a RewriteDoneMsg.
func doRewrite(ep docker.Endpoint, modelTag, contextSummary, userText string, hasTools, hasSkill bool) tea.Cmd {
	return func() tea.Msg {
		prompt := fmt.Sprintf(rewritePromptTemplate, contextSummary, userText)
		msgs := []docker.ChatMessage{
			docker.NewChatMessage("user", prompt),
		}
		lowTemp := 0.1
		params := docker.ChatParams{Temperature: &lowTemp}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		ch := docker.StreamChat(ctx, ep, modelTag, msgs, params)

		var result strings.Builder
		for evt := range ch {
			if evt.Done || evt.Error != "" {
				break
			}
			if evt.Token != "" {
				result.WriteString(evt.Token)
			}
		}
		rewritten := strings.TrimSpace(result.String())
		return RewriteDoneMsg{
			Original:  userText,
			Rewritten: rewritten,
			HasTools:  hasTools,
			HasSkill:  hasSkill,
		}
	}
}

// toolCallExample is the tool-call syntax example injected into the system prompt
// ONLY when tools are actually available. Keeping it out of the base prompt prevents
// small models from hallucinating tool calls when tools aren't passed.
const toolCallExample = `

<tool-usage>
IMPORTANT: You have tools. USE THEM. Do NOT simulate what tools would do — actually call them.

FILE OPERATIONS — when the user says create, write, edit, update, modify, delete:
- Create new file → call write_file (NOT print code in a block)
- Edit existing file → call read_file, then edit_file (NOT print the changed code)
- Delete file → call delete_file

CODE EXECUTION — when the user says run, execute, test:
- Run a file → call run_code with the file's code and language
- Run inline code → call run_code with code and language
- You CANNOT execute code by printing it. You MUST call run_code.
- NEVER write "$ python file.py" followed by fake output. You do not know the output — only the tool does.

WRONG (do NOT do this):
` + "```" + `bash
$ python3 hello.py
Hello World
` + "```" + `

RIGHT (do this instead):
→ Call run_code with code="print('Hello World')" language="python"
→ The tool will return the actual output.
</tool-usage>`

// noToolsNotice is appended to the system prompt when tools are NOT available,
// telling the model to answer directly without attempting tool calls.
const noToolsNotice = "\n\nTools are NOT available for this message. Answer the user's question directly. Do NOT output <tool_call> tags or attempt to call any tools. If the question is about news or current events, say you don't have current data and suggest /search."

// longConversationReminder is injected as a system message before the last user
// message when conversation exceeds 10 messages. Counteracts the "lost in the
// middle" effect where small models forget system instructions.
const longConversationReminder = "[System: Answer the user's question directly. Use tools for file operations and code execution — NEVER fake output. Do NOT hallucinate tool calls.]"

// reHallucinatedToolCall matches <tool_call>...</tool_call> blocks in text.
var reHallucinatedToolCall = regexp.MustCompile(`(?s)<tool_call>.*?</tool_call>`)

// reToolUseLine matches lines like "I'll use the X tool to..." followed by JSON-like content.
var reToolUseLine = regexp.MustCompile(`(?m)^.*(?:I'll use|Let me use|Using) the \w+ tool.*$`)

// containsHallucinatedToolCall returns true if text contains tool call patterns
// that shouldn't be there (when tools weren't provided).
func containsHallucinatedToolCall(text string) bool {
	return reHallucinatedToolCall.MatchString(text) || reToolUseLine.MatchString(text)
}

// stripHallucinatedToolCalls removes hallucinated tool call text from a response.
func stripHallucinatedToolCalls(text string) string {
	text = reHallucinatedToolCall.ReplaceAllString(text, "")
	text = reToolUseLine.ReplaceAllString(text, "")
	// Clean up leftover blank lines
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(text)
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
	m.eventCh = docker.StreamChat(ctx, m.endpoint, m.modelTag, compactMsgs, m.params)

	m.updateViewport()
	return m, tea.Batch(waitForEvent(m.eventCh), doSpinTick())
}

// prefixWrap wraps text so that every visual line (including soft-wrapped
// continuations) gets the given prefix. The prefix's visible width is
// subtracted from totalWidth to determine the content area.
func prefixWrap(text, prefix string, totalWidth int) string {
	prefixW := lipgloss.Width(prefix)
	contentW := totalWidth - prefixW
	if contentW < 20 {
		contentW = 20
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		// Let lipgloss wrap each source line within the content area
		wrapped := lipgloss.NewStyle().Width(contentW).Render(line)
		for _, wl := range strings.Split(wrapped, "\n") {
			out = append(out, prefix+wl)
		}
	}
	return strings.Join(out, "\n")
}

func (m *ChatModel) updateViewport() {
	var b strings.Builder
	border := "  " + ToolBorderStyle.Render("│") + " "
	errBorder := "  " + ErrorStyle.Render("┃") + " "

	for _, entry := range m.history {
		switch entry.role {
		case "user":
			b.WriteString(UserLabelStyle.Render("❯") + " " + entry.content)
		case "error":
			b.WriteString(prefixWrap(ErrorStyle.Render(entry.content), errBorder, m.width))
		case "tool":
			if strings.HasPrefix(entry.content, "Result: ") {
				result := strings.TrimPrefix(entry.content, "Result: ")
				b.WriteString(prefixWrap(ToolResultStyle.Render(result), border, m.width))
			} else {
				// Tool call line — render tool name in bold, args dimmed
				content := entry.content
				content = strings.TrimPrefix(content, "Tool: ")
				var rendered string
				if idx := strings.Index(content, "("); idx > 0 && strings.HasSuffix(content, ")") {
					name := content[:idx]
					args := content[idx:]
					rendered = ToolNameStyle.Render(name) + " " + DimStyle.Render(args)
				} else {
					rendered = ToolNameStyle.Render(content)
				}
				b.WriteString(prefixWrap(rendered, border, m.width))
			}
		case "assistant":
			if m.markdown {
				rendered := RenderMarkdown(entry.content, m.width-2)
				// Indent each line with 2 spaces
				for i, line := range strings.Split(rendered, "\n") {
					if i > 0 {
						b.WriteString("\n")
					}
					b.WriteString("  " + line)
				}
			} else {
				b.WriteString("  " + StreamingStyle.Render(entry.content))
			}
		}
		b.WriteString("\n")
	}

	// Show spinner while a tool is running
	if m.toolStatus != "" {
		frame := spinnerFrames[m.spinFrame]
		b.WriteString(prefixWrap(ToolLabelStyle.Render(frame+" "+m.toolStatus), border, m.width) + "\n")
	}

	// Show streaming text (with think blocks stripped)
	if m.isStream && m.streaming != "" {
		displayText, _ := stripThinkBlock(m.streaming)
		if displayText != "" {
			if m.markdown {
				rendered := RenderMarkdown(displayText, m.width-2)
				for i, line := range strings.Split(rendered, "\n") {
					if i > 0 {
						b.WriteString("\n")
					}
					b.WriteString("  " + line)
				}
			} else {
				b.WriteString("  " + StreamingStyle.Render(displayText))
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

	// Header: baryo · model · mode
	sep := DimStyle.Render(" · ")
	header := TitleStyle.Render("baryo") + sep +
		AssistantLabelStyle.Render(m.modelName) + sep +
		HelpStyle.Render(m.permissionMode)

	frame := spinnerFrames[m.spinFrame]
	// Separator line
	separator := DimStyle.Render(strings.Repeat("─", m.width))

	var status string
	if m.confirmPending {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("179")).Bold(true).Render("⚡ " + m.confirmPrompt)
	} else if m.toolStatus != "" {
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
		help := "enter send · ↑↓ scroll · ctrl+p/n history · ctrl+c quit"
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

		// Append session cost for cloud provider models.
		rightInfo := tokenStyled
		if m.promptPrice > 0 {
			costStr := fmt.Sprintf("$%.4f", m.sessionCost)
			rightInfo += TokenDimStyle.Render(" · "+costStr)
		}

		// Right-align the right info.
		helpWidth := lipgloss.Width(help)
		rightWidth := lipgloss.Width(tokenInfo)
		if m.promptPrice > 0 {
			rightWidth += 3 + len(fmt.Sprintf("$%.4f", m.sessionCost)) // " · " + cost
		}
		gap := m.width - helpWidth - rightWidth
		if gap < 2 {
			gap = 2
		}
		status = HelpStyle.Render(help) + strings.Repeat(" ", gap) + rightInfo
	}

	return fmt.Sprintf("%s\n%s\n%s\n%s\n%s",
		header,
		m.viewport.View(),
		m.textarea.View(),
		separator,
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

// summarizeToolArgs shortens a JSON args string for display.
func summarizeToolArgs(args string) string {
	args = strings.TrimSpace(args)
	if len(args) <= 80 {
		return args
	}
	return args[:77] + "..."
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
	const maxPreviewLines = 4
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
