package webhook

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serve(hub *Hub, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	hub.HandlerFunc()(w, r)
	return w
}

func TestHubMapsHandlerResultToStatus(t *testing.T) {
	cases := map[Result]int{
		Accepted:         http.StatusOK,
		Unauthorized:     http.StatusUnauthorized,
		MethodNotAllowed: http.StatusMethodNotAllowed,
		Dropped:          http.StatusServiceUnavailable,
	}
	for result, status := range cases {
		hub := NewHub()
		require.NoError(t, hub.Register("h", func(Request) Result { return result }))
		w := serve(hub, httptest.NewRequest("POST", PathPrefix+"h", strings.NewReader("x")))
		assert.Equal(t, status, w.Code, "result %v", result)
	}
}

func TestHubRejectsOversizedBody(t *testing.T) {
	hub := NewHub()
	called := false
	require.NoError(t, hub.Register("h", func(Request) Result { called = true; return Accepted }))

	w := serve(hub, httptest.NewRequest("POST", PathPrefix+"h", strings.NewReader(strings.Repeat("a", maxBodyBytes+1))))
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.False(t, called, "a truncated body must not be delivered as if complete")

	w = serve(hub, httptest.NewRequest("POST", PathPrefix+"h", strings.NewReader(strings.Repeat("a", maxBodyBytes))))
	assert.Equal(t, http.StatusOK, w.Code, "exactly the limit is fine")
}

func TestHubBodyReadErrorIs400(t *testing.T) {
	hub := NewHub()
	called := false
	require.NoError(t, hub.Register("h", func(Request) Result { called = true; return Accepted }))
	r := httptest.NewRequest("POST", PathPrefix+"h", io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF)))
	w := serve(hub, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, called)
}

func TestHubUnknownPathIs404WithoutReadingBody(t *testing.T) {
	hub := NewHub()
	r := httptest.NewRequest("POST", PathPrefix+"nope", io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF)))
	assert.Equal(t, http.StatusNotFound, serve(hub, r).Code)
}

func TestHubStripsCredentialsFromPayload(t *testing.T) {
	hub := NewHub()
	var got Request
	require.NoError(t, hub.Register("h", func(r Request) Result { got = r; return Accepted }))

	r := httptest.NewRequest("POST", PathPrefix+"h?token=qs-secret&keep=1", strings.NewReader(`{}`))
	r.Header.Set("Cookie", "piworker_session=abc")
	r.Header.Set("Authorization", "Bearer xyz")
	r.Header.Set("X-Webhook-Token", "hdr-secret")
	r.Header.Set("X-PiWorker-CSRF", "csrf")
	r.Header.Set("X-Github-Event", "push")
	require.Equal(t, http.StatusOK, serve(hub, r).Code)

	for _, h := range []string{"Cookie", "Authorization", "X-Webhook-Token", "X-Piworker-Csrf"} {
		assert.NotContains(t, got.Headers, h)
	}
	assert.Equal(t, "push", got.Headers["X-Github-Event"])
	assert.NotContains(t, got.Query, "token")
	assert.Equal(t, []string{"1"}, got.Query["keep"])
	assert.Equal(t, "hdr-secret", got.Token, "header token wins and is passed out of band")

	r = httptest.NewRequest("POST", PathPrefix+"h?token=qs-secret", nil)
	require.Equal(t, http.StatusOK, serve(hub, r).Code)
	assert.Equal(t, "qs-secret", got.Token)
}
