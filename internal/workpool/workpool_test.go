package workpool_test

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/workpool"
)

func TestRun(t *testing.T) {
	cases := map[string]struct {
		workers int
		items   int
	}{
		"no items":                {workers: 4, items: 0},
		"one worker":              {workers: 1, items: 10},
		"zero means one":          {workers: 0, items: 5},
		"more workers than items": {workers: 8, items: 3},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			items := make([]int, tc.items)
			for i := range items {
				items[i] = i
			}
			var got []int // collect runs in this goroutine: no lock needed
			workpool.Run(context.Background(), tc.workers, items,
				func(_ context.Context, n *int) int { return *n * 10 },
				func(r int) { got = append(got, r) })
			slices.Sort(got)
			want := make([]int, tc.items)
			for i := range want {
				want[i] = i * 10
			}
			if !slices.Equal(got, want) {
				t.Fatalf("collected %v, want %v", got, want)
			}
		})
	}
}

// TestRunStopsHandingOutWorkOnCancel: once ctx is cancelled, no new item
// starts, and Run still returns.
func TestRunStopsHandingOutWorkOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var started atomic.Int64
	items := make([]int, 100)
	workpool.Run(ctx, 1, items,
		func(_ context.Context, _ *int) int {
			if started.Add(1) == 3 {
				cancel()
			}
			return 0
		},
		func(int) {})
	if n := started.Load(); n >= int64(len(items)) {
		t.Fatalf("%d items started after cancellation, want fewer than %d", n, len(items))
	}
}
