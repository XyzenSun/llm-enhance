package app

import "context"

// Run 启动 HTTP 服务并记录启动事件。
func (a *App) Run() error {
	a.Logger.Info("server_started", nil)
	return a.Server.ListenAndServe()
}

// Shutdown 优雅关闭 HTTP 服务并刷新日志。
func (a *App) Shutdown(ctx context.Context) error {
	defer a.Logger.Close()
	return a.Server.Shutdown(ctx)
}
