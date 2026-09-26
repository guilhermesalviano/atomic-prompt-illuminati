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
	koalaW   = 18 // sprite width in cells
	koalaGap = 2  // cells between the sprite and its bubble; holds the z's
	koalaH   = 10 // sprite height in rows
)

// koalaSprite is the mascot as pixel art: a koala hugging a eucalyptus
// trunk. One character is one pixel and two pixel rows share a terminal row
// through half blocks, so pixels come out roughly square. The eye is always
// 'E', plus 'c' when closed (a dash) or 'o' when open (a tall dot).
var koalaSprite = [2 * koalaH]string{
	"....KTLTK.........",
	"....KTLBK....KKKK.",
	"....KBLTK...KGWWGK",
	"KK..KTLTKKKKKWWWGK",
	"KTK.KTBTKGGGKWWGGK",
	"KTTKKTLTKGGGGKGGGK",
	".KTTTTLTKKKGGGKKK.",
	"...KKTLTKNNGGoGGK.",
	"....KTBTKNNGGEcGK.",
	"..KKKTLTKNNGGGGGK.",
	".KGGGGGTKWWWGGGGK.",
	".KGKGKKKKKKKKKGGGK",
	"...KGGGGGGGGGGGGGK",
	"...KGKGKGGGGGGGGGK",
	"...KKKKKKKKKKKGGGK",
	"....KTLTKDGGGGGGGK",
	"....KTBTKKDGGGGGGK",
	"....KBLKGGKDGGGGGK",
	"....KTKGKGKDGGGGK.",
	"....KTKKKKKKKKKK..",
}

// koalaPalette colors the sprite's pixels; anything missing is transparent.
var koalaPalette = map[byte]lipgloss.Color{
	'K': "#1A1A1A", // outline
	'G': "#A8A8AA", // fur
	'D': "#808084", // fur in shadow
	'W': "#F2F2F2", // inner ear and chin
	'N': "#6E3A1E", // nose
	'T': "#A0623A", // bark
	'L': "#BA7C4C", // bark highlight
	'B': "#7A4526", // bark groove
}

// moodColor is the accent for the sparkle and bubble: the stage's role color.
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
	// Napping, content or sad koalas keep their eyes shut, like the sleepy
	// original; the others are awake and blink now and then.
	closed := m == moodSleep || m == moodDone || m == moodFail || f%32 >= 30

	var side []string
	width := koalaW + koalaGap
	if bubbleW > 0 {
		side = koalaBubble(m, f, caption, bubbleW, acc)
		width += lipgloss.Width(side[0])
	}
	out := make([]string, koalaH)
	for i := range out {
		r := koalaRow(koalaSprite[2*i], koalaSprite[2*i+1], closed)
		if s := koalaSparkle(m, f, i); s != "" {
			r += acc.Render(s)
		}
		if i-1 >= 0 && i-1 < len(side) {
			r += strings.Repeat(" ", max(0, koalaW+koalaGap-lipgloss.Width(r))) + side[i-1]
		}
		out[i] = r + strings.Repeat(" ", max(0, width-lipgloss.Width(r)))
	}
	return out
}

// koalaRow folds two pixel rows into one terminal row: the upper pixel is a
// half block's foreground and the lower one its background.
func koalaRow(top, bot string, closed bool) string {
	var b strings.Builder
	for x := 0; x < koalaW; x++ {
		t, tok := koalaPixel(top[x], closed)
		u, uok := koalaPixel(bot[x], closed)
		st := lipgloss.NewStyle()
		switch {
		case !tok && !uok:
			b.WriteByte(' ')
		case !tok:
			b.WriteString(st.Foreground(u).Render("▄"))
		case !uok:
			b.WriteString(st.Foreground(t).Render("▀"))
		case t == u:
			b.WriteString(st.Foreground(t).Render("█"))
		default:
			b.WriteString(st.Foreground(t).Background(u).Render("▀"))
		}
	}
	return b.String()
}

// koalaPixel resolves a sprite pixel, including the mood-dependent eye.
func koalaPixel(c byte, closed bool) (lipgloss.Color, bool) {
	switch c {
	case 'E':
		c = 'K'
	case 'c', 'o':
		if closed == (c == 'c') {
			c = 'K'
		} else {
			c = 'G'
		}
	}
	col, ok := koalaPalette[c]
	return col, ok
}

// koalaSparkle decorates row i to the right of the sprite (z's drifting up
// from the ear, stars). It must fit in koalaGap so bubble-less rows stay
// koalaW+koalaGap wide.
func koalaSparkle(m mood, f, i int) string {
	var frames []string
	switch m {
	case moodSleep:
		frames = []string{"z", " z", " Z", ""} // row 2, 1, 0, then a pause
	case moodDone:
		frames = []string{"✧", " ✦", "", ""}
	default:
		return ""
	}
	p := f / 6 % 4
	if i != 2-p {
		return ""
	}
	return frames[p]
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
