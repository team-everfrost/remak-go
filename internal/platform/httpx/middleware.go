package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const (
	requestIDKey contextKey = "request_id"
	actorKey     contextKey = "actor"
)

type Actor struct {
	AccountID uuid.UUID
	PublicID  uuid.UUID
	Role      string
}

type TokenVerifier interface {
	VerifyAccessToken(token string) (Actor, error)
}

type AccountActiveFunc func(context.Context, uuid.UUID) (bool, error)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if !validRequestID(requestID) {
			requestID = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validRequestID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, current := range value {
		if (current >= 'a' && current <= 'z') || (current >= 'A' && current <= 'Z') ||
			(current >= '0' && current <= '9') || strings.ContainsRune("-_.:/", current) {
			continue
		}
		return false
	}
	return true
}

func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error("http panic", "request_id", RequestIDFromContext(r.Context()), "panic", recovered, "stack", string(debug.Stack()))
					WriteError(w, Internal(nil))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(recorder, r)
			logger.Info("http request",
				"request_id", RequestIDFromContext(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", recorder.status,
				"bytes", recorder.bytes,
				"duration_ms", time.Since(started).Milliseconds(),
			)
		})
	}
}

type responseRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(body []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	written, err := r.ResponseWriter.Write(body)
	r.bytes += written
	return written, err
}

func (r *responseRecorder) Flush() {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *responseRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func Authenticate(verifier TokenVerifier, accountActive AccountActiveFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				WriteError(w, Unauthorized("missing_access_token", "로그인이 필요합니다"))
				return
			}
			actor, err := verifier.VerifyAccessToken(strings.TrimPrefix(header, "Bearer "))
			if err != nil {
				WriteError(w, Unauthorized("invalid_access_token", "인증 정보가 올바르지 않습니다"))
				return
			}
			if accountActive != nil {
				active, activeErr := accountActive(r.Context(), actor.AccountID)
				if activeErr != nil {
					WriteError(w, Internal(activeErr))
					return
				}
				if !active {
					WriteError(w, Unauthorized("invalid_access_token", "인증 정보가 올바르지 않습니다"))
					return
				}
			}
			ctx := context.WithValue(r.Context(), actorKey, actor)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ActorFromContext(ctx context.Context) Actor {
	actor, _ := ctx.Value(actorKey).(Actor)
	return actor
}

func RequestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey).(string)
	return requestID
}
