package daemon

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultDeliveryCacheSize bounds how many recent delivery IDs one server
	// remembers (#747). Each entry is a 32-byte digest plus list overhead.
	DefaultDeliveryCacheSize = 1024

	// DefaultDeliveryCacheTTL is how long a delivery ID is remembered. Provider
	// retries arrive within minutes; a replay after this window is processed.
	DefaultDeliveryCacheTTL = time.Hour
)

// webhookBodyKeyPrefix namespaces body digests apart from delivery IDs.
const webhookBodyKeyPrefix = "body-sha256:"

// deliveryIDHeaders lists the headers that carry a per-delivery ID, in lookup
// order. Gitea sends X-GitHub-Delivery and X-Gitea-Delivery with one value.
// GitLab sends webhook-id and Idempotency-Key (equal, constant across its
// retries) and X-Gitlab-Event-UUID. Bitbucket sends X-Request-UUID.
// X-Gitlab-Webhook-UUID and Bitbucket's X-Hook-UUID are deliberately absent:
// they name the webhook configuration, so every push from one hook shares them.
var deliveryIDHeaders = []string{
	"X-GitHub-Delivery",
	"X-Gitea-Delivery",
	"Webhook-Id",
	"Idempotency-Key",
	"X-Gitlab-Event-UUID",
	"X-Request-UUID",
}

// WebhookDeliveryID returns the request's delivery ID for replay detection,
// prefixed with the header it came from, or "" when no delivery header is set.
// The value is attacker-controlled; callers must sanitize it before logging.
func WebhookDeliveryID(h http.Header) string {
	for _, name := range deliveryIDHeaders {
		if v := strings.TrimSpace(h.Get(name)); v != "" {
			return name + ":" + v
		}
	}
	return ""
}

// WebhookBodyKey returns the replay key for an authorized request body: its
// SHA-256 in a namespace separate from delivery IDs. The delivery headers are
// not signed, so a captured body re-sent with a fresh delivery ID still
// matches this key. An empty body returns "": it carries no event, so the
// plain signed trigger (an empty body sent repeatedly) keeps working.
func WebhookBodyKey(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	sum := sha256.Sum256(body)
	return webhookBodyKeyPrefix + hex.EncodeToString(sum[:])
}

// IsWebhookBodyKey reports whether a key returned by WebhookReplayKeys is the
// body digest rather than the delivery ID.
func IsWebhookBodyKey(key string) bool {
	return strings.HasPrefix(key, webhookBodyKeyPrefix)
}

// WebhookReplayKeys returns the request's replay keys: its delivery ID and
// its body digest. Either may be "".
func WebhookReplayKeys(h http.Header, body []byte) []string {
	return []string{WebhookDeliveryID(h), WebhookBodyKey(body)}
}

// PushDeletesBranch reports whether a push payload deletes its ref (#747):
// GitHub sets "deleted": true, and GitHub, GitLab, and Gitea send an "after"
// made only of zeros. A body that is not a JSON object, or whose fields have
// other types, is not a deletion.
func PushDeletesBranch(body []byte) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return false
	}
	var deleted bool
	if raw, ok := fields["deleted"]; ok && json.Unmarshal(raw, &deleted) == nil && deleted {
		return true
	}
	var after string
	if raw, ok := fields["after"]; ok && json.Unmarshal(raw, &after) == nil {
		return isZeroCommitID(after)
	}
	return false
}

// isZeroCommitID reports whether s is a non-empty run of '0' characters, the
// null object ID providers send as "after" for a deleted ref.
func isZeroCommitID(s string) bool {
	return s != "" && strings.Trim(s, "0") == ""
}

// DeliveryCache remembers recent webhook replay keys (delivery IDs and body
// digests) so a repeated delivery is not acted on twice (#747). It is bounded: at most capacity entries, each
// forgotten after ttl, oldest dropped first when full. Keys are SHA-256
// digests, so memory does not depend on header length. The zero value is ready
// to use with DefaultDeliveryCacheSize and DefaultDeliveryCacheTTL. Safe for
// concurrent use.
type DeliveryCache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	now      func() time.Time
	order    *list.List // front = newest; values are *deliveryEntry
	entries  map[[sha256.Size]byte]*list.Element
}

type deliveryEntry struct {
	key  [sha256.Size]byte
	seen time.Time
}

// NewDeliveryCache returns a cache with the given bounds. A non-positive
// capacity or ttl falls back to the default.
func NewDeliveryCache(capacity int, ttl time.Duration) *DeliveryCache {
	return &DeliveryCache{capacity: capacity, ttl: ttl}
}

// init applies defaults. Caller holds c.mu.
func (c *DeliveryCache) init() {
	if c.entries != nil {
		return
	}
	if c.capacity <= 0 {
		c.capacity = DefaultDeliveryCacheSize
	}
	if c.ttl <= 0 {
		c.ttl = DefaultDeliveryCacheTTL
	}
	if c.now == nil {
		c.now = time.Now
	}
	c.order = list.New()
	c.entries = make(map[[sha256.Size]byte]*list.Element)
}

// Claim records every non-empty key and returns true when none was seen
// within the TTL. It returns false for a repeat of any key, which the caller
// should ignore, and then records nothing. Empty keys (no delivery header, an
// empty body) are skipped; with no keys left it always returns true.
func (c *DeliveryCache) Claim(ids ...string) bool {
	_, ok := c.ClaimMatch(ids...)
	return ok
}

// ClaimMatch is Claim that also returns, on a repeat, the first given id that
// was already recorded, so the caller can log which key matched.
func (c *DeliveryCache) ClaimMatch(ids ...string) (matched string, ok bool) {
	keys := deliveryKeys(ids)
	if len(keys) == 0 {
		return "", true
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.init()

	now := c.now()
	c.evictExpired(now)
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, seen := c.entries[sha256.Sum256([]byte(id))]; seen {
			return id, false
		}
	}
	for _, key := range keys {
		c.entries[key] = c.order.PushFront(&deliveryEntry{key: key, seen: now})
	}
	for c.order.Len() > c.capacity {
		c.remove(c.order.Back())
	}
	return "", true
}

// Seen reports whether id was recorded within the TTL, without recording it
// or changing the cache. An empty id is never seen.
func (c *DeliveryCache) Seen(id string) bool {
	if id == "" {
		return false
	}
	key := sha256.Sum256([]byte(id))

	c.mu.Lock()
	defer c.mu.Unlock()
	c.init()
	el, ok := c.entries[key]
	return ok && c.now().Sub(el.Value.(*deliveryEntry).seen) < c.ttl
}

// Release forgets every given key, so a provider retry after a failed forward
// is processed.
func (c *DeliveryCache) Release(ids ...string) {
	keys := deliveryKeys(ids)
	if len(keys) == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.init()
	for _, key := range keys {
		if el, ok := c.entries[key]; ok {
			c.remove(el)
		}
	}
}

// deliveryKeys digests the non-empty ids, dropping duplicates.
func deliveryKeys(ids []string) [][sha256.Size]byte {
	keys := make([][sha256.Size]byte, 0, len(ids))
	seen := make(map[[sha256.Size]byte]bool, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		key := sha256.Sum256([]byte(id))
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

// evictExpired drops entries older than the TTL. Entries are in insertion
// order, so it stops at the first live one. Caller holds c.mu.
func (c *DeliveryCache) evictExpired(now time.Time) {
	for el := c.order.Back(); el != nil; el = c.order.Back() {
		if now.Sub(el.Value.(*deliveryEntry).seen) < c.ttl {
			return
		}
		c.remove(el)
	}
}

// remove deletes one entry. Caller holds c.mu.
func (c *DeliveryCache) remove(el *list.Element) {
	delete(c.entries, el.Value.(*deliveryEntry).key)
	c.order.Remove(el)
}
