package handlers

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goclaw/internal/config"
	"goclaw/internal/skills"
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
	tmp := t.TempDir()
	extPath := filepath.Join(tmp, "extensions_config.json")
	_ = os.WriteFile(extPath, []byte(`{"mcp_servers":{},"skills":{}}`), 0o644)
	cfg := &config.AppConfig{ExtensionsRef: config.ExtensionsConfigRef{ConfigPath: extPath}}

	h := NewSkillsHandler(cfg, reg)
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
	data, err := os.ReadFile(extPath)
	if err != nil {
		t.Fatalf("read extensions failed: %v", err)
	}
	if !strings.Contains(string(data), `"demo"`) {
		t.Fatalf("expected updated skill persisted to extensions file")
	}
}

func TestSkillsHandler_InstallSkill_Success(t *testing.T) {
	cwd, _ := os.Getwd()
	tmp := t.TempDir()
	_ = os.Chdir(tmp)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	threadID := "thread-1"
	archiveRel := "outputs/demo.skill"
	archiveHost := filepath.Join(".goclaw", "threads", threadID, "user-data", archiveRel)
	if err := os.MkdirAll(filepath.Dir(archiveHost), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeSkillArchive(archiveHost, "demo-skill", "Demo skill"); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	extPath := filepath.Join(tmp, "extensions_config.json")
	cfg := &config.AppConfig{
		ExtensionsRef: config.ExtensionsConfigRef{ConfigPath: extPath},
		Skills:        config.SkillsConfig{Path: filepath.Join(tmp, "skills")},
	}
	h := NewSkillsHandler(cfg, nil)

	body := `{"thread_id":"thread-1","path":"/mnt/user-data/outputs/demo.skill"}`
	req := httptest.NewRequest(http.MethodPost, "/api/skills/install", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, nil)

	h.InstallSkill(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	data, err := os.ReadFile(extPath)
	if err != nil {
		t.Fatalf("read extensions: %v", err)
	}
	var ext config.ExtensionsConfig
	if err := json.Unmarshal(data, &ext); err != nil {
		t.Fatalf("unmarshal extensions: %v", err)
	}
	if !ext.Skills["demo-skill"].Enabled {
		t.Fatalf("expected demo-skill enabled in extensions")
	}
	if _, err := os.Stat(filepath.Join(tmp, "skills", "custom", "demo-skill", "SKILL.md")); err != nil {
		t.Fatalf("expected extracted SKILL.md: %v", err)
	}
}

func TestSkillsHandler_InstallSkill_NotFound(t *testing.T) {
	cfg := &config.AppConfig{
		ExtensionsRef: config.ExtensionsConfigRef{ConfigPath: filepath.Join(t.TempDir(), "extensions_config.json")},
	}
	h := NewSkillsHandler(cfg, nil)
	body := `{"thread_id":"thread-1","path":"/mnt/user-data/outputs/missing.skill"}`
	req := httptest.NewRequest(http.MethodPost, "/api/skills/install", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, nil)

	h.InstallSkill(ctx)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestSkillsHandler_InstallSkill_MissingSkillMD(t *testing.T) {
	cwd, _ := os.Getwd()
	tmp := t.TempDir()
	_ = os.Chdir(tmp)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	threadID := "thread-1"
	archiveRel := "outputs/invalid.skill"
	archiveHost := filepath.Join(".goclaw", "threads", threadID, "user-data", archiveRel)
	if err := os.MkdirAll(filepath.Dir(archiveHost), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeRawArchive(archiveHost, map[string]string{"README.md": "# no skill"}); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	cfg := &config.AppConfig{
		ExtensionsRef: config.ExtensionsConfigRef{ConfigPath: filepath.Join(tmp, "extensions_config.json")},
		Skills:        config.SkillsConfig{Path: filepath.Join(tmp, "skills")},
	}
	h := NewSkillsHandler(cfg, nil)
	body := `{"thread_id":"thread-1","path":"/mnt/user-data/outputs/invalid.skill"}`
	req := httptest.NewRequest(http.MethodPost, "/api/skills/install", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, nil)

	h.InstallSkill(ctx)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "SKILL.md") {
		t.Fatalf("expected SKILL.md validation error, got %s", rr.Body.String())
	}
}

func TestSkillsHandler_InstallSkill_InvalidName(t *testing.T) {
	cwd, _ := os.Getwd()
	tmp := t.TempDir()
	_ = os.Chdir(tmp)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	threadID := "thread-1"
	archiveRel := "outputs/invalid-name.skill"
	archiveHost := filepath.Join(".goclaw", "threads", threadID, "user-data", archiveRel)
	if err := os.MkdirAll(filepath.Dir(archiveHost), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeRawArchive(archiveHost, map[string]string{
		"SKILL.md": "---\nname: Invalid_Name\ndescription: bad\n---\n# test",
	}); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	cfg := &config.AppConfig{
		ExtensionsRef: config.ExtensionsConfigRef{ConfigPath: filepath.Join(tmp, "extensions_config.json")},
		Skills:        config.SkillsConfig{Path: filepath.Join(tmp, "skills")},
	}
	h := NewSkillsHandler(cfg, nil)
	body := `{"thread_id":"thread-1","path":"/mnt/user-data/outputs/invalid-name.skill"}`
	req := httptest.NewRequest(http.MethodPost, "/api/skills/install", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, nil)

	h.InstallSkill(ctx)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "hyphen-case") {
		t.Fatalf("expected name validation error, got %s", rr.Body.String())
	}
}

func writeSkillArchive(path, skillName, desc string) error {
	return writeRawArchive(path, map[string]string{
		"SKILL.md": "---\nname: " + skillName + "\ndescription: " + desc + "\n---\n\n# Hello",
	})
}

func writeRawArchive(path string, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(content)); err != nil {
			return err
		}
	}
	return zw.Close()
}
