# Node → Go 迁移与验收

## 数据映射

| Node 版 | Go 版 |
|---|---|
| `.run/accounts.json` 的 `active` | `settings.active` |
| `manualUrl` | `settings.manualUrl` |
| `accounts[]` | `accounts` 表 |
| `password: dpapi:/aes:/plain:` | `password_enc` 原样保留 |
| `subKeys` | `sub_keys` 表 |
| `.run/cache/*.json` | `cache` 表 |
| `.run/config.json` | 保留，增加 `schemaVersion` |

首次启动自动迁移；旧文件重命名为 `.migrated-<timestamp>`，可回滚。

## 行为对齐清单

- [ ] 本机来源读取 `%APPDATA%\FastLink\FastLink\database.sqlite`
- [ ] 账号登录、Cookie、`subscribe_url`
- [ ] Scheme B 解密：raw / XOR / AES-GCM / obfuscated base64
- [ ] Clash 模板输出与 Node 版字节一致（用 golden fixture 对比）
- [ ] 400 自动重登
- [ ] 每来源独立 key；旧 `?key=面板口令` 跟随 active
- [ ] 来源 key 不能打开面板 / 不能代理 `b64`、`url`
- [ ] 缓存按来源分开；切换来源优先缓存
- [ ] 刷新失败保留旧快照
- [ ] DPAPI 中文密码往返
- [ ] 静态 UI：登录、订阅、账号、解密工具、高级设置

## 双跑方法

1. Node 版继续运行 8787；
2. Go 版用 `--root .run-go --port 8789 --no-open` 启动；
3. 对同一来源分别请求 `/sub`，比较：
   - HTTP 状态
   - 字节数 / SHA256
   - 节点数
   - `subscription-userinfo`
4. 通过后替换发布 exe。

## 回滚

保留旧 `fastlink.exe` 和 `.run/accounts.json` / `cache/*.json`；Go 迁移不会删除原文件，只重命名。
