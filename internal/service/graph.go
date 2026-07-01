package service

import (
	"context"
	"fmt"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	"github.com/optimuspaul/personal-oplog/internal/projection"
	"github.com/optimuspaul/personal-oplog/internal/render"
)

// GraphFormat selects how Graph renders the diagram.
type GraphFormat string

const (
	// FormatMermaid returns Mermaid gitGraph source text.
	FormatMermaid GraphFormat = "mermaid"
	// FormatSVG returns a self-contained SVG document.
	FormatSVG GraphFormat = "svg"
)

// GraphInput selects what to draw. Task, when set (a ULID or fuzzy name),
// scopes the diagram to that task's lineage — every task reachable from it
// through originated-from and block links, in either direction — instead of the
// whole journal. Format defaults to Mermaid.
//
// Since, Until, and Range optionally narrow the diagram to a date window
// (inclusive on both ends). Since/Until are explicit bounds — a calendar date
// (2006-01-02), an RFC3339 timestamp, or "today"/"yesterday"; a lone Since or
// Until naming a bare date collapses to that single day. Range is a
// natural-language span ("last week", "last 2 days", "yesterday", "this week")
// resolved against the service clock, and is used only when neither Since nor
// Until is set.
type GraphInput struct {
	Task   string
	Format GraphFormat
	Since  string
	Until  string
	Range  string
}

// GraphResult carries both renderings plus the scope that produced them so the
// caller can show one or both.
type GraphResult struct {
	Format  GraphFormat
	Mermaid string
	SVG     string
	// TaskCount is the number of tasks (lanes, excluding the trunk) drawn.
	TaskCount int
	// Scoped reports whether the diagram was narrowed to a task lineage.
	Scoped bool
}

// Graph renders a git-graph diagram of the journal. With no Task it covers
// every task; with a Task it covers that task's connected lineage.
func (s *Service) Graph(ctx context.Context, in GraphInput) (GraphResult, error) {
	events, err := s.store.ListEvents(ctx, types.EventFilter{})
	if err != nil {
		return GraphResult{}, fmt.Errorf("graph: %w", err)
	}

	scoped := false
	if in.Task != "" {
		id, err := resolveRef(projection.Build(events), in.Task)
		if err != nil {
			return GraphResult{}, fmt.Errorf("graph: %w", err)
		}
		events = scopeToLineage(events, id)
		scoped = true
	}

	// Resolve the date window after lineage scoping so task-name resolution and
	// link-following still see the whole history, then keep only the events that
	// fall inside the window.
	window, err := parseWindow(in.Since, in.Until, in.Range, s.now())
	if err != nil {
		return GraphResult{}, fmt.Errorf("graph: %w", err)
	}
	if !window.empty() {
		events = filterByWindow(events, window)
		scoped = true
	}

	g := render.BuildGraph(events)
	format := in.Format
	if format == "" {
		format = FormatMermaid
	}

	out := GraphResult{
		Format:    format,
		Mermaid:   render.Mermaid(g),
		TaskCount: len(g.Lanes) - 1, // exclude the trunk
		Scoped:    scoped,
	}
	if format == FormatSVG {
		out.SVG = render.SVG(g, render.SVGOptions{})
	}
	return out, nil
}

// filterByWindow keeps only the events whose timestamps fall within w
// (inclusive on both bounds).
func filterByWindow(events []types.Event, w timeWindow) []types.Event {
	out := events[:0:0]
	for _, e := range events {
		if w.Since != nil && e.Timestamp.Before(*w.Since) {
			continue
		}
		if w.Until != nil && e.Timestamp.After(*w.Until) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// scopeToLineage keeps only the events whose tasks are reachable from seedID
// through originated-from and block edges (followed in both directions), so a
// scoped graph shows a self-contained cluster of related work.
func scopeToLineage(events []types.Event, seedID string) []types.Event {
	world := projection.Build(events)

	// Build an undirected adjacency over task links.
	adj := map[string]map[string]struct{}{}
	link := func(a, b string) {
		if a == "" || b == "" {
			return
		}
		for _, pair := range [2][2]string{{a, b}, {b, a}} {
			if adj[pair[0]] == nil {
				adj[pair[0]] = map[string]struct{}{}
			}
			adj[pair[0]][pair[1]] = struct{}{}
		}
	}
	for _, t := range world.Tasks() {
		link(t.ID, t.OriginTaskID)
		for _, b := range t.BlockedBy {
			link(t.ID, b)
		}
		for _, b := range t.Blocks {
			link(t.ID, b)
		}
	}

	keep := map[string]struct{}{seedID: {}}
	stack := []string{seedID}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range adj[cur] {
			if _, seen := keep[next]; !seen {
				keep[next] = struct{}{}
				stack = append(stack, next)
			}
		}
	}

	out := events[:0:0]
	for _, e := range events {
		if _, ok := keep[e.TaskID]; ok {
			out = append(out, e)
		}
	}
	return out
}
