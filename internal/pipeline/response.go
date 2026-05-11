package pipeline

import (
	"io"
	"net/http"
	"time"

	"ai-api-stronger/internal/planner"
)

func WriteUpstreamResponse(w http.ResponseWriter, upstream *http.Response, headerPlan planner.HeaderPlan, statusMap map[int]int) error {
	for k, vals := range upstream.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	ApplyHeaderPlanWithOriginAt(w.Header(), upstream.Header, headerPlan, "", time.Now())
	status := upstream.StatusCode
	if mapped, ok := statusMap[status]; ok {
		status = mapped
	}
	w.WriteHeader(status)
	_, err := io.Copy(w, upstream.Body)
	return err
}
