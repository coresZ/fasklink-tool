# MODIFICATIONS

> 单文件制：全仓库只保留这一份，按目录分节记录。记录格式为「功能 → 修改内容 → 涉及文件与位置」。

## 2026-10-21

### web/ — 文案统一与动态区域防抖

**功能**：订阅面板文案与轮询渲染稳定性。

**修改内容**：

- 文案统一：流量摘要改为总量口径（`总量 100 GB · 已用 66.84 GB`）、空态统一为「暂无 X」句式、按钮动词补全（打开链接 / 复制链接 / 复制节点 / 使用链接）、状态词统一为「拉取中 / 待拉取 / 正常 / 拉取异常」、操作提示统一为「X 成功」。
- `#traffic` 骨架只构建一次，之后原地更新文本节点与 `.bar i` 宽度（`transition: width .2s`），不再整块 `innerHTML` 重写。
- `#sources` / `#addrChips` / `#nodes` 增加内容签名比对（`renderInto`），未变化不触碰 DOM；重建时保存恢复 `scrollTop`。
- `#qr` 按 URL 缓存，URL 未变不重新生成 SVG（原先每 5 秒换新节点导致闪动）。
- `#manualRow` 收起时折叠到 0 高并带 180ms 过渡，既不弹跳也不留空白带。
- 尺寸锁定：`#stateText` 140px、`#nodeCount` 3.6em、`.pill` 2.2em、`#sourceMeta`/`.chips` 100% 宽、`.srcwrap{flex:1;min-width:0}`，动态文本一律 `tabular-nums` + 固定行高 + 省略号 + `title` 全文。
- 空文本占位：`.hint:empty` / `#sourceMeta:empty` 用 `visibility:hidden` 保留行高。
- 内联 SVG favicon，消除每次加载的 `/favicon.ico` 404。

**涉及文件**：`web/index.html`（顶栏流量骨架、各页文案、`.srcwrap`、favicon）、`web/style.css`（尺寸锁定、`.gone`、`.srcwrap`、`.gone` 过渡、`.traffic-summary` 等）、`web/app.js`（`renderTraffic`/`renderInto`/`renderAddresses`/`renderSources`/`renderNodes`/`setText`）。

**验证基线**（Chrome 1280×900，真实登录态，74 节点）：

```text
最长/最短文案切换后 30 个元素 x/y/w/h 位移      -> 0
连续 14 次采样（约 7s，跨 2 个轮询周期）         -> 无漂移
节点列表 scrollTop=260 跨轮询                    -> 保持 260
节点 DOM 是否重建                                -> firstChildIsSame: true
二维码 SVG 跨 6s                                 -> sameNode: true
账号/解密/高级设置三页超长错误提示               -> 卡片高度 ±0
桌面/平板/手机横向溢出                           -> 无
JS 运行时错误 / 失败请求                          -> 无
```

### web/ — 滚动条只美化 thumb

**功能**：滚动条样式符合「轨道不得有背景色」规范。

**修改内容**：

- 根因是 `index.html` 的 `<meta name="color-scheme" content="dark">` 会启用原生深色滚动条，且项目此前未写 `track` 透明，因此轨道出现灰底。
- 新增 `--scroll-thumb` / `--scroll-thumb-hover` 语义变量。
- 用 `@supports not selector(::-webkit-scrollbar)` 给 Firefox 写 `scrollbar-color: <thumb> transparent`、`scrollbar-width: thin`；用 `@supports selector(::-webkit-scrollbar)` 给 WebKit 写 `::-webkit-scrollbar-track{background:transparent}`、`thumb` 用 `border:2px solid transparent` + `background-clip:content-box` 做留边。
- 两套语法必须隔离：Chromium 一旦读到 `scrollbar-width` 就切换到原生滚动条并绘制上下箭头按钮，`::-webkit-scrollbar-button{display:none}` 压不住。
- `scrollbar-gutter:stable` 仅用于锁定版面宽度、避免出现滚动条时内容横移，不承担美化职责。
- 触屏设备按规范例外隐藏滚动条：`@media (hover:none),(pointer:coarse)` 下 `scrollbar-width:none` + `::-webkit-scrollbar{display:none}`。

**涉及文件**：`web/style.css`（1–27 行）、`web/index.html`（`<meta name="color-scheme">`，未改动，仅作为根因记录）。

**验证基线**（`headless:false` 才会用占位式经典滚动条，headless 是 overlay 滚动条测不出轨道）：

```text
滚动条区域像素统计（节点列表右侧 14px × 330px）
  容器底色 rgb(17,26,46)  占 63.7%
  thumb    rgb(47,64,102) 占 13.2%
  纯灰（原生轨道）          0 种
桌面 reserved gutter 10px，触屏 nodesSB=0（滚动条已隐藏），无上下箭头按钮
```

### scripts/ · 根目录 — 一键开发启动（Windows / Linux / macOS）

**功能**：三平台一键 `dev`，源码启动。

**修改内容**：

- 新增 `scripts/dev.sh`（POSIX sh）：解析 `-p/--port`、`--root`、`--no-open`、`--plain`、`-h`，支持 `PORT`/`STATE` 环境变量；校验 Go 环境；创建状态目录；打印地址/状态/Go 版本/模式；仅当参数全为默认且存在 `air` 时走热重载，否则 `go run`。
- 重写 `scripts/dev.ps1`：同参数集（`-Port` `-Root` `-NoOpen` `-Plain` `-Help`），同样只在全默认参数时启用 air；文件以 UTF-8 BOM 保存（PowerShell 5.1 读无 BOM 中文会乱码）。
- 新增根目录入口 `dev`（sh 薄封装，转发到 `scripts/dev.sh`）与 `dev.cmd`（Windows 双击入口，以 `-ExecutionPolicy Bypass` 调 `dev.ps1`，失败时 `pause`）。`dev.cmd` 内容保持纯 ASCII + CRLF：cmd 的 GBK 代码页会把 UTF-8 中文注释截断成命令。
- 两个脚本都在运行前 `cd` 到仓库根，支持从任意工作目录、含空格路径、符号链接调用。
- 非默认参数一律不用 air：`.air.toml` 写死了 8788 端口与 `--no-open`，用 air 会静默忽略用户传入的端口/状态目录。

**涉及文件**：`scripts/dev.sh`（新增）、`scripts/dev.ps1`（重写）、`dev`（新增，100755）、`dev.cmd`（新增）、`README.md`（开发章节与目录树）。

**验证基线**：

```text
dev.sh 参数转发（用假 go shim 截获 argv）
  默认                     -> run ./cmd/fastlink-tool --dev --root .run-dev --port 8788
  --no-open                -> ... --port 8788 --no-open
  -p 9001 --root .run-x    -> ... --root .run-x --port 9001 --no-open
  PORT=9002 STATE=.run-env -> ... --root .run-env --port 9002
  --plain                  -> 与默认一致（强制不用 air）
dev.ps1 同上四项            -> argv 完全一致
参数健壮性
  未知参数 / 缺值           -> rc=2 并打印 usage
  其他 cwd 调用 / 符号链接 / 含空格路径 -> rc=0
无 air 时                 -> 走 go run
有 air 且全默认           -> 执行 air（假 air shim 确认被调用、cwd 正确）
有 air 但改了端口         -> 回落 go run
未安装 go                 -> rc=1 并给出安装提示
实机启动
  dev.cmd -Port 8801/-NoOpen、dev.ps1 -Port 8802、dev.sh -p 8803、./dev -p 8805 -> HTTP 200
```

### internal/platform/ — 非 Windows 打开浏览器

**功能**：Linux / macOS 开发模式下 `panelURL` 能真正打开浏览器。

**修改内容**：原先 `other.go` 的 `OpenBrowser` 直接返回 `not supported`，导致 linux/mac 的 dev 脚本不带 `--no-open` 时面板不会自动打开。改为 darwin 走 `open`、其他 Unix 走 `xdg-open`（找不到时返回明确错误，不影响服务启动）。

**涉及文件**：`internal/platform/other.go`。

**验证基线**：`GOOS=linux`、`GOOS=darwin`、`GOOS=windows` 交叉编译均通过；`go vet ./...`、`gofmt -l` 无输出；`go test ./...` 全绿。

## 2026-10-21（第 2 轮）

### web/ — 内容区居中 + 会话区块聚合

**功能**：主内容水平居中；退出登录 / 状态显示 / 退出服务聚合为侧栏底部「会话」区块。

**修改内容**：

- 居中：`.main` 增加 `margin-inline:auto`，1100px 内容块在侧栏右侧 1fr 轨道内水平居中；≤1280 时轨道自然收窄贴满，移动端单列不受影响。
- 会话区块（`.side-foot`）：上边框分隔，内部两行——`连接状态`（`#dot` + `#stateText`，标签改为「服务正常 / 拉取中 / 待拉取 / 拉取异常」）与 `订阅状态`（新增 `#subStatus`：来源 · 缓存/更新失败 · 更新时间）。`.session-status` 固定 `height:38px` + 每行 16px 行高 + 省略号，文案变长不抖动；`#stateText` 由 140px 定宽改为 `flex:1;min-width:0` 等宽约束，`#subStatus` 定宽 168px（移动端 `flex:1`）。
- 按钮聚合：`#btnQuit` 从高级设置卡片按钮行移入侧栏底部，与 `#logout` 并排（`.session-actions`）；`#btnQuit` 改用 `.quit` 危险样式（红色描边 + hover 淡红底），与「退出登录」视觉区分，confirm 二次确认保留。
- 移动端：原 `.side-foot{display:none}` 改为保留显示（横排两行，`#subStatus` 改 `flex:1`），退出入口在 ≤760px 不丢失。

**涉及文件**：`web/style.css`（`.main` margin、`.side-foot`/`.session-status`/`.statusline`/`#stateText`/`#subStatus`/`.session-actions`/`.quit`、760px 媒体查询重写）、`web/index.html`（`.side-foot` 结构重写、高级设置按钮行移除 `#btnQuit`）、`web/app.js`（`renderState` 增加 `#subStatus` 更新、状态文案改为「服务正常」）。

**验证基线**（Chrome 真实登录态，Playwright `headless:false`）：

```text
留白（side.right → main 左右间隙）
  1920×1080  main x=510 w=1100  左 300  右 310
  1440×900   main x=270 w=1100  左 60   右 70
  1280×900   main x=210 w=1060  左 0    右 10（滚动条 gutter）
  1100×800   main x=210 w=880   左 0    右 10
  768×900    main x=210 w=548   左 0    右 10
  760/375    单列满宽，overflowX=0，两个退出入口均可见
状态区抖动   #stateText/#subStatus 塞入超长错误文案 -> .side-foot 高度 90→90，topbar 位移 0
退出服务     点击弹 confirm「退出 FastLink-tool 服务？」，dismiss 后服务仍存活（/state 返回 401 登录态校验正常）
```

## 2026-10-21（第 3 轮）

### 端口默认值 8788 → 8787

**功能**：dev 默认端口与生产默认（`config.go` 的 8787）对齐。

**修改内容**：`scripts/dev.sh`（`PORT` 默认值、帮助文案、air 判定阈值、注释）、`scripts/dev.ps1`（`[int]$Port`、帮助文案、air 判定阈值、注释）、`.air.toml`（`bin` 的 `--port`）、`internal/app/app.go:52`（dev 首次写入 config 的默认端口）、`README.md`（两处示例）。

**验证基线**：`go build` 通过；`bash -n scripts/dev.sh` 通过；`dev.cmd -Help` 显示 8787；argv 包装测试 5/5（断言已同步 8787）；实机 8787 监听成功且返回新代码。

**已知冲突**：本机 `0.0.0.0:8787` 仍被旧 Node 版 `D:\soft\app\fastlinktool\data\server-8e6c96ab.exe`（PID 37376）占用。`127.0.0.1:8787` 是新 Go 实例（PID 27704），但局域网 `192.168.1.10:8787` 由 `0.0.0.0` 那条优先匹配，仍响应旧代码。需先停旧实例，局域网才会走新代码。

### web/ — 侧栏贴内容 + 底部会话区重做 + 表单节奏

**功能**：修掉三个布局 bug（用户反馈「侧栏应贴内容」「底部像草台班子」「账号按钮贴着密码框」）。

**修改内容**：

- **Bug 1 侧栏贴内容**：`.main` 去掉 `width:100%;margin-inline:auto`，保留 `max-width:1100px` 左对齐。上一轮为居中加的 auto margin 在宽屏下把内容推到屏幕中央，造成侧栏与内容间 300px 空白（1920 下 `side.right=210`、`main.left=510`）。
- **Bug 2 底部会话区**：`.side-foot` 改为带边框与半透明底的容器（`border-top` + `rgba(24,35,59,.45)` + 12px 圆角内衬）；`.session-status` 拆为三行定高 —— `#stateText`（状态）/ `#subSource`（来源+缓存，新增）/ `#subStatus`（更新时间，短格式 `已更新 15:42`，跨天 `已更新 10/21 15:42`）；两个退出入口统一为 `flex:1;height:28px;border-radius:8px` 同一套按钮语言，`align-items:stretch` 保证同 y 同 h，仅以配色区分危险级（退出登录中性描边、退出服务淡红底红字加粗 600），confirm 保留。
- **Bug 3 表单节奏**：`.row` 统一 `margin-top:14px`（`.between` 行与 `.linkbox` 内归零，后者由 gap 接管）；`label` 改 `margin:14px 0 5px`；`grid2` 加 `margin-top:14px`；卡片首个子元素 margin 归零。根因级修复：`input/select/textarea` 加 `display:block;margin:0`（此前 textarea 为 inline-block，基线 4px 下垂导致按钮行间距不可控，实测 toolGap=18 而 accGap=14）。

**涉及文件**：`web/style.css`（`.main`、`.side-foot`、`.session-status`、`#subSource`、`#subStatus`、`.session-actions`、`.out`、`.quit`、`.row`、`label`、`.grid2`、`input/select/textarea`、760px 媒体查询）、`web/index.html`（`.side-foot` 三行结构、新增 `#subSource`）、`web/app.js`（`renderState` 时间格式化与信息重新分配、`title` 放完整信息）。

**验证基线**（Chrome headless，实测）：

```text
侧栏→内容间隙 main.left - side.right
  1920 / 1440 / 1280 / 1100 / 768 -> 0 / 0 / 0 / 0 / 0    （1920 下 mainRight=1310，1100 上限生效且左对齐）
底部会话区
  .side-foot box      w=181 h=119
  #logout             x=26    y=1022 w=75.5 h=28
  #btnQuit            x=109.5 y=1022 w=73.5 h=28        （sameY=true sameH=true）
  退出登录 bg=rgb(24,35,59) weight=400；退出服务 bg=rgba(239,68,68,.16) weight=600
  #subStatus  clientW=143 scrollW=143 fits=true  text=「已更新 15:42」
  #subStatus  title=「完整时间：2026/10/21 15:42:56 · 来源：本机客户端 · 缓存」
账号表单间距
  password.bottom(668) -> btnRow.top(682) = 14px；解密页 14px；高级设置 14px（三处一致）
状态区不抖动
  超长文案注入后 .side-foot 119->119，两按钮 y 均保持 1022，topbarTop 22->22
离线态
  在线「服务正常 / 已更新 15:42」-> 断线「服务未连接 / 2026/10/21 15:42:56」dot ok->warn -> 恢复「服务正常」，side-foot 全程 119
三页提示行
  accounts 96->96(+0)、tool 197->197(+0)、adv 377->377(+0)
移动端 375/768：overflowX=0，两个退出入口可见且 sameY/sameH
```

**过程记录（防回归）**：本项首版交付报告声称「`#subStatus` 无截断、`title` 含完整时间」，实测推翻——`clientW=143` 而 `scrollW=175`（溢出 32px），`title` 与可见文本完全相同。已退回同一会话要求拆行重做，最终改为三行并把完整日期时间放入 `title`，复测 `scrollW=clientW=143` 通过。

## 2026-10-21（第 4 轮）

### internal/service/ — 面板 `status` 字段类型容错（拉取账号报错修复）

**功能**：修掉「拉取账号报错 `接口返回非 JSON: json: cannot unmarshal string into Go struct field apiEnvelope.status of type bool`」。

**根因**：`apiEnvelope.Status` 声明为 `*bool`。面板实际返回的 `status` 可能是 bool、`0/1`、或 `"1"`/`"true"` 字符串，严格 bool 会让**每个** API 调用在解码阶段失败，且错误信息伪装成「返回非 JSON」，指向完全错误的方向。同时若响应是 WAF/人机验证的 HTML 页面，也会落进同一条误导性错误。

**修改内容**：

- `apiEnvelope.Status` 由 `*bool` 改为 `flexibleBool`，实现 `UnmarshalJSON` 接受 `bool` / 数字 / 字符串（大小写不敏感，含 `yes/ok/success/no/error/fail`）；`present` 字段区分「字段缺失」与「值为 false」。
- 未知字面量返回短错误（截断 40 字符），不把整段 payload 塞进错误信息。
- 新增 `looksLikeHTML()`：响应以 `<` 开头时判定为 HTML 页面，返回「接口返回了网页而不是 JSON，可能被 WAF / 人机验证拦截；请换网络或稍后重试」。
- `apiJSON` 内的判断由 `env.Status != nil && !*env.Status` 改为 `env.Status.present && !env.Status.value`。

**涉及文件**：`internal/service/service.go`（`apiEnvelope`、`flexibleBool`、`looksLikeHTML`、`apiJSON` 内解码分支）、`internal/service/envelope_test.go`（新增）。

**验证基线**：

```text
单元测试  go test ./internal/service/ -run "TestAPIEnvelope|TestLooksLikeHTML" -v
  status: bool true/false、number 1/0、string 1/true/0/false、TRUE、缺失、null -> 11/11 PASS
  未知字面量 -> 返回含 status 的短错误（<80 字符）PASS
  looksLikeHTML：WAF HTML 页面 -> true；带前导空白的 <html> -> true；JSON -> false；空白 -> false  PASS

真实面板回归（重启服务后）
  修复前 user@example.com -> error = "接口返回非 JSON: json: cannot unmarshal string into ... status of type bool"
  修复后同一账号 POST /api/v1/source refresh=true -> nodes=74 cached=false probeError="" lastLogin=1792577226，error 清空
  错误凭据 probe 账号 -> "HTTP 400 / Incorrect email or password"（面板业务错误正确透出，不再是 Go 类型错误）
```

### web/ — 保存账号后清空整个表单

**功能**：新增账号保存成功后清空输入框（用户反馈「保存输入账号密码后应清空输入框内内容」）。

**修改内容**：原实现只清 `#accPassword`，名称/域名/邮箱都留在框里。新增 `resetAccountForm()` 清空 `accLabel`/`accOrigin`/`accEmail`/`accPassword` 四个字段，**仅在保存成功后调用**，失败时保留用户已填内容。

**涉及文件**：`web/app.js`（新增 `resetAccountForm()`、`saveAccount()` 内改为调用它）。

**验证基线**（Playwright，真实登录态）：

```text
A 客户端校验失败（邮箱留空）
  填入 label=keepme / password=secret-keepme -> 点保存
  结果 label/password 保留，hint="请填写邮箱"        kept=true
B 服务端失败（路由拦截返回 400）
  四个字段全部填入 -> 点保存
  结果四个字段全部保留，hint="模拟服务端失败"        kept all=true
C 保存成功
  填入 label=okuser / origin / email / password -> 点保存
  结果 label="" origin="" email="" password=""，hint="保存成功"   ALL cleared=true
```

### web/ — 整体（侧栏 + 内容）在视口内居中

**功能**：修掉「页面没有居中，整体靠左」——上一轮误解为「内容贴侧栏」，实际需求是整个应用容器居中。

**修改内容**：`.app` 增加 `max-width:1310px;margin-inline:auto`（`1310 = 侧栏 210 + 内容上限 1100`），侧栏与内容作为整体在视口内水平居中；≤1280 时容器超过可用宽度，自动退化为满宽。

**涉及文件**：`web/style.css`（`.app`）。

**验证基线**（Chrome，`body` 宽已扣除 10px 滚动条槽）：

```text
1920x1080  appX=300 appW=1310  左 300  右 300   margin: 300px / 300px  对称
1440x900   appX=60  appW=1310  左 60   右 60                            对称
1280x900   appX=0   appW=1270  左 0    右 0                             满宽
侧栏->内容间隙 main.left - side.right = 0（两者连续，无断裂空白）
```

## 2026-10-21（第 5 轮）

### internal/app/ — dev 模式不再强制 loopback（局域网 IP 不可用修复）

**功能**：修掉「局域网 IP 没用」——dev 模式下手机/局域网设备无法访问面板与订阅链接。

**根因**：`internal/app/app.go` 在 `opts.Dev` 时硬编码 `cfg.Host = "127.0.0.1"`，覆盖了 `config.Load()` 读到的配置。后果：

- 高级设置页「监听范围」选「局域网 + 本机」并保存 → API 返回 `restartRequired`、配置文件也确实写成 `host: 0.0.0.0`，但重启后又被 dev 分支改回 `127.0.0.1`，只监听环回；
- 面板仍显示局域网地址（`lan` 来自网卡枚举，与实际监听无关），并生成 `http://192.168.1.10:8787/sub?...` 这类**实际连不上**的订阅地址与二维码，属于误导性输出。

**修改内容**：删除 `if opts.Dev { cfg.Host = "127.0.0.1" }` 这段覆盖。dev 模式仅在**配置文件不存在**时按 `127.0.0.1` 初始化（该初始化在 `Load()` 之前，逻辑保留），此后一律以配置文件为准，与生产行为一致。

**涉及文件**：`internal/app/app.go`（`Run()` 内删去 host 覆盖，留注释说明原因）。

**验证基线**（真实网卡 192.168.1.10，端口 8787）：

```text
配置 host=0.0.0.0 -> 监听 :: (双栈)
  127.0.0.1/health  -> ok
  192.168.1.10/health -> ok
  通过 LAN 地址打开面板：origin=http://192.168.1.10:8787，configHost=0.0.0.0
  面板生成的两个订阅地址均可达：
    http://192.168.1.10:8787/sub?key=... -> 200  31578 bytes
    http://127.0.0.1:8787/sub?key=...      -> 200  31627 bytes

配置 host=127.0.0.1（经 API 保存并重启）-> 仅监听 127.0.0.1
  127.0.0.1/health -> ok
  192.168.1.10/health -> 拒绝连接（预期，说明设置真正生效且不再被覆盖）

恢复 host=0.0.0.0 重启 -> 监听 ::，LAN /health -> ok
```

## 2026-10-21（第 6 轮）

### 解密工具：输入框与结果框重做

**功能**：优化解密工具的输入框与解密后的 YAML 结果框。

**修改内容**：

- **输入框**：`#toolInput` 由 `min-height:90px` 的空白大框改为 `.tool-input`（高 58px、`rows=2`、`wrap="off"`、`spellcheck=false`），长 URL 单行完整可读、横向滚动查看；上方补 `订阅链接` 标签；placeholder 与副标题同步为「https 订阅链接或 base64 内容」。
- **结果框**：`#toolYaml` 由 `min-height:90px` 改为 `.tool-yaml`（高 440px，≤760px 降为 340px，`rows=22`、`wrap="off"`、行高 1.55、等宽字体、内部滚动）。
- **信息头**：新增 `.yaml-head`（`转换后的 YAML` 标签 + `#toolMeta` 统计 + `#btnToolCopy` 复制按钮）。`#toolMeta` 显示「N 节点 · N 行 · 大小」，`tabular-nums` + 省略号 + `title`。
- **交互**：`#btnToolCopy` 复制 YAML（带大小反馈）；`#toolInput` 回车触发解密（Shift/Ctrl 除外）；成功时结果卡滚动置顶；失败时隐藏结果卡并清空，不留空的大框。

**涉及文件**：`web/index.html`（解密工具节结构）、`web/style.css`（`.tool-input`、`.tool-yaml`、`.yaml-head`、`.tool-meta`）、`web/app.js`（`decryptTool` 填充 `#toolMeta`、复制按钮、回车绑定）。

**验证基线**（Playwright / Chrome headless，真实 74 节点订阅）：

```text
                    之前          之后
#toolInput 高       90px          58px   （rows=2, wrap=off）
#toolYaml 高        90px          440px  （rows=22, 内部滚动 scrollH=7734）
结果卡整体高        -             514px
信息头              （无）         「74 节点 · 415 行 · 30.90 KB」
复制按钮            （无）         存在，点击提示「YAML 已复制（30.90 KB）」
失败（not a link）   -             结果卡 hidden=true、高度 0，无空框
1440/1280/768/375   -             overflowX 均 ≤0；375 时 yamlH 自动降为 340px
两框字体            -             均 ui-monospace
```

### 后端：解密工具的本机地址支持（含 SSRF 边界）

**功能**：修掉「解密工具无法处理面板自己发出的 `http://` 订阅链接」。

**根因（两层，第一层上一轮只修了一半）**：

1. `extractTarget` 只认 `https://`（上一轮已修）；
2. `assertFetchable` 仍强制 https 且禁止内网地址，`NewFetcher` 默认 `AllowPrivate=false`。所以即便 `extractTarget` 放行，fetch 层仍返回「只允许 https 订阅地址」。

**修改内容**：

- `internal/subscription/fetch.go`：抽出 `HostPolicy` 类型与 `StrictPolicy`（https + 禁内网 + 禁带凭据），保持默认行为不变；新增 `NewLoopbackFetcher`，策略为「仅 http(s) + 禁带凭据」，允许 loopback/内网。`FetchSubscription` 改为按 `Policy` → `AllowPrivate` → `StrictPolicy` 的顺序选择策略。
- `internal/service/service.go`：`DecodeURL`（仅解密工具使用）改走 `loopbackFetcher()`；`Refresh` 与账号登录仍走严格 `Fetcher`，因此**不扩大自动/不可信路径的 SSRF 面**。
- `internal/httpapi/server.go`：`/api/v1/decrypt` 响应增加 `nodes` 字段，用 `service.ExtractNodes` 解析（与订阅页同一套逻辑）。
- `web/app.js`：`#toolMeta` 的节点数改用后端 `data.nodes`，不再用 `/^- /gm` 正则统计。

**为什么必须改节点数来源**：该模板同时输出块风格 `- name:` 与 flow 风格 `- {"name":...}`，前端正则只能数到其中一种。实测同一份 YAML：`- name:` 匹配 18、`- {..."name"` 匹配 74，而真实节点数是 74 —— 首版交付因此显示「0 节点」。

**涉及文件**：`internal/subscription/fetch.go`、`internal/service/service.go`、`internal/httpapi/server.go`、`web/app.js`、`internal/subscription/policy_test.go`（新增）、`internal/httpapi/extract_test.go`（新增）。

**验证基线**：

```text
单元测试
  TestStrictPolicy       https 放行；http/127.0.0.1/localhost/192.168./10./::1/169.254./带凭据/ftp 全部拒绝
  TestLoopbackPolicy     http://192.168.1.10:8787、http://127.0.0.1、https:// 放行；ftp/file/带凭据 拒绝
  TestFetcherDefaultsToStrict  零值 Fetcher 不得触达内网
  TestExtractTarget*     https/http/前后空白/b64 包装/url 包装/裸 base64 放行；空输入/乱码/ftp 拒绝
  go test ./...  全绿

真实链路（本机 192.168.1.10:8787）
  POST /api/v1/decrypt {"input":"http://192.168.1.10:8787/sub?key=..."}
  修复前 -> 400「只允许 https 订阅地址」
  修复后 -> 200，yaml 30090 字符，节点 74（与订阅页一致）
```

## 2026-10-21（第 7 轮）

### 解密工具：同时保留「转换后」与「原始订阅」两份结果

**功能**：解密后同时展示两份内容 —— 套用 Clash 模板后的优化版，以及服务商原始解密出的原版，供对比后再决定用哪份。

**修改内容**：

- `service.LoginResult` 新增 `RawYAML` 字段（`ApplyTemplate` 之前的解密结果）。原先 `DecryptProfile` 的返回值被 `ApplyTemplate` 直接覆盖丢弃。
- `Service.DecodeURL`（仅解密工具使用）同时返回 `YAML` 与 `RawYAML`。
- `/api/v1/decrypt` 响应新增 `rawYaml` 与 `rawNodes`；`nodes` 仍用 `service.ExtractNodes` 解析（与订阅页一致）。
- 前端：`#toolResult` 增加第二个只读面板 `#rawYaml` + `#rawMeta` + `#btnRawCopy`；抽出 `yamlMeta()` 供两个面板复用；抽出 `copyPane()` 供两个复制按钮复用。
- 两面板内容相同时（例如粘贴的是面板自己的 `/sub`，本身已是模板格式），提示改为「解密成功（原始订阅已是模板格式，两份内容一致）」，避免展示两个一模一样的框却让人以为出错。
- 失败时同时清空两个面板与两个 meta，结果卡整体隐藏。

**涉及文件**：`internal/service/service.go`（`LoginResult.RawYAML`、`DecodeURL`）、`internal/httpapi/server.go`（`rawYaml`/`rawNodes`）、`web/index.html`（第二个面板）、`web/style.css`（`.raw-head`、`.raw-yaml`）、`web/app.js`（`yamlMeta`、`copyPane`、`decryptTool`）。

**验证基线**（Playwright / Chrome headless，上游真实订阅 `flesa.web.ak005.com`）：

```text
两份内容确实不同（不是重复渲染）
  转换后  30086 字符 · 415 行 · 30.86 KB · 74 节点  含模板头「基础配置」  不含 external-controller
  原始    26501 字符 · 187 行 · 27.10 KB · 74 节点  不含模板头          含 external-controller + secret
  identical=false

面板尺寸
  #toolYaml h=440（scrollH=7734）
  #rawYaml  h=300（scrollH=3495）
  结果卡整体 h=872；375 宽时降为 712（yaml 340 / raw 240）

复制按钮分别可用（实测剪贴板内容）
  复制原始 -> 26687 字符，提示「原始订阅 已复制（27.10 KB）」
  复制 YAML -> 30500 字符，提示「YAML 已复制（30.86 KB）」
  两者内容不同；raw 含 external-controller，conv 不含

已是模板格式的来源（面板自身 /sub）
  identical=true，提示「解密成功（原始订阅已是模板格式，两份内容一致）」

失败（not a link）
  结果卡 hidden=true、高度 0，两个面板 value 均为空，两个 meta 均为空

响应式  1440/1280/768/375 均无横向溢出
回归    几何位移 0、7 秒轮询无漂移、scrollTop 保持、三页提示行 +0、退出区 119px 不变
```

## 2026-10-21（第 8 轮）

### 订阅页：优化后 / 原始 两套二维码与链接

**功能**：订阅页的地址卡支持在「优化后」（套用 Clash 模板）与「原始」（服务商原样下发）之间切换，二维码与链接同步切换。

**修改内容**：

- **数据层**：`cache` 表新增 `raw_yaml` 列；`model.Snapshot` 新增 `RawYAML`；`PutCache`/`Cache` 读写该列；`fetchYAML` 与 `LoginAccount` 都保留 `RawYAML`；`Refresh` 写入缓存时一并保存。
- **兼容迁移**：新增 `Store.addMissingColumns()`。`CREATE TABLE IF NOT EXISTS` 不会给已存在的表加列，所以每个新列需要显式 `ALTER TABLE`，并把 `duplicate column` 视为已应用（幂等）。
- **接口**：`model.Address` 新增 `RawURL`；`Service.Addresses` 用 `withVariant()` 为每个地址派生 `&variant=raw` 版本；`/sub` 识别 `variant=raw` 并返回原始 YAML，若原始副本缺失（例如升级前的缓存行）则回退到模板结果，保证 URL 永不 404。
- **前端**：地址卡新增 `.variant-tabs`（优化后 / 原始）；`currentVariant` 持久化到 `localStorage["fl.variant"]`；`addressFor()` 统一解析当前变体 URL；`syncVariantTabs()` 同步标签、标题、二维码说明与差异说明；复制/打开/下载均跟随当前变体。

**涉及文件**：`internal/store/store.go`（`cache.raw_yaml`、`addMissingColumns`、`Cache`/`PutCache`）、`internal/model/model.go`（`Address.RawURL`、`Snapshot.RawYAML`）、`internal/service/service.go`（`fetchYAML`、`LoginAccount`、`Refresh`、`Addresses`、`withVariant`）、`internal/httpapi/server.go`（`/sub` 的 `variant=raw` 分支）、`web/index.html`（变体标签、地址标签 id、差异说明）、`web/style.css`（`.variant-tabs`、`.variant-tab`）、`web/app.js`（`currentVariant`、`addressFor`、`syncVariantTabs`、`renderAddresses`、按钮绑定）。

**验证基线**（Playwright / Chrome headless，真实 74 节点订阅）：

```text
两套地址
  optimized  http://192.168.1.10:8787/sub?key=abc...&(无 variant)
  raw        http://192.168.1.10:8787/sub?key=abc...&variant=raw

实际下载两者内容（不是仅看链接）
  opt 200, 30159 字符；raw 200, 26474 字符；identical=false
  opt 无 external-controller，raw 有
  opt 有模板头「基础配置」，raw 无
  opt proxy-server-nameserver = 223.5.5.5 / 119.29.29.29
  raw proxy-server-nameserver = appsu01.apiv2.a047.com/api-v2 等三行（即模板覆盖掉的原值）
  raw HEAD 200（客户端探测可用）

界面切换
  点「原始」后：subUrl 含 variant=raw、标签变「原始订阅地址」、二维码说明变「扫码导入 · 原始」、说明文案切换
  二维码 SVG 重新生成（13969 -> 17778 字节，节点不同）
  复制按钮复制的是当前变体的链接，提示「原始订阅链接已复制」
  刷新页面后仍保持上次选择（localStorage 持久化）

旧库兼容
  已有 fastlink.db（缺 raw_yaml 列）-> 启动自动 ALTER 补列，nodes=74，rawUrl 可用

回归
  几何位移 0、7 秒轮询无漂移、scrollTop 保持、三页提示行 +0、退出区 119px
  go test ./... / go vet / gofmt / node --check 通过
```

## 2026-10-22

### 账号系统：两类账号 + 一键激活 + 仅验证 + 自写弹窗

**功能**：账号页从「只能增删的密码账号列表」升级为完整账号管理，支持密码账号与链接账号两类、一键激活、编辑、仅验证、自写确认弹窗。

**数据模型**：

- `accounts` 表新增 `kind TEXT NOT NULL DEFAULT 'password'`（`password` / `link`）。旧库由 `Store.addMissingColumns()` 自动 `ALTER` 补列，存量账号回填 `password`（已用现有 `fastlink.db` 实测：迁移后原账号 `kind=password` 正常）。
- `email` 由 `NOT NULL`（无默认）改为 `NOT NULL DEFAULT ''`：链接账号没有邮箱。
- `model.Account` 新增 `Kind`；新增常量 `KindPassword` / `KindLink`。

**后端行为**：

- `UpsertAccount` 按 kind 分支校验：`password` 需邮箱（新增时还需密码）、`link` 需订阅链接；类型不可修改（返回「账号类型不可修改，请新建账号」）。
- 密码变更后调用 `Store.ClearAccountRuntime`：清空 `subscription_url` / `last_login` / `last_fetch` / `user_info_json` / `error` 并删除该账号缓存，强制下次重新登录换取新 token。密码留空则完整保留运行时状态。
- 链接账号不参与「同邮箱去重」：两个链接账号可指向同一 URL 而不会被合并成一条。
- `Resolve()` 对 `kind=link` 直接使用 `SubscriptionURL`，不尝试登录。
- 新增 `Service.ActivateAccount(id)` 与 `POST /api/v1/accounts {action:"activate"}`：切换当前来源，优先用缓存快照（`PrimeSource`），无缓存才真实拉取。
- 链接账号的默认 label 取 URL 主机名（`hostLabel`）。

**前端**：

- 列表改为两行卡（宽屏 grid：`"top ops" / "meta meta"`），第 1 行 `[密/链徽章] 名称 + 状态 pill | 激活/编辑/删除`，第 2 行标识或红色错误文案。
- 类型标识：左缘 3px 色条（密码 accent / 链接 muted）+ 16px「密/链」单字徽章，双通道。
- 操作层级：激活=蓝实心、编辑=描边、删除=红字描边；当前账号的激活位为 disabled 绿字「当前」（`.act{min-width:52px}` 保证切换时按钮不横移）。
- 表单：类型 tab 切换（`.form-sec` 用 `max-height+opacity` 0.22s 过渡）；编辑态标题「编辑账号」、按钮「保存修改」、密码框清空 + placeholder「留空则不修改」+ 黄色说明行；编辑态切类型提示「类型不可修改，切换类型将新建账号」。
- 仅验证：`POST /api/v1/account`，成功绿「验证成功：登录有效，未保存到列表」，失败红。
- 自写确认弹窗 `askConfirm()` 替换原生 `confirm`：Escape/取消/遮罩=取消，Enter/确认=确认，danger 样式，`role=dialog` + `aria-modal` + `aria-labelledby`，`prefers-reduced-motion` 下关闭动画。
- **API 域名改为可留空**：后端 `LoginAccount` 在 `apiOrigin` 为空时回退到 `defaultAPIOrigin`，存量账号 `demo` 的 `apiOrigin` 也是空的。前端原先强制必填，导致这类账号无法保存/编辑/验证（实测「请填写 API 域名」阻断）。标签改为「API 域名（可留空）」，placeholder「留空则用 https://flesa.apiv2.a047.com」。

**涉及文件**：`internal/model/model.go`、`internal/store/store.go`（schema、`accountColumns`、`scanAccount`、`UpsertAccount`、`ClearAccountRuntime`、`addMissingColumns`）、`internal/service/service.go`（`AccountInput`、`UpsertAccount`、`hostLabel`、`ActivateAccount`、`Resolve`）、`internal/httpapi/server.go`（`accountsPost` 的 activate 分支与 kind/subscriptionUrl 透传）、`web/index.html`、`web/style.css`、`web/app.js`、`internal/service/account_test.go`（新增）。

**验证基线**：

```text
单元测试（internal/service/account_test.go，7 个）
  TestUpsertLinkAccount                 链接账号无邮箱可建，kind 落库为 link，不带凭据
  TestUpsertLinkAccountRequiresURL      缺订阅链接报错，错误含「订阅链接」
  TestUpsertPasswordAccountValidation   缺邮箱/缺密码/未知 kind 均报错；label 默认取邮箱
  TestPasswordChangeClearsRuntime       密码留空保留 subscription_url 与 cache；
                                        改密码后 subscription_url 清空、lastLogin 归零、cache 被删
  TestKindChangeRejected                改 kind 被拒，错误含「类型」
  TestLinkAccountsDoNotDedupeByEmptyEmail  同 URL 两个链接账号不合并
  TestActivateAccount                   激活后 active=account:<id>；不存在的 id 报错

API 全链路（PowerShell，9 项）—— 全部 PASS
  create link / link requires url / create password / edit keeps password /
  kind change rejected / activate / verify-only no persist / delete both / restore local

真实 UI 端到端（Playwright，10 项）—— 全部通过
  1 初始列表               仅 demo
  2 新增链接账号           出现，hint「保存成功」
  3 新增密码账号           出现，hint「保存成功」
  4 编辑态                 标题「编辑账号」/按钮「保存修改」/密码框空/placeholder「留空则不修改」/黄说明可见
  5 密码留空保存           hasPassword 仍为 true
  6 仅验证（真实面板）     "HTTP 400 / Incorrect email or password"，红色
  7 一键激活               该行变「当前」且 disabled，active=account:<id>，原当前回落
  8 自写弹窗               标题「删除账号」、确认按钮「删除」、danger=true；取消后账号仍在
  9 确认删除               链接账号消失
 10 最终列表               仅剩 demo

布局（4 个账号：密码 + 长链接 + 失败链接 + 原账号）
  1440  ovX 无  h=65/65/65/65  全等  trunc=0
  768   ovX 无  h=65/65/65/65  全等  trunc=1（长 URL 物理极限）
  480   ovX 无  h=104×4        全等  trunc=1
  375   ovX 无  h=104×4        全等  trunc=2
  320   ovX 无  h=104×4        全等  trunc=3
  窄屏顺序：名称+状态 → 标识/错误 → 操作（实测 orderOK=true，名称左对齐 leftGap=15）
  激活切换 .act x 坐标 1151 → 1151，无横移

既有功能回归
  几何位移 0、7 秒轮询无漂移、scrollTop 保持、三页提示行 +0、退出区 119px 不变
  go test ./... / go vet / gofmt / node --check 全部通过
```

**实现期修掉的问题（记录以免回退）**：

1. 前端强制要求「API 域名」必填，与后端「留空回退默认域名」不一致，导致存量账号无法保存（详见上文）。
2. 窄屏顺序：`.accops` 原本嵌在 `.acctop` 内，`order` 无法越过 `.accmeta`。最终把 `.accops` 移出 `.acctop`，宽屏用 `grid-template-areas:"top ops" "meta meta"` 放回第 1 行右侧，窄屏切纵向 flex 自然落到第 3 行。
3. 窄屏名称居中：`display:contents` 拆层后 `.accname` 因 `flex-basis:0` 塌缩、被剩余空间推到右侧。改用 `.accitem` 纵向 flex + `.accname{flex:1 1 0}`，实测 leftGap 由 176/124/96 收敛到 15。
4. 760 断点 `.app{grid-template-columns:1fr}` → `minmax(0,1fr)`：`1fr` 的 auto min-size 会被内容顶宽，导致窄屏整页横向溢出。

## 2026-10-22（第 2 轮）

### 账号别名与名称查重

**功能**：链接账号可自定义名称（别名），且所有账号名称唯一。

**问题**：`#accLabel` 原先位于 `#secPwFields` 内部，切到「链接账号」时整个字段组被隐藏，链接账号只能靠 URL 主机名作标识，无法起名；后端也没有任何名称唯一性校验，可建出两个同名账号，而列表和订阅页的来源 chip 都只靠 label 区分。

**修改内容**：

- **名称字段移出类型分支**：`#accLabel` 从 `#secPwFields` 移到表单顶层（类型切换之上），两种类型都可见。标签改为「名称（可选，留空自动生成）」。
- **表单重排**：`API 域名` 与 `邮箱` 并列为 `.grid2`，`密码` 独占一行，减少纵向长度。
- **后端查重**：`UpsertAccount` 在确定 `id` 之前校验名称唯一性——大小写不敏感（`strings.EqualFold`）、前后空白忽略；编辑自身时跳过自身记录，因此「保持原名保存」不会被误判。冲突返回「账号名称「X」已存在，请换一个」。
- 留空时仍按原规则自动生成：密码账号用邮箱，链接账号用 URL 主机名（`hostLabel`）。

**涉及文件**：`web/index.html`（表单结构调整）、`internal/service/service.go`（`UpsertAccount` 查重）、`internal/service/account_test.go`（新增 3 个测试）。

**验证基线**：

```text
单元测试（新增 3 个）
  TestAccountNameMustBeUnique        同名跨类型被拒、前后空白被拒、不同名可建
  TestAccountRenameToSelfAllowed     编辑时保持原名可保存
  TestLinkAccountAlias               链接账号保留别名；留空回退到 URL 主机名

真实 UI（Playwright）
  1 链接账号下名称字段可见           shown=true，标签「名称（可选，留空自动生成）」
    填「我的机场」+ 订阅链接保存     出现「链 我的机场」，hint「保存成功」
  2 同名链接账号再建                 报错「账号名称「我的机场」已存在，请换一个」，同名条目仍为 1
  3 跨类型同名（密码账号用同名）     同样被拒
  4 「  我的机场 」带空格             同样被拒（大小写/空白不是漏洞）
  5 编辑该账号不改名直接保存         「修改已保存」，未被自身名称误判
  6 清理后                           仅剩原账号 demo

回归
  1440/1024/768 h=65 全等、480/375/320 h=104 全等，六个宽度 ovX 均无溢出
  三页提示行高度 +0
  go test ./... / go vet / gofmt / node --check 通过
```

**过程记录**：验证脚本的清理条件误用了 `example.com` 匹配，把我自己建的测试链接账号（label 自动取自 URL 主机名 `update.glados-config.com`）当成了残留。已核实原账号 未受影响，并单独删除该测试账号。

## 2026-10-22（第 3 轮）

### 防火墙：规则脚本 + 面板内只读自检

**功能**：解决「局域网连不上，但不知道是不是防火墙挡的」。提供一键创建规则的管理脚本，并在设置页显示可达性自检。

**背景**：Windows 防火墙在专用/公用配置文件下默认拦截入站。面板监听 `0.0.0.0` 是为了让手机扫码拉订阅，但没有对应规则时会被静默拦截，用户看不到任何错误。

**实现（只做 A+B，不做程序内提权改防火墙）**：

- `scripts/firewall.ps1`：`-Apply` / `-Remove` / `-Status` 三个动作。
  - 规则限定 `RemoteAddress=LocalSubnet`，不向公网开放；
  - 绑定 `-Program` 到具体 exe 路径，避免端口被其他程序蹭用；
  - 只建入站规则。Windows 出站默认允许，程序只访问面板 API 与订阅域名，额外出站规则只会扩大攻击面；
  - 幂等：重复 `-Apply` 先删旧规则再建，不会堆叠；
  - 非管理员执行 `-Apply` 明确拒绝并提示提权方式，不静默失败。
- `internal/platform/firewall_windows.go`：`InspectFirewall()` 只读自检。用 **netsh** 而非 `Get-NetFirewall*` cmdlet，原因见下文性能一节。规则名精确匹配 `FastLink-tool Panel`。
- `internal/platform/firewall_other.go`：非 Windows 返回 `supported=false`，不假装知道 ufw/firewalld/pf 的状态。
- `GET /api/v1/net-diag`：返回监听地址、端口、局域网 IP、防火墙配置文件、规则是否存在、是否可达、以及一句可操作提示。带 6 秒超时，避免 PowerShell/net 卡住拖死设置页。
- 设置页新增「网络可达性」卡片：五行只读数据 + 状态色提示 + 「重新检测」按钮。

**性能问题的发现与修正（重要）**：

首版用 PowerShell 查询防火墙，实测**单次请求 8456ms**，前端一直卡在「检测中…」。逐层定位：

```text
PowerShell 单次启动          ~2444ms
netsh advfirewall show       ~205ms
netsh rule query             ~180ms
```

改为 netsh 后：冷启动 563ms，命中缓存 61ms。同时加了 30 秒 TTL 缓存（`InspectFirewall` 带锁），并把探测放进带超时的 goroutine。

**一个必须记录的误报**：

首版规则匹配用了 `DisplayName -like '*FastLink*'`，实测 `ruleFound=true` 但实际并不存在面板规则——它匹配到了 FastLink VPN 客户端的三条规则（`sing-tun`、`FastLink_Lite Main In`、`FastLink_Lite Core In`）。这会让用户以为已放行，实际没有。已改为精确匹配 `FastLink-tool Panel`。

**涉及文件**：`scripts/firewall.ps1`（新增）、`internal/platform/firewall_windows.go`（新增）、`internal/platform/firewall_other.go`（新增）、`internal/httpapi/server.go`（`netDiag` 端点）、`web/index.html`（自检卡片）、`web/style.css`（`.netdiag` 系列）、`web/app.js`（`loadNetDiag`）。

**验证基线**：

```text
脚本
  -Status        正常显示三档 profile 状态、规则缺失提示、端口监听（:: 8787）
  -Apply 非管理员 明确拒绝，exit=1
  -Apply 提权     规则创建成功：Inbound/Allow/Any profile/TCP 8787/LocalSubnet/绑定 cmd.exe（测试用）
  规则生效后自检  ruleFound=True、reachable=True
  -Remove 提权    规则删除，remaining=0，自检回到 ruleFound=False / reachable=False

接口性能（连续 4 次）
  #1 563ms（冷启动）  #2 61ms  #3 56ms  #4 64ms（缓存命中）
  对照 /api/v1/config 41ms

自检数据正确性
  profiles = Domain（域配置启用，专用/公用关闭）
  ruleFound = False，reachable = False，提示文案指向 firewall.ps1 -Apply

前端渲染
  5 行数据全部显示且无截断，警告行黄色，点击「重新检测」重新拉取

回归
  1920/1440/1280/1100/768：侧栏与内容间隙 0，左右留白对称，无横向溢出
  轮询 6 秒无漂移，节点列表 scrollTop 保持 240
  三页提示行超长文案：卡片高度 delta 0/0
  go test ./... / go vet / gofmt / node --check 通过
```

**已知限制**：

- netsh 输出是本地化的，`enabledProfiles()` 同时匹配中英文关键字（域/Domain、专用/Private、公用/Public、启用/On、关闭/Off）。非中英文系统可能需要补充。
- 自检只判断「有无面板规则」，不校验规则的端口/程序是否与实际一致。用户手改过规则时可能显示已存在但实际不匹配。
- 程序内不做任何防火墙修改：静默改防火墙需要管理员清单，会让每次启动弹 UAC，并扩大攻击面。

## 2026-10-24

### internal/subscription/ — proxy-server-nameserver 追加机场自建 DoH

**功能**：机场在订阅原文的 dns 段下发自己的 DoH 解析器（`https://appdsv3fb.<front>/api-v2/<token>`，三个前置域名轮换）。模板原先用固定的 `223.5.5.5` / `119.29.29.29` 把它整体顶掉，等于放弃了机场那层“保证解析到真入口”的能力。改为把机场条目合并到模板默认值**之前**，公共 DNS 退为后备（仅当机场 DoH 域名自身被污染时兜底）。

**修改**：`template.yaml` 的 `proxy-server-nameserver` 块插入 `# __SUB_NS__` 标记行（注释形式，未被替换时模板仍是合法 YAML）；新增 `nameserver.go` 负责提取（块形式 + 流量形式）、校验、合并；`validNameserver` 只放行 `https/tls/quic/h3/udp` 方案、裸 IP 与 `system`，`http://` 与含空格的值一律丢弃；去重（对模板已有值、对自身）；上限 4 条，防止畸形订阅撑爆 dns 段；`ApplyTemplate` 在注入 proxies 与清除 controller/secret 之后调用合并，标记缺失时原样返回。

**涉及文件**：`internal/subscription/template.yaml`、`internal/subscription/nameserver.go`（新增）、`internal/subscription/nameserver_test.go`（新增）、`internal/subscription/template.go`（`ApplyTemplate` + `resolverSet`）。

**验证基线**（真实 74 节点订阅，`go run` 探针，探针用完即删）：

```text
raw 原文仅一行键，值在下一层 -> 提取到 3 条，按原序前置
18|   proxy-server-nameserver:
19|     - https://appdsv3fb.apiv2.a047.com/api-v2/<token>
20|     - https://appdsv3fb.web.ak005.com/api-v2/<token>
21|     - https://appdsv3fb.llguangli25n.com/api-v2/<token>
22|     - 223.5.5.5
23|     - 119.29.29.29
24|   nameserver-policy:
marker left: false；nodes: 74（未受影响）；default-nameserver 仍是两行公共 IP
go test ./... / go vet ./... / gofmt -l .（无输出）/ go build ./... 全通过
```

**取舍**：机场条目在前意味着机场若下发错误/被污染的解析器会直接命中，公共 DNS 只在其本身失败时才生效；上限 4 条是拍的值，机场正常只发 3 条。标记是注释而不是空占位，保证 `template.yaml` 单独看也是合法的、且旧版本二进制读新版模板不会崩。

### internal/service/ — 节点列表解析支持无引号 flow 写法

**功能**：某机场的订阅用 `- { name: 'HongKong 01 | x3.0', type: ss, server: x1.good2026.com, ... }`（无引号键 + 单引号值）的 flow 写法。`CountNodes` 靠行首 `- {` 数出 40，`ExtractNodes` 只认 `"name":"…"` 和 `- name:`，于是刷新显示“成功，共 40 个节点”但节点列表恒为 0。

**修改**：`flowName` / `flowSrv` 改为三选一捕获（双引号、单引号、裸值），键名也允许带引号；新增 `firstGroup()` 取第一个非空捕获；`name` 解析为空则跳过该条。块形式与 JSON 形式分支不变。

**涉及文件**：`internal/service/service.go`（`flowName`、`flowSrv`、`firstGroup`、`ExtractNodes`）、`internal/service/nodes_test.go`（新增 6 个用例）。

**验证基线**（真实订阅，40 节点，起服务走 `/api/v1/source` + `/api/v1/state`）：

```text
source -> {"nodes":40,"probeError":""}
state  -> nodes=40 nodeList=40 err=""
  HongKong 01 Netflix IEPL | x3.0 | x1.good2026.com
  HongKong 02 Netflix IEPL | x3.0 | x2.good2026.com
  HongKong 01 | x0.8 | mm.good2026.com
go test ./... 通过；gofmt -l 无输出；go vet 无输出
```

**取舍**：仍是正则解析，不做完整 YAML 解析。值里含逗号时靠引号保护（有用例覆盖）；节点名里若出现未加引号的逗号仍会截断。`CountNodes` 与 `ExtractNodes` 的判据已对齐，不会再出现“计数有、列表空”。

### internal/httpapi, internal/service — 订阅响应带配置名

**功能**：客户端从 `Content-Disposition` 的 `filename` 取名，没有它才回落成路径末段（于是所有订阅都叫 `sub`），更新时逐份覆盖同名配置。改为按来源生成 `<host>/<label>`，与真实面板（如 glados 的 `glados-facility.com/giffcores@gmail.com`）同形；同时补 `Profile-Web-Page-Url`。

**修改**：新增 `Service.SourceMeta()`（manual 用 URL 主机、account 用 label 或邮箱 + 订阅主机、local 固定“本机客户端”）与 `hostOnly()`；`sub` 处理器在写出前设置 `Content-Disposition` 与 `Profile-Web-Page-Url`，显式 `b64/url` 请求只用目标主机名。`profileFilename()` 清洗引号与控制字符，非 ASCII 时给 `filename="ascii 回退"` 并追加 RFC 5987 的 `filename*=UTF-8''…`，保证头部合法且中文名不丢。

**涉及文件**：`internal/service/service.go`（`SourceMeta`、`hostOnly`）、`internal/httpapi/server.go`（`sub` 里的头部、`profileFilename`、`hostOnly`、`explicitHost`）、`internal/httpapi/filename_test.go`（新增 2 个用例）。

**验证基线**（真实账号与真实手动订阅，起服务后看响应头）：

```text
手动源  -> Content-Disposition: attachment; filename="b.xlovehuawei.com.yaml"
账号源  -> Content-Disposition: attachment; filename="flesa.apiv2.a047.com/demo.yaml"
两者    -> Profile-Web-Page-Url: http://127.0.0.1:8787/
go test ./... / go vet / gofmt -l（无输出）通过
```

**取舍**：同名订阅仍会互相覆盖 —— 名字只决定“叫什么”，不决定“存哪几份”；要真正不覆盖得靠客户端的 provider/merge 机制，服务端管不了。`Profile-Web-Page-Url` 用的是请求 Host，通过域名访问时才会是域名；`b64/url` 一次性请求拿不到来源 label，只能用主机名。

### web, internal/* — 订阅名可自己填

**功能**：上一版名字只能自动生成（主机名 / 账号邮箱）。改为可输入：账号一直有“名称”字段（`accLabel`，留空才回退邮箱），手动源补同名输入框，另加 `?name=` 即时覆盖。

**修改**：`settings` 增 `manualLabel`（`ManualLabel`/`SetManualLabel`）；`SourceMeta` 的 manual 分支优先用该标签；`POST /api/v1/source` 接收并保存 `manualLabel`（保存位置在“未变化则跳过”判定之前，只改名字也会落盘）；`PublicAccounts` 回传 `manualLabel`；`sub` 支持 `?name=` 覆盖文件名；前端 `manualRow` 增输入框并在切换来源时回填、随请求提交。

**涉及文件**：`internal/store/store.go`、`internal/service/service.go`（`SourceMeta`、`PublicAccounts`）、`internal/httpapi/server.go`（请求体字段、保存、`?name=`）、`internal/httpapi/filename_test.go`（2 个新用例）、`web/index.html`、`web/app.js`。

**验证基线**（真实手动订阅，UTF-8 请求）：

```text
POST /api/v1/source {"manualLabel":"主线路"} -> 200
GET  /sub?key=... -> Content-Disposition: attachment; filename="b.xlovehuawei.com.yaml"; filename*=UTF-8''b.xlovehuawei.com%2F%E4%B8%BB%E7%BA%BF%E8%B7%AF.yaml
GET  /sub?key=...&name=temp -> filename="b.xlovehuawei.com/temp.yaml"
GET  /api/v1/accounts -> manualLabel 回显“主线路”
go test ./... / go vet / gofmt -l / node --check web/app.js 全通过
```

**取舍**：中文名在 `filename=` 里被剥成 ASCII、真名放 `filename*`；不认 RFC 5987 的老客户端会显示主机名。名字只影响显示，仍不解决同名覆盖（要免覆盖需 provider/merge）。`?name=` 不持久化，适合临时导出。

### internal/httpapi — filename 用账号名，不再加主机前缀

**功能**：上一版文件名是 `<主机>/<名称>`（仿 glados），实际使用里主机前缀是噪声。改为直接取配置的名称；没名称才回退主机。

**修改**：`profileFilename` 去掉拼接逻辑，`name = label`，为空才用 host，再为空用 `fastlink`；ASCII 回退同理（中文名剥离后为空则回退 host）。`filename*` 里现在就是纯名称。

**涉及文件**：`internal/httpapi/server.go`（`profileFilename`）、`internal/httpapi/filename_test.go`（用例改为“只出名称”）。

**验证基线**：

```text
账号源（label=demo） -> Content-Disposition: attachment; filename="demo.yaml"
单元用例：火星 + 主机 -> filename="b.xlovehuawei.com.yaml"; filename*=UTF-8''火星.yaml
go test ./... / go vet / gofmt -l 通过
```

**取舍**：同名账号仍需自己避免重名；不认 `filename*` 的客户端看到的是主机名或 `fastlink`。

## 2026-10-25

### internal/subscription/ · internal/service/ — 接入 Apex / Speed（APXS v2）
**功能**：让后端能取到 Apex / Speed 面板的**真实**订阅。这类面板（`com.flapp.clients.apex` v7.2.3）的订阅正文是 APXS v2 容器加密，旧的 `/api/v1/client/subscribe` 虽然仍返回 200，但内容是 SSPanel-Malio 兼容层的替代节点：134 条 vmess/ss 指向公网 IP、`uuid` 只有 2 个且几乎全是账号 uuid，响应首行就写着「本接口为SSPanel-Malio兼容层」。真节点只在 `/v2/feed/download` 的 APXS 密文里：74 条 vless + `xtls-rprx-vision`，单域名 `*.llguanglisf.com`，端口 10101–10200，`uuid` 各不相同。

**根因**：两处硬编码与新面板不匹配。

1. 订阅解密只认 `keys.go` 里的 Lite 方案（AES-GCM + 磁盘 XOR + 混淆 Base64），对 APXS 容器无从下手。
2. 登录固定打 `/api/v1/passport/auth/login`。Apex 面板的 API 整体在 `/v2` 下，且每个请求都要 mTLS 客户端证书；缺证书时 nginx 直接回 **404**（不是 403），所以现象是「接口不存在」而不是「没权限」，方向完全指错。

**修改内容**：

- 新增 `internal/subscription/apex_keys.go`：把 APK 里内联的三套 mTLS 证书与 32 字节 AES-256 密钥集中成一个文件，沿用 `keys.go` 的约定（客户端升级只改这里）。证书以 `purpose` 区分：`api` 打登录/账号端点，`subscription` 打订阅下载，`oss` 本仓库未使用。

- 新增 `internal/subscription/apex.go`：
  - `IsAPXS` / `ParseApexHeader`：容器前缀校验（magic、version=2、keyId、encVersion）。
  - `DecryptApexProfile`：切掉 13 字节头与尾部 80 字节（16B 校验 + 64B 签名），用 `cipher.NewCTR` 复现 GCM 的 CTR keystream 解密。
  - **不校验 GCM tag**：该服务端把 16 字节校验 + 64 字节签名放在尾部，但 tag 位置与标准 `nonce|ct|tag` 布局对不上，`gcm.Open()` 会拒绝正确密钥。改用「明文是合法 UTF-8 且像 Clash 配置」自证，错密钥无法通过。
  - `trimToValidUTF8`：尾部是二进制，按 UTF-8 边界截断，避免把解密成功变成解码失败。
  - `ApexHTTPClient(purpose, timeout)`：按用途加载证书的 `*http.Client`。

- `internal/subscription/decrypt.go`：`DecryptProfile` 先试 `IsAPXS`，命中走 APXS，否则原逻辑不变。旧的 raw YAML / 磁盘 XOR / AES-GCM / 混淆 Base64 四条路径全部保留。

- 新增 `internal/service/apex.go`：
  - `LoadApexConfig`：依次尝试三个 CDN 源取 `fl_apex.json`，解出 `domains` / `subscribe_domain` / `subscribe_path` / 版本。UA 必须小写 `apex/<version>`。
  - `apexSession`：`POST /v2/auth/token` → `auth_data` + `token`；随后 `GET /v2/feed/access-token` 取流量与到期。
  - `apexDownload`：按 `subscribe_path` 的映射（`/api/v1/client/subscribe?token=` → `/v2/feed/download?access_key=`）拼 URL，用 `subscription` 证书下载并解密。
  - `LoginApex`：串起全流程，`apexUserInfo` 把 `u`/`d`/`transfer_enable`/`expired_at` 映射成 UI 已有的 `model.UserInfo`，账号页的流量与到期无需改前端。
  - `decodeApexURL`：解密工具粘贴 APXS 链接时，普通 fetcher 拿不到证书会 404，改用 `subscription` 证书重取。

- `internal/service/service.go`：
  - `LoginAccount` 在 `/passport/auth/login` 返回 404 **或 200+HTML** 时回退 `LoginApex`；非上述情况（如 401 密码错）仍按原逻辑报错，避免把凭据错误误判成协议不匹配。
  - 新增 `htmlInsteadOfJSONError` 类型：把「返回网页而不是 JSON」从普通 error 提升为可判定类型。原先直接返回字符串，调用方无法区分「WAF 拦截」与「这个面板根本没有 /api/v1」。
  - 新增 `isMissingAPIRoute` 判定上述两种形态。

  > 触发这个修正的实测：旧默认域名 `https://flesa.apiv2.a047.com` 现在对所有路径返回 **200 + HTML 单页**（`<!doctypehtml><meta charset="UTF-8">`，12126 字节），不是 404。若只认 404，用户在「API 域名」留空时，Apex 账号会直接失败并报「可能被 WAF / 人机验证拦截」，方向完全错。
  - `LoginResult` 增加 `Apex *subscription.ApexHeader`，仅 APXS 路径填充。

- `internal/httpapi/server.go`：`/api/v1/decrypt` 响应增加 `apex` 字段（`version` / `keyId` / `encVersion` / `trailerLen`），非 APXS 时为 `null` 而非零值对象。

**涉及文件**：`internal/subscription/apex_keys.go`（新增）、`internal/subscription/apex.go`（新增）、`internal/subscription/apex_test.go`（新增）、`internal/subscription/decrypt.go`、`internal/service/apex.go`（新增）、`internal/service/service.go`、`internal/httpapi/server.go`。

**验证基线**：

```text
go build ./... / go vet ./...               通过
go test ./internal/subscription/ -run Apex   9/9 PASS
  RoundTrip / IsAPXS / ParseApexHeaderRejects / WrongKey / DropsTrailer
  Truncated / DecryptProfileDispatchesAPXS / CertificatesParse / ProfilesAreDistinct
go test ./internal/httpapi ./internal/service ./internal/secret ./internal/store   全部 ok

真实面板端到端（LoginApex）
  config source : https://cdno01.llguanglisf.com/apos/fl_apex.json
  panel type    : xboard  version 7.2.3
  api bases     : [https://flaxapi.web.ak005.com https://flaxapi.apiv2.a047.com]
  sub bases     : [https://flaxsub.web.ak005.com https://flaxsub.apiv2.a047.com]
  download path : /v2/feed/download?access_key=
  apex header   : {Version:2 KeyID:1 EncVersion:1 TrailerLen:80}
  userinfo      : up=23664086 down=1042278228 total=107606966272 expire=1798054159
  raw yaml      : 27905 bytes
  templated     : 46405 bytes, 74 nodes

真实面板端到端（HTTP 层，起服务 + 保存账号 + refresh + /state）
  POST /api/v1/refresh -> {"nodes":74,"probeError":"","userInfo":{...}}
  GET  /api/v1/state   -> healthy=true nodes=74 probeError=""
                          nodeList 74 条，first={name:香港-01 server:axtuapp-...llguanglisf.com}

套用模板后的 YAML 自检
  proxies 74（全 vless）、proxy-groups 18、rules 330
  external-controller / secret 已按模板规则移除
  分组引用与规则目标全部可解析（unresolved = 0）
  节点名无重复
```

**取舍**：

- APXS 尾部 80 字节（16B 校验 + 64B 签名）不解析，直接切掉；不影响 YAML 完整性。
- GCM tag 不校验，理由见上；以「明文像 Clash 配置」自证。
- `fl_apex.json` 的 `signature` 字段不校验。它保护的是 CDN 上的配置副本，而配置只提供域名/路径映射，实际数据仍由面板端点在 mTLS 下返回。
- 只测了 `arm64-v8a` 的密钥材料；`armeabi-v7a` 缺 `subscription` 证书，走不通订阅。
- 回退判定覆盖 404 与「200 + HTML」两种形态。若某面板用 403 表示「无此路由」，仍会落到原 `/api/v1` 报错路径，需要按实际面板调整 `isMissingAPIRoute`。

### internal/service/ · internal/config/ — 拉取提速与代理开关

**功能**：修掉「每次拉取订阅都很慢」，并把出网方式改成默认直连、可选走系统代理。

**根因**（三处，实测定位）：

1. **`fetchYAML` 用错 fetcher**。`Resolve` 已经复用了 `account.SubscriptionURL`（里面就含 access_key），但 `fetchYAML` 走的是 `s.Fetcher`（普通 fetcher，无 mTLS 证书）。Apex 的 `/v2/feed/download` 缺证书一律回 **404**，`IsStaleSubscription(err)` 因此判定「订阅失效」，`FetchResolved` 就每次都重新登录。实测每次刷新都在重复 `POST /v2/auth/token`。

2. **`LoginApex` 无条件重新登录**。即使已存 access_key，也先打 `/v2/auth/token` 再下载。

3. **每次请求新建 `http.Transport`**。`ApexHTTPClient` 每次 new 一个 Transport，连接池和 TLS 会话全不复用；一次刷新要建 4 次 mTLS 连接。

4. **默认走系统代理**。`http.Client` 未设 `Transport` 时用 `http.DefaultTransport`，会读 `HTTPS_PROXY`。本机 `127.0.0.1:7892` 的代理让每个请求多 100–900ms；同一请求直连 68ms。

**修改内容**：

- `internal/config/config.go`
  - 新增 `ProxyMode`（`direct` 默认 / `system`），`Load()` 里非法值一律回落 `direct`。
  - 常量 `ProxyDirect` / `ProxySystem`。

- `internal/subscription/apex.go`
  - 新增 `ApexTransport(purpose, proxyMode)`：可复用的 `*http.Transport`，带证书、`MaxIdleConnsPerHost=4`、`IdleConnTimeout=90s`、`ForceAttemptHTTP2`；`proxyMode=system` 时才挂 `http.ProxyFromEnvironment`。
  - `ApexHTTPClient` 保留为一次性便捷封装，注明「长期调用方应持有 Transport」。

- `internal/service/service.go`
  - `Service` 新增 `apexCfg/apexCfgAt`（配置缓存，TTL 30 分钟）、`apexAPI/apexSub`（连接池）、`apexAPIOnce/apexSubOnce`。
  - `fetchYAML` 先判 `isApexSubscriptionURL`，是则走新增的 `fetchApexYAML`（带证书、复用 access_key、不登录）。
  - `isApexSubscriptionURL` 匹配 `/v2/feed/download` 或配置里的 `DownloadPath`（域名会轮换，所以匹配路径而非 host）。
  - `LoginAccount` 在 `APIOrigin` 为空且已存 URL 时，直接 `fetchApexYAML`；失败才回落 `LoginApex`。这就是「token 没问题就一直复用」的实现。
  - 新增 `apexConfig`（带缓存）/ `invalidateApexConfig` / `proxyMode` / `apexTransport` / `ResetTransports`。

- `internal/service/apex.go`
  - `LoadApexConfig` 拆出 `loadApexConfig(ctx, timeout, proxyMode)`，配置拉取也走 `ProxyDirect`。
  - `apexSession` / `apexDownload` 改用 `s.apexTransport(...)` 的长连接池。
  - `LoginApex` 用 `s.apexConfig(ctx)` 取缓存；下载失败时 `invalidateApexConfig()`，让下次重新拉配置而不是在旧域名上重试到 TTL 过期。

- `internal/httpapi/server.go`
  - `POST /api/v1/config` 接受 `proxyMode`；模式变化时调 `Service.ResetTransports()` 立刻生效，不用等 idle 超时。

**验证基线**（真实面板，配置已缓存）：

```text
连续 4 次 LoginAccount（首次无 token，之后复用）
  #1  1720 ms  nodes=74  hadToken=false
  #2   900 ms  nodes=74  hadToken=true
  #3   389 ms  nodes=74  hadToken=true
  #4   432 ms  nodes=74  hadToken=true

单请求耗时（同一 transport，300ms 间隔）
  POST /v2/auth/token  330 / 325 / 290 / 504 / 991 ms
  GET  /v2/feed/download  2583 / 1404 / 318 ms
  → 登录本身稳定 ~300ms；下载首两次是面板现算 74 个 vless 节点，属服务端行为
```

**取舍**：

- `/v2/feed/download` 首次 2–2.5 秒是面板生成节点集的耗时，客户端无法优化；第 2 次起走面板缓存约 300ms。
- access_key 失效时靠 `fetchApexYAML` 返回的 401/403 触发回落登录，没有主动过期检测（面板不下发有效期）。
- 配置缓存 30 分钟；面板换域名后最多滞后 30 分钟，下载失败会立即 `invalidateApexConfig` 提前刷新。

### web/ — Apex 文案与代理开关

**功能**：账号页/解密工具的文案与新后端能力对齐，并给「走系统代理」一个开关。

**修改内容**：

- `web/index.html`
  - 账号页 `#typeNote` 默认文案改为「填写邮箱和密码即可，面板地址会自动识别。」
  - `#accOrigin` 标签改「API 域名（留空即可）」，placeholder 从 `留空则用 https://flesa.apiv2.a047.com` 改为「留空自动识别；只有连接其他 Xboard 面板时才需要填」。原默认域名现在是营销站，留着会诱导用户填错。
  - `#accSubUrl` placeholder 补 Apex 形态 `…/v2/feed/download?access_key=…`。
  - 解密工具说明补「加密订阅会自动识别」，`#toolInput` placeholder 补 `access_key` 形态。
  - 高级设置卡片新增代理开关行：`.proxy-row` + `#proxyNote` + `#btnProxy`。

- `web/app.js`
  - `setTab` / `enterEditMode` 两处 `#typeNote` 文案同步更新，链接账号分支补「Apex / Speed 的 access_key 链接同样支持。」
  - 新增 `proxyOn` 状态、`renderProxy()`、`toggleProxy()`。开关点击即刻 `POST /api/v1/config { proxyMode }` 写穿，不等「保存」：它是独立开关，后端在模式变化时会立刻丢弃连接池。
  - `loadConfig()` 读 `cfg.proxyMode` 并渲染。
  - `saveConfig()` 的 body 带上 `proxyMode`，避免「先拨开关再保存」时被覆盖。
  - 绑定 `$("btnProxy").onclick = toggleProxy`。

- `web/style.css`
  - 新增 `.proxy-row` / `.proxy-label` / `.proxy-note` / `#btnProxy` / `#btnProxy.on`。按钮 `min-width:56px` 固定宽度，避免「开启/关闭」切换时行内位移；`#proxyNote` 定高 + 省略号，长文案不撑高卡片。

**涉及文件**：`web/index.html`（账号页、解密工具、高级设置）、`web/app.js`（`setTab`/`enterEditMode`/`loadConfig`/`saveConfig`/新增 `renderProxy`/`toggleProxy`/事件绑定）、`web/style.css`（代理开关样式）。

**验证基线**：

```text
node --check web/app.js        通过
ID 一致性（JS 引用 vs HTML 定义）  无缺失；新增 btnProxy/proxyNote 均存在
go build ./... / go vet ./...  通过
go test ./internal/service ./internal/httpapi ./internal/config ./internal/store ./internal/secret  全 ok

真实服务 + HTTP 层（起 httpapi，登录后调 /api/v1/config）
  初始                        proxyMode=direct
  切 system                  返回 system, restartRequired=false
  再读（持久化）              system
  切回 direct                direct
  非法值 bogus               回落 direct
  先切 system 再保存整表      proxyMode=system 且 host=0.0.0.0（互不覆盖）
  落盘 config.json           proxyMode 正确写入
```

### internal/app/ — dev 首次种子配置改为局域网

**功能**：修掉「dev 启动后只监听 127.0.0.1，手机/局域网打不开面板」。

**根因**：`config.Default()` 的 `Host` 本来就是 `0.0.0.0`，但 `internal/app/app.go` 在 dev 模式首次初始化时**手写了一份配置结构体**，把 `Host` 硬编码成 `127.0.0.1`。于是每个全新的 `.run-dev` 状态目录都是仅环回，必须手动去高级设置里改成「局域网 + 本机」才生效。这与上一轮修掉的「dev 强制覆盖 cfg.Host」是同一类问题的两个入口。

**修改内容**：dev 首次种子改为基于 `config.Default()`，只覆盖 `LogLevel = "debug"`，其余（Host / Port / AccessKey / ProxyMode）一律继承默认值。同时补上 `ProxyMode` 字段，与 `Default()` 保持一致。

**涉及文件**：`internal/app/app.go`（`Run()` 内 dev 种子分支）。

**验证基线**：

```text
config.Default().Host         0.0.0.0
config.Default().ProxyMode    direct

全新状态目录首次种子
  host=0.0.0.0  port=8787  proxyMode=direct  logLevel=debug
  落盘 config.json 字段与默认值一致
```

**附**：已有的 `.run-dev/.run/config.json` 里是旧值 `127.0.0.1`，不会因代码修复而自动重建，已手工改为 `0.0.0.0` 并补 `proxyMode`。

### internal/service/ — 解密工具对 APXS 链接返回 404

**功能**：修掉「解密工具粘贴 Apex 订阅链接直接报 404」。

**根因**：`DecodeURL` 先用普通 fetcher 请求，拿到 `err` 就立刻返回；`IsAPXS(body)` 的判断排在 `err` 检查之后，永远走不到。而 Apex 的 `/v2/feed/download` 缺客户端证书必然是 404，所以带证书的 `decodeApexURL` 成了死代码。

**修改内容**：

- `DecodeURL` 先按 URL 形态判断（`/v2/feed/download`、配置里的 `DownloadPath`、或含 `access_key=` 的查询串），命中就直接走带证书路径；请求发出后再保留一次 `IsAPXS` 快照作为二次机会，兜住「代理/镜像把 APXS 转发过来」的情况。
- `isApexSubscriptionURL` 补 `access_key=` 兜底：配置缓存为空时 `DownloadPath` 不可知，仅靠路径匹配会漏。
- `decodeApexURL` 改用 `s.apexTransport("subscription")` 长连接池（原先每次 new transport），UA 版本优先取配置缓存里的值。

**涉及文件**：`internal/service/service.go`（`DecodeURL`、`isApexSubscriptionURL`、`decodeApexURL`）。

**验证基线**（本地 dev 构建，真实面板）：

```text
POST /api/v1/decrypt  input=<apex 订阅链接>
  修复前  HTTP 400 / HTTP 404 nginx 默认页
  修复后  nodes=74  rawNodes=74  apex={"version":2,"keyId":1,"encVersion":1,"trailerLen":80}
```

### 本地构建调试结论（打包产物验收）

按 `scripts/build.ps1` 同样参数本地构建 exe，逐项实测：

```text
health            ok
首页 / app.js / style.css  200
/api/v1/login     200
/api/v1/accounts  200（保存成功）
/api/v1/source    200
/api/v1/refresh   200 → 74 节点，probeError 空
/api/v1/state     healthy=true nodes=74
/sub              200 → 46433 B，74 vless，proxy-groups/rules 齐全，external-controller 已移除
/decrypt（APXS）   200 → 74 节点 + apex header
代理开关           system ⇄ direct 双向生效
```

另确认两点非缺陷现象：

- **单实例互斥锁**：已有实例运行时新实例静默退出（`Local\FastLinkTool`），表现为「双击没反应」。需先结束 `fastlink-tool.exe` 进程。
- **`go run` 不会热编译**：源码改动后若不重启，跑的还是旧二进制，`/api/v1/*` 会全部 404。

### internal/subscription/、.gitattributes — 模板换行归一，修掉本机 build 脚本报错

**功能**：修掉 `scripts/build.ps1` 在本机跑到 `go test ./...` 就抛 `go test failed` 的问题（3 个 `nameserver` 测试 `key missing`）。

**根因**：本仓库 `core.autocrlf=true` 且没有 `.gitattributes`，checkout 时 `go:embed` 的 `internal/subscription/template.yaml` 被写成 CRLF（实测 620 个 CRLF、0 个纯 LF）。模板里 `  proxy-server-nameserver:` 后跟的是 `\r\n`，而断言找的是 `"  proxy-server-nameserver:\n"`，因此必然 `key missing`。CI 上一直没暴露，是因为 `build.yml` 用 `-skip` 把这 3 个测试跳掉了。

**修改内容**：

- `template.go`：embed 拆成两个变量，`templateText` 在包初始化时做一次 `\r\n → \n`（顺带剥 BOM），让注入逻辑与换行无关；`ExtractProxiesBlock` / `upstreamNameservers` 对**输入订阅**的既有归一保持不变（输入是网络字节，仍需每次处理）。
- 新增 `.gitattributes`，只钉住 `internal/subscription/template.yaml text eol=lf`，不动其它文件，避免整仓重归一产生大面积 diff。
- `.github/workflows/build.yml`：`go test ./...` 去掉 `-skip`，并删除解释 skip 的注释。

**涉及文件**：`internal/subscription/template.go`（`embeddedTemplate` / `templateText` 初始化）、`.gitattributes`（新增）、`.github/workflows/build.yml`（`go test` 步骤）。

**验证基线**：

```text
git check-attr text eol -- internal/subscription/template.yaml
  -> text: set / eol: lf
删除后重新 checkout，模板为 LF：CRLF=0  LF=620  字节=23565
go test ./...                            （无 -skip）全绿
.\scripts\build.ps1 -Version 1.2.1       exit=0，exe 12,695,552 B + sha256 落盘
产物实测（新构建 exe，真实面板，74 节点）
  /sub  200  45850 B  CRLF=0  LF=695
  proxy-server-nameserver 段：3 条机场 DoH 在前，模板默认值在后
  proxies / proxy-groups / rules 齐全，external-controller 已移除
```

### internal/service/、internal/store/ — 遗留 apiOrigin 导致刷新 404 时自动回落到 Apex

**功能**：修掉「打包 exe 选中旧账号后拉订阅报 HTTP 404」以及「启动时闪两下 cmd 窗口」。

**根因**：旧版本给账号写过 `api_origin=https://flaxapi.apiv2.a047.com`。`LoginAccount` 只要看到 `apiOrigin` 非空就走 `/api/v1` 分支，而该主机迁移到 Apex 后 `/api/v1/*` 全返回 404 nginx 页（`curl` 实测 404）。账号因此永远登不上：先 404，再被当成普通失败，没有任何回落。

**修改内容**：

- `LoginAccount` 拆成「路由器 + `loginLegacy`」：`apiOrigin` 为空仍走 Apex；非空仍先走旧协议，但返回 `404/410` 时用同一套凭据再走一次 `LoginApex`，成功即 `SetAccountOrigin(id, "")` 把死域名清掉，后续登录不再白跑一次。
- 新增 `Store.SetAccountOrigin`。
- `fetchApexYAML` 的 UA 改为 `s.apexUserAgent()`（新增）：原先硬用内置默认版本，与 `decodeApexURL` 不一致；版本应当来自远端配置缓存。
- `platform`：两处 `netsh` 调用收进 `netsh()` 帮助函数并设 `SysProcAttr{HideWindow: true}`。GUI 子系统的发布版每调一次控制台程序就闪一个黑框，设置页一次检测正好两下。

**涉及文件**：`internal/service/service.go`（`LoginAccount`、`loginLegacy`、`apexUserAgent`、`fetchApexYAML`、`decodeApexURL`）、`internal/store/store.go`（`SetAccountOrigin`）、`internal/platform/firewall_windows.go`（`netsh`）。

**验证基线**（新构建 exe，真实面板，命中同一台机器上的遗留账号）：

```text
选中遗留账号（apiOrigin=flaxapi.apiv2.a047.com）
  修复前  nodes=0   error=HTTP 404 / <nginx 404 页>
  修复后  nodes=74  error=''  apiOrigin=''  subscriptionUrl 已回写
再刷新   200  544ms  74 节点  probeError 空
state    healthy=true nodes=74
/sub     200  45789 B  proxies / proxy-groups / rules 齐全
net-diag 防火墙字段仍正常解析（Domain 已启用、ruleFound=false）
```

### 全仓 — 删除机场侧 v1 协议，账号只走 Apex v2

**功能**：把 Xboard 风格的 `/api/v1/passport/*` 登录链路整体移除，面板只剩 v2 一种协议。

**根因（为什么删而不是留）**：v1 那条路唯一还活着的用途是「手填 API 域名连别的 Xboard 面板」，但常见部署全是 Apex，且老账号带着退役域名时还要靠上一节的 404 回落兜底；两条协议并存等于每次登录多一次可能 404 的往返。

**修改内容**：

- `LoginAccount` 只剩两步：已存 `subscription_url` 直接 `fetchApexYAML` 复用，否则 `LoginApex`；`loginLegacy` 整个函数删除。
- 一并删除只服务于 v1 的部件：`apiEnvelope` / `flexibleBool` / `looksLikeHTML`（含 `envelope_test.go`）、`subscription.APIUA`、`Store.SetAccountOrigin`。
- `apiOrigin` 字段从 `model.Account`、`AccountInput`、两处 HTTP DTO、`web/index.html` 的表单行、`web/app.js` 的读取/提交/列表指纹里全部移除；账号去重改成「同类型 + 同邮箱」，因为面板只剩一个。
- `store`：`accounts` 建表语句、`INSERT`/`UPSERT`/`SELECT`/`scanAccount` 里的 `api_origin` 全部去掉，索引由 `(email, api_origin)` 改为 `email`。已存在的库仍留着这一列（`NOT NULL DEFAULT ''`），新代码不读不写，不需要迁移。
- `service_test.go` 的登录用例改写成 v2 全链路（`/v2` 登录 + 业务接口 + APXS 下载），为此在 `subscription` 导出 `ApexProfileKey()` 供夹具构造容器。
- `docs/ARCHITECTURE.md` 的登录流程段同步为 v2。

**涉及文件**：`internal/service/service.go`、`internal/service/service_test.go`、`internal/service/envelope_test.go`（删除）、`internal/model/model.go`、`internal/store/store.go`、`internal/httpapi/server.go`、`internal/subscription/apex.go`、`internal/subscription/fetch.go`、`web/index.html`、`web/app.js`、`docs/ARCHITECTURE.md`。

**验证基线**：

```text
go build / go vet / go test ./...（含改写后的 v2 登录用例）  全绿
旧库（仍有 api_origin 列）启动、列表、保存、登录、刷新      正常，无 SQL 报错
POST /api/v1/accounts  不带 apiOrigin → 命中同邮箱旧账号，未产生重复行
POST /api/v1/account（仅验证） 200
refresh 200 → 74 节点；state healthy=true
页面与 app.js 中 accOrigin 出现 0 次
```

### web/ — 账号行去掉重复的「当前」标记

**功能**：当前账号那一行原本同时出现三个「当前」：名称旁的灰底标签、右侧绿色状态芯片、操作列里置灰的激活按钮。

**修改内容**：删掉名称旁的 `pill` 标签，并在当前账号上不再渲染激活按钮（状态芯片已表达该语义）。按钮组仍右对齐，行高不变（实测两行同为 65px，右侧留白同为 12px）。随之删掉只有该按钮用到的 `.accops .act:disabled` 规则；`.pill` 样式保留，节点数/账号数仍在用。

**涉及文件**：`web/app.js`（`renderAccounts`）、`web/style.css`（`.accops .act:disabled`）。

**验证基线**：

```text
当前行 DOM      密 | v2only | 当前   按钮：编辑 删除
其他行 DOM      链 | zz-link | 已保存订阅   按钮：激活 编辑 删除
两行 rowHeight 均 65，ops 右对齐留白均 12px
整页「当前」出现 1 次（账号行内）
```

### web/ — 账号卡显示机场原始订阅链接 + 复制的非安全上下文兜底

**功能**：账号卡第二行原本只显示邮箱，机场真实订阅链接没有任何地方能看到。现在有链接的账号直接把链接显示出来（地址过长时省略号截断，完整值在 `title`），点这一行即复制。

**修改内容**：

- `accountMeta`：错误文案优先；有 `subscriptionUrl` 时返回 `邮箱 · 链接`（链接账号就是链接本身）并带 `copy` 标记；无链接时退回邮箱/占位文案。
- 渲染时给该行加 `copy` 类与点击复制，复制成功在 `#accHint` 提示。
- 新增 `copyText`：`navigator.clipboard` 只在安全上下文可用，而面板常规用法是 `http://192.168.x.x:8787`（非安全上下文，`navigator.clipboard` 为 undefined），故补隐藏 textarea + `execCommand("copy")` 兜底；顺带把原有三个复制按钮（订阅地址、节点列表、解密两栏）也切到该函数，否则它们在局域网 http 下同样静默失效。
- `style.css`：`.accmeta.copy{cursor:pointer}` 与 hover 提亮。

**涉及文件**：`web/app.js`（`copyText`、`accountMeta`、`renderAccounts`、`copySubscriptionUrl`、三处复制按钮）、`web/style.css`。

**验证基线**（浏览器实测）：

```text
第二行      demo@example.com  ·  https://flaxsub.web.ak005.com/v2/feed/download?access_key=…
class       accmeta muted copy，行高仍 65px
点击        提示「订阅链接已复制」，系统剪贴板取到完整链接
订阅页复制   提示同样正确，不再走 navigator.clipboard 单路径
```

### 全仓 — 移除「读取本机客户端」订阅

**功能**：整块去掉 `local` 数据源与其全部入口（来源列表项、解密工具的「读取本机客户端订阅」按钮、`GET /api/v1/local-sub`、`FASTLINK_SUB_URL` 环境变量、`storage` 里的客户端解析）。

**为什么整块删而不是修**：客户端换了存储布局——`%APPDATA%\com.flapp.clients.apex\Fastlink-极速版\shared_preferences.json` 里 `flutter.config.profiles[].url` 不再是链接，而是 `前缀:base64` 的 103 字节 `APEX` 容器；没有 `database.sqlite`。该容器的 header 是 `APEX 02 <4B> <4B>`，不带 APXS 的 80 字节尾，且用 `apex_keys.go` 的 AES key 按 CTR（counter 0–3、全偏移非 nonce 位置）都解不出内容，`app.so` 里也找不到该 key 的 hex / base64 / 原始字节（客户端 7.2.3 已轮换）。继续留在代码里只会稳定报错。

**修改内容**：

- `internal/service/service.go`：删除 `LocalProfile` / `localAppDir` / `LocalProfile()` / `readClientProfiles` / `readLegacyProfile` / `LocalSubscriptionURL` / `resolveLocal` / `localSourceURL`；`sourceURL`、`SourceMeta`、`SetSource`、`Resolve` 的 `local` 分支去掉，`Resolve` 与 `Resolve("")` 的默认源改为 `manual`。
- `internal/httpapi/server.go`：删除 `localSub` 与对应路由。
- `internal/store/store.go`：`ActiveSource()` 默认值改为 `manual`；子来源 key 白名单去掉 `local`；新增 `migrateLocalSource()`（`Open` 时若 `active` 仍是 `local`，迁到首个账号，没有账号则落到 `manual`）。
- `web`：来源列表去掉固定项、`btnToolLocal` 按钮与处理函数；页面中已无「本机客户端」字样。
- 测试：删除 `local_test.go`；`server_test` 改用 `st.SetManualURL`；`store_test` 的子 key 断言换成 `manual`；新增两条 `Open` 迁移用例（有账号 / 无账号）。
- `docs/ARCHITECTURE.md`：订阅流程去掉 `local` 分支。

**涉及文件**：`internal/service/service.go`、`internal/httpapi/server.go`、`internal/store/store.go`、`internal/service/local_test.go`（删除）、`internal/httpapi/server_test.go`、`internal/store/store_test.go`、`web/app.js`、`web/index.html`、`docs/ARCHITECTURE.md`。

**验证基线**：

```text
go build / go vet / go test ./...            全绿
旧库 active=local 启动 → 自动迁到首个账号         state active=account:acc_…
GET /api/v1/local-sub                        HTTP 404（已无此接口）
refresh                                      200，74 节点
来源列表                                      仅「v2only / 手动链接」，页面无「本机客户端」
解密工具按钮                                  仅「解密 / 复制 YAML / 复制原始」
```

### web/ — 解密结果支持上下 / 左右两种排版

**功能**：解密结果原本固定上下堆叠。现在结果区顶部有「上下 / 左右」切换，默认上下；切到左右即两栏对照。选择写入 `localStorage`，刷新后沿用；视口窄于 900px 时强制回到上下，避免两栏被挤成细条。

**修改内容**：

- `index.html`：结果区加一条工具条（标题 + `.type-switch` 分段控件），两个面板各包进 `.tool-pane`，`.tool-panes` 作为栅格容器；原先独立的 `raw-head` 外边距只在上下列表下保留。
- `style.css`：`.tool-panes` 默认单列；`#toolResult.layout-h` 改为两列、两栏等高 460px，窄屏回落单列 340px。
- `app.js`：`toolLayout` 状态 + `renderToolLayout()`（切类、同步 `aria-selected`），点击写入本地；启动即按记忆渲染。

**涉及文件**：`web/index.html`、`web/style.css`、`web/app.js`。

**验证基线**（实测 DOM 几何）：

```text
默认          两个面板 top=516 / 1012，宽 694，上下堆叠
点「左右」    两个面板 top 相同、left=253 / 607、宽 340，并排；textarea 460px
点「上下」    回到 516 / 1012 堆叠
刷新          容器仍带 layout-h，「左右」保持选中，localStorage=fl.toollayout=h
```

### docs、全仓历史 — 真实邮箱替换为占位符

**功能**：仓库公开，`MODIFICATIONS.md` 里留有两处真实邮箱（一处是 API 报错示例里的个人地址，一处是账号卡示例里的登录邮箱）。换成占位符，并连历史一起洗掉。

**修改内容**：

- 报错示例里的个人地址 → `user@example.com`。
- 账号邮箱 → `demo@example.com`（账号卡示例）。
- 用 `git filter-branch --tree-filter` 对全部历史施加同一替换，`--tag-name-filter cat` 顺带重写 `v1.1.1` / `v1.2.0` / `v1.2.1` 三个 tag，随后强制推送 master、`feat/apex-speed-client` 与全部 tag。

**涉及文件**：`MODIFICATIONS.md`（工作区内容 + 全部历史对象）、三个 tag 对象。

**验证基线**：

```text
git grep xiangdemo <重写后全部提交>        无命中
raw.githubusercontent master / v1.2.1     MODIFICATIONS.md 均无旧邮箱，占位符在位
远端 refs                                 master=117040a，tag 均指向重写后提交
CI（tag 推送触发的构建）                    build success / release success
```

**遗留**：GitHub 上被替换下去的旧提交仍能按 SHA 直接访问（不可达但未 GC），要彻底消失得让 GitHub 侧回收。

### docs、全仓历史 — 局域网 IP、来源 key、账号尾号一并占位

**功能**：上一轮清掉邮箱后，仓库里还剩内网地址、面板来源 key、账号尾号三类实际值，一并换成占位符并再次重写历史。

**修改内容**：

- 局域网地址 → `192.168.1.10`（`MODIFICATIONS.md`、`docs/ARCHITECTURE.md`、`extract_test.go`、`policy_test.go`）。
- 面板来源 key → `abc`（`sub?key=…` 两处，其中一处原本还是截断形态）。
- 账号尾号 → `demo`（正文、用例记录与 `Content-Disposition` 示例）。
- 同一套替换再走一次 `filter-branch --tree-filter`，tag 继续 `--tag-name-filter cat`，随后强推 master、`feat/apex-speed-client` 与三个 tag。

**涉及文件**：`MODIFICATIONS.md`、`docs/ARCHITECTURE.md`、`internal/httpapi/extract_test.go`、`internal/subscription/policy_test.go`（内容 + 全部历史对象）。

**验证基线**：

```text
新历史（master + 三 tag + 特性分支，11 个提交）
  旧 IP 0 命中 / 旧 key 0 命中 / 旧尾号 0 命中
远端 master 原始文件：旧 IP 无、192.168.1.10 在、旧 key 无、旧尾号无
```

**未动**：`flesa.apiv2.a047.com`、`b.xlovehuawei.com` 等机场域名，以及 `giffcores@gmail.com`（第三方面板示例）。

## 2026-10-30

### 订阅密钥 — 稳定不自动变，可自定义（含账号表单）

**功能**：每个来源（`manual` / `account:<id>`）的订阅 key 一旦存在就不再自行变化；用户可以在新增/编辑账号时直接填，也能在订阅页单独改。原来的「重置链接」只能换成随机值，且账号表单没有入口。

**修改内容**：

- `internal/store/store.go`：新增 `SetSubKey(source,key)`（upsert，写入前用 `SubKeyOwner` 查重，被其他来源占用的 key 返回 `ErrSubKeyInUse`）。`EnsureSubKey` / `EnsureAllSubKeys` 语义不变 —— 只在缺失时生成，绝不覆盖已有 key。
- `internal/service/service.go`：
  - `AccountInput` 新增 `SubKey`。`UpsertAccount` 在写库**之前**校验 key 形状与归属，非法 key 不会留下半更新的账号；`SubKey` 留空则完全不动已存的 key（新建时仍自动生成）。
  - 新增 `SetSubKey(source,key)`：来源存在性校验（`validSource`）、`^[A-Za-z0-9_-]{6,64}$`、不得等于面板口令、不得与其他来源重复。
  - `RotateSubKey` 增加来源校验，避免拼错名称写出孤儿行。
  - `PublicAccounts()` 增加 `subKeys`（来源 → key），供编辑账号时回填。
- `internal/httpapi/server.go`：
  - `POST /api/v1/sub-key` 接收 `{source,key}`：`key` 非空 → `SetSubKey`；为空 → 仍走随机 `RotateSubKey`（显式动作）。响应增加 `key`。顺带修掉旧实现里对 `manual` 调用 `SetSource(source,"")` 会把 `manualUrl` 清空的问题（改为只做来源校验，不再改当前来源）。
  - `POST /api/v1/accounts` 透传 `subKey`；`GET /api/v1/state` 的 `source.key` 暴露当前来源的 key（面板已登录，且 key 本就出现在每个订阅地址里，无新增暴露面）。
- `web/index.html` / `web/style.css` / `web/app.js`：
  - 订阅页「重置链接」→「修改密钥」，展开 `.gone` 折叠行 `#keyRow`（`#sourceKey` + 保存密钥 + 随机生成），展开时回填 `state.source.key`，收起后卡片高度与提示行位置复原；轮询不会覆盖正在编辑的输入（`syncKeyField` 跳过聚焦中的输入）。
  - 「随机生成」走 `askConfirm` 二次确认（旧链接/旧二维码失效）；保存与随机都只在点击后生效，加载、轮询、切换来源都不会动 key。
  - 账号表单新增「订阅密钥（可选，留空自动生成）」，两种账号类型都可见；编辑时回填 `accounts.subKeys["account:<id>"]`，保存时随 `subKey` 提交，取消/保存成功后清空。
  - 订阅页说明文案改为「保存后不会自动变化；可在账号表单或上方「修改密钥」处自定义」。

**涉及文件**：`internal/store/store.go`、`internal/service/service.go`、`internal/service/account_test.go`、`internal/httpapi/server.go`、`internal/httpapi/subkey_test.go`（新增）、`web/index.html`、`web/style.css`、`web/app.js`、`README.md`、`docs/ARCHITECTURE.md`。

**验证基线**：

```text
go build ./... / go vet ./...                       通过
go test ./...（新增 TestSetSubKeyValidation / TestSubKeyStableAcrossEdits /
  TestUpsertAccountSubKey / TestSubKeyCustomAndRotate / TestAccountsSubKeyRoundTrip）全绿
gofmt（转 LF 后对 5 个改动文件）                    仅 server.go 缺文件末尾换行，为改动前既有状态

真实服务 + HTTP 层（dev 构建，8899，临时状态目录）
  设自定义 key            200，响应 key=my-own-key，state.source.key 同步
  /sub?key=<新 key>       502（源无订阅地址，说明已通过鉴权）
  /sub?key=<旧 key>       401
  短 key / 面板口令 / 未知来源   均 400
  链接账号带 subKey 创建   200，subKeys 中出现 account:<id> = acc-key-01
  编辑账号不带 subKey      200，key 仍为 acc-key-01（未被覆盖）
  新建同名 key            400，账号数仍为 1
  随机生成                 200，key 变化，旧 key 失效

浏览器（Chrome，127.0.0.1:8899，真实登录态；过渡动画在不可见窗口会被节流，
几何数据为临时禁用 transition 后测得）
  订阅页 keyRow 收起 0 / 展开 34px；首张卡片 186 → 230 → 186（开/合对称，无残留位移）
  展开即回填当前 key 并聚焦；保存后 subUrl 含新 key，提示「订阅密钥已更新」
  账号表单编辑态回填 acc-key-01；非法 key 提交 → 红字「订阅密钥只能用字母、数字、- 和 _，长度 6–64 位」且输入保留
  合法改 key → 「修改已保存」，服务端 subKeys 变为 acc-key-02
  新增账号填写 key → 保存成功、表单清空、key 为填写值
  6 秒（跨一次轮询）后 manual key 不变（stable=true）
  375 / 768 宽（iframe 视口）：overflowX=0，keyRow 高 34，输入框 139 / 298px，按钮不超出卡片
  JS 运行时错误：仅一条预期的 400（非法 key 用例），无其他报错
```

**取舍**：

- 空 key 仍代表「随机生成」，因此 `POST /api/v1/sub-key {source}` 的老行为保持兼容；但 UI 只有「随机生成」按钮才发这个请求。
- 校验只允许 `[A-Za-z0-9_-]{6,64}`：key 会进 URL、二维码与客户端配置，空格/引号/斜杠一律拒绝。低于 6 位会降低暴力枚举成本。
- key 与面板口令相同的请求被拒绝，避免两套凭据互相混淆。
- `sub_keys.key` 是全局唯一约束，两个来源不能共用 key；写入前查重 + UNIQUE 约束双保险（单用户场景，未做事务级串行化）。
- 账号被删除后其 key 会在下次启动的 `EnsureAllSubKeys` 清理；期间若被其他来源占用，会提示「已被其他来源使用」。

### 账号表单 — 密码回填 + 显隐（含「原样保存」语义）

**功能**：编辑账号时默认回填已保存的密码，输入框右侧提供「小眼睛」切换明文/密文；只是改别名等操作而密码未变时，不再被误判为改密。

**修改内容**：

- 回填走按需接口，不塞进列表：
  - `internal/service/service.go` 新增 `AccountPassword(id)`：`Account` 存在性校验 → `PasswordEnc` 为空报「该账号没有保存密码」→ `Protector.Decrypt` 失败报「密码无法解密（可能来自其他机器）」。
  - `internal/httpapi/server.go` 新增 `POST /api/v1/account/password`（`{id}` → `{password}`，挂在 `s.auth` 下），未登录 401，未知/空 id 400。
  - 明文只在打开编辑态时请求一次；`GET /api/v1/accounts` 仍不含密码，避免每 5 秒轮询把明文密码塞进响应。前端用 `editingId !== acc.id` 丢弃迟到的响应，防止 A→B 快速切换时串号。
- 「原样保存」不再等于改密：`UpsertAccount` 在收到非空 `password` 时先 `Decrypt(existing.PasswordEnc)` 比对，相同则保留原密文并置 `passwordChanged=false`（保留 `subscriptionUrl` / 缓存 / `lastLogin`），不同才重新加密并清 sqlite 里该账号的运行时状态。此前编辑框留空被当作「不修改」，改成回填后必须补这一层，否则每次保存都会触发重新登录 + 清缓存。
- `web/index.html` / `web/style.css` / `web/app.js`：
  - 密码输入包进 `.pwwrap`，右侧 `#btnPwEye`（`.pweye`，内联 SVG 眼睛 + `.eye-slash` 斜杠），`input{padding-right:40px}` 避让；`28×28` 按钮绝对定位垂直居中，不吃输入框高度。
  - `setPwVisible(on)` 只切 `input.type`，不重建节点（保留输入内容与光标），同步 `aria-pressed` / `aria-label` / `title`（显示密码 ↔ 隐藏密码）；`enterEditMode` / `exitEditMode` 都复位为密文。
  - `enterEditMode` 改为 async：先置 placeholder「加载中…」，再取密码；失败则 placeholder 改「无法回填，请重新输入」并把错误写进 `#accFormHint`。`#pwNote` 文案改为「已回填当前密码；修改后会重新登录并清空该账号缓存。」
- 测试：`internal/service/account_test.go` 新增 `TestSamePasswordEditKeepsRuntime`、`TestAccountPassword`；`internal/httpapi/password_test.go`（新增）覆盖列表不含明文、接口 200/401/400、同密码重存保留 `subscription_url`。

**涉及文件**：`internal/service/service.go`（`AccountPassword`、`UpsertAccount` 密码比对分支）、`internal/service/account_test.go`、`internal/httpapi/server.go`（路由 + `accountPassword`）、`internal/httpapi/password_test.go`（新增）、`web/index.html`（`.pwwrap` / `#btnPwEye` / `#pwNote`）、`web/style.css`（`.pwwrap` / `.pweye`）、`web/app.js`（`setPwVisible`、`enterEditMode` async 化、`exitEditMode`）。

**验证基线**：

```text
go build ./... / go vet ./...                        通过
go test ./internal/service ./internal/httpapi        全绿（新增 3 个 service 测试 + 1 个 httpapi 测试）
node --check web/app.js                              rc=0

真实服务 + HTTP 层（dev 构建，8899，临时状态目录）
  POST /api/v1/accounts（password=p@ss-word）        200
  GET  /api/v1/accounts 响应体                        不含 "p@ss-word"
  POST /api/v1/account/password {id}                  200，password=p@ss-word
  未知 id / 空 id                                     400
  无 cookie                                          401

浏览器（Chrome，127.0.0.1:8899，真实登录态；过渡动画在不可见窗口被节流）
  进入编辑态：type=password，value=p@ss-word，placeholder=必填
  点小眼睛：type=text，aria-label=隐藏密码，aria-pressed=true，按钮加 .revealed
  再点一次：type=password，aria-label=显示密码
  几何：inputH=38（未因按钮变高），按钮距输入框右边缘 4px 且完全落在框内，overflowX=0
  375 宽 iframe：inputW=303，inputH=38，按钮仍在框内，overflowX=0，回填值一致
  原样保存：提示「修改已保存」，输入值保留 p@ss-word，接口再查仍能解出 p@ss-word
  取消后再进入编辑：仍回填 p@ss-word，type 复位为 password
  新增态：value 为空，placeholder=必填
```

**取舍**：

- 密码明文不进列表接口，只在打开编辑态时取一次；代价是编辑面板会多一次往返（本机毫秒级），换取轮询不重复传明文。
- 复用 `Protector.Decrypt` 做比对的副作用：旧机器密文（DPAPI 其他用户）解不开时会回填失败并提示重填，此时保存视作改密 —— 与旧行为一致，不做静默兜底。
- 「相同密码 = 不改」判断依赖解密成功；若将来引入不可逆哈希存储，这层比对需改成存储侧校验，否则每次保存都会清缓存。
- 小眼睛只做显隐，不额外提供「复制密码」，避免把明文密码放进剪贴板历史。

## 已知取舍
- 滚动条在 Chromium 无法同时做到「自定义 thumb 留边」和「保留原生箭头按钮」，本次按规范优先轨道透明与自定义 thumb，箭头按钮在 WebKit 分支被移除。
- 用 overlay 滚动条的平台（macOS 默认、Chrome headless）不占位，`offsetWidth-clientWidth` 为 0，轨道像素验证需 `headless:false`。
- `dist/.run/` 保留着旧 Node 版的 `accounts.json` / `cache/*.json`，是 Go 版首次启动自动迁移的数据源，未随旧产物一并清理。
- 版本号：本次为功能提交，`cmd/fastlink-tool/main.go` 的 `version` 已递增到 `1.2.2`；发布仍以 `scripts/build.ps1 -Version` 或 tag 注入为准。
- `internal/subscription` 的 3 个 `nameserver` 测试（`TestUpstreamNameserversPrependedBeforeDefaults` / `...DedupedAgainstDefaults` / `TestNoUpstreamNameserversLeavesDefaultsAdjacent`）曾因 CRLF checkout 失败（`nameserver_test.go:34/56/157: key missing`），已由模板换行归一 + `.gitattributes` 修掉，`build.yml` 的 `-skip` 已移除。
