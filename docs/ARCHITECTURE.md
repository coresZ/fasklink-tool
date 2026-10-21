# FastLink-tool 架构

## 目标

一个 Windows 单文件程序 `fastlink-tool.exe`：单进程、单端口、无 Node、无 .NET 运行时、无 PowerShell。

```text
cmd/fastlink-tool
  └─ internal/app
       ├─ internal/httpapi      HTTP API + /sub + 静态 SPA
       ├─ internal/service      来源解析 / 账号 / 刷新 / 缓存
       ├─ internal/subscription 拉取 / Scheme B 解密 / Clash 模板
       ├─ internal/store        SQLite 账号 / sub_keys / cache
       ├─ internal/secret       DPAPI / AES
       └─ web/                   embed 静态 UI
```

## 启动模式

| 模式 | 命令 | 状态目录 | 资源 |
|---|---|---|---|
| 开发 | `go run ./cmd/fastlink-tool --dev --root .run-dev --port 8788 --no-open` | `.run-dev` | `web/` 磁盘 |
| 发布 | `fastlink-tool.exe` | exe 目录 `.run` | `web/` embed |

开发模式输出控制台日志；发布模式写 `.run/server.out.log`，错误用 MessageBox。

## 数据

```text
<root>/.run/config.json
<root>/.run/fastlink.db
<root>/.run/.key
<root>/.run/server.out.log
```

`config.json`：

```json
{
  "schemaVersion": 2,
  "host": "0.0.0.0",
  "port": 8787,
  "accessKey": "fastlink",
  "domains": ["192.168.31.227"]
}
```

`fastlink.db` 表：

- `accounts`：账号、`password_enc`、`subscription_url`、last_login/last_fetch、user_info、error
- `sub_keys`：来源 → 24 位随机 key
- `cache`：来源 → 上次 YAML / userInfo / fetchedAt
- `settings`：`active`、`manualUrl`
- `schema_migrations`

首次启动从旧 Node 版 `.run/accounts.json` 和 `.run/cache/*.json` 迁移，原文件重命名为 `.migrated-<timestamp>`。

## 订阅流程

```text
active source
  ├─ local   -> %APPDATA%\FastLink\FastLink\shared_preferences.json + database.sqlite
  ├─ account -> accounts.subscription_url
  │            没有 URL -> LoginAccount
  └─ manual  -> settings.manualUrl

Resolve -> FetchSubscription (UA, 4MB, HTTPS/SSRF guard)
        -> DecryptProfile (raw / XOR / AES-GCM / obfuscated base64)
        -> ApplyTemplate (proxies block -> template.yaml)
        -> cache + probe
```

账号登录：

```text
POST /api/v1/passport/auth/login  {email,password} -> auth_data
GET  /api/v1/user/info            Authorization
GET  /api/v1/user/getSubscribe    -> subscribe_url
GET  subscribe_url                UA + same-origin Authorization / cookies
```

`400/401/403/404/410` 或 `invalid token` 会重登一次。

## API

- `/api/v1/*`：面板 API，Cookie/口令
- `/sub`、`/clash`、`/config.yaml`：Clash 订阅，支持面板口令或来源 key
- `/health`：探活

来源 key 只能拉自己的来源，不能打开面板，不能带 `b64`/`url` 代理任意地址。

## 构建

```powershell
.\scripts\build.ps1 -Version 1.0.0
```

输出 `dist/fastlink-tool.exe`（`-H=windowsgui`，无控制台）。
