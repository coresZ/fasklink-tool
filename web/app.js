import { qrSvg } from "/vendor/qr.mjs";

const $ = (id) => document.getElementById(id);
let state = null;
let accounts = { active: "local", manualUrl: "", accounts: [] };
let addresses = [];
let currentAddress = 0;
// Which subscription variant the address card shows: "optimized" (Clash template
// applied) or "raw" (provider's own config). Persisted so a reload keeps the choice.
let currentVariant = localStorage.getItem("fl.variant") === "raw" ? "raw" : "optimized";
let offline = "";

async function api(path, options = {}) {
  const opts = { ...options, headers: { ...(options.headers || {}) } };
  if (opts.body && typeof opts.body !== "string") {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(opts.body);
  }
  const res = await fetch(path, opts);
  const data = await res.json().catch(() => ({}));
  if (res.status === 401) {
    showLogin();
    throw new Error(data.error || "需要口令");
  }
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
  return data;
}

function escapeHTML(value) {
  return String(value ?? "").replace(/[&<>"']/g, (ch) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[ch]));
}

function showLogin() {
  $("app").classList.add("hidden");
  $("login").classList.remove("hidden");
}

function showApp() {
  $("login").classList.add("hidden");
  $("app").classList.remove("hidden");
}

function setHint(id, text, ok = false) {
  const el = $(id);
  if (!el) return;
  el.textContent = text || "";
  el.title = text || "";
  el.className = "hint" + (text ? (ok ? " ok" : " err") : "");
}

function formatBytes(n) {
  if (!n || n <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0, v = n;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i += 1; }
  return (i === 0 ? v.toFixed(0) : v.toFixed(2)) + " " + units[i];
}

function setText(el, text) {
  if (el.textContent !== text) el.textContent = text;
}

function renderTraffic() {
  const info = state && state.userInfo;
  const has = Boolean(info && info.total);
  const used = has ? (info.download || 0) + (info.upload || 0) : 0;
  const ratio = has ? Math.min(100, used / info.total * 100) : 0;
  const cls = has ? (ratio >= 90 ? "bar danger" : ratio >= 70 ? "bar warn" : "bar") : "bar";
  const box = $("traffic");
  // Build skeleton once; afterwards update in place so the bar animates smoothly.
  if (!box.dataset.ready) {
    box.dataset.ready = "1";
    box.innerHTML =
      `<div class="bar"><i style="width:0%"></i></div>` +
      `<div class="muted traffic-summary"></div>` +
      `<div class="stats">` +
      `<div class="stat"><div class="k">已用</div><div class="v"></div></div>` +
      `<div class="stat"><div class="k">下载</div><div class="v"></div></div>` +
      `<div class="stat"><div class="k">上传</div><div class="v"></div></div>` +
      `<div class="stat"><div class="k">总量</div><div class="v"></div></div>` +
      `<div class="stat"><div class="k">到期</div><div class="v"></div></div>` +
      `</div>`;
  }
  const bar = box.querySelector(".bar");
  const fill = box.querySelector(".bar i");
  if (bar.className !== cls) bar.className = cls;
  fill.style.width = ratio.toFixed(1) + "%";
  const summary = has ? `总量 ${formatBytes(info.total)} · 已用 ${formatBytes(used)}` : "暂无流量数据";
  setText(box.querySelector(".traffic-summary"), summary);
  box.querySelector(".traffic-summary").title = summary;
  const stats = box.querySelectorAll(".stat .v");
  const value = (v) => (has && v != null ? formatBytes(v) : "--");
  const expire = has && info.expire ? new Date(info.expire * 1000).toLocaleDateString("zh-CN") : "--";
  setText(stats[0], value(used));
  setText(stats[1], value(info && info.download));
  setText(stats[2], value(info && info.upload));
  setText(stats[3], value(info && info.total));
  setText(stats[4], expire);
  setText($("trafficPct"), has ? ratio.toFixed(1) + "%" : "--");
}

function sourceLabel() {
  if (!state || !state.source) return "当前来源";
  return state.source.label || (state.source.active === "local" ? "本机客户端" : state.source.active);
}

function renderInto(box, signature, build) {
  // Skip rebuild entirely when content is unchanged; restore scroll when rebuilding.
  if (box.dataset.sig === signature) return false;
  box.dataset.sig = signature;
  const top = box.scrollTop;
  box.innerHTML = "";
  build(box);
  box.scrollTop = top;
  return true;
}

function renderSources() {
  const box = $("sources");
  const items = [{ key: "local", label: "本机客户端" }];
  for (const acc of accounts.accounts || []) items.push({ key: "account:" + acc.id, label: acc.label || acc.email || acc.id, error: acc.error });
  items.push({ key: "manual", label: "手动链接" });
  const active = state && state.source ? state.source.active : "local";
  renderInto(box, JSON.stringify({ items: items.map((i) => [i.key, i.label, i.error || ""]), active }), (b) => {
    for (const item of items) {
      const btn = document.createElement("button");
      btn.textContent = item.label;
      if (item.key === active) btn.classList.add("on");
      if (item.error) btn.title = "异常：" + item.error;
      btn.onclick = () => switchSource(item.key);
      b.appendChild(btn);
    }
  });
  $("manualRow").classList.toggle("gone", active !== "manual");
  if (active === "manual") { $("manualUrl").value = accounts.manualUrl || ""; $("manualLabel").value = accounts.manualLabel || ""; }
  const when = state && state.source && state.source.fetchedAt
    ? new Date(state.source.fetchedAt * 1000).toLocaleString("zh-CN")
    : "尚未拉取";
  const meta = `${sourceLabel()} · ${state && state.source && state.source.cached ? "缓存 · " : ""}上次更新 ${when}`;
  const metaEl = $("sourceMeta");
  setText(metaEl, meta);
  metaEl.title = meta + (state && state.probeError ? ` · ${state.probeError}` : "");
  if (state && state.probeError) setHint("sourceHint", state.probeError);
}

// The URL for the currently selected variant. rawUrl is absent for older servers,
// in which case we fall back to the templated URL rather than showing nothing.
function addressFor(addr) {
  if (!addr) return "";
  if (currentVariant === "raw" && addr.rawUrl) return addr.rawUrl;
  return addr.url || "";
}

function syncVariantTabs() {
  for (const tab of document.querySelectorAll(".variant-tab")) {
    tab.classList.toggle("on", tab.dataset.variant === currentVariant);
    tab.setAttribute("aria-selected", tab.dataset.variant === currentVariant ? "true" : "false");
  }
  const isRaw = currentVariant === "raw";
  setText($("addrLabel"), "当前来源 · " + (isRaw ? "原始订阅地址" : "优化后订阅地址"));
  setText($("qrCaption"), isRaw ? "扫码导入 · 原始" : "扫码导入 · 优化后");
  setText($("variantNote"), isRaw
    ? "原始：服务商下发的配置，未经修改（保留 external-controller / secret 等原字段）。"
    : "优化后：套用 Clash 模板，策略组与规则已重写，移除 external-controller / secret。");
}

function renderAddresses() {
  addresses = (state && state.addresses) || [];
  if (currentAddress >= addresses.length) currentAddress = 0;
  const addr = addresses[currentAddress];
  const chips = $("addrChips");
  renderInto(chips, JSON.stringify({ addresses, current: currentAddress }), (b) => {
    addresses.forEach((a, index) => {
      const btn = document.createElement("button");
      btn.textContent = a.label;
      btn.title = a.label;
      if (index === currentAddress) btn.classList.add("on");
      btn.onclick = () => { currentAddress = index; renderAddresses(); };
      b.appendChild(btn);
    });
  });
  syncVariantTabs();
  const url = addressFor(addr);
  if (!url) {
    $("qr").innerHTML = "";
    $("qr").dataset.url = "";
    setText($("subUrl"), "");
    $("subUrl").title = "";
    return;
  }
  setText($("subUrl"), url);
  $("subUrl").title = url;
  const qr = $("qr");
  if (qr.dataset.url !== url) {
    try {
      qr.dataset.url = url;
      qr.innerHTML = qrSvg(url, { scale: 4, margin: 2, dark: "#0f172a", light: "#ffffff" });
    } catch {
      qr.dataset.url = "";
      qr.innerHTML = "";
    }
  }
}

function renderNodes() {
  const nodes = (state && state.nodeList) || [];
  setText($("nodeCount"), String(nodes.length));
  const sig = JSON.stringify(nodes);
  renderInto($("nodes"), sig, (b) => {
    for (const node of nodes) {
      const el = document.createElement("div");
      el.className = "node";
      el.innerHTML = `<span>${escapeHTML(node.name)}</span><span class="srv">${escapeHTML(node.server || "")}</span>`;
      b.appendChild(el);
    }
  });
}

function renderState() {
  if (!state) return;
  renderTraffic();
  renderSources();
  renderAddresses();
  renderNodes();
  const err = state.probeError || "";
  // Service reachability outranks subscription state: if the last poll failed,
  // the panel is stale and saying "服务正常" would be a lie.
  const stateLabel = offline ? "服务未连接" : err ? "拉取异常" : state.nodes ? "服务正常" : state.pending ? "拉取中" : "待拉取";
  const dotClass = offline || err ? "warn" : state.nodes ? "ok" : "warn";
  $("dot").className = "dot " + dotClass;
  setText($("stateText"), stateLabel);
  $("stateText").title = offline ? `服务未连接：${offline}` : err ? `拉取异常：${err}` : stateLabel;
  // Fixed-height session block, three lines: state / source+cache / last update.
  // Time uses a short human format so the visible line never ellipsizes; the
  // full timestamp (date + time) lives in the title for hover inspection.
  const fetched = state.source && state.source.fetchedAt ? new Date(state.source.fetchedAt * 1000) : null;
  const hhmm = fetched
    ? fetched.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false })
    : null;
  const when = fetched
    ? (fetched.toDateString() === new Date().toDateString() ? hhmm : `${fetched.getMonth() + 1}/${fetched.getDate()} ${hhmm}`)
    : "尚未更新";
  const fullWhen = fetched ? fetched.toLocaleString("zh-CN", { hour12: false }) : "尚未更新";
  const cacheLabel = err ? "更新失败" : state.source && state.source.cached ? "缓存" : "实时";
  const srcLine = offline ? "数据为断开前快照" : `${sourceLabel()} · ${cacheLabel}`;
  setText($("subSource"), srcLine);
  $("subSource").title = offline ? "数据为断开前快照" : `来源：${sourceLabel()} · ${cacheLabel}${err ? " · " + err : ""}`;
  setText($("subStatus"), offline ? fullWhen : `已更新 ${when}`);
  $("subStatus").title = offline ? `服务未连接：${offline}（显示断开前快照）` : `完整时间：${fullWhen} · 来源：${sourceLabel()} · ${cacheLabel}`;
}

async function loadAccounts() {
  accounts = await api("/api/v1/accounts");
  renderSources();
}

async function refresh() {
  try {
    state = await api("/api/v1/state");
    offline = "";
    renderState();
  } catch (err) {
    if (err.message === "需要口令") return;
    // Keep the last good snapshot on screen, but stop claiming the service is up.
    offline = err.message;
    if (state) renderState();
  }
}

async function switchSource(source, force = false) {
  setHint("sourceHint", force ? "正在登录并拉取…" : "正在切换…");
  try {
    const body = { source, refresh: force };
  if (source === "manual") { body.manualUrl = $("manualUrl").value.trim(); body.manualLabel = $("manualLabel").value.trim(); }
    const data = await api("/api/v1/source", { method: "POST", body });
    await loadAccounts();
    await refresh();
    setHint("sourceHint", data.probeError ? "切换成功，但拉取失败：" + data.probeError : "切换成功" + (data.cached ? "（使用缓存）" : "") + "，共 " + data.nodes + " 个节点", !data.probeError);
  } catch (err) {
    setHint("sourceHint", err.message);
  }
}

async function refreshCurrent() {
  setHint("sourceHint", "正在刷新…");
  try {
    const data = await api("/api/v1/refresh", { method: "POST" });
    await refresh();
    setHint("sourceHint", data.probeError ? "刷新失败：" + data.probeError : "刷新成功，共 " + data.nodes + " 个节点", !data.probeError);
  } catch (err) {
    setHint("sourceHint", err.message);
  }
}

function accountDerived(acc) {
  // 状态从 error / active / hasPassword / subscriptionUrl 推导：
  // 错误优先；当前账号其次；有订阅链接是「已保存订阅」，否则待登录/待拉取。
  const on = state && state.source && state.source.active === "account:" + acc.id;
  if (acc.error) return { on, cls: "st-error", text: "错误", title: "出错" };
  if (on) return { on, cls: "st-current", text: "当前", title: "当前订阅来源" };
  if (acc.kind === "link" ? acc.subscriptionUrl : acc.hasPassword || acc.subscriptionUrl)
    return { on, cls: "st-ok", text: "已保存订阅", title: "已保存订阅" };
  return { on, cls: "st-pending", text: "待登录", title: "待登录" };
}

function accountMeta(acc) {
  // 第 2 行文本：错误时替换为错误文案，否则按类型给标识信息。
  if (acc.error) return { text: acc.error, isErr: true };
  if (acc.kind === "link") return { text: acc.subscriptionUrl || "未填写订阅链接", isErr: !acc.subscriptionUrl };
  return { text: [acc.email, acc.apiOrigin].filter(Boolean).join(" · ") || "未填写登录信息", isErr: false };
}

function renderAccounts() {
  const box = $("accList");
  const list = accounts.accounts || [];
  setText($("accCount"), String(list.length));
  const sig = JSON.stringify({
    list: list.map((a) => [a.id, a.kind, a.label, a.email, a.apiOrigin, a.subscriptionUrl, a.error, a.hasPassword]),
    active: state && state.source && state.source.active,
  });
  renderInto(box, sig, (b) => {
    if (!list.length) {
      b.innerHTML = '<div class="acc-empty">暂无账号。在下方添加一个密码账号或链接账号，即可开始拉取订阅。</div>';
      return;
    }
    for (const acc of list) {
      const isPw = acc.kind !== "link";
      const d = accountDerived(acc);
      const meta = accountMeta(acc);
      const name = acc.label || acc.email || (acc.kind === "link" ? "链接账号" : "密码账号") || acc.id;
      const item = document.createElement("div");
      item.className = "accitem " + (isPw ? "pw" : "link") + (d.on ? " current" : "");
      item.innerHTML =
        `<div class="acctop">` +
        `<div class="accname" title="${escapeHTML(name)}">` +
        `<span class="acctype" title="${isPw ? "密码账号" : "链接账号"}">${isPw ? "密" : "链"}</span>` +
        `<span title="${escapeHTML(name)}">${escapeHTML(name)}</span>` +
        (d.on ? '<span class="pill">当前</span>' : "") +
        `</div>` +
        `<span class="accstate ${d.cls}" title="${escapeHTML(d.title)}">${escapeHTML(d.text)}</span>` +
        `</div>` +
        `<div class="accmeta${meta.isErr ? "" : " muted"}" title="${escapeHTML(meta.text)}">` +
        (meta.isErr ? `<span class="err-text">${escapeHTML(meta.text)}</span>` : escapeHTML(meta.text)) +
        `</div>` +
        `<div class="accops">` +
        `<button class="act" ${d.on ? "disabled" : ""} type="button">${d.on ? "当前" : "激活"}</button>` +
        `<button class="edit" type="button">编辑</button>` +
        `<button class="delete" type="button">删除</button>` +
        `</div>`;
      item.querySelector(".act").onclick = () => activateAccount(acc.id, name);
      item.querySelector(".edit").onclick = () => enterEditMode(acc);
      item.querySelector(".delete").onclick = () => deleteAccount(acc.id, name);
      b.appendChild(item);
    }
  });
}

async function activateAccount(id, name) {
  setHint("accHint", "正在激活…", true);
  try {
    const data = await api("/api/v1/accounts", { method: "POST", body: { action: "activate", id } });
    accounts = data;
    await refresh();
    renderAccounts();
    renderState();
    setHint("accHint", "已切换当前来源：" + name, true);
  } catch (err) {
    setHint("accHint", err.message);
  }
}

// 自写确认弹窗：原生 confirm 无法套用页面视觉体系。Escape/取消/遮罩=取消，
// Enter/确认=确认。返回 Promise<boolean>。
function askConfirm({ title, body, confirmText = "确认", danger = false }) {
  return new Promise((resolve) => {
    const mask = document.createElement("div");
    mask.className = "modal-mask";
    mask.innerHTML =
      `<div class="modal" role="dialog" aria-modal="true" aria-labelledby="askConfirmTitle">` +
      `<div class="modal-title" id="askConfirmTitle"></div>` +
      `<div class="modal-body"></div>` +
      `<div class="modal-actions">` +
      `<button class="modal-cancel" type="button">取消</button>` +
      `<button class="modal-ok${danger ? " danger" : ""}" type="button"></button>` +
      `</div></div>`;
    mask.querySelector(".modal-title").textContent = title;
    mask.querySelector(".modal-body").textContent = body;
    mask.querySelector(".modal-ok").textContent = confirmText;
    const close = (result) => {
      document.removeEventListener("keydown", onKey);
      mask.remove();
      resolve(result);
    };
    const onKey = (e) => {
      if (e.key === "Escape") close(false);
      if (e.key === "Enter") close(true);
    };
    mask.querySelector(".modal-cancel").onclick = () => close(false);
    mask.querySelector(".modal-ok").onclick = () => close(true);
    mask.addEventListener("mousedown", (e) => { if (e.target === mask) close(false); });
    document.addEventListener("keydown", onKey);
    document.body.appendChild(mask);
    mask.querySelector(".modal-ok").focus();
  });
}

async function deleteAccount(id, name) {
  const ok = await askConfirm({
    title: "删除账号",
    body: `将删除「${name}」该账号、它的缓存和专属订阅链接。此操作不可撤销。`,
    confirmText: "删除",
    danger: true,
  });
  if (!ok) return;
  try {
    const data = await api("/api/v1/accounts", { method: "POST", body: { action: "delete", id } });
    accounts = data;
    if (editingId === id) exitEditMode();
    await refresh();
    renderAccounts();
    renderState();
    setHint("accHint", "已删除：" + name, true);
  } catch (err) {
    setHint("accHint", err.message);
  }
}

// ---- 账号表单：类型切换 / 新增 / 编辑 / 仅验证 ----

let formType = "pw"; // "pw" | "link"
let editingId = null; // null = 新增

function setFormType(t) {
  const pw = t === "pw";
  if (editingId && pw !== formType) {
    // 编辑态下账号类型不可修改：切 tab 允许看，但保存会变成新建。
    setHint("accFormHint", "正在编辑：" + editingLabel() + "（类型不可修改，切换类型将新建账号）");
  }
  formType = t;
  $("tabPw").classList.toggle("on", pw);
  $("tabLink").classList.toggle("on", !pw);
  $("secPwFields").classList.toggle("gone", !pw);
  $("secLinkFields").classList.toggle("gone", pw);
  $("btnAccTest").classList.toggle("hidden", !pw);
  $("verifyNote").classList.toggle("hidden", !pw);
  setText($("typeNote"), pw ? "通过 API 域名 + 邮箱 + 密码登录，自动换取订阅。" : "只需要一条订阅链接，直接拉取，无密码。");
  if (!editingId) setHint("accFormHint", "");
}

function editingLabel() {
  const acc = (accounts.accounts || []).find((a) => a.id === editingId);
  if (!acc) return "";
  return acc.label || acc.email || acc.id;
}

function enterEditMode(acc) {
  editingId = acc.id;
  formType = acc.kind === "link" ? "link" : "pw";
  $("formTitle").textContent = "编辑账号";
  $("btnAccSave").textContent = "保存修改";
  $("btnCancelEdit").classList.remove("hidden");
  $("tabPw").classList.toggle("on", formType === "pw");
  $("tabLink").classList.toggle("on", formType !== "pw");
  $("secPwFields").classList.toggle("gone", formType !== "pw");
  $("secLinkFields").classList.toggle("gone", formType === "pw");
  $("btnAccTest").classList.toggle("hidden", formType !== "pw");
  $("verifyNote").classList.toggle("hidden", formType !== "pw");
  setText($("typeNote"), formType === "pw" ? "通过 API 域名 + 邮箱 + 密码登录，自动换取订阅。" : "只需要一条订阅链接，直接拉取，无密码。");
  $("accLabel").value = acc.label || "";
  $("accOrigin").value = acc.apiOrigin || "";
  $("accEmail").value = acc.email || "";
  $("accPassword").value = "";
  $("accPassword").placeholder = "留空则不修改";
  $("accSubUrl").value = acc.subscriptionUrl || "";
  $("pwNote").classList.remove("hidden");
  setHint("accFormHint", "正在编辑：" + (acc.label || acc.email || acc.id) + (formType === "pw" ? "（密码账号）" : "（链接账号）"), true);
}

function exitEditMode() {
  editingId = null;
  $("formTitle").textContent = "新增账号";
  $("btnAccSave").textContent = "保存账号";
  $("btnCancelEdit").classList.add("hidden");
  $("accPassword").placeholder = "必填";
  $("pwNote").classList.add("hidden");
  for (const id of ["accLabel", "accOrigin", "accEmail", "accPassword", "accSubUrl"]) $(id).value = "";
  setHint("accFormHint", "");
  setFormType(formType);
}

async function verifyAccount() {
  const payload = {
    apiOrigin: $("accOrigin").value.trim(),
    email: $("accEmail").value.trim(),
    password: $("accPassword").value,
  };
  // API 域名可留空：后端会回退到默认面板域名，现有账号的 apiOrigin 也是空的。
  // 这里若强制必填，会与后端校验不一致，导致这类账号无法验证或保存。
  if (!payload.email) return setHint("accFormHint", "请填写邮箱");
  if (!payload.password) return setHint("accFormHint", "请填写密码");
  setHint("accFormHint", "正在验证…", true);
  try {
    await api("/api/v1/account", { method: "POST", body: payload });
    setHint("accFormHint", "验证成功：登录有效，未保存到列表", true);
  } catch (err) {
    setHint("accFormHint", err.message);
  }
}

async function saveAccount() {
  const isPw = formType === "pw";
  const payload = { kind: isPw ? "password" : "link" };
  payload.label = $("accLabel").value.trim();
  if (editingId) payload.id = editingId;
  if (isPw) {
    payload.apiOrigin = $("accOrigin").value.trim();
    payload.email = $("accEmail").value.trim();
    payload.password = $("accPassword").value;
    // API 域名可留空（后端回退到默认面板域名）；邮箱必填。
    if (!payload.email) return setHint("accFormHint", "请填写邮箱");
    // 编辑时留空 = 不修改密码；新增时必填。
    if (!editingId && !payload.password) return setHint("accFormHint", "请填写密码");
  } else {
    payload.subscriptionUrl = $("accSubUrl").value.trim();
    if (!payload.subscriptionUrl) return setHint("accFormHint", "请填写订阅链接");
  }
  setHint("accFormHint", editingId ? "正在保存…" : "正在保存…", true);
  try {
    const data = await api("/api/v1/accounts", { method: "POST", body: payload });
    accounts = data;
    await refresh();
    renderAccounts();
    renderState();
    if (editingId) {
      const acc = (data.accounts || []).find((a) => a.id === editingId);
      if (acc) { enterEditMode(acc); } else { exitEditMode(); }
      setHint("accFormHint", "修改已保存", true);
    } else {
      exitEditMode();
      setHint("accFormHint", "保存成功", true);
    }
  } catch (err) {
    setHint("accFormHint", err.message);
  }
}

async function loadConfig() {
  const cfg = await api("/api/v1/config");
  $("cfgHost").value = cfg.host;
  $("cfgPort").value = cfg.port;
  $("cfgKey").value = cfg.accessKey || "";
  $("cfgDomains").value = (cfg.domains || []).join(", ");
}

// 网络可达性自检：只读，不会改动防火墙。结果用于提示「局域网为什么连不上」。
async function loadNetDiag() {
  const box = $("netDiagBody");
  setText(box, "检测中…");
  try {
    const d = await api("/api/v1/net-diag");
    const fw = d.firewall || {};
    const profiles = (fw.profilesEnabled || []).join(" / ") || "无（防火墙已关闭）";
    const rows = [
      ["监听地址", d.listenHost === "0.0.0.0" ? "所有网卡（局域网 + 本机）" : "仅本机"],
      ["端口", String(d.port)],
      ["局域网地址", (d.lan || []).join(" / ") || "未检测到"],
      ["防火墙", fw.supported ? profiles : "当前平台不支持自检"],
      ["面板规则", fw.supported ? (fw.ruleFound ? "已存在" : "不存在") : "—"],
    ];
    box.innerHTML = rows
      .map(([k, v]) => `<div class="netdiag-row"><span class="k">${escapeHTML(k)}</span><span class="v" title="${escapeHTML(v)}">${escapeHTML(v)}</span></div>`)
      .join("");
    const note = document.createElement("div");
    note.className = "netdiag-note " + (d.reachable ? "ok" : "warn");
    note.textContent = d.hint || "";
    box.appendChild(note);
  } catch (err) {
    setText(box, err.message);
  }
}

async function saveConfig() {
  const body = {
    host: $("cfgHost").value,
    port: Number($("cfgPort").value),
    accessKey: $("cfgKey").value,
    domains: $("cfgDomains").value.split(/[\n,]/).map((x) => x.trim()).filter(Boolean),
  };
  try {
    const data = await api("/api/v1/config", { method: "POST", body });
    setHint("cfgHint", data.restartRequired ? "保存成功，端口变更需重启服务生效" : "保存成功", true);
  } catch (err) { setHint("cfgHint", err.message); }
}

async function showLogs() {
  try {
    const data = await api("/api/v1/logs?lines=200");
    $("logText").textContent = data.text || "(空)";
    $("logCard").classList.remove("hidden");
  } catch (err) { setHint("cfgHint", err.message); }
}

// 结果信息头：节点数 / 行数 / 字节数。节点数来自后端解析（与订阅页同一套逻辑），
// 前端不能用正则数 `- `：这份模板同时会输出块风格 `- name:` 和 flow 风格 `- {"name":...}`。
function yamlMeta(text, nodes) {
  if (!text) return "";
  const lines = text.split("\n").length;
  return `${Number.isFinite(nodes) ? nodes : 0} 节点 · ${lines} 行 · ${formatBytes(new Blob([text]).size)}`;
}

async function decryptTool() {
  const input = $("toolInput").value.trim();
  if (!input) return setHint("toolHint", "请先粘贴订阅链接");
  setHint("toolHint", "正在解密…", true);
  try {
    const data = await api("/api/v1/decrypt", { method: "POST", body: { input } });
    const yaml = data.yaml || "";
    const raw = data.rawYaml || "";
    $("toolYaml").value = yaml;
    $("rawYaml").value = raw;
    for (const [id, text, nodes] of [["toolMeta", yaml, data.nodes], ["rawMeta", raw, data.rawNodes]]) {
      const meta = yamlMeta(text, nodes);
      setText($(id), meta);
      $(id).title = meta;
    }
    $("toolResult").classList.remove("hidden");
    $("toolYaml").scrollTop = 0;
    $("rawYaml").scrollTop = 0;
    // Same content in both panes means the source was already templated (e.g. the
    // panel's own /sub). Say so instead of showing two identical boxes.
    const same = raw && yaml === raw;
    setHint("toolHint", same ? "解密成功（原始订阅已是模板格式，两份内容一致）" : "解密成功", true);
  } catch (err) {
    // 失败时不展示结果卡，不留空的大框。
    $("toolResult").classList.add("hidden");
    $("toolYaml").value = "";
    $("rawYaml").value = "";
    setText($("toolMeta"), "");
    setText($("rawMeta"), "");
    setHint("toolHint", err.message);
  }
}

async function login(key, remember) {
  await api("/api/v1/login", { method: "POST", body: { key, remember } });
  await boot();
}

async function boot() {
  try {
    state = await api("/api/v1/state");
    showApp();
    await loadAccounts();
    renderAccounts();
    renderState();
    setInterval(refresh, 5000);
  } catch (err) {
    if (err.message === "需要口令") showLogin();
  }
}

// ------------------------------------------------------------------ bindings

$("loginForm").onsubmit = async (event) => {
  event.preventDefault();
  try {
    await login($("loginKey").value, $("loginRemember").checked);
  } catch (err) {
    setHint("loginHint", err.message);
  }
};

$("logout").onclick = async (event) => {
  event.preventDefault();
  await fetch("/api/v1/logout", { method: "POST" });
  location.reload();
};

for (const btn of document.querySelectorAll("[data-sec]")) {
  btn.onclick = () => {
    document.querySelectorAll(".nav-item").forEach((x) => x.classList.remove("active"));
    document.querySelectorAll(".sec").forEach((x) => x.classList.remove("active"));
    btn.classList.add("active");
    $("sec-" + btn.dataset.sec).classList.add("active");
    if (btn.dataset.sec === "accounts") { loadAccounts().then(renderAccounts).catch(() => {}); }
    if (btn.dataset.sec === "adv") { loadConfig().catch((e) => setHint("cfgHint", e.message)); loadNetDiag(); }
  };
}

$("btnRefresh").onclick = refreshCurrent;
$("btnResetKey").onclick = async () => {
  if (!confirm("重置当前来源的订阅链接？旧二维码和旧链接会立即失效。")) return;
  try {
    await api("/api/v1/sub-key", { method: "POST", body: { source: state.source.active } });
    await refresh();
    setHint("sourceHint", "链接已重置，请重新扫码或复制", true);
  } catch (err) { setHint("sourceHint", err.message); }
};
$("btnManual").onclick = () => switchSource("manual");
for (const tab of document.querySelectorAll(".variant-tab")) {
  tab.onclick = () => {
    if (tab.dataset.variant === currentVariant) return;
    currentVariant = tab.dataset.variant === "raw" ? "raw" : "optimized";
    localStorage.setItem("fl.variant", currentVariant);
    // Re-render only the address card: the QR is regenerated because the URL
    // changed, everything else on the page stays untouched.
    renderAddresses();
  };
}
$("btnCopy").onclick = async () => {
  const text = $("subUrl").textContent;
  const label = currentVariant === "raw" ? "原始订阅链接" : "订阅链接";
  try { await navigator.clipboard.writeText(text); setHint("sourceHint", `${label}已复制`, true); }
  catch { setHint("sourceHint", "复制失败，请手动选择链接"); }
};
$("btnOpen").onclick = () => window.open($("subUrl").textContent, "_blank");
$("btnDownload").onclick = () => { location.href = $("subUrl").textContent; };
$("btnCopyNodes").onclick = async () => {
  const text = ((state && state.nodeList) || []).map((n) => n.name + (n.server ? "  " + n.server : "")).join("\n");
  try { await navigator.clipboard.writeText(text || "暂无节点"); setHint("sourceHint", "节点列表已复制", true); } catch { setHint("sourceHint", "复制失败"); }
};
$("tabPw").onclick = () => setFormType("pw");
$("tabLink").onclick = () => setFormType("link");
$("btnAccSave").onclick = saveAccount;
$("btnAccTest").onclick = verifyAccount;
$("btnCancelEdit").onclick = () => { exitEditMode(); };
$("btnSaveCfg").onclick = saveConfig;
$("btnLogs").onclick = showLogs;
$("btnNetDiag").onclick = loadNetDiag;
$("btnQuit").onclick = async () => {
  if (!confirm("退出 FastLink-tool 服务？")) return;
  await fetch("/api/v1/quit", { method: "POST" });
  document.body.innerHTML = '<div class="login"><div class="card">服务已退出</div></div>';
};
$("btnToolLocal").onclick = async () => {
  try {
    const data = await api("/api/v1/local-sub");
    $("toolInput").value = data.url || "";
    setHint("toolHint", "已填入本机客户端订阅", true);
  } catch (err) { setHint("toolHint", err.message); }
};
$("btnToolRun").onclick = decryptTool;
const copyPane = async (id, label) => {
  const text = $(id).value;
  if (!text) return setHint("toolHint", "没有可复制的内容");
  try {
    await navigator.clipboard.writeText(text);
    setHint("toolHint", `${label}已复制（${formatBytes(new Blob([text]).size)}）`, true);
  } catch { setHint("toolHint", "复制失败，请手动选择文本"); }
};
$("btnToolCopy").onclick = () => copyPane("toolYaml", "YAML ");
$("btnRawCopy").onclick = () => copyPane("rawYaml", "原始订阅 ");
$("toolInput").addEventListener("keydown", (event) => {
  if (event.key === "Enter" && !event.shiftKey && !event.ctrlKey && !event.metaKey) {
    event.preventDefault();
    decryptTool();
  }
});

// Support ?key= for quick panel login.
(async () => {
  const key = new URLSearchParams(location.search).get("key");
  if (key) {
    try {
      await login(key, true);
      history.replaceState(null, "", location.pathname);
      return;
    } catch (err) {
      setHint("loginHint", err.message);
    }
  }
  await boot();
})();
