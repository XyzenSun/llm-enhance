package app

import (
	"fmt"
	"net/http"

	"ai-api-stronger/internal/config"
	"ai-api-stronger/internal/logging"
	"ai-api-stronger/internal/privacy"
	"ai-api-stronger/internal/router"
	"ai-api-stronger/internal/upstream"
)

// App 持有服务运行所需的快照、客户端池、隐私处理器、日志和 HTTP Server。
type App struct {
	// Snapshot 是当前不可变运行快照的原子存储。
	Snapshot *config.SnapshotStore
	// Clients 是按上游行为复用的 HTTP 客户端池。
	Clients *upstream.Manager
	// Privacy 是启用隐私保护时调用的真实隐私处理器。
	Privacy privacy.Processor
	// Logger 是服务级结构化日志输出器。
	Logger logging.Logger
	// Server 是对外提供代理和管理接口的 HTTP 服务。
	Server *http.Server
}

// New 从配置文件构建应用依赖和 HTTP 服务。
func New(configPath string) (*App, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return nil, err
	}
	snapshot, err := config.BuildSnapshot(cfg)
	if err != nil {
		return nil, err
	}
	store := config.NewSnapshotStore(snapshot)
	clients := upstream.NewManager()
	var privacyProcessor privacy.Processor
	if snapshot.Privacy != nil {
		privacyProcessor, err = privacy.NewLibraryProcessor(snapshot.Privacy, snapshot.Runtime.DefaultTimeout)
		if err != nil {
			return nil, err
		}
	}
	logger := logging.NewNoop()
	if cfg.Log.Enable {
		logger = logging.NewConsole(cfg.Log.Level)
	}
	h := router.New(store, clients, privacyProcessor, configPath)
	srv := &http.Server{Addr: fmt.Sprintf("%s:%d", cfg.Server.ListenAddress, cfg.Server.ListenPort), Handler: h, ReadTimeout: cfg.Server.ReadTimeout, WriteTimeout: cfg.Server.WriteTimeout, IdleTimeout: cfg.Server.IdleTimeout}
	return &App{Snapshot: store, Clients: clients, Privacy: privacyProcessor, Logger: logger, Server: srv}, nil
}
