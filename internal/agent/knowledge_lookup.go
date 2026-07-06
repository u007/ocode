package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/u007/ocode/internal/knowledge"
)

// KnowledgeLookupTool dispatches the context subagent to answer a knowledge
// question from the OKF bundle. It is always registered (stable tool-definition
// block preserves the provider prompt-cache prefix) and soft-fails when the
// knowledge system is not active.
type KnowledgeLookupTool struct {
	mainAgent *Agent
}

func (t *KnowledgeLookupTool) Name() string        { return "knowledge_lookup" }
func (t *KnowledgeLookupTool) Description() string { return "Look up a question in the project's OKF knowledge bundle. Use this for why/decision/playbook questions before exploring code." }
func (t *KnowledgeLookupTool) Parallel() bool      { return true }

func (t *KnowledgeLookupTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "knowledge_lookup",
		"description": "Consult curated project knowledge (OKF bundle under docs/). Use this to answer 'why', 'what did we decide', 'is there a playbook/gotcha' questions before exploring source code. Dispatches the context sub-agent internally.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"question": map[string]interface{}{
					"type":        "string",
					"description": "The question to look up in the knowledge bundle.",
				},
			},
			"required": []string{"question"},
		},
	}
}

func (t *KnowledgeLookupTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid knowledge_lookup arguments: %w", err)
	}
	if params.Question == "" {
		return "", fmt.Errorf("question is required")
	}

	// Check that the knowledge system is active.
	if t.mainAgent == nil || !t.mainAgent.DocPromptEnabled() {
		return "Knowledge system not active — run /docs on and /docs init to set up the OKF knowledge bundle.", nil
	}

	wd := t.mainAgent.workDir
	if wd == "" {
		wd, _ = os.Getwd()
	}
	if _, ok := knowledge.DetectBundle(wd); !ok {
		return "Knowledge bundle not found — run /docs init to set up the OKF knowledge bundle.", nil
	}

	// Dispatch the context subagent via the existing TaskTool mechanism.
	taskTool, ok := t.mainAgent.GetTool("task")
	if !ok {
		return "", fmt.Errorf("task tool not available")
	}
	task, ok := taskTool.(*TaskTool)
	if !ok {
		return "", fmt.Errorf("task tool has unexpected type")
	}

	prompt := fmt.Sprintf(`Answer the following knowledge question using the project's OKF knowledge bundle. Use doc_search and doc_get to find relevant documents. If no relevant documents exist, say so clearly.

Question: %s

Guidelines:
- Start with doc_search to find relevant docs.
- Read the most relevant docs with doc_get for full content.
- Verify any claims against the codebase if needed using grep/glob/read.
- Cite document paths for every claim.
- If the knowledge bundle has no relevant information, state that clearly.`, params.Question)

	result, err := task.ExecuteRaw("context", prompt, false)
	if err != nil {
		return "", fmt.Errorf("knowledge_lookup: context agent error: %w", err)
	}
	return strings.TrimSpace(result), nil
}
