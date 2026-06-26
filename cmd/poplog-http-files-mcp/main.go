// Command poplog-http-files-mcp runs the personal-oplog work-journal MCP server
// over HTTP (the streamable transport), backed by the JSONL file store.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/optimuspaul/personal-oplog/internal/mcp"
	"github.com/optimuspaul/personal-oplog/internal/persistence/jsonl"
	"github.com/optimuspaul/personal-oplog/internal/service"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Fatalf("poplog-http-files-mcp: %v", err)
	}
}

func run() error {
	dir := flag.String("dir", defaultDir(), "directory for the Oplog data store")
	addr := flag.String("addr", "127.0.0.1:8080", "host:port to listen on")
	flag.Parse()

	store, err := jsonl.NewStore(*dir)
	if err != nil {
		return fmt.Errorf("open store at %q: %w", *dir, err)
	}

	svc := service.New(store)
	server := mcp.NewServer(svc, version)

	// The same server handles every session; getServer ignores the request.
	handler := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return server },
		nil,
	)

	httpServer := &http.Server{
		Addr:    *addr,
		Handler: handler,
	}

	// Stop cleanly on Ctrl-C / SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Shut the HTTP server down when the signal context is cancelled.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("poplog-http-files-mcp: shutdown: %v", err)
		}
	}()

	log.Printf("poplog-http-files-mcp %s listening on %s (store: %s)", version, *addr, *dir)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
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
