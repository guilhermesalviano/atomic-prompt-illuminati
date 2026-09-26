package models

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/guilhermesalviano/korchestrate/internal/agent"
	"github.com/guilhermesalviano/korchestrate/internal/config"
)

// antigravityDiscoverTimeout bounds the `agy models` call.
const antigravityDiscoverTimeout = 15 * time.Second

// DiscoverAntigravity runs `agy models` and lists the model ids it prints.
// The ids already encode the reasoning level (e.g. gemini-3.1-pro-high), so no
// separate efforts are offered. When the CLI fails the built-in default is
// returned.
func DiscoverAntigravity() AgentInfo {
	ctx, cancel := context.WithTimeout(context.Background(), antigravityDiscoverTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, agent.AntigravityBin, "models").Output()
	if err == nil {
		if info := ParseAntigravityModels(out); len(info.Models) > 0 {
			return info
		}
	}
	return AgentInfo{
		Agent:  "antigravity",
		Models: []ModelInfo{{ID: config.DefaultModelFor("antigravity")}},
	}
}

// ParseAntigravityModels decodes the `id<TAB>display name` lines printed by
// `agy models`, skipping the progress banner and any other free text.
func ParseAntigravityModels(data []byte) AgentInfo {
	info := AgentInfo{Agent: "antigravity"}
	for _, line := range strings.Split(stripANSI(string(data)), "\n") {
		id, _, ok := strings.Cut(strings.TrimSpace(line), "\t")
		id = strings.TrimSpace(id)
		if !ok || id == "" || strings.ContainsAny(id, " ") {
			continue
		}
		info.Models = append(info.Models, ModelInfo{ID: id})
	}
	return info
}
