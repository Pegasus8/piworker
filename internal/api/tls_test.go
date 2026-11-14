package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/tlsca"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCAPEM = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"

func tlsRouter(h *TLSHandler) *mux.Router {
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func TestCACertificateDownload(t *testing.T) {
	h := NewTLSHandler("auto", func() tlsca.Status { return tlsca.Status{} }, []byte(testCAPEM))
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := httptest.NewRecorder()
		tlsRouter(h).ServeHTTP(w, httptest.NewRequest(method, CACertPath, nil))
		require.Equal(t, http.StatusOK, w.Code, method)
		assert.Equal(t, "application/x-x509-ca-cert", w.Header().Get("Content-Type"))
		assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
		assert.Contains(t, w.Header().Get("Content-Disposition"), `filename="piworker-ca.crt"`)
		if method == http.MethodGet {
			assert.Equal(t, testCAPEM, w.Body.String())
		}
	}
}

func TestCACertificateMissingWithoutLocalCA(t *testing.T) {
	for _, h := range []*TLSHandler{
		NewTLSHandler("off", nil, nil),
		NewTLSHandler("custom", func() tlsca.Status { return tlsca.Status{} }, nil),
	} {
		w := httptest.NewRecorder()
		tlsRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, CACertPath, nil))
		assert.Equal(t, http.StatusNotFound, w.Code)
	}
}

func TestTLSStatus(t *testing.T) {
	notAfter := time.Date(2027, 12, 10, 8, 0, 0, 0, time.UTC)
	h := NewTLSHandler("auto", func() tlsca.Status {
		return tlsca.Status{CAFingerprint: "SHA256:AA:BB", SANs: []string{"localhost", "192.168.1.10"}, NotAfter: notAfter}
	}, []byte(testCAPEM))
	w := httptest.NewRecorder()
	tlsRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/tls", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Mode          string     `json:"mode"`
			CAFingerprint string     `json:"caFingerprint"`
			SANs          []string   `json:"sans"`
			NotAfter      *time.Time `json:"notAfter"`
			CACertURL     string     `json:"caCertUrl"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.True(t, body.Success)
	assert.Equal(t, "auto", body.Data.Mode)
	assert.Equal(t, "SHA256:AA:BB", body.Data.CAFingerprint)
	assert.Equal(t, []string{"localhost", "192.168.1.10"}, body.Data.SANs)
	require.NotNil(t, body.Data.NotAfter)
	assert.True(t, notAfter.Equal(*body.Data.NotAfter))
	assert.Equal(t, CACertPath, body.Data.CACertURL)
}

func TestTLSStatusWhenOff(t *testing.T) {
	w := httptest.NewRecorder()
	tlsRouter(NewTLSHandler("off", nil, nil)).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/tls", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"success":true,"data":{"mode":"off","caFingerprint":"","sans":[],"notAfter":null,"caCertUrl":"","httpsPort":null,"httpPort":null}}`, w.Body.String())
}

// The trust page builds its links (HTTPS address, CA download over plain HTTP)
// from the ports PiWorker really listens on; null while unknown or not served.
func TestTLSStatusReportsListenerPorts(t *testing.T) {
	type ports struct {
		HTTPSPort *int `json:"httpsPort"`
		HTTPPort  *int `json:"httpPort"`
	}
	read := func(h *TLSHandler) ports {
		w := httptest.NewRecorder()
		tlsRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/tls", nil))
		require.Equal(t, http.StatusOK, w.Code)
		var body struct {
			Data ports `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body.Data
	}
	intp := func(n int) *int { return &n }

	h := NewTLSHandler("auto", func() tlsca.Status { return tlsca.Status{} }, []byte(testCAPEM))
	assert.Equal(t, ports{}, read(h), "unknown before the listeners are bound")
	h.SetPorts(9443, 9080)
	assert.Equal(t, ports{HTTPSPort: intp(9443), HTTPPort: intp(9080)}, read(h))
	h.SetPorts(8443, 0)
	assert.Equal(t, ports{HTTPSPort: intp(8443)}, read(h), "http_addr disabled")

	off := NewTLSHandler("off", nil, nil)
	off.SetPorts(0, 8080)
	assert.Equal(t, ports{HTTPPort: intp(8080)}, read(off), "TLS off: plain HTTP only")
}

// A CA close to expiry is reported to the editor too.
func TestTLSStatusCarriesTheCAWarning(t *testing.T) {
	h := NewTLSHandler("auto", func() tlsca.Status { return tlsca.Status{Warning: "The local CA expires soon"} }, []byte(testCAPEM))
	w := httptest.NewRecorder()
	tlsRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/tls", nil))
	var body struct {
		Data TLSInfo `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "The local CA expires soon", body.Data.Warning)
}

// With native HTTPS the session cookie must be Secure even if a request reached
// the handler without TLS (defense in depth: the HTTP listener never serves /api).
func TestSecureCookiesOption(t *testing.T) {
	router, h, _ := sessionTestRouter(t, true)
	WithSecureCookies(true)(h)
	cookie := loginCookie(t, router)
	assert.True(t, cookie.Secure)
	w := authRequest(router, "POST", "/api/auth/logout", nil, cookie)
	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, w.Result().Cookies()[0].Secure, "the clearing cookie keeps the same attributes")
}
