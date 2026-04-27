package usage

import (
	"context"
	"testing"
	"time"
)

type pluginFunc func(ctx context.Context, record Record)

func (f pluginFunc) HandleUsage(ctx context.Context, record Record) { f(ctx, record) }

func TestManagerFlushWaitsForQueuedAndInFlightRecords(t *testing.T) {
	manager := NewManager(1)
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})

	manager.Register(pluginFunc(func(ctx context.Context, record Record) {
		close(started)
		<-release
		close(finished)
	}))

	manager.Publish(context.Background(), Record{Provider: "codex", Model: "gpt-5.4"})

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("plugin did not start")
	}

	flushDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		flushDone <- manager.Flush(ctx)
	}()

	select {
	case err := <-flushDone:
		t.Fatalf("Flush() returned before in-flight record completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-flushDone:
		if err != nil {
			t.Fatalf("Flush() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Flush() did not return after plugin completed")
	}

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("plugin did not finish")
	}
}
