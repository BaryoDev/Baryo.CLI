// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"fmt"
)

// validModes lists the permission modes the gate understands.
const validModes = "auto, confirm, suggest"

// unknownModeMessage is the refusal for a mode the gate does not know. An
// unknown or empty mode must never run a call that needs approval.
func unknownModeMessage(mode, name string) string {
	return fmt.Sprintf("[blocked] %s not run, unknown permission mode %q (valid modes: %s)", name, mode, validModes)
}

// confirmGate applies the permission mode to a call that needs approval.
// It returns a message and true when the call must not proceed.
//
// One gate for native destructive tools and for MCP tools that are not known to
// be read-only, so the two cannot drift apart.
func confirmGate(ctx context.Context, mode string, ch chan confirmRequest, name, argsJSON string) (string, bool) {
	switch mode {
	case "auto":
		return "", false
	case "suggest":
		return fmt.Sprintf("[suggest mode] Would run %s, approve with --yolo or permission_mode: auto", name), true
	case "confirm":
		respCh := make(chan bool, 1)
		select {
		case ch <- confirmRequest{
			Name:   name,
			Args:   argsJSON,
			Prompt: formatConfirmPrompt(name, argsJSON),
			RespCh: respCh,
		}:
		case <-ctx.Done():
			return "cancelled", true
		}
		select {
		case approved := <-respCh:
			if !approved {
				return fmt.Sprintf("[denied] %s was not approved", name), true
			}
		case <-ctx.Done():
			return "cancelled", true
		}
	default:
		return unknownModeMessage(mode, name), true
	}
	return "", false
}
