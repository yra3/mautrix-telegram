package telegram

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"go.mau.fi/mautrix-telegram/pkg/gotd/bin"
	"go.mau.fi/mautrix-telegram/pkg/gotd/exchange"
)

// countingPermanentConn fails like a rejected auth key, which is a permanent
// error, so Client.Run returns as soon as the connection is used.
type countingPermanentConn struct {
	runs *atomic.Int64
}

func (c countingPermanentConn) Run(context.Context) error {
	c.runs.Inc()
	return exchange.ErrKeyFingerprintNotFound
}

func (countingPermanentConn) Invoke(context.Context, bin.Encoder, bin.Decoder) error { return nil }
func (countingPermanentConn) Ping(context.Context) error                             { return nil }

// blockingConn keeps the connection "running" until its context is canceled.
type blockingConn struct {
	started sync.Once
	ready   chan struct{}
}

func (c *blockingConn) Run(ctx context.Context) error {
	c.started.Do(func() { close(c.ready) })
	<-ctx.Done()
	return ctx.Err()
}

func (c *blockingConn) Invoke(context.Context, bin.Encoder, bin.Decoder) error { return nil }
func (c *blockingConn) Ping(context.Context) error                             { return nil }

func newRerunTestClient() *Client {
	return NewClient(1, "hash", Options{
		NoUpdates: true,
		ReconnectionBackoff: func() backoff.BackOff {
			return backoff.NewConstantBackOff(time.Nanosecond)
		},
	})
}

// TestClient_RunAfterPreviousRunReturned is a regression test for a session
// that could never come back: Run used to refuse to start whenever c.ctx was
// already canceled, and the previous Run always cancels c.ctx on exit. A
// bridge that restarts a dead client with the same instance therefore got
// "client already closed: context canceled" on every attempt and stayed dead
// until the whole process was restarted.
func TestClient_RunAfterPreviousRunReturned(t *testing.T) {
	client := newRerunTestClient()
	var runs atomic.Int64
	client.conn = countingPermanentConn{runs: &runs}

	ctx := context.Background()
	callback := func(context.Context) error { return nil }

	require.Error(t, client.Run(ctx, callback), "first run should fail on the permanent connection error")
	require.True(t, client.ctx.Err() != nil, "a finished Run must leave its context canceled")

	err := client.Run(ctx, callback)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "already closed")
	require.NotContains(t, err.Error(), "already running")
	require.EqualValues(t, 2, runs.Load(), "the second Run must reach the connection")
}

// TestClient_RunRejectsConcurrent checks that the restart path does not break
// the protection against two simultaneous runs sharing one client.
func TestClient_RunRejectsConcurrent(t *testing.T) {
	client := newRerunTestClient()
	conn := &blockingConn{ready: make(chan struct{})}
	client.conn = conn

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- client.Run(ctx, func(context.Context) error { return nil })
	}()
	<-conn.ready

	err := client.Run(ctx, func(context.Context) error { return nil })
	require.ErrorContains(t, err, "client is already running")

	cancel()
	<-done
}
