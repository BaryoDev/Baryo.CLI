// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"github.com/arnelirobles/baryo-cli/internal/docker"
	"github.com/arnelirobles/baryo-cli/internal/session"
)

// ModelsLoadedMsg is sent when the model list has been fetched.
type ModelsLoadedMsg struct {
	Models []docker.DockerModel
	Err    error
}

// StreamTokenMsg carries a streaming event from the model.
type StreamTokenMsg struct {
	Event docker.StreamEvent
}

// ModelSelectedMsg is sent when the user picks a model.
type ModelSelectedMsg struct {
	Model docker.DockerModel
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
	Downloaded []docker.DockerModel
	Available  []docker.SearchModel
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
