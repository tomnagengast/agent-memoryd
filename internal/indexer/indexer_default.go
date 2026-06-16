//go:build !zvec

package indexer

import (
	"fmt"

	"github.com/tomnagengast/agent-memoryd/internal/config"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
)

func New(cfg config.Config) (memory.Index, error) {
	switch cfg.IndexBackend {
	case "", "lexical":
		return memory.LexicalIndex{}, nil
	case "zvec":
		return nil, fmt.Errorf("zvec index requested but binary was built without -tags zvec")
	default:
		return nil, fmt.Errorf("unknown index backend %q", cfg.IndexBackend)
	}
}
