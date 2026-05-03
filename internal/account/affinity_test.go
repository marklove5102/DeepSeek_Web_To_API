package account

import (
	"testing"
	"time"
)

func TestAffinityLockSerializesSameSessionKey(t *testing.T) {
	aff := NewAffinity()
	unlock := aff.Lock("session-a")

	entered := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		unlockSecond := aff.Lock("session-a")
		defer unlockSecond()
		close(entered)
	}()

	select {
	case <-entered:
		t.Fatal("second lock for same key entered before first unlock")
	case <-time.After(50 * time.Millisecond):
	}

	unlock()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("second lock did not enter after first unlock")
	}
	<-done
}

func TestAffinityLockAllowsDifferentSessionKeys(t *testing.T) {
	aff := NewAffinity()
	unlock := aff.Lock("session-a")
	defer unlock()

	entered := make(chan struct{})
	go func() {
		unlockSecond := aff.Lock("session-b")
		defer unlockSecond()
		close(entered)
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("different session keys should not block each other")
	}
}

func TestScopedSessionKeyIncludesCallerHash(t *testing.T) {
	keyA := ScopedSessionKey(CallerHash("caller-a"), "claude:caller")
	keyB := ScopedSessionKey(CallerHash("caller-b"), "claude:caller")
	if keyA == "" || keyB == "" {
		t.Fatal("expected scoped keys to be non-empty")
	}
	if keyA == keyB {
		t.Fatal("expected scoped keys to differ for different callers")
	}
}

func TestSessionKeySupportsResponsesInput(t *testing.T) {
	bodyA := []byte(`{"instructions":"system A","input":[{"role":"user","content":[{"type":"input_text","text":"same first user"}]}]}`)
	bodyB := []byte(`{"instructions":"system A","input":[{"role":"user","content":[{"type":"input_text","text":"same first user"}]},{"role":"assistant","content":"old answer"},{"role":"user","content":"new turn"}]}`)
	keyA := SessionKey(CallerHash("caller"), bodyA)
	keyB := SessionKey(CallerHash("caller"), bodyB)
	if keyA == "" || keyB == "" {
		t.Fatalf("expected responses input session keys, got %q %q", keyA, keyB)
	}
	if keyA != keyB {
		t.Fatalf("expected same first-user responses input to share key, got %q %q", keyA, keyB)
	}
}

func TestSessionKeyResponsesInstructionsAffectScope(t *testing.T) {
	bodyA := []byte(`{"instructions":"system A","input":"same first user"}`)
	bodyB := []byte(`{"instructions":"system B","input":"same first user"}`)
	keyA := SessionKey(CallerHash("caller"), bodyA)
	keyB := SessionKey(CallerHash("caller"), bodyB)
	if keyA == "" || keyB == "" {
		t.Fatalf("expected responses input session keys, got %q %q", keyA, keyB)
	}
	if keyA == keyB {
		t.Fatalf("expected different instructions to produce different keys, both got %q", keyA)
	}
}

func TestSessionKeyUserIdentityAffectsScope(t *testing.T) {
	bodyA := []byte(`{"user":"user-a","messages":[{"role":"user","content":"你好，请用一句话介绍你自己。"}]}`)
	bodyB := []byte(`{"user":"user-b","messages":[{"role":"user","content":"你好，请用一句话介绍你自己。"}]}`)
	keyA := SessionKey(CallerHash("caller"), bodyA)
	keyB := SessionKey(CallerHash("caller"), bodyB)
	if keyA == "" || keyB == "" {
		t.Fatalf("expected user-scoped session keys, got %q %q", keyA, keyB)
	}
	if keyA == keyB {
		t.Fatalf("expected different users to produce different keys, both got %q", keyA)
	}
}

func TestSessionKeyMetadataConversationAffectsScope(t *testing.T) {
	bodyA := []byte(`{"metadata":{"conversation_id":"conv-a"},"messages":[{"role":"user","content":"same first user"}]}`)
	bodyB := []byte(`{"metadata":{"conversation_id":"conv-b"},"messages":[{"role":"user","content":"same first user"}]}`)
	keyA := SessionKey(CallerHash("caller"), bodyA)
	keyB := SessionKey(CallerHash("caller"), bodyB)
	if keyA == "" || keyB == "" {
		t.Fatalf("expected metadata-scoped session keys, got %q %q", keyA, keyB)
	}
	if keyA == keyB {
		t.Fatalf("expected different metadata conversations to produce different keys, both got %q", keyA)
	}
}
