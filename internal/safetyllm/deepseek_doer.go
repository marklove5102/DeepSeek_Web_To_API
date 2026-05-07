package safetyllm

import (
	"context"
	"errors"
	"strings"

	"DeepSeek_Web_To_API/internal/auth"
	dsclient "DeepSeek_Web_To_API/internal/deepseek/client"
	"DeepSeek_Web_To_API/internal/promptcompat"
	"DeepSeek_Web_To_API/internal/sse"
)

// DeepSeekDoer adapts dsclient.Client + sse collection into the
// minimal CompletionDoer surface the safetyllm.LLMChecker needs.
//
// One safety check = CreateSession + GetPow + CallCompletion(thinking=false,
// search=false, model=cfg.Model) + SSE collect. The session is one-shot;
// the caller's own auto-delete behavior cleans it up after the request
// completes (no extra session-delete here — keeps this doer side-effect
// free relative to the request lifecycle).
type DeepSeekDoer struct {
	Client *dsclient.Client
}

func NewDeepSeekDoer(client *dsclient.Client) *DeepSeekDoer {
	return &DeepSeekDoer{Client: client}
}

func (d *DeepSeekDoer) RunSafetyCheck(ctx context.Context, a *auth.RequestAuth, model, prompt string) (string, error) {
	if d == nil || d.Client == nil {
		return "", errors.New("safetyllm deepseek doer not initialised")
	}
	if a == nil {
		return "", errors.New("safetyllm requires authenticated RequestAuth")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "deepseek-v4-flash-nothinking"
	}
	sessionID, err := d.Client.CreateSession(ctx, a, 1)
	if err != nil {
		return "", err
	}
	powResp, err := d.Client.GetPow(ctx, a, 1)
	if err != nil {
		return "", err
	}
	stdReq := promptcompat.StandardRequest{
		ResolvedModel: model,
		FinalPrompt:   prompt,
		Thinking:      false,
		Search:        false,
	}
	payload := stdReq.CompletionPayload(sessionID)
	resp, err := d.Client.CallCompletion(ctx, a, payload, powResp, 1)
	if err != nil {
		return "", err
	}
	collected := sse.CollectStream(resp, false, true)
	text := strings.TrimSpace(collected.Text)
	if text == "" {
		// Some upstream paths put the binary verdict in thinking when the
		// nothinking suffix is honored loosely. Fall back so we don't
		// fail-open on a successful upstream call.
		text = strings.TrimSpace(collected.Thinking)
	}
	if text == "" {
		return "", errors.New("safetyllm upstream returned empty completion")
	}
	return text, nil
}
