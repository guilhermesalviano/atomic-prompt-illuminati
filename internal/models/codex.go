package models

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DiscoverCodex reads the model catalog codex caches at
// $CODEX_HOME/models_cache.json. When the cache is missing or unreadable the
// built-in default model with the common effort ladder is returned.
func DiscoverCodex() AgentInfo {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		if u, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(u, ".codex")
		}
	}
	if home != "" {
		if data, err := os.ReadFile(filepath.Join(home, "models_cache.json")); err == nil {
			if info, err := ParseCodexCache(data); err == nil && len(info.Models) > 0 {
				return info
			}
		}
	}
	return defaultCodex()
}

// defaultCodex mirrors the codex fallback used by the orchestrator config.
func defaultCodex() AgentInfo {
	return AgentInfo{
		Agent: "codex",
		Models: []ModelInfo{
			{ID: "gpt-6-sol", DefaultEffort: "medium", Efforts: []string{"low", "medium", "high", "xhigh", "max", "ultra"}},
		},
	}
}

// ParseCodexCache decodes the models_cache.json document codex maintains.
func ParseCodexCache(data []byte) (AgentInfo, error) {
	var cache struct {
		Models []struct {
			Slug          string `json:"slug"`
			DefaultReason string `json:"default_reasoning_level"`
			Levels        []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return AgentInfo{}, err
	}
	info := AgentInfo{Agent: "codex"}
	for _, m := range cache.Models {
		if m.Slug == "" {
			continue
		}
		mi := ModelInfo{ID: m.Slug, DefaultEffort: m.DefaultReason}
		for _, l := range m.Levels {
			if l.Effort != "" {
				mi.Efforts = append(mi.Efforts, l.Effort)
			}
		}
		info.Models = append(info.Models, mi)
	}
	return info, nil
}
