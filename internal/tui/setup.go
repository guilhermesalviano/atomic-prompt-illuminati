// Pre-run model setup: the overlay opened with "m" that lets the user pick
// provider, model and reasoning effort for each pipeline stage before a run
// starts. Selections are sticky for the session and snapshotted onto each run.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/guibs/atomic-prompt-illuminati/internal/agent"
	"github.com/guibs/atomic-prompt-illuminati/internal/models"
)

// setupPage is how many rows pgup/pgdn jump in the model list.
const setupPage = 10

type setupMode int

const (
	setupStages setupMode = iota
	setupProvider
	setupModel
	setupEffort
)

// setupUI is the overlay state machine. cursor addresses the option list of
// the current mode; filter narrows the model list.
type setupUI struct {
	active bool
	mode   setupMode
	stage  agent.Kind // stage being edited
	cursor int
	filter []rune
	top    int
}

// choiceFor returns a pointer to the sticky choice for a stage.
func (a *App) choiceFor(k agent.Kind) *models.Choice {
	switch k {
	case agent.Planner:
		return &a.choices.Planner
	case agent.Executor:
		return &a.choices.Executor
	default:
		return &a.choices.Reviewer
	}
}

func stageIndex(k agent.Kind) int {
	for i, s := range stageOrder {
		if s == k {
			return i
		}
	}
	return 0
}

// setupProviders lists selectable adapters, preferring discovered catalogs.
func (a *App) setupProviders() []string {
	if a.catalog == nil {
		return models.ProviderOrder()
	}
	var out []string
	for _, name := range models.ProviderOrder() {
		if info := a.catalog.Agent(name); info != nil && len(info.Models) > 0 {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return models.ProviderOrder()
	}
	return out
}

// setupModelList returns the stage's current provider models matching the
// active filter.
func (a *App) setupModelList(provider string) []models.ModelInfo {
	if a.catalog == nil {
		return nil
	}
	info := a.catalog.Agent(provider)
	if info == nil {
		return nil
	}
	f := strings.ToLower(string(a.setup.filter))
	var out []models.ModelInfo
	for _, m := range info.Models {
		if f == "" || strings.Contains(strings.ToLower(m.ID), f) {
			out = append(out, m)
		}
	}
	return out
}

// setupOptions is the selectable list for the current mode.
func (a *App) setupOptions() []string {
	switch a.setup.mode {
	case setupProvider:
		return a.setupProviders()
	case setupModel:
		list := a.setupModelList(a.choiceFor(a.setup.stage).Agent)
		ids := make([]string, len(list))
		for i, m := range list {
			ids[i] = m.ID
		}
		return ids
	case setupEffort:
		c := a.choiceFor(a.setup.stage)
		return append([]string{""}, a.catalog.Efforts(c.Agent, c.Model)...)
	default:
		out := make([]string, len(stageOrder))
		for i, k := range stageOrder {
			out[i] = string(k)
		}
		return out
	}
}

func (a *App) providerCursor(name string) int {
	for i, p := range a.setupProviders() {
		if p == name {
			return i
		}
	}
	return 0
}

func (a *App) modelCursor(provider, model string) int {
	for i, m := range a.setupModelList(provider) {
		if m.ID == model {
			return i
		}
	}
	return 0
}

// handleSetupKey drives the model-setup overlay.
func (a *App) handleSetupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := &a.setup
	move := func(d int) {
		if n := len(a.setupOptions()); n > 0 {
			s.cursor = min(max(s.cursor+d, 0), n-1)
		}
	}
	typeFilter := func(runes string) {
		s.filter = append(s.filter, []rune(runes)...)
		s.cursor, s.top = 0, 0
	}
	switch msg.String() {
	case "ctrl+c":
		return a.quit()
	case "esc":
		a.setupBack()
	case "q":
		if s.mode != setupModel {
			s.active = false
		} else {
			typeFilter("q")
		}
	case "up":
		move(-1)
	case "down":
		move(1)
	case "k":
		if s.mode != setupModel {
			move(-1)
		} else {
			typeFilter("k")
		}
	case "j":
		if s.mode != setupModel {
			move(1)
		} else {
			typeFilter("j")
		}
	case "pgup", "ctrl+u", "K", "shift+up":
		if s.mode == setupModel {
			move(-setupPage)
		}
	case "pgdown", "ctrl+d", "J", "shift+down":
		if s.mode == setupModel {
			move(setupPage)
		}
	case "enter":
		a.setupEnter()
	case "backspace":
		if s.mode == setupModel && len(s.filter) > 0 {
			s.filter = s.filter[:len(s.filter)-1]
			s.cursor, s.top = 0, 0
		}
	default:
		if msg.Type != tea.KeyRunes && msg.Type != tea.KeySpace {
			return a, nil
		}
		r := msg.String()
		if s.mode == setupModel {
			typeFilter(r)
			return a, nil
		}
		if len(r) == 1 && r[0] >= '1' && r[0] <= '9' {
			if i := int(r[0] - '1'); i < len(a.setupOptions()) {
				s.cursor = i
				a.setupEnter()
			}
		}
	}
	return a, nil
}

// setupBack answers esc: one level out, or close from the stage list.
func (a *App) setupBack() {
	s := &a.setup
	switch s.mode {
	case setupProvider:
		s.mode, s.cursor = setupStages, stageIndex(s.stage)
	case setupModel:
		s.mode = setupProvider
		s.cursor = a.providerCursor(a.choiceFor(s.stage).Agent)
		s.filter, s.top = nil, 0
	case setupEffort:
		s.mode, s.cursor = setupStages, stageIndex(s.stage)
	default:
		s.active = false
	}
}

// setupEnter commits the selection under the cursor and advances the flow.
func (a *App) setupEnter() {
	s := &a.setup
	switch s.mode {
	case setupStages:
		if s.cursor >= 0 && s.cursor < len(stageOrder) {
			s.stage = stageOrder[s.cursor]
			s.mode = setupProvider
			s.cursor = a.providerCursor(a.choiceFor(s.stage).Agent)
		}
	case setupProvider:
		provs := a.setupProviders()
		if s.cursor >= len(provs) {
			return
		}
		c := a.choiceFor(s.stage)
		c.Agent = provs[s.cursor]
		if a.catalog.Find(c.Agent, c.Model) == nil {
			c.Model = a.catalog.DefaultModel(c.Agent)
			c.Variant = ""
		}
		s.mode = setupModel
		s.filter, s.top = nil, 0
		s.cursor = a.modelCursor(c.Agent, c.Model)
	case setupModel:
		list := a.setupModelList(a.choiceFor(s.stage).Agent)
		if s.cursor >= len(list) {
			return
		}
		c := a.choiceFor(s.stage)
		c.Model = list[s.cursor].ID
		c.Variant = ""
		if efforts := a.catalog.Efforts(c.Agent, c.Model); len(efforts) > 0 {
			s.mode = setupEffort
			s.cursor = 0 // "model default"
		} else {
			s.mode, s.cursor = setupStages, stageIndex(s.stage)
		}
	case setupEffort:
		c := a.choiceFor(s.stage)
		efforts := a.catalog.Efforts(c.Agent, c.Model)
		switch {
		case s.cursor == 0:
			c.Variant = ""
		case s.cursor-1 < len(efforts):
			c.Variant = efforts[s.cursor-1]
		}
		s.mode, s.cursor = setupStages, stageIndex(s.stage)
	}
}

// --- rendering ---------------------------------------------------------------

func choiceText(c models.Choice) string {
	s := c.Agent + " · " + c.Model
	if c.Variant != "" {
		s += " · " + c.Variant
	}
	return s
}

func (a *App) catalogSummary() string {
	if a.catalog == nil {
		return "discovering models…"
	}
	var parts []string
	for _, name := range models.ProviderOrder() {
		if info := a.catalog.Agent(name); info != nil {
			parts = append(parts, fmt.Sprintf("%s %d", name, len(info.Models)))
		}
	}
	return "catalog · " + strings.Join(parts, " · ")
}

// renderSetup draws the modal centered on the terminal.
func (a *App) renderSetup(w, h int) string {
	boxW := min(64, max(44, w-6))
	inner := boxW - 4
	var lines []string
	switch a.setup.mode {
	case setupStages:
		lines = a.renderSetupStages(inner)
	case setupProvider:
		lines = a.renderSetupProvider(inner)
	case setupModel:
		lines = a.renderSetupModel(inner, h)
	case setupEffort:
		lines = a.renderSetupEffort(inner)
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(cGold).Padding(0, 1).Width(boxW - 2).
		Render(strings.Join(lines, "\n"))
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
}

func (a *App) setupTitle(step string) string {
	return goldStyle.Bold(true).Render("◆ "+strings.ToUpper(string(a.setup.stage))+" "+step) +
		mutedStyle.Render("  ·  press esc to go back")
}

func (a *App) renderSetupStages(w int) []string {
	lines := []string{
		goldStyle.Bold(true).Render("◆ MODELS") + mutedStyle.Render("  ·  the next run will use these"),
		"",
	}
	for i, k := range stageOrder {
		mark := "  "
		style := textStyle
		if i == a.setup.cursor {
			mark = goldStyle.Render("❯ ")
			style = goldStyle.Bold(true)
		}
		row := fmt.Sprintf("%d. %-9s %s", i+1, string(k), choiceText(a.choices.For(k)))
		lines = append(lines, " "+mark+style.Render(truncate(row, w-4)))
	}
	lines = append(lines,
		"",
		faintStyle.Render(truncate(a.catalogSummary(), w-2)),
		truncate(" "+strings.Join([]string{
			keyHint("↑↓", "choose"), keyHint("enter", "change"), keyHint("esc", "close"),
		}, mutedStyle.Render("  ·  ")), w-2),
	)
	return lines
}

func (a *App) renderSetupProvider(w int) []string {
	current := a.choiceFor(a.setup.stage).Agent
	lines := []string{a.setupTitle("provider"), ""}
	for i, p := range a.setupProviders() {
		mark := "  "
		style := textStyle
		if i == a.setup.cursor {
			mark = goldStyle.Render("❯ ")
			style = goldStyle.Bold(true)
		}
		label := fmt.Sprintf("%d. %s", i+1, p)
		if p == current {
			label += " (current)"
		}
		if a.catalog != nil {
			if info := a.catalog.Agent(p); info != nil {
				label += mutedStyle.Render(fmt.Sprintf("  %d models", len(info.Models)))
			}
		}
		lines = append(lines, " "+mark+style.Render(truncate(label, w-4)))
	}
	lines = append(lines, "",
		truncate(" "+strings.Join([]string{
			keyHint("↑↓", "choose"), keyHint("enter", "select"), keyHint("esc", "back"),
		}, mutedStyle.Render("  ·  ")), w-2))
	return lines
}

func (a *App) renderSetupModel(w, h int) []string {
	c := a.choiceFor(a.setup.stage)
	list := a.setupModelList(c.Agent)
	lines := []string{a.setupTitle("model") + mutedStyle.Render("  ·  " + c.Agent), ""}

	rows := min(max(h-10, 3), 16)
	filter := string(a.setup.filter)
	if filter == "" {
		filter = faintStyle.Render("type to filter")
	} else {
		filter = goldStyle.Render(filter) + goldStyle.Render("▏")
	}
	lines = append(lines, " "+mutedStyle.Render("filter ")+filter, "")

	if a.catalog == nil {
		lines = append(lines, " "+faintStyle.Render("discovering models…"))
	} else if len(list) == 0 {
		lines = append(lines, " "+faintStyle.Render("no models match; backspace to clear the filter"))
	} else {
		top := a.setup.top
		if a.setup.cursor < top {
			top = a.setup.cursor
		}
		if a.setup.cursor >= top+rows {
			top = a.setup.cursor - rows + 1
		}
		a.setup.top = min(max(top, 0), max(0, len(list)-rows))
		for i := a.setup.top; i < len(list) && i < a.setup.top+rows; i++ {
			mark := "  "
			style := textStyle
			if i == a.setup.cursor {
				mark = goldStyle.Render("❯ ")
				style = goldStyle.Bold(true)
			} else if list[i].ID == c.Model {
				style = mutedStyle
			}
			lines = append(lines, " "+mark+style.Render(truncate(list[i].ID, w-4)))
		}
		total := 0
		if info := a.catalog.Agent(c.Agent); info != nil {
			total = len(info.Models)
		}
		lines = append(lines, " "+faintStyle.Render(fmt.Sprintf("%d of %d models", len(list), total)))
	}
	lines = append(lines, "",
		truncate(" "+strings.Join([]string{
			keyHint("type", "filter"), keyHint("↑↓", "choose"), keyHint("pgup/pgdn", "page"),
			keyHint("enter", "select"), keyHint("esc", "back"),
		}, mutedStyle.Render("  ·  ")), w-2))
	return lines
}

func (a *App) renderSetupEffort(w int) []string {
	c := a.choiceFor(a.setup.stage)
	mi := a.catalog.Find(c.Agent, c.Model)
	lines := []string{a.setupTitle("effort") + mutedStyle.Render("  ·  " + c.Model), ""}

	options := a.setupOptions()
	for i, opt := range options {
		mark := "  "
		style := textStyle
		if i == a.setup.cursor {
			mark = goldStyle.Render("❯ ")
			style = goldStyle.Bold(true)
		}
		label := "model default"
		if i > 0 {
			label = opt
			if mi != nil && opt == mi.DefaultEffort {
				label += " (model default)"
			}
		}
		if i > 0 && opt == c.Variant {
			label += " (current)"
		}
		lines = append(lines, " "+mark+style.Render(truncate(label, w-4)))
	}
	lines = append(lines, "",
		truncate(" "+strings.Join([]string{
			keyHint("↑↓", "choose"), keyHint("enter", "select"), keyHint("esc", "back"),
		}, mutedStyle.Render("  ·  ")), w-2))
	return lines
}
