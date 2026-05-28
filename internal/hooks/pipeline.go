package hooks

import "encoding/json"

type ToolBeforeFunc func(name string, args json.RawMessage) json.RawMessage
type ToolAfterFunc func(name, result string) string

// ChatParams holds optional LLM request parameter overrides. Pointer fields
// distinguish "not set" from an explicit zero.
type ChatParams struct {
	Temperature *float64
	TopP        *float64
	MaxTokens   *int
}

type ChatParamsFunc func(model string, params ChatParams) ChatParams
type ShellEnvFunc func(cwd string) map[string]string

// Pipeline holds registered hook functions for all four hook points.
type Pipeline struct {
	toolBefore []ToolBeforeFunc
	toolAfter  []ToolAfterFunc
	chatParams []ChatParamsFunc
	shellEnv   []ShellEnvFunc
}

func New() *Pipeline { return &Pipeline{} }

func (p *Pipeline) RegisterToolBefore(fn ToolBeforeFunc) { p.toolBefore = append(p.toolBefore, fn) }
func (p *Pipeline) RegisterToolAfter(fn ToolAfterFunc)   { p.toolAfter = append(p.toolAfter, fn) }
func (p *Pipeline) RegisterChatParams(fn ChatParamsFunc) { p.chatParams = append(p.chatParams, fn) }
func (p *Pipeline) RegisterShellEnv(fn ShellEnvFunc)     { p.shellEnv = append(p.shellEnv, fn) }

func (p *Pipeline) RunToolBefore(name string, args json.RawMessage) json.RawMessage {
	for _, fn := range p.toolBefore {
		args = fn(name, args)
	}
	return args
}

func (p *Pipeline) RunToolAfter(name, result string) string {
	for _, fn := range p.toolAfter {
		result = fn(name, result)
	}
	return result
}

func (p *Pipeline) RunChatParams(model string, params ChatParams) ChatParams {
	for _, fn := range p.chatParams {
		params = fn(model, params)
	}
	return params
}

func (p *Pipeline) RunShellEnv(cwd string) map[string]string {
	merged := map[string]string{}
	for _, fn := range p.shellEnv {
		for k, v := range fn(cwd) {
			merged[k] = v
		}
	}
	return merged
}
