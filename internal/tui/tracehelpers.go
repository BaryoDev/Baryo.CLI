// SPDX-License-Identifier: MIT

package tui

import (
	"os"
	"os/exec"
	"strings"

	"github.com/arnelirobles/baryo-cli/internal/llm"
	"github.com/arnelirobles/baryo-cli/internal/session"
	"github.com/arnelirobles/baryo-cli/internal/trace"
)

// traceEnabled mirrors the trace config key. It is a package switch set once at
// startup rather than a thirteenth constructor parameter.
var traceEnabled = true

// SetTraceEnabled turns trajectory recording on or off for this process.
func SetTraceEnabled(on bool) { traceEnabled = on }

// openRecorder attaches a trace recorder for the current session, replacing any
// previous one. A failure to open is not worth interrupting a chat for: the
// recorder stays nil and every call on it is a no-op.
func (m *ChatModel) openRecorder(providerKeys map[string]string) {
	if m.recorder != nil {
		_ = m.recorder.Close()
		m.recorder = nil
	}
	if !traceEnabled || m.session == nil {
		return
	}
	path, err := session.TracePath(m.session.ID)
	if err != nil {
		return
	}
	secrets := make([]string, 0, len(providerKeys))
	for _, v := range providerKeys {
		secrets = append(secrets, v)
	}
	if rec, err := trace.New(path, secrets); err == nil {
		m.recorder = rec
	}
}

// endpointLabel names the endpoint a task ran against, without leaking a key.
func endpointLabel(ep llm.Endpoint) string {
	if ep.Provider != "" {
		return ep.Provider
	}
	if ep.BaseURL != "" {
		return "remote"
	}
	return "local"
}

// workingDir returns the current directory, or "" if it cannot be determined.
func workingDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

// gitHead returns the short HEAD sha, or "" outside a repository. A trace is
// only useful if it says which state of the tree it describes.
func gitHead() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
