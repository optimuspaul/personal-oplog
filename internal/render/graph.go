// Package render turns the oplog event stream into git-graph-style diagrams:
// Mermaid gitGraph source (text) and a self-contained SVG rendered by a small
// native layout engine — no headless browser, no external service.
//
// The shape mirrors how a working day actually unfolds: each task is a branch,
// each event on it is a commit, and a task that was spawned from another
// (originated_from) forks off its parent's branch at the point in time it began.
package render

import (
	"sort"
	"time"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	"github.com/optimuspaul/personal-oplog/internal/projection"
)

// trunkID is the synthetic root lane every top-level task forks from. It gives
// the diagram a single anchor (and gives Mermaid its required first commit).
const trunkID = "__trunk__"

// TrunkName is the display name of the synthetic trunk lane.
const TrunkName = "journal"

// Node is one event positioned on a lane. Seq is its global chronological
// position across the whole graph; it drives the x-axis in every renderer.
type Node struct {
	Seq     int
	LaneID  string
	EventID string
	Action  types.Action
	Time    time.Time
	// Message is the event's free text, shown in the renderer's message column.
	Message string
	// Tag is a short annotation shown at the commit (e.g. "done", "blocked").
	Tag string
	// Highlight marks a milestone commit (completion); Reverse marks a setback
	// (block). They map to Mermaid commit types and to SVG styling.
	Highlight bool
	Reverse   bool
	// BlockerID, when set, records the task this block event points at. Mermaid
	// gitGraph cannot draw arbitrary cross-branch edges, so this only surfaces in
	// the SVG (a dashed dependency arrow) and the commit tag.
	BlockerID string
}

// Lane is a branch in the diagram — the trunk or a single task. ParentID is the
// lane it forks from; ParentSeq is the parent's HEAD position at the fork, which
// is where the branch visually attaches.
type Lane struct {
	ID        string
	Name      string
	Status    projection.TaskStatus
	ParentID  string
	ParentSeq int
	Index     int // assigned row, trunk = 0
	FirstSeq  int
	LastSeq   int
}

// Graph is the renderer-agnostic intermediate: ordered lanes and the
// chronological node stream that threads through them.
type Graph struct {
	Lanes []*Lane
	Nodes []Node
	// FocusTaskID is the task currently in focus, used by renderers to highlight
	// its most recent row. Empty when nothing is active.
	FocusTaskID string
	laneBy      map[string]*Lane
}

// Lane returns the lane with id, or nil.
func (g *Graph) Lane(id string) *Lane {
	return g.laneBy[id]
}

// BuildGraph folds events into a Graph. Events may arrive in any order; they are
// sorted chronologically (ID breaks ties, matching the projection) before
// folding so branch forks attach at the right point in time. Status for each
// lane is taken from the projected World so node colour reflects final state.
func BuildGraph(events []types.Event) *Graph {
	sorted := make([]types.Event, len(events))
	copy(sorted, events)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Timestamp.Equal(sorted[j].Timestamp) {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})

	world := projection.Build(events)

	g := &Graph{laneBy: make(map[string]*Lane)}
	trunk := &Lane{ID: trunkID, Name: TrunkName, Index: 0, ParentSeq: -1, FirstSeq: -1, LastSeq: -1}
	g.laneBy[trunkID] = trunk
	g.Lanes = append(g.Lanes, trunk)

	seq := 0
	for _, e := range sorted {
		if e.TaskID == "" {
			continue
		}
		lane := g.ensureLane(e, world)
		n := Node{
			Seq:     seq,
			LaneID:  lane.ID,
			EventID: e.ID,
			Action:  e.Action,
			Time:    e.Timestamp,
			Message: e.Message,
		}
		annotate(&n, e)
		g.Nodes = append(g.Nodes, n)
		if lane.FirstSeq < 0 {
			lane.FirstSeq = seq
		}
		lane.LastSeq = seq
		seq++
	}
	if focus := world.Focus(); focus != nil {
		g.FocusTaskID = focus.ID
	}
	return g
}

// ensureLane returns the lane for an event's task, creating it on first sight.
// A task forks from its origin lane when one is known and already has a commit;
// otherwise it forks from the trunk.
func (g *Graph) ensureLane(e types.Event, world *projection.World) *Lane {
	if lane, ok := g.laneBy[e.TaskID]; ok {
		return lane
	}

	parentID := trunkID
	if t := world.Task(e.TaskID); t != nil && t.OriginTaskID != "" {
		if p, ok := g.laneBy[t.OriginTaskID]; ok && p.LastSeq >= 0 {
			parentID = p.ID
		}
	}
	parent := g.laneBy[parentID]

	lane := &Lane{
		ID:        e.TaskID,
		Name:      laneName(e, world),
		Status:    laneStatus(e.TaskID, world),
		ParentID:  parentID,
		ParentSeq: parent.LastSeq, // trunk seed (or origin HEAD) at fork time
		Index:     len(g.Lanes),
		FirstSeq:  -1,
		LastSeq:   -1,
	}
	g.laneBy[lane.ID] = lane
	g.Lanes = append(g.Lanes, lane)
	return lane
}

func laneName(e types.Event, world *projection.World) string {
	if t := world.Task(e.TaskID); t != nil && t.Name != "" {
		return t.Name
	}
	if e.Name != "" {
		return e.Name
	}
	return e.TaskID
}

func laneStatus(id string, world *projection.World) projection.TaskStatus {
	if t := world.Task(id); t != nil {
		return t.Status
	}
	return projection.StatusNew
}

// annotate sets a node's tag and milestone flags from its action.
func annotate(n *Node, e types.Event) {
	switch e.Action {
	case types.ActionComplete:
		n.Tag = "done"
		n.Highlight = true
	case types.ActionBlock:
		n.Tag = "blocked"
		n.Reverse = true
		n.BlockerID = e.LinkTaskID
	case types.ActionCheckpoint:
		n.Tag = "checkpoint"
	case types.ActionPark:
		n.Tag = "parked"
	case types.ActionResume:
		n.Tag = "resume"
	case types.ActionRestart:
		n.Tag = "restart"
	case types.ActionStart, types.ActionNote:
		// no tag — start is implied by the branch fork, notes are plain commits
	}
}
