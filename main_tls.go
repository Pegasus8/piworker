package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/Pegasus8/piworker/internal/api"
	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/tlsca"
	"github.com/Pegasus8/piworker/internal/webhook"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// tlsSource is a certificate source for the HTTPS listener: the local CA
// (tls.mode = "auto") or the administrator's files ("custom").
type tlsSource interface {
	TLSConfig() *tls.Config
	Status() tlsca.Status
	CACertPEM() []byte
}

// newTLSSource builds the certificate source for cfg; nil when TLS is off.
func newTLSSource(cfg config.Config) (tlsSource, error) {
	switch cfg.TLS.Mode {
	case config.TLSModeAuto:
		m, err := tlsca.NewManager(tlsca.Options{Dir: cfg.TLSDir(), ExtraHosts: cfg.TLS.Hosts})
		if err != nil {
			return nil, err
		}
		return m, nil
	case config.TLSModeCustom:
		fc, err := tlsca.LoadFileCertificate(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			return nil, err
		}
		return fc, nil
	}
	return nil, nil
}

// tlsRoutes registers the CA download and GET /api/tls.
func tlsRoutes(mode string, src tlsSource) routerOption {
	return func(r *mux.Router) {
		var status func() tlsca.Status
		var caPEM []byte
		if src != nil {
			status, caPEM = src.Status, src.CACertPEM()
		}
		api.NewTLSHandler(mode, status, caPEM).RegisterRoutes(r)
	}
}

// regenerateCA replaces the local CA (piworker -tls-regenerate-ca), issues a
// server certificate signed by it and returns its fingerprint.
func regenerateCA(cfg config.Config) (string, error) {
	if err := tlsca.RegenerateCA(cfg.TLSDir()); err != nil {
		return "", err
	}
	m, err := tlsca.NewManager(tlsca.Options{Dir: cfg.TLSDir(), ExtraHosts: cfg.TLS.Hosts})
	if err != nil {
		return "", err
	}
	return m.CAFingerprint(), nil
}

// serverSet holds the main listener (HTTPS, or HTTP when TLS is off) and the
// optional plain-HTTP listener that redirects to it.
type serverSet struct {
	main, redirect     *http.Server
	mainLn, redirectLn net.Listener
	tls                bool
}

// listen binds the configured addresses. tlsConf is nil when TLS is off; then
// addr serves plain HTTP and http_addr is unused.
func listen(cfg config.Config, app http.Handler, tlsConf *tls.Config) (*serverSet, error) {
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return nil, err
	}
	s := &serverSet{main: newHTTPServer(cfg.Addr, app), mainLn: ln}
	if tlsConf == nil {
		return s, nil
	}
	s.tls = true
	s.main.TLSConfig = tlsConf
	if cfg.HTTPAddr == "" {
		return s, nil
	}
	rln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		ln.Close()
		return nil, fmt.Errorf("http_addr: %w", err)
	}
	_, httpsPort, _ := net.SplitHostPort(ln.Addr().String())
	s.redirect = newHTTPServer(cfg.HTTPAddr, redirectHandler(httpsPort, app, cfg.WebhooksHTTP))
	s.redirectLn = rln
	return s, nil
}

// serve starts every server; unexpected errors are sent to errCh, which needs
// room for two.
func (s *serverSet) serve(errCh chan<- error) {
	run := func(serve func() error) {
		if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}
	if s.tls {
		go run(func() error { return s.main.ServeTLS(s.mainLn, "", "") })
	} else {
		go run(func() error { return s.main.Serve(s.mainLn) })
	}
	if s.redirect != nil {
		go run(func() error { return s.redirect.Serve(s.redirectLn) })
	}
}

func (s *serverSet) all() []*http.Server {
	if s.redirect == nil {
		return []*http.Server{s.main}
	}
	return []*http.Server{s.main, s.redirect}
}

// redirectHandler answers the plain-HTTP listener: everything is
// 308-redirected to HTTPS (keeping method and body), except the CA download
// and, when webhooksHTTP is set, webhooks.
func redirectHandler(httpsPort string, app http.Handler, webhooksHTTP bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The router may clean dot segments, so decide on the cleaned path.
		clean := path.Clean("/" + r.URL.Path)
		if r.URL.Path == api.CACertPath || (webhooksHTTP && strings.HasPrefix(clean, webhook.PathPrefix) && strings.HasPrefix(r.URL.Path, webhook.PathPrefix)) {
			app.ServeHTTP(w, r)
			return
		}
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		if host == "" {
			http.Error(w, "Missing Host header", http.StatusBadRequest)
			return
		}
		authority := net.JoinHostPort(host, httpsPort)
		if httpsPort == "443" {
			authority = strings.TrimSuffix(net.JoinHostPort(host, "0"), ":0")
		}
		http.Redirect(w, r, "https://"+authority+r.URL.RequestURI(), http.StatusPermanentRedirect)
	})
}

// publicURL is the address printed for browsers on the local network:
// the mDNS name of this host (or localhost) and the listening port.
func publicURL(scheme, hostname, port string) string {
	host := "localhost"
	if short, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(hostname)), "."); short != "" {
		host = short + ".local"
	}
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		return scheme + "://" + host
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

// announceListeners logs where PiWorker is reachable and, with a local CA,
// prints the URL and CA fingerprint on stdout for installers and humans.
func announceListeners(s *serverSet, src tlsSource) {
	hostname, _ := os.Hostname()
	_, port, _ := net.SplitHostPort(s.mainLn.Addr().String())
	if !s.tls {
		log.Info().Str("addr", s.mainLn.Addr().String()).Str("url", publicURL("http", hostname, port)).Msg("HTTP server started (TLS off)")
		return
	}
	url := publicURL("https", hostname, port)
	st := src.Status()
	ev := log.Info().Str("addr", s.mainLn.Addr().String()).Str("url", url).Strs("names", st.SANs).Time("certificateExpires", st.NotAfter)
	if st.CAFingerprint != "" {
		ev = ev.Str("caFingerprint", st.CAFingerprint)
	}
	ev.Msg("HTTPS server started")
	if s.redirect != nil {
		log.Info().Str("addr", s.redirectLn.Addr().String()).Msg("HTTP redirect listener started")
	}
	if st.CAFingerprint != "" {
		fmt.Printf("PiWorker HTTPS: %s\nPiWorker CA fingerprint: %s\nTrust this device: %s/trust\n", url, st.CAFingerprint, url)
	}
}

// watchTLS keeps the certificate current until ctx ends: auto mode re-checks
// names, addresses and expiry every interval; SIGHUP refreshes immediately
// (auto) or reloads the certificate files (custom). SIGHUP is captured before
// watchTLS returns and released once the returned channel is closed. Without a
// certificate source nothing is watched and the channel is already closed.
func watchTLS(ctx context.Context, src tlsSource, interval time.Duration) <-chan struct{} {
	done := make(chan struct{})
	if src == nil {
		close(done)
		return done
	}
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	ticker := time.NewTicker(interval)
	go func() {
		defer close(done)
		defer signal.Stop(hup)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if m, ok := src.(*tlsca.Manager); ok {
					refreshTLS(m)
				}
			case <-hup:
				switch s := src.(type) {
				case *tlsca.Manager:
					refreshTLS(s)
				case *tlsca.FileCertificate:
					if err := s.Reload(); err != nil {
						log.Error().Err(err).Msg("Cannot reload the TLS certificate; still serving the previous one")
					} else {
						log.Info().Time("certificateExpires", s.Status().NotAfter).Msg("TLS certificate reloaded")
					}
				}
			}
		}
	}()
	return done
}

func refreshTLS(m *tlsca.Manager) {
	before := m.Status().NotAfter
	if err := m.Refresh(); err != nil {
		log.Error().Err(err).Msg("Cannot refresh the TLS certificate")
	}
	if st := m.Status(); !st.NotAfter.Equal(before) {
		log.Info().Strs("names", st.SANs).Time("certificateExpires", st.NotAfter).Msg("TLS certificate re-issued")
	}
}
