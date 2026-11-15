package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/rs/zerolog/log"
)

// maxHeaderBytes caps a request's line and headers on every listener: the
// editor's and agents' requests need a few KiB (cookies, a bearer token).
const maxHeaderBytes = 64 << 10

// newHTTPServer builds the HTTP server. Every request context derives from a
// base context that is cancelled as soon as Shutdown begins, so long-lived
// requests that honour r.Context() (the SSE event stream) end promptly instead
// of holding Shutdown until its timeout. Server errors go to zerolog, rate
// limited, with handshake noise at debug level (serverErrorWriter). The
// request line and headers are capped at maxHeaderBytes (Go's default is
// 1 MiB), larger ones refused with 431 before any handler runs.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	baseCtx, cancelBase := context.WithCancel(context.Background())
	srv := &http.Server{
		Addr:           addr,
		Handler:        handler,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   15 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: maxHeaderBytes,
		BaseContext:    func(net.Listener) context.Context { return baseCtx },
		ErrorLog:       newServerErrorWriter(log.Logger, time.Now).logger(),
	}
	srv.RegisterOnShutdown(cancelBase)
	return srv
}

// gracefulShutdown stops the process in dependency order: refuse new deploys,
// drain every HTTP server (which cancels streams via the base context), and
// only then stop the flows, so in-flight API calls never observe a
// half-stopped runtime and nothing can be deployed after StopAll.
// The flushers (the MCP audit writer) run last, once nothing can record
// anything any more, while the store is still open.
func gracefulShutdown(srvs []*http.Server, flows *service.Lifecycle, manager *flow.RuntimeManager, timeout time.Duration, flushers ...func(context.Context) error) error {
	flows.BeginShutdown()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	errs := make([]error, len(srvs))
	var wg sync.WaitGroup
	for i, srv := range srvs {
		wg.Go(func() { errs[i] = srv.Shutdown(ctx) })
	}
	wg.Wait()
	manager.StopAll()
	flushCtx, cancelFlush := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelFlush()
	for _, flush := range flushers {
		errs = append(errs, flush(flushCtx))
	}
	return errors.Join(errs...)
}
