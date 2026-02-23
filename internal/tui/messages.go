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

// StreamTokenMsg carries a single token from the streaming response.
type StreamTokenMsg struct {
	Token string
	Done  bool
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
