package client

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"DeepSeek_Web_To_API/internal/account"
	"DeepSeek_Web_To_API/internal/auth"
	"DeepSeek_Web_To_API/internal/config"
	dsprotocol "DeepSeek_Web_To_API/internal/deepseek/protocol"
	powpkg "DeepSeek_Web_To_API/pow"
)

type completionSwitchDoer struct {
	seenTokens   []string
	seenSessions []string
}

func (d *completionSwitchDoer) Do(req *http.Request) (*http.Response, error) {
	d.seenTokens = append(d.seenTokens, strings.TrimSpace(req.Header.Get("authorization")))
	d.seenSessions = append(d.seenSessions, requestBodyString(req))
	if strings.Contains(req.Header.Get("authorization"), "token-1") {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":"busy"}`)),
			Request:    req,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("data: {\"v\":\"ok\"}\n" + "data: [DONE]\n")),
		Request:    req,
	}, nil
}

type completionSwitchRegularDoer struct{}

func (completionSwitchRegularDoer) Do(req *http.Request) (*http.Response, error) {
	switch req.URL.String() {
	case dsprotocol.DeepSeekCreateSessionURL:
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"ok","data":{"biz_code":0,"biz_data":{"id":"session-2"}}}`)),
			Request:    req,
		}, nil
	case dsprotocol.DeepSeekCreatePowURL:
		challengeHash := powpkg.DeepSeekHashV1([]byte(powpkg.BuildPrefix("salt", 1712345678) + "0"))
		body := `{"code":0,"msg":"ok","data":{"biz_code":0,"biz_data":{"challenge":{"algorithm":"DeepSeekHashV1","challenge":"` +
			hex.EncodeToString(challengeHash[:]) +
			`","salt":"salt","expire_at":1712345678,"difficulty":1,"signature":"sig","target_path":"` +
			dsprotocol.DeepSeekCompletionTargetPath + `"}}}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	default:
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":"unexpected url"}`)),
			Request:    req,
		}, nil
	}
}

func TestCallCompletionSwitchesAccountAfterFailedAttempt(t *testing.T) {
	t.Setenv("DEEPSEEK_WEB_TO_API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[
			{"email":"acc1@example.com","password":"pwd","token":"token-1"},
			{"email":"acc2@example.com","password":"pwd","token":"token-2"}
		],
		"runtime":{"account_max_inflight":1}
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	resolver := auth.NewResolver(store, pool, func(_ context.Context, acc config.Account) (string, error) {
		if acc.Email == "acc2@example.com" {
			return "token-2", nil
		}
		return "token-1", nil
	})
	doer := &completionSwitchDoer{}
	client := &Client{
		Auth:       resolver,
		Store:      store,
		regular:    completionSwitchRegularDoer{},
		stream:     doer,
		fallbackS:  &http.Client{},
		fallback:   &http.Client{},
		maxRetries: 3,
	}
	first, ok := store.FindAccount("acc1@example.com")
	if !ok {
		t.Fatal("missing first account")
	}
	a := &auth.RequestAuth{
		UseConfigToken: true,
		DeepSeekToken:  "token-1",
		AccountID:      "acc1@example.com",
		Account:        first,
		TriedAccounts:  map[string]bool{},
	}
	resp, err := client.CallCompletion(context.Background(), a, map[string]any{"chat_session_id": "s"}, "pow", 2)
	if err != nil {
		t.Fatalf("CallCompletion returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if a.AccountID != "acc2@example.com" {
		t.Fatalf("expected switched account, got %q", a.AccountID)
	}
	if len(doer.seenTokens) != 2 || !strings.Contains(doer.seenTokens[1], "token-2") {
		t.Fatalf("expected retry with second token, saw %#v", doer.seenTokens)
	}
	if len(doer.seenSessions) != 2 || !strings.Contains(doer.seenSessions[1], "session-2") {
		t.Fatalf("expected retry payload to use new session, saw %#v", doer.seenSessions)
	}
}

func TestCallCompletionReturnsUpstreamStatusFailure(t *testing.T) {
	t.Parallel()

	client := &Client{
		stream: encodedBodyDoerFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"busy"}`)),
				Request:    req,
			}, nil
		}),
		fallbackS:  &http.Client{},
		maxRetries: 1,
	}

	resp, err := client.CallCompletion(context.Background(), &auth.RequestAuth{DeepSeekToken: "x"}, map[string]any{"chat_session_id": "s"}, "pow", 1)
	if resp != nil {
		t.Fatalf("expected nil response on exhausted upstream status, got %#v", resp)
	}
	var failure *RequestFailure
	if !errors.As(err, &failure) {
		t.Fatalf("expected RequestFailure, got %T %v", err, err)
	}
	if failure.Kind != FailureUpstreamStatus || failure.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unexpected failure: %#v", failure)
	}
	if !strings.Contains(failure.Message, "busy") {
		t.Fatalf("expected upstream body in failure, got %q", failure.Message)
	}
}

func TestStreamPostDoesNotFallbackAfterContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &Client{}

	resp, err := client.streamPost(ctx, encodedBodyDoerFunc(func(req *http.Request) (*http.Response, error) {
		return nil, context.Canceled
	}), "https://example.test/sse", nil, map[string]any{"prompt": "hi"})

	if resp != nil {
		t.Fatalf("expected nil response, got %#v", resp)
	}
	if !IsClientCancelledError(err) {
		t.Fatalf("expected client-cancelled error, got %T %v", err, err)
	}
}

func requestBodyString(req *http.Request) string {
	if req == nil || req.Body == nil {
		return ""
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return ""
	}
	req.Body = io.NopCloser(strings.NewReader(string(body)))
	return string(body)
}
