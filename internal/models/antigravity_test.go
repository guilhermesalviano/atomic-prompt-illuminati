package models

import "testing"

func TestParseAntigravityModels(t *testing.T) {
	out := "Fetching available models...\ngemini-3.1-pro-high\tGemini 3.1 Pro (High)\nclaude-sonnet-4-6\tClaude Sonnet 4.6 (Thinking)\n\n"
	info := ParseAntigravityModels([]byte(out))
	if len(info.Models) != 2 || info.Models[0].ID != "gemini-3.1-pro-high" || info.Models[1].ID != "claude-sonnet-4-6" {
		t.Fatalf("models = %+v", info.Models)
	}
}
