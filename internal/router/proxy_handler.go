package router

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"ai-api-stronger/internal/planner"
	"ai-api-stronger/internal/response"
)

func ParseProxyPath(path string) (planner.RouteInfo, bool) {
	pathParts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(pathParts) < 4 || pathParts[1] != "proxy" {
		return planner.RouteInfo{}, false
	}
	return planner.RouteInfo{AccessKey: pathParts[0], Channel: pathParts[2], TailPath: strings.Join(pathParts[3:], "/")}, true
}

func (h *Handler) serveProxy(w http.ResponseWriter, r *http.Request) {
	snapshot := h.snapshotOrError(w)
	if snapshot == nil {
		return
	}

	route, ok := ParseProxyPath(r.URL.Path)
	if !ok {
		response.WriteError(w, response.CodeNotFound, "route not found")
		return
	}
	route.RawQuery = r.URL.RawQuery
	if route.AccessKey != snapshot.Security.AccessKey {
		response.WriteError(w, response.CodeAuthFailed, "authentication failed")
		return
	}

	channel := snapshot.Channels[route.Channel]
	if channel == nil {
		response.WriteError(w, response.CodeChannelNotFound, "channel does not exist")
		return
	}
	// Disabled channels remain in the snapshot so requests can distinguish
	// "known but disabled" from "not found" as required by the public error map.
	if !channel.Enabled {
		response.WriteError(w, response.CodeChannelDisabled, "channel is disabled")
		return
	}

	requestBody, err := readRequestBody(r)
	if err != nil {
		response.WriteError(w, response.CodeBadRequest, "failed to read request body")
		return
	}
	executionPlan, err := planner.BuildExecutionPlan(snapshot, route, r, requestBody)
	if err != nil {
		mapPlannerError(w, err)
		return
	}
	h.pipeline().Execute(w, r, executionPlan, requestBody)
}

func readRequestBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func mapPlannerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, planner.ErrChannelNotFound):
		response.WriteError(w, response.CodeChannelNotFound, "channel does not exist")
	case errors.Is(err, planner.ErrChannelDisabled):
		response.WriteError(w, response.CodeChannelDisabled, "channel is disabled")
	case errors.Is(err, planner.ErrInvalidRequestBody):
		response.WriteError(w, response.CodeBodyProcessingFailed, "invalid request body")
	default:
		response.WriteError(w, response.CodeRuleProcessingFailed, "request planning failed")
	}
}
