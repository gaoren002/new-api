package service

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/setting"
)

var promptAuditHTTPClients sync.Map

func promptAuditHTTPClient(endpoint setting.PromptAuditEndpoint) (*http.Client, error) {
	if err := setting.ValidatePromptAuditBaseURL(endpoint.BaseURL); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%s\x00%s\x00%d", endpoint.ID, endpoint.BaseURL, endpoint.TimeoutMS)
	if cached, ok := promptAuditHTTPClients.Load(key); ok {
		return cached.(*http.Client), nil
	}
	timeout := time.Duration(endpoint.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = time.Duration(setting.DefaultPromptAuditTimeoutMS) * time.Millisecond
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	actual, _ := promptAuditHTTPClients.LoadOrStore(key, client)
	return actual.(*http.Client), nil
}
