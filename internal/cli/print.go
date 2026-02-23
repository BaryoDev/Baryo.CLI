// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package cli

import (
	"context"
	"fmt"
	"os/signal"
	"strings"
	"syscall"

	"github.com/arnelirobles/baryo-cli/internal/docker"
)

// RunPrint streams a single prompt/response to stdout without the TUI.
func RunPrint(model docker.DockerModel, prompt string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	messages := []docker.ChatMessage{
		{Role: "user", Content: prompt},
	}

	ch := docker.StreamChat(ctx, model.Tag, messages)

	for token := range ch {
		if strings.HasPrefix(token, "error:") {
			return fmt.Errorf("%s", strings.TrimPrefix(token, "error: "))
		}
		fmt.Print(token)
	}

	fmt.Println()
	return nil
}
