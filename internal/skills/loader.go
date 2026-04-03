package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bookerbai/goclaw/internal/config"
)

// Loader scans skills directories and loads enabled skills.
type Loader struct{}

func NewLoader() *Loader { return &Loader{} }

func (l *Loader) Load(rootPath string, ext config.ExtensionsConfig) ([]*Skill, error) {
	base := strings.TrimSpace(rootPath)
	if base == "" {
		base = "skills"
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return nil, fmt.Errorf("skills loader: resolve path %q failed: %w", base, err)
	}

	scanRoots := []string{
		filepath.Join(absBase, "public"),
		filepath.Join(absBase, "custom"),
	}

	out := make([]*Skill, 0)
	for _, root := range scanRoots {
		if _, err := os.Stat(root); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("skills loader: stat %s failed: %w", root, err)
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if strings.ToUpper(d.Name()) != "SKILL.MD" {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s failed: %w", path, err)
			}

			meta, _, err := ParseSkillMarkdown(string(data))
			if err != nil {
				return fmt.Errorf("parse %s failed: %w", path, err)
			}
			if meta.Name == "" {
				meta.Name = filepath.Base(filepath.Dir(path))
			}
			if !isSkillEnabled(ext, meta.Name) {
				return nil
			}

			out = append(out, &Skill{
				Metadata: meta,
				Dir:      filepath.Dir(path),
				FilePath: path,
				Plugin:   NewNoopPlugin(meta.Name),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Metadata.Name < out[j].Metadata.Name
	})
	return out, nil
}

func isSkillEnabled(ext config.ExtensionsConfig, name string) bool {
	state, ok := ext.Skills[name]
	if !ok {
		return true
	}
	return state.Enabled
}
