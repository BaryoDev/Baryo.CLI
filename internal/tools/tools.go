// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"context"
	"fmt"

	"github.com/arnelirobles/baryo-cli/internal/docker"
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
	return tool.Execute(ctx, argsJSON)
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

// DockerDefinitions converts all registered tool definitions to docker.ToolDefinition format.
func DockerDefinitions() []docker.ToolDefinition {
	defs := AllDefinitions()
	out := make([]docker.ToolDefinition, len(defs))
	for i, d := range defs {
		out[i] = docker.ToolDefinition{
			Type: d.Type,
			Function: docker.FunctionDefinition{
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
