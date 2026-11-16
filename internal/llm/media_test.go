package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

// pngBytes is a 1x1 PNG.
var pngBytes, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")

func TestLoadMediaForms(t *testing.T) {
	ctx := context.Background()
	b64 := base64.StdEncoding.EncodeToString(pngBytes)

	got, err := LoadMedia(ctx, "data:image/png;base64,"+b64, 1<<20, false)
	if err != nil || len(got) != 1 || got[0].MediaType != "image/png" || !bytes.Equal(got[0].Data, pngBytes) {
		t.Fatalf("data URL: %+v %v", got, err)
	}
	got, err = LoadMedia(ctx, b64, 1<<20, false)
	if err != nil || len(got) != 1 || got[0].MediaType != "image/png" {
		t.Fatalf("raw base64 sniffed: %+v %v", got, err)
	}
	got, err = LoadMedia(ctx, pngBytes, 1<<20, false)
	if err != nil || len(got) != 1 || got[0].MediaType != "image/png" {
		t.Fatalf("bytes: %+v %v", got, err)
	}
	got, err = LoadMedia(ctx, []any{"https://example.com/a.jpg", b64}, 1<<20, true)
	if err != nil || len(got) != 2 || got[0].URL != "https://example.com/a.jpg" || got[0].Data != nil || got[1].MediaType != "image/png" {
		t.Fatalf("list with a URL: %+v %v", got, err)
	}
	got, err = LoadMedia(ctx, []string{b64}, 1<<20, false)
	if err != nil || len(got) != 1 {
		t.Fatalf("[]string: %+v %v", got, err)
	}
	got, err = LoadMedia(ctx, nil, 1<<20, false)
	if err != nil || len(got) != 0 {
		t.Fatalf("nil: %+v %v", got, err)
	}
	// Audio passes (only image types are restricted).
	mp3 := append([]byte("ID3"), make([]byte, 64)...)
	got, err = LoadMedia(ctx, mp3, 1<<20, false)
	if err != nil || len(got) != 1 || got[0].MediaType != "audio/mpeg" {
		t.Fatalf("audio: %+v %v", got, err)
	}
}

func TestLoadMediaRejects(t *testing.T) {
	ctx := context.Background()
	b64 := base64.StdEncoding.EncodeToString(pngBytes)
	cases := map[string]struct {
		value    any
		max      int64
		allowURL bool
	}{
		"url not allowed":    {"https://example.com/a.png", 1 << 20, false},
		"other scheme":       {"ftp://example.com/a.png", 1 << 20, true},
		"too large":          {b64, 10, false},
		"too large total":    {[]any{b64, b64}, int64(len(pngBytes)) + 10, false},
		"not base64":         {"hello world!", 1 << 20, false},
		"bad data URL":       {"data:image/png,notbase64", 1 << 20, false},
		"unsupported image":  {"data:image/tiff;base64," + b64, 1 << 20, false},
		"not a string":       {42, 1 << 20, false},
		"list of non-string": {[]any{1}, 1 << 20, false},
		"empty string":       {"", 1 << 20, false},
	}
	for name, c := range cases {
		if _, err := LoadMedia(ctx, c.value, c.max, c.allowURL); err == nil {
			t.Errorf("%s: expected an error", name)
		} else if strings.Contains(err.Error(), b64[:20]) {
			t.Errorf("%s: error quotes the data: %v", name, err)
		}
	}
}
