package render

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
)

var t0 = time.Date(2026, 6, 26, 9, 0, 0, 0, time.UTC)

// ev builds an event nbSecs after t0 so order is deterministic.
func ev(id string, sec int, action types.Action, task, name, link string) types.Event {
	e := types.Event{
		ID:        id,
		Timestamp: t0.Add(time.Duration(sec) * time.Second),
		Action:    action,
		TaskID:    task,
		Name:      name,
	}
	if link != "" {
		e.LinkTaskID = link
		e.Rel = types.RelForAction(action)
	}
	return e
}

// sampleEvents models: task A started, spawned B (originated_from), B got
// blocked by C, C completed, then A completed.
func sampleEvents() []types.Event {
	return []types.Event{
		ev("e1", 0, types.ActionStart, "A", "build auth", ""),
		ev("e2", 1, types.ActionStart, "B", "oauth spike", "A"),
		ev("e3", 2, types.ActionStart, "C", "config fix", ""),
		ev("e4", 3, types.ActionBlock, "B", "", "C"),
		ev("e5", 4, types.ActionCheckpoint, "A", "", ""),
		ev("e6", 5, types.ActionComplete, "C", "", ""),
		ev("e7", 6, types.ActionResume, "B", "", ""),
		ev("e8", 7, types.ActionComplete, "A", "", ""),
	}
}

func TestBuildGraph_LanesAndForks(t *testing.T) {
	g := BuildGraph(sampleEvents())

	// trunk + A, B, C
	if got := len(g.Lanes); got != 4 {
		t.Fatalf("lanes = %d, want 4", got)
	}
	if g.Lanes[0].ID != trunkID {
		t.Fatalf("first lane = %q, want trunk", g.Lanes[0].ID)
	}

	// B originated from A, so B forks off A's lane (not the trunk).
	b := g.Lane("B")
	if b == nil || b.ParentID != "A" {
		t.Fatalf("B.ParentID = %v, want A", b)
	}
	// A has no origin, so it forks off the trunk.
	if a := g.Lane("A"); a == nil || a.ParentID != trunkID {
		t.Fatalf("A.ParentID = %v, want trunk", a)
	}

	// Nodes are in chronological order with contiguous seqs.
	if len(g.Nodes) != len(sampleEvents()) {
		t.Fatalf("nodes = %d, want %d", len(g.Nodes), len(sampleEvents()))
	}
	for i, n := range g.Nodes {
		if n.Seq != i {
			t.Fatalf("node[%d].Seq = %d", i, n.Seq)
		}
	}
}

func TestBuildGraph_BlockEdgeRecorded(t *testing.T) {
	g := BuildGraph(sampleEvents())
	var found bool
	for _, n := range g.Nodes {
		if n.Action == types.ActionBlock {
			found = true
			if n.BlockerID != "C" {
				t.Fatalf("block BlockerID = %q, want C", n.BlockerID)
			}
			if !n.Reverse || n.Tag != "blocked" {
				t.Fatalf("block node not annotated: %+v", n)
			}
		}
	}
	if !found {
		t.Fatal("no block node in graph")
	}
}

func TestMermaid_Validity(t *testing.T) {
	out := Mermaid(BuildGraph(sampleEvents()))

	// The mainBranchName directive must come first so `checkout "journal"`
	// resolves to the trunk instead of failing as an uncreated branch.
	if !strings.HasPrefix(out, "%%{init:") || !strings.Contains(out, "'mainBranchName': \"journal\"") {
		t.Fatalf("missing mainBranchName directive:\n%s", out)
	}
	if !strings.Contains(out, "\ngitGraph\n") {
		t.Fatalf("missing gitGraph header:\n%s", out)
	}
	// Trunk must be seeded with a commit before any branch.
	firstBranch := strings.Index(out, "branch ")
	firstCommit := strings.Index(out, "commit ")
	if firstCommit < 0 || (firstBranch >= 0 && firstCommit > firstBranch) {
		t.Fatalf("trunk not seeded before first branch:\n%s", out)
	}

	// Every branch/checkout target must be a branch that was created earlier in
	// the stream (Mermaid rejects forward references). The trunk counts as
	// pre-created.
	created := map[string]bool{quote(TrunkName): true}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "branch "):
			created[strings.TrimPrefix(line, "branch ")] = true
		case strings.HasPrefix(line, "checkout "):
			target := strings.TrimPrefix(line, "checkout ")
			if !created[target] {
				t.Fatalf("checkout of uncreated branch %s:\n%s", target, out)
			}
		}
	}

	// Completion and block render as their Mermaid commit types.
	if !strings.Contains(out, "type: HIGHLIGHT") {
		t.Errorf("expected a HIGHLIGHT (complete) commit:\n%s", out)
	}
	if !strings.Contains(out, "type: REVERSE") {
		t.Errorf("expected a REVERSE (block) commit:\n%s", out)
	}
}

func TestSVG_WellFormed(t *testing.T) {
	out := SVG(BuildGraph(sampleEvents()), SVGOptions{})
	if !strings.HasPrefix(out, "<svg") || !strings.Contains(out, "</svg>") {
		t.Fatalf("not an svg document:\n%s", out[:min(200, len(out))])
	}
	if o, c := strings.Count(out, "<svg"), strings.Count(out, "</svg>"); o != c {
		t.Fatalf("unbalanced svg tags: %d open, %d close", o, c)
	}
	// Task names appear as gutter labels.
	if !strings.Contains(out, "oauth spike") {
		t.Errorf("expected gutter label for task B")
	}
}

func TestSVG_RowPerEventGeometry(t *testing.T) {
	events := sampleEvents()
	out := SVG(BuildGraph(events), SVGOptions{})
	// One row per event: viewBox height = header + rows*rowH + footer.
	wantH := headerH + len(events)*rowH + footerH
	if !strings.Contains(out, fmt.Sprintf("viewBox=\"0 0 %d %d\"", svgWidthMin, wantH)) &&
		!strings.Contains(out, fmt.Sprintf(" %d\" role=", wantH)) {
		t.Errorf("expected viewBox height %d for %d events:\n%s", wantH, len(events), firstLines(out, 3))
	}
}

func TestSVG_ChipsVsDotsAndBezier(t *testing.T) {
	out := SVG(BuildGraph(sampleEvents()), SVGOptions{})
	// Lifecycle events (checkpoint/complete/block) render as chips (rounded rects).
	if !strings.Contains(out, `rx="3"`) {
		t.Errorf("expected lifecycle chips (rounded rects)")
	}
	// Chip glyphs are present.
	for _, g := range []string{"✓", "⊘"} {
		if !strings.Contains(out, g) {
			t.Errorf("expected chip glyph %q", g)
		}
	}
	// Ordinary events render as node dots (circles).
	if !strings.Contains(out, "<circle") {
		t.Errorf("expected node dots (circles)")
	}
	// A branch-off (B from A) draws a bezier join.
	if !strings.Contains(out, `<path d="M `) || !strings.Contains(out, " C ") {
		t.Errorf("expected a bezier branch-off path")
	}
}

func TestSVG_ActiveFocusBand(t *testing.T) {
	g := BuildGraph(sampleEvents())
	// Force a focus and confirm a full-width band rect is emitted at its row.
	g.FocusTaskID = "A"
	out := SVG(g, SVGOptions{})
	if !strings.Contains(out, `<rect x="0" y=`) {
		t.Errorf("expected a full-width active-focus band rect:\n%s", out)
	}
	// With no focus, no band.
	g.FocusTaskID = ""
	if strings.Contains(SVG(g, SVGOptions{}), `<rect x="0" y=`) {
		t.Errorf("did not expect a band when nothing is in focus")
	}
}

func TestSVG_Message(t *testing.T) {
	events := []types.Event{
		ev("e1", 0, types.ActionStart, "A", "build auth", ""),
	}
	events[0].Message = "kick off & <scope>"
	out := SVG(BuildGraph(events), SVGOptions{})
	// Message renders as "{action} — {message}" with XML escaping.
	if !strings.Contains(out, "start — kick off &amp; &lt;scope&gt;") {
		t.Errorf("expected escaped message line:\n%s", out)
	}
}

func TestSVG_LongMessageWrapsAndGrowsRow(t *testing.T) {
	long := "This is a deliberately long checkpoint note that should wrap across " +
		"several lines instead of being truncated, so the whole message stays " +
		"readable in the rendered graph without any information being lost."
	events := []types.Event{ev("e1", 0, types.ActionCheckpoint, "A", "task", "")}
	events[0].Message = long
	out := SVG(BuildGraph(events), SVGOptions{})

	// A one-row graph with a single-line message would be exactly this tall.
	oneLineH := headerH + rowHeight(1) + footerH
	// The wrapped message must produce more than one line, growing the SVG.
	msgTexts := strings.Count(out, `font-size="12"`)
	if msgTexts < 2 {
		t.Errorf("expected the long message to wrap into multiple lines, got %d:\n%s", msgTexts, out)
	}
	// No ellipsis: the full text is present (spot-check the tail word).
	if strings.Contains(out, "…") || !strings.Contains(out, "lost.") {
		t.Errorf("message appears truncated; expected full text ending in 'lost.'")
	}
	// The taller row makes the document taller than the single-line case.
	if !taller(out, oneLineH) {
		t.Errorf("expected viewBox height > %d for a wrapped message", oneLineH)
	}
}

// taller reports whether the SVG's viewBox height exceeds h.
func taller(svg string, h int) bool {
	var w, got int
	if _, err := fmt.Sscanf(svg, `<svg xmlns="http://www.w3.org/2000/svg" width="100%%" viewBox="0 0 %d %d"`, &w, &got); err != nil {
		return false
	}
	return got > h
}

func TestBuildGraph_Empty(t *testing.T) {
	g := BuildGraph(nil)
	if len(g.Lanes) != 1 || g.Lanes[0].ID != trunkID {
		t.Fatalf("empty graph should have only the trunk, got %d lanes", len(g.Lanes))
	}
	// Renderers must not panic on an empty graph.
	if !strings.Contains(Mermaid(g), "\ngitGraph\n") {
		t.Fatal("Mermaid on empty graph malformed")
	}
	if !strings.Contains(SVG(g, SVGOptions{}), "</svg>") {
		t.Fatal("SVG on empty graph malformed")
	}
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
