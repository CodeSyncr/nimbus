// Package serverless adapts a Nimbus application (any http.Handler, such as
// app.Router) to serverless invocation models — starting with AWS Lambda.
//
// It has no AWS SDK dependency: it converts the Lambda proxy event JSON to a
// standard *http.Request, runs it through your handler, and converts the
// recorded response back to the Lambda proxy response shape. Your Lambda entry
// point wires it to the AWS runtime:
//
//	// cmd/lambda/main.go
//	package main
//
//	import (
//	    "github.com/aws/aws-lambda-go/lambda"
//	    "github.com/CodeSyncr/nimbus/serverless"
//	    "yourapp/bin"
//	)
//
//	func main() {
//	    app := bin.Boot()      // build the app (routes + middleware)
//	    _ = app.Boot()         // run providers/plugins (no HTTP listener)
//	    lambda.Start(serverless.Lambda(app.Router))
//	}
//
// Supports both API Gateway / Lambda Function URL payload formats: v2.0
// (HTTP API, Function URLs) and v1.0 (REST API, ALB).
package serverless

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Response is the Lambda proxy response shape shared by API Gateway v1/v2,
// Function URLs, and ALB. The AWS runtime marshals this to JSON.
type Response struct {
	StatusCode        int                 `json:"statusCode"`
	Headers           map[string]string   `json:"headers,omitempty"`
	MultiValueHeaders map[string][]string `json:"multiValueHeaders,omitempty"`
	Cookies           []string            `json:"cookies,omitempty"` // v2 only
	Body              string              `json:"body"`
	IsBase64Encoded   bool                `json:"isBase64Encoded"`
}

// Handler is the function signature aws-lambda-go's lambda.Start accepts.
type Handler func(ctx context.Context, payload json.RawMessage) (Response, error)

// Lambda adapts an http.Handler (e.g. app.Router) into a Lambda handler.
func Lambda(h http.Handler) Handler {
	return func(ctx context.Context, payload json.RawMessage) (Response, error) {
		req, v2, err := eventToRequest(ctx, payload)
		if err != nil {
			return Response{StatusCode: http.StatusBadRequest, Body: "invalid event: " + err.Error()}, nil
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return recorderToResponse(rec, v2), nil
	}
}

// ── Event → *http.Request ──────────────────────────────────────────

// proxyEvent is a superset of the v1 and v2 proxy event fields we need.
type proxyEvent struct {
	Version string `json:"version"`

	// v2.0 (HTTP API / Function URL)
	RawPath        string   `json:"rawPath"`
	RawQueryString string   `json:"rawQueryString"`
	Cookies        []string `json:"cookies"`
	RequestContext struct {
		HTTP struct {
			Method   string `json:"method"`
			Path     string `json:"path"`
			SourceIP string `json:"sourceIp"`
		} `json:"http"`
	} `json:"requestContext"`

	// v1.0 (REST API / ALB)
	HTTPMethod                      string              `json:"httpMethod"`
	Path                            string              `json:"path"`
	QueryStringParameters           map[string]string   `json:"queryStringParameters"`
	MultiValueQueryStringParameters map[string][]string `json:"multiValueQueryStringParameters"`
	MultiValueHeaders               map[string][]string `json:"multiValueHeaders"`

	// shared
	Headers         map[string]string `json:"headers"`
	Body            string            `json:"body"`
	IsBase64Encoded bool              `json:"isBase64Encoded"`
}

func eventToRequest(ctx context.Context, payload json.RawMessage) (*http.Request, bool, error) {
	var e proxyEvent
	if err := json.Unmarshal(payload, &e); err != nil {
		return nil, false, err
	}

	// Detect payload format: v2 exposes the method under requestContext.http.
	v2 := e.Version == "2.0" || e.RequestContext.HTTP.Method != ""

	method := e.HTTPMethod
	path := e.Path
	rawQuery := ""
	if v2 {
		method = e.RequestContext.HTTP.Method
		path = e.RawPath
		if path == "" {
			path = e.RequestContext.HTTP.Path
		}
		rawQuery = e.RawQueryString
	} else {
		rawQuery = encodeV1Query(e.QueryStringParameters, e.MultiValueQueryStringParameters)
	}
	if method == "" {
		method = http.MethodGet
	}
	if path == "" {
		path = "/"
	}

	// Decode the body (base64 for binary payloads).
	var body io.Reader
	if e.Body != "" {
		if e.IsBase64Encoded {
			raw, err := base64.StdEncoding.DecodeString(e.Body)
			if err != nil {
				return nil, v2, err
			}
			body = strings.NewReader(string(raw))
		} else {
			body = strings.NewReader(e.Body)
		}
	}

	target := path
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, v2, err
	}

	// Headers: v1 may carry multiValueHeaders; v2 folds everything into headers.
	for k, vs := range e.MultiValueHeaders {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	for k, v := range e.Headers {
		if _, ok := e.MultiValueHeaders[k]; ok {
			continue // already added from multi-value form
		}
		req.Header.Set(k, v)
	}
	// v2 delivers cookies out-of-band; rejoin them into a Cookie header.
	if len(e.Cookies) > 0 {
		req.Header.Set("Cookie", strings.Join(e.Cookies, "; "))
	}

	if host := req.Header.Get("Host"); host != "" {
		req.Host = host
	}
	req.RemoteAddr = e.RequestContext.HTTP.SourceIP
	return req, v2, nil
}

func encodeV1Query(single map[string]string, multi map[string][]string) string {
	vals := url.Values{}
	for k, vs := range multi {
		for _, v := range vs {
			vals.Add(k, v)
		}
	}
	for k, v := range single {
		if _, ok := multi[k]; ok {
			continue
		}
		vals.Set(k, v)
	}
	return vals.Encode()
}

// ── ResponseRecorder → Response ────────────────────────────────────

func recorderToResponse(rec *httptest.ResponseRecorder, v2 bool) Response {
	status := rec.Code
	if status == 0 {
		status = http.StatusOK
	}

	resp := Response{StatusCode: status}
	body := rec.Body.Bytes()

	// Binary-safe: base64-encode bodies that are not valid UTF-8.
	if len(body) > 0 && !utf8.Valid(body) {
		resp.Body = base64.StdEncoding.EncodeToString(body)
		resp.IsBase64Encoded = true
	} else {
		resp.Body = string(body)
	}

	h := rec.Header()
	// Set-Cookie must not be comma-folded. v2 uses the dedicated cookies field;
	// v1 uses multiValueHeaders so each cookie is emitted separately.
	cookies := h.Values("Set-Cookie")

	if v2 {
		resp.Headers = map[string]string{}
		for k, vs := range h {
			if http.CanonicalHeaderKey(k) == "Set-Cookie" {
				continue
			}
			resp.Headers[k] = strings.Join(vs, ", ")
		}
		resp.Cookies = cookies
	} else {
		resp.MultiValueHeaders = map[string][]string{}
		for k, vs := range h {
			resp.MultiValueHeaders[k] = vs
		}
	}
	return resp
}
