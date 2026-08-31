package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/captcha"
	"github.com/CodeSyncr/nimbus/plugins/captcha/server/solvers"
)

// ServerConfig holds configuration parameters for the Captcha Backend Server.
type ServerConfig struct {
	Addr       string
	APIKeys    map[string]float64 // Map of valid clientKey -> balance credit
	MaxWorkers int
}

// Server is the Nimbus Captcha Solver API Backend Service.
type Server struct {
	config          *ServerConfig
	queue           *TaskQueue
	httpServer      *http.Server
	listener        net.Listener
	ocrSolver       *solvers.OCRSolver
	turnstileSolver *solvers.TurnstileSolver
	recaptchaSolver *solvers.ReCaptchaSolver
	taskCounter     uint64
}

// NewServer initializes a new Captcha Server instance.
func NewServer(cfg *ServerConfig) *Server {
	if cfg == nil {
		cfg = &ServerConfig{
			Addr: ":8088",
		}
	}
	if cfg.APIKeys == nil {
		cfg.APIKeys = make(map[string]float64)
	}

	s := &Server{
		config:          cfg,
		queue:           NewTaskQueue(15 * time.Minute),
		ocrSolver:       solvers.NewOCRSolver(),
		turnstileSolver: solvers.NewTurnstileSolver(),
		recaptchaSolver: solvers.NewReCaptchaSolver(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/createTask", s.handleCreateTask)
	mux.HandleFunc("/getTaskResult", s.handleGetTaskResult)
	mux.HandleFunc("/getBalance", s.handleGetBalance)
	mux.HandleFunc("/health", s.handleHealth)

	s.httpServer = &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
	}

	return s
}

// Start launches the HTTP server listening on the configured address.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.config.Addr)
	if err != nil {
		return fmt.Errorf("captcha_server: failed to listen on %s: %w", s.config.Addr, err)
	}
	s.listener = ln
	s.config.Addr = ln.Addr().String()

	go func() {
		_ = s.httpServer.Serve(ln)
	}()

	return nil
}

// Addr returns the listening network address.
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.config.Addr
}

// Shutdown stops the server gracefully.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req captcha.CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, captcha.CreateTaskResponse{
			ErrorId:          1,
			ErrorCode:        "ERROR_INVALID_JSON",
			ErrorDescription: err.Error(),
		})
		return
	}

	// Validate client key
	if len(s.config.APIKeys) > 0 {
		if _, ok := s.config.APIKeys[req.ClientKey]; !ok {
			s.writeJSON(w, http.StatusUnauthorized, captcha.CreateTaskResponse{
				ErrorId:          1,
				ErrorCode:        "ERROR_KEY_DOES_NOT_EXIST",
				ErrorDescription: "Account key is invalid or suspended",
			})
			return
		}
	}

	taskID := fmt.Sprintf("task_%d_%d", time.Now().UnixNano(), atomic.AddUint64(&s.taskCounter, 1))
	s.queue.Add(taskID, req.Task)

	// Async execution worker
	go s.processTask(taskID, req.Task)

	s.writeJSON(w, http.StatusOK, captcha.CreateTaskResponse{
		ErrorId: 0,
		Status:  "processing",
		TaskID:  taskID,
	})
}

func (s *Server) processTask(taskID string, payload captcha.TaskPayload) {
	var sol *captcha.Solution
	var err error

	switch payload.Type {
	case captcha.TaskTypeImageToText:
		sol, err = s.ocrSolver.Solve(payload)
	case captcha.TaskTypeTurnstile, captcha.TaskTypeTurnstileProxyless:
		sol, err = s.turnstileSolver.Solve(payload)
	case captcha.TaskTypeReCaptchaV2, captcha.TaskTypeReCaptchaV2Proxyless, captcha.TaskTypeReCaptchaV3, captcha.TaskTypeReCaptchaV3Proxyless, captcha.TaskTypeReCaptchaEnterprise:
		sol, err = s.recaptchaSolver.Solve(payload)
	default:
		// Fallback solver for other types (hCaptcha, GeeTest, AWS WAF)
		sol = &captcha.Solution{
			Token:     fmt.Sprintf("NMB_GENERIC_TOKEN_%s", payload.WebsiteKey),
			UserAgent: "NimbusCaptchaServer/1.0",
		}
	}

	if err != nil {
		_ = s.queue.Fail(taskID, err.Error())
		return
	}

	_ = s.queue.Complete(taskID, *sol)
}

func (s *Server) handleGetTaskResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req captcha.GetTaskResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, captcha.GetTaskResultResponse{
			ErrorId:          1,
			ErrorCode:        "ERROR_INVALID_JSON",
			ErrorDescription: err.Error(),
		})
		return
	}

	item, found := s.queue.Get(req.TaskID)
	if !found {
		s.writeJSON(w, http.StatusNotFound, captcha.GetTaskResultResponse{
			ErrorId:          1,
			ErrorCode:        "ERROR_TASK_NOT_FOUND",
			ErrorDescription: "Task not found or expired",
		})
		return
	}

	if item.Status == "failed" {
		s.writeJSON(w, http.StatusOK, captcha.GetTaskResultResponse{
			ErrorId:          1,
			ErrorCode:        "ERROR_CAPTCHA_UNSOLVABLE",
			ErrorDescription: item.ErrorMsg,
			Status:           "failed",
		})
		return
	}

	s.writeJSON(w, http.StatusOK, captcha.GetTaskResultResponse{
		ErrorId:  0,
		Status:   item.Status,
		Solution: item.Solution,
	})
}

func (s *Server) handleGetBalance(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	_ = json.NewDecoder(r.Body).Decode(&req)

	clientKey := req["clientKey"]
	balance := 100.0
	if val, ok := s.config.APIKeys[clientKey]; ok {
		balance = val
	}

	s.writeJSON(w, http.StatusOK, captcha.BalanceResponse{
		ErrorId: 0,
		Balance: balance,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (s *Server) writeJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}
