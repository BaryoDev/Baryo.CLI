// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import "strings"

// ModelHints contains model-family-specific parameter recommendations.
type ModelHints struct {
	Family       string   // "qwen", "llama", "mistral", "phi", "gemma", "unknown"
	DisableThink bool     // append /no_think to system prompt (qwen3)
	Temperature  *float64 // recommended default if user hasn't set one
	TopK         *int     // recommended top_k
	StopTokens   []string // extra stop sequences
}

// DetectModelHints inspects the model tag string and returns optimized defaults
// for the detected model family. Falls back to safe defaults for unknown models.
func DetectModelHints(modelTag string) ModelHints {
	lower := strings.ToLower(modelTag)

	switch {
	case strings.Contains(lower, "qwen"):
		temp := 0.7
		topK := 20
		return ModelHints{
			Family:       "qwen",
			DisableThink: true,
			Temperature:  &temp,
			TopK:         &topK,
		}

	case strings.Contains(lower, "phi"):
		temp := 0.7
		return ModelHints{
			Family:      "phi",
			Temperature: &temp,
			StopTokens:  []string{"<|end|>", "<|endoftext|>"},
		}

	case strings.Contains(lower, "llama"):
		temp := 0.7
		return ModelHints{
			Family:      "llama",
			Temperature: &temp,
		}

	case strings.Contains(lower, "mistral"):
		temp := 0.7
		return ModelHints{
			Family:      "mistral",
			Temperature: &temp,
		}

	case strings.Contains(lower, "gemma"):
		temp := 0.7
		return ModelHints{
			Family:      "gemma",
			Temperature: &temp,
		}

	case strings.HasPrefix(lower, "gemini"):
		temp := 1.0
		return ModelHints{
			Family:      "gemini",
			Temperature: &temp,
		}

	default:
		return ModelHints{
			Family: "unknown",
		}
	}
}
