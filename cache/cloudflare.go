package cache

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const cloudflarePrefix = "nimbus:cache:"

// CloudflareKVStore uses Cloudflare Workers KV for edge-distributed caching.
// Requires CLOUDFLARE_ACCOUNT_ID, CLOUDFLARE_NAMESPACE_ID, CLOUDFLARE_API_TOKEN.
// KV is eventually consistent; writes may take up to 60s to propagate globally.
type CloudflareKVStore struct {
	client    *http.Client
	accountID string
	namespace string
	token     string
	prefix    string
	baseURL   string
}

// NewCloudflareKVStore creates a Cloudflare KV cache store.
func NewCloudflareKVStore(accountID, namespaceID, apiToken string) *CloudflareKVStore {
	return &CloudflareKVStore{
		client:    &http.Client{Timeout: 30 * time.Second},
		accountID: accountID,
		namespace: namespaceID,
		token:     apiToken,
		prefix:    cloudflarePrefix,
		baseURL:   "https://api.cloudflare.com/client/v4",
	}
}

// NewCloudflareKVStoreWithPrefix creates a Cloudflare KV store with a custom key prefix.
func NewCloudflareKVStoreWithPrefix(accountID, namespaceID, apiToken, prefix string) *CloudflareKVStore {
	s := NewCloudflareKVStore(accountID, namespaceID, apiToken)
	s.prefix = prefix
	return s
}

func (c *CloudflareKVStore) key(k string) string {
	return c.prefix + k
}

func (c *CloudflareKVStore) valuesURL(key string, expirationTTL int) string {
	u := fmt.Sprintf("%s/accounts/%s/storage/kv/namespaces/%s/values/%s",
		c.baseURL, c.accountID, c.namespace, url.PathEscape(key))
	if expirationTTL > 0 {
		u += fmt.Sprintf("?expiration_ttl=%d", expirationTTL)
	}
	return u
}

// Set stores a value. Values are JSON-serialized. Min TTL: 60 seconds for Cloudflare KV.
func (c *CloudflareKVStore) Set(key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	k := c.key(key)
	expTTL := 0
	if ttl >= 60*time.Second {
		expTTL = int(ttl.Seconds())
	} else if ttl > 0 {
		expTTL = 60 // Cloudflare minimum
	}
	req, err := http.NewRequest(http.MethodPut, c.valuesURL(k, expTTL), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cloudflare kv: put %s: %s %s", key, resp.Status, string(body))
	}
	return nil
}

// Get returns the value and true if found.
func (c *CloudflareKVStore) Get(key string) (any, bool) {
	k := c.key(key)
	req, err := http.NewRequest(http.MethodGet, c.valuesURL(k, 0), nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, false
	}
	return v, true
}

// Delete removes a key.
func (c *CloudflareKVStore) Delete(key string) error {
	k := c.key(key)
	u := fmt.Sprintf("%s/accounts/%s/storage/kv/namespaces/%s/values/%s",
		c.baseURL, c.accountID, c.namespace, url.PathEscape(k))
	req, err := http.NewRequest(http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("cloudflare kv: delete %s: %s", key, resp.Status)
	}
	return nil
}

// Remember returns the cached value or calls fn, stores the result, and returns it.
func (c *CloudflareKVStore) Remember(key string, ttl time.Duration, fn func() (any, error)) (any, error) {
	if v, ok := c.Get(key); ok {
		return v, nil
	}
	v, err := fn()
	if err != nil {
		return nil, err
	}
	_ = c.Set(key, v, ttl)
	return v, nil
}

var _ Store = (*CloudflareKVStore)(nil)
