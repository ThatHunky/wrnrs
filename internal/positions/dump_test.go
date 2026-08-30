package positions_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"wrnrs/internal/catalog"
	"wrnrs/internal/positions"
)

type recordingSender struct {
	sent []string
	fail error
}

func (s *recordingSender) SendItem(_ context.Context, item catalog.Item) error {
	if s.fail != nil {
		return s.fail
	}
	s.sent = append(s.sent, item.ID)
	return nil
}

func dumpItems(n int) []catalog.Item {
	items := make([]catalog.Item, 0, n)
	for i := 1; i <= n; i++ {
		items = append(items, catalog.Item{ID: strconv.Itoa(i)})
	}
	return items
}

func TestDumpSendsEveryItemAndThrottlesBetweenThem(t *testing.T) {
	sender := &recordingSender{}
	var slept []time.Duration

	sent, err := positions.Dump(context.Background(), sender, positions.DumpOptions{
		Items:    dumpItems(3),
		Interval: time.Second,
		Sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if sent != 3 || len(sender.sent) != 3 {
		t.Fatalf("sent %d items (%v), want 3", sent, sender.sent)
	}
	if len(slept) != 2 {
		t.Fatalf("slept %d times, want 2 — between items, not after the last", len(slept))
	}
	for _, d := range slept {
		if d != time.Second {
			t.Fatalf("slept %v, want the configured 1s interval", d)
		}
	}
}

func TestDumpStopsBeforeTheNextSendWhenStoppedFlips(t *testing.T) {
	sender := &recordingSender{}
	stopAfter := 2
	sent, err := positions.Dump(context.Background(), sender, positions.DumpOptions{
		Items:    dumpItems(10),
		Interval: time.Second,
		Sleep:    func(context.Context, time.Duration) error { return nil },
		Stopped:  func() bool { return len(sender.sent) >= stopAfter },
	})
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if sent != 2 {
		t.Fatalf("sent = %d, want 2 — the stop flag must be checked before each send", sent)
	}
}

func TestDumpStopsOnCancelledContext(t *testing.T) {
	sender := &recordingSender{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sent, err := positions.Dump(ctx, sender, positions.DumpOptions{
		Items:    dumpItems(5),
		Interval: time.Second,
		Sleep:    func(context.Context, time.Duration) error { return nil },
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Dump on a cancelled context returned %v, want context.Canceled", err)
	}
	if sent != 0 {
		t.Fatalf("sent = %d on a cancelled context, want 0", sent)
	}
}

func TestDumpReturnsTheSendErrorAndTheCountSoFar(t *testing.T) {
	sender := &recordingSender{fail: errors.New("telegram exploded")}

	sent, err := positions.Dump(context.Background(), sender, positions.DumpOptions{
		Items:    dumpItems(3),
		Interval: time.Second,
		Sleep:    func(context.Context, time.Duration) error { return nil },
	})
	if err == nil {
		t.Fatal("Dump with a failing sender succeeded, want the error surfaced")
	}
	if sent != 0 {
		t.Fatalf("sent = %d, want 0 — the first send already failed", sent)
	}
}

func TestDumpWithEmptyItemsReturnsZeroCleanly(t *testing.T) {
	sender := &recordingSender{}

	sent, err := positions.Dump(context.Background(), sender, positions.DumpOptions{
		Items:    nil,
		Interval: time.Second,
		Sleep:    func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatalf("Dump with no items: %v", err)
	}
	if sent != 0 {
		t.Fatalf("sent = %d, want 0", sent)
	}
}

func TestDumpWorksWithoutStoppedWired(t *testing.T) {
	sender := &recordingSender{}

	sent, err := positions.Dump(context.Background(), sender, positions.DumpOptions{
		Items:    dumpItems(3),
		Interval: time.Second,
		Sleep:    func(context.Context, time.Duration) error { return nil },
		Stopped:  nil,
	})
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if sent != 3 {
		t.Fatalf("sent = %d, want 3 — a nil Stopped must never block sends", sent)
	}
}

func TestDumpDefaultSleepHonoursContextCancellation(t *testing.T) {
	sender := &recordingSender{}
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	sent, err := positions.Dump(ctx, sender, positions.DumpOptions{
		Items:    dumpItems(2),
		Interval: time.Hour,
		// Sleep left nil: exercises the default sleep implementation.
	})
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Dump with nil Sleep on a cancelled context returned %v, want context.Canceled", err)
	}
	if sent != 1 {
		t.Fatalf("sent = %d, want 1 — the first item sends, then cancellation cuts the wait short", sent)
	}
	if elapsed >= time.Hour {
		t.Fatalf("Dump took %v, the default sleep did not honour context cancellation", elapsed)
	}
}
