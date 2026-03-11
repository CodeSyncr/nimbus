// Package http re-exports commonly used net/http symbols so that
// consumers only need a single import: "github.com/CodeSyncr/nimbus/http".
package http

import stdlib "net/http"

// ── Types ───────────────────────────────────────────────────────

// Handler is a re-export of net/http.Handler.
type Handler = stdlib.Handler

// HandlerFunc is a re-export of net/http.HandlerFunc.
// Note: renamed to StdHandlerFunc to avoid conflict with router.HandlerFunc.
type StdHandlerFunc = stdlib.HandlerFunc

// Request is a re-export of net/http.Request.
type Request = stdlib.Request

// ResponseWriter is a re-export of net/http.ResponseWriter.
type ResponseWriter = stdlib.ResponseWriter

// Header is a re-export of net/http.Header.
type Header = stdlib.Header

// Cookie is a re-export of net/http.Cookie.
type Cookie = stdlib.Cookie

// ServeMux is a re-export of net/http.ServeMux.
type ServeMux = stdlib.ServeMux

// FileSystem is a re-export of net/http.FileSystem.
type FileSystem = stdlib.FileSystem

// Dir is a re-export of net/http.Dir.
type Dir = stdlib.Dir

// SameSite allows a server to define a cookie attribute making it impossible
// for the browser to send this cookie along with cross-site requests.
type SameSite = stdlib.SameSite

// Flusher is implemented by ResponseWriters that allow flushing buffered data.
type Flusher = stdlib.Flusher

// SameSite mode constants.
const (
	SameSiteDefaultMode = stdlib.SameSiteDefaultMode
	SameSiteLaxMode     = stdlib.SameSiteLaxMode
	SameSiteStrictMode  = stdlib.SameSiteStrictMode
	SameSiteNoneMode    = stdlib.SameSiteNoneMode
)

// ── HTTP Method constants ───────────────────────────────────────

const (
	MethodGet     = stdlib.MethodGet
	MethodHead    = stdlib.MethodHead
	MethodPost    = stdlib.MethodPost
	MethodPut     = stdlib.MethodPut
	MethodPatch   = stdlib.MethodPatch
	MethodDelete  = stdlib.MethodDelete
	MethodConnect = stdlib.MethodConnect
	MethodOptions = stdlib.MethodOptions
	MethodTrace   = stdlib.MethodTrace
)

// ── Status code constants ───────────────────────────────────────

const (
	StatusContinue           = stdlib.StatusContinue
	StatusSwitchingProtocols = stdlib.StatusSwitchingProtocols

	StatusOK             = stdlib.StatusOK
	StatusCreated        = stdlib.StatusCreated
	StatusAccepted       = stdlib.StatusAccepted
	StatusNoContent      = stdlib.StatusNoContent
	StatusResetContent   = stdlib.StatusResetContent
	StatusPartialContent = stdlib.StatusPartialContent

	StatusMovedPermanently  = stdlib.StatusMovedPermanently
	StatusFound             = stdlib.StatusFound
	StatusSeeOther          = stdlib.StatusSeeOther
	StatusNotModified       = stdlib.StatusNotModified
	StatusTemporaryRedirect = stdlib.StatusTemporaryRedirect
	StatusPermanentRedirect = stdlib.StatusPermanentRedirect

	StatusBadRequest          = stdlib.StatusBadRequest
	StatusUnauthorized        = stdlib.StatusUnauthorized
	StatusPaymentRequired     = stdlib.StatusPaymentRequired
	StatusForbidden           = stdlib.StatusForbidden
	StatusNotFound            = stdlib.StatusNotFound
	StatusMethodNotAllowed    = stdlib.StatusMethodNotAllowed
	StatusConflict            = stdlib.StatusConflict
	StatusGone                = stdlib.StatusGone
	StatusUnprocessableEntity = stdlib.StatusUnprocessableEntity
	StatusTooManyRequests     = stdlib.StatusTooManyRequests

	StatusInternalServerError = stdlib.StatusInternalServerError
	StatusNotImplemented      = stdlib.StatusNotImplemented
	StatusBadGateway          = stdlib.StatusBadGateway
	StatusServiceUnavailable  = stdlib.StatusServiceUnavailable
	StatusGatewayTimeout      = stdlib.StatusGatewayTimeout
)

// ── Functions ───────────────────────────────────────────────────

// Error replies to the request with the specified error message and HTTP code.
var Error = stdlib.Error

// NotFoundHandler returns a simple handler that replies with a 404 not found error.
var NotFoundHandler = stdlib.NotFoundHandler

// Redirect replies to the request with a redirect to url.
var StdRedirect = stdlib.Redirect

// StripPrefix returns a handler that serves HTTP requests by removing the
// given prefix from the request URL's Path.
var StripPrefix = stdlib.StripPrefix

// FileServer returns a handler that serves HTTP requests with the contents
// of the file system rooted at root.
var FileServer = stdlib.FileServer

// ListenAndServe listens on the TCP network address addr and then calls
// Serve with handler to handle requests on incoming connections.
var ListenAndServe = stdlib.ListenAndServe

// StatusText returns a text for the HTTP status code.
var StatusText = stdlib.StatusText

// SetCookie adds a Set-Cookie header to the provided ResponseWriter's headers.
var SetCookie = stdlib.SetCookie

// NewServeMux allocates and returns a new ServeMux.
var NewServeMux = stdlib.NewServeMux

// MaxBytesReader limits the size of an incoming request body.
var MaxBytesReader = stdlib.MaxBytesReader

// ServeFile replies to the request with the contents of the named file.
var ServeFile = stdlib.ServeFile
