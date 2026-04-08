package builtin

import (
	"context"
	"encoding/json"
	"testing"
)

// TestClarificationTool_Name tests the Name method
func TestClarificationTool_Name(t *testing.T) {
	tool := NewClarificationTool()
	if tool.Name() != "ask_clarification" {
		t.Errorf("expected name 'ask_clarification', got %q", tool.Name())
	}
}

// TestClarificationTool_Description tests the Description method
func TestClarificationTool_Description(t *testing.T) {
	tool := NewClarificationTool()
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

// TestClarificationTool_InputSchema tests the InputSchema method
func TestClarificationTool_InputSchema(t *testing.T) {
	tool := NewClarificationTool()
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("expected non-empty input schema")
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("input schema is not valid JSON: %v", err)
	}
}

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

func TestClarificationTool_Execute_WithClarificationType(t *testing.T) {
	tool := NewClarificationTool()

	validTypes := []string{"missing_info", "ambiguous_requirement", "approach_choice", "risk_confirmation", "suggestion"}
	for _, ct := range validTypes {
		in, _ := json.Marshal(clarificationInput{
			Description:       "need info",
			Question:          "Which approach?",
			ClarificationType: ct,
		})

		out, err := tool.Execute(context.Background(), string(in))
		if err != nil {
			t.Errorf("unexpected error for type %q: %v", ct, err)
			continue
		}

		var result map[string]any
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Errorf("invalid JSON for type %q: %v", ct, err)
			continue
		}

		if result["clarification_type"] != ct {
			t.Errorf("expected clarification_type %q, got %v", ct, result["clarification_type"])
		}
	}
}

func TestClarificationTool_Execute_InvalidClarificationType(t *testing.T) {
	tool := NewClarificationTool()
	in, _ := json.Marshal(clarificationInput{
		Description:       "need info",
		Question:          "Test question?",
		ClarificationType: "invalid_type",
	})

	_, err := tool.Execute(context.Background(), string(in))
	if err == nil {
		t.Error("expected error for invalid clarification_type")
	}
}

func TestClarificationTool_Execute_InvalidJSON(t *testing.T) {
	tool := NewClarificationTool()
	_, err := tool.Execute(context.Background(), `{invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestNewClarificationTool(t *testing.T) {
	tool := NewClarificationTool()
	if tool == nil {
		t.Error("expected non-nil tool")
	}
}
