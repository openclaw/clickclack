package httpapi

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// AccessLogMode selects how much of the per-request access log the server
// writes. The zero value logs every request, which is what operators got
// before the mode existed.
type AccessLogMode string

const (
	AccessLogAll    AccessLogMode = "all"
	AccessLogErrors AccessLogMode = "errors"
	AccessLogOff    AccessLogMode = "off"
)

type pathOnlyLogFormatter struct {
	Logger middleware.LoggerInterface
	Mode   AccessLogMode
}

func (f *pathOnlyLogFormatter) NewLogEntry(r *http.Request) middleware.LogEntry {
	mode := f.mode()
	if mode == AccessLogOff {
		return &pathOnlyLogEntry{logger: f.logger(), request: r, mode: mode}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	prefix := fmt.Sprintf("method=%q scheme=%q host=%q proto=%q remote=%q correlation_id=%q ", r.Method, scheme, r.Host, r.Proto, r.RemoteAddr, correlationIDFromContext(r.Context()))
	return &pathOnlyLogEntry{logger: f.logger(), prefix: prefix, request: r, mode: mode}
}

func (f *pathOnlyLogFormatter) logger() middleware.LoggerInterface {
	if f.Logger != nil {
		return f.Logger
	}
	return log.Default()
}

func (f *pathOnlyLogFormatter) mode() AccessLogMode {
	if f.Mode == "" {
		return AccessLogAll
	}
	return f.Mode
}

type pathOnlyLogEntry struct {
	logger  middleware.LoggerInterface
	prefix  string
	request *http.Request
	mode    AccessLogMode
}

func (e *pathOnlyLogEntry) Write(status, bytes int, _ http.Header, elapsed time.Duration, _ any) {
	switch e.mode {
	case AccessLogOff:
		return
	case AccessLogErrors:
		if status < http.StatusBadRequest {
			return
		}
	}
	route := chi.RouteContext(e.request.Context()).RoutePattern()
	if route == "" {
		route = "unmatched"
	}
	e.logger.Print(fmt.Sprintf("%sroute=%q status=%03d bytes=%d elapsed=%s", e.prefix, route, status, bytes, elapsed))
}

func (e *pathOnlyLogEntry) Panic(v any, _ []byte) {
	middleware.PrintPrettyStack(v)
}
