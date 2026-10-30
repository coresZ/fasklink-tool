# FastLink-tool

FastLink 本机订阅发布面板的 Go 版实现。单文件、单进程、单端口，不依赖 Node / .NET / PowerShell。

## 开发（源码启动）

一键启动（推荐）：

```powershell
.\dev.cmd                 # Windows：双击或命令行，参数透传给 dev.ps1
```

```bash
./dev                     # Linux / macOS：等价于 ./scripts/dev.sh
```

底层脚本：

| 平台 | 脚本 | 说明 |
|---|---|---|
| Windows | `scripts/dev.ps1` | PowerShell 5.1+；`-Port` `-Root` `-NoOpen` `-Plain` |
| Linux / macOS | `scripts/dev.sh` | POSIX sh；`-p` `--root` `--no-open` `--plain` |

脚本会检查 Go 环境、创建状态目录、打印地址后以 `--dev` 运行源码。装有 `air` 且参数全为默认时自动走热重载，否则回落到 `go run`（`.air.toml` 固定了 8787 端口，参数被覆盖会静默失效，所以非默认参数一律不用 air）。

手动启动：

```powershell
go run ./cmd/fastlink-tool --dev --root .run-dev --port 8787 --no-open
```

- `--dev`：控制台日志、`web/` 从磁盘读取、独立单实例；
- 默认状态目录 `.run-dev`，不会影响生产 `.run`；
- 修改 Go 代码时重新 `go run` 即可；修改 `web/*` 刷新浏览器即可；
- 可选热重载：`go install github.com/air-verse/air@latest` 后脚本会自动启用，或直接运行 `air`。

## 构建

```powershell
.\scripts\build.ps1 -Version 1.1.0
```

产物：`dist/fastlink-tool.exe`（Windows x64，单文件，无控制台）。

## 局域网访问与防火墙

面板监听 `0.0.0.0` 时，Windows 防火墙在专用/公用配置文件下默认拦截入站连接。需要放行时（管理员 PowerShell）：

```powershell
.\scripts\firewall.ps1 -Status   # 只读检查，无需管理员
.\scripts\firewall.ps1 -Apply    # 创建入站规则（仅本地子网，绑定本程序）
.\scripts\firewall.ps1 -Remove   # 撤销
```

规则限定 `RemoteAddress=LocalSubnet` 且绑定 exe 路径，公网无法访问，端口也不会被其他程序蹭用。

设置页的「网络可达性」卡片会显示当前监听地址、局域网 IP、防火墙配置文件与规则状态。该自检**只读**，不会修改防火墙——静默改防火墙需要管理员清单，会让每次启动弹 UAC。

## 当前能力

- 本机 FastLink 客户端数据库读取；
- 多账号登录、Cookie / 授权头、订阅获取；
- Scheme B 解密 + Clash 模板；
- 本机 / 账号 / 手动来源切换；
- 每来源独立订阅 key，扫码与链接不串；key 保存后不会自动变化，可在账号表单或订阅页自定义；
- 刷新失败保留旧快照；
- DPAPI 密码保存（原生 Windows API，不启动 PowerShell）；
- SQLite 状态：账号、来源 key、缓存；
- 静态 SPA：订阅、账号、解密工具、高级设置。

## 目录

```text
cmd/fastlink-tool/       入口
internal/app/            启动、生命周期
internal/config/         配置
internal/httpapi/        API / 订阅端点 / 认证
internal/service/        来源、账号、刷新、缓存
internal/store/          SQLite
internal/secret/         DPAPI / AES
internal/subscription/   拉取、解密、模板
web/                     静态 SPA + QR
scripts/                 dev / build
dev, dev.cmd             一键开发启动入口
```

## 数据

```text
<root>/.run/config.json   端口、口令、域名
<root>/.run/fastlink.db   账号、sub_keys、cache、settings
<root>/.run/.key          DPAPI 不可用时的 AES key
<root>/.run/server.out.log
```

首次启动会从旧版 `.run/accounts.json` 和 `.run/cache/*.json` 自动迁移。

## 文档

- [架构](docs/ARCHITECTURE.md)
- [Node → Go 迁移](docs/MIGRATION.md)
