// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package mcp

import "encoding/json"

// TrustReadOnly marks every tool on a server as read-only, so its calls are not
// gated. Use it for servers that ship no annotations but only read.
const TrustReadOnly = "read-only"

// ServerConfig describes an MCP server to connect to.
type ServerConfig struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Env     []string `yaml:"env"`
	Trust   string   `yaml:"trust"` // "read-only", or empty for gated
}

// --- JSON-RPC 2.0 types ---

// JSONRPCRequest is a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCNotification is a JSON-RPC 2.0 notification (no id field).
type JSONRPCNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError is a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// --- MCP protocol types ---

// InitializeParams is sent by the client to initialize the MCP session.
type InitializeParams struct {
	ProtocolVersion string     `json:"protocolVersion"`
	ClientInfo      ClientInfo `json:"clientInfo"`
	Capabilities    struct{}   `json:"capabilities"`
}

// ClientInfo identifies the MCP client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the server's response to initialize.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
	Capabilities    ServerCapabilities `json:"capabilities"`
}

// ServerInfo describes the MCP server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServerCapabilities describes what the MCP server supports.
type ServerCapabilities struct {
	Tools *struct{} `json:"tools,omitempty"`
}

// ToolAnnotations carries the optional behavioural hints from the MCP spec.
// ReadOnlyHint is a pointer so an absent hint is distinguishable from false:
// absent means unknown, and unknown is gated.
type ToolAnnotations struct {
	ReadOnlyHint *bool `json:"readOnlyHint,omitempty"`
}

// MCPToolDef describes a tool exposed by an MCP server.
type MCPToolDef struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	InputSchema json.RawMessage  `json:"inputSchema"`
	Annotations *ToolAnnotations `json:"annotations,omitempty"`
}

// ToolListResult is the response from tools/list.
type ToolListResult struct {
	Tools []MCPToolDef `json:"tools"`
}

// CallToolParams is sent to invoke a tool on the server.
type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// CallToolResult is the server's response to tools/call.
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock is a typed content element in MCP responses.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}
