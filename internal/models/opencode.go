package models

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/guilhermesalviano/korchestrate/internal/config"
)

// opencodeDiscoverTimeout bounds the `opencode models --verbose` call.
const opencodeDiscoverTimeout = 15 * time.Second

// DiscoverOpencode runs `opencode models --verbose` and extracts every
// provider/model pair the CLI knows, plus each model's variants (opencode's
// reasoning-effort knob). When the CLI is unavailable the built-in default is
// returned.
func DiscoverOpencode() AgentInfo {
	ctx, cancel := context.WithTimeout(context.Background(), opencodeDiscoverTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "opencode", "models", "--verbose").Output()
	if err != nil || len(out) == 0 {
		return defaultOpencode()
	}
	info, perr := ParseOpencodeVerbose(out)
	if perr != nil || len(info.Models) == 0 {
		return defaultOpencode()
	}
	return info
}

// defaultOpencode mirrors the reviewer default in the orchestrator config.
func defaultOpencode() AgentInfo {
	return AgentInfo{
		Agent:  "opencode",
		Models: []ModelInfo{{ID: config.DefaultModelFor("opencode")}},
	}
}

// ParseOpencodeVerbose decodes the alternating `provider/model` header lines
// and pretty-printed JSON blocks printed by `opencode models --verbose`. Only
// the JSON blocks matter: each carries providerID, id and variants.
func ParseOpencodeVerbose(data []byte) (AgentInfo, error) {
	info := AgentInfo{Agent: "opencode"}
	for _, block := range jsonBlocks(stripANSI(string(data))) {
		var m struct {
			ID         string `json:"id"`
			ProviderID string `json:"providerID"`
			Variants   map[string]struct {
				ReasoningEffort string `json:"reasoningEffort"`
			} `json:"variants"`
		}
		if err := json.Unmarshal([]byte(block), &m); err != nil {
			continue
		}
		if m.ID == "" {
			continue
		}
		full := m.ID
		if m.ProviderID != "" {
			full = m.ProviderID + "/" + m.ID
		}
		mi := ModelInfo{ID: full}
		for _, v := range sortedKeys(m.Variants) {
			mi.Efforts = append(mi.Efforts, v)
		}
		info.Models = append(info.Models, mi)
	}
	return info, nil
}

// jsonBlocks returns the top-level JSON objects embedded in the text: runs of
// lines that start with "{", end with "}" and parse as a whole.
func jsonBlocks(text string) []string {
	var blocks []string
	var cur []string
	depth := 0
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case depth == 0 && trimmed == "{":
			cur = []string{"{"}
			depth = 1
		case depth > 0:
			cur = append(cur, line)
			depth += braceDelta(trimmed)
			if depth <= 0 {
				blocks = append(blocks, strings.Join(cur, "\n"))
				cur, depth = nil, 0
			}
		}
	}
	return blocks
}

// braceDelta counts unescaped braces on a single line.
func braceDelta(line string) int {
	delta := 0
	inString := false
	esc := false
	for _, r := range line {
		switch {
		case esc:
			esc = false
		case r == '\\':
			esc = true
		case r == '"':
			inString = !inString
		case !inString && r == '{':
			delta++
		case !inString && r == '}':
			delta--
		}
	}
	return delta
}
