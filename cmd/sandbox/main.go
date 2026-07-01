// Command sandbox renders the oplog journal as a git-graph from the command
// line — a thin dev harness over internal/render. It reads the JSONL store and
// prints Mermaid gitGraph source (default) or writes a self-contained SVG.
//
//	go run ./cmd/sandbox                       # mermaid of the whole journal
//	go run ./cmd/sandbox -format svg -o g.svg  # svg to a file
//	go run ./cmd/sandbox -dir ~/.oplog         # point at a store
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/optimuspaul/personal-oplog/internal/persistence/jsonl"
	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	"github.com/optimuspaul/personal-oplog/internal/render"
)

func main() {
	dir := flag.String("dir", defaultDir(), "oplog JSONL store directory")
	format := flag.String("format", "mermaid", "output format: mermaid or svg")
	out := flag.String("o", "", "write to this file instead of stdout")
	flag.Parse()

	store, err := jsonl.NewStore(*dir)
	if err != nil {
		log.Fatalf("open store at %q: %v", *dir, err)
	}
	events, err := store.ListEvents(context.Background(), types.EventFilter{})
	if err != nil {
		log.Fatalf("list events: %v", err)
	}

	g := render.BuildGraph(events)
	var body string
	if *format == "svg" {
		body = render.SVG(g, render.SVGOptions{})
	} else {
		body = render.Mermaid(g)
	}

	if *out == "" {
		fmt.Print(body)
		return
	}
	if err := os.WriteFile(*out, []byte(body), 0o644); err != nil {
		log.Fatalf("write %q: %v", *out, err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d tasks)\n", *out, len(g.Lanes)-1)
}

func defaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".oplog"
	}
	return filepath.Join(home, ".oplog")
}
