//go:build !zvec

package zvecindex

import (
	"context"
	"fmt"

	"github.com/tomnagengast/agent-memoryd/internal/memory"
)

type Index struct{}

func New(string) (*Index, error) {
	return nil, fmt.Errorf("zvec index unavailable: build with -tags zvec and native zvec libraries")
}

func (i *Index) Name() string {
	return "zvec"
}

func (i *Index) Search(context.Context, []memory.Record, memory.SearchRequest) ([]memory.SearchResult, error) {
	return nil, fmt.Errorf("zvec index unavailable")
}

func (i *Index) Upsert(context.Context, memory.Record) error {
	return fmt.Errorf("zvec index unavailable")
}

func (i *Index) Delete(context.Context, string) error {
	return fmt.Errorf("zvec index unavailable")
}

func (i *Index) Rebuild(context.Context, []memory.Record) error {
	return fmt.Errorf("zvec index unavailable")
}
