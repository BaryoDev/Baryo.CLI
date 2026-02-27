// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// startupTimeout is the maximum time to wait for an MCP server to initialize.
const startupTimeout = 15 * time.Second

// callTimeout is the default timeout for individual RPC calls.
const callTimeout = 30 * time.Second

// Client is a JSON-RPC client that communicates with an MCP server over stdio.
type Client struct {
	name    string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	tools   []MCPToolDef
	nextID  atomic.Int64
	pending sync.Map // map[int]chan JSONRPCResponse
	mu      sync.Mutex
	closed  bool
}

// NewClient starts an MCP server process and performs the initialization handshake.
func NewClient(ctx context.Context, name, command string, args, env []string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
	cmd.Stderr = io.Discard // prevent MCP server stderr from leaking to terminal

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command, err)
	}

	c := &Client{
		name:   name,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}

	go c.readLoop()

	// Use a timeout context for the entire startup handshake.
	startCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	// Initialize handshake
	initParams := InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo: ClientInfo{
			Name:    "baryo",
			Version: "1.0.0",
		},
	}
	result, err := c.call(startCtx, "initialize", initParams)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("initialize: %w", err)
	}

	var initResult InitializeResult
	if err := json.Unmarshal(result, &initResult); err != nil {
		c.Close()
		return nil, fmt.Errorf("parse initialize result: %w", err)
	}

	// Send initialized notification (no id field per JSON-RPC spec)
	c.notify("notifications/initialized", nil)

	// Fetch tool list
	toolsResult, err := c.call(startCtx, "tools/list", struct{}{})
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("tools/list: %w", err)
	}

	var toolList ToolListResult
	if err := json.Unmarshal(toolsResult, &toolList); err != nil {
		c.Close()
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}
	c.tools = toolList.Tools

	return c, nil
}

// Tools returns the tools available on this server.
func (c *Client) Tools() []MCPToolDef {
	return c.tools
}

// CallTool executes a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]interface{}) (string, bool, error) {
	params := CallToolParams{
		Name:      name,
		Arguments: args,
	}

	// Apply a timeout if the parent context doesn't have one.
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	result, err := c.call(callCtx, "tools/call", params)
	if err != nil {
		return "", true, err
	}

	var toolResult CallToolResult
	if err := json.Unmarshal(result, &toolResult); err != nil {
		return "", true, fmt.Errorf("parse tools/call result: %w", err)
	}

	// Concatenate text content blocks
	var sb strings.Builder
	for _, block := range toolResult.Content {
		if block.Type == "text" {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(block.Text)
		}
	}

	return sb.String(), toolResult.IsError, nil
}

// Close kills the child process.
func (c *Client) Close() {
	if c.closed {
		return
	}
	c.closed = true
	c.stdin.Close()
	if c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	c.cmd.Wait()
}

// readLoop reads JSON-RPC responses from stdout and dispatches to pending requests.
func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	// MCP messages can be large (tool results), increase buffer
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var resp JSONRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}

		if ch, ok := c.pending.LoadAndDelete(resp.ID); ok {
			ch.(chan JSONRPCResponse) <- resp
		}
	}
}

// call sends a JSON-RPC request and waits for the response.
func (c *Client) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := int(c.nextID.Add(1))
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	respCh := make(chan JSONRPCResponse, 1)
	c.pending.Store(id, respCh)
	defer c.pending.Delete(id)

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	_, err = c.stdin.Write(append(data, '\n'))
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// notify sends a JSON-RPC notification (no id, no response expected).
func (c *Client) notify(method string, params interface{}) {
	notif := JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	data, _ := json.Marshal(notif)
	c.mu.Lock()
	c.stdin.Write(append(data, '\n'))
	c.mu.Unlock()
}
