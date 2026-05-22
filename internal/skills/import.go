package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type pluginManifest struct {
	Name       string         `json:"name"`
	Skills     []string       `json:"skills"`
	MCPServers map[string]any `json:"mcpServers"`
}

func (s *Service) installPlugin(root string, manifestPath string, opts InstallOptions) ([]Skill, error) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var manifest pluginManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("parse plugin manifest: %w", err)
	}
	if len(manifest.MCPServers) > 0 {
		for id, cfg := range manifest.MCPServers {
			cfgJSON, _ := json.Marshal(cfg)
			_, _ = s.db.Exec(`INSERT OR REPLACE INTO skill_plugin_mcp_candidates(id, plugin_path, config_json, enabled, created_at) VALUES (?, ?, ?, 0, ?)`,
				NormalizeID(firstNonEmpty(manifest.Name, filepath.Base(root))+"-"+id), root, string(cfgJSON), formatTime(s.now().UTC()))
		}
	}
	var installed []Skill
	for _, rel := range manifest.Skills {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		path := filepath.Clean(filepath.Join(root, rel))
		if !strings.HasPrefix(path, filepath.Clean(root)+string(filepath.Separator)) && path != filepath.Clean(root) {
			return nil, fmt.Errorf("plugin skill path escapes plugin root: %s", rel)
		}
		skill, err := s.InstallLocal(path, InstallOptions{
			Provenance: firstNonEmpty(opts.Provenance, manifestPath),
			Source:     "plugin",
			TrustState: opts.TrustState,
		})
		if err != nil {
			return nil, err
		}
		installed = append(installed, skill)
	}
	if len(installed) == 0 {
		return nil, fmt.Errorf("plugin has no importable skills")
	}
	return installed, nil
}

func (s *Service) installHermesTree(root string, opts InstallOptions) ([]Skill, error) {
	var installed []Skill
	for _, base := range []string{filepath.Join(root, "skills"), filepath.Join(root, "optional-skills")} {
		_ = filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry == nil || entry.IsDir() || entry.Name() != "SKILL.md" {
				return nil
			}
			skill, installErr := s.InstallLocal(filepath.Dir(path), InstallOptions{
				Provenance: firstNonEmpty(opts.Provenance, root),
				Source:     "hermes",
				TrustState: opts.TrustState,
			})
			if installErr == nil {
				installed = append(installed, skill)
			}
			return nil
		})
	}
	if len(installed) == 0 {
		return nil, fmt.Errorf("path does not contain SKILL.md or a supported plugin/Hermes skill tree: %s", root)
	}
	return installed, nil
}
