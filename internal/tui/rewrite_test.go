package tui

import (
	"strings"
	"testing"

	"github.com/arnelirobles/baryo-cli/internal/llm"
)

var remote = llm.Endpoint{Provider: "anthropic", BaseURL: "https://api.anthropic.com", APIKey: "k"}
var local = llm.LocalEndpoint("/var/run/docker.sock")

func TestShouldRewriteOnRemoteEndpoint(t *testing.T) {
	if !shouldRewrite(true, remote, true, false, true, "fix the test", ToolsDynamic) {
		t.Error("a short tool-oriented message on a remote endpoint should be rewritten")
	}
}

func TestShouldRewriteRespectsExistingConditions(t *testing.T) {
	cases := []struct {
		name          string
		enabled       bool
		hasTools      bool
		hasSkill      bool
		supportsTools bool
		text          string
		strategy      ToolStrategy
	}{
		{name: "disabled", enabled: false, hasTools: true, supportsTools: true, text: "fix it", strategy: ToolsDynamic},
		{name: "no tools this turn", enabled: true, hasTools: false, supportsTools: true, text: "fix it", strategy: ToolsDynamic},
		{name: "skill active", enabled: true, hasTools: true, hasSkill: true, supportsTools: true, text: "fix it", strategy: ToolsDynamic},
		{name: "model cannot call tools", enabled: true, hasTools: true, supportsTools: false, text: "fix it", strategy: ToolsDynamic},
		{name: "message too long", enabled: true, hasTools: true, supportsTools: true, text: strings.Repeat("x", rewriteMaxInputLen+1), strategy: ToolsDynamic},
		{name: "non-dynamic mode", enabled: true, hasTools: true, supportsTools: true, text: "fix it", strategy: ToolsAll},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if shouldRewrite(c.enabled, remote, c.hasTools, c.hasSkill, c.supportsTools, c.text, c.strategy) {
				t.Errorf("expected no rewrite when %s", c.name)
			}
		})
	}
}

// The rewrite pass costs a whole extra request before the real one. On a local
// endpoint the user waits through that prefill, it regularly exceeds its own 15s
// timeout on CPU and is discarded, and on a single-slot server it evicts the
// conversation's KV cache so the real request then starts cold.
func TestShouldNotRewriteOnLocalEndpoint(t *testing.T) {
	if shouldRewrite(true, local, true, false, true, "fix the test", ToolsDynamic) {
		t.Error("the rewrite pass should not run against a local endpoint")
	}
}
