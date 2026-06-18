package memory

import "context"

// API is the interface that all memory store implementations must satisfy.
// It is implemented by *Store (direct zvec access) and will be implemented
// by an RPC client in Phase B.
type API interface {
	Add(ctx context.Context, req AddRequest) (Record, error)
	Get(ctx context.Context, id string) (Record, error)
	Search(ctx context.Context, req SearchRequest) ([]SearchResult, error)
	Forget(ctx context.Context, id string) error
	List(ctx context.Context) ([]Record, error)
	Status(ctx context.Context) (Status, error)
	Backfill(ctx context.Context) (int, error)
	// Optimize merges pending FTS index segments so that records written in
	// this session are durable and visible to FTS queries in future sessions.
	// Must be called before Close() when writes need to survive a process restart.
	Optimize(ctx context.Context) error
	Close() error
}

// compile-time assertion: *Store must satisfy API.
var _ API = (*Store)(nil)
