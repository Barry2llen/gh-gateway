package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type captureTransport struct {
	request *http.Request
	body    string
}

func (c *captureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	c.request = request.Clone(request.Context())
	data, _ := io.ReadAll(request.Body)
	c.body = string(data)
	return &http.Response{StatusCode: 201, Header: http.Header{"X-Upstream": []string{"yes"}}, Body: io.NopCloser(strings.NewReader("proxied")), Request: request}, nil
}

func TestProxyPreservesRequestAndPinsHost(t *testing.T) {
	transport := &captureTransport{}
	handler := New("git.example.com", transport)
	request := httptest.NewRequest(http.MethodPost, "https://git.example.com/api/v1/repos?q=1", strings.NewReader("body"))
	request.Header.Set("Authorization", "token incoming")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if transport.request.Host != "git.example.com" || transport.request.URL.Host != "git.example.com" {
		t.Fatalf("host = %q URL host = %q", transport.request.Host, transport.request.URL.Host)
	}
	if transport.request.URL.Path != "/api/v1/repos" || transport.request.URL.RawQuery != "q=1" || transport.body != "body" {
		t.Fatalf("request = %s?%s body=%q", transport.request.URL.Path, transport.request.URL.RawQuery, transport.body)
	}
	if transport.request.Header.Get("Authorization") != "token incoming" {
		t.Fatal("authorization was not preserved")
	}
	if response.Code != 201 || response.Body.String() != "proxied" || response.Header().Get("X-Upstream") != "yes" {
		t.Fatalf("response = %+v body=%q", response.Result(), response.Body.String())
	}
}
