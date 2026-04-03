package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bookerbai/goclaw/internal/skills"
)

func TestSkillsHandler_ListSkills_Empty(t *testing.T) {
	h := NewSkillsHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/skills", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, nil)

	h.ListSkills(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	list := resp["skills"].([]any)
	if len(list) != 0 {
		t.Errorf("expected empty list, got %v", list)
	}
}

func TestSkillsHandler_ListSkills_WithRegistry(t *testing.T) {
	reg := skills.NewRegistry()
	sk := &skills.Skill{Metadata: skills.SkillMetadata{Name: "demo", Enabled: true}}
	_ = reg.Register(sk)

	h := NewSkillsHandler(nil, reg)
	req := httptest.NewRequest(http.MethodGet, "/api/skills", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, nil)

	h.ListSkills(ctx)

	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	list := resp["skills"].([]any)
	if len(list) != 1 {
		t.Errorf("expected 1 skill, got %d", len(list))
	}
}

func TestSkillsHandler_GetSkill_NotFound(t *testing.T) {
	h := NewSkillsHandler(nil, skills.NewRegistry())
	req := httptest.NewRequest(http.MethodGet, "/api/skills/unknown", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"name": "unknown"})

	h.GetSkill(ctx)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestSkillsHandler_UpdateSkill(t *testing.T) {
	reg := skills.NewRegistry()
	sk := &skills.Skill{Metadata: skills.SkillMetadata{Name: "demo", Enabled: false}}
	_ = reg.Register(sk)

	h := NewSkillsHandler(nil, reg)
	body := `{"enabled": true}`
	req := httptest.NewRequest(http.MethodPut, "/api/skills/demo", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"name": "demo"})

	h.UpdateSkill(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	updated := reg.GetByName("demo")
	if !updated.Metadata.Enabled {
		t.Error("expected skill to be enabled")
	}
}
