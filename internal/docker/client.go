// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

// SocketPath returns the Docker Model Runner inference socket path.
func SocketPath() string {
	if p := os.Getenv("DOCKER_MODEL_SOCKET"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return home + "/Library/Containers/com.docker.docker/Data/inference.sock"
}

// newHTTPClient creates an HTTP client that dials the unix socket.
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", SocketPath())
			},
		},
	}
}

// StreamChat sends a chat request and streams tokens into the returned channel.
// The channel is closed when streaming ends. Errors are sent as a single
// token prefixed with "error:".
func StreamChat(ctx context.Context, model string, messages []ChatMessage) <-chan string {
	ch := make(chan string, 64)

	go func() {
		defer close(ch)

		reqBody := ChatRequest{
			Model:    model,
			Messages: messages,
			Stream:   true,
		}

		body, err := json.Marshal(reqBody)
		if err != nil {
			ch <- fmt.Sprintf("error: %v", err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, "POST",
			"http://localhost/v1/chat/completions",
			bytes.NewReader(body))
		if err != nil {
			ch <- fmt.Sprintf("error: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		client := newHTTPClient()
		resp, err := client.Do(req)
		if err != nil {
			ch <- fmt.Sprintf("error: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			ch <- fmt.Sprintf("error: server returned %d", resp.StatusCode)
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()

			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			if data == "[DONE]" {
				return
			}

			var chunk StreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					select {
					case ch <- choice.Delta.Content:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch
}
