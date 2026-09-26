package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// mood is what the koala mascot is doing; it mirrors a run's state.
type mood int

const (
	moodSleep  mood = iota // nothing running
	moodPlan               // planner running
	moodExec               // executor running
	moodReview             // reviewer running
	moodGate               // waiting on the human
	moodDone               // run finished
	moodFail               // run failed or aborted
)

const (
	koalaW   = 17 // sprite width in cells
	koalaGap = 4  // cells between the sprite and its bubble
	koalaH   = 8  // sprite height in rows, including the bob gap
)

var (
	cFur  = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	cNose = lipgloss.AdaptiveColor{Light: "#111827", Dark: "#4B5563"}

	furStyle  = lipgloss.NewStyle().Foreground(cFur)
	noseStyle = lipgloss.NewStyle().Foreground(cNose)
)

// moodColor is the accent for eyes, prop and bubble: the stage's role color.
func moodColor(m mood) lipgloss.AdaptiveColor {
	switch m {
	case moodPlan:
		return cViolet
	case moodExec:
		return cCyan
	case moodReview:
		return cGold
	case moodGate:
		return cAmber
	case moodDone:
		return cGreen
	case moodFail:
		return cRed
	}
	return cMuted
}

// koala renders the mascot for mood m at animation frame f (one per tick).
// caption, when set, replaces the mood's default bubble text. The bubble is
// bubbleW cells wide and omitted when bubbleW <= 0. Every row has the same
// width so the block can be centered line by line.
func koala(m mood, f int, caption string, bubbleW int) []string {
	acc := lipgloss.NewStyle().Foreground(moodColor(m))

	// Eyes: 7 cells between the cheeks. Pupils drift for "looking around".
	eyes := func(glyph string, look int) string {
		return strings.Repeat(" ", look) + glyph + "   " + glyph + strings.Repeat(" ", 2-look)
	}
	blink := f%32 >= 30
	var eye, mouth string
	switch m {
	case moodSleep:
		eye, mouth = eyes("-", 1), " ~ "
	case moodPlan:
		eye, mouth = eyes("o", []int{0, 1, 2, 1}[f/8%4]), " o "
	case moodExec:
		eye, mouth = eyes("•", 1), []string{" - ", " w "}[f/3%2]
	case moodReview:
		eye, mouth = "[o]-[o]", " - " // reading glasses
		if f/6%2 == 1 {
			eye = "[•]-[•]"
		}
	case moodGate:
		eye, mouth = eyes("O", 1), " o "
	case moodDone:
		eye, mouth = eyes("^", 1), "\\_/"
		blink = false
	case moodFail:
		eye, mouth = eyes("╥", 1), "/-\\"
		blink = false
	}
	if blink && m != moodSleep && m != moodReview {
		eye = eyes("-", 1)
	}

	paw := "   "
	if m == moodGate {
		paw = []string{" \\o", " -o"}[f/3%2]
	}

	rows := []string{
		furStyle.Render(" ,--.       ,--. ") + acc.Render(koalaSparkle(m, f)),
		furStyle.Render("( (  `-----'  ) )") + acc.Bold(true).Render(paw),
		furStyle.Render(" `-/ ") + acc.Bold(true).Render(eye) + furStyle.Render(" \\-' "),
		furStyle.Render("   |   ") + noseStyle.Render("▄█▄") + furStyle.Render("   |   "),
		furStyle.Render("   \\   ") + textStyle.Render(mouth) + furStyle.Render("   /   "),
		furStyle.Render("    `-.___.-'    "),
		koalaProp(m, f, acc),
	}

	// Bob the head while typing and bounce when done.
	if (m == moodExec && f/3%2 == 0) || (m == moodDone && f/4%2 == 0) {
		rows = append([]string{""}, rows...)
	} else {
		rows = append(rows, "")
	}

	var side []string
	width := koalaW + koalaGap
	if bubbleW > 0 {
		side = koalaBubble(m, f, caption, bubbleW, acc)
		width += lipgloss.Width(side[0])
	}
	out := make([]string, koalaH)
	for i := range out {
		r := rows[i]
		if i-1 >= 0 && i-1 < len(side) {
			r += strings.Repeat(" ", max(0, koalaW+koalaGap-lipgloss.Width(r))) + side[i-1]
		}
		out[i] = r + strings.Repeat(" ", max(0, width-lipgloss.Width(r)))
	}
	return out
}

// koalaProp is the row under the chin: what the koala is holding.
func koalaProp(m mood, f int, acc lipgloss.Style) string {
	switch m {
	case moodSleep:
		return greenStyle.Render("  ══════❦════════ ")
	case moodPlan:
		n := f / 4 % 8
		return acc.Render("   ✎ " + strings.Repeat("⋯", n) + strings.Repeat(" ", 8-n) + "    ")
	case moodExec:
		keys := []rune("▫▫▫▫▫▫▫▫▫")
		keys[(f*7+f/2)%len(keys)] = '▪'
		keys[(f*3+4)%len(keys)] = '▪'
		return acc.Render("   [" + string(keys) + "]   ")
	case moodReview:
		doc := []rune("≡≡≡≡≡≡≡≡≡")
		doc[f/2%len(doc)] = '⌕'
		return acc.Render("   ▕" + string(doc) + "▏   ")
	case moodGate:
		return acc.Bold(true).Render("   ◆ ◆ ◆ ◆ ◆ ◆   ")
	case moodDone:
		return acc.Render([]string{"   ✦  ·  ✧  ·  ✦ ", "   ·  ✧  ✦  ✧  · "}[f/5%2])
	case moodFail:
		return mutedStyle.Render([]string{"      '    '     ", "     .      .    "}[f/4%2])
	}
	return ""
}

// koalaSparkle decorates the top-right corner (floating z's, stars). It must
// fit in koalaGap so bubble-less rows stay koalaW+koalaGap wide.
func koalaSparkle(m mood, f int) string {
	switch m {
	case moodSleep:
		return []string{" z", "  Z", "   z", ""}[f/6%4]
	case moodDone:
		return []string{" ✧", "  ✦", ""}[f/5%3]
	}
	return ""
}

// koalaBubble is the 3-row speech bubble to the koala's right.
func koalaBubble(m mood, f int, caption string, w int, acc lipgloss.Style) []string {
	dots := strings.Repeat(".", f/4%4)
	text := map[mood]string{
		moodSleep:  "zzz… waiting for a prompt",
		moodPlan:   "hmm, drafting a plan" + dots,
		moodExec:   "tap tap tap" + dots,
		moodReview: "reading the diff" + dots,
		moodGate:   "your turn!",
		moodDone:   "shipped it ✔",
		moodFail:   "oh no… it broke",
	}[m]
	if caption != "" && m != moodSleep {
		text = caption
	}
	inner := max(8, w-4)
	text = ansi.Truncate(text, inner, "…")
	text += strings.Repeat(" ", inner-lipgloss.Width(text))
	return []string{
		faintStyle.Render("╭" + strings.Repeat("─", inner+2) + "╮"),
		faintStyle.Render("┤ ") + acc.Render(text) + faintStyle.Render(" │"),
		faintStyle.Render("╰" + strings.Repeat("─", inner+2) + "╯"),
	}
}
