// Package workpool runs independent units of work in parallel for the LLM
// phases, which all share one shape: a bounded number of workers, results
// gathered by a single owner, and cancellation that stops handing out work.
package workpool

import (
	"context"
	"sync"
)

// Run calls work on each item with at most workers (at least one) calls at
// once, and hands every result to collect. collect runs in the calling
// goroutine, one result at a time, in completion order: it may update
// state without locking. Once ctx is cancelled no new item starts; the
// items already started finish and are collected. Run returns when every
// started item has been collected.
func Run[T, R any](
	ctx context.Context,
	workers int,
	items []T,
	work func(context.Context, *T) R,
	collect func(R),
) {
	jobs := make(chan *T)
	results := make(chan R)
	var wg sync.WaitGroup
	for range max(workers, 1) {
		wg.Go(func() {
			for item := range jobs {
				results <- work(ctx, item)
			}
		})
	}
	go func() {
		defer close(results)
		defer wg.Wait()
		defer close(jobs)
		for i := range items {
			select {
			case <-ctx.Done():
				return
			case jobs <- &items[i]:
			}
		}
	}()
	for r := range results {
		collect(r)
	}
}
