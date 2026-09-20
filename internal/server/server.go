package server

import (
	"net/http"
	"time"

	messagesapp "github.com/d-kuro/kirocc/internal/app/messages"
	"github.com/d-kuro/kirocc/internal/config"
	"github.com/d-kuro/kirocc/internal/kiroclient"
	"github.com/d-kuro/kirocc/internal/tracing"
)

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithOTel enables OpenTelemetry tracing middleware.
func WithOTel(bodyLimit int) ServerOption {
	return func(s *Server) {
		s.otel = true
		s.otelBodyLimit = bodyLimit
	}
}

// WithCapture enables upstream capture logging in the messages service.
func WithCapture(enabled bool) ServerOption {
	return func(s *Server) { s.captureEnabled = enabled }
}

// WithKeepAliveInterval sets the idle interval for streaming SSE comments.
// A zero duration disables keep-alive comments.
func WithKeepAliveInterval(interval time.Duration) ServerOption {
	return func(s *Server) { s.keepAliveInterval = interval }
}

// WithMaxRequestBody caps the client request body in bytes. Zero disables the cap.
func WithMaxRequestBody(limit int64) ServerOption {
	return func(s *Server) { s.maxRequestBody = limit }
}

// Server is the HTTP server for the kirocc proxy.
type Server struct {
	apiKey            string
	otel              bool
	otelBodyLimit     int
	captureEnabled    bool
	keepAliveInterval time.Duration
	maxRequestBody    int64
	mux               *http.ServeMux
	messages          *messagesapp.Service
}

// New creates a new Server.
func New(authMgr messagesapp.TokenGetter, apiKey string, client kiroclient.Client, opts ...ServerOption) *Server {
	s := &Server{
		apiKey:         apiKey,
		mux:            http.NewServeMux(),
		maxRequestBody: config.DefaultMaxRequestBody,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.messages = messagesapp.New(authMgr, client,
		messagesapp.WithCapture(s.captureEnabled),
		messagesapp.WithKeepAliveInterval(s.keepAliveInterval),
		messagesapp.WithMaxRequestBody(s.maxRequestBody),
	)
	s.registerRoutes()
	return s
}

// Handler returns the http.Handler for the server.
func (s *Server) Handler() http.Handler {
	h := traceMiddleware(corsMiddleware(s.authMiddleware(s.mux)))
	if s.otel {
		h = tracing.Middleware(h, s.otelBodyLimit)
	}
	return h
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /v1/models", s.handleModels)
	s.mux.HandleFunc("POST /v1/messages/count_tokens", s.messages.HandleCountTokens)
	s.mux.HandleFunc("POST /v1/messages", s.messages.HandleMessages)
	// Method fallbacks: without these, a wrong method on a known path would
	// fall into the "/" catch-all as 404 instead of the correct 405.
	s.mux.HandleFunc("/health", methodNotAllowed("GET"))
	s.mux.HandleFunc("/v1/models", methodNotAllowed("GET"))
	s.mux.HandleFunc("/v1/messages/count_tokens", methodNotAllowed("POST"))
	s.mux.HandleFunc("/v1/messages", methodNotAllowed("POST"))
	// Catch-all AFTER specific routes: JSON 404 for anything Claude Code
	// probes behind a gateway (flags/org endpoints). Pattern "/" only wins
	// when nothing more specific matches.
	s.mux.HandleFunc("/", s.handleNotFound)
}
