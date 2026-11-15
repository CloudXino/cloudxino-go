package cloudxino

import (
	"context"
	"fmt"
	"time"
)

// WaitOptions tunes a waiter. Zero values pick the defaults.
type WaitOptions struct {
	// Interval between checks (default 5s).
	Interval time.Duration
	// Timeout for the whole wait (default: until ctx ends).
	Timeout time.Duration
}

// ErrStuck is returned by a waiter when the resource ends in a failed state.
type ErrStuck struct {
	Kind, ID, Status, Detail string
}

func (e *ErrStuck) Error() string {
	msg := fmt.Sprintf("cloudxino: %s %s is %s", e.Kind, e.ID, e.Status)
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// waitFor calls check until it reports done, fails, or the context / timeout ends.
func waitFor(ctx context.Context, opts *WaitOptions, check func(context.Context) (bool, error)) error {
	interval := 5 * time.Second
	if opts != nil && opts.Interval > 0 {
		interval = opts.Interval
	}
	if opts != nil && opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	for {
		done, err := check(ctx)
		if err != nil || done {
			return err
		}
		t := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			t.Stop()
			return fmt.Errorf("cloudxino: gave up waiting: %w", ctx.Err())
		case <-t.C:
		}
	}
}

func strv(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
