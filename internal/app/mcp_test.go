package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tomnagengast/agent-memoryd/internal/config"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
	"github.com/tomnagengast/agent-memoryd/internal/version"
)

func TestMCPStatusToolRegisteredAndReturnsStoreStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newFakeMemoryAPI()
	store.records["one"] = memory.Record{ID: "one", Body: "one"}
	store.records["two"] = memory.Record{ID: "two", Body: "two"}

	session, cleanup := connectTestMCP(t, ctx, newMCPServer(config.Config{}, store))
	defer cleanup()

	initResult := session.InitializeResult()
	if initResult == nil {
		t.Fatal("initialize result is nil")
	}
	if got := initResult.ServerInfo.Version; got != version.Value() {
		t.Fatalf("server version = %q, want %q", got, version.Value())
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if !hasMCPTool(tools.Tools, "status") {
		t.Fatalf("status tool not registered: %#v", toolNames(tools.Tools))
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "status",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call status: %v", err)
	}
	if result.IsError {
		t.Fatalf("status returned tool error: %#v", result.Content)
	}

	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var status memory.Status
	if err := json.Unmarshal(raw, &status); err != nil {
		t.Fatalf("decode status: %v\n%s", err, raw)
	}
	if status.Path != "/fake" || status.Backend != "fake" || status.MemoryCount != 2 {
		t.Fatalf("status = %#v, want fake backend with 2 memories", status)
	}

	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode status fields: %v", err)
	}
	for _, field := range []string{"path", "backend", "memory_count", "pending_embedding", "embedder"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("status field %q missing from %s", field, raw)
		}
	}
}

func connectTestMCP(t *testing.T, ctx context.Context, server *mcp.Server) (*mcp.ClientSession, func()) {
	t.Helper()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		serverSession.Close()
		t.Fatalf("connect client: %v", err)
	}
	return clientSession, func() {
		clientSession.Close()
		serverSession.Close()
	}
}

func hasMCPTool(tools []*mcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func toolNames(tools []*mcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}
