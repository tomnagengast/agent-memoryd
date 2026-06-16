//go:build zvec

package indexer

import (
	"fmt"

	"github.com/tomnagengast/agent-memoryd/internal/config"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
	"github.com/tomnagengast/agent-memoryd/internal/zvecindex"
)

func New(cfg config.Config) (memory.Index, error) {
	switch cfg.IndexBackend {
	case "", "lexical":
		return memory.LexicalIndex{}, nil
	case "zvec":
		return zvecindex.New(cfg.ZvecPath)
	default:
		return nil, fmt.Errorf("unknown index backend %q", cfg.IndexBackend)
	}
}
