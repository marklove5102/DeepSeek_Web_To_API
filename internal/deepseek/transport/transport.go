package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"DeepSeek_Web_To_API/internal/deepseek/protocol"

	utls "github.com/refraction-networking/utls"
)

type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

type DialContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)

type Client struct {
	http *http.Client
}

func New(timeout time.Duration) *Client {
	return NewWithDialContext(timeout, nil)
}

func NewWithDialContext(timeout time.Duration, dialContext DialContextFunc) *Client {
	useEnvProxy := dialContext == nil
	if dialContext == nil {
		dialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	base := &http.Transport{
		ForceAttemptHTTP2:   false,
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
		DialContext:         dialContext,
		DialTLSContext:      safariTLSDialer(dialContext),
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
	}
	if useEnvProxy {
		base.Proxy = http.ProxyFromEnvironment
	}
	return &Client{http: &http.Client{Timeout: timeout, Transport: base}}
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if err := validateDeepSeekRequest(req); err != nil {
		return nil, err
	}
	// #nosec G704 -- requests are constrained to the fixed DeepSeek HTTPS host above.
	return c.http.Do(req)
}

func validateDeepSeekRequest(req *http.Request) error {
	if req == nil || req.URL == nil {
		return fmt.Errorf("deepseek request URL is required")
	}
	if !strings.EqualFold(req.URL.Scheme, "https") {
		return fmt.Errorf("deepseek request scheme must be https")
	}
	if !strings.EqualFold(req.URL.Hostname(), protocol.DeepSeekHost) {
		return fmt.Errorf("deepseek request host is not allowed: %s", req.URL.Hostname())
	}
	return nil
}

func NewFallbackClient(timeout time.Duration, dialContext DialContextFunc) *http.Client {
	useEnvProxy := dialContext == nil
	if dialContext == nil {
		dialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	base := &http.Transport{
		ForceAttemptHTTP2:   false,
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
		DialContext:         dialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
	}
	if useEnvProxy {
		base.Proxy = http.ProxyFromEnvironment
	}
	return &http.Client{Timeout: timeout, Transport: base}
}

func safariTLSDialer(dialContext DialContextFunc) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if dialContext == nil {
		dialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		plainConn, err := dialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		host, _, _ := net.SplitHostPort(addr)
		uCfg := &utls.Config{ServerName: host}
		uConn := utls.UClient(plainConn, uCfg, utls.HelloSafari_Auto)
		if err := forceHTTP11ALPN(uConn); err != nil {
			_ = plainConn.Close()
			return nil, err
		}
		err = uConn.HandshakeContext(ctx)
		if err != nil {
			_ = plainConn.Close()
			return nil, err
		}
		if negotiated := uConn.ConnectionState().NegotiatedProtocol; negotiated != "" && negotiated != "http/1.1" {
			_ = uConn.Close()
			return nil, fmt.Errorf("unexpected ALPN protocol negotiated: %s", negotiated)
		}
		return uConn, nil
	}
}

func forceHTTP11ALPN(uConn *utls.UConn) error {
	if err := uConn.BuildHandshakeState(); err != nil {
		return err
	}
	for _, ext := range uConn.Extensions {
		alpnExt, ok := ext.(*utls.ALPNExtension)
		if !ok {
			continue
		}
		alpnExt.AlpnProtocols = []string{"http/1.1"}
		return nil
	}
	return nil
}
