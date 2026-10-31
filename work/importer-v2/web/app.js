let browsingActual = true;
let stateData = null,
  selected = '',
  visibleGames = [],
  pageNo = 0,
  pageSize = 50,
  reviewWindow = null,
  pendingDraft = null;
async function api(path, body) {
  let r = await fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  return await r.json();
}
function status(msg, ok = true) {
  let el = document.querySelector('#status');
  el.textContent = msg;
  el.className = 'status ' + (ok ? 'ok' : 'bad');
}
let uiPrefs = { theme: 'dark', ai_url: '', model: '' };
function applyTheme(t) {
  t = t === 'light' ? 'light' : 'dark';
  document.documentElement.setAttribute('data-theme', t);
  try {
    localStorage.setItem('sci-theme', t);
  } catch (e) {}
  let b = document.querySelector('#themeToggle');
  if (b) {
    b.textContent = t === 'light' ? '☀️' : '🌙';
    let tip = t === 'light' ? '切换到黑夜模式' : '切换到白天模式';
    b.title = tip;
    b.setAttribute('aria-label', tip);
  }
}
function toggleTheme() {
  let next =
    document.documentElement.getAttribute('data-theme') === 'light'
      ? 'dark'
      : 'light';
  applyTheme(next);
  uiPrefs.theme = next;
  savePrefs({ theme: next });
}
function toggleAI() {
  let p = document.querySelector('#aiPanel');
  if (!p) return;
  p.open = !p.open;
  if (p.open) {
    let f = p.querySelector('#aiurl');
    if (f && !f.value) f.focus();
  }
}
function savePrefs(patch) {
  return api('/api/ui-settings', patch).catch(() => {});
}
async function loadPrefs() {
  try {
    let s = await api('/api/ui-settings');
    if (s && s.theme) {
      uiPrefs.theme = s.theme;
      applyTheme(s.theme);
    }
    if (s && s.ai_url) document.querySelector('#aiurl').value = s.ai_url;
    if (s && s.model) uiPrefs.model = s.model;
  } catch (e) {}
  let u = document.querySelector('#aiurl');
  if (u)
    u.addEventListener('change', () => {
      uiPrefs.ai_url = u.value.trim();
      savePrefs({ ai_url: uiPrefs.ai_url });
    });
  let m = document.querySelector('#model');
  if (m)
    m.addEventListener('change', () => {
      uiPrefs.model = m.value;
      savePrefs({ model: uiPrefs.model });
    });
  let all = document.querySelector('#selectAll');
  if (all) all.addEventListener('change', () => toggleSelectAll(all.checked));
}
function nameList() {
  if (browsingActual && stateData.snapshot?.collections) {
    let names = stateData.snapshot.collections
        .filter((c) => !c.dynamic && (!c.system || c.id === 'favorite'))
        .map((c) => c.name),
      order = stateData.manifest.collection_order || [];
    return [
      ...order.filter((n) => names.includes(n)),
      ...names.filter((n) => !order.includes(n)),
    ];
  }
  let order = stateData.manifest.collection_order || [];
  let names = [
    ...new Set((stateData.manifest.games || []).flatMap((g) => planTargets(g))),
  ];
  return [
    ...order.filter((n) => names.includes(n)),
    ...names.filter((n) => !order.includes(n)).sort(),
  ];
}
function dynamicCollections() {
  return (stateData.snapshot?.collections || []).filter(
    (c) => c.dynamic && !c.system,
  );
}
function isDynamic(n) {
  return dynamicCollections().some((c) => c.name === n);
}
function renderTree() {
  let dyn = document.querySelector('#dynamicTree'),
    stat = document.querySelector('#staticTree');
  dyn.innerHTML = '';
  function add(label, key, count) {
    let b = document.createElement('button');
    b.textContent = label + (count === undefined ? '' : ' · ' + count);
    b.className = selected === key ? 'active' : '';
    b.onclick = () => {
      selected = key;
      pageNo = 0;
      renderAll();
    };
    dyn.appendChild(b);
  }
  add('全部游戏', '');
  add('未分类', '@uncategorized', uncategorizedIDs().size);
  add('无静态收藏', '@no-static', noStaticIDs().size);
  for (let c of dynamicCollections())
    add('◈ ' + c.name, c.name, (c.appids || []).length);
  stat.innerHTML = '<div class="group">静态收藏 · ↑↓调整显示和处理顺序</div>';
  let names = nameList().filter((n) => n !== '未分类');
  for (let [i, n] of names.entries()) {
    let row = document.createElement('div');
    row.className = 'collection-row';
    let b = document.createElement('button');
    b.style.flex = '1';
    b.className = selected === n ? 'active' : '';
    let count = browsingActual
      ? (
          (stateData.snapshot?.collections || []).find((c) => c.name === n)
            ?.appids || []
        ).length
      : planCounts.get(n) || 0;
    b.textContent = n + ' · ' + count;
    b.onclick = () => {
      selected = n;
      pageNo = 0;
      renderAll();
    };
    let up = document.createElement('button'),
      dn = document.createElement('button');
    up.textContent = '↑';
    dn.textContent = '↓';
    up.title = '上移';
    dn.title = '下移';
    up.className = dn.className = 'move';
    up.onclick = () => moveCollection(i, -1);
    dn.onclick = () => moveCollection(i, 1);
    row.append(b, up, dn);
    row.addEventListener('dragover', (e) => {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'move';
      row.classList.add('drop-target');
    });
    row.addEventListener('dragleave', () =>
      row.classList.remove('drop-target'),
    );
    row.addEventListener('drop', (e) => {
      e.preventDefault();
      row.classList.remove('drop-target');
      let id = Number(e.dataTransfer.getData('text/plain'));
      if (id && !checkedGames.size) {
        checkedGames.add(id);
        updateSelectionCount();
      }
      applyTargetTo(n);
    });
    stat.appendChild(row);
  }
}
function noStaticIDs() {
  let ids = new Set((stateData.snapshot?.apps || []).map((a) => a.appid));
  if (!browsingActual) {
    for (let g of stateData.manifest.games)
      if (planTargets(g).length) ids.delete(g.appid);
    return ids;
  }
  for (let c of stateData.snapshot?.collections || [])
    if (!c.dynamic && (!c.system || c.id === 'favorite'))
      for (let id of c.appids || []) ids.delete(id);
  return ids;
}
function uncategorizedIDs() {
  if (browsingActual) {
    let c = (stateData.snapshot?.collections || []).find(
      (c) => c.id === 'uncategorized',
    );
    if (c) return new Set(c.appids || []);
  }
  let ids = noStaticIDs();
  for (let c of dynamicCollections())
    for (let id of c.appids || []) ids.delete(id);
  return ids;
}
function moveCollection(i, d) {
  let a = nameList(),
    j = i + d;
  if (j < 0 || j >= a.length) return;
  [a[i], a[j]] = [a[j], a[i]];
  browsingActual = false;
  stateData.manifest.collection_order = a;
  renderAll();
  saveManifest(false);
}
let planMemberships = new Map(),
  planCounts = new Map(),
  ruleIndex = new Map();
function indexPlan() {
  planMemberships = new Map();
  planCounts = new Map();
  ruleIndex = new Map(
    (stateData.manifest.games || []).map((g) => [g.appid, g]),
  );
  if (stateData.manifest.collection_state) {
    for (let c of stateData.manifest.collection_state.collections || [])
      if (!c.dynamic && (!c.system || c.id === 'favorite')) {
        planCounts.set(c.name, (c.appids || []).length);
        for (let id of c.appids || []) {
          if (!planMemberships.has(id)) planMemberships.set(id, []);
          planMemberships.get(id).push(c.name);
        }
      }
  } else
    for (let g of stateData.manifest.games) {
      let names = g.target && g.target !== '未分类' ? [g.target] : [];
      planMemberships.set(g.appid, names);
      for (let n of names) planCounts.set(n, (planCounts.get(n) || 0) + 1);
    }
}
function planTargets(g) {
  return planMemberships.get(g.appid) || [];
}
function renderAll() {
  indexPlan();
  renderTree();
  renderPublisherFilter();
  renderGames();
}
let checkedGames = new Set(),
  removalPlan = null;
function publisherNames(g) {
  let p = stateData.prices?.[g.appid];
  return p?.publishers?.length
    ? p.publishers
    : g.publishers || ruleIndex.get(g.appid)?.publishers || [];
}
function renderPublisherFilter() {
  let el = document.querySelector('#publisherFilter');
  if (!el) return;
  let selectedPublisher = el.value,
    names = [...new Set(stateData.manifest.games.flatMap(publisherNames))].sort(
      (a, b) => a.localeCompare(b),
    );
  el.innerHTML =
    '<option value="">全部发行厂商</option><option value="@unknown">厂商待查询</option>' +
    names
      .map((n) => '<option value="' + esc(n) + '">' + esc(n) + '</option>')
      .join('');
  if ([...el.options].some((x) => x.value === selectedPublisher))
    el.value = selectedPublisher;
}
function playedText(t) {
  return t > 0 ? new Date(t * 1000).toLocaleString() : '无记录';
}
function durationText(n) {
  return n === undefined || n === null
    ? '未提供'
    : n >= 60
      ? (n / 60).toFixed(1) + ' 小时'
      : n + ' 分钟';
}
function syncSelectAll() {
  let b = document.querySelector('#selectAll');
  if (!b) return;
  let ids = visibleGames.map((g) => g.appid);
  b.checked = ids.length > 0 && ids.every((id) => checkedGames.has(id));
  b.indeterminate = !b.checked && ids.some((id) => checkedGames.has(id));
}
function updateSelectionCount() {
  let el = document.querySelector('#selectionCount');
  if (el) el.textContent = '已选 ' + checkedGames.size + ' 款（跨页保留）';
  syncSelectAll();
}
function toggleSelectAll(on) {
  for (let g of visibleGames) {
    if (on) checkedGames.add(g.appid);
    else checkedGames.delete(g.appid);
  }
  renderGames();
}
function priceText(p) {
  if (!p) return '<span class="subtle">未查询</span>';
  if (p.error) return '<span class="subtle">待核实</span>';
  if (p.free) return '<span class="free">免费</span>';
  if (p.final_cny)
    return (
      '<span class="price">¥' +
      p.final_cny.toFixed(2) +
      (p.discount_percent ? ' (-' + p.discount_percent + '%)' : '') +
      '</span>'
    );
  return '<span class="subtle">无报价</span>';
}
function renderGames() {
  if (!stateData) return;
  let q = (document.querySelector('#search').value || '').toLowerCase(),
    dynamic = isDynamic(selected),
    actual = browsingActual,
    appMap = new Map((stateData.snapshot?.apps || []).map((a) => [a.appid, a])),
    members = new Set(
      (
        (stateData.snapshot?.collections || []).find(
          (c) => c.name === selected,
        ) || {}
      ).appids || [],
    );
  if (selected === '@uncategorized') members = uncategorizedIDs();
  if (selected === '@no-static') members = noStaticIDs();
  let source = actual
    ? (stateData.snapshot?.apps || []).map((a) => ({
        ...a,
        target:
          (stateData.snapshot.collections || [])
            .filter(
              (c) =>
                !c.dynamic &&
                (!c.system || c.id === 'favorite') &&
                (c.appids || []).includes(a.appid),
            )
            .map((c) => c.name)
            .join('；') || '未分类',
      }))
    : stateData.manifest.games;
  let pub = document.querySelector('#publisherFilter')?.value || '',
    sort = document.querySelector('#gameSort')?.value || 'name';
  let gs = source.filter(
    (g) =>
      (!selected || selected.startsWith('@') || dynamic || actual
        ? !selected || members.has(g.appid)
        : planTargets(g).includes(selected)) &&
      (g.name + ' ' + g.appid + ' ' + publisherNames(g).join(' '))
        .toLowerCase()
        .includes(q) &&
      (!pub ||
        (pub === '@unknown'
          ? !publisherNames(g).length
          : publisherNames(g).includes(pub))),
  );
  if (sort === 'recent')
    gs.sort(
      (a, b) =>
        (appMap.get(b.appid)?.last_played || 0) -
        (appMap.get(a.appid)?.last_played || 0),
    );
  else if (sort === 'playtime')
    gs.sort(
      (a, b) =>
        (appMap.get(b.appid)?.playtime_minutes ?? -1) -
        (appMap.get(a.appid)?.playtime_minutes ?? -1),
    );
  else gs.sort((a, b) => a.name.localeCompare(b.name));
  visibleGames = gs;
  document.querySelector('#collectionTitle').textContent =
    selected === '@uncategorized'
      ? '未分类'
      : selected === '@no-static'
        ? '无静态收藏'
        : selected || '全部游戏';
  document.querySelector('#collectionMeta').textContent =
    gs.length +
    ' 款 · ' +
    (actual ? 'Steam 实际状态' : '待保存方案') +
    (dynamic ? ' · 动态成员只读，可另加静态归属' : '');
  let names = nameList().filter((n) => !isDynamic(n));
  document.querySelector('#bulkTargets').innerHTML = [
    '未分类',
    ...names.filter((n) => n !== '未分类'),
  ]
    .map((n) => '<option value="' + esc(n) + '">')
    .join('');
  let totalPages = Math.max(1, Math.ceil(gs.length / pageSize));
  if (pageNo >= totalPages) pageNo = totalPages - 1;
  document.querySelector('#pageInfo').textContent =
    '第 ' +
    (pageNo + 1) +
    ' / ' +
    totalPages +
    ' 页 · 每页 ' +
    pageSize +
    ' 款';
  let page = gs.slice(pageNo * pageSize, (pageNo + 1) * pageSize),
    body = document.querySelector('#games');
  body.innerHTML = '';
  for (let g of page) {
    let tr = document.createElement('tr'),
      info = appMap.get(g.appid) || {},
      rule = ruleIndex.get(g.appid) || g,
      tags = [];
    if (rule.protect) tags.push('保护');
    if (rule.quality_indie) tags.push('精品');
    if (rule.low_value) tags.push('低价值');
    if (rule.free_candidate) tags.push('免费候选');
    tr.innerHTML =
      '<td></td><td><b>' +
      esc(g.name) +
      '</b><small>AppID ' +
      g.appid +
      (rule.reason ? ' · ' + esc(rule.reason) : '') +
      '</small></td><td>' +
      esc(actual ? g.target : planTargets(g).join('；') || '未分类') +
      '</td><td>' +
      esc(publisherNames(g).join('；') || '待查询') +
      '</td><td>' +
      esc(playedText(info.last_played)) +
      '</td><td>' +
      esc(durationText(info.playtime_minutes)) +
      '</td><td>' +
      priceText((stateData.prices || {})[g.appid]) +
      '</td><td>' +
      tags.map((x) => '<span class="pill">' + x + '</span>').join('') +
      '</td>';
    let cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.dataset.appid = g.appid;
    cb.checked = checkedGames.has(g.appid);
    cb.onchange = () => {
      if (cb.checked) checkedGames.add(g.appid);
      else checkedGames.delete(g.appid);
      updateSelectionCount();
    };
    tr.children[0].appendChild(cb);
    tr.draggable = true;
    tr.addEventListener('dragstart', (e) => {
      if (!checkedGames.has(g.appid)) {
        checkedGames.clear();
        checkedGames.add(g.appid);
        cb.checked = true;
        updateSelectionCount();
      }
      e.dataTransfer.setData('text/plain', String(g.appid));
      e.dataTransfer.effectAllowed = 'move';
      tr.classList.add('dragging');
    });
    tr.addEventListener('dragend', () => tr.classList.remove('dragging'));
    body.appendChild(tr);
  }
  updateSelectionCount();
}
function pageBy(d) {
  pageNo = Math.max(0, pageNo + d);
  renderGames();
}
function togglePageSelection() {
  let boxes = [...document.querySelectorAll('#games input[type=checkbox]')],
    all = boxes.every((b) => b.checked);
  for (let b of boxes) {
    b.checked = !all;
    if (all) checkedGames.delete(Number(b.dataset.appid));
    else checkedGames.add(Number(b.dataset.appid));
  }
  updateSelectionCount();
}
async function applyTargetTo(raw) {
  let n = (raw || '').trim();
  if (!n || isDynamic(n)) {
    status('只能指定静态收藏；动态收藏是只读的', false);
    return;
  }
  let ids = new Set(checkedGames);
  if (!ids.size) {
    status('请先勾选游戏', false);
    return;
  }
  let before = JSON.parse(JSON.stringify(stateData.manifest)),
    m = stateData.manifest;
  if (m.collection_state) {
    for (let c of m.collection_state.collections)
      if (!c.dynamic && (!c.system || c.id === 'favorite'))
        c.appids = (c.appids || []).filter((id) => !ids.has(id));
    if (n !== '未分类') {
      let c = m.collection_state.collections.find(
        (c) => !c.dynamic && (!c.system || c.id === 'favorite') && c.name === n,
      );
      if (!c) {
        c = {
          id: 'manual-' + Date.now(),
          name: n,
          dynamic: false,
          system: false,
          editable: true,
          deletable: true,
          appids: [],
        };
        m.collection_state.collections.push(c);
      }
      c.appids = [...new Set([...c.appids, ...ids])];
    }
  }
  for (let g of m.games)
    if (ids.has(g.appid)) {
      g.target = n;
      g.priority = 6;
      g.dynamic_only_if_matched = false;
    }
  let x = await saveManifest(false);
  if (!x.ok) {
    stateData.manifest = before;
    status(x.error, false);
    return;
  }
  browsingActual = false;
  checkedGames.clear();
  renderAll();
  status(
    '已把 ' +
      ids.size +
      ' 款的目标改为「' +
      n +
      '」，请预览确认；动态收藏保持不变',
    true,
  );
}
async function applySelectedTarget() {
  await applyTargetTo(document.querySelector('#bulkTarget').value);
}
function esc(x) {
  return String(x).replace(
    /[&<>\"]/g,
    (c) =>
      ({
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '\\': '&#92;',
        '"': '&quot;',
      })[c],
  );
}
async function saveManifest(show = true) {
  stateData.manifest.game_count = stateData.manifest.games.length;
  let x = await api('/api/config', stateData.manifest);
  if (show) status(x.ok ? '规则已保存' : '规则保存失败', x.ok);
  return x;
}
async function loadState() {
  stateData = await api('/api/state');
  selected = '';
  renderAll();
  status(
    stateData.connected
      ? 'Steam 已读取 · ' + stateData.snapshot.apps.length + ' 款'
      : stateData.startup_done
        ? '未连接：' +
          (stateData.startup_error || '请使用准备连接 Steam，再重新读取')
        : '正在尝试读取 Steam…',
    !!stateData.connected,
  );
  let historyHeading = document.querySelector('#historyView h2');
  historyHeading.textContent = '历史 · ' + stateData.history_dir;
  historyHeading.style.overflowWrap = 'anywhere';
  refreshHistory();
}
let connectionBusy = false;
function setConnectionBusy(busy) {
  connectionBusy = busy;
  document
    .querySelectorAll('.connection-action')
    .forEach((b) => (b.disabled = busy));
}
async function diagnostics() {
  if (connectionBusy) return;
  setConnectionBusy(true);
  status('正在检查 Steam 连接…');
  try {
    let x = await api('/api/diagnostics', {});
    document.querySelector('#diagnosticsPanel').classList.remove('hidden');
    document.querySelector('#diagnosticsResult').innerHTML = (x.steps || [])
      .map(
        (s) =>
          '<p class="' +
          (s.ok ? 'ok' : 'bad') +
          '"><b>' +
          esc(s.name) +
          ' · ' +
          (s.ok ? '通过' : '未通过') +
          '</b><br>' +
          esc(s.detail) +
          '</p>',
      )
      .join('');
    status(x.ok ? '连接诊断通过' : '连接诊断发现问题，请查看结果', x.ok);
  } catch (e) {
    status('诊断失败：' + e.message, false);
  } finally {
    setConnectionBusy(false);
  }
}
async function restartSteam() {
  if (
    connectionBusy ||
    !confirm('请先退出正在运行的游戏。Steam 将正常退出并重新启动调试入口。')
  )
    return;
  setConnectionBusy(true);
  status('正在重启 Steam 并读取游戏库，请等待…');
  try {
    let x = await api('/api/restart-steam', {});
    if (x.ok) browsingActual = true;
    await loadState();
    status(
      x.ok ? 'Steam 已重启并读取 · ' + x.apps + ' 款' : '连接失败：' + x.error,
      x.ok,
    );
  } catch (e) {
    status('连接失败：' + e.message, false);
  } finally {
    setConnectionBusy(false);
  }
}
async function connect() {
  if (connectionBusy) return;
  setConnectionBusy(true);
  status('正在读取 Steam…');
  try {
    let x = await api('/api/connect', {});
    if (x.ok) browsingActual = true;
    await loadState();
    status(
      x.ok ? 'Steam 已连接 · ' + x.apps + ' 款' : '连接失败：' + x.error,
      x.ok,
    );
  } catch (e) {
    status('连接失败：' + e.message, false);
  } finally {
    setConnectionBusy(false);
  }
}
async function previewRemoval() {
  if (connectionBusy) return;
  if (!checkedGames.size) return status('请先勾选要检查的免费游戏', false);
  if (checkedGames.size > 50)
    return status('每批最多检查 50 款，请减少勾选数量', false);
  let dialog = document.querySelector('#removalDialog');
  if (!dialog) {
    dialog = document.createElement('dialog');
    dialog.id = 'removalDialog';
    dialog.style.cssText =
      'width:min(960px,95vw);max-height:85vh;overflow:auto;background:#182431;color:#e9f0f5;border:1px solid #4b7591;border-radius:10px;padding:24px';
    dialog.innerHTML =
      '<h2>批量移除免费游戏 · 先检查</h2><p>移除的是账号免费许可证，不是卸载文件。只执行下表通过检查的游戏；已有游玩记录、保护游戏、收费/锁区/下架、从属内容均跳过。不会编辑动态收藏或调用卸载向导。</p><div id="removalRows"></div><p id="removalMessage"></p><button id="removalConfirm" onclick="confirmRemoval()" class="warn" disabled>确认移除通过检查的游戏</button><button class="secondary" onclick="document.querySelector(\'#removalDialog\').close()">关闭</button>';
    document.body.appendChild(dialog);
  }
  removalPlan = null;
  dialog.showModal();
  document.querySelector('#removalRows').textContent =
    '正在核实当前中国区商店、最后游玩记录和 Steam 原生免费许可证…';
  document.querySelector('#removalMessage').textContent = '';
  document.querySelector('#removalConfirm').disabled = true;
  setConnectionBusy(true);
  try {
    let x = await api('/api/removal/preview', { appids: [...checkedGames] });
    if (!x.ok) {
      document.querySelector('#removalRows').textContent = x.error;
      return;
    }
    removalPlan = x.plan;
    let count = x.plan.items.filter((v) => v.eligible).length;
    document.querySelector('#removalRows').innerHTML =
      '<table><thead><tr><th>游戏</th><th>检查结果</th><th>说明</th></tr></thead><tbody>' +
      x.plan.items
        .map(
          (v) =>
            '<tr><td>' +
            esc(v.name) +
            '<small>AppID ' +
            v.appid +
            '</small></td><td class="' +
            (v.eligible ? 'ok' : 'bad') +
            '">' +
            (v.eligible ? '可移除' : '跳过') +
            '</td><td>' +
            esc(v.reason) +
            '</td></tr>',
        )
        .join('') +
      '</tbody></table>';
    document.querySelector('#removalMessage').textContent =
      count +
      ' 款通过检查。确认后会再次核验；本次检查无法保证未来是否收费或下架。';
    document.querySelector('#removalConfirm').disabled = !count;
  } catch (e) {
    document.querySelector('#removalRows').textContent =
      '检查失败：' + e.message;
  } finally {
    setConnectionBusy(false);
  }
}
async function confirmRemoval() {
  if (!removalPlan || connectionBusy) return;
  let count = removalPlan.items.filter((v) => v.eligible).length;
  if (
    !confirm(
      '确认从账号移除 ' +
        count +
        ' 款通过检查的免费游戏许可证？如需再次游玩，要重新获取。',
    )
  )
    return;
  setConnectionBusy(true);
  document.querySelector('#removalConfirm').disabled = true;
  document.querySelector('#removalMessage').textContent =
    '正在重新核验并逐项执行，请勿切换账号或关闭工具…';
  let token = removalPlan.token;
  removalPlan = null;
  try {
    let x = await api('/api/removal/apply', { token, confirm: 'REMOVE' });
    let lines = (x.results || []).map((v) => v.name + '：' + v.detail);
    document.querySelector('#removalMessage').textContent =
      (x.ok ? '本批已完成' : '已停止：' + x.error) +
      (lines.length ? '\n' + lines.join('\n') : '');
    document.querySelector('#removalMessage').style.whiteSpace = 'pre-wrap';
    for (let v of x.results || [])
      if (v.status === 'removed') checkedGames.delete(v.appid);
    updateSelectionCount();
    status(x.ok ? '移除请求已由 Steam 确认，请重新读取游戏库' : x.error, x.ok);
  } catch (e) {
    document.querySelector('#removalMessage').textContent =
      '连接中断，结果未确认。请重新读取 Steam 后核实，避免直接重复操作：' +
      e.message;
  } finally {
    setConnectionBusy(false);
  }
}
async function preview() {
  let x = await api('/api/preview', {});
  status(x.ok ? '预览完成' : '预览被阻止：' + x.error, x.ok);
  renderPreview(x.preview);
}
function renderPreview(p) {
  let el = document.querySelector('#preview');
  if (!p) {
    el.textContent = '没有预览结果';
    return;
  }
  let counts = {};
  for (let t of Object.values(p.effective_targets || {}))
    counts[t || '未分类（可保留动态）'] =
      (counts[t || '未分类（可保留动态）'] || 0) + 1;
  let rows = Object.entries(counts)
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([n, c]) => '<tr><td>' + esc(n) + '</td><td>' + c + '</td></tr>')
    .join('');
  let actions = (p.actions || [])
    .map(
      (a) =>
        '<tr><td>' +
        esc(a.phase) +
        '</td><td>' +
        esc(a.name) +
        '</td><td>' +
        (a.before || []).length +
        ' → ' +
        (a.after || []).length +
        '</td></tr>',
    )
    .join('');
  let blocked = (p.blocked || [])
    .map((x) => '<p class="bad">' + esc(x) + '</p>')
    .join('');
  el.innerHTML =
    '<div><b>账号：</b>' +
    esc(p.account || '未知') +
    '　<b>清单匹配：</b>' +
    p.found_total +
    ' / ' +
    p.manifest_count +
    '　<b>本次处理：</b>' +
    p.scope_count +
    '　<b>未找到：</b>' +
    (p.missing || []).length +
    '</div>' +
    blocked +
    '<h3>静态目标收藏</h3><table><thead><tr><th>收藏</th><th>游戏数</th></tr></thead><tbody>' +
    rows +
    '</tbody></table><h3>待执行变更</h3><table><thead><tr><th>操作</th><th>收藏</th><th>成员数</th></tr></thead><tbody>' +
    actions +
    '</tbody></table><p class="subtle">动态收藏只读取，不在变更列表中。正式保存前会再次核对 Steam 当前状态并备份。</p>';
}
async function apply() {
  if (
    !confirm(
      '请确认已在预览中检查动态提示、数量和目标收藏。保存会修改 Steam 收藏并先写入备份。',
    )
  )
    return;
  let x = await api('/api/apply', { confirm: 'APPLY' });
  if (x.ok) {
    browsingActual = true;
    await loadState();
  }
  status(x.ok ? '已保存：' + x.backup : x.error, x.ok);
}
async function quitTool() {
  if (!confirm('关闭 Steam 收藏整理工具？')) return;
  await api('/api/quit', {});
  status('工具已退出', true);
}
async function refreshPrices() {
  let ids = visibleGames.map((g) => g.appid);
  if (!ids.length) {
    status('当前列表没有游戏', false);
    return;
  }
  status('正在获取 ' + Math.min(ids.length, 500) + ' 款中国区价格…', true);
  let x = await api('/api/prices/refresh', { appids: ids });
  if (x.ok) {
    stateData.prices = x.prices;
    renderAll();
    document.querySelector('#priceInfo').textContent =
      '已缓存 ' + x.count + ' 款；查询时间 ' + new Date().toLocaleString();
  }
  status(x.ok ? '价格已更新' : x.error, x.ok);
}
async function models() {
  let x = await api('/api/ai/models', {
    url: document.querySelector('#aiurl').value,
    key: document.querySelector('#aikey').value,
  });
  status(x.ok ? '模型已载入' : '模型获取失败：' + x.error, x.ok);
  if (x.ok) {
    let sel = document.querySelector('#model');
    sel.innerHTML = '';
    for (let m of x.data.data || []) {
      let o = document.createElement('option');
      o.value = m.id;
      o.textContent = m.id;
      sel.appendChild(o);
    }
    if (uiPrefs.model) sel.value = uiPrefs.model;
  }
}
function openReviewWindow() {
  if (!window.__desktopShell)
    return window.open(
      '/review?wait=1',
      'steam-classification-review',
      'width=1100,height=850,resizable=yes,scrollbars=yes',
    );
  let overlay = document.getElementById('reviewOverlay');
  if (!overlay) {
    overlay = document.createElement('div');
    overlay.id = 'reviewOverlay';
    overlay.style.cssText =
      'position:fixed;inset:0;z-index:9999;display:flex;padding:18px;background:rgba(8,14,21,.88)';
    let box = document.createElement('div');
    box.style.cssText =
      'flex:1;display:flex;flex-direction:column;min-width:0;background:#101923;border:1px solid #344b5c;border-radius:10px;overflow:hidden';
    let bar = document.createElement('div');
    bar.style.cssText =
      'display:flex;align-items:center;justify-content:space-between;gap:12px;padding:8px 12px;background:#1b2a38;border-bottom:1px solid #2c4050';
    let title = document.createElement('span');
    title.textContent = '收藏方案审核';
    title.style.fontWeight = '600';
    let close = document.createElement('button');
    close.className = 'secondary';
    close.textContent = '关闭审核窗口';
    close.onclick = closeReviewPane;
    bar.appendChild(title);
    bar.appendChild(close);
    let frame = document.createElement('iframe');
    frame.id = 'reviewFrame';
    frame.title = '收藏方案审核';
    frame.style.cssText = 'flex:1;border:0;background:#101923';
    box.appendChild(bar);
    box.appendChild(frame);
    overlay.appendChild(box);
    document.body.appendChild(overlay);
  }
  let frame = document.getElementById('reviewFrame');
  frame.src = '/review?wait=1';
  return frame.contentWindow;
}
function closeReviewPane() {
  let overlay = document.getElementById('reviewOverlay');
  if (overlay) overlay.remove();
  reviewWindow = null;
}
function openReview(reset = false) {
  if (reset) {
    pendingDraft = null;
    reviewWindow = openReviewWindow();
    return reviewWindow;
  }
  if (!reviewWindow || reviewWindow.closed) reviewWindow = openReviewWindow();
  return reviewWindow;
}
async function draftReview(m, kind, source) {
  let normalized = await api('/api/import/normalize', m);
  if (!normalized.ok) {
    status(normalized.error, false);
    return;
  }
  m = normalized.manifest;
  let staged = await api('/api/draft', { manifest: m, kind, source });
  if (!staged.ok) {
    status(staged.error, false);
    return;
  }
  pendingDraft = { type: 'steam-draft', manifest: m, kind, source };
  let w = openReview();
  if (!w) {
    status('审核窗口被浏览器拦截，请允许本地弹窗后重试', false);
    return;
  }
  if (
    w.location.origin === location.origin &&
    w.document.readyState === 'complete'
  )
    w.postMessage(pendingDraft, location.origin);
}
window.addEventListener('message', (e) => {
  if (e.origin !== location.origin) return;
  if (e.data?.type === 'review-close') {
    closeReviewPane();
    return;
  }
  if (
    e.data?.type === 'review-ready' &&
    pendingDraft &&
    reviewWindow &&
    !reviewWindow.closed
  )
    reviewWindow.postMessage(pendingDraft, location.origin);
  if (e.data?.type === 'draft-accepted') {
    pendingDraft = null;
    browsingActual = false;
    loadState();
    status('草稿已接受并载入主表', true);
  }
  if (e.data?.type === 'draft-rejected') {
    pendingDraft = null;
    status('已拒绝草稿，主表未改变', true);
  }
});
async function suggest() {
  let p = document.querySelector('#prompt').value.trim();
  if (!p) {
    status('请先填写自己的提示词', false);
    return;
  }
  if (!stateData.manifest.games.length) {
    status('请先读取 Steam 库', false);
    return;
  }
  let w = openReview(true);
  if (!w) {
    status('请允许本地审核弹窗', false);
    return;
  }
  let games = stateData.manifest.games,
    targets = nameList().filter((n) => !isDynamic(n)),
    apps = new Map((stateData.snapshot?.apps || []).map((a) => [a.appid, a])),
    all = [];
  for (let i = 0; i < games.length; i += 80) {
    status(
      'AI 建议 ' + Math.min(i + 80, games.length) + ' / ' + games.length,
      true,
    );
    let batch = games
      .slice(i, i + 80)
      .map((g) => ({
        ...g,
        price: (stateData.prices || {})[g.appid] || null,
        last_played: (apps.get(g.appid) || {}).last_played || 0,
      }));
    let x = await api('/api/ai/suggest', {
      url: document.querySelector('#aiurl').value,
      key: document.querySelector('#aikey').value,
      model: document.querySelector('#model').value,
      prompt: p,
      games: batch,
      targets,
    });
    if (!x.ok) {
      status(x.error, false);
      return;
    }
    try {
      let raw = x.response.choices?.[0]?.message?.content || '';
      raw = raw
        .replace(/^\x60\x60\x60json\s*/, '')
        .replace(/\s*\x60\x60\x60$/, '');
      let parsed = JSON.parse(raw);
      all.push(...(Array.isArray(parsed) ? parsed : parsed.suggestions || []));
    } catch (e) {
      status('AI 返回不是可解析 JSON', false);
      return;
    }
  }
  let draft = JSON.parse(JSON.stringify(stateData.manifest));
  delete draft.collection_state;
  let by = new Map(all.map((v) => [Number(v.appid), v]));
  for (let g of draft.games) {
    let v = by.get(g.appid);
    if (
      v &&
      typeof v.target === 'string' &&
      v.target.trim() &&
      !isDynamic(v.target)
    ) {
      g.target = v.target.trim();
      g.priority = 6;
      g.dynamic_only_if_matched = false;
      g.reason = 'AI 建议（待审核）：' + (v.reason || '');
    }
  }
  await draftReview(draft, 'AI 分类建议', '用户提示词');
  status('AI 草稿已打开独立审核窗口', true);
}
async function importRules() {
  let f = document.querySelector('#ruleFile').files[0];
  if (!f) {
    status('请选择 JSON 文件', false);
    return;
  }
  let w = openReview(true);
  if (!w) {
    status('请允许本地审核弹窗', false);
    return;
  }
  try {
    let m = JSON.parse(await f.text());
    await draftReview(m, 'JSON 分类导入', f.name);
  } catch (e) {
    status('JSON 解析失败：' + e.message, false);
  }
}
async function exportCurrent() {
  if (!stateData.manifest.game_count) {
    status('请先读取 Steam 或接受导入方案', false);
    return;
  }
  let x = await api('/api/save-json', {
    name: 'classification-current.json',
    data: stateData.manifest,
  });
  status(x.ok ? '已保存到 ' + x.path + '（没有增加历史备份）' : x.error, x.ok);
}
async function exportLibrary() {
  let x = await api('/api/export', {});
  if (x.ok) {
    refreshHistory();
    status('游戏列表和收藏已导出并写入历史：' + x.dir, true);
  } else status(x.error, false);
}
async function refreshHistory() {
  let x = await api('/api/history');
  let el = document.querySelector('#historyList');
  el.innerHTML = '';
  if (!x.ok) {
    el.textContent = x.error;
    return;
  }
  for (let item of x.items || []) {
    let row = document.createElement('div');
    row.className = 'history-row';
    let b = document.createElement('button');
    b.className = 'secondary';
    b.style.flex = '1';
    b.textContent =
      item.title + ' · ' + new Date(item.created_at).toLocaleString();
    b.onclick = async () => {
      openReview(true);
      let y = await api('/api/history/read', { id: item.id });
      if (y.ok) await draftReview(y.data, '历史导入审核', item.title);
      else status(y.error, false);
    };
    let ex = document.createElement('button');
    ex.textContent = '导出 JSON';
    ex.className = 'secondary';
    ex.onclick = async () => {
      let y = await api('/api/history/read', { id: item.id });
      if (!y.ok) {
        status(y.error, false);
        return;
      }
      let nm = (item.path || '').split(/[\\/]/).pop() || 'history.json',
        z = await api('/api/save-json', { name: nm, data: y.data });
      status(z.ok ? '已保存到 ' + z.path : z.error, z.ok);
    };
    let d = document.createElement('button');
    d.textContent = '删除';
    d.className = 'secondary';
    d.onclick = async () => {
      let y = await api('/api/history/delete', { id: item.id });
      if (!y.ok) status(y.error, false);
      refreshHistory();
    };
    row.append(b, ex, d);
    el.appendChild(row);
  }
}
function showTab(tab) {
  document
    .querySelector('#libraryView')
    .classList.toggle('hidden', tab !== 'library');
  document
    .querySelector('#historyView')
    .classList.toggle('hidden', tab !== 'history');
  if (tab === 'history') refreshHistory();
}
async function startup() {
  applyTheme(document.documentElement.getAttribute('data-theme') || 'dark');
  await loadPrefs();
  await loadState();
  while (!stateData.startup_done && !stateData.connected) {
    await new Promise((resolve) => setTimeout(resolve, 1000));
    await loadState();
  }
}
startup();
