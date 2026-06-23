// Package mcp exposes the Oplog service as a set of MCP tools.
//
// It is a thin adapter: each tool unmarshals typed arguments, calls a
// service method, and renders both a human-readable summary and structured
// output. All journal logic lives in the service layer.
package mcp

import (
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/optimuspaul/personal-oplog/internal/service"
)

const serverName = "oplog"

// NewServer builds an MCP server exposing the Oplog tools backed by svc.
func NewServer(svc *service.Service, version string) *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    serverName,
		Title:   "Oplog work journal",
		Version: version,
	}, nil)

	registerTools(server, svc)
	return server
}
