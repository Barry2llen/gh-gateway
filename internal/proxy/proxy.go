package proxy

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

func New(host string, transport http.RoundTripper) http.Handler {
	target := &url.URL{Scheme: "https", Host: host}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		originalDirector(request)
		request.Host = host
	}
	proxy.Transport = transport
	proxy.FlushInterval = -1
	return proxy
}
