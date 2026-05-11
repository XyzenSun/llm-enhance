package pipeline

import (
	"net/http"
	"strings"
	"time"

	"ai-api-stronger/internal/planner"
)

type fakeStreamResponseWriter struct {
	inner     http.ResponseWriter
	statusMap map[int]int
	wrote     bool
}

func newFakeStreamResponseWriter(inner http.ResponseWriter, upstreamHeader http.Header, headerPlan planner.HeaderPlan, statusMap map[int]int, requestID string) http.ResponseWriter {
	for k, vals := range upstreamHeader {
		if skipFakeStreamUpstreamHeader(k) {
			continue
		}
		for _, v := range vals {
			inner.Header().Add(k, v)
		}
	}
	ApplyHeaderPlanWithOriginAt(inner.Header(), upstreamHeader, headerPlan, requestID, time.Now())
	return &fakeStreamResponseWriter{inner: inner, statusMap: statusMap}
}

func skipFakeStreamUpstreamHeader(key string) bool {
	lower := strings.ToLower(key)
	if _, hopByHop := hopByHopHeaders[lower]; hopByHop {
		return true
	}
	switch lower {
	case "content-length", "content-encoding":
		return true
	}
	return false
}

func (w *fakeStreamResponseWriter) Header() http.Header {
	return w.inner.Header()
}

func (w *fakeStreamResponseWriter) WriteHeader(statusCode int) {
	if w.wrote {
		return
	}
	if mapped, ok := w.statusMap[statusCode]; ok {
		statusCode = mapped
	}
	w.wrote = true
	w.inner.WriteHeader(statusCode)
}

func (w *fakeStreamResponseWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.inner.Write(p)
}

func (w *fakeStreamResponseWriter) Flush() {
	if f, ok := w.inner.(http.Flusher); ok {
		f.Flush()
	}
}
