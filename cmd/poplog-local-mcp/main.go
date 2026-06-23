// Command poplog-local-mcp runs the personal-oplog work-journal MCP server
// over stdio.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/optimuspaul/personal-oplog/internal/mcp"
	"github.com/optimuspaul/personal-oplog/internal/persistence/jsonl"
	"github.com/optimuspaul/personal-oplog/internal/service"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Fatalf("poplog-local-mcp: %v", err)
	}
}

func run() error {
	dir := flag.String("dir", defaultDir(), "directory for the Oplog data store")
	flag.Parse()

	store, err := jsonl.NewStore(*dir)
	if err != nil {
		return fmt.Errorf("open store at %q: %w", *dir, err)
	}

	svc := service.New(store)
	server := mcp.NewServer(svc, version)

	// Stop cleanly on Ctrl-C / SIGTERM; Run returns when the context is cancelled.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := server.Run(ctx, &mcpsdk.StdioTransport{}); err != nil {
		return fmt.Errorf("run server: %w", err)
	}
	return nil
}

// defaultDir resolves the default store location, ~/.oplog, falling back to
// a relative ".oplog" if the home directory cannot be determined.
func defaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".oplog"
	}
	return filepath.Join(home, ".oplog")
}
