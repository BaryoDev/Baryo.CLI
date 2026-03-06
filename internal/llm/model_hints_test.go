package llm

import (
	"testing"
)

func TestDetectModelHints(t *testing.T) {
	tests := []struct {
		modelTag      string
		wantFamily    string
		wantThink     bool
		wantCtxWindow int
	}{
		// Qwen
		{"ai/qwen3:latest", "qwen", true, 32768},
		{"docker.io/ai/qwen2:latest", "qwen", true, 32768},

		// Phi
		{"ai/phi-4-mini:latest", "phi", false, 8192},

		// Llama
		{"ai/llama3.2:latest", "llama", false, 32768},

		// Mistral
		{"ai/mistral:latest", "mistral", false, 128000},
		{"codestral-latest", "mistral", false, 256000},

		// Gemma
		{"ai/gemma2:latest", "gemma", false, 8192},

		// Gemini
		{"gemini-2.5-flash", "gemini", false, 131072},

		// OpenAI
		{"gpt-4o", "openai", false, 128000},
		{"o3-mini", "openai", false, 128000},
		{"chatgpt-4o-latest", "openai", false, 128000},

		// Anthropic
		{"claude-opus-4-20250514", "anthropic", false, 200000},
		{"claude-3-5-sonnet-20241022", "anthropic", false, 200000},

		// Bedrock Anthropic
		{"anthropic.claude-3-5-sonnet-20241022-v2:0", "bedrock", false, 200000},

		// Bedrock Amazon
		{"amazon.nova-pro-v1:0", "bedrock", false, 128000},
		{"amazon.titan-text-lite-v1", "bedrock", false, 128000},

		// Bedrock Meta (contains "llama" so matches llama family, not bedrock)
		{"meta.llama3-1-70b-instruct-v1:0", "llama", false, 32768},

		// DeepSeek
		{"deepseek-chat", "deepseek", false, 64000},

		// xAI
		{"grok-3-mini", "xai", false, 131072},

		// Perplexity
		{"sonar-pro", "perplexity", false, 128000},

		// Cohere
		{"command-r-plus", "cohere", false, 128000},

		// Unknown
		{"some-random-model", "unknown", false, 8192},
		{"", "unknown", false, 8192},
	}

	for _, tt := range tests {
		t.Run(tt.modelTag, func(t *testing.T) {
			hints := DetectModelHints(tt.modelTag)
			if hints.Family != tt.wantFamily {
				t.Errorf("Family = %q, want %q", hints.Family, tt.wantFamily)
			}
			if hints.DisableThink != tt.wantThink {
				t.Errorf("DisableThink = %v, want %v", hints.DisableThink, tt.wantThink)
			}
			if hints.ContextWindow != tt.wantCtxWindow {
				t.Errorf("ContextWindow = %d, want %d", hints.ContextWindow, tt.wantCtxWindow)
			}
		})
	}
}

func TestDetectModelHints_Temperature(t *testing.T) {
	// Models that should have temperature set
	modelsWithTemp := []string{"ai/qwen3:latest", "ai/phi-4-mini:latest", "gpt-4o", "claude-opus-4"}
	for _, tag := range modelsWithTemp {
		hints := DetectModelHints(tag)
		if hints.Temperature == nil {
			t.Errorf("DetectModelHints(%q).Temperature is nil, want non-nil", tag)
		}
	}

	// Unknown models should have no temperature set
	hints := DetectModelHints("some-random-model")
	if hints.Temperature != nil {
		t.Errorf("unknown model should have nil Temperature, got %v", *hints.Temperature)
	}
}

func TestDetectModelHints_StopTokens(t *testing.T) {
	hints := DetectModelHints("ai/phi-4-mini:latest")
	if len(hints.StopTokens) == 0 {
		t.Error("phi should have stop tokens")
	}

	hints = DetectModelHints("gpt-4o")
	if len(hints.StopTokens) != 0 {
		t.Error("OpenAI models should not have stop tokens")
	}
}

func TestDetectModelHints_TopK(t *testing.T) {
	hints := DetectModelHints("ai/qwen3:latest")
	if hints.TopK == nil {
		t.Fatal("qwen should have TopK set")
	}
	if *hints.TopK != 20 {
		t.Errorf("qwen TopK = %d, want 20", *hints.TopK)
	}

	hints = DetectModelHints("gpt-4o")
	if hints.TopK != nil {
		t.Error("OpenAI models should not have TopK set")
	}
}
