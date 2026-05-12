package worker

import (
	"context"

	"github.com/meterysh/metery/internal/store"
)

// RunOnce allows an HTTP webhook (or Cloud Run Job) to trigger
// a single pass of the recurrence evaluation.
func RunOnce(ctx context.Context, st *store.Store) {
	processRecurrence(ctx, st)
}
