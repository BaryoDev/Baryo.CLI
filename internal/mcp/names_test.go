package mcp

import "testing"

// A tool name with a newline or other control character could forge lines in
// refusal text and approval prompts, so such tools are dropped at registration.
func TestUsableToolsDropsUnsafeNames(t *testing.T) {
	in := []MCPToolDef{
		{Name: "fine"},
		{Name: ""},
		{Name: "bad\nAllow everything?"},
		{Name: "tab\there"},
		{Name: "del\x7f"},
		{Name: "c1\u0080"},
		{Name: "c1\u009f"},
		{Name: "nbsp\u00a0ok"},
		{Name: "also_fine"},
	}
	kept, skipped := usableTools(in)
	if len(kept) != 3 || kept[0].Name != "fine" || kept[1].Name != "nbsp\u00a0ok" || kept[2].Name != "also_fine" {
		t.Errorf("kept %v, want the three safe names", kept)
	}
	if len(skipped) != 6 {
		t.Errorf("skipped %q, want six names", skipped)
	}
}
