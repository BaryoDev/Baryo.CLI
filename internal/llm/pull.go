// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// SearchModel represents a model returned by `docker model search`.
type SearchModel struct {
	Name        string `json:"Name"`
	Description string `json:"Description"`
	Downloads   int    `json:"Downloads"`
	Stars       int    `json:"Stars"`
	Source      string `json:"Source"`
}

// SearchModels runs `docker model search --json` and returns available models
// from Docker Hub.
func SearchModels() ([]SearchModel, error) {
	cmd := exec.Command("docker", "model", "search", "--json", "--source=dockerhub")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker model search failed: %w", err)
	}

	var models []SearchModel
	if err := json.Unmarshal(out, &models); err != nil {
		return nil, fmt.Errorf("failed to parse model search: %w", err)
	}

	return models, nil
}

// StreamPull runs `docker model pull <name>` and streams progress lines into
// the returned channel. The channel is closed when the pull completes.
// An "error: ..." line is sent if the pull fails.
func StreamPull(ctx context.Context, name string) <-chan string {
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		cmd := exec.CommandContext(ctx, "docker", "model", "pull", name)

		r, w, err := os.Pipe()
		if err != nil {
			ch <- "error: " + err.Error()
			return
		}
		cmd.Stdout = w
		cmd.Stderr = w

		if err := cmd.Start(); err != nil {
			w.Close()
			r.Close()
			ch <- "error: " + err.Error()
			return
		}
		w.Close()

		scanner := bufio.NewScanner(r)
		// Split on both \n and \r so we catch progress lines that use \r
		scanner.Split(splitLines)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				select {
				case ch <- line:
				case <-ctx.Done():
					r.Close()
					return
				}
			}
		}
		r.Close()

		if err := cmd.Wait(); err != nil {
			select {
			case ch <- "error: " + err.Error():
			case <-ctx.Done():
			}
		}
	}()
	return ch
}

// splitLines is a bufio.SplitFunc that splits on \n or \r.
func splitLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' || data[i] == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}
