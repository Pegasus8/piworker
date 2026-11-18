package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/rs/zerolog/log"

	"github.com/Pegasus8/piworker/internal/node"
)

// Response body limits: a provider (or whatever answers at a connection's
// endpoint) cannot make PiWorker read without bound.
const (
	maxChatBody       = 8 << 20
	maxModelsBody     = 4 << 20
	maxEmbeddingsBody = 64 << 20
)

// MaxAudioBytes bounds the audio a transcription sends (OpenAI's limit).
const MaxAudioBytes = 25 << 20

// maxErrorMessage bounds an APIError's message.
const maxErrorMessage = maxQuoted

// ErrSandboxed: LLM calls never run in a dry-run or a node test (the LLM
// nodes are not safe, so Registry.Derive stubs them anyway).
var ErrSandboxed = errors.New("LLM calls do not run in a dry-run or a node test")

// provider is a provider's client. Its calls return the usage they spent
// also when they fail after the provider answered (a refusal, an answer
// that is not the JSON asked for), so the budget counts it.
type provider interface {
	chat(ctx context.Context, req Request) (*Response, Usage, error)
	models(ctx context.Context) ([]ModelInfo, error)
	embed(ctx context.Context, model string, inputs []string) ([]Embedding, Usage, error)
	transcribe(ctx context.Context, model string, audio Media, language string) (string, Usage, error)
}

// NewClient returns a client of the connection c. It resolves the API key
// through ctx's secret store (an undefined secret is an error) and, for
// every call, checks the daily budget of the connection in FromContext(ctx)
// (with the stored connection's limit), runs the call and records what it
// spent (Store.RecordUsage), also when the answer then fails validation. A
// sandboxed context gets ErrSandboxed. Listing the models spends no tokens:
// it is neither checked nor recorded.
func NewClient(ctx context.Context, c Connection) (Client, error) {
	if node.InSandbox(ctx) {
		return nil, ErrSandboxed
	}
	c.Normalize()
	key, err := c.resolvedKey(ctx)
	if err != nil {
		return nil, err
	}
	hc := newHTTPClient()
	var p provider
	switch c.Provider {
	case ProviderAnthropic:
		if key == "" {
			return nil, fmt.Errorf("LLM connection %q has no API key (Anthropic needs one)", c.Name)
		}
		p = newAnthropicClient(c, key, hc)
	case ProviderOpenAI:
		p = &openAIClient{conn: c, key: key, http: hc}
	default:
		return nil, fmt.Errorf("LLM connection %q: unknown provider %q", c.Name, c.Provider)
	}
	return &meteredClient{conn: c, store: FromContext(ctx), p: p}, nil
}

// meteredClient checks requests, spends from the connection's budget and
// records the usage around a provider's client.
type meteredClient struct {
	conn  Connection
	store *Store
	p     provider
}

// checkBudget checks the budget with the stored connection's limit (the
// admin may have changed it since the client was built), or c's own for a
// connection that is not stored (a test of unsaved settings).
func (m *meteredClient) checkBudget() error {
	c := m.conn
	if stored, ok := m.store.Get(c.Name); ok {
		c.DailyTokenLimit = stored.DailyTokenLimit
	}
	return m.store.CheckBudget(c)
}

func (m *meteredClient) record(u Usage) {
	if err := m.store.RecordUsage(m.conn.Name, u); err != nil {
		log.Warn().Err(err).Str("connection", m.conn.Name).Msg("llm: usage not recorded")
	}
}

// Chat implements Client.
func (m *meteredClient) Chat(ctx context.Context, req Request) (*Response, error) {
	if req.Model == "" {
		return nil, ErrNoModel
	}
	if req.MaxTokens <= 0 {
		return nil, errors.New("maxTokens must be greater than 0")
	}
	if !slices.Contains(Efforts, req.Effort) {
		return nil, fmt.Errorf("invalid effort %q (low, medium, high, xhigh or max)", req.Effort)
	}
	// The prepared schema is compiled once: for the check here and the
	// answer's validation (finishAnswer).
	var schema *jsonschema.Resolved
	if req.Schema != nil {
		prepared, resolved, err := prepareSchema(req.Schema)
		if err != nil {
			return nil, err
		}
		req.Schema, schema = prepared, resolved
	} else if req.JSON {
		if req.System == "" {
			req.System = jsonInstruction
		} else {
			req.System += "\n\n" + jsonInstruction
		}
	}
	for _, msg := range req.Messages {
		for _, img := range msg.Images {
			if img.URL == "" && !imageTypes[img.MediaType] {
				return nil, fmt.Errorf("unsupported image type %q (PNG, JPEG, GIF or WebP)", img.MediaType)
			}
		}
	}
	if err := m.checkBudget(); err != nil {
		return nil, err
	}
	resp, usage, err := m.p.chat(ctx, req)
	m.record(usage)
	if err != nil {
		return nil, err
	}
	if err := finishAnswer(req, schema, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// finishAnswer parses and validates the JSON answer a request asked for;
// schema is req.Schema compiled (ValidateOutput's rule).
func finishAnswer(req Request, schema *jsonschema.Resolved, resp *Response) error {
	if req.Schema == nil && !req.JSON {
		return nil
	}
	if resp.Stop == StopMaxTokens {
		return fmt.Errorf("%w: the answer was cut at maxTokens (%d)", ErrInvalidAnswer, req.MaxTokens)
	}
	if resp.Stop == StopToolUse && strings.TrimSpace(resp.Text) == "" {
		return nil // a tool loop step: the JSON answer comes at its end
	}
	v, err := ParseJSONAnswer(resp.Text)
	if err != nil {
		return err
	}
	if req.Schema != nil {
		if err := validateResolved(schema, v); err != nil {
			return err
		}
	}
	resp.JSON = v
	return nil
}

// Models implements Client (no budget: it spends no tokens).
func (m *meteredClient) Models(ctx context.Context) ([]ModelInfo, error) {
	return m.p.models(ctx)
}

// Embed implements Client.
func (m *meteredClient) Embed(ctx context.Context, model string, inputs []string) ([]Embedding, Usage, error) {
	if model == "" {
		return nil, Usage{}, ErrNoModel
	}
	if len(inputs) == 0 {
		return nil, Usage{}, nil
	}
	if err := m.checkBudget(); err != nil {
		return nil, Usage{}, err
	}
	embs, usage, err := m.p.embed(ctx, model, inputs)
	m.record(usage)
	if err != nil {
		return nil, usage, err
	}
	return embs, usage, nil
}

// Transcribe implements Client.
func (m *meteredClient) Transcribe(ctx context.Context, model string, audio Media, language string) (string, error) {
	if model == "" {
		return "", ErrNoModel
	}
	if err := m.checkBudget(); err != nil {
		return "", err
	}
	text, usage, err := m.p.transcribe(ctx, model, audio, language)
	m.record(usage)
	return text, err
}

// sharedTransport is the transport of every client: one connection pool for
// the process (the nodes build a client per call). Private and loopback
// addresses are allowed: the endpoint is the admin's (agents only pick a
// connection by name).
//
// It sets no ResponseHeaderTimeout: an OpenAI-compatible answer is not
// streamed, so its headers arrive only once the whole answer is generated
// (minutes on a slow local model, a long reasoning run or a long
// transcription). The caller's context, bounded by the node's timeout, is
// what ends a call; it ends it as a permanent error (requestError), where
// a transport limit would be a transient one and the runtime would pay for
// the same generation again.
var sharedTransport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	TLSHandshakeTimeout:   10 * time.Second,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          20,
	IdleConnTimeout:       90 * time.Second,
	ExpectContinueTimeout: time.Second,
}

// newHTTPClient returns a client's HTTP client: no overall timeout (the
// caller's context bounds a call: answers stream, or are generated before
// their headers are sent, for minutes).
func newHTTPClient() *http.Client {
	return &http.Client{Transport: sharedTransport}
}

// errBodyTooLarge is a response body over its limit.
var errBodyTooLarge = errors.New("the LLM provider's answer is too large")

// cappedBody reads at most n bytes of a body, then fails.
type cappedBody struct {
	r    io.ReadCloser
	left int64
}

func (b *cappedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		// One byte more tells a body of exactly the limit from a longer one.
		var one [1]byte
		if n, _ := b.r.Read(one[:]); n > 0 {
			return 0, errBodyTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.r.Read(p)
	b.left -= int64(n)
	return n, err
}

func (b *cappedBody) Close() error { return b.r.Close() }

// capBody limits resp's body to n bytes.
func capBody(resp *http.Response, n int64) {
	if resp != nil && resp.Body != nil {
		resp.Body = &cappedBody{r: resp.Body, left: n}
	}
}

// transientStatus lists the statuses worth retrying (rate limits, overload,
// timeouts, gateways).
var transientStatus = map[int]bool{408: true, 409: true, 425: true, 429: true, 500: true, 502: true, 503: true, 504: true, 529: true}

// statusError returns the error of a provider's error status: an *APIError
// whose message is cut and has every occurrence of key replaced, wrapped
// with node.Transient when the status is worth retrying.
func statusError(status int, message, key string) error {
	err := &APIError{Status: status, Message: truncate(strings.TrimSpace(redact(message, key)), maxErrorMessage)}
	if transientStatus[status] {
		return node.Transient(err)
	}
	return err
}

// requestError classifies an error of a request that got no answer: the
// caller's cancellation or deadline is returned as is (permanent: the node's
// timeout ran out), anything else is a network error (transient). The
// message has the key removed.
func requestError(ctx context.Context, err error, key string) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("LLM request: %w", ctxErr)
	}
	if errors.Is(err, errBodyTooLarge) {
		return errBodyTooLarge
	}
	return node.Transient(fmt.Errorf("LLM request failed: %s", truncate(redact(err.Error(), key), maxErrorMessage)))
}

// redact replaces every occurrence of key in s.
func redact(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, "<redacted>")
}
