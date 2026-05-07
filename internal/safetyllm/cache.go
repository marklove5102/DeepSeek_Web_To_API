package safetyllm

import (
	"container/list"
	"sync"
	"time"
)

// lruCache is a small thread-safe LRU with absolute-time expiration.
// Bounded by entry count + per-entry TTL.
type lruCache struct {
	mu    sync.Mutex
	max   int
	items map[string]*list.Element
	order *list.List
}

type cacheEntry struct {
	key       string
	verdict   Verdict
	expiresAt time.Time
}

func newLRUCache(max int) *lruCache {
	if max <= 0 {
		max = 1
	}
	return &lruCache{
		max:   max,
		items: make(map[string]*list.Element, max),
		order: list.New(),
	}
}

func (c *lruCache) get(key string, now time.Time) (Verdict, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return Verdict{}, false
	}
	entry := el.Value.(*cacheEntry)
	if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
		c.order.Remove(el)
		delete(c.items, key)
		return Verdict{}, false
	}
	c.order.MoveToFront(el)
	return entry.verdict, true
}

func (c *lruCache) set(key string, v Verdict, expiresAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		entry := el.Value.(*cacheEntry)
		entry.verdict = v
		entry.expiresAt = expiresAt
		c.order.MoveToFront(el)
		return
	}
	entry := &cacheEntry{key: key, verdict: v, expiresAt: expiresAt}
	el := c.order.PushFront(entry)
	c.items[key] = el
	for c.order.Len() > c.max {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		oldEntry := oldest.Value.(*cacheEntry)
		c.order.Remove(oldest)
		delete(c.items, oldEntry.key)
	}
}

func (c *lruCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// purge drops every entry. Used by LLMChecker when audit semantics
// (model, fail_open, enabled) change — stale verdicts from a previous
// model would otherwise be replayed to the new model's callers.
func (c *lruCache) purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*list.Element, c.max)
	c.order.Init()
}
