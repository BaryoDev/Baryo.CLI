// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package llm

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

// Providers maps provider names to their base URLs.
var Providers = map[string]string{
	"gemini":      "https://generativelanguage.googleapis.com/v1beta/openai",
	"openrouter":  "https://openrouter.ai/api/v1",
	"openai":      "https://api.openai.com/v1",
	"anthropic":   "https://api.anthropic.com/v1",
	"groq":        "https://api.groq.com/openai/v1",
	"mistral":     "https://api.mistral.ai/v1",
	"together":    "https://api.together.xyz/v1",
	"fireworks":   "https://api.fireworks.ai/inference/v1",
	"deepseek":    "https://api.deepseek.com",
	"xai":         "https://api.x.ai/v1",
	"cerebras":    "https://api.cerebras.ai/v1",
	"perplexity":  "https://api.perplexity.ai",
	"sambanova":   "https://api.sambanova.ai/v1",
	"cohere":      "https://api.cohere.ai/compatibility/v1",
	"huggingface": "https://router.huggingface.co/v1",
	"github":      "https://models.github.ai/inference",
	"ollama":      "https://ollama.com/v1",
}

// prefixToProvider maps model name prefixes to provider names for quick detection.
var prefixToProvider = []struct {
	prefixes []string
	provider string
}{
	{[]string{"anthropic.", "amazon.", "meta.", "mistral.", "cohere.", "ai21."}, "bedrock"},
	{[]string{"gemini-"}, "gemini"},
	{[]string{"gpt-", "o1", "o3", "chatgpt-"}, "openai"},
	{[]string{"claude-"}, "anthropic"},
	{[]string{"deepseek-"}, "deepseek"},
	{[]string{"grok-"}, "xai"},
	{[]string{"mistral-", "codestral", "pixtral"}, "mistral"},
	{[]string{"sonar"}, "perplexity"},
	{[]string{"command-"}, "cohere"},
}

// DetectProvider guesses the provider from a model tag string.
func DetectProvider(modelTag string) string {
	lower := strings.ToLower(modelTag)
	for _, entry := range prefixToProvider {
		for _, prefix := range entry.prefixes {
			if strings.HasPrefix(lower, prefix) {
				return entry.provider
			}
		}
	}
	return ""
}

// ProviderEndpoint returns an Endpoint for the named provider.
func ProviderEndpoint(provider, apiKey string) Endpoint {
	ep := Endpoint{
		BaseURL:  Providers[provider],
		APIKey:   apiKey,
		Provider: provider,
	}
	if provider == "bedrock" {
		ep.Region = apiKey
		ep.APIKey = ""
	}
	return ep
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
func ListProviderModels(provider, apiKey string) ([]Model, error) {
	// Anthropic uses a hardcoded model list (no /models endpoint with pricing).
	if provider == "anthropic" {
		return listAnthropicModels(), nil
	}

	// Bedrock uses the AWS SDK to list foundation models.
	if provider == "bedrock" {
		return listBedrockModels(apiKey)
	}

	// GitHub Models uses a separate catalog API.
	if provider == "github" {
		return listGitHubModels(apiKey)
	}

	// Ollama Cloud uses a hardcoded model list.
	if provider == "ollama" {
		return listOllamaCloudModels(), nil
	}

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

	var models []Model
	for _, m := range result.Data {
		id := normalizeModelID(provider, m.ID)
		if !filterProviderModel(provider, id, m) {
			continue
		}
		promptPrice, completionPrice := parseProviderPricing(provider, m, id)
		dm := Model{
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

	case "openai":
		// Keep chat models; skip embedding, tts, dall-e, whisper, moderation.
		for _, skip := range []string{"embedding", "tts", "dall-e", "whisper", "moderation", "davinci", "babbage"} {
			if strings.Contains(id, skip) {
				return false
			}
		}
		return true

	case "mistral":
		// Skip embedding models.
		if strings.Contains(id, "embed") {
			return false
		}
		return true

	case "deepseek":
		// Only keep chat and reasoner models.
		return id == "deepseek-chat" || id == "deepseek-reasoner"

	case "cohere":
		// Skip non-chat models (embeddings, reranking); keep everything else.
		for _, skip := range []string{"embed", "rerank"} {
			if strings.Contains(id, skip) {
				return false
			}
		}
		return true

	case "huggingface":
		// HF /models returns all task types; keep only chat-capable models.
		// HF model IDs use org/model format (contain a slash).
		if !strings.Contains(id, "/") {
			return false
		}
		for _, skip := range []string{"embed", "image", "audio", "tts", "whisper", "vit", "clip", "bert", "diffus"} {
			if strings.Contains(strings.ToLower(id), skip) {
				return false
			}
		}
		return true

	case "bedrock":
		// Bedrock filtering is done in listBedrockModels; accept everything here.
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
	case "openai":
		p := LookupOpenAIPricing(modelID)
		return p.PromptPrice, p.CompletionPrice
	case "bedrock":
		p := LookupBedrockPricing(modelID)
		return p.PromptPrice, p.CompletionPrice
	case "groq", "mistral", "together", "fireworks", "deepseek", "xai", "cerebras", "perplexity", "sambanova", "cohere":
		p := lookupProviderPricing(provider, modelID)
		return p.PromptPrice, p.CompletionPrice
	}
	return 0, 0
}

// openaiPricing maps OpenAI model prefixes to their per-token pricing.
var openaiPricing = map[string]ModelPricing{
	"gpt-4o":       {PromptPrice: 2.50 / 1_000_000, CompletionPrice: 10.0 / 1_000_000},
	"gpt-4o-mini":  {PromptPrice: 0.15 / 1_000_000, CompletionPrice: 0.60 / 1_000_000},
	"gpt-4.1":      {PromptPrice: 2.00 / 1_000_000, CompletionPrice: 8.00 / 1_000_000},
	"gpt-4.1-mini": {PromptPrice: 0.40 / 1_000_000, CompletionPrice: 1.60 / 1_000_000},
	"gpt-4.1-nano": {PromptPrice: 0.10 / 1_000_000, CompletionPrice: 0.40 / 1_000_000},
	"o3":           {PromptPrice: 2.00 / 1_000_000, CompletionPrice: 8.00 / 1_000_000},
	"o3-mini":      {PromptPrice: 1.10 / 1_000_000, CompletionPrice: 4.40 / 1_000_000},
	"o1":           {PromptPrice: 15.0 / 1_000_000, CompletionPrice: 60.0 / 1_000_000},
	"o1-mini":      {PromptPrice: 1.10 / 1_000_000, CompletionPrice: 4.40 / 1_000_000},
}

// LookupOpenAIPricing returns pricing for an OpenAI model ID by matching prefixes.
func LookupOpenAIPricing(modelID string) ModelPricing {
	// Try longest prefix first for specificity (e.g. "gpt-4o-mini" before "gpt-4o").
	best := ""
	var bestPricing ModelPricing
	for prefix, p := range openaiPricing {
		if strings.HasPrefix(modelID, prefix) && len(prefix) > len(best) {
			best = prefix
			bestPricing = p
		}
	}
	return bestPricing
}

// bedrockPricing maps Bedrock model prefixes to their per-token pricing.
var bedrockPricing = map[string]ModelPricing{
	"anthropic.claude-opus-4":     {PromptPrice: 15.0 / 1_000_000, CompletionPrice: 75.0 / 1_000_000},
	"anthropic.claude-sonnet-4":   {PromptPrice: 3.0 / 1_000_000, CompletionPrice: 15.0 / 1_000_000},
	"anthropic.claude-3-5-sonnet": {PromptPrice: 3.0 / 1_000_000, CompletionPrice: 15.0 / 1_000_000},
	"anthropic.claude-3-5-haiku":  {PromptPrice: 0.80 / 1_000_000, CompletionPrice: 4.0 / 1_000_000},
	"amazon.nova-pro":             {PromptPrice: 0.80 / 1_000_000, CompletionPrice: 3.20 / 1_000_000},
	"amazon.nova-lite":            {PromptPrice: 0.06 / 1_000_000, CompletionPrice: 0.24 / 1_000_000},
	"amazon.nova-micro":           {PromptPrice: 0.035 / 1_000_000, CompletionPrice: 0.14 / 1_000_000},
	"meta.llama3-1-70b":           {PromptPrice: 0.72 / 1_000_000, CompletionPrice: 0.72 / 1_000_000},
	"meta.llama3-1-8b":            {PromptPrice: 0.22 / 1_000_000, CompletionPrice: 0.22 / 1_000_000},
	"mistral.mistral-large":       {PromptPrice: 2.0 / 1_000_000, CompletionPrice: 6.0 / 1_000_000},
}

// LookupBedrockPricing returns pricing for a Bedrock model ID by matching prefixes.
func LookupBedrockPricing(modelID string) ModelPricing {
	best := ""
	var bestPricing ModelPricing
	for prefix, p := range bedrockPricing {
		if strings.HasPrefix(modelID, prefix) && len(prefix) > len(best) {
			best = prefix
			bestPricing = p
		}
	}
	return bestPricing
}

// anthropicPricing maps Anthropic model prefixes to their per-token pricing.
var anthropicPricing = map[string]ModelPricing{
	"claude-opus-4":     {PromptPrice: 15.0 / 1_000_000, CompletionPrice: 75.0 / 1_000_000},
	"claude-sonnet-4":   {PromptPrice: 3.0 / 1_000_000, CompletionPrice: 15.0 / 1_000_000},
	"claude-3-5-haiku":  {PromptPrice: 0.80 / 1_000_000, CompletionPrice: 4.0 / 1_000_000},
	"claude-3-5-sonnet": {PromptPrice: 3.0 / 1_000_000, CompletionPrice: 15.0 / 1_000_000},
}

// LookupAnthropicPricing returns pricing for an Anthropic model ID by matching prefixes.
func LookupAnthropicPricing(modelID string) ModelPricing {
	best := ""
	var bestPricing ModelPricing
	for prefix, p := range anthropicPricing {
		if strings.HasPrefix(modelID, prefix) && len(prefix) > len(best) {
			best = prefix
			bestPricing = p
		}
	}
	return bestPricing
}

// providerPricingTables maps provider names to their model pricing tables.
var providerPricingTables = map[string]map[string]ModelPricing{
	"groq": {
		"llama-3.3-70b": {PromptPrice: 0.59 / 1_000_000, CompletionPrice: 0.79 / 1_000_000},
		"llama-3.1-8b":  {PromptPrice: 0.05 / 1_000_000, CompletionPrice: 0.08 / 1_000_000},
		"gemma2-9b":     {PromptPrice: 0.20 / 1_000_000, CompletionPrice: 0.20 / 1_000_000},
		"mistral-saba":  {PromptPrice: 0.20 / 1_000_000, CompletionPrice: 0.60 / 1_000_000},
	},
	"mistral": {
		"mistral-large": {PromptPrice: 2.00 / 1_000_000, CompletionPrice: 6.00 / 1_000_000},
		"mistral-small": {PromptPrice: 0.10 / 1_000_000, CompletionPrice: 0.30 / 1_000_000},
		"codestral":     {PromptPrice: 0.30 / 1_000_000, CompletionPrice: 0.90 / 1_000_000},
		"mistral-nemo":  {PromptPrice: 0.15 / 1_000_000, CompletionPrice: 0.15 / 1_000_000},
		"pixtral-large": {PromptPrice: 2.00 / 1_000_000, CompletionPrice: 6.00 / 1_000_000},
	},
	"deepseek": {
		"deepseek-chat":     {PromptPrice: 0.27 / 1_000_000, CompletionPrice: 1.10 / 1_000_000},
		"deepseek-reasoner": {PromptPrice: 0.55 / 1_000_000, CompletionPrice: 2.19 / 1_000_000},
	},
	"xai": {
		"grok-3":      {PromptPrice: 3.00 / 1_000_000, CompletionPrice: 15.0 / 1_000_000},
		"grok-3-mini": {PromptPrice: 0.30 / 1_000_000, CompletionPrice: 0.50 / 1_000_000},
		"grok-2":      {PromptPrice: 2.00 / 1_000_000, CompletionPrice: 10.0 / 1_000_000},
	},
	"perplexity": {
		"sonar-pro":       {PromptPrice: 3.00 / 1_000_000, CompletionPrice: 15.0 / 1_000_000},
		"sonar":           {PromptPrice: 1.00 / 1_000_000, CompletionPrice: 1.00 / 1_000_000},
		"sonar-reasoning": {PromptPrice: 1.00 / 1_000_000, CompletionPrice: 5.00 / 1_000_000},
	},
	"cohere": {
		"command-a":      {PromptPrice: 2.50 / 1_000_000, CompletionPrice: 10.0 / 1_000_000},
		"command-r-plus": {PromptPrice: 2.50 / 1_000_000, CompletionPrice: 10.0 / 1_000_000},
		"command-r7b":    {PromptPrice: 0.0375 / 1_000_000, CompletionPrice: 0.15 / 1_000_000},
		"command-r":      {PromptPrice: 0.15 / 1_000_000, CompletionPrice: 0.60 / 1_000_000},
	},
}

// lookupProviderPricing returns pricing for a model by matching prefixes
// in the provider-specific pricing table.
func lookupProviderPricing(provider, modelID string) ModelPricing {
	table, ok := providerPricingTables[provider]
	if !ok {
		return ModelPricing{}
	}
	best := ""
	var bestPricing ModelPricing
	for prefix, p := range table {
		if strings.HasPrefix(modelID, prefix) && len(prefix) > len(best) {
			best = prefix
			bestPricing = p
		}
	}
	return bestPricing
}

// LookupPricing returns pricing for a model by dispatching to the correct
// provider-specific table. This is the single entry point for all pricing lookups.
func LookupPricing(provider, modelID string) ModelPricing {
	switch provider {
	case "gemini":
		return LookupGeminiPricing(modelID)
	case "openai":
		return LookupOpenAIPricing(modelID)
	case "anthropic":
		return LookupAnthropicPricing(modelID)
	case "bedrock":
		return LookupBedrockPricing(modelID)
	default:
		return lookupProviderPricing(provider, modelID)
	}
}

// anthropicModelDef describes a hardcoded Anthropic model for the picker.
type anthropicModelDef struct {
	ID         string
	ContextLen int
}

// listAnthropicModels returns the hardcoded list of Anthropic chat models.
func listAnthropicModels() []Model {
	defs := []anthropicModelDef{
		{"claude-opus-4-20250514", 200000},
		{"claude-sonnet-4-20250514", 200000},
		{"claude-3-5-haiku-20241022", 200000},
		{"claude-3-5-sonnet-20241022", 200000},
	}
	models := make([]Model, len(defs))
	for i, d := range defs {
		p := LookupAnthropicPricing(d.ID)
		models[i] = Model{
			Name:            d.ID,
			Tag:             d.ID,
			Provider:        "anthropic",
			Params:          formatContextLen(d.ContextLen),
			PromptPrice:     p.PromptPrice,
			CompletionPrice: p.CompletionPrice,
		}
	}
	return models
}

// listOllamaCloudModels returns the hardcoded list of Ollama Cloud models.
func listOllamaCloudModels() []Model {
	defs := []anthropicModelDef{
		{"qwen3-coder:480b-cloud", 32000},
		{"gpt-oss:120b-cloud", 128000},
		{"gpt-oss:20b-cloud", 128000},
		{"deepseek-v3.1:671b-cloud", 128000},
	}
	models := make([]Model, len(defs))
	for i, d := range defs {
		models[i] = Model{
			Name:     d.ID,
			Tag:      d.ID,
			Provider: "ollama",
			Params:   formatContextLen(d.ContextLen),
		}
	}
	return models
}

// gitHubCatalogEntry represents a model from the GitHub Models catalog API.
type gitHubCatalogEntry struct {
	ID                        string   `json:"id"`
	Name                      string   `json:"name"`
	Publisher                 string   `json:"publisher"`
	Summary                   string   `json:"summary"`
	Capabilities              []string `json:"capabilities"`
	SupportedInputModalities  []string `json:"supported_input_modalities"`
	SupportedOutputModalities []string `json:"supported_output_modalities"`
	Limits                    struct {
		MaxInputTokens  int `json:"max_input_tokens"`
		MaxOutputTokens int `json:"max_output_tokens"`
	} `json:"limits"`
}

// listGitHubModels fetches models from the GitHub Models catalog API.
func listGitHubModels(apiKey string) ([]Model, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	req, err := http.NewRequest("GET", "https://models.github.ai/catalog/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach github models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("github models returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var entries []gitHubCatalogEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("failed to parse model catalog: %w", err)
	}

	var models []Model
	for _, e := range entries {
		// Only keep models that support text output and chat capability.
		if !sliceContains(e.SupportedOutputModalities, "text") {
			continue
		}
		if !sliceContains(e.SupportedInputModalities, "text") {
			continue
		}

		contextLen := e.Limits.MaxInputTokens
		dm := Model{
			Name:     e.ID,
			Tag:      e.ID,
			Provider: "github",
		}
		if contextLen > 0 {
			dm.Params = formatContextLen(contextLen)
		}
		models = append(models, dm)
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].Name < models[j].Name
	})

	return models, nil
}

// sliceContains returns true if the slice contains the given string.
func sliceContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
