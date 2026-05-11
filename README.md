# AI API Stronger

AI API Stronger 是一个 Go 语言实现的 AI API 增强代理服务，位于客户端与上游 LLM 服务（OpenAI、Anthropic、Gemini 等）之间，提供请求路由、策略变换、隐私保护、流式响应处理等能力。

## 功能特性

- **多渠道路由** — 按 URL 路径将请求分派到不同上游渠道，支持 OpenAI / Anthropic / Gemini 格式
- **策略变换** — 对请求头、请求体、响应头、响应状态码进行可控变换（model_rewrite、system_prompt 注入、max_tokens/temperature 覆盖、自定义字段注入）
- **隐私保护** — 可选启用 LLM 驱动的隐私脱敏处理器，在请求到达上游前自动遮蔽敏感信息
- **非流式转流式** — 将上游非流式响应转换为 SSE 流式响应返回给客户端
- **配置热重载** — 通过管理 API 无需重启即可切换运行配置，失败时自动回退旧配置
- **健康检查** — 返回运行状态、配置哈希、HTTP 客户端池统计等信息
- **出站代理** — 支持 HTTP / SOCKS5 代理转发上游请求
- **运行时插值** — 配置中的 `${uuid}` 等变量在请求时动态展开
- **统一错误响应** — 所有错误以一致的 JSON 结构返回，附带数字错误码

## 快速开始

### 前置要求

- Go 1.23 或更高版本

### 安装依赖

```bash
cd gocode
go mod download
```

### 配置

复制示例配置并修改：

```bash
cp config.example.yaml config.yaml
```

编辑 `config.yaml`，填入你的上游 API 地址和密钥。所有敏感值支持 `${ENV_VAR}` 环境变量展开，也可使用 `.env.local` 文件：

```bash
# .env.local
REAL_UPSTREAM_API_KEY=sk-your-real-key
REAL_UPSTREAM_BASE_URL=https://api.openai.com
REAL_UPSTREAM_MODEL=gpt-4o-mini
```

### 启动

```bash
go run ./cmd/ai-api-stronger -config ./config.yaml -env ./.env.local
```

或使用辅助脚本：

```bash
./scripts/start-configtest.sh
```

### 请求示例

代理请求路径格式：`/{access_key}/proxy/{channel}/{upstream_path}`

```bash
curl -X POST http://127.0.0.1:28080/your-access-key/proxy/openai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Hello"}]}'
```

## 管理接口

所有管理接口需要 `X-Access-Key` 头鉴权。

### 健康检查

```bash
curl -H "X-Access-Key: your-access-key" http://127.0.0.1:28080/api/health
```

### 配置热重载

```bash
curl -X POST -H "X-Access-Key: your-access-key" \
  -H "Content-Type: application/json" \
  -d '{"path_type":"local","path":"./config.yaml"}' \
  http://127.0.0.1:28080/api/config/reload
```

## 项目结构

```
gocode/
  cmd/ai-api-stronger/     程序入口
  internal/
    app/                   应用装配（config → snapshot → server）
    config/                配置加载、校验、不可变运行快照
    router/                HTTP 路由（管理 + 代理）
    pipeline/              请求执行管线（变换、插值、响应）
    planner/               执行计划构建（模型提取、规则匹配）
    upstream/              HTTP 客户端池、出站代理
    privacy/               隐私脱敏处理器
    streaming/             流式检测、非流式转流式、假流式
    logging/               结构化日志 + 文件轮转
    response/              统一错误码 + JSON 响应
  config.example.yaml      配置示例
  go.mod / go.sum          模块依赖
docs/                       设计文档
```

## 配置说明

完整配置示例见 `gocode/config.example.yaml`，设计文档见 `docs/` 目录。

核心配置段落：

| 段落 | 说明 |
|------|------|
| `server` | 监听地址、端口、超时 |
| `security` | access_key 鉴权密钥 |
| `runtime` | 默认超时 |
| `log` | 日志开关、级别、保留天数 |
| `llm_private_protect_config` | 隐私处理器所需的可信 LLM 配置 |
| `channels` | 上游渠道定义，包含 upstream、channel_policy、model_rules |

### 渠道策略

每个渠道的 `channel_policy` 控制请求/响应变换：

- `request_headers.set/delete` — 设置或删除请求头
- `request_body.model_rewrite` — 模型名别名映射
- `request_body.system_prompt` — 注入系统提示词
- `request_body.max_tokens / temperature` — 覆盖参数
- `request_body.custom` — JSON 任意字段注入
- `response_headers.set/delete` — 设置或删除响应头
- `response_status` — 上游状态码映射（如 `429: 400`）
- `streaming_setting` — 非流式转流式配置

### 模型规则

`model_rules` 在客户端请求特定模型时，叠加额外的策略变换，覆盖渠道默认策略。

## 运行测试

```bash
cd gocode
go test ./internal/...
```

## 许可证

MIT License，详见 [LICENSE](LICENSE)。