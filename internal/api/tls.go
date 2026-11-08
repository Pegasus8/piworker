package api

import (
	"net/http"
	"time"

	"github.com/Pegasus8/piworker/internal/tlsca"
	"github.com/gorilla/mux"
)

// CACertPath serves the local CA certificate. It is public, answered by the
// plain HTTP listener too and, like webhooks, reachable under any Host name,
// so a device can fetch the CA before trusting it.
const CACertPath = "/piworker-ca.crt"

// TLSInfo is the GET /api/tls payload: how this server is reached over HTTPS
// and what a client must trust.
type TLSInfo struct {
	Mode          string     `json:"mode"`
	CAFingerprint string     `json:"caFingerprint"`
	SANs          []string   `json:"sans"`
	NotAfter      *time.Time `json:"notAfter"`
	CACertURL     string     `json:"caCertUrl"`
}

// TLSHandler serves the CA certificate download and the TLS status.
type TLSHandler struct {
	mode   string
	status func() tlsca.Status
	caPEM  []byte
}

// NewTLSHandler creates a handler for the given tls.mode. status is nil when
// TLS is off; caPEM is nil without a local CA (custom or off).
func NewTLSHandler(mode string, status func() tlsca.Status, caPEM []byte) *TLSHandler {
	return &TLSHandler{mode: mode, status: status, caPEM: caPEM}
}

// RegisterRoutes registers the CA download (public) and /api/tls (behind the
// API's authentication).
func (h *TLSHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc(CACertPath, h.CACert).Methods(http.MethodGet, http.MethodHead)
	r.HandleFunc("/api/tls", h.Status).Methods(http.MethodGet)
}

// CACert returns the CA certificate in PEM form. The x509-ca-cert media type
// lets Firefox, iOS and Android offer their certificate import flow directly.
func (h *TLSHandler) CACert(w http.ResponseWriter, r *http.Request) {
	if len(h.caPEM) == 0 {
		http.Error(w, "This server has no local certificate authority (tls.mode is not auto).", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", `inline; filename="piworker-ca.crt"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(h.caPEM)
}

// Status returns the TLS mode, CA fingerprint and server certificate details.
// GET /api/tls
func (h *TLSHandler) Status(w http.ResponseWriter, r *http.Request) {
	info := TLSInfo{Mode: h.mode, SANs: []string{}}
	if h.status != nil {
		st := h.status()
		info.CAFingerprint = st.CAFingerprint
		if st.SANs != nil {
			info.SANs = st.SANs
		}
		if !st.NotAfter.IsZero() {
			info.NotAfter = &st.NotAfter
		}
	}
	if len(h.caPEM) > 0 {
		info.CACertURL = CACertPath
	}
	w.Header().Set("Cache-Control", "no-store")
	writeSuccess(w, http.StatusOK, info, "")
}
