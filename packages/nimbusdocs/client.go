/*
|--------------------------------------------------------------------------
| Nimbus Cloud documents — Go client
|--------------------------------------------------------------------------
|
| Server-side client for the Sigma and Carbon document APIs. It holds the
| API key, so it belongs in your backend; the browser only ever receives
| the signed link this client returns.
|
|   sigma, _ := nimbusdocs.New(nimbusdocs.Sigma, os.Getenv("SIGMA_API_KEY"))
|   doc, err := sigma.Upload(ctx, nimbusdocs.Upload{
|       Content: csv, Filename: "q3.csv", IdempotencyKey: order.ID,
|   })
|   // hand doc.URL to the page
|
*/

package nimbusdocs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Product selects which app a client talks to.
type Product string

// The products served by the API.
const (
	Sigma  Product = "sigma"
	Carbon Product = "carbon"
)

// DefaultBaseURL is where the API lives.
const DefaultBaseURL = "https://nimbusgo.space"

// Client talks to one product with one key.
type Client struct {
	product Product
	apiKey  string
	baseURL string
	http    *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at another deployment, for local testing.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithHTTPClient supplies the transport.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithTimeout bounds each request. Default 30s.
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.http.Timeout = d } }

// New builds a client. The key must belong to the product: a Carbon key
// presented to Sigma is refused by the API, not silently accepted.
func New(product Product, apiKey string, opts ...Option) (*Client, error) {
	if product != Sigma && product != Carbon {
		return nil, fmt.Errorf("nimbusdocs: unknown product %q", product)
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("nimbusdocs: an API key is required")
	}
	c := &Client{
		product: product,
		apiKey:  apiKey,
		baseURL: DefaultBaseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// Product this client serves.
func (c *Client) Product() Product { return c.product }

// ── types ─────────────────────────────────────────────────────────────────

// Document is one stored document. URL fields are set on responses that
// minted a link.
type Document struct {
	ID           uint       `json:"id"`
	Product      Product    `json:"product"`
	Title        string     `json:"title"`
	Filename     string     `json:"filename"`
	MIME         string     `json:"mime"`
	Size         int64      `json:"size"`
	Checksum     string     `json:"checksum"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	URL          string     `json:"url,omitempty"`
	URLExpiresAt *time.Time `json:"url_expires_at,omitempty"`
	Mode         string     `json:"mode,omitempty"`
}

// Link is a freshly minted signed URL.
type Link struct {
	URL       string    `json:"url"`
	Mode      string    `json:"mode"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Quota is this period's allowance.
type Quota struct {
	Period      string `json:"period"`
	Limit       int    `json:"limit"`
	Used        int    `json:"used"`
	Remaining   int    `json:"remaining"`
	Overage     int    `json:"overage"`
	UsedPercent int    `json:"used_percent"`
	Allowed     bool   `json:"allowed"`
	Warn        bool   `json:"warn"`
	Paid        bool   `json:"paid"`
}

// Upload describes a document to create.
type Upload struct {
	Content  []byte
	Filename string
	Title    string
	MIME     string
	// TTL of the link returned with the document. Zero means the default.
	TTL  time.Duration
	Mode string
	// RetainDays deletes the document after this many days. Zero keeps it.
	RetainDays int
	// IdempotencyKey makes a retried upload return the original document
	// instead of creating, and charging for, a second one.
	IdempotencyKey string
}

// LinkOptions shape a re-minted link.
type LinkOptions struct {
	TTL  time.Duration
	Mode string
}

// Error is every failure the API reports. Code is stable; branch on it.
type Error struct {
	Code    string
	Status  int
	Message string
	Quota   *Quota
}

func (e *Error) Error() string { return "nimbusdocs: " + e.Message }

// Stable error codes.
const (
	CodeUnauthorized  = "unauthorized"
	CodeQuotaExceeded = "quota_exceeded"
	CodeNotFound      = "not_found"
	CodeTooLarge      = "too_large"
	CodeInvalid       = "invalid_request"
	CodeRateLimited   = "rate_limited"
	CodeServer        = "server_error"
)

// ── calls ─────────────────────────────────────────────────────────────────

// Upload creates a document and returns it with a signed URL.
func (c *Client) Upload(ctx context.Context, in Upload) (*Document, error) {
	if len(in.Content) == 0 {
		return nil, &Error{Code: CodeInvalid, Message: "content is required"}
	}
	q := url.Values{}
	if in.TTL > 0 {
		q.Set("ttl", strconv.Itoa(int(in.TTL.Seconds())))
	}
	if in.Mode != "" {
		q.Set("mode", in.Mode)
	}
	if in.RetainDays > 0 {
		q.Set("retain_days", strconv.Itoa(in.RetainDays))
	}
	title := in.Title
	if title == "" {
		title = in.Filename
	}
	body, _ := json.Marshal(map[string]string{
		"title": title, "filename": in.Filename, "mime": in.MIME, "content": string(in.Content),
	})
	headers := map[string]string{"Content-Type": "application/json"}
	if in.IdempotencyKey != "" {
		headers["Idempotency-Key"] = in.IdempotencyKey
	}
	var doc Document
	if err := c.do(ctx, http.MethodPost, "/documents", q, body, headers, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// List returns documents, newest first.
func (c *Client) List(ctx context.Context, limit int) ([]Document, error) {
	if limit <= 0 {
		limit = 50
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	var out struct {
		Documents []Document `json:"documents"`
	}
	if err := c.do(ctx, http.MethodGet, "/documents", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Documents, nil
}

// Get returns one document's metadata.
func (c *Client) Get(ctx context.Context, id uint) (*Document, error) {
	var doc Document
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/documents/%d", id), nil, nil, nil, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Link mints a fresh signed URL — the answer to an expired one.
func (c *Client) Link(ctx context.Context, id uint, opts LinkOptions) (*Link, error) {
	q := url.Values{}
	if opts.TTL > 0 {
		q.Set("ttl", strconv.Itoa(int(opts.TTL.Seconds())))
	}
	if opts.Mode != "" {
		q.Set("mode", opts.Mode)
	}
	var l Link
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/documents/%d/link", id), q, nil, nil, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// Delete removes a document and its content.
func (c *Client) Delete(ctx context.Context, id uint) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/documents/%d", id), nil, nil, nil, nil)
}

// RevokeAllLinks invalidates every outstanding signed link at once.
func (c *Client) RevokeAllLinks(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/links/revoke", nil, nil, nil, nil)
}

// Usage reports this period's allowance.
func (c *Client) Usage(ctx context.Context) (*Quota, error) {
	var q Quota
	if err := c.do(ctx, http.MethodGet, "/usage", nil, nil, nil, &q); err != nil {
		return nil, err
	}
	return &q, nil
}

// ── transport ─────────────────────────────────────────────────────────────

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body []byte, headers map[string]string, out any) error {
	u := c.baseURL + "/api/v1/" + string(c.product) + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return &Error{Code: CodeInvalid, Message: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return &Error{Code: CodeServer, Message: "could not reach Nimbus Cloud: " + err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			Error string `json:"error"`
			Quota *Quota `json:"quota"`
		}
		_ = json.Unmarshal(raw, &payload)
		msg := payload.Error
		if msg == "" {
			msg = fmt.Sprintf("Nimbus Cloud returned %d", resp.StatusCode)
		}
		return &Error{Code: codeFor(resp.StatusCode), Status: resp.StatusCode, Message: msg, Quota: payload.Quota}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return &Error{Code: CodeServer, Status: resp.StatusCode, Message: "unexpected response: " + err.Error()}
		}
	}
	return nil
}

func codeFor(status int) string {
	switch status {
	case 401, 403:
		return CodeUnauthorized
	case 402:
		return CodeQuotaExceeded
	case 404:
		return CodeNotFound
	case 413:
		return CodeTooLarge
	case 429:
		return CodeRateLimited
	case 400:
		return CodeInvalid
	}
	if status >= 500 {
		return CodeServer
	}
	return CodeInvalid
}
