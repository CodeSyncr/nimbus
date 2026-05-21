package supabase

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// FunctionsClient invokes Supabase Edge Functions.
type FunctionsClient struct {
	client *Client
}

// InvokeOptions configures an Edge Function invocation.
type InvokeOptions struct {
	Headers map[string]string
	Method  string
}

// FunctionResponse is the result of invoking an Edge Function.
type FunctionResponse struct {
	StatusCode int
	Headers    http.Header
	body       []byte
}

// Body returns the raw response body.
func (r *FunctionResponse) Body() []byte { return r.body }

// JSON decodes the response body into v.
func (r *FunctionResponse) JSON(v any) error {
	return json.Unmarshal(r.body, v)
}

// Text returns the response body as a string.
func (r *FunctionResponse) Text() string { return string(r.body) }

// Invoke calls a Supabase Edge Function by name with a JSON payload.
//
//	resp, err := client.Functions.Invoke("hello", map[string]any{"name": "World"})
//	var result map[string]string
//	resp.JSON(&result)
func (f *FunctionsClient) Invoke(name string, payload any, opts ...InvokeOptions) (*FunctionResponse, error) {
	u := f.client.url + "/functions/v1/" + name

	method := "POST"
	var extraHeaders map[string]string
	if len(opts) > 0 {
		if opts[0].Method != "" {
			method = opts[0].Method
		}
		extraHeaders = opts[0].Headers
	}

	var body io.Reader
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("supabase: functions marshal: %w", err)
		}
		body = bytes.NewReader(b)
	}
	for k, v := range extraHeaders {
		headers[k] = v
	}

	resp, err := f.client.do(method, u, body, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("supabase: functions read body: %w", err)
	}

	return &FunctionResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		body:       respBody,
	}, nil
}

// InvokeRaw calls an Edge Function with a raw body (non-JSON).
func (f *FunctionsClient) InvokeRaw(name string, body io.Reader, contentType string) (*FunctionResponse, error) {
	u := f.client.url + "/functions/v1/" + name

	resp, err := f.client.do("POST", u, body, map[string]string{
		"Content-Type": contentType,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("supabase: functions read body: %w", err)
	}

	return &FunctionResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		body:       respBody,
	}, nil
}
