// 界面交互回归测试：用 jsdom 加载 web/ 下的真实页面 + 真实脚本，
// 桩掉 fetch 后逐项验证主题切换、AI 浮层、表头全选、拖拽归类。
// 运行：node ui_dom_test.mjs（依赖 devDependencies 里的 jsdom，npm ci 即可）
import { JSDOM } from "jsdom";
import fs from "node:fs";

// 读 web/ 下的源码并做与 gui_ui.go 相同的占位符替换，保证测的就是要发布的页面。
// 用 split/join 而不是 replace，避免 CSS/JS 里的 $ 被当成替换模式。
const asset = f => fs.readFileSync(new URL("./web/" + f, import.meta.url), "utf8");
const html = asset("index.html")
  .split("{{CSS}}").join(asset("app.css"))
  .split("{{JS}}").join(asset("app.js"));
if (html.includes("{{CSS}}") || html.includes("{{JS}}")) {
  console.error("web/index.html 的占位符没有被替换"); process.exit(2);
}

const server = { theme: "light", ai_url: "https://saved.example.com/v1", model: "saved-model" };

function makeApi(state) {
  return function fetchStub(url, opts = {}) {
    const method = (opts.method || "GET").toUpperCase();
    let body = {};
    try { body = opts.body ? JSON.parse(opts.body) : {}; } catch (e) {}
    let payload;
    if (url === "/api/ui-settings") {
      if (method === "GET") payload = { ...server };
      else {
        for (const k of ["theme", "ai_url", "model"]) if (k in body) server[k] = body[k];
        payload = { ok: true, settings: { ...server } };
      }
    } else if (url === "/api/state") {
      payload = state.stateData;
    } else if (url === "/api/history") {
      payload = { ok: true, items: [] };
    } else {
      payload = { ok: true };
    }
    return Promise.resolve({ json: () => Promise.resolve(payload) });
  };
}

function stateData() {
  const games = [];
  for (let i = 1; i <= 5; i++) {
    games.push({ appid: 1000 + i, name: "游戏" + i, target: "未分类", priority: 6 });
  }
  return {
    connected: true,
    startup_done: true,
    history_dir: "X:/hist",
    manifest: { schema: 2, game_count: games.length, games, collection_order: ["主系列"] },
    snapshot: {
      account: "123", apps: games.map(g => ({ appid: g.appid, name: g.name, last_played: 0, playtime_minutes: 10 })),
      collections: [{ id: "x", name: "主系列", dynamic: false, system: false, appids: [] }],
    },
    prices: {},
  };
}

(async () => {
  const dom = new JSDOM(html, {
    runScripts: "dangerously",
    url: "http://127.0.0.1:1/",
    pretendToBeVisual: true,
    beforeParse(window) {
      const sd = stateData();
      window.fetch = makeApi({ stateData: sd });
      window.confirm = () => true;
      window.stateData = sd;
      window.jsdomError = [];
      window.addEventListener("error", e => window.jsdomError.push(String(e.message)));
    },
  });
  const { window } = dom;
  const doc = window.document;
  // startup() 里 loadState 后有轮询，等它稳定
  await new Promise(r => setTimeout(r, 300));
  const errors = () => window.jsdomError || [];
  const results = [];
  const check = (name, cond) => results.push([cond ? "PASS" : "FAIL", name]);

  // ---- 1) 主题：默认浅色（codex 行为）→ toggleTheme 变深色并 POST 落盘 ----
  check("初始 data-theme=light", doc.documentElement.getAttribute("data-theme") === "light");
  check("主题按钮存在", !!doc.querySelector("#themeToggle"));
  window.toggleTheme();
  check("点击后变 dark", doc.documentElement.getAttribute("data-theme") === "dark");
  check("按钮图标换成 🌙", doc.querySelector("#themeToggle").textContent === "🌙");
  await new Promise(r => setTimeout(r, 20));
  check("主题已 POST 到服务端", server.theme === "dark");
  window.toggleTheme();
  check("再点回 light", doc.documentElement.getAttribute("data-theme") === "light");
  server.theme = "light";

  // ---- 2) AI 浮层 ----
  const panel = doc.querySelector("#aiPanel");
  check("AI 浮层存在且默认收起", !!panel && panel.open === false);
  check("工具栏有 ✨ AI 整理 入口", [...doc.querySelectorAll(".toolbar button")].some(b => b.textContent.includes("AI 整理")));
  check("AI 地址已从服务端回填", doc.querySelector("#aiurl").value === "https://saved.example.com/v1");
  window.toggleAI();
  check("toggleAI 后展开", panel.open === true);
  window.toggleAI();
  check("再调一次收起", panel.open === false);

  // ---- 3) 表头全选 ----
  const all = doc.querySelector("#selectAll");
  check("表头全选存在", !!all);
  check("初始未选", all.checked === false && all.indeterminate === false);
  const boxes = doc.querySelectorAll("#games input[type=checkbox]");
  boxes.forEach((cb, i) => { cb.checked = i < 2; cb.dispatchEvent(new window.Event("change")); });
  check("勾选 2 行后 selectionCount 更新", doc.querySelector("#selectionCount").textContent.includes("已选 2"));
  check("部分勾选时全选呈半选态", all.indeterminate === true);
  all.checked = true; all.dispatchEvent(new window.Event("change"));
  check("点全选后计数=5", doc.querySelector("#selectionCount").textContent.includes("已选 5"));
  check("半选态清除", all.indeterminate === false);
  all.checked = false; all.dispatchEvent(new window.Event("change"));
  check("取消全选后计数=0", doc.querySelector("#selectionCount").textContent.includes("已选 0"));

  // ---- 4) 拖拽：行可拖；收藏行接到 drop 后调用 applyTargetTo ----
  const row = doc.querySelector("#games tr");
  check("游戏行 draggable=true", row && row.draggable === true);
  const dropRow = doc.querySelector("#staticTree .collection-row");
  check("静态收藏行存在", !!dropRow);
  // 模拟拖放：先勾一行，再对收藏行派发 drop
  doc.querySelectorAll("#games input[type=checkbox]").forEach((cb, i) => { if (i < 2) { cb.checked = true; cb.dispatchEvent(new window.Event("change")); } });
  const dt = { getData: () => "1001", preventDefault() {}, dropEffect: "move" };
  const ev = new window.Event("drop", { bubbles: true, cancelable: true });
  Object.defineProperty(ev, "dataTransfer", { value: dt });
  dropRow.dispatchEvent(ev);
  await new Promise(r => setTimeout(r, 50));
  const targets = [...doc.querySelectorAll("#games tr")].map(tr => tr.children[2].textContent).join("|");
  check("drop 后两行目标变为「主系列」", targets === "主系列|主系列|未分类|未分类|未分类");
  check("保存后自动切到待保存方案", doc.querySelector("#collectionMeta").textContent.includes("待保存方案"));
  // 拖到动态收藏名应被拒绝
  window.selected = ""; window.browsingActual = true; window.renderAll();

  // ---- 5) codex 布局：导航按钮 / 标题栏 / 筛选列 / 保存按钮位置 ----
  const navText = [...doc.querySelectorAll(".codex-nav button")].map(b => b.textContent.trim()).join("|");
  check("导航按钮顺序正确", navText === "游戏库|历史|连接诊断|刷新列表|退出工具");
  check("导航里没有「重新读取 Steam」", !navText.includes("重新读取"));
  check("导航里没有「生成预览」", !navText.includes("生成预览"));
  const headBar = doc.querySelector(".codex-main-heading");
  check("标题栏顺序：标题→AI→已选→移除→价格", (() => {
    const parts = [...headBar.children].filter(e => e.tagName === "H2" || e.tagName === "BUTTON" || e.id === "selectionCount")
      .map(e => e.id || e.textContent.trim());
    return JSON.stringify(parts) === JSON.stringify(["collectionTitle", "aiToggle", "selectionCount", "移除游戏", "获取商店资料和价格"]);
  })());
  check("移除游戏与获取价格在标题栏右侧", [...headBar.querySelectorAll("button")].map(b => b.textContent.trim()).join("|") === "✨ AI 整理|移除游戏|获取商店资料和价格");
  check("确认保存收藏在左侧收藏面板", !!doc.querySelector("#collectionHead > button"));
  check("左侧有 ⓘ 提示", (doc.querySelector("#collectionHint") || {}).textContent === "ⓘ");
  check("视图切换在左栏顶部且文案为 codex 版", (() => {
    const vs = doc.querySelector("aside .view-switch");
    return vs && [...vs.querySelectorAll("button")].map(b => b.textContent.trim()).join("|") === "当前收藏|待保存";
  })());
  check("筛选列包含 搜索/🔍按钮/排序", (() => {
    const col = doc.querySelector("#filterColumn");
    return col && col.querySelector("#search") && col.querySelector("#searchToggle").textContent.includes("🔍") && col.querySelector("#gameSort").textContent.includes("按名称");
  })());
  check("排序只剩「按名称」", doc.querySelector("#gameSort").textContent.trim() === "按名称");
  check("连接诊断弹窗存在", !!doc.querySelector("#diagnosticsDialog"));
  check("主题按钮固定在页面右上（不在 .top 内）", !doc.querySelector(".top #themeToggle") && !!doc.querySelector("#themeSwitcher #themeToggle"));
  check("预览区默认隐藏", doc.querySelector("#preview").closest("section").classList.contains("hidden"));
  check("历史标题就是「历史」", doc.querySelector("#historyView h2").textContent === "历史");
  check("树首行是「全部游戏 · N款」", doc.querySelector("#dynamicTree button").textContent.includes("全部游戏 · 5款"));

  check("无未捕获 JS 错误", errors().length === 0);
  if (errors().length) console.log("JS errors:", errors());

  let failed = 0;
  for (const [s, name] of results) {
    console.log(s, name);
    if (s === "FAIL") failed++;
  }
  console.log(failed ? `\n${failed} 项失败` : "\n全部通过");
  process.exit(failed ? 1 : 0);
})().catch(e => { console.error("测试崩溃:", e); process.exit(2); });
