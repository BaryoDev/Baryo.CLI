// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Providers maps provider names to their OpenAI-compatible base URLs.
var Providers = map[string]string{
	"gemini":     "https://generativelanguage.googleapis.com/v1beta/openai",
	"openrouter": "https://openrouter.ai/api/v1",
}

// ProviderEndpoint returns an Endpoint for the named provider.
func ProviderEndpoint(provider, apiKey string) Endpoint {
	return Endpoint{
		BaseURL: Providers[provider],
		APIKey:  apiKey,
	}
}

// LocalEndpoint wraps a socket path into an Endpoint.
func LocalEndpoint(socketPath string) Endpoint {
	return Endpoint{SocketPath: socketPath}
}

// providerModelsResponse is the response from /models with OpenRouter-specific fields.
type providerModelsResponse struct {
	Data []providerModelEntry `json:"data"`
}

type providerModelEntry struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContextLen   int    `json:"context_length"`
	Architecture struct {
		Modality string `json:"modality"`
	} `json:"architecture"`
	Pricing struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
}

// ListProviderModels queries a remote provider's /models endpoint and returns
// the available models, each tagged with the provider name.
func ListProviderModels(provider, apiKey string) ([]DockerModel, error) {
	baseURL, ok := Providers[provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}

	client := &http.Client{Timeout: 15 * time.Second}

	req, err := http.NewRequest("GET", baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", provider, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%s returned %d: %s", provider, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result providerModelsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse model list: %w", err)
	}

	var models []DockerModel
	for _, m := range result.Data {
		id := normalizeModelID(provider, m.ID)
		if !filterProviderModel(provider, id, m) {
			continue
		}
		promptPrice, completionPrice := parseProviderPricing(provider, m, id)
		dm := DockerModel{
			Name:            id,
			Tag:             id,
			Provider:        provider,
			PromptPrice:     promptPrice,
			CompletionPrice: completionPrice,
		}
		// Show context length as Params for provider models.
		if m.ContextLen > 0 {
			dm.Params = formatContextLen(m.ContextLen)
		}
		models = append(models, dm)
	}

	// For OpenRouter, sort by name for consistent ordering.
	if provider == "openrouter" {
		sort.Slice(models, func(i, j int) bool {
			return models[i].Name < models[j].Name
		})
	}

	return models, nil
}

// normalizeModelID cleans up provider-specific model ID prefixes.
func normalizeModelID(provider, id string) string {
	switch provider {
	case "gemini":
		// Gemini API returns "models/gemini-2.5-flash" — strip the prefix.
		return strings.TrimPrefix(id, "models/")
	default:
		return id
	}
}

// filterProviderModel returns true if the model ID should be shown for a provider.
func filterProviderModel(provider, id string, entry providerModelEntry) bool {
	switch provider {
	case "gemini":
		// Only keep chat-capable Gemini models; skip imagen, veo, embedding, aqa, etc.
		if !strings.HasPrefix(id, "gemini-") {
			return false
		}
		for _, skip := range []string{"image-generation", "-tts", "-native-audio", "embedding", "computer-use", "robotics"} {
			if strings.Contains(id, skip) {
				return false
			}
		}
		return true

	case "openrouter":
		// Only text-in/text-out models (skip image/audio generation).
		modality := entry.Architecture.Modality
		if modality != "" && modality != "text->text" && modality != "text+image->text" {
			return false
		}
		// Skip ":free" suffixed models (duplicates of paid versions).
		if strings.HasSuffix(id, ":free") {
			return false
		}
		return true

	default:
		return true
	}
}

// formatContextLen formats a context length as a human-readable string.
func formatContextLen(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%dM ctx", n/1_000_000)
	}
	return fmt.Sprintf("%dk ctx", n/1000)
}

// ModelPricing holds per-token pricing for a model.
type ModelPricing struct {
	PromptPrice     float64 // cost per prompt token
	CompletionPrice float64 // cost per completion token
}

// geminiPricing maps Gemini model prefixes to their per-token pricing.
// Prices are per token (Google quotes per 1M tokens, so divide by 1_000_000).
var geminiPricing = map[string]ModelPricing{
	"gemini-2.5-pro":   {PromptPrice: 1.25 / 1_000_000, CompletionPrice: 10.0 / 1_000_000},
	"gemini-2.5-flash": {PromptPrice: 0.30 / 1_000_000, CompletionPrice: 2.50 / 1_000_000},
	"gemini-2.0-flash": {PromptPrice: 0.10 / 1_000_000, CompletionPrice: 0.40 / 1_000_000},
}

// LookupGeminiPricing returns pricing for a Gemini model ID by matching prefixes.
func LookupGeminiPricing(modelID string) ModelPricing {
	for prefix, p := range geminiPricing {
		if strings.HasPrefix(modelID, prefix) {
			return p
		}
	}
	return ModelPricing{}
}

// parseProviderPricing extracts per-token pricing from a provider model entry.
func parseProviderPricing(provider string, entry providerModelEntry, modelID string) (prompt, completion float64) {
	switch provider {
	case "gemini":
		p := LookupGeminiPricing(modelID)
		return p.PromptPrice, p.CompletionPrice
	case "openrouter":
		// OpenRouter returns price per token as string (e.g. "0.00000025").
		prompt, _ = strconv.ParseFloat(entry.Pricing.Prompt, 64)
		completion, _ = strconv.ParseFloat(entry.Pricing.Completion, 64)
		return prompt, completion
	}
	return 0, 0
}
