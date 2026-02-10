package slack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/alekspetrov/pilot/internal/logging"
	"github.com/gorilla/websocket"
)

// Default backoff parameters for reconnection.
const (
	defaultInitialBackoff = 1 * time.Second
	defaultMaxBackoff     = 30 * time.Second
	defaultBackoffFactor  = 2
)

// ListenerOption configures a Listener.
type ListenerOption func(*Listener)

// WithBackoff overrides the default backoff parameters.
func WithBackoff(initial, max time.Duration) ListenerOption {
	return func(l *Listener) {
		if initial > 0 {
			l.initialBackoff = initial
		}
		if max > 0 {
			l.maxBackoff = max
		}
	}
}

// WithDialer overrides the default WebSocket dialer.
// Useful for testing.
func WithDialer(d *websocket.Dialer) ListenerOption {
	return func(l *Listener) {
		l.dialer = d
	}
}

// Listener manages a persistent Socket Mode connection with auto-reconnect.
// It obtains a WebSocket URL via SocketModeClient.OpenConnection, dials the
// WebSocket, runs a SocketModeHandler for event reading and ping/pong
// keepalive, and reconnects with exponential backoff on any failure.
type Listener struct {
	client         *SocketModeClient
	dialer         *websocket.Dialer
	log            *slog.Logger
	initialBackoff time.Duration
	maxBackoff     time.Duration
}

// NewListener creates a Listener that will use the given SocketModeClient
// to obtain WebSocket URLs and reconnect on failure.
func NewListener(client *SocketModeClient, opts ...ListenerOption) *Listener {
	l := &Listener{
		client:         client,
		dialer:         websocket.DefaultDialer,
		log:            logging.WithComponent("slack.listener"),
		initialBackoff: defaultInitialBackoff,
		maxBackoff:     defaultMaxBackoff,
	}
	for _, o := range opts {
		o(l)
	}
	return l
}

// Listen runs a reconnection loop that:
//  1. Calls OpenConnection to get a fresh WSS URL
//  2. Dials the WebSocket
//  3. Runs SocketModeHandler (read loop + ping/pong keepalive)
//  4. On read error or disconnect envelope → closes connection, backs off, reconnects
//  5. Resets backoff on successful connection
//
// Events are sent on the returned channel. The channel is closed when ctx is
// cancelled or a permanent error (e.g. auth failure) occurs. The returned
// error is non-nil only for permanent failures; context cancellation returns nil.
func (l *Listener) Listen(ctx context.Context) (<-chan RawSocketEvent, error) {
	// Merged event channel — survives reconnections.
	merged := make(chan RawSocketEvent, 64)

	go l.reconnectLoop(ctx, merged)

	return merged, nil
}

// reconnectLoop is the core loop that connects, forwards events, and
// reconnects with exponential backoff.
func (l *Listener) reconnectLoop(ctx context.Context, merged chan<- RawSocketEvent) {
	defer close(merged)

	backoff := l.initialBackoff

	for {
		if err := ctx.Err(); err != nil {
			l.log.Info("listener shutting down", slog.Any("reason", err))
			return
		}

		// Step 1: Get a fresh WebSocket URL.
		wssURL, err := l.client.OpenConnection(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// Auth failures are permanent — no point retrying.
			if errors.Is(err, ErrAuthFailure) {
				l.log.Error("permanent auth failure, stopping listener",
					slog.Any("error", err))
				return
			}
			l.log.Warn("failed to open connection, retrying",
				slog.Any("error", err),
				slog.Duration("backoff", backoff))
			if !l.sleep(ctx, backoff) {
				return
			}
			backoff = l.nextBackoff(backoff)
			continue
		}

		// Step 2: Dial the WebSocket.
		conn, _, err := l.dialer.DialContext(ctx, wssURL, nil)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			l.log.Warn("failed to dial websocket, retrying",
				slog.String("url", wssURL),
				slog.Any("error", err),
				slog.Duration("backoff", backoff))
			if !l.sleep(ctx, backoff) {
				return
			}
			backoff = l.nextBackoff(backoff)
			continue
		}

		// Step 3: Connection established — reset backoff.
		backoff = l.initialBackoff
		l.log.Info("socket mode connected", slog.String("url", wssURL))

		// Step 4: Run handler, forward events until disconnect/error.
		handler, events := NewSocketModeHandler(conn)

		// Run handler in background; it blocks until connection dies.
		done := make(chan struct{})
		go func() {
			handler.Run()
			close(done)
		}()

		// Forward events from per-connection channel to merged channel.
		l.forwardEvents(ctx, events, merged, done)

		// Handler returned — connection is dead. Loop to reconnect.
		l.log.Info("connection lost, reconnecting",
			slog.Duration("backoff", backoff))

		if ctx.Err() != nil {
			return
		}
		if !l.sleep(ctx, backoff) {
			return
		}
		backoff = l.nextBackoff(backoff)
	}
}

// forwardEvents reads from a per-connection events channel and writes to the
// merged channel. Returns when the per-connection channel is closed (handler
// exited) or ctx is cancelled.
func (l *Listener) forwardEvents(ctx context.Context, events <-chan RawSocketEvent, merged chan<- RawSocketEvent, done <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			// Drain any remaining events after handler exits.
			for evt := range events {
				select {
				case merged <- evt:
				default:
					l.log.Warn("merged channel full, dropping event",
						slog.String("type", string(evt.Type)))
				}
			}
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			select {
			case merged <- evt:
			case <-ctx.Done():
				return
			}
		}
	}
}

// nextBackoff doubles the current backoff, capped at maxBackoff.
func (l *Listener) nextBackoff(current time.Duration) time.Duration {
	next := current * defaultBackoffFactor
	if next > l.maxBackoff {
		next = l.maxBackoff
	}
	return next
}

// sleep waits for the given duration or until ctx is cancelled.
// Returns true if the sleep completed, false if ctx was cancelled.
func (l *Listener) sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// ErrPermanentFailure indicates a non-retryable error (e.g. auth failure).
var ErrPermanentFailure = fmt.Errorf("slack socket mode: permanent failure")
