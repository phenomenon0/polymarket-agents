// Package core provides minimal framework types for tool execution.
// This is a standalone shim extracted from the Agent-GO core framework.
package core

import (
	"context"
	"encoding/json"
	"time"
)

// Tool execution status constants.
const (
	ToolComplete = "complete"
	ToolFailed   = "failed"
	ToolCanceled = "canceled"
)

// ToolContext carries context for tool execution.
type ToolContext struct {
	Ctx     context.Context
	Request *Message
}

// ToolExecResult is the result of a tool execution.
type ToolExecResult struct {
	Status   string         `json:"status"`
	Output   interface{}    `json:"output,omitempty"`
	Error    string         `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ToolChunk represents a streaming chunk from a tool.
type ToolChunk struct {
	Index   int    `json:"index"`
	Data    string `json:"data,omitempty"`
	IsFinal bool   `json:"is_final,omitempty"`
}

// Message represents a message in the agent framework.
type Message struct {
	Role    string              `json:"role,omitempty"`
	Content string              `json:"content,omitempty"`
	ToolReq *ToolRequestPayload `json:"tool_req,omitempty"`
}

// ToolRequestPayload holds tool invocation data.
type ToolRequestPayload struct {
	Name     string          `json:"name,omitempty"`
	Input    any             `json:"input,omitempty"`
	InputRaw json.RawMessage `json:"input_raw,omitempty"`
}

// ToolPolicy defines rate limiting and retry policies for tools.
type ToolPolicy struct {
	MaxRetries      int           `json:"max_retries"`
	BaseBackoff     time.Duration `json:"base_backoff"`
	MaxBackoff      time.Duration `json:"max_backoff"`
	Retriable       bool          `json:"retriable"`
	DefaultTimeout  time.Duration `json:"default_timeout"`
	RateLimitPerSec float64       `json:"rate_limit_per_sec"`
	Burst           int           `json:"burst"`
	LimitKey        string        `json:"limit_key"`
	BudgetPerDay    float64       `json:"budget_per_day"`
	CostPerCall     float64       `json:"cost_per_call"`
}

// ToolRegistry is a registry for tools with policies.
type ToolRegistry struct {
	tools []registeredTool
}

type registeredTool struct {
	tool      interface{}
	policy    ToolPolicy
	riskClass interface{}
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{}
}

// Register registers a tool with a policy and optional risk class.
func (r *ToolRegistry) Register(tool interface{}, policy ToolPolicy, riskClass interface{}) {
	r.tools = append(r.tools, registeredTool{
		tool:      tool,
		policy:    policy,
		riskClass: riskClass,
	})
}
