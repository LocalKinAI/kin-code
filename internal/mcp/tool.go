package mcp

import (
	"github.com/LocalKinAI/kincode/pkg/provider"
	"github.com/LocalKinAI/kincode/pkg/tools"
)

// MCPTool wraps an MCP server tool as a kincode Tool.
type MCPTool struct {
	client *Client
	name   string
	desc   string
	schema map[string]any
}

// Ensure MCPTool implements tools.Tool.
var _ tools.Tool = (*MCPTool)(nil)

func (t *MCPTool) Name() string        { return "mcp_" + t.name }
func (t *MCPTool) Description() string  { return t.desc }

func (t *MCPTool) Def() provider.ToolDef {
	params := t.schema
	if params == nil {
		params = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}
	return provider.NewToolDef(t.Name(), t.desc, params)
}

func (t *MCPTool) Execute(args map[string]any) (string, error) {
	return t.client.CallTool(t.name, args)
}

// ToolsFromClient converts all MCP tool definitions into kincode Tool instances.
func ToolsFromClient(c *Client) []tools.Tool {
	var result []tools.Tool
	for _, td := range c.Tools() {
		result = append(result, &MCPTool{
			client: c,
			name:   td.Name,
			desc:   td.Description,
			schema: td.InputSchema,
		})
	}
	return result
}
