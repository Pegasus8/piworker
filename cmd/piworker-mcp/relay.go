package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// The relay is protocol-version agnostic: it forwards each JSON-RPC message
// it reads from stdin, unchanged, as one POST to --url (MCP streamable HTTP
// transport) and writes what PiWorker answers to stdout, one message per
// line. It only reads what the transport needs: the id (to match answers and
// cancellations), the method, the negotiated protocol version and the
// per-request _meta version of 2026-07-28 and later.

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	// codeUpstream marks errors the bridge reports on PiWorker's behalf: it
	// could not be reached or verified, did not answer in time, or answered
	// with an HTTP error (data.httpStatus then holds the status).
	codeUpstream = -32000

	defaultMaxMessageBytes  = 4 << 20  // a message read from stdin
	defaultMaxResponseBytes = 16 << 20 // an answer or event from PiWorker
	defaultMaxQueued        = 64
	defaultRequestTimeout   = 2 * time.Minute
	defaultDrainTimeout     = 10 * time.Second
	// shutdownGrace bounds waiting for cancelled requests to wind down.
	shutdownGrace = time.Second

	// metaProtocolVersion is the per-request protocol version (SEP-2575).
	metaProtocolVersion = "io.modelcontextprotocol/protocolVersion"
	// standardHeadersSince is the first protocol version whose requests
	// carry Mcp-Method and Mcp-Name headers.
	standardHeadersSince = "2026-07-28"
)

var (
	errTimeout  = errors.New("request timed out")
	errShutdown = errors.New("piworker-mcp is shutting down")
	errNoAnswer = errors.New("PiWorker accepted the request without answering it")
)

// relayConfig configures a relay; zero limits take the defaults.
type relayConfig struct {
	Endpoint  *url.URL
	Token     string
	Connector *connector
	Log       *log.Logger

	MaxMessageBytes  int
	MaxResponseBytes int
	MaxInFlight      int // HTTP requests at once
	MaxQueued        int // messages accepted and not finished
	RequestTimeout   time.Duration
	DrainTimeout     time.Duration // how long in-flight requests may finish after stdin closes
}

type relay struct {
	cfg relayConfig
	sem chan struct{}
	out *output
	wg  sync.WaitGroup

	mu         sync.Mutex
	inflight   map[string]*call // requests by id
	pending    int              // messages accepted and not finished
	negotiated string           // protocol version from the initialize answer
}

// call is a request in flight.
type call struct {
	ctx    context.Context
	cancel context.CancelFunc
	// abandoned is set when the client cancels the request: it then gets no
	// answer, as the MCP cancellation rules require.
	abandoned atomic.Bool
}

func newRelay(cfg relayConfig) *relay {
	if cfg.MaxMessageBytes <= 0 {
		cfg.MaxMessageBytes = defaultMaxMessageBytes
	}
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = defaultMaxResponseBytes
	}
	if cfg.MaxInFlight <= 0 {
		cfg.MaxInFlight = defaultMaxInFlight
	}
	if cfg.MaxQueued <= 0 {
		cfg.MaxQueued = defaultMaxQueued
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	if cfg.DrainTimeout <= 0 {
		cfg.DrainTimeout = defaultDrainTimeout
	}
	if cfg.Log == nil {
		cfg.Log = log.New(io.Discard, "", 0)
	}
	return &relay{cfg: cfg, sem: make(chan struct{}, cfg.MaxInFlight), inflight: map[string]*call{}}
}

type inputLine struct {
	data    []byte
	tooLong bool
}

// serve relays until stdin ends (then in-flight requests get DrainTimeout to
// finish and serve returns nil), ctx is cancelled, or stdout fails.
func (r *relay) serve(ctx context.Context, in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	r.out = &output{w: out, fail: cancel}
	lines := make(chan inputLine)
	go r.read(ctx, in, lines)
	for {
		select {
		case <-ctx.Done():
			r.wait(shutdownGrace)
			return context.Cause(ctx)
		case l, ok := <-lines:
			if !ok {
				return r.drain(ctx, cancel)
			}
			if l.tooLong {
				r.reply(errorResponse(nil, codeInvalidRequest, fmt.Sprintf("invalid request: the message is larger than %d bytes", r.cfg.MaxMessageBytes), nil))
				continue
			}
			r.handle(ctx, l.data)
		}
	}
}

// read feeds stdin's lines to lines and closes it at EOF.
func (r *relay) read(ctx context.Context, in io.Reader, lines chan<- inputLine) {
	defer close(lines)
	br := bufio.NewReaderSize(in, 64<<10)
	for {
		line, err := readLine(br, r.cfg.MaxMessageBytes)
		var l inputLine
		switch {
		case errors.Is(err, errLineTooLong):
			l.tooLong = true
		case err != nil:
			if !errors.Is(err, io.EOF) {
				r.logf("reading stdin: %v", err)
			}
			return
		default:
			if l.data = bytes.TrimSpace(line); len(l.data) == 0 {
				continue
			}
		}
		select {
		case lines <- l:
		case <-ctx.Done():
			return
		}
	}
}

// drain lets in-flight requests finish after stdin closed, within
// DrainTimeout, then cancels the rest.
func (r *relay) drain(ctx context.Context, cancel context.CancelCauseFunc) error {
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	timer := time.NewTimer(r.cfg.DrainTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		r.logf("stdin closed; gave up on %d request(s) still in flight after %s", r.pendingCount(), r.cfg.DrainTimeout)
		cancel(errShutdown)
		r.wait(shutdownGrace)
	case <-ctx.Done():
		r.wait(shutdownGrace)
	}
	if err := r.out.failure(); err != nil {
		return err
	}
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, errShutdown) {
		return cause
	}
	return nil
}

// wait waits for the goroutines of in-flight messages, at most d (one stuck
// writing to a client that stopped reading must not block the exit).
func (r *relay) wait(d time.Duration) {
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
	}
}

func (r *relay) handle(ctx context.Context, line []byte) {
	m, reject := parseMessage(line)
	if reject != nil {
		r.reply(reject)
		return
	}
	switch m.kind {
	case kindRequest:
		c, reject := r.register(ctx, m)
		if reject != nil {
			r.reply(reject)
			return
		}
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			defer r.finish(m, c)
			r.forward(c.ctx, m, c)
		}()
	case kindNotification, kindResponse:
		if m.method == "notifications/cancelled" {
			// Handled here: aborting the POST is how a stateless server
			// learns of it (PiWorker sets PropagateRequestCancellation).
			r.cancelRequest(m.params)
			return
		}
		if !r.admit() {
			r.logf("dropped %s: too many messages in flight", m.label())
			return
		}
		octx, cancel := context.WithTimeoutCause(ctx, r.cfg.RequestTimeout, errTimeout)
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			defer r.finish(m, nil)
			defer cancel()
			r.forward(octx, m, nil)
		}()
	}
}

// register admits a request: its id must not be in flight and the queue must
// have room.
func (r *relay) register(ctx context.Context, m *message) (*call, []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.inflight[m.key]; dup {
		return nil, errorResponse(m.id, codeInvalidRequest, fmt.Sprintf("invalid request: id %s is already in use by a request in flight", m.id), nil)
	}
	if r.pending >= r.cfg.MaxQueued {
		return nil, errorResponse(m.id, codeUpstream, fmt.Sprintf("piworker-mcp: too many requests in flight (%d); wait for answers before sending more", r.pending), nil)
	}
	cctx, cancel := context.WithTimeoutCause(ctx, r.cfg.RequestTimeout, errTimeout)
	c := &call{ctx: cctx, cancel: cancel}
	r.inflight[m.key] = c
	r.pending++
	return c, nil
}

// admit counts a one-way message against the queue bound.
func (r *relay) admit() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending >= r.cfg.MaxQueued {
		return false
	}
	r.pending++
	return true
}

func (r *relay) finish(m *message, c *call) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c != nil {
		if r.inflight[m.key] == c {
			delete(r.inflight, m.key)
		}
		c.cancel()
	}
	r.pending--
}

func (r *relay) pendingCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pending
}

func (r *relay) cancelRequest(params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	key, ok := idKey(p.RequestID)
	if !ok {
		return
	}
	r.mu.Lock()
	c := r.inflight[key]
	r.mu.Unlock()
	if c != nil {
		c.abandoned.Store(true)
		c.cancel()
	}
}

// forward sends m and, for a request, answers it: with PiWorker's answer or
// with an error explaining why there is none.
func (r *relay) forward(ctx context.Context, m *message, c *call) {
	err := r.exchange(ctx, m, c)
	if err == nil {
		return
	}
	if c == nil {
		r.logf("%s: %v", m.label(), r.toRPCError(ctx, err).Message)
		return
	}
	if c.abandoned.Load() {
		return
	}
	e := r.toRPCError(ctx, err)
	r.logf("%s: %s", m.label(), e.Message)
	r.reply(errorResponse(m.id, e.Code, e.Message, e.Data))
}

func (r *relay) exchange(ctx context.Context, m *message, c *call) error {
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return ctx.Err()
	}
	client, err := r.cfg.Connector.client(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &bridgeError{err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.Endpoint.String(), bytes.NewReader(m.raw))
	if err != nil {
		return err
	}
	r.setHeaders(req.Header, m)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return statusError(resp, r.cfg.Endpoint)
	}
	if m.kind != kindRequest {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil
	}
	switch ct := mediaType(resp.Header.Get("Content-Type")); ct {
	case "application/json":
		body, err := io.ReadAll(io.LimitReader(resp.Body, int64(r.cfg.MaxResponseBytes)+1))
		if err != nil {
			return fmt.Errorf("reading PiWorker's answer: %w", err)
		}
		if len(body) > r.cfg.MaxResponseBytes {
			return &bridgeError{fmt.Sprintf("PiWorker's answer is larger than %d bytes", r.cfg.MaxResponseBytes)}
		}
		a, err := parseAnswer(body)
		if err != nil {
			return err
		}
		if a.fromServer {
			return &bridgeError{"PiWorker's answer is not a JSON-RPC message: it is a request or notification"}
		}
		return r.deliver(m, c, a, body)
	case "text/event-stream":
		return r.readStream(m, c, resp.Body)
	case "":
		return errNoAnswer
	default:
		return &bridgeError{fmt.Sprintf("PiWorker answered with %q instead of JSON or an event stream (HTTP %d)", ct, resp.StatusCode)}
	}
}

// readStream relays an event stream: PiWorker's requests and notifications
// for the call as they come, then the answer, which ends the stream.
func (r *relay) readStream(m *message, c *call, body io.Reader) error {
	delivered := false
	err := scanEvents(body, r.cfg.MaxResponseBytes, func(data []byte) (bool, error) {
		a, err := parseAnswer(data)
		switch {
		case err != nil:
			r.logf("%s: ignored an event that is not a JSON-RPC message", m.label())
			return false, nil
		case a.fromServer:
			r.answer(c, data)
			return false, nil
		case a.key != m.key:
			r.logf("%s: ignored an answer for another request (id %s)", m.label(), a.id)
			return false, nil
		}
		delivered = true
		return true, r.deliver(m, c, a, data)
	})
	if err != nil {
		return err
	}
	if !delivered {
		return &bridgeError{"PiWorker ended the event stream without answering"}
	}
	return nil
}

// deliver writes the answer to m.
func (r *relay) deliver(m *message, c *call, a *answer, raw []byte) error {
	if a.key != m.key {
		return &bridgeError{fmt.Sprintf("PiWorker answered another request (id %s instead of %s)", a.id, m.id)}
	}
	if m.method == "initialize" {
		r.noteNegotiated(a.result)
	}
	r.answer(c, raw)
	return nil
}

// answer writes a message for the call unless the client abandoned it.
func (r *relay) answer(c *call, msg []byte) {
	if c.abandoned.Load() {
		return
	}
	r.reply(msg)
}

func (r *relay) reply(msg []byte) {
	if err := r.out.write(msg); err != nil && !errors.Is(err, errOutputFailed) {
		r.logf("cannot relay a message to stdout: %v", err)
	}
}

// noteNegotiated records the protocol version of a successful initialize, to
// send it with every later request (MCP-Protocol-Version header).
func (r *relay) noteNegotiated(result json.RawMessage) {
	var res struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if json.Unmarshal(result, &res) != nil || !validVersion(res.ProtocolVersion) {
		return
	}
	r.mu.Lock()
	r.negotiated = res.ProtocolVersion
	r.mu.Unlock()
}

// setHeaders sets the streamable HTTP transport headers, as the go-sdk client
// does: the protocol version from the request's _meta, else the negotiated
// one; from 2026-07-28 on, Mcp-Method and Mcp-Name too.
func (r *relay) setHeaders(h http.Header, m *message) {
	h.Set("Authorization", "Bearer "+r.cfg.Token)
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json, text/event-stream")
	h.Set("User-Agent", userAgent())
	pv := m.metaVersion()
	if pv == "" {
		r.mu.Lock()
		pv = r.negotiated
		r.mu.Unlock()
	}
	if pv == "" {
		return
	}
	h.Set("Mcp-Protocol-Version", pv)
	if pv >= standardHeadersSince && m.method != "" {
		h.Set("Mcp-Method", m.method)
		// A name that cannot travel in a header is left out: the server then
		// explains what is wrong, rather than the request failing here.
		if name := m.name(); name != "" && isHeaderValue(name) {
			h.Set("Mcp-Name", name)
		}
	}
}

func (r *relay) logf(format string, args ...any) { r.cfg.Log.Printf(format, args...) }

// bridgeError is an error whose text is already meant for the client.
type bridgeError struct{ msg string }

func (e *bridgeError) Error() string { return e.msg }

// output writes one message per line to stdout.
type output struct {
	mu   sync.Mutex
	w    io.Writer
	err  error
	fail context.CancelCauseFunc // stops the relay when stdout fails
}

var errOutputFailed = errors.New("stdout failed")

func (o *output) write(msg []byte) error {
	var buf bytes.Buffer
	if err := json.Compact(&buf, msg); err != nil {
		return err
	}
	buf.WriteByte('\n')
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return errOutputFailed
	}
	if _, err := o.w.Write(buf.Bytes()); err != nil {
		o.err = fmt.Errorf("cannot write to stdout (the client is gone): %w", err)
		o.fail(o.err)
		return errOutputFailed
	}
	return nil
}

func (o *output) failure() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.err
}
