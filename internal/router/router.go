package router

import (
	"net/http"
	"strings"

	"ai-api-stronger/internal/config"
	"ai-api-stronger/internal/pipeline"
	"ai-api-stronger/internal/privacy"
	"ai-api-stronger/internal/response"
	"ai-api-stronger/internal/upstream"
)

// Handler 负责区分管理接口和代理接口，并串联运行时依赖。
type Handler struct {
	// Store 提供请求开始时读取的不可变配置快照。
	Store *config.SnapshotStore
	// Clients 是发起上游请求使用的受管客户端池。
	Clients *upstream.Manager
	// Privacy 是当前快照对应的隐私保护处理器。
	Privacy privacy.Processor
	// ConfigPath 是管理接口默认重载的配置文件路径。
	ConfigPath string
}

// New 创建包含管理与代理路由的 HTTP handler。
func New(store *config.SnapshotStore, clients *upstream.Manager, privacyProcessor privacy.Processor, configPath string) http.Handler {
	return &Handler{Store: store, Clients: clients, Privacy: privacyProcessor, ConfigPath: configPath}
}

// ServeHTTP 根据路径前缀分派管理接口或代理接口。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.serveAdmin(w, r)
		return
	}
	if r.URL.Path == "/api" {
		response.WriteError(w, response.CodeNotFound, "route not found")
		return
	}
	h.serveProxy(w, r)
}

// snapshotOrError 读取当前快照，缺失时写入统一错误响应。
func (h *Handler) snapshotOrError(w http.ResponseWriter) *config.RuntimeSnapshot {
	s := h.Store.Load()
	if s == nil {
		response.WriteError(w, response.CodeInternalError, "runtime snapshot is not ready")
		return nil
	}
	return s
}

// pipeline 用当前 handler 依赖构造一次请求执行管线。
func (h *Handler) pipeline() pipeline.Pipeline {
	return pipeline.Pipeline{Clients: h.Clients, Privacy: h.Privacy}
}
