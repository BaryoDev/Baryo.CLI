// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"github.com/arnelirobles/baryo-cli/internal/index"
	"github.com/arnelirobles/baryo-cli/internal/llm"
	"github.com/arnelirobles/baryo-cli/internal/rag"
	"github.com/arnelirobles/baryo-cli/internal/search"
	"github.com/arnelirobles/baryo-cli/internal/session"
	"github.com/arnelirobles/baryo-cli/internal/setup"
)

// ModelsLoadedMsg is sent when the model list has been fetched.
type ModelsLoadedMsg struct {
	Models    []llm.Model
	Available []llm.SearchModel // Docker Hub models (may be nil)
	Err       error
}

// StreamTokenMsg carries a streaming event from the model.
type StreamTokenMsg struct {
	Event llm.StreamEvent
}

// ModelSelectedMsg is sent when the user picks a model.
type ModelSelectedMsg struct {
	Model llm.Model
}

// ShowSessionsMsg requests transition to the session picker screen.
type ShowSessionsMsg struct {
	Sessions []session.Summary
}

// SessionLoadedMsg is sent when a session has been loaded from disk.
type SessionLoadedMsg struct {
	Session *session.Session
	Err     error
}

// SessionCancelledMsg is sent when the user presses esc on the session picker.
type SessionCancelledMsg struct{}

// ShowModelsMsg requests transition to the model browser screen.
type ShowModelsMsg struct {
	Downloaded []llm.Model
	Available  []llm.SearchModel
	Err        error
}

// PullStatusMsg carries a progress line from docker model pull.
type PullStatusMsg struct {
	Status string
	Done   bool
}

// ModelBrowserCancelMsg is sent when the user presses esc on the model browser.
type ModelBrowserCancelMsg struct{}

// SearchResultMsg carries web search results back to the chat.
type SearchResultMsg struct {
	Query   string
	Results string
	Err     error
}

// FetchResultMsg carries fetched URL content back to the chat.
type FetchResultMsg struct {
	URL     string
	Content string
	Err     error
}

// MentionCandidatesMsg carries async glob results for @-mention completion.
type MentionCandidatesMsg struct {
	Prefix     string   // the partial text that was globbed
	StartPos   int      // byte position of @ in the text
	Candidates []string // matched file paths
}

// DiffResultMsg carries git diff output back to the chat.
type DiffResultMsg struct {
	Output string
	Err    error
}

// RunResultMsg carries shell command output back to the chat.
type RunResultMsg struct {
	Command string
	Output  string
	Err     error
}

// CommitResultMsg carries the result of a git commit back to the chat.
type CommitResultMsg struct {
	Message string
	Err     error
}

// confirmRequest is sent from the executor goroutine to the TUI when a
// destructive tool needs user approval.
type confirmRequest struct {
	Name    string    // tool name
	Args    string    // raw JSON args
	Prompt  string    // human-friendly prompt text
	RespCh  chan bool // executor blocks on this; TUI sends true/false
}

// ToolConfirmMsg is the Bubble Tea message that carries a confirm request
// from the channel listener into the Update loop.
type ToolConfirmMsg struct {
	Req confirmRequest
}

// RewriteDoneMsg is sent when the prompt rewrite pass completes.
type RewriteDoneMsg struct {
	Original  string
	Rewritten string
	HasTools  bool
	HasSkill  bool
}

// ResearchProgressMsg carries a status update from the research pipeline.
type ResearchProgressMsg struct {
	Status string
}

// ResearchDoneMsg signals that the research pipeline has finished.
type ResearchDoneMsg struct {
	Result search.ResearchResult
	Err    error
}

// SetupPromptMsg triggers the first-run setup prompt.
type SetupPromptMsg struct{}

// SetupProgressMsg carries a download progress update from the setup pipeline.
type SetupProgressMsg struct {
	Progress setup.DownloadProgress
}

// SetupDoneMsg signals that skill setup has completed.
type SetupDoneMsg struct {
	Installed int
	Err       error
}

// StrategyLoadedMsg carries parsed strategy JSON back to the chat.
type StrategyLoadedMsg struct {
	Goal        string
	Facts       string
	Constraints string
	Context     string
	Path        string
	Err         error
}

// StrategySearchProgressMsg carries a status update from the knowledge gap search pipeline.
type StrategySearchProgressMsg struct {
	Status string
}

// StrategySearchDoneMsg signals that knowledge gap searches have finished.
type StrategySearchDoneMsg struct {
	Queries []string // the queries that were run
	Results []string // corresponding results from DeepQuery
	Err     error
}

// RepoMapReadyMsg signals that the background repo index has finished building.
type RepoMapReadyMsg struct {
	Index *index.Index
}

// RepoMapUpdatedMsg signals that an incremental repo map refresh has completed.
type RepoMapUpdatedMsg struct{}

// RAGReadyMsg signals that the background RAG index has finished building.
type RAGReadyMsg struct {
	RAG *rag.RAG
}

// SourceIndexReadyMsg signals that background source file indexing has completed.
type SourceIndexReadyMsg struct{}

