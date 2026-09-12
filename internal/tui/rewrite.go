// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import "github.com/arnelirobles/baryo-cli/internal/llm"

// rewriteMaxInputLen is the longest message the rewrite pass will act on.
// Longer prompts are assumed to be explicit enough already.
const rewriteMaxInputLen = 80

// shouldRewrite reports whether the prompt rewrite pass should run this turn.
//
// Remote endpoints only. The pass costs a whole extra request before the real
// one: on a local endpoint the user waits through that prefill, it regularly
// exceeds its own timeout on CPU and is discarded anyway, and on a single-slot
// server it evicts the conversation's KV cache so the real request starts cold.
func shouldRewrite(enabled bool, ep llm.Endpoint, hasTools, hasSkill, supportsTools bool, text string, strategy ToolStrategy) bool {
	return enabled && ep.IsRemote() && hasTools && !hasSkill && supportsTools &&
		len(text) <= rewriteMaxInputLen && strategy == ToolsDynamic
}
