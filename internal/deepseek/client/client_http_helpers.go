package client

import (
	"bufio"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
)

const decodedResponseBodyMaxBytes = 16 << 20

func readResponseBody(resp *http.Response) ([]byte, error) {
	if err := decodeResponseBody(resp); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, decodedResponseBodyMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > decodedResponseBodyMaxBytes {
		return nil, fmt.Errorf("decoded response body exceeds %d bytes", decodedResponseBodyMaxBytes)
	}
	return raw, nil
}

func decodeResponseBody(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	encodings := responseContentEncodings(resp)
	if len(encodings) == 0 || resp.Uncompressed {
		clearContentEncodingHeaders(resp)
		return nil
	}

	original := resp.Body
	reader := io.Reader(original)
	closers := make([]io.Closer, 0, len(encodings)+1)
	for i := len(encodings) - 1; i >= 0; i-- {
		encoding := encodings[i]
		switch encoding {
		case "identity":
			continue
		case "gzip":
			gz, err := gzip.NewReader(reader)
			if err != nil {
				return err
			}
			reader = gz
			closers = append(closers, gz)
		case "br":
			reader = brotli.NewReader(reader)
		case "deflate":
			br := bufio.NewReader(reader)
			var rc io.ReadCloser
			var err error
			if looksLikeZlibStream(br) {
				rc, err = zlib.NewReader(br)
				if err != nil {
					return err
				}
			} else {
				rc = flate.NewReader(br)
			}
			reader = rc
			closers = append(closers, rc)
		default:
			return fmt.Errorf("unsupported content encoding %q", encoding)
		}
	}
	closers = append(closers, original)
	resp.Body = decodingReadCloser{Reader: reader, closers: closers}
	resp.Uncompressed = true
	clearContentEncodingHeaders(resp)
	return nil
}

func responseContentEncodings(resp *http.Response) []string {
	raw := strings.TrimSpace(resp.Header.Get("Content-Encoding"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		encoding := strings.ToLower(strings.TrimSpace(part))
		if encoding != "" {
			out = append(out, encoding)
		}
	}
	return out
}

func clearContentEncodingHeaders(resp *http.Response) {
	if resp == nil {
		return
	}
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
}

func looksLikeZlibStream(r *bufio.Reader) bool {
	header, err := r.Peek(2)
	if err != nil {
		return true
	}
	cmf := int(header[0])
	flg := int(header[1])
	return cmf&0x0f == 8 && (cmf*256+flg)%31 == 0
}

type decodingReadCloser struct {
	io.Reader
	closers []io.Closer
}

func (r decodingReadCloser) Close() error {
	var firstErr error
	for _, closer := range r.closers {
		if closer == nil {
			continue
		}
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func preview(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 160 {
		return s[:160]
	}
	return s
}

func (c *Client) jsonHeaders(headers map[string]string) map[string]string {
	out := cloneStringMap(headers)
	out["Content-Type"] = "application/json"
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
