// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// Manager manages multiple MCP server connections.
type Manager struct {
	clients map[string]*Client            // server name → client
	toolMap map[string]string             // qualified tool name → server name
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewManager creates a new MCP manager.
func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		clients: make(map[string]*Client),
		toolMap: make(map[string]string),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// connectResult holds the outcome of connecting to one MCP server.
type connectResult struct {
	name   string
	client *Client
	err    error
}

// Start connects to all configured MCP servers concurrently. Failed connections
// are non-fatal — the manager continues without them and returns errors for logging.
func (m *Manager) Start(ctx context.Context, configs []ServerConfig) []error {
	results := make([]connectResult, len(configs))
	var wg sync.WaitGroup

	for i, cfg := range configs {
		wg.Add(1)
		go func(idx int, cfg ServerConfig) {
			defer wg.Done()
			client, err := NewClient(m.ctx, cfg.Name, cfg.Command, cfg.Args, cfg.Env)
			results[idx] = connectResult{name: cfg.Name, client: client, err: err}
		}(i, cfg)
	}
	wg.Wait()

	var errs []error
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Errorf("mcp server %q: %w", r.name, r.err))
			continue
		}
		m.clients[r.name] = r.client
		for _, tool := range r.client.Tools() {
			qualified := "mcp__" + r.name + "__" + tool.Name
			m.toolMap[qualified] = r.name
		}
	}
	return errs
}

// ToolDefinitions returns llm.ToolDefinition entries for all MCP tools
// across all connected servers.
func (m *Manager) ToolDefinitions() []llm.ToolDefinition {
	var defs []llm.ToolDefinition
	for serverName, client := range m.clients {
		for _, tool := range client.Tools() {
			qualified := "mcp__" + serverName + "__" + tool.Name

			// Parse InputSchema as parameters
			var params interface{}
			if len(tool.InputSchema) > 0 {
				json.Unmarshal(tool.InputSchema, &params)
			}
			if params == nil {
				params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			}
			params = sanitizeSchema(params)

			defs = append(defs, llm.ToolDefinition{
				Type: "function",
				Function: llm.FunctionDefinition{
					Name:        qualified,
					Description: fmt.Sprintf("[MCP:%s] %s", serverName, tool.Description),
					Parameters:  params,
				},
			})
		}
	}
	return defs
}

// redundantServers lists MCP server names whose tools are largely covered
// by native baryo tools. Their tools are still routable via Execute() when
// explicitly requested, but are excluded from the tool list sent to the model
// to avoid overwhelming small models with too many definitions.
var redundantServers = map[string]bool{
	"filesystem": true, // native: read_file, write_file, edit_file, list_directory, grep, glob
	"git":        true, // native: git_status, git_diff, git_log, gh + /commit /diff /undo
	"memory":     true, // knowledge graph — useful but 8 tools; rarely needed for most prompts
}

// contextThreshold is the context window size above which all MCP tools are
// included (large models can handle the extra definitions).
const contextThreshold = 16384

// CompactToolDefinitions returns MCP tool definitions, dynamically adjusted
// based on the model's context window size:
//   - Large context (>16K): all MCP tools included (only exact name overlaps skipped)
//   - Small context (≤16K): redundant servers skipped, descriptions trimmed
//
// The full set is always routable via Execute when users explicitly request MCP tools.
func (m *Manager) CompactToolDefinitions(nativeNames []string, contextWindow int) []llm.ToolDefinition {
	native := make(map[string]bool, len(nativeNames))
	for _, n := range nativeNames {
		native[n] = true
	}

	largeContext := contextWindow > contextThreshold

	var defs []llm.ToolDefinition
	for serverName, client := range m.clients {
		// Small models: skip entire servers whose domain is covered by native tools.
		if !largeContext && redundantServers[serverName] {
			continue
		}

		for _, tool := range client.Tools() {
			// Always skip exact name overlaps regardless of model size.
			if native[tool.Name] {
				continue
			}

			qualified := "mcp__" + serverName + "__" + tool.Name

			desc := tool.Description
			// Small models: trim description to first sentence to save tokens.
			if !largeContext {
				if idx := strings.Index(desc, ". "); idx > 0 && idx < 120 {
					desc = desc[:idx+1]
				} else if len(desc) > 120 {
					desc = desc[:120]
				}
			}

			var params interface{}
			if len(tool.InputSchema) > 0 {
				json.Unmarshal(tool.InputSchema, &params)
			}
			if params == nil {
				params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			}
			params = sanitizeSchema(params)
			// Small models: strip verbose schema fields.
			if !largeContext {
				params = trimSchema(params)
			}

			defs = append(defs, llm.ToolDefinition{
				Type: "function",
				Function: llm.FunctionDefinition{
					Name:        qualified,
					Description: fmt.Sprintf("[MCP:%s] %s", serverName, desc),
					Parameters:  params,
				},
			})
		}
	}
	return defs
}

// sanitizeSchema ensures all properties in a JSON schema have a "type" field.
// Some MCP servers emit schemas with properties missing "type" (e.g. GitHub's
// create_pull_request has "base_branch" without one). Strict providers like
// Cohere reject these with a 400 error.
func sanitizeSchema(v interface{}) interface{} {
	m, ok := v.(map[string]interface{})
	if !ok {
		return v
	}
	if props, ok := m["properties"].(map[string]interface{}); ok {
		for k, pv := range props {
			if pm, ok := pv.(map[string]interface{}); ok {
				if _, hasType := pm["type"]; !hasType {
					pm["type"] = "string"
				}
				props[k] = sanitizeSchema(pm)
			}
		}
	}
	if items, ok := m["items"].(map[string]interface{}); ok {
		m["items"] = sanitizeSchema(items)
	}
	return m
}

// trimSchema removes verbose fields ($schema, additionalProperties, etc.)
// from JSON schema objects to reduce token usage.
func trimSchema(v interface{}) interface{} {
	m, ok := v.(map[string]interface{})
	if !ok {
		return v
	}
	delete(m, "$schema")
	delete(m, "additionalProperties")
	// Recurse into properties
	if props, ok := m["properties"].(map[string]interface{}); ok {
		for k, pv := range props {
			props[k] = trimSchema(pv)
		}
	}
	// Recurse into items
	if items, ok := m["items"].(map[string]interface{}); ok {
		m["items"] = trimSchema(items)
	}
	return m
}

// Execute routes a qualified tool call to the correct MCP server.
func (m *Manager) Execute(ctx context.Context, qualifiedName, argsJSON string) (string, bool) {
	serverName, ok := m.toolMap[qualifiedName]
	if !ok {
		return fmt.Sprintf("unknown MCP tool: %s", qualifiedName), true
	}
	client, ok := m.clients[serverName]
	if !ok {
		return fmt.Sprintf("MCP server %q not connected", serverName), true
	}

	// Strip the mcp__<server>__ prefix to get the original tool name
	prefix := "mcp__" + serverName + "__"
	toolName := strings.TrimPrefix(qualifiedName, prefix)

	// Parse args JSON
	var args map[string]interface{}
	if argsJSON != "" && argsJSON != "{}" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf("invalid arguments: %v", err), true
		}
	}
	if args == nil {
		args = make(map[string]interface{})
	}

	content, isError, err := client.CallTool(ctx, toolName, args)
	if err != nil {
		return fmt.Sprintf("MCP tool error: %v", err), true
	}
	return content, isError
}

// IsMCPTool returns true if the given name is a registered MCP tool.
func (m *Manager) IsMCPTool(name string) bool {
	_, ok := m.toolMap[name]
	return ok
}

// ServerNames returns the names of all connected servers in sorted order.
func (m *Manager) ServerNames() []string {
	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ServerTools returns the tool names for a specific server.
func (m *Manager) ServerTools(name string) []string {
	client, ok := m.clients[name]
	if !ok {
		return nil
	}
	var names []string
	for _, tool := range client.Tools() {
		names = append(names, tool.Name)
	}
	return names
}

// Close stops all MCP server processes.
func (m *Manager) Close() {
	m.cancel()
	for _, client := range m.clients {
		client.Close()
	}
}
