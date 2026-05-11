package pipeline

import (
	"net/http"
	"strings"
	"time"

	"ai-api-stronger/internal/planner"
)

var hopByHopHeaders = map[string]struct{}{
	"connection": {}, "keep-alive": {}, "proxy-authenticate": {}, "proxy-authorization": {}, "te": {}, "trailer": {}, "transfer-encoding": {}, "upgrade": {},
}

func ApplyRequestHeaders(src http.Header, plan planner.HeaderPlan, requestID string) http.Header {
	out := make(http.Header, len(src))
	for k, vals := range src {
		if _, skip := hopByHopHeaders[strings.ToLower(k)]; skip {
			continue
		}
		out[k] = append([]string(nil), vals...)
	}
	ApplyHeaderPlanWithOriginAt(out, src, plan, requestID, time.Now())
	return out
}

func ApplyHeaderPlan(h http.Header, plan planner.HeaderPlan, requestID string) {
	ApplyHeaderPlanAt(h, plan, requestID, time.Now())
}

func ApplyHeaderPlanAt(h http.Header, plan planner.HeaderPlan, requestID string, now time.Time) {
	ApplyHeaderPlanWithOriginAt(h, h, plan, requestID, now)
}

func ApplyHeaderPlanWithOriginAt(h http.Header, origin http.Header, plan planner.HeaderPlan, requestID string, now time.Time) {
	originalValues := make(map[string]string, len(plan.Set))
	for key := range plan.Set {
		originalValues[key] = origin.Get(key)
	}
	for _, key := range plan.Delete {
		h.Del(key)
	}
	for key, value := range plan.Set {
		value = strings.ReplaceAll(value, "${uuid}", requestID)
		h.Set(key, interpolateRuntimeVariables(value, originalValues[key], now))
	}
}
