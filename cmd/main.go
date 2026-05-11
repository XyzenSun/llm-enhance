package main

import (
	"flag"
	"log"
	"net/http"

	"ai-api-stronger/internal/app"
	"ai-api-stronger/internal/config"
)

// main 加载本地环境与配置，并启动 AI API 代理服务。
func main() {
	configPath := flag.String("config", "config.yaml", "path to config.yaml")
	envPath := flag.String("env", ".env.local", "optional local env file for config variable expansion")
	flag.Parse()
	if *envPath != "" {
		if err := config.LoadEnvFile(*envPath); err != nil {
			log.Fatal(err)
		}
	}
	a, err := app.New(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := a.Run(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
