package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	"github.com/optimuspaul/personal-oplog/internal/service"
)

func TestGraph_WholeJournalMermaid(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	a := mustLog(t, svc, service.LogInput{Task: "build auth", Action: types.ActionStart})
	mustLog(t, svc, service.LogInput{Task: "oauth spike", Action: types.ActionStart, Link: a.ID})
	mustLog(t, svc, service.LogInput{Task: "unrelated chore", Action: types.ActionStart})

	res, err := svc.Graph(ctx, service.GraphInput{})
	if err != nil {
		t.Fatalf("Graph: %v", err)
	}
	if res.Format != service.FormatMermaid {
		t.Fatalf("format = %q, want mermaid (default)", res.Format)
	}
	if res.Scoped {
		t.Fatal("whole-journal graph should not be scoped")
	}
	if res.TaskCount != 3 {
		t.Fatalf("task count = %d, want 3", res.TaskCount)
	}
	if !strings.Contains(res.Mermaid, "\ngitGraph\n") {
		t.Fatalf("bad mermaid:\n%s", res.Mermaid)
	}
	if res.SVG != "" {
		t.Fatal("svg should be empty when mermaid requested")
	}
}

func TestGraph_ScopeToLineage(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	a := mustLog(t, svc, service.LogInput{Task: "build auth", Action: types.ActionStart})
	b := mustLog(t, svc, service.LogInput{Task: "oauth spike", Action: types.ActionStart, Link: a.ID})
	// A blocker linked to B — part of B's lineage.
	c := mustLog(t, svc, service.LogInput{Task: "config fix", Action: types.ActionStart})
	mustLog(t, svc, service.LogInput{Task: b.ID, Action: types.ActionBlock, Link: c.ID})
	// Wholly unrelated work that must be excluded when scoped to A.
	mustLog(t, svc, service.LogInput{Task: "unrelated chore", Action: types.ActionStart})

	res, err := svc.Graph(ctx, service.GraphInput{Task: "build auth", Format: service.FormatSVG})
	if err != nil {
		t.Fatalf("Graph: %v", err)
	}
	if !res.Scoped {
		t.Fatal("expected scoped graph")
	}
	// A, B, C are linked; the chore is not.
	if res.TaskCount != 3 {
		t.Fatalf("scoped task count = %d, want 3 (A,B,C)", res.TaskCount)
	}
	if !strings.HasPrefix(res.SVG, "<svg") {
		t.Fatalf("expected svg output, got %d bytes not starting with <svg", len(res.SVG))
	}
}

func TestGraph_UnknownTask(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Graph(context.Background(), service.GraphInput{Task: "nope"}); err == nil {
		t.Fatal("expected error for unknown task")
	}
}
