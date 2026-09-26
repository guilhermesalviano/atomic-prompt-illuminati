package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/guibs/atomic-prompt-illuminati/internal/agent"
	"github.com/guibs/atomic-prompt-illuminati/internal/models"
)

func testCatalog() *models.Catalog {
	return &models.Catalog{Agents: []models.AgentInfo{
		{Agent: "claude", Models: []models.ModelInfo{{ID: "opus"}, {ID: "sonnet"}}},
		{Agent: "codex", Models: []models.ModelInfo{
			{ID: "gpt-6-sol", DefaultEffort: "medium", Efforts: []string{"low", "medium", "high"}},
			{ID: "gpt-6-luna", DefaultEffort: "low", Efforts: []string{"low", "high"}},
		}},
		{Agent: "opencode", Models: []models.ModelInfo{
			{ID: "opencode-go/deepseek-v4.1-flash"},
			{ID: "zai/glm-5.2", Efforts: []string{"high", "max"}},
		}},
	}}
}

func key(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func enter() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }
func esc() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyEsc} }

func TestSetupOverlayFlow(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.width, a.height = 100, 30
	a.catalog = testCatalog()

	// "m" opens the overlay on the stage list.
	a.handleKey(key("m"))
	if !a.setup.active || a.setup.mode != setupStages {
		t.Fatal("m should open the setup overlay")
	}
	// Digit 2 opens the executor row directly.
	a.handleKey(key("2"))
	if a.setup.mode != setupProvider || a.setup.stage != agent.Executor {
		t.Fatalf("digit should open the provider list for the executor: %+v", a.setup)
	}

	// Providers are claude, codex, opencode; codex is the current one.
	if provs := a.setupProviders(); provs[1] != "codex" {
		t.Fatalf("unexpected provider order: %v", provs)
	}
	if a.setup.cursor != 1 {
		t.Fatalf("cursor should start on the current provider (codex): %d", a.setup.cursor)
	}
	a.handleKey(enter())
	if a.setup.mode != setupModel {
		t.Fatalf("enter should open the model list: %+v", a.setup)
	}

	// Filter down to luna and select it.
	a.handleKey(key("l"))
	a.handleKey(key("u"))
	a.handleKey(key("n"))
	a.handleKey(key("a"))
	a.handleKey(enter())
	if a.choices.Executor.Model != "gpt-6-luna" {
		t.Fatalf("model not applied: %+v", a.choices.Executor)
	}
	if a.setup.mode != setupEffort {
		t.Fatalf("models with efforts should advance to the effort list: %+v", a.setup)
	}

	// Effort row 0 is "model default"; pick "high" (row 2).
	a.handleKey(key("j"))
	a.handleKey(key("j"))
	a.handleKey(enter())
	if a.choices.Executor.Model != "gpt-6-luna" || a.choices.Executor.Variant != "high" {
		t.Fatalf("effort not applied: %+v", a.choices.Executor)
	}
	if a.setup.mode != setupStages {
		t.Fatalf("effort selection should return to the stage list: %+v", a.setup)
	}

	// esc closes the overlay.
	a.handleKey(esc())
	if a.setup.active {
		t.Fatal("esc should close the overlay from the stage list")
	}
}

func TestSetupProviderSwitchResetsModel(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.catalog = testCatalog()

	a.setup = setupUI{active: true, mode: setupProvider, stage: agent.Executor}
	a.handleKey(enter()) // cursor 0: switch to claude
	c := a.choiceFor(agent.Executor)
	if c.Agent != "claude" || c.Model != "opus" {
		t.Fatalf("provider switch should preselect the adapter default: %+v", c)
	}
	a.handleKey(enter()) // pick opus: no efforts, straight back to the stage list
	if c.Model != "opus" || a.setup.mode != setupStages || c.Variant != "" {
		t.Fatalf("claude models have no efforts: %+v %+v", c, a.setup)
	}
}

func TestSetupEscWalksBackwards(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.catalog = testCatalog()
	a.setup = setupUI{active: true, mode: setupModel, stage: agent.Planner}
	a.handleKey(key("x"))
	a.handleKey(esc())
	if a.setup.mode != setupProvider || string(a.setup.filter) != "" {
		t.Fatalf("esc from the model list should clear the filter: %+v", a.setup)
	}
	a.handleKey(esc())
	if a.setup.mode != setupStages {
		t.Fatalf("esc from the provider list should return to the stages: %+v", a.setup)
	}
	a.handleKey(esc())
	if a.setup.active {
		t.Fatal("esc from the stage list should close the overlay")
	}
}

func TestSetupFilterRunesAndBackspace(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.catalog = testCatalog()
	a.setup = setupUI{active: true, mode: setupModel, stage: agent.Executor}
	a.handleKey(key("g"))
	a.handleKey(key("p"))
	a.handleKey(key("t"))
	if got := string(a.setup.filter); got != "gpt" {
		t.Fatalf("filter = %q, want gpt", got)
	}
	if n := len(a.setupModelList("codex")); n != 2 {
		t.Fatalf("filter should match 2 codex models, got %d", n)
	}
	a.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := string(a.setup.filter); got != "gp" {
		t.Fatalf("backspace should trim the filter, got %q", got)
	}
}

func TestSetupModelWithoutEffortsClearsVariant(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.catalog = testCatalog()
	// Reviewer default is opencode with variant high.
	if a.choices.Reviewer.Variant != "high" {
		t.Fatalf("reviewer should default to variant high: %+v", a.choices.Reviewer)
	}
	a.setup = setupUI{active: true, mode: setupModel, stage: agent.Reviewer}
	// Pick the first model (opencode-go/deepseek-v4.1-flash), which has no variants.
	a.handleKey(enter())
	if a.choices.Reviewer.Variant != "" || a.setup.mode != setupStages {
		t.Fatalf("variant should reset for a model without efforts: %+v %+v", a.choices.Reviewer, a.setup)
	}
}

func TestStartRunSnapshotsChoices(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.width, a.height = 100, 30
	a.catalog = testCatalog()

	var got models.Choices
	a.OnStart = func(_ *Session, _, _ string, choices models.Choices) {
		got = choices
	}
	a.choices.Executor = models.Choice{Agent: "codex", Model: "gpt-6-astra", Variant: "max"}

	cmd := a.startRun("wt", "do it")
	if cmd == nil {
		t.Fatal("expected command")
	}
	cmd()
	if got.Executor.Model != "gpt-6-astra" || got.Executor.Variant != "max" {
		t.Fatalf("OnStart did not receive the choices: %+v", got)
	}
	e := a.entries[0]
	if e.Models.Executor.Model != "gpt-6-astra" {
		t.Fatalf("entry did not snapshot the choices: %+v", e.Models)
	}
}

func TestStageCardsUseEntryChoices(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.width, a.height = 120, 30
	a.catalog = testCatalog()
	a.choices.Executor = models.Choice{Agent: "codex", Model: "gpt-6-astra", Variant: "max"}

	e := newEntry("x", "/repo")
	a.entries = []*Entry{e}

	view := a.View()
	if !strings.Contains(view, "gpt-6-astra") {
		t.Fatalf("stage card should show the current selection:\n%s", view)
	}

	// A run with its own snapshot wins over the session selection.
	e.Models = models.Choices{
		Planner:  models.Choice{Agent: "claude", Model: "opus"},
		Executor: models.Choice{Agent: "codex", Model: "gpt-5.5"},
		Reviewer: models.Choice{Agent: "opencode", Model: "zai/glm-5.2", Variant: "high"},
	}
	view = a.View()
	if !strings.Contains(view, "gpt-5.5") {
		t.Fatalf("stage card should show the run's snapshot:\n%s", view)
	}
	if strings.Contains(view, "gpt-6-astra") {
		t.Fatalf("session selection should not leak into a snapshotted run:\n%s", view)
	}
}

func TestSetupViewFillsTerminal(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.catalog = testCatalog()
	for _, mode := range []setupMode{setupStages, setupProvider, setupModel, setupEffort} {
		a.setup = setupUI{active: true, mode: mode, stage: agent.Executor}
		for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 40}} {
			a.width, a.height = size[0], size[1]
			lines := strings.Split(a.View(), "\n")
			if len(lines) != size[1] {
				t.Errorf("mode %d %dx%d: %d lines", mode, size[0], size[1], len(lines))
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > size[0] {
					t.Errorf("mode %d %dx%d line %d: width %d", mode, size[0], size[1], i, w)
				}
			}
		}
	}
}

func TestCatalogMsgUpdatesCatalog(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	if a.catalog != nil {
		t.Fatal("catalog should start nil")
	}
	a.Update(catalogMsg{catalog: testCatalog()})
	if a.catalog == nil || a.catalog.Find("codex", "gpt-6-sol") == nil {
		t.Fatal("catalogMsg should install the discovered catalog")
	}
}
