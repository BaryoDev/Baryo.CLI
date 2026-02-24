// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// ListModels runs `docker model list --json` and returns the available models.
func ListModels() ([]DockerModel, error) {
	cmd := exec.Command("docker", "model", "list", "--json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker model list failed: %w", err)
	}

	var raw []dockerModelRaw
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse model list: %w", err)
	}

	models := make([]DockerModel, 0, len(raw))
	for _, r := range raw {
		if len(r.Tags) == 0 {
			continue
		}
		tag := r.Tags[0]
		// Extract short name: "docker.io/ai/mistral:latest" -> "ai/mistral"
		name := tag
		name = strings.TrimPrefix(name, "docker.io/")
		if idx := strings.LastIndex(name, ":"); idx != -1 {
			name = name[:idx]
		}

		models = append(models, DockerModel{
			Name:   name,
			Tag:    tag,
			Params: r.Config.Parameters,
			Size:   r.Config.Size,
		})
	}

	return models, nil
}

// ollamaTagsResponse matches the JSON from Ollama's /api/tags endpoint.
type ollamaTagsResponse struct {
	Models []struct {
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		Details struct {
			ParameterSize string `json:"parameter_size"`
			Family        string `json:"family"`
		} `json:"details"`
	} `json:"models"`
}

// ListRemoteModels queries a remote Ollama server for available models.
// socketPath should be a TCP address like "tcp://localhost:11434".
func ListRemoteModels(socketPath string) ([]DockerModel, error) {
	client := &http.Client{
		Transport: newHTTPClient(socketPath).Transport,
		Timeout:   15 * time.Second,
	}

	resp, err := client.Get("http://localhost/api/tags")
	if err != nil {
		// Try OpenAI-compatible endpoint as fallback.
		return listRemoteModelsOpenAI(client)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return listRemoteModelsOpenAI(client)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var tags ollamaTagsResponse
	if err := json.Unmarshal(body, &tags); err != nil {
		return nil, fmt.Errorf("failed to parse model list: %w", err)
	}

	models := make([]DockerModel, 0, len(tags.Models))
	for _, m := range tags.Models {
		name := m.Name
		// Strip ":latest" for display.
		displayName := strings.TrimSuffix(name, ":latest")

		size := ""
		if m.Size > 0 {
			size = formatBytes(m.Size)
		}

		models = append(models, DockerModel{
			Name:   displayName,
			Tag:    name,
			Params: m.Details.ParameterSize,
			Size:   size,
		})
	}

	return models, nil
}

// openAIModelsResponse matches the JSON from /v1/models.
type openAIModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// listRemoteModelsOpenAI queries via the OpenAI-compatible /v1/models endpoint.
func listRemoteModelsOpenAI(client *http.Client) ([]DockerModel, error) {
	resp, err := client.Get("http://localhost/v1/models")
	if err != nil {
		return nil, fmt.Errorf("cannot reach server: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result openAIModelsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse model list: %w", err)
	}

	models := make([]DockerModel, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, DockerModel{
			Name: m.ID,
			Tag:  m.ID,
		})
	}

	return models, nil
}

// PreloadModel sends a request to Ollama to load the model into memory.
// Ollama's /api/generate with keep_alive loads the model without generating.
// Blocks until the model is loaded or an error occurs.
func PreloadModel(socketPath, model string) error {
	client := &http.Client{
		Transport: newHTTPClient(socketPath).Transport,
		Timeout:   10 * time.Minute,
	}

	body := fmt.Sprintf(`{"model":%q,"keep_alive":"10m"}`, model)
	resp, err := client.Post("http://localhost/api/generate", "application/json", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to preload model: %w", err)
	}
	defer resp.Body.Close()
	// Drain the response body so the connection is properly closed.
	io.Copy(io.Discard, resp.Body)
	return nil
}

func formatBytes(b int64) string {
	const gib = 1024 * 1024 * 1024
	const mib = 1024 * 1024
	if b >= gib {
		return fmt.Sprintf("%.1f GiB", float64(b)/float64(gib))
	}
	return fmt.Sprintf("%.0f MiB", float64(b)/float64(mib))
}
