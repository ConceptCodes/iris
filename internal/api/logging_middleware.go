package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/trace"
)

// responseWriterWrapper wraps http.ResponseWriter to capture the status code.
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

// WriteHeader captures the status code and calls the underlying WriteHeader.
func (w *responseWriterWrapper) WriteHeader(code int) {
	if !w.written {
		w.statusCode = code
		w.written = true
		w.ResponseWriter.WriteHeader(code)
	}
}

// Write captures the first write to set status code if not already set.
func (w *responseWriterWrapper) Write(b []byte) (int, error) {
	if !w.written {
		w.statusCode = http.StatusOK
		w.written = true
	}
	return w.ResponseWriter.Write(b)
}

// StructuredLogger returns a middleware that logs HTTP requests with structured fields.
// It extracts request ID from chi's middleware.RequestID and trace ID from OpenTelemetry.
// Logs are output at INFO level for successful requests, WARN for 4xx, ERROR for 5xx.
func StructuredLogger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			wrapped := &responseWriterWrapper{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			requestID := r.Context().Value(middleware.RequestIDKey)
			if requestID == nil {
				requestID = ""
			}

			span := trace.SpanFromContext(r.Context())
			traceID := ""
			if span.SpanContext().HasTraceID() {
				traceID = span.SpanContext().TraceID().String()
			}

			remoteIP := r.RemoteAddr
			if ip := r.Header.Get("X-Real-IP"); ip != "" {
				remoteIP = ip
			}

			userAgent := r.Header.Get("User-Agent")

			next.ServeHTTP(wrapped, r)

			latency := time.Since(start)

			level := slog.LevelInfo
			msg := "request"
			if wrapped.statusCode >= 500 {
				level = slog.LevelError
				msg = "request_error"
			} else if wrapped.statusCode >= 400 {
				level = slog.LevelWarn
				msg = "request_client_error"
			}

			slog.Log(r.Context(), level, msg,
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.statusCode,
				"latency_ms", latency.Milliseconds(),
				"request_id", requestID,
				"trace_id", traceID,
				"remote_ip", remoteIP,
				"user_agent", userAgent,
			)
		})
	}
}
