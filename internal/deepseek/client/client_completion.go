package client

import (
	dsprotocol "DeepSeek_Web_To_API/internal/deepseek/protocol"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"DeepSeek_Web_To_API/internal/auth"
	"DeepSeek_Web_To_API/internal/config"
	trans "DeepSeek_Web_To_API/internal/deepseek/transport"
)

const completionFailureBodyLimit = 4096

var errNoCompletionSwitchCandidate = errors.New("no completion switch candidate")

func (c *Client) CallCompletion(ctx context.Context, a *auth.RequestAuth, payload map[string]any, powResp string, maxAttempts int) (*http.Response, error) {
	if maxAttempts <= 0 {
		maxAttempts = c.maxRetries
	}
	if failure := requestContextFailure("completion", ctx, nil); failure != nil {
		return nil, failure
	}
	clients := c.requestClientsForAuth(ctx, a)
	headers := c.authHeaders(a.DeepSeekToken)
	headers["x-ds-pow-response"] = powResp
	captureSession := c.capture.Start("deepseek_completion", dsprotocol.DeepSeekCompletionURL, a.AccountID, payload)
	attempts := 0
	var lastErr error
	for attempts < maxAttempts {
		resp, err := c.streamPost(ctx, clients.stream, dsprotocol.DeepSeekCompletionURL, headers, payload)
		if err != nil {
			lastErr = transportFailure("completion", ctx, err)
			config.Logger.Warn("[completion] request failed", "account", accountIDForLog(a), "failure_kind", requestFailureKind(lastErr), "error", err)
			if !completionFailureRetryable(lastErr) {
				return nil, lastErr
			}
			attempts++
			if attempts >= maxAttempts {
				break
			}
			if switchErr := c.switchCompletionAccount(ctx, a, &clients, &headers, payload); switchErr == nil {
				continue
			} else if !errors.Is(switchErr, errNoCompletionSwitchCandidate) {
				config.Logger.Warn("[completion] switch account failed", "account", accountIDForLog(a), "error", switchErr)
				return nil, firstError(switchErr, lastErr)
			}
			if err := sleepCompletionRetry(ctx, time.Second); err != nil {
				return nil, err
			}
			continue
		}
		if resp.StatusCode == http.StatusOK {
			if captureSession != nil {
				resp.Body = captureSession.WrapBody(resp.Body, resp.StatusCode)
			}
			resp = c.wrapCompletionWithAutoContinue(ctx, a, payload, powResp, resp)
			return resp, nil
		}
		if captureSession != nil {
			resp.Body = captureSession.WrapBody(resp.Body, resp.StatusCode)
		}
		lastErr = completionStatusFailure(resp)
		config.Logger.Warn("[completion] upstream returned non-OK status", "account", accountIDForLog(a), "status", resp.StatusCode, "failure_kind", requestFailureKind(lastErr), "error", lastErr)
		if resp.Body != nil {
			if err := resp.Body.Close(); err != nil {
				config.Logger.Warn("[completion] close upstream response body failed", "account", accountIDForLog(a), "status", resp.StatusCode, "error", err)
			}
		}
		attempts++
		if attempts >= maxAttempts {
			break
		}
		if switchErr := c.switchCompletionAccount(ctx, a, &clients, &headers, payload); switchErr == nil {
			continue
		} else if !errors.Is(switchErr, errNoCompletionSwitchCandidate) {
			config.Logger.Warn("[completion] switch account failed", "account", accountIDForLog(a), "error", switchErr)
			return nil, firstError(switchErr, lastErr)
		}
		if err := sleepCompletionRetry(ctx, time.Second); err != nil {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, &RequestFailure{Op: "completion", Kind: FailureUnknown, Message: "completion failed"}
}

func (c *Client) switchCompletionAccount(ctx context.Context, a *auth.RequestAuth, clients *requestClients, headers *map[string]string, payload map[string]any) error {
	if c == nil || c.Auth == nil || a == nil || !a.UseConfigToken {
		return errNoCompletionSwitchCandidate
	}
	if !c.hasCompletionSwitchCandidate(a) {
		return errNoCompletionSwitchCandidate
	}
	if !c.Auth.SwitchAccount(ctx, a) {
		if failure := requestContextFailure("completion", ctx, nil); failure != nil {
			return failure
		}
		return errNoCompletionSwitchCandidate
	}
	*clients = c.requestClientsForAuth(ctx, a)
	sessionID, err := c.createCompletionRetrySession(ctx, a, *clients)
	if err != nil {
		return err
	}
	powResp, err := c.getCompletionRetryPow(ctx, a, *clients)
	if err != nil {
		return err
	}
	payload["chat_session_id"] = sessionID
	nextHeaders := c.authHeaders(a.DeepSeekToken)
	nextHeaders["x-ds-pow-response"] = powResp
	*headers = nextHeaders
	return nil
}

func (c *Client) hasCompletionSwitchCandidate(a *auth.RequestAuth) bool {
	if c == nil || c.Store == nil || a == nil {
		return true
	}
	accounts := c.Store.Accounts()
	if len(accounts) <= 1 {
		return false
	}
	tried := len(a.TriedAccounts)
	if current := strings.TrimSpace(a.AccountID); current != "" {
		if a.TriedAccounts == nil || !a.TriedAccounts[current] {
			tried++
		}
	}
	return tried < len(accounts)
}

func (c *Client) createCompletionRetrySession(ctx context.Context, a *auth.RequestAuth, clients requestClients) (string, error) {
	headers := c.authHeaders(a.DeepSeekToken)
	resp, status, err := c.postJSONWithStatus(ctx, clients.regular, clients.fallback, dsprotocol.DeepSeekCreateSessionURL, headers, map[string]any{"agent": "chat"})
	if err != nil {
		return "", transportFailure("create session", ctx, err)
	}
	code, bizCode, msg, bizMsg := extractResponseStatus(resp)
	if status == http.StatusOK && code == 0 && bizCode == 0 {
		if sessionID := extractCreateSessionID(resp); sessionID != "" {
			return sessionID, nil
		}
	}
	message := failureMessage(msg, bizMsg, "create session failed")
	kind := FailureUpstreamStatus
	if isTokenInvalid(status, code, bizCode, msg, bizMsg) || isAuthIndicativeBizFailure(msg, bizMsg) {
		kind = authFailureKind(a.UseConfigToken)
	}
	return "", &RequestFailure{Op: "create session", Kind: kind, StatusCode: status, Message: message}
}

func (c *Client) getCompletionRetryPow(ctx context.Context, a *auth.RequestAuth, clients requestClients) (string, error) {
	headers := c.authHeaders(a.DeepSeekToken)
	resp, status, err := c.postJSONWithStatus(ctx, clients.regular, clients.fallback, dsprotocol.DeepSeekCreatePowURL, headers, map[string]any{"target_path": dsprotocol.DeepSeekCompletionTargetPath})
	if err != nil {
		return "", transportFailure("get pow", ctx, err)
	}
	code, bizCode, msg, bizMsg := extractResponseStatus(resp)
	if status == http.StatusOK && code == 0 && bizCode == 0 {
		data, _ := resp["data"].(map[string]any)
		bizData, _ := data["biz_data"].(map[string]any)
		challenge, _ := bizData["challenge"].(map[string]any)
		answer, err := ComputePow(ctx, challenge)
		if err != nil {
			return "", transportFailure("get pow", ctx, err)
		}
		return BuildPowHeader(challenge, answer)
	}
	message := failureMessage(msg, bizMsg, "get pow failed")
	kind := FailureUpstreamStatus
	if isTokenInvalid(status, code, bizCode, msg, bizMsg) || isAuthIndicativeBizFailure(msg, bizMsg) {
		kind = authFailureKind(a.UseConfigToken)
	}
	return "", &RequestFailure{Op: "get pow", Kind: kind, StatusCode: status, Message: message}
}

func (c *Client) streamPost(ctx context.Context, doer trans.Doer, url string, headers map[string]string, payload any) (*http.Response, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	headers = c.jsonHeaders(headers)
	clients := c.requestClientsFromContext(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := doer.Do(req)
	if err != nil {
		if failure := requestContextFailure("completion", ctx, err); failure != nil {
			return nil, failure
		}
		config.Logger.Warn("[deepseek] fingerprint stream request failed, fallback to std transport", "url", url, "error", err)
		req2, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
		if reqErr != nil {
			return nil, reqErr
		}
		for k, v := range headers {
			req2.Header.Set(k, v)
		}
		resp, err = clients.fallbackS.Do(req2)
		if err != nil {
			return nil, err
		}
		if err := decodeResponseBody(resp); err != nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				config.Logger.Warn("[deepseek] close fallback stream body failed", "url", url, "error", closeErr)
			}
			return nil, err
		}
		return resp, nil
	}
	if err := decodeResponseBody(resp); err != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			config.Logger.Warn("[deepseek] close stream body failed", "url", url, "error", closeErr)
		}
		return nil, err
	}
	return resp, nil
}

func completionStatusFailure(resp *http.Response) error {
	if resp == nil {
		return &RequestFailure{Op: "completion", Kind: FailureUpstreamStatus, Message: "missing upstream response"}
	}
	message := http.StatusText(resp.StatusCode)
	if resp.Body != nil {
		body, err := io.ReadAll(io.LimitReader(resp.Body, completionFailureBodyLimit+1))
		if err != nil {
			return &RequestFailure{Op: "completion", Kind: FailureUpstreamNetwork, StatusCode: resp.StatusCode, Message: "read upstream error body: " + err.Error(), Cause: err}
		}
		if len(body) > completionFailureBodyLimit {
			body = body[:completionFailureBodyLimit]
		}
		if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
			message = trimmed
		}
	}
	return &RequestFailure{Op: "completion", Kind: FailureUpstreamStatus, StatusCode: resp.StatusCode, Message: message}
}

func completionFailureRetryable(err error) bool {
	return !IsClientCancelledError(err) && !IsUpstreamTimeoutError(err)
}

func sleepCompletionRetry(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return requestContextFailure("completion", ctx, nil)
	case <-timer.C:
		return nil
	}
}

func accountIDForLog(a *auth.RequestAuth) string {
	if a == nil {
		return ""
	}
	return a.AccountID
}
