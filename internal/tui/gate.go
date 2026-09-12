// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"context"
	"fmt"
)

// confirmGate applies the permission mode to a call that needs approval.
// It returns a message and true when the call must not proceed.
//
// One gate for native destructive tools and for MCP tools that are not known to
// be read-only, so the two cannot drift apart.
func confirmGate(ctx context.Context, mode string, ch chan confirmRequest, name, argsJSON string) (string, bool) {
	switch mode {
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
	}
	// "auto" proceeds.
	return "", false
}
