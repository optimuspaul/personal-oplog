package render

import (
	"fmt"
	"strings"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
)

// This file renders a Graph as a GitKraken-style commit graph: one row per
// event, a fixed left gutter of task labels, thin colored lane rails, bezier
// branch-off joins, node dots for ordinary events and tag chips for lifecycle
// events, a full-width band on the active-focus row, and a message column.
//
// See visulaizer.md for the design. Colour encodes *which task* (a stable
// palette by first appearance), exactly like a branch colour in git — status is
// conveyed by the chip glyph, not by colour.

// Layout constants (px). See visulaizer.md §6.
const (
	svgWidthMin = 680
	gutterW     = 130
	laneGap     = 26
	headerH     = 36
	rowH        = 30 // minimum row height (a single-line message)
	lineH       = 16 // height of one wrapped message line
	rowPadV     = 7  // vertical padding above+below the message block
	footerH     = 50
	nodeR       = 5 // start/resume/restart node radius
	noteR       = 4 // note node radius
	chipW       = 20
	chipH       = 14
	msgGap      = 24  // space between the last lane and the message column
	msgMinX     = 250 // message column never starts left of this
	msgW        = 430 // message column width
	railW       = 2
)

// ramp is a task's colour ramp; colour encodes identity, not status.
type ramp struct {
	rail  string // mid stop — rail + dot fill
	ring  string // light stop — dot halo ring
	chip  string // dark stop — chip background
	glyph string // lightest stop — chip glyph + ring on dark chrome
}

// palette is assigned to tasks in order of first appearance and is stable for
// the life of the journal. gray (last) doubles as the overflow colour.
var palette = []ramp{
	{rail: "#534AB7", ring: "#7F77DD", chip: "#26215C", glyph: "#CECBF6"}, // purple
	{rail: "#0F6E56", ring: "#2BA583", chip: "#0A3D30", glyph: "#BFE9DC"}, // teal
	{rail: "#993C1D", ring: "#D2683F", chip: "#4D1E0E", glyph: "#F3CDBD"}, // coral
	{rail: "#A83A6E", ring: "#D96B9B", chip: "#4F1B33", glyph: "#F4C9DD"}, // pink
	{rail: "#2C5AA8", ring: "#5B8AD9", chip: "#16294D", glyph: "#C4D6F2"}, // blue
	{rail: "#3F7A33", ring: "#6FB05F", chip: "#1C3717", glyph: "#CDE8C4"}, // green
	{rail: "#9A6B12", ring: "#CE9A33", chip: "#4A330A", glyph: "#F2E0BF"}, // amber
	{rail: "#6E6C66", ring: "#989690", chip: "#34332F", glyph: "#D8D6CE"}, // gray
}

// chrome holds the theme-dependent colours that are not per-task.
type chrome struct {
	bg          string
	headerText  string
	messageText string
	divider     string
	bandOpacity string
}

func themeChrome(theme string) chrome {
	if theme == "light" {
		return chrome{
			bg:          "#FFFFFF",
			headerText:  "#898781",
			messageText: "#3D3D3A",
			divider:     "#E1E0D9",
			bandOpacity: "0.12",
		}
	}
	return chrome{
		bg:          "#1B1B19",
		headerText:  "#898781",
		messageText: "#C2C0B6",
		divider:     "#2C2C2A",
		bandOpacity: "0.22",
	}
}

// SVGOptions tunes the render. The zero value is valid: dark theme, focus taken
// from the Graph, width auto-sized to the lanes.
type SVGOptions struct {
	// ActiveTaskID overrides the highlighted task; empty uses Graph.FocusTaskID.
	ActiveTaskID string
	// Theme is "dark" (default) or "light"; affects chrome only, not lanes.
	Theme string
}

// SVG renders the graph as a self-contained GitKraken-style SVG document.
func SVG(g *Graph, opts SVGOptions) string {
	ch := themeChrome(opts.Theme)
	active := opts.ActiveTaskID
	if active == "" {
		active = g.FocusTaskID
	}

	// Task lanes are every lane but the synthetic trunk; lane number is the
	// task's first-appearance order (0-based), which also picks its colour.
	laneNum := map[string]int{}
	maxLane := -1
	for _, l := range g.Lanes {
		if l.ID == trunkID {
			continue
		}
		n := l.Index - 1 // trunk is Index 0
		laneNum[l.ID] = n
		if n > maxLane {
			maxLane = n
		}
	}

	laneX := func(n int) int { return gutterW + n*laneGap }
	msgX := msgMinX
	if maxLane >= 0 {
		msgX = max(msgX, laneX(maxLane)+msgGap)
	}
	width := max(msgX+msgW, svgWidthMin)
	rows := len(g.Nodes)

	// Rows have variable height: a message is wrapped to the column width and its
	// row grows to fit every line, so nothing is truncated. Precompute the
	// wrapped lines and the cumulative top/center of each row.
	msgWrapW := width - msgX - 12
	msgLines := make([][]string, rows)
	rowTop := make([]int, rows+1)
	rowCenter := make([]int, rows)
	cursorY := headerH
	for i, n := range g.Nodes {
		msgLines[i] = wrapText(messageText(n), msgWrapW, 12)
		h := rowHeight(len(msgLines[i]))
		rowTop[i] = cursorY
		rowCenter[i] = cursorY + h/2
		cursorY += h
	}
	rowTop[rows] = cursorY
	height := cursorY + footerH

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="100%%" viewBox="0 0 %d %d" role="img" font-family="-apple-system, ui-sans-serif, system-ui, sans-serif">`+"\n",
		width, height)
	fmt.Fprintf(&b, "<title>oplog work graph</title>\n<desc>%d events across %d tasks</desc>\n", rows, maxLane+1)
	if ch.bg != "" {
		fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="%s"/>`+"\n", width, height, ch.bg)
	}

	// Active-focus band (drawn first so everything sits on top).
	if lane := g.laneBy[active]; lane != nil && lane.LastSeq >= 0 {
		col := palette[laneNum[lane.ID]%len(palette)]
		seq := lane.LastSeq
		fmt.Fprintf(&b, `<rect x="0" y="%d" width="%d" height="%d" fill="%s" opacity="%s"/>`+"\n",
			rowTop[seq], width, rowTop[seq+1]-rowTop[seq], col.rail, ch.bandOpacity)
	}

	// Header + row dividers.
	fmt.Fprintf(&b, `<text x="14" y="%d" font-size="11" font-weight="600" fill="%s">GRAPH</text>`+"\n", headerH-12, ch.headerText)
	fmt.Fprintf(&b, `<text x="%d" y="%d" font-size="11" font-weight="600" fill="%s">EVENT</text>`+"\n", msgX, headerH-12, ch.headerText)
	fmt.Fprintf(&b, `<line x1="0" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="1"/>`+"\n", headerH, width, headerH, ch.divider)
	for i := 1; i < rows; i++ {
		fmt.Fprintf(&b, `<line x1="0" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="1" opacity="0.5"/>`+"\n", rowTop[i], width, rowTop[i], ch.divider)
	}

	// Rails + bezier branch-offs (before nodes so dots sit on top).
	for _, lane := range g.Lanes {
		if lane.ID == trunkID || lane.FirstSeq < 0 {
			continue
		}
		col := palette[laneNum[lane.ID]%len(palette)]
		cx := laneX(laneNum[lane.ID])
		yTop, yBot := rowCenter[lane.FirstSeq], rowCenter[lane.LastSeq]

		if lane.ParentID != trunkID {
			// Branch-off: bezier from the parent rail (at the top edge of the start
			// row) into this lane's node, with control points at the vertical mid.
			if p, ok := laneNum[lane.ParentID]; ok {
				px := laneX(p)
				yFork := rowTop[lane.FirstSeq]
				yCtrl := (yFork + yTop) / 2
				fmt.Fprintf(&b, `<path d="M %d %d C %d %d, %d %d, %d %d" fill="none" stroke="%s" stroke-width="%d"/>`+"\n",
					px, yFork, px, yCtrl, cx, yCtrl, cx, yTop, col.rail, railW)
			}
		}
		if yBot > yTop {
			fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="%d"/>`+"\n",
				cx, yTop, cx, yBot, col.rail, railW)
		}
	}

	// Nodes / chips.
	for _, n := range g.Nodes {
		lane := g.laneBy[n.LaneID]
		col := palette[laneNum[lane.ID]%len(palette)]
		cx, cy := laneX(laneNum[lane.ID]), rowCenter[n.Seq]
		if glyph, isChip := chipGlyph(n.Action); isChip {
			drawChip(&b, cx, cy, glyph, col)
		} else {
			drawDot(&b, cx, cy, n.Action, col)
		}
	}

	// Gutter labels + messages.
	seen := map[string]bool{}
	for i, n := range g.Nodes {
		lane := g.laneBy[n.LaneID]
		col := palette[laneNum[lane.ID]%len(palette)]

		// Gutter label: on a task's first row, and when it re-enters via resume/restart.
		if !seen[lane.ID] || n.Action == types.ActionResume || n.Action == types.ActionRestart {
			label := truncate(laneLabelText(lane), gutterW-24, 11)
			fmt.Fprintf(&b, `<text x="14" y="%d" font-size="11" font-weight="600" fill="%s">%s</text>`+"\n",
				rowCenter[n.Seq]+4, col.rail, escapeXML(label))
		}
		seen[lane.ID] = true

		// Message: wrapped lines, centered vertically within the row.
		lines := msgLines[i]
		startY := rowCenter[n.Seq] - (len(lines)-1)*lineH/2 + 4
		for k, line := range lines {
			fmt.Fprintf(&b, `<text x="%d" y="%d" font-size="12" fill="%s">%s</text>`+"\n",
				msgX, startY+k*lineH, ch.messageText, escapeXML(line))
		}
	}

	// Footer legend.
	drawLegend(&b, 14, height-footerH+28, ch.headerText)
	b.WriteString("</svg>\n")
	return b.String()
}

// chipGlyph reports whether an action renders as a chip and, if so, its glyph.
// Lifecycle events are chips; ordinary events (start/resume/restart/note) are dots.
func chipGlyph(a types.Action) (string, bool) {
	switch a {
	case types.ActionCheckpoint, types.ActionComplete:
		return "✓", true
	case types.ActionPark:
		return "‖", true
	case types.ActionBlock:
		return "⊘", true
	default:
		return "", false
	}
}

func drawDot(b *strings.Builder, cx, cy int, action types.Action, col ramp) {
	r := nodeR
	if action == types.ActionNote {
		r = noteR
	}
	// Halo ring then filled dot, echoing the screenshot's avatar rings.
	fmt.Fprintf(b, `<circle cx="%d" cy="%d" r="%d" fill="none" stroke="%s" stroke-width="2"/>`+"\n",
		cx, cy, r+2, col.ring)
	fmt.Fprintf(b, `<circle cx="%d" cy="%d" r="%d" fill="%s"/>`+"\n", cx, cy, r, col.rail)
}

func drawChip(b *strings.Builder, cx, cy int, glyph string, col ramp) {
	x := cx - chipW/2
	yy := cy - chipH/2
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="%d" height="%d" rx="3" fill="%s" stroke="%s" stroke-width="1"/>`+"\n",
		x, yy, chipW, chipH, col.chip, col.rail)
	fmt.Fprintf(b, `<text x="%d" y="%d" text-anchor="middle" font-size="10" fill="%s">%s</text>`+"\n",
		cx, cy+3, col.glyph, escapeXML(glyph))
}

func laneLabelText(lane *Lane) string {
	name := strings.TrimSpace(lane.Name)
	if name == "" {
		name = shortID(lane.ID)
	}
	return name
}

// messageText is the right-column line: "{action} — {message}", or just the
// action verb when the event has no message.
func messageText(n Node) string {
	verb := actionVerb(n.Action)
	if msg := strings.TrimSpace(n.Message); msg != "" {
		return fmt.Sprintf("%s — %s", verb, msg)
	}
	return verb
}

func drawLegend(b *strings.Builder, x, y int, color string) {
	items := []string{"● event", "✓ checkpoint / complete", "‖ parked", "⊘ blocked"}
	cx := x
	for _, it := range items {
		fmt.Fprintf(b, `<text x="%d" y="%d" font-size="11" fill="%s">%s</text>`+"\n", cx, y, color, escapeXML(it))
		cx += len([]rune(it))*7 + 18
	}
}

// rowHeight is the height of a row whose message wrapped to nLines lines: a
// single line keeps the compact base height, extra lines grow the row.
func rowHeight(nLines int) int {
	return max(rowPadV*2+nLines*lineH, rowH)
}

// charsPerPx is the inverse of the estimated glyph width (~0.58em) used to fit
// text to a pixel width without a real text-measurement pass.
func maxCharsFor(maxPx, fontPx int) int {
	return max(int(float64(maxPx)/(float64(fontPx)*0.58)), 1)
}

// wrapText greedily wraps s to lines that fit maxPx at fontPx, breaking on
// spaces and hard-breaking any single word longer than the line. It always
// returns at least one line so every row has a stable height.
func wrapText(s string, maxPx, fontPx int) []string {
	maxChars := maxCharsFor(maxPx, fontPx)
	var lines []string
	cur := ""
	flush := func() {
		if cur != "" {
			lines = append(lines, cur)
			cur = ""
		}
	}
	for _, w := range strings.Fields(s) {
		// Hard-break a word that can't fit on any line.
		for len([]rune(w)) > maxChars {
			flush()
			r := []rune(w)
			lines = append(lines, string(r[:maxChars]))
			w = string(r[maxChars:])
		}
		switch {
		case cur == "":
			cur = w
		case len([]rune(cur))+1+len([]rune(w)) <= maxChars:
			cur += " " + w
		default:
			flush()
			cur = w
		}
	}
	flush()
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// truncate shortens s with an ellipsis to fit maxPx at the given font size,
// estimating width at ~0.58em per character (good enough for sans-serif).
func truncate(s string, maxPx, fontPx int) string {
	r := []rune(s)
	maxChars := maxCharsFor(maxPx, fontPx)
	if len(r) <= maxChars {
		return s
	}
	if maxChars <= 1 {
		return "…"
	}
	return string(r[:maxChars-1]) + "…"
}

func escapeXML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}
