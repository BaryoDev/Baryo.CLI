// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// streamChatBedrock sends a chat request to the AWS Bedrock ConverseStream API
// and streams events. Same return signature as streamChatRaw.
func streamChatBedrock(ctx context.Context, ep Endpoint, model string, messages []ChatMessage, params ChatParams, tools []ToolDefinition) (<-chan StreamEvent, <-chan streamResult) {
	ch := make(chan StreamEvent, 64)
	resCh := make(chan streamResult, 1)

	go func() {
		defer close(ch)
		defer close(resCh)

		region := ep.APIKey // region is stored in the APIKey field for Bedrock
		cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
		if err != nil {
			ch <- StreamEvent{Error: fmt.Sprintf("failed to load AWS config: %v", err)}
			return
		}

		client := bedrockruntime.NewFromConfig(cfg)

		system, brMsgs := convertToBedrockMessages(messages)

		input := &bedrockruntime.ConverseStreamInput{
			ModelId:  &model,
			Messages: brMsgs,
		}

		if system != "" {
			input.System = []types.SystemContentBlock{
				&types.SystemContentBlockMemberText{Value: system},
			}
		}

		// Inference configuration.
		var infCfg types.InferenceConfiguration
		hasInfCfg := false
		if params.MaxTokens != nil {
			v := int32(*params.MaxTokens)
			infCfg.MaxTokens = &v
			hasInfCfg = true
		}
		if params.Temperature != nil {
			v := float32(*params.Temperature)
			infCfg.Temperature = &v
			hasInfCfg = true
		}
		if params.TopP != nil {
			v := float32(*params.TopP)
			infCfg.TopP = &v
			hasInfCfg = true
		}
		if len(params.Stop) > 0 {
			infCfg.StopSequences = params.Stop
			hasInfCfg = true
		}
		if hasInfCfg {
			input.InferenceConfig = &infCfg
		}

		if len(tools) > 0 {
			input.ToolConfig = convertToBedrockTools(tools)
		}

		output, err := client.ConverseStream(ctx, input)
		if err != nil {
			ch <- StreamEvent{Error: fmt.Sprintf("bedrock ConverseStream: %v", err)}
			return
		}

		stream := output.GetStream()
		defer stream.Close()

		// Track tool calls by content block index.
		type toolCallAcc struct {
			id       string
			funcName string
			args     strings.Builder
		}
		toolCalls := make(map[int32]*toolCallAcc)

		var inputTokens, outputTokens int
		var finishReason string

		for evt := range stream.Events() {
			switch e := evt.(type) {
			case *types.ConverseStreamOutputMemberContentBlockStart:
				if start, ok := e.Value.Start.(*types.ContentBlockStartMemberToolUse); ok {
					idx := *e.Value.ContentBlockIndex
					toolCalls[idx] = &toolCallAcc{
						id:       derefStr(start.Value.ToolUseId),
						funcName: derefStr(start.Value.Name),
					}
				}

			case *types.ConverseStreamOutputMemberContentBlockDelta:
				switch delta := e.Value.Delta.(type) {
				case *types.ContentBlockDeltaMemberText:
					select {
					case ch <- StreamEvent{Token: delta.Value}:
					case <-ctx.Done():
						return
					}
				case *types.ContentBlockDeltaMemberToolUse:
					idx := *e.Value.ContentBlockIndex
					if tc, ok := toolCalls[idx]; ok && delta.Value.Input != nil {
						tc.args.WriteString(*delta.Value.Input)
					}
				}

			case *types.ConverseStreamOutputMemberMessageStop:
				finishReason = string(e.Value.StopReason)

			case *types.ConverseStreamOutputMemberMetadata:
				if e.Value.Usage != nil {
					inputTokens = int(*e.Value.Usage.InputTokens)
					outputTokens = int(*e.Value.Usage.OutputTokens)
				}
			}
		}

		if err := stream.Err(); err != nil {
			ch <- StreamEvent{Error: fmt.Sprintf("bedrock stream error: %v", err)}
			return
		}

		// Build completed tool calls.
		var calls []ToolCall
		for _, acc := range toolCalls {
			calls = append(calls, ToolCall{
				ID:   acc.id,
				Type: "function",
				Function: FunctionCall{
					Name:      acc.funcName,
					Arguments: acc.args.String(),
				},
			})
		}

		// Map Bedrock stop reasons to OpenAI-compatible values.
		switch finishReason {
		case "end_turn":
			finishReason = "stop"
		case "tool_use":
			finishReason = "tool_calls"
		case "max_tokens":
			finishReason = "length"
		}

		var usage *UsageStats
		if inputTokens > 0 || outputTokens > 0 {
			usage = &UsageStats{
				PromptTokens:     inputTokens,
				CompletionTokens: outputTokens,
				TotalTokens:      inputTokens + outputTokens,
			}
		}

		resCh <- streamResult{ToolCalls: calls, Usage: usage, FinishReason: finishReason}
	}()

	return ch, resCh
}

// convertToBedrockMessages extracts the system prompt and converts
// ChatMessages to Bedrock's message format.
func convertToBedrockMessages(msgs []ChatMessage) (string, []types.Message) {
	var system string
	var out []types.Message

	for _, m := range msgs {
		switch m.Role {
		case "system":
			if m.Content != nil {
				system = *m.Content
			}
		case "user":
			if m.Content != nil {
				out = append(out, types.Message{
					Role:    types.ConversationRoleUser,
					Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: *m.Content}},
				})
			}
		case "assistant":
			if len(m.ToolCalls) > 0 {
				var blocks []types.ContentBlock
				if m.Content != nil && *m.Content != "" {
					blocks = append(blocks, &types.ContentBlockMemberText{Value: *m.Content})
				}
				for _, tc := range m.ToolCalls {
					// Parse the JSON arguments string into a generic map for the document type.
					var inputMap interface{}
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &inputMap); err != nil {
						inputMap = map[string]interface{}{}
					}
					blocks = append(blocks, &types.ContentBlockMemberToolUse{
						Value: types.ToolUseBlock{
							ToolUseId: strPtr(tc.ID),
							Name:      strPtr(tc.Function.Name),
							Input:     document.NewLazyDocument(inputMap),
						},
					})
				}
				out = append(out, types.Message{
					Role:    types.ConversationRoleAssistant,
					Content: blocks,
				})
			} else if m.Content != nil {
				out = append(out, types.Message{
					Role:    types.ConversationRoleAssistant,
					Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: *m.Content}},
				})
			}
		case "tool":
			content := ""
			if m.Content != nil {
				content = *m.Content
			}
			out = append(out, types.Message{
				Role: types.ConversationRoleUser,
				Content: []types.ContentBlock{
					&types.ContentBlockMemberToolResult{
						Value: types.ToolResultBlock{
							ToolUseId: strPtr(m.ToolCallID),
							Content: []types.ToolResultContentBlock{
								&types.ToolResultContentBlockMemberText{Value: content},
							},
						},
					},
				},
			})
		}
	}

	return system, out
}

// convertToBedrockTools converts ToolDefinitions to Bedrock's tool configuration.
func convertToBedrockTools(tools []ToolDefinition) *types.ToolConfiguration {
	brTools := make([]types.Tool, len(tools))
	for i, t := range tools {
		brTools[i] = &types.ToolMemberToolSpec{
			Value: types.ToolSpecification{
				Name:        strPtr(t.Function.Name),
				Description: strPtr(t.Function.Description),
				InputSchema: &types.ToolInputSchemaMemberJson{
					Value: document.NewLazyDocument(t.Function.Parameters),
				},
			},
		}
	}
	return &types.ToolConfiguration{
		Tools:      brTools,
		ToolChoice: &types.ToolChoiceMemberAuto{Value: types.AutoToolChoice{}},
	}
}

// listBedrockModels queries AWS Bedrock for available foundation models.
func listBedrockModels(region string) ([]DockerModel, error) {
	ctx := context.Background()
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := bedrock.NewFromConfig(cfg)
	output, err := client.ListFoundationModels(ctx, &bedrock.ListFoundationModelsInput{})
	if err != nil {
		return nil, fmt.Errorf("bedrock ListFoundationModels: %w", err)
	}

	var models []DockerModel
	for _, m := range output.ModelSummaries {
		if m.ModelId == nil {
			continue
		}

		// Only include text-output models.
		hasTextOutput := false
		for _, mod := range m.OutputModalities {
			if mod == bedrocktypes.ModelModalityText {
				hasTextOutput = true
				break
			}
		}
		if !hasTextOutput {
			continue
		}

		// Skip legacy models.
		if m.ModelLifecycle != nil && m.ModelLifecycle.Status == bedrocktypes.FoundationModelLifecycleStatusLegacy {
			continue
		}

		// Skip embedding models.
		hasTextInput := false
		for _, mod := range m.InputModalities {
			if mod == bedrocktypes.ModelModalityText {
				hasTextInput = true
				break
			}
		}
		if !hasTextInput {
			continue
		}

		id := *m.ModelId
		p := LookupBedrockPricing(id)
		models = append(models, DockerModel{
			Name:            id,
			Tag:             id,
			Provider:        "bedrock",
			PromptPrice:     p.PromptPrice,
			CompletionPrice: p.CompletionPrice,
		})
	}

	return models, nil
}

// derefStr safely dereferences a *string, returning "" if nil.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// strPtr returns a pointer to the given string.
func strPtr(s string) *string {
	return &s
}
