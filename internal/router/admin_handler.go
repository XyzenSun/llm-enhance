package router

import (
	"encoding/json"
	"net/http"
	"runtime"

	"ai-api-stronger/internal/config"
	"ai-api-stronger/internal/privacy"
	"ai-api-stronger/internal/response"
)

// reloadRequest 是配置热重载管理接口的请求体。
type reloadRequest struct {
	// PathType 表示配置来源类型，支持 local 或 network。
	PathType string `json:"path_type"`
	// Path 表示本地文件路径或网络配置地址。
	Path string `json:"path"`
}

// serveAdmin 校验管理鉴权并分派健康检查或配置重载接口。
func (h *Handler) serveAdmin(w http.ResponseWriter, r *http.Request) {
	snapshot := h.snapshotOrError(w)
	if snapshot == nil {
		return
	}
	if r.Header.Get("X-Access-Key") != snapshot.Security.AccessKey {
		response.WriteError(w, response.CodeAuthFailed, "authentication failed")
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/health":
		h.health(w, snapshot)
	case r.Method == http.MethodPost && r.URL.Path == "/api/config/reload":
		h.reload(w, r)
	default:
		response.WriteError(w, response.CodeNotFound, "route not found")
	}
}

// health 返回当前快照和进程运行状态。
func (h *Handler) health(w http.ResponseWriter, snapshot *config.RuntimeSnapshot) {
	var memoryStats runtime.MemStats
	runtime.ReadMemStats(&memoryStats)
	healthData := map[string]any{
		"status":          "ok",
		"config_hash":     snapshot.ConfigHash,
		"channel_count":   len(snapshot.Channels),
		"goroutine_count": runtime.NumGoroutine(),
		"memory_alloc":    memoryStats.Alloc,
	}
	if h.Clients != nil {
		healthData["http_clients"] = h.Clients.Stats()
	}
	response.WriteSuccess(w, healthData)
}

// reload 校验并构建新快照，成功后原子替换当前配置。
func (h *Handler) reload(w http.ResponseWriter, r *http.Request) {
	var request reloadRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.PathType == "" || request.Path == "" {
		response.WriteError(w, response.CodeBadRequest, "path_type and path are required")
		return
	}

	snapshot, privacyProcessor, err := buildReloadSnapshot(request)
	if err != nil {
		response.WriteError(w, response.CodeConfigError, "config reload failed")
		return
	}

	// 只有在新配置校验通过且隐私处理器成功构建后，才替换当前快照。
	// 这样可以保证重载失败时继续沿用旧快照处理现有流量。
	if h.Clients != nil {
		h.Clients.MarkAllDraining()
	}
	h.Privacy = privacyProcessor
	h.Store.Store(snapshot)
	response.WriteSuccess(w, map[string]any{"config_hash": snapshot.ConfigHash, "channel_count": len(snapshot.Channels)})
}

// buildReloadSnapshot 从重载来源构建新快照和匹配的隐私处理器。
func buildReloadSnapshot(request reloadRequest) (*config.RuntimeSnapshot, privacy.Processor, error) {
	cfg, err := config.LoadReload(request.PathType, request.Path)
	if err != nil {
		return nil, nil, err
	}
	snapshot, err := config.BuildSnapshot(cfg)
	if err != nil {
		return nil, nil, err
	}
	var privacyProcessor privacy.Processor
	if snapshot.Privacy != nil {
		privacyProcessor, err = privacy.NewLibraryProcessor(snapshot.Privacy, snapshot.Runtime.DefaultTimeout)
		if err != nil {
			return nil, nil, err
		}
	}
	return snapshot, privacyProcessor, nil
}
