package httptransport

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(body)
	r.bytes += n
	return n, err
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (r *statusRecorder) Push(target string, opts *http.PushOptions) error {
	pusher, ok := r.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(start)
		durationMS := duration.Milliseconds()
		if duration > 0 && durationMS == 0 {
			durationMS = 1
		}
		group := logGroupForPath(r.URL.Path)
		noise := logNoise(r.URL.Path, group)
		message := fmt.Sprintf("%s %s -> %d %dms", r.Method, r.URL.Path, status, durationMS)
		fields := []zap.Field{
			zap.String("source", "http"),
			zap.String("event", "http.request"),
			zap.String("group", group),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("query", r.URL.RawQuery),
			zap.String("status", strconv.Itoa(status)),
			zap.Int("bytes", recorder.bytes),
			zap.Int64("durationMs", durationMS),
			zap.Duration("duration", duration),
			zap.Bool("noise", noise),
			zap.String("requestId", middleware.GetReqID(r.Context())),
			zap.String("remoteAddr", r.RemoteAddr),
		}
		if status >= 500 {
			zap.L().Error(message, fields...)
			return
		}
		if status >= 400 || durationMS >= 1000 {
			zap.L().Warn(message, fields...)
			return
		}
		zap.L().Info(message, fields...)
	})
}

func recoverLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				zap.L().Error("http panic recovered",
					zap.String("source", "http"),
					zap.String("event", "http.panic"),
					zap.String("group", logGroupForPath(r.URL.Path)),
					zap.String("method", r.Method),
					zap.String("status", strconv.Itoa(http.StatusInternalServerError)),
					zap.Any("durationMs", nil),
					zap.Any("panic", recovered),
					zap.String("path", r.URL.Path),
					zap.String("requestId", middleware.GetReqID(r.Context())),
					zap.ByteString("stacktrace", debug.Stack()),
				)
				writeJSONAPIError(w, http.StatusInternalServerError, "internal-server-error", "Internal server error", "request failed", "")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func logGroupForPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return "frontend"
	}
	if strings.HasPrefix(path, "/assets/") || path == "/favicon.ico" {
		return "static"
	}
	if !strings.HasPrefix(path, "/api/") {
		return "frontend"
	}
	segment := strings.TrimPrefix(path, "/api/")
	if before, _, ok := strings.Cut(segment, "/"); ok {
		segment = before
	}
	switch segment {
	case "auth", "settings", "logs", "market", "meetings", "paper", "wake", "dashboard", "health":
		return segment
	case "message-subscriptions", "message-subscription-filters", "platform-adapters", "ingested-messages":
		return "messaging"
	case "ai":
		return "ai"
	case "research-teams":
		return "research"
	default:
		if segment == "" {
			return "unknown"
		}
		if _, err := strconv.Atoi(segment); err == nil {
			return "unknown"
		}
		return segment
	}
}

func logNoise(path string, group string) bool {
	if group == "static" || group == "frontend" {
		return true
	}
	return strings.HasPrefix(path, "/api/logs")
}
