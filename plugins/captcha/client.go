package captcha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client handles interaction with the Nimbus Cloud Captcha API (CapSolver alternative).
type Client struct {
	config     *Config
	httpClient *http.Client
}

// NewClient initializes a new Captcha API Client.
func NewClient(cfg *Config) (*Client, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://api.nimbuscloud.io/v1/captcha"
	}

	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}

	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}, nil
}

// CreateTask submits a new captcha task to Nimbus Cloud.
func (c *Client) CreateTask(ctx context.Context, payload TaskPayload) (*CreateTaskResponse, error) {
	if c.config.MockMode {
		return &CreateTaskResponse{
			ErrorId: 0,
			Status:  "ready",
			TaskID:  "mock-task-id-12345",
		}, nil
	}

	reqBody := CreateTaskRequest{
		ClientKey: c.config.APIKey,
		Task:      payload,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("captcha: failed to marshal create task request: %w", err)
	}

	endpoint := strings.TrimRight(c.config.Endpoint, "/") + "/createTask"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("captcha: failed to create http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("captcha: network error on createTask: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("captcha: failed to read response body: %w", err)
	}

	var res CreateTaskResponse
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("captcha: failed to unmarshal response (%s): %w", string(bodyBytes), err)
	}

	if res.ErrorId != 0 {
		return &res, fmt.Errorf("captcha API error [%s]: %s", res.ErrorCode, res.ErrorDescription)
	}

	return &res, nil
}

// GetTaskResult queries the current status and solution for a task ID.
func (c *Client) GetTaskResult(ctx context.Context, taskID string) (*GetTaskResultResponse, error) {
	if c.config.MockMode {
		return &GetTaskResultResponse{
			ErrorId: 0,
			Status:  "ready",
			Solution: Solution{
				Token:     "mock-captcha-token-approved",
				UserAgent: "NimbusCaptchaMock/1.0",
				Text:      "MOCK_OCR_RESULT",
			},
		}, nil
	}

	reqBody := GetTaskResultRequest{
		ClientKey: c.config.APIKey,
		TaskID:    taskID,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("captcha: failed to marshal task result request: %w", err)
	}

	endpoint := strings.TrimRight(c.config.Endpoint, "/") + "/getTaskResult"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("captcha: failed to create http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("captcha: network error on getTaskResult: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("captcha: failed to read response body: %w", err)
	}

	var res GetTaskResultResponse
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("captcha: failed to unmarshal response: %w", err)
	}

	if res.ErrorId != 0 {
		return &res, fmt.Errorf("captcha API error [%s]: %s", res.ErrorCode, res.ErrorDescription)
	}

	return &res, nil
}

// Solve is a high-level helper that creates a task and polls until solved or timed out.
func (c *Client) Solve(ctx context.Context, payload TaskPayload) (*Solution, error) {
	start := time.Now()

	createRes, err := c.CreateTask(ctx, payload)
	if err != nil {
		return nil, err
	}

	if c.config.MockMode {
		return &Solution{
			Token:     "mock-captcha-token-approved",
			Text:      "MOCK_OCR_RESULT",
			SolveTime: time.Since(start),
		}, nil
	}

	taskID := createRes.TaskID
	if taskID == "" {
		return nil, fmt.Errorf("captcha: received empty taskId from createTask")
	}

	pollInterval := c.config.PollingInterval
	if pollInterval <= 0 {
		pollInterval = 1 * time.Second
	}

	maxRetries := c.config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 60
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for i := 0; i < maxRetries; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			res, err := c.GetTaskResult(ctx, taskID)
			if err != nil {
				return nil, err
			}

			if res.Status == "ready" {
				res.Solution.SolveTime = time.Since(start)
				return &res.Solution, nil
			}
		}
	}

	return nil, fmt.Errorf("captcha: solving task %s timed out after %d retries", taskID, maxRetries)
}

// GetBalance checks the remaining credit on the Nimbus Cloud API key.
func (c *Client) GetBalance(ctx context.Context) (float64, error) {
	if c.config.MockMode {
		return 999.99, nil
	}

	reqBody := map[string]string{
		"clientKey": c.config.APIKey,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return 0, fmt.Errorf("captcha: failed to marshal balance request: %w", err)
	}

	endpoint := strings.TrimRight(c.config.Endpoint, "/") + "/getBalance"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("captcha: failed to create balance request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("captcha: network error on getBalance: %w", err)
	}
	defer resp.Body.Close()

	var res BalanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return 0, fmt.Errorf("captcha: failed to decode balance response: %w", err)
	}

	if res.ErrorId != 0 {
		return 0, fmt.Errorf("captcha API error [%s]: %s", res.ErrorCode, res.ErrorDescription)
	}

	return res.Balance, nil
}
