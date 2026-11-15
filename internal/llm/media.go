package llm

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/Pegasus8/piworker/internal/node"
)

// imageTypes are the image media types every provider accepts.
var imageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// LoadMedia reads the media of a message value: a string, a list of
// strings, or []byte. A string is a data URL (data:<type>;base64,...), an
// http(s) URL or raw base64 (its type sniffed with http.DetectContentType).
// A URL is returned as is (Media.URL, not fetched) when allowURL is set:
// images are sent by URL, audio is fetched by its node through the nodes'
// guarded HTTP client; without allowURL a URL is an error. maxBytes bounds
// the decoded size of all the media together. Images must be PNG, JPEG, GIF
// or WebP. A nil value is no media. Errors never quote the data.
func LoadMedia(ctx context.Context, value any, maxBytes int64, allowURL bool) ([]Media, error) {
	var items []any
	switch v := value.(type) {
	case nil:
		return nil, nil
	case string, []byte:
		items = []any{v}
	case []string:
		for _, s := range v {
			items = append(items, s)
		}
	case []any:
		items = v
	default:
		return nil, fmt.Errorf("media must be a string (base64, a data URL or a URL), a list of strings or bytes, not %T", value)
	}
	var out []Media
	var total int64
	for i, item := range items {
		m, err := loadOne(item, maxBytes-total, allowURL)
		if err != nil {
			if len(items) > 1 {
				return nil, fmt.Errorf("media %d: %w", i+1, err)
			}
			return nil, err
		}
		total += int64(len(m.Data))
		out = append(out, m)
	}
	if sb := node.SandboxFrom(ctx); sb != nil && total > 0 {
		if err := sb.Charge(total); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// errMediaTooLarge is wrapped by LoadMedia when the media are over maxBytes.
var errMediaTooLarge = errors.New("media too large")

func loadOne(item any, remaining int64, allowURL bool) (Media, error) {
	var data []byte
	declared := ""
	switch v := item.(type) {
	case []byte:
		data = v
	case string:
		s := strings.TrimSpace(v)
		lower := strings.ToLower(s)
		switch {
		case s == "":
			return Media{}, errors.New("media is empty")
		case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
			if !allowURL {
				return Media{}, errors.New("a URL is not accepted here: pass the content (base64 or a data URL)")
			}
			return Media{URL: s}, nil
		case strings.HasPrefix(lower, "data:"):
			mediaType, payload, err := parseDataURL(s)
			if err != nil {
				return Media{}, err
			}
			declared = mediaType
			if data, err = decodeBase64(payload, remaining); err != nil {
				return Media{}, err
			}
		default:
			if strings.Contains(s, "://") {
				return Media{}, errors.New("only http(s) URLs are accepted")
			}
			var err error
			if data, err = decodeBase64(s, remaining); err != nil {
				return Media{}, err
			}
		}
	default:
		return Media{}, fmt.Errorf("media must be a string or bytes, not %T", item)
	}
	if int64(len(data)) > remaining {
		return Media{}, fmt.Errorf("%w (at most %d bytes)", errMediaTooLarge, remaining)
	}
	if len(data) == 0 {
		return Media{}, errors.New("media is empty")
	}
	mediaType := declared
	if mediaType == "" {
		mediaType = http.DetectContentType(data)
	}
	if mt, _, err := mime.ParseMediaType(mediaType); err == nil {
		mediaType = mt
	}
	if strings.HasPrefix(mediaType, "image/") && !imageTypes[mediaType] {
		return Media{}, fmt.Errorf("unsupported image type %s (PNG, JPEG, GIF or WebP)", mediaType)
	}
	return Media{MediaType: mediaType, Data: data}, nil
}

// parseDataURL splits a base64 data URL into its media type and payload.
func parseDataURL(s string) (string, string, error) {
	comma := strings.IndexByte(s, ',')
	if comma < 0 {
		return "", "", errors.New("invalid data URL")
	}
	meta := s[len("data:"):comma]
	if !strings.HasSuffix(strings.ToLower(meta), ";base64") {
		return "", "", errors.New("a data URL must be base64 (data:<type>;base64,...)")
	}
	mediaType := strings.ToLower(strings.TrimSpace(meta[:len(meta)-len(";base64")]))
	if mt, _, err := mime.ParseMediaType(mediaType); err == nil {
		mediaType = mt
	}
	return mediaType, s[comma+1:], nil
}

// decodeBase64 decodes standard or URL-safe base64, padded or not, refusing
// before decoding what would exceed maxBytes.
func decodeBase64(s string, maxBytes int64) ([]byte, error) {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	if int64(len(s))/4*3 > maxBytes+3 {
		return nil, fmt.Errorf("%w (at most %d bytes)", errMediaTooLarge, maxBytes)
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if data, err := enc.DecodeString(s); err == nil {
			return data, nil
		}
	}
	return nil, errors.New("media is not base64, a data URL or an http(s) URL")
}
