package builtin

import (
	"context"
	"encoding/json"
	"testing"
)

func TestClarificationTool_Execute(t *testing.T) {
	tool := NewClarificationTool()
	in, _ := json.Marshal(clarificationInput{
		Description: "need more info",
		Question:    "Which library should I use?",
		Options:     []string{"lodash", "underscore"},
	})

	out, err := tool.Execute(context.Background(), string(in))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if result["action"] != "clarify" {
		t.Errorf("expected action=clarify, got %v", result["action"])
	}
	if result["question"] != "Which library should I use?" {
		t.Errorf("unexpected question: %v", result["question"])
	}
	opts, ok := result["options"].([]interface{})
	if !ok || len(opts) != 2 {
		t.Errorf("expected 2 options, got %v", result["options"])
	}
}

func TestClarificationTool_Execute_NoOptions(t *testing.T) {
	tool := NewClarificationTool()
	in, _ := json.Marshal(clarificationInput{
		Description: "need info",
		Question:    "What is the target directory?",
	})

	out, err := tool.Execute(context.Background(), string(in))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var result map[string]any
	_ = json.Unmarshal([]byte(out), &result)
	if _, exists := result["options"]; exists {
		t.Errorf("expected no options key, got %v", result["options"])
	}
}

func TestClarificationTool_Execute_EmptyQuestion(t *testing.T) {
	tool := NewClarificationTool()
	in, _ := json.Marshal(clarificationInput{
		Description: "need info",
		Question:    "",
	})

	_, err := tool.Execute(context.Background(), string(in))
	if err == nil {
		t.Error("expected error for empty question")
	}
}
