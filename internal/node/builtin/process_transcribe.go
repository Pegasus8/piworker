package builtin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// TranscribeProcess turns the audio of a message (a voice note, a
// recording) into text through an OpenAI-compatible transcription API
// (docs/design/llm.md §3): OpenAI's Whisper, or a local server offering
// /audio/transcriptions.
type TranscribeProcess struct {
	*node.BaseNode
	llmSettings

	audio       string // message path
	language    string // ISO-639-1 code ("" = detected)
	resultField string

	// fetch downloads audio URLs. It refuses private and loopback
	// addresses: the URL comes from message data (a chat's voice note), so
	// without the guard a message could make PiWorker read an internal
	// service and send what it answers to the provider. It has no timeout
	// of its own: the node's timeout bounds the whole call.
	fetch *http.Client
}

// transcribeDefaultModel is the model asked when neither the node nor the
// connection names one (OpenAI's).
const transcribeDefaultModel = "whisper-1"

// transcribeProviders names the connections that offer transcription.
const transcribeProviders = "OpenAI, or a local server with a Whisper model (LocalAI, Speaches)"

// languageRe is an ISO-639-1 code (Whisper's), or a three-letter ISO-639
// code some servers take.
var languageRe = regexp.MustCompile(`^[a-z]{2,3}$`)

// NewTranscribeProcess creates a TranscribeProcess from configuration.
func NewTranscribeProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	cfg := base.GetConfig()
	settings, err := parseLLMSettings(base, llmDefaultTimeout)
	if err != nil {
		return nil, err
	}
	p := &TranscribeProcess{BaseNode: base, llmSettings: settings, fetch: newGuardedHTTPClient(0, false)}
	if v, present := cfg["audio"]; !present || v == nil {
		p.audio = "payload"
	} else if p.audio, err = textSetting(cfg, "audio"); err != nil {
		return nil, err
	} else if p.audio = strings.TrimSpace(p.audio); p.audio == "" {
		return nil, errors.New("audio is required: the message path of the audio, e.g. payload or payload.voice")
	}
	if err := checkMessagePath("audio", p.audio); err != nil {
		return nil, err
	}
	if p.language, err = textSetting(cfg, "language"); err != nil {
		return nil, err
	}
	p.language = strings.ToLower(strings.TrimSpace(p.language))
	if p.language != "" && !languageRe.MatchString(p.language) {
		return nil, errors.New("language must be an ISO-639-1 code such as en or es (or empty to detect it)")
	}
	if p.resultField, err = parseResultField(base); err != nil {
		return nil, err
	}
	return p, nil
}

// Process transcribes the message's audio and passes the message on with
// the text.
func (p *TranscribeProcess) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	var out *types.Message
	err := p.run(ctx, func(ctx context.Context) error {
		s, err := p.open(ctx, transcribeDefaultModel)
		if err != nil {
			return err
		}
		if err := unsupportedOnAnthropic(s.conn, "transcription", transcribeProviders); err != nil {
			return err
		}
		audio, err := p.loadAudio(ctx, msg)
		if err != nil {
			return err
		}
		text, err := s.client.Transcribe(ctx, s.model, audio, p.language)
		if err != nil {
			return err
		}
		out = msg.Clone()
		llm.SetResult(out, p.resultField, text)
		// Transcription spends no tokens: one request (llm.Client).
		out.SetMeta("llm", map[string]any{"connection": s.conn.Name, "model": s.model, "requests": int64(1)})
		out.SourcePort = "output"
		return nil
	})
	if err != nil {
		return nil, err
	}
	return []*types.Message{out}, nil
}

// loadAudio reads the one audio at the node's message path: inline
// (base64, a data URL, bytes) or an http(s) URL it downloads. Errors name
// the path, never the data or the URL.
func (p *TranscribeProcess) loadAudio(ctx context.Context, msg *types.Message) (llm.Media, error) {
	fail := func(err error) (llm.Media, error) {
		return llm.Media{}, fmt.Errorf("audio (%s): %w", p.audio, err)
	}
	v, ok := llm.MessageValue(msg, p.audio)
	if !ok || v == nil {
		return fail(errors.New("the message has no audio there"))
	}
	media, err := llm.LoadMedia(ctx, v, llm.MaxAudioBytes, true)
	if err != nil {
		return fail(err)
	}
	switch len(media) {
	case 0:
		return fail(errors.New("the message has no audio there"))
	case 1:
	default:
		return fail(fmt.Errorf("one audio is transcribed at a time, not a list of %d", len(media)))
	}
	m := media[0]
	if m.URL != "" {
		if m, err = fetchAudio(ctx, p.fetch, m.URL, llm.MaxAudioBytes); err != nil {
			return fail(err)
		}
	}
	if err := checkAudioType(m.MediaType); err != nil {
		return fail(err)
	}
	return m, nil
}

// checkAudioType refuses what is clearly not audio (a text, a web page, an
// image), which a wrong message path or URL gives; anything else goes to
// the provider, which knows its formats.
func checkAudioType(mediaType string) error {
	if strings.HasPrefix(mediaType, "text/") || strings.HasPrefix(mediaType, "image/") {
		return fmt.Errorf("it is not audio (%s)", mediaType)
	}
	return nil
}

// audioExtensionTypes names the media type of an audio file by its
// extension, for servers that answer application/octet-stream (Telegram's
// file server does) with content http.DetectContentType does not know.
var audioExtensionTypes = map[string]string{
	".mp3": "audio/mpeg", ".mpga": "audio/mpeg", ".mpeg": "audio/mpeg",
	".m4a": "audio/mp4", ".mp4": "audio/mp4", ".aac": "audio/aac",
	".wav": "audio/wav", ".ogg": "audio/ogg", ".oga": "audio/ogg", ".opus": "audio/ogg",
	".webm": "audio/webm", ".flac": "audio/flac",
}

// blockedAddress is the text of newGuardedHTTPClient's refusal of a
// private or loopback address (its allowPrivate hint does not apply here).
const (
	blockedAddress     = "non-public address"
	allowPrivateAdvice = " (set allowPrivate to allow)"
)

// fetchAudio downloads the audio at rawURL with client, reading at most
// maxBytes. Its errors never quote the URL (a chat's file URL may carry a
// bot token): HTTP statuses, network errors with the URL replaced by its
// scheme and host. Rate limits, server errors and network errors are
// transient; a refused private address, other statuses and a body that is
// too large are not.
func fetchAudio(ctx context.Context, client *http.Client, rawURL string, maxBytes int64) (llm.Media, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return llm.Media{}, errors.New("the audio URL is not a valid URL")
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return llm.Media{}, fmt.Errorf("fetching the audio: %w", ctxErr)
		}
		err = redactURLError(err, rawURL, displayURL(rawURL))
		text := strings.Replace(err.Error(), allowPrivateAdvice, "", 1)
		if strings.Contains(text, blockedAddress) {
			return llm.Media{}, fmt.Errorf("fetching the audio failed: %s: audio URLs must be public addresses", text)
		}
		return llm.Media{}, node.Transient(fmt.Errorf("fetching the audio failed: %s", text))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		err := fmt.Errorf("the audio URL answered HTTP %d", resp.StatusCode)
		if transientHTTPStatus(resp.StatusCode) {
			return llm.Media{}, node.Transient(err)
		}
		return llm.Media{}, err
	}
	tooLarge := fmt.Errorf("the audio is too large (at most %d MiB)", maxBytes>>20)
	if maxBytes < 1<<20 {
		tooLarge = fmt.Errorf("the audio is too large (at most %d bytes)", maxBytes)
	}
	if resp.ContentLength > maxBytes {
		return llm.Media{}, tooLarge
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return llm.Media{}, fmt.Errorf("fetching the audio: %w", ctxErr)
		}
		return llm.Media{}, node.Transient(errors.New("fetching the audio failed: the connection broke while reading it"))
	}
	if int64(len(data)) > maxBytes {
		return llm.Media{}, tooLarge
	}
	if len(data) == 0 {
		return llm.Media{}, errors.New("the audio URL answered no content")
	}
	return llm.Media{MediaType: audioMediaType(resp.Header.Get("Content-Type"), req.URL, data), Data: data}, nil
}

// audioMediaType is the media type of downloaded audio: the declared one
// when it says something, else sniffed from the content, else guessed from
// the URL's extension.
func audioMediaType(declared string, u *url.URL, data []byte) string {
	mediaType := ""
	if mt, _, err := mime.ParseMediaType(declared); err == nil {
		mediaType = strings.ToLower(mt)
	}
	if mediaType == "" || mediaType == "application/octet-stream" || mediaType == "binary/octet-stream" {
		mediaType = "application/octet-stream"
		if mt, _, err := mime.ParseMediaType(http.DetectContentType(data)); err == nil {
			mediaType = mt
		}
	}
	if mediaType == "application/octet-stream" {
		if t, ok := audioExtensionTypes[strings.ToLower(path.Ext(u.Path))]; ok {
			mediaType = t
		}
	}
	return mediaType
}

// transientHTTPStatus reports the statuses worth retrying: timeouts, rate
// limits and server errors.
func transientHTTPStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

// Ports returns the port definitions.
func (p *TranscribeProcess) Ports() (inputs []types.Port, outputs []types.Port) {
	return llmInputs, llmOutputs
}

// Validate checks the configuration (the constructor already did).
func (p *TranscribeProcess) Validate() error { return nil }

func transcribeConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"connection": llmSchemaConnection(),
			"model":      llmSchemaModel("Transcription model, e.g. whisper-1 (empty: the connection's default model, else whisper-1)"),
			"audio": {Type: "string", Title: "Audio",
				Description:  "Message path of the audio, e.g. payload or payload.voice: base64, a data URL, bytes or an http(s) URL (at most 25 MiB)",
				Default:      "payload",
				AgentVisible: true},
			"language": {Type: "string", Title: "Language",
				Description:  "Language spoken, as an ISO-639-1 code such as en or es (empty: detected); naming it improves accuracy",
				AgentVisible: true},
			"resultField": llmSchemaResultField(),
			"timeout":     llmSchemaTimeout(llmDefaultTimeout),
		},
		Required: []string{"connection", "audio"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (p *TranscribeProcess) GetConfigSchema() node.ConfigSchema { return transcribeConfigSchema() }

// TranscribeProcessInfo returns the node type info for registration.
func TranscribeProcessInfo() node.NodeTypeInfo {
	schema := transcribeConfigSchema()
	return node.NodeTypeInfo{
		Type:        "process-transcribe",
		Name:        "Transcribe Audio",
		Description: "Turn speech in an audio message into text",
		Documentation: `## Transcribe Audio

Sends the audio of a message (a voice note, a recording) to a speech-to-text
model and passes the message on with the text. It needs an OpenAI-compatible
**LLM connection** that offers transcription: OpenAI (Whisper), or a local
server with a Whisper model such as LocalAI or Speaches. Anthropic has no
transcription API.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| connection | string | - | LLM connection (required; OpenAI-compatible) |
| model | string | connection's, else whisper-1 | Transcription model |
| audio | string | payload | Message path of the audio (required) |
| language | string | - | Spoken language as an ISO-639-1 code (` + "`en`" + `, ` + "`es`" + `; empty: detected) |
| resultField | string | - | Payload field for the text (empty: replace the payload) |
| timeout | integer | 120 | Longest time for the whole call, in seconds (1-600) |

If the connection's default model is a chat model, set **model** to the
transcription model (` + "`whisper-1`" + ` on OpenAI).

## Audio

**audio** names where the message holds the audio: base64, a data URL
(` + "`data:audio/ogg;base64,...`" + `), bytes, or an http(s) URL the node downloads.
Downloads only reach public addresses, never the local network. At most
25 MiB; common formats are MP3, MP4/M4A, WAV, OGG/Opus, WebM and FLAC.

## Output

The message with the transcribed text, as the payload or in
` + "`resultField`" + `. **meta.llm** holds the connection, the model and
` + "`requests`" + ` (transcription counts requests, not tokens).

## Example

Transcribe a voice note before answering it with an **LLM Prompt**:

` + "```" + `
audio: payload.voiceUrl
language: en
resultField: text
` + "```" + `

## Errors

An audio the provider cannot read, a URL that does not answer audio, a
private address, a spent daily limit and a timeout fail the message. Rate
limits, overloaded providers and network errors are transient: the flow's
retries apply to them.

## Privacy

The audio is sent to the connection's provider. With a cloud provider it
leaves the device; a local server keeps it on your network.
`,
		Category: types.NodeCategoryProcessing,
		Risk:     node.RiskExternal,
		Inputs:   llmInputs,
		Outputs:  llmOutputs,
		Config:   &schema,
		Icon:     "audio-lines",
	}
}

func init() {
	info := TranscribeProcessInfo()
	if err := node.Register(info, NewTranscribeProcess); err != nil {
		panic("failed to register transcribe process: " + err.Error())
	}
}
