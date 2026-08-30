package positions

import (
	"context"
	"time"

	"wrnrs/internal/catalog"
)

// Sender delivers one item to the chat. Kept narrow so Dump stays testable
// without a Telegram client.
type Sender interface {
	SendItem(ctx context.Context, item catalog.Item) error
}

// DumpOptions configures a throttled, interruptible dump of a selection to
// the chat.
type DumpOptions struct {
	Items    []catalog.Item
	Interval time.Duration
	// Sleep is injected so tests do not wait in real time. Defaults to a
	// context-aware real sleep when nil.
	Sleep func(ctx context.Context, d time.Duration) error
	// Stopped is polled before every send so the stop button takes effect
	// within one interval rather than after the whole batch. May be nil.
	Stopped func() bool
}

// Dump sends every item in options.Items through sender, sleeping
// options.Interval between sends (not after the last one). It stops early —
// without error — if options.Stopped reports true before a send, or with the
// context's error if ctx is cancelled. A send error aborts the run and is
// returned along with the count of items already sent.
func Dump(ctx context.Context, sender Sender, options DumpOptions) (int, error) {
	sleep := options.Sleep
	if sleep == nil {
		sleep = defaultSleep
	}

	sent := 0
	for i, item := range options.Items {
		if err := ctx.Err(); err != nil {
			return sent, err
		}
		if options.Stopped != nil && options.Stopped() {
			return sent, nil
		}
		if err := sender.SendItem(ctx, item); err != nil {
			return sent, err
		}
		sent++
		if i < len(options.Items)-1 {
			if err := sleep(ctx, options.Interval); err != nil {
				return sent, err
			}
		}
	}
	return sent, nil
}

// defaultSleep waits for d, or returns ctx.Err() early if ctx is cancelled
// first.
func defaultSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
