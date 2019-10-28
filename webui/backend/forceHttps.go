package backend

import (
	"net/http"
	"net/url"
	"net"

	"github.com/Pegasus8/piworker/processment/configs"
)

type redirectHTTPSHandlerStruct struct {
	handler http.Handler
}

func (r *redirectHTTPSHandlerStruct) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	redirectToHTTPS(r.handler.ServeHTTP)
}

func redirectToHTTPS(endpoint func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		if !tlsSupport { return }

		if req.TLS == nil {
			host, _, err := net.SplitHostPort(req.Host)
			if err != nil {
				// no port in host
				host = req.Host
			}
			newURL := url.URL{
				Scheme:   "https",
				Host:     net.JoinHostPort(host, configs.CurrentConfigs.WebUI.ListeningPort),
				Path:     req.URL.Path,
				RawQuery: req.URL.RawQuery,
			}
			http.Redirect(w, req, newURL.String(), http.StatusTemporaryRedirect)
			return
		}

		
	}
}

func redirectToHTTPSHandler(handler http.Handler) http.Handler {
	return &redirectHTTPSHandlerStruct{handler}
}