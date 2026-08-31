package captcha

import "time"

// TaskType represents the kind of Captcha challenge to solve.
type TaskType string

const (
	// TurnstileTaskTypes
	TaskTypeTurnstile          TaskType = "TurnstileTask"
	TaskTypeTurnstileProxyless TaskType = "TurnstileTaskProxyless"

	// ReCaptchaTaskTypes
	TaskTypeReCaptchaV2          TaskType = "ReCaptchaV2Task"
	TaskTypeReCaptchaV2Proxyless TaskType = "ReCaptchaV2TaskProxyless"
	TaskTypeReCaptchaV3          TaskType = "ReCaptchaV3Task"
	TaskTypeReCaptchaV3Proxyless TaskType = "ReCaptchaV3TaskProxyless"
	TaskTypeReCaptchaEnterprise  TaskType = "ReCaptchaV2EnterpriseTask"

	// HCaptchaTaskTypes
	TaskTypeHCaptcha          TaskType = "HCaptchaTask"
	TaskTypeHCaptchaProxyless TaskType = "HCaptchaTaskProxyless"

	// Vision & OCR
	TaskTypeImageToText TaskType = "ImageToTextTask"

	// Advanced
	TaskTypeGeeTest   TaskType = "GeeTestTask"
	TaskTypeAmazonWAF TaskType = "AmazonWAFTask"
)

// TaskPayload defines parameters required to solve a captcha.
type TaskPayload struct {
	Type        TaskType          `json:"type"`
	WebsiteURL  string            `json:"websiteURL,omitempty"`
	WebsiteKey  string            `json:"websiteKey,omitempty"`
	PageAction  string            `json:"pageAction,omitempty"`
	MinScore    float64           `json:"minScore,omitempty"`
	Body        string            `json:"body,omitempty"` // For ImageToText base64 string
	Proxy       string            `json:"proxy,omitempty"`
	UserAgent   string            `json:"userAgent,omitempty"`
	MetaData    map[string]any    `json:"metadata,omitempty"`
	ExtraParams map[string]string `json:"extraParams,omitempty"`
}

// CreateTaskRequest is the request sent to Nimbus Cloud API.
type CreateTaskRequest struct {
	ClientKey string      `json:"clientKey"`
	Task      TaskPayload `json:"task"`
	AppID     string      `json:"appID,omitempty"`
}

// CreateTaskResponse is returned after task submission.
type CreateTaskResponse struct {
	ErrorId          int    `json:"errorId"`
	ErrorCode        string `json:"errorCode,omitempty"`
	ErrorDescription string `json:"errorDescription,omitempty"`
	Status           string `json:"status,omitempty"` // "idle", "processing", "ready"
	TaskID           string `json:"taskId,omitempty"`
}

// GetTaskResultRequest requests the outcome of an async captcha task.
type GetTaskResultRequest struct {
	ClientKey string `json:"clientKey"`
	TaskID    string `json:"taskId"`
}

// Solution contains the resulting token or solved output.
type Solution struct {
	Token     string            `json:"token,omitempty"`
	GRecaptchaResponse string   `json:"gRecaptchaResponse,omitempty"`
	Text      string            `json:"text,omitempty"` // OCR result
	UserAgent string            `json:"userAgent,omitempty"`
	RespKey   string            `json:"respKey,omitempty"`
	SolveTime time.Duration     `json:"solveTime,omitempty"`
	Extra     map[string]any    `json:"extra,omitempty"`
}

// GetTaskResultResponse returns status and solution of a captcha task.
type GetTaskResultResponse struct {
	ErrorId          int      `json:"errorId"`
	ErrorCode        string   `json:"errorCode,omitempty"`
	ErrorDescription string   `json:"errorDescription,omitempty"`
	Status           string   `json:"status"` // "processing" or "ready"
	Solution         Solution `json:"solution,omitempty"`
}

// BalanceResponse returns remaining user credit or quota on Nimbus Cloud.
type BalanceResponse struct {
	ErrorId          int     `json:"errorId"`
	ErrorCode        string  `json:"errorCode,omitempty"`
	ErrorDescription string  `json:"errorDescription,omitempty"`
	Balance          float64 `json:"balance"`
}

// VerificationResult contains the result of validating a user's submitted token (Turnstile/reCAPTCHA).
type VerificationResult struct {
	Success     bool      `json:"success"`
	ChallengeTS time.Time `json:"challenge_ts,omitempty"`
	Hostname    string    `json:"hostname,omitempty"`
	Score       float64   `json:"score,omitempty"`
	Action      string    `json:"action,omitempty"`
	ErrorCodes  []string  `json:"error-codes,omitempty"`
}
