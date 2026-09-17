// SPDX-License-Identifier: MIT

package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ExportSchema is the version of the request an exporter receives. An exporter should
// refuse a schema it does not know rather than guess.
const ExportSchema = "baryo.exporter.v1"

// exportTimeout bounds one exporter run. Generous, because an export walks every session.
const exportTimeout = 10 * time.Minute

// SessionRef describes one session to export. Paths, not content: a session's archive can
// be large, and an exporter may want to stream it rather than receive it down a pipe.
type SessionRef struct {
	ID           string `json:"id"`
	Title        string `json:"title,omitempty"`
	ModelName    string `json:"model_name,omitempty"`
	ModelTag     string `json:"model_tag,omitempty"`
	CWD          string `json:"cwd,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	MessagesPath string `json:"messages_path"`
	ArchivePath  string `json:"archive_path,omitempty"`
	TracePath    string `json:"trace_path,omitempty"`
}

// ExportRequest is the JSON object written to an exporter's stdin.
type ExportRequest struct {
	Schema      string       `json:"schema"`
	BaryoVer    string       `json:"baryo_version"`
	SessionsDir string       `json:"sessions_dir"`
	Sessions    []SessionRef `json:"sessions"`
	Since       string       `json:"since,omitempty"`
	OutDir      string       `json:"out_dir"`
}

// ExportResponse is the JSON object an exporter writes to stdout.
type ExportResponse struct {
	OK       bool     `json:"ok"`
	Written  []string `json:"written,omitempty"`
	Sessions int      `json:"sessions,omitempty"`
	Events   int      `json:"events,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// RunExporter runs one exporter capability and returns what it reported.
//
// Failure modes are kept apart on purpose, because they need different answers from the
// user: a plugin that could not be run at all, a plugin that ran and failed, and a plugin
// whose output could not be understood are three different problems and the error text
// says which.
func RunExporter(ctx context.Context, m Manifest, c Capability, req ExportRequest) (ExportResponse, error) {
	full, err := m.resolveCommand(c.Command)
	if err != nil {
		// Re-checked at run time rather than trusted from load time: the file could have
		// been swapped for a symlink out of the directory in between.
		return ExportResponse{}, fmt.Errorf("plugin %s: %w", m.Name, err)
	}

	req.Schema = ExportSchema
	body, err := json.Marshal(req)
	if err != nil {
		return ExportResponse{}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, exportTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, full, c.Args...)
	cmd.Dir = m.Dir
	cmd.Stdin = bytes.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Kill the whole run rather than block on a pipe a grandchild still holds. Without
	// this, a plugin that spawns something long-lived keeps the export hanging well past
	// its timeout, because Wait is waiting on the inherited pipe, not the process.
	cmd.WaitDelay = 5 * time.Second

	runErr := cmd.Run()

	// Parse stdout even on a non-zero exit: a well-behaved plugin reports why it failed
	// in its response, and that message is more useful than the exit code.
	var resp ExportResponse
	parsed := false
	if trimmed := bytes.TrimSpace(stdout.Bytes()); len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &resp); err == nil {
			parsed = true
		}
	}

	if runErr != nil {
		detail := strings.TrimSpace(stderr.String())
		if parsed && resp.Error != "" {
			detail = resp.Error
		}
		if runCtx.Err() == context.DeadlineExceeded {
			return resp, fmt.Errorf("plugin %s timed out after %v", m.Name, exportTimeout)
		}
		if detail == "" {
			detail = runErr.Error()
		}
		return resp, fmt.Errorf("plugin %s failed: %s", m.Name, truncate(detail, 2000))
	}

	if !parsed {
		return resp, fmt.Errorf("plugin %s wrote no usable response on stdout (stderr: %s)",
			m.Name, truncate(strings.TrimSpace(stderr.String()), 500))
	}
	if !resp.OK {
		msg := resp.Error
		if msg == "" {
			msg = "reported failure without a reason"
		}
		return resp, fmt.Errorf("plugin %s: %s", m.Name, truncate(msg, 2000))
	}
	return resp, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "... (truncated)"
}
