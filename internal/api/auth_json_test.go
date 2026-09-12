package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialsRejectAmbiguousJSON(t *testing.T) {
	for name, body := range map[string]string{
		"duplicate field": `{"username":"first","username":"second","password":"password"}`,
		"trailing value":  `{"username":"admin","password":"password"} {}`,
		"invalid utf8":    "{\"username\":\"admin\",\"password\":\"\xff\"}",
	} {
		t.Run(name, func(t *testing.T) {
			_, h, _ := sessionTestRouter(t, false)
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(body))
			_, ok := h.readCredentials(w, r)
			require.False(t, ok)
			require.Equal(t, 400, w.Code)
		})
	}
}

func TestCredentialsAcceptDocumentedJSON(t *testing.T) {
	_, h, _ := sessionTestRouter(t, false)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(" {\"username\":\" admin \",\"password\":\"contraseña\"} \n"))
	req, ok := h.readCredentials(w, r)
	require.True(t, ok)
	require.Equal(t, "admin", req.Username)
	require.Equal(t, "contraseña", req.Password)
}
