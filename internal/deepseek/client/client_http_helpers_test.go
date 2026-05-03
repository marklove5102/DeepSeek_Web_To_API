package client

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

type encodedBodyDoerFunc func(*http.Request) (*http.Response, error)

func (f encodedBodyDoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestReadResponseBodyDecodesGzip(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Encoding": []string{"gzip"}},
		Body:       io.NopCloser(bytes.NewReader(gzipBytes("你好，gzip"))),
	}

	body, err := readResponseBody(resp)
	if err != nil {
		t.Fatalf("readResponseBody returned error: %v", err)
	}
	if string(body) != "你好，gzip" {
		t.Fatalf("unexpected decoded body: %q", string(body))
	}
	if got := resp.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("expected content encoding header cleared, got %q", got)
	}
}

func TestReadResponseBodyRejectsOversizedDecodedBody(t *testing.T) {
	plain := strings.Repeat("x", decodedResponseBodyMaxBytes+1)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Encoding": []string{"gzip"}},
		Body:       io.NopCloser(bytes.NewReader(gzipBytes(plain))),
	}

	_, err := readResponseBody(resp)
	if err == nil {
		t.Fatal("expected oversized decoded body to be rejected")
	}
	if !strings.Contains(err.Error(), "decoded response body exceeds") {
		t.Fatalf("unexpected oversized body error: %v", err)
	}
}

func TestStreamPostDecodesBrotliSSEBody(t *testing.T) {
	t.Parallel()

	plain := "data: {\"p\":\"response/content\",\"v\":\"你好，brotli\"}\n" +
		"data: [DONE]\n"
	c := &Client{}
	resp, err := c.streamPost(context.Background(), encodedBodyDoerFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Encoding": []string{"br"}},
			Body:       io.NopCloser(bytes.NewReader(brotliBytes(plain))),
			Request:    req,
		}, nil
	}), "https://example.test/sse", nil, map[string]any{"prompt": "hi"})
	if err != nil {
		t.Fatalf("streamPost returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read stream body failed: %v", err)
	}
	if string(body) != plain {
		t.Fatalf("unexpected decoded stream body: %q", string(body))
	}
	if strings.Contains(string(body), "\ufffd") {
		t.Fatalf("decoded stream contains replacement characters: %q", string(body))
	}
}

func gzipBytes(s string) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(s))
	_ = zw.Close()
	return buf.Bytes()
}

func brotliBytes(s string) []byte {
	var buf bytes.Buffer
	zw := brotli.NewWriter(&buf)
	_, _ = zw.Write([]byte(s))
	_ = zw.Close()
	return buf.Bytes()
}
