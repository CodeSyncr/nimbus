package supabase

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client is the core Supabase client that all sub-clients use.
type Client struct {
	url            string
	anonKey        string
	serviceRoleKey string
	jwtSecret      string
	httpClient     *http.Client

	// Sub-clients for each Supabase service.
	Storage   *StorageClient
	Functions *FunctionsClient
	Realtime  *RealtimeClient
	Auth      *AuthClient
}

var (
	globalClient *Client
	globalMu     sync.RWMutex
)

func SetGlobal(c *Client) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalClient = c
}

func GetClient() *Client {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalClient
}

// NewClient creates a Supabase client from the given config.
func NewClient(cfg Config) *Client {
	url := strings.TrimSuffix(cfg.URL, "/")
	c := &Client{
		url:            url,
		anonKey:        cfg.AnonKey,
		serviceRoleKey: cfg.ServiceRoleKey,
		jwtSecret:      cfg.JWTSecret,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	c.Storage = &StorageClient{client: c}
	c.Functions = &FunctionsClient{client: c}
	c.Realtime = &RealtimeClient{
		client:   c,
		channels: make(map[string]*Channel),
	}
	c.Auth = &AuthClient{client: c}
	return c
}

// apiKey returns the best available key (service role > anon).
func (c *Client) apiKey() string {
	if c.serviceRoleKey != "" {
		return c.serviceRoleKey
	}
	return c.anonKey
}

func (c *Client) do(method, url string, body io.Reader, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("apikey", c.apiKey())
	req.Header.Set("Authorization", "Bearer "+c.apiKey())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return c.httpClient.Do(req)
}

func (c *Client) doJSON(method, url string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("supabase: marshal: %w", err)
		}
		body = bytes.NewReader(b)
	}
	return c.do(method, url, body, map[string]string{
		"Content-Type": "application/json",
	})
}

func decodeJSON[T any](resp *http.Response) (T, error) {
	var result T
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return result, fmt.Errorf("supabase: %s %s: %d %s", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, string(b))
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result, fmt.Errorf("supabase: decode: %w", err)
	}
	return result, nil
}

func drainResp(resp *http.Response) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase: %s %s: %d %s", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, string(b))
	}
	return nil
}
