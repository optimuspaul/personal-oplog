package render

import (
	"fmt"
	"strings"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
)

// Mermaid renders a Graph as Mermaid gitGraph source. Each lane becomes a
// branch, each node a commit; a task forks from its origin's branch at the
// commit that was its HEAD when the task began.
//
// gitGraph cannot draw arbitrary cross-branch edges, so a block relationship is
// shown only as a REVERSE commit tagged with the blocker's name, not as a line
// to the blocking branch. (The SVG renderer draws that edge.)
func Mermaid(g *Graph) string {
	var b strings.Builder
	// Rename Mermaid's default branch to the trunk so later `checkout` of it
	// resolves — without this directive the main branch is "main", and any
	// `checkout "journal"` fails with "branch not yet created".
	fmt.Fprintf(&b, "%%%%{init: {'gitGraph': {'mainBranchName': %s}}}%%%%\n", quote(TrunkName))
	b.WriteString("gitGraph\n")

	// Walk nodes in global chronological order, switching branches as needed so
	// forks attach at the correct point. The trunk is current at the start.
	current := trunkID
	created := map[string]bool{trunkID: true}

	// Seed the trunk so Mermaid has its required first commit and every
	// top-level task has something to branch from.
	b.WriteString("  commit id: \"journal-start\"\n")

	for _, n := range g.Nodes {
		lane := g.laneBy[n.LaneID]
		if !created[lane.ID] {
			// Fork from the parent: switch to it, then branch.
			if current != lane.ParentID {
				fmt.Fprintf(&b, "  checkout %s\n", quote(branchName(g, lane.ParentID)))
				current = lane.ParentID
			}
			fmt.Fprintf(&b, "  branch %s\n", quote(branchName(g, lane.ID)))
			created[lane.ID] = true
			current = lane.ID
		}
		if current != lane.ID {
			fmt.Fprintf(&b, "  checkout %s\n", quote(branchName(g, lane.ID)))
			current = lane.ID
		}
		b.WriteString("  " + commitLine(n) + "\n")
	}
	return b.String()
}

// branchName is the Mermaid branch label for a lane: its task name, made unique
// with a short id suffix when a name would otherwise collide. The trunk keeps
// its plain name (Mermaid's main branch).
func branchName(g *Graph, laneID string) string {
	lane := g.laneBy[laneID]
	if lane == nil || laneID == trunkID {
		return TrunkName
	}
	name := strings.TrimSpace(lane.Name)
	if name == "" {
		name = lane.ID
	}
	return fmt.Sprintf("%s (%s)", name, shortID(lane.ID))
}

func commitLine(n Node) string {
	parts := []string{"commit"}
	parts = append(parts, fmt.Sprintf("id: %s", quote(commitLabel(n))))
	if n.Tag != "" {
		parts = append(parts, fmt.Sprintf("tag: %s", quote(n.Tag)))
	}
	switch {
	case n.Highlight:
		parts = append(parts, "type: HIGHLIGHT")
	case n.Reverse:
		parts = append(parts, "type: REVERSE")
	}
	return strings.Join(parts, " ")
}

// commitLabel is the per-commit id Mermaid shows. It must be unique within the
// diagram, so it carries the action plus the event's short id.
func commitLabel(n Node) string {
	return fmt.Sprintf("%s %s", actionVerb(n.Action), shortID(n.EventID))
}

func actionVerb(a types.Action) string {
	switch a {
	case types.ActionStart:
		return "start"
	case types.ActionResume:
		return "resume"
	case types.ActionRestart:
		return "restart"
	case types.ActionPark:
		return "park"
	case types.ActionBlock:
		return "block"
	case types.ActionCheckpoint:
		return "checkpoint"
	case types.ActionNote:
		return "note"
	case types.ActionComplete:
		return "complete"
	default:
		return string(a)
	}
}

// shortID returns a stable short form of a ULID-ish id for compact labels.
func shortID(id string) string {
	if len(id) <= 6 {
		return id
	}
	return id[len(id)-6:]
}

// quote wraps a label in double quotes, escaping any embedded ones, so names
// with spaces or slashes stay valid Mermaid.
func quote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `'`) + `"`
}
