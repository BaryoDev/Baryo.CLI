package llm

import (
	"testing"
)

func TestDetectProvider(t *testing.T) {
	tests := []struct {
		modelTag string
		want     string
	}{
		// Bedrock
		{"anthropic.claude-3-5-sonnet-20241022-v2:0", "bedrock"},
		{"amazon.nova-pro-v1:0", "bedrock"},
		{"meta.llama3-1-70b-instruct-v1:0", "bedrock"},
		{"mistral.mistral-large-2411-v1:0", "bedrock"},
		{"cohere.command-r-plus-v1:0", "bedrock"},
		{"ai21.j2-ultra-v1", "bedrock"},

		// Gemini
		{"gemini-2.5-flash", "gemini"},
		{"gemini-2.5-pro-preview", "gemini"},
		{"gemini-2.0-flash", "gemini"},

		// OpenAI
		{"gpt-4o", "openai"},
		{"gpt-4o-mini", "openai"},
		{"gpt-4.1", "openai"},
		{"o1-mini", "openai"},
		{"o3-mini", "openai"},
		{"chatgpt-4o-latest", "openai"},

		// Anthropic
		{"claude-opus-4-20250514", "anthropic"},
		{"claude-sonnet-4-20250514", "anthropic"},
		{"claude-3-5-haiku-20241022", "anthropic"},

		// DeepSeek
		{"deepseek-chat", "deepseek"},
		{"deepseek-reasoner", "deepseek"},

		// xAI
		{"grok-3", "xai"},
		{"grok-3-mini", "xai"},

		// Mistral
		{"mistral-large-latest", "mistral"},
		{"codestral-latest", "mistral"},
		{"pixtral-large-latest", "mistral"},

		// Perplexity
		{"sonar-pro", "perplexity"},
		{"sonar-reasoning", "perplexity"},

		// Cohere
		{"command-r-plus", "cohere"},
		{"command-a-03-2025", "cohere"},

		// Unknown
		{"ai/mistral", ""},
		{"some-random-model", ""},
		{"", ""},

		// Case insensitive
		{"GPT-4o", "openai"},
		{"CLAUDE-opus-4", "anthropic"},
		{"Gemini-2.5-flash", "gemini"},
	}

	for _, tt := range tests {
		t.Run(tt.modelTag, func(t *testing.T) {
			got := DetectProvider(tt.modelTag)
			if got != tt.want {
				t.Errorf("DetectProvider(%q) = %q, want %q", tt.modelTag, got, tt.want)
			}
		})
	}
}

func TestProviderEndpoint(t *testing.T) {
	t.Run("standard provider", func(t *testing.T) {
		ep := ProviderEndpoint("openai", "sk-test-key")
		if ep.BaseURL != "https://api.openai.com/v1" {
			t.Errorf("BaseURL = %q, want OpenAI URL", ep.BaseURL)
		}
		if ep.APIKey != "sk-test-key" {
			t.Errorf("APIKey = %q, want %q", ep.APIKey, "sk-test-key")
		}
		if ep.Provider != "openai" {
			t.Errorf("Provider = %q, want %q", ep.Provider, "openai")
		}
		if ep.Region != "" {
			t.Errorf("Region = %q, want empty", ep.Region)
		}
	})

	t.Run("bedrock provider uses region", func(t *testing.T) {
		ep := ProviderEndpoint("bedrock", "us-east-1")
		if ep.APIKey != "" {
			t.Errorf("APIKey = %q, want empty for bedrock", ep.APIKey)
		}
		if ep.Region != "us-east-1" {
			t.Errorf("Region = %q, want %q", ep.Region, "us-east-1")
		}
		if ep.Provider != "bedrock" {
			t.Errorf("Provider = %q, want %q", ep.Provider, "bedrock")
		}
	})

	t.Run("anthropic provider", func(t *testing.T) {
		ep := ProviderEndpoint("anthropic", "sk-ant-test")
		if ep.BaseURL != "https://api.anthropic.com/v1" {
			t.Errorf("BaseURL = %q, want Anthropic URL", ep.BaseURL)
		}
		if ep.APIKey != "sk-ant-test" {
			t.Errorf("APIKey = %q, want %q", ep.APIKey, "sk-ant-test")
		}
	})
}

func TestLocalEndpoint(t *testing.T) {
	ep := LocalEndpoint("/var/run/docker.sock")
	if ep.SocketPath != "/var/run/docker.sock" {
		t.Errorf("SocketPath = %q, want docker socket path", ep.SocketPath)
	}
	if ep.IsRemote() {
		t.Error("LocalEndpoint should not be remote")
	}
}

func TestLookupPricing(t *testing.T) {
	tests := []struct {
		provider    string
		modelID     string
		wantNonZero bool
	}{
		// Gemini
		{"gemini", "gemini-2.5-flash", true},
		{"gemini", "gemini-2.5-pro-preview", true},
		{"gemini", "gemini-2.0-flash", true},
		{"gemini", "gemini-unknown-model", false},

		// OpenAI
		{"openai", "gpt-4o", true},
		{"openai", "gpt-4o-mini", true},
		{"openai", "gpt-4.1", true},
		{"openai", "o3-mini", true},
		{"openai", "unknown-model", false},

		// Anthropic
		{"anthropic", "claude-opus-4-20250514", true},
		{"anthropic", "claude-sonnet-4-20250514", true},
		{"anthropic", "claude-3-5-haiku-20241022", true},
		{"anthropic", "unknown-model", false},

		// Bedrock
		{"bedrock", "anthropic.claude-3-5-sonnet", true},
		{"bedrock", "amazon.nova-pro-v1:0", true},
		{"bedrock", "unknown-model", false},

		// Other providers
		{"deepseek", "deepseek-chat", true},
		{"xai", "grok-3", true},
		{"perplexity", "sonar-pro", true},
		{"cohere", "command-a", true},
		{"groq", "llama-3.3-70b", true},
		{"mistral", "mistral-large", true},

		// Unknown provider
		{"unknown", "some-model", false},
	}

	for _, tt := range tests {
		t.Run(tt.provider+"/"+tt.modelID, func(t *testing.T) {
			p := LookupPricing(tt.provider, tt.modelID)
			if tt.wantNonZero && (p.PromptPrice == 0 || p.CompletionPrice == 0) {
				t.Errorf("LookupPricing(%q, %q) returned zero pricing, want non-zero", tt.provider, tt.modelID)
			}
			if !tt.wantNonZero && (p.PromptPrice != 0 || p.CompletionPrice != 0) {
				t.Errorf("LookupPricing(%q, %q) returned non-zero pricing, want zero", tt.provider, tt.modelID)
			}
		})
	}
}

func TestLookupOpenAIPricing_LongestPrefixMatch(t *testing.T) {
	// gpt-4o-mini should match "gpt-4o-mini" prefix, not "gpt-4o"
	mini := LookupOpenAIPricing("gpt-4o-mini-2024-07-18")
	regular := LookupOpenAIPricing("gpt-4o-2024-05-13")

	if mini.PromptPrice == 0 {
		t.Fatal("mini pricing should be non-zero")
	}
	if regular.PromptPrice == 0 {
		t.Fatal("regular pricing should be non-zero")
	}
	// gpt-4o-mini should be cheaper than gpt-4o
	if mini.PromptPrice >= regular.PromptPrice {
		t.Errorf("gpt-4o-mini prompt price (%v) should be less than gpt-4o (%v)",
			mini.PromptPrice, regular.PromptPrice)
	}
}

func TestFormatContextLen(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{8000, "8k ctx"},
		{32000, "32k ctx"},
		{128000, "128k ctx"},
		{200000, "200k ctx"},
		{1000000, "1M ctx"},
		{2000000, "2M ctx"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatContextLen(tt.input)
			if got != tt.want {
				t.Errorf("formatContextLen(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeModelID(t *testing.T) {
	tests := []struct {
		provider string
		id       string
		want     string
	}{
		{"gemini", "models/gemini-2.5-flash", "gemini-2.5-flash"},
		{"gemini", "gemini-2.5-flash", "gemini-2.5-flash"},
		{"openai", "gpt-4o", "gpt-4o"},
		{"openrouter", "meta/llama-3", "meta/llama-3"},
	}

	for _, tt := range tests {
		t.Run(tt.provider+"/"+tt.id, func(t *testing.T) {
			got := normalizeModelID(tt.provider, tt.id)
			if got != tt.want {
				t.Errorf("normalizeModelID(%q, %q) = %q, want %q", tt.provider, tt.id, got, tt.want)
			}
		})
	}
}

func TestFilterProviderModel(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		id       string
		entry    providerModelEntry
		want     bool
	}{
		// Gemini filters
		{"gemini chat model", "gemini", "gemini-2.5-flash", providerModelEntry{}, true},
		{"gemini imagen", "gemini", "imagen-3.0", providerModelEntry{}, false},
		{"gemini embedding", "gemini", "gemini-embedding-001", providerModelEntry{}, false},
		{"gemini tts", "gemini", "gemini-2.5-flash-tts", providerModelEntry{}, false},

		// OpenAI filters
		{"openai chat", "openai", "gpt-4o", providerModelEntry{}, true},
		{"openai embedding", "openai", "text-embedding-ada-002", providerModelEntry{}, false},
		{"openai tts", "openai", "tts-1", providerModelEntry{}, false},
		{"openai dalle", "openai", "dall-e-3", providerModelEntry{}, false},
		{"openai whisper", "openai", "whisper-1", providerModelEntry{}, false},

		// OpenRouter filters
		{"openrouter text", "openrouter", "meta/llama-3",
			providerModelEntry{Architecture: struct {
				Modality string `json:"modality"`
			}{Modality: "text->text"}}, true},
		{"openrouter free", "openrouter", "meta/llama-3:free",
			providerModelEntry{Architecture: struct {
				Modality string `json:"modality"`
			}{Modality: "text->text"}}, false},
		{"openrouter image gen", "openrouter", "openai/dall-e-3",
			providerModelEntry{Architecture: struct {
				Modality string `json:"modality"`
			}{Modality: "text->image"}}, false},

		// DeepSeek filters
		{"deepseek chat", "deepseek", "deepseek-chat", providerModelEntry{}, true},
		{"deepseek reasoner", "deepseek", "deepseek-reasoner", providerModelEntry{}, true},
		{"deepseek other", "deepseek", "deepseek-coder", providerModelEntry{}, false},

		// Mistral filters
		{"mistral chat", "mistral", "mistral-large-latest", providerModelEntry{}, true},
		{"mistral embed", "mistral", "mistral-embed", providerModelEntry{}, false},

		// Unknown provider passes everything
		{"unknown provider", "unknown", "any-model", providerModelEntry{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterProviderModel(tt.provider, tt.id, tt.entry)
			if got != tt.want {
				t.Errorf("filterProviderModel(%q, %q) = %v, want %v", tt.provider, tt.id, got, tt.want)
			}
		})
	}
}

func TestSliceContains(t *testing.T) {
	tests := []struct {
		slice []string
		s     string
		want  bool
	}{
		{[]string{"text", "image"}, "text", true},
		{[]string{"text", "image"}, "audio", false},
		{[]string{}, "text", false},
		{nil, "text", false},
	}

	for _, tt := range tests {
		got := sliceContains(tt.slice, tt.s)
		if got != tt.want {
			t.Errorf("sliceContains(%v, %q) = %v, want %v", tt.slice, tt.s, got, tt.want)
		}
	}
}

func TestListAnthropicModels(t *testing.T) {
	models := listAnthropicModels()
	if len(models) == 0 {
		t.Fatal("listAnthropicModels returned no models")
	}

	for _, m := range models {
		if m.Provider != "anthropic" {
			t.Errorf("model %q provider = %q, want %q", m.Name, m.Provider, "anthropic")
		}
		if m.Name == "" {
			t.Error("model has empty name")
		}
		if m.PromptPrice == 0 {
			t.Errorf("model %q has zero prompt price", m.Name)
		}
		if m.Params == "" {
			t.Errorf("model %q has empty params", m.Name)
		}
	}
}

func TestListOllamaCloudModels(t *testing.T) {
	models := listOllamaCloudModels()
	if len(models) == 0 {
		t.Fatal("listOllamaCloudModels returned no models")
	}

	for _, m := range models {
		if m.Provider != "ollama" {
			t.Errorf("model %q provider = %q, want %q", m.Name, m.Provider, "ollama")
		}
		if m.Name == "" {
			t.Error("model has empty name")
		}
	}
}

func TestProvidersMap(t *testing.T) {
	// Verify all expected providers are present
	expected := []string{
		"gemini", "openrouter", "openai", "anthropic", "groq",
		"mistral", "together", "fireworks", "deepseek", "xai",
		"cerebras", "perplexity", "sambanova", "cohere",
		"huggingface", "github", "ollama",
	}
	for _, p := range expected {
		if _, ok := Providers[p]; !ok {
			t.Errorf("missing provider %q in Providers map", p)
		}
	}

	// All URLs should start with https://
	for name, url := range Providers {
		if url == "" {
			t.Errorf("provider %q has empty URL", name)
		}
		if len(url) < 8 || url[:8] != "https://" {
			t.Errorf("provider %q URL %q does not start with https://", name, url)
		}
	}
}
