// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// Result is the outcome of a tool execution.
type Result struct {
	Content string
	IsError bool
}

// Definition describes a tool available to the model.
// Fields match the OpenAI function calling JSON schema.
type Definition struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

// FunctionDef describes a function the model can call.
type FunctionDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// Tool is a registered tool with its schema and executor.
type Tool struct {
	Def         Definition
	Execute     func(ctx context.Context, argsJSON string) Result
	Destructive bool // true for tools that modify the filesystem or run code
}

var registry = map[string]Tool{}

// Register adds a tool to the global registry.
func Register(name string, tool Tool) {
	registry[name] = tool
}

// Execute runs a tool by name with the given JSON arguments.
func Execute(ctx context.Context, name, argsJSON string) Result {
	tool, ok := registry[name]
	if !ok {
		return Result{Content: fmt.Sprintf("unknown tool: %s", name), IsError: true}
	}
	// Some models (e.g. Gemini) concatenate multiple JSON objects in one call
	// like {"path":"a"}{"path":"b"}. Extract only the first valid object.
	argsJSON = sanitizeArgs(argsJSON)
	return tool.Execute(ctx, argsJSON)
}

// sanitizeArgs extracts the first valid JSON object from args that may contain
// concatenated objects (e.g. {"path":"a"}{"path":"b"} → {"path":"a"}).
func sanitizeArgs(argsJSON string) string {
	dec := json.NewDecoder(strings.NewReader(argsJSON))
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return argsJSON // let downstream handle the error
	}
	// If there's more content after the first object, return only the first.
	if dec.More() {
		return string(raw)
	}
	return argsJSON
}

// IsDestructive returns true if the named tool is marked as destructive.
func IsDestructive(name string) bool {
	tool, ok := registry[name]
	if !ok {
		return false
	}
	return tool.Destructive
}

// AllDefinitions returns the tool definitions for all registered tools.
func AllDefinitions() []Definition {
	defs := make([]Definition, 0, len(registry))
	for _, t := range registry {
		defs = append(defs, t.Def)
	}
	return defs
}

// DockerDefinitions converts all registered tool definitions to llm.ToolDefinition format.
func DockerDefinitions() []llm.ToolDefinition {
	defs := AllDefinitions()
	out := make([]llm.ToolDefinition, len(defs))
	for i, d := range defs {
		out[i] = llm.ToolDefinition{
			Type: d.Type,
			Function: llm.FunctionDefinition{
				Name:        d.Function.Name,
				Description: d.Function.Description,
				Parameters:  d.Function.Parameters,
			},
		}
	}
	return out
}

// scriptToolNames are the tools needed for skill script execution.
var scriptToolNames = []string{"run_code", "run_script"}

// Names returns the names of all registered tools.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

// ReadOnlyDefinitions returns tool definitions for non-destructive tools only.
// Used in plan mode to restrict the model to read-only exploration.
func ReadOnlyDefinitions() []Definition {
	var defs []Definition
	for _, t := range registry {
		if !t.Destructive {
			defs = append(defs, t.Def)
		}
	}
	return defs
}

// ReadOnlyDockerDefinitions converts non-destructive tool definitions to llm.ToolDefinition format.
func ReadOnlyDockerDefinitions() []llm.ToolDefinition {
	defs := ReadOnlyDefinitions()
	out := make([]llm.ToolDefinition, len(defs))
	for i, d := range defs {
		out[i] = llm.ToolDefinition{
			Type: d.Type,
			Function: llm.FunctionDefinition{
				Name:        d.Function.Name,
				Description: d.Function.Description,
				Parameters:  d.Function.Parameters,
			},
		}
	}
	return out
}

// ScriptDefinitions returns only the run_code and run_script tool definitions.
// Used when a skill is active to reduce tool count for smaller models.
func ScriptDefinitions() []Definition {
	var defs []Definition
	for _, name := range scriptToolNames {
		if t, ok := registry[name]; ok {
			defs = append(defs, t.Def)
		}
	}
	return defs
}
