// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package cli

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/arnelirobles/baryo-cli/internal/docker"
)

// RunPrint streams a single prompt/response to stdout without the TUI.
func RunPrint(ep docker.Endpoint, systemPrompt string, model docker.DockerModel, prompt string, params docker.ChatParams) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var messages []docker.ChatMessage
	if systemPrompt != "" {
		messages = append(messages, docker.NewChatMessage("system", systemPrompt))
	}
	messages = append(messages, docker.NewChatMessage("user", prompt))

	ch := docker.StreamChat(ctx, ep, model.Tag, messages, params)

	for evt := range ch {
		if evt.Error != "" {
			return fmt.Errorf("%s", evt.Error)
		}
		if evt.Token != "" {
			fmt.Print(evt.Token)
		}
		// Ignore tool events and Done in print mode.
	}

	fmt.Println()
	return nil
}
