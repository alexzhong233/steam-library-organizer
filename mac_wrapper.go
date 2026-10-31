package main

// macOS launcher for the Steam importer. The importer serves its GUI over a
// local HTTP port. This launcher adds a same-origin reverse proxy so the app
// has a persistent light/dark/system theme selector without changing the
// Steam-facing core.
import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var guiURL = regexp.MustCompile(`GUI:\s+(https?://127\.0\.0\.1:[0-9]+/)`)

const themeInjection = `
<style id="codex-theme-overrides">
:root{color-scheme:dark}body{transition:background-color .18s ease,color .18s ease}
#codex-theme-switcher{position:fixed;z-index:2147483647;right:18px;top:14px;display:flex;align-items:center;gap:7px}main>.top>div:first-child>.subtle{display:none!important}
#codex-theme-toggle{width:36px;height:34px;padding:0;border:1px solid #526b80;border-radius:10px;background:rgba(19,31,43,.94);box-shadow:0 8px 24px rgba(0,0,0,.22);font-size:18px;line-height:30px;cursor:pointer}
#codex-action-bar{display:flex;align-items:center;gap:4px;flex-wrap:wrap;margin-top:-10px}
#codex-diagnostics-dialog{width:min(760px,calc(100vw - 40px));max-height:80vh;overflow:auto;border:1px solid #52738c;border-radius:12px;padding:20px;background:#182431;color:#e9f0f5;box-shadow:0 16px 50px rgba(0,0,0,.4)}#codex-diagnostics-dialog::backdrop{background:rgba(4,12,20,.58)}#codex-diagnostics-dialog .dialog-head{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:12px}#codex-diagnostics-dialog .dialog-head h2{margin:0}#codex-diagnostics-dialog .dialog-close{background:#33495c;border-color:#607f95}
.codex-diagnostic-source{display:none!important}#collectionMeta.codex-status-only{font-size:0}#collectionMeta.codex-status-only::after{content:attr(data-status);font-size:14px}
#codex-filter-column{display:flex;flex-direction:row;align-items:center;justify-content:flex-start;gap:8px;margin:0 0 12px;min-height:34px}#codex-filter-column>#gameSort{flex:0 0 150px;width:150px;margin:0}#codex-filter-column>#search{flex:0 1 320px;width:320px;max-width:42%;min-width:150px;margin:0}#codex-filter-column>#search.codex-search-hidden{display:none!important}#codex-search-toggle{flex:0 0 auto;margin:0;white-space:nowrap}.codex-drag-target{outline:2px dashed #61b8e8!important;outline-offset:-2px}.table-wrap tbody tr[draggable="true"]{cursor:grab}.table-wrap tbody tr[draggable="true"]:active{cursor:grabbing}
#staticTree .collection-row,.tree .collection-row{display:flex!important;align-items:center!important;gap:2px!important;min-height:36px!important;width:100%!important;position:relative!important}.tree .collection-row>button:first-child{width:auto!important;min-width:0!important;flex:1 1 auto!important;height:36px!important;min-height:36px!important;max-height:36px!important;padding:7px 8px!important;line-height:20px!important;white-space:nowrap!important;overflow:hidden!important;text-overflow:ellipsis!important;text-align:left!important}.tree .collection-row>.move{width:28px!important;min-width:28px!important;height:36px!important;min-height:36px!important;max-height:36px!important;flex:0 0 28px!important;padding:7px 2px!important;display:inline-flex!important;align-items:center!important;justify-content:center!important;text-align:center!important;line-height:20px!important}.tree .collection-row>button{transform:none!important}.tree .collection-row:hover>button{transform:translateY(-1px)!important}.tree .collection-row:hover>button:first-child,.tree .collection-row:hover>.move{background-color:rgba(74,137,176,.16)!important}
#codex-collection-hint{position:relative;z-index:10}.layout{column-gap:4px!important}.layout>.panel{margin-top:0!important}#libraryView section.panel>.toolbar:first-child{margin-top:0!important}.codex-main-heading{display:flex!important;align-items:center!important;gap:8px!important;height:34px!important;min-height:34px!important}.codex-main-heading>h2{margin:0!important;line-height:1.25!important}.codex-main-heading>#selectionCount{margin-right:auto!important}.codex-heading-row,#codex-collection-head{display:flex!important;align-items:center!important;height:34px!important;min-height:34px!important}.codex-heading-row h2,#codex-collection-head h2{margin:0!important;line-height:1.25!important}#codex-collection-tooltip{position:fixed;z-index:2147483647;max-width:min(320px,calc(100vw - 24px));padding:9px 11px;border:1px solid #52738c;border-radius:7px;background:#182431;color:#e9f0f5;box-shadow:0 8px 22px rgba(0,0,0,.28);font-size:12px;font-weight:400;line-height:1.45;white-space:normal;pointer-events:none}#codex-select-all,#games input[type="checkbox"]{appearance:none;-webkit-appearance:none;width:18px;height:18px;margin:0;border:2px solid #7f99aa;border-radius:5px;background:#fff;vertical-align:middle;cursor:pointer;position:relative}#codex-select-all:checked,#games input[type="checkbox"]:checked{background:#2b84ba;border-color:#2b84ba}#codex-select-all:checked::after,#games input[type="checkbox"]:checked::after{content:'✓';position:absolute;left:2px;top:-3px;color:#fff;font-size:15px;font-weight:700}#codex-select-all:indeterminate{background:linear-gradient(#2b84ba,#2b84ba) center/9px 3px no-repeat,#fff}.codex-games-table{table-layout:fixed!important;min-width:980px!important}.codex-games-table th:nth-child(1),.codex-games-table td:nth-child(1){width:48px}.codex-games-table th:nth-child(2),.codex-games-table td:nth-child(2){width:220px;max-width:220px}.codex-games-table th:nth-child(3),.codex-games-table td:nth-child(3){width:235px;max-width:235px;white-space:pre-line}.codex-games-table th:nth-child(4),.codex-games-table td:nth-child(4){width:110px}.codex-games-table th:nth-child(5),.codex-games-table td:nth-child(5){width:110px;white-space:nowrap}.codex-games-table th:nth-child(6),.codex-games-table td:nth-child(6){width:90px}.codex-games-table th:nth-child(7),.codex-games-table td:nth-child(7){width:100px}.codex-games-table th:nth-child(8),.codex-games-table td:nth-child(8){width:80px}.codex-games-table td:nth-child(2){overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
#codex-ai-panel{position:fixed;z-index:2147483646;right:18px;top:64px;width:min(430px,calc(100vw - 36px));max-height:calc(100vh - 86px);overflow:auto;box-shadow:0 14px 36px rgba(0,0,0,.3)}#codex-ai-panel:not([open]){display:none}
.layout{grid-template-columns:300px minmax(500px,1fr)!important}.right{grid-column:1/-1!important;display:block!important}
.codex-hidden-preview{display:none!important}.codex-nav{display:flex;align-items:center;gap:4px}.codex-nav button:last-child{margin-left:auto}
html[data-theme="light"]{color-scheme:light}html[data-theme="light"] body{background:linear-gradient(145deg,#eef4f8,#f8fbfd 62%,#e9f1f7)!important;color:#1d2935!important}
html[data-theme="light"] p,html[data-theme="light"] small{color:#526578!important}html[data-theme="light"] .panel,html[data-theme="light"] .toolbar{background:rgba(255,255,255,.94)!important;border-color:#c8d6e2!important;box-shadow:0 8px 24px rgba(44,67,87,.08)}
html[data-theme="light"] .tree button{border-bottom-color:#e0e8ef!important;color:#1d2935!important}html[data-theme="light"] .tree button.active{background:#dceefe!important;color:#123452!important}
html[data-theme="light"] input,html[data-theme="light"] select,html[data-theme="light"] textarea{background:#fff!important;color:#1d2935!important;border-color:#b9cad8!important}html[data-theme="light"] th{background:#e8f0f6!important;color:#284258!important}
html[data-theme="light"] td,html[data-theme="light"] th{border-bottom-color:#d9e3eb!important}html[data-theme="light"] .table-wrap{border-color:#c8d6e2!important}html[data-theme="light"] .note{background:#edf7ff!important;color:#29455c!important}
html[data-theme="light"] #codex-theme-toggle{background:rgba(255,255,255,.96);border-color:#b9cad8;box-shadow:0 8px 24px rgba(44,67,87,.16)}
button{transition:filter .15s ease,transform .15s ease}button:hover{transform:translateY(-1px)}
@media(max-width:760px){#codex-theme-switcher{right:10px;top:10px}.top{padding-right:60px!important}}
</style>
<script>
(function(){
const KEY='steam-collection-importer-theme';
function saveSettings(p){fetch('/api/ui-settings',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(p)}).catch(function(){})}
function setTheme(v){v=v==='dark'?'dark':'light';document.documentElement.dataset.theme=v;localStorage.setItem(KEY,v);var b=document.getElementById('codex-theme-toggle');if(b){b.textContent=v==='dark'?'☀️':'🌙';b.title=v==='dark'?'切换到白天模式':'切换到黑夜模式';b.setAttribute('aria-label',b.title)}saveSettings({theme:v})}
function setup(){
  var metaGuard=document.getElementById('collectionMeta');if(!metaGuard){metaGuard=document.createElement('span');metaGuard.id='collectionMeta';metaGuard.style.display='none';document.body.appendChild(metaGuard)}
  var saved=localStorage.getItem(KEY)||'light';
  var wrap=document.createElement('div');wrap.id='codex-theme-switcher';var tb=document.createElement('button');tb.id='codex-theme-toggle';tb.onclick=function(){setTheme(document.documentElement.dataset.theme==='dark'?'light':'dark')};wrap.appendChild(tb);document.body.appendChild(wrap);setTheme(saved);
  var first=document.querySelector('main > .toolbar');
  var diagDialog=document.createElement('dialog');diagDialog.id='codex-diagnostics-dialog';diagDialog.innerHTML='<div class="dialog-head"><h2>连接诊断</h2><button type="button" class="dialog-close">关闭</button></div><div id="codex-diagnostics-content"><p>正在等待诊断结果…</p></div>';document.body.appendChild(diagDialog);diagDialog.querySelector('.dialog-close').onclick=function(){diagDialog.close()};
  var removalBtn=null;
  if(first){first.classList.add('codex-nav');var btns=Array.prototype.slice.call(first.querySelectorAll('button'));var lib=btns.find(function(b){return b.textContent.trim()==='游戏库'});var hist=btns.find(function(b){return b.textContent.indexOf('历史')===0});var quit=btns.find(function(b){return b.textContent.trim()==='退出工具'});var diag=btns.find(function(b){return b.textContent.trim()==='连接诊断'});var restart=btns.find(function(b){return b.textContent.trim()==='准备连接 Steam'});var connect=btns.find(function(b){return b.textContent.trim()==='重新读取 Steam'});var prev=btns.find(function(b){return b.textContent.trim()==='生成预览'});if(lib)lib.textContent='游戏库';if(hist)hist.textContent='历史';if(quit){quit.textContent='退出工具';first.appendChild(quit)}if(diag){diag.textContent='连接诊断';diag.style.display='';diag.onclick=function(){if(!window.diagnostics)return;diagDialog.querySelector('#codex-diagnostics-content').innerHTML='<p>正在检查 Steam 连接…</p>';if(diagDialog.showModal)diagDialog.showModal();Promise.resolve(window.diagnostics()).then(function(){var source=document.getElementById('diagnosticsResult');if(source)diagDialog.querySelector('#codex-diagnostics-content').innerHTML=source.innerHTML||'<p>没有诊断结果。</p>'})}}if(restart){restart.textContent='刷新列表';restart.style.display='';restart.onclick=function(){if(typeof stateData!=='undefined'&&stateData&&stateData.connected){if(typeof window.connect==='function')window.connect()}else if(typeof window.restartSteam==='function')window.restartSteam()};}if(connect)connect.style.display='none';if(prev)prev.style.display='none';removalBtn=Array.prototype.slice.call(document.querySelectorAll('button')).find(function(b){return b.textContent.trim()==='移除选中的免费游戏'});if(removalBtn){removalBtn.textContent='移除游戏';removalBtn.title='仅可移除中国区当前免费、之后仍可重新获取且没有游玩记录的游戏；工具会在执行前再次核验。';removalBtn.setAttribute('aria-label',removalBtn.title);removalBtn.style.display='';}if(diag)first.insertBefore(diag,quit||null);if(restart)first.insertBefore(restart,quit||null);var save=Array.prototype.slice.call(first.querySelectorAll('button')).find(function(b){return b.textContent.indexOf('确认保存')===0});if(save){save.textContent='确认保存收藏';save.style.display='';}
  }
  var panel=Array.prototype.slice.call(document.querySelectorAll('details')).find(function(x){return (x.querySelector('summary')?.textContent||'').trim()==='AI 整理'});if(panel){panel.id='codex-ai-panel';panel.open=false;['aiurl','aikey','model'].forEach(function(id){var e=document.getElementById(id);if(e)e.addEventListener('change',function(){saveSettings({ai_url:(document.getElementById('aiurl')||{}).value||'',api_key:(document.getElementById('aikey')||{}).value||'',model:(document.getElementById('model')||{}).value||''})})});fetch('/api/ui-settings').then(function(r){return r.json()}).then(function(s){if(s.ai_url&&document.getElementById('aiurl'))document.getElementById('aiurl').value=s.ai_url;if(s.api_key&&document.getElementById('aikey'))document.getElementById('aikey').value=s.api_key;if(s.model&&document.getElementById('model'))document.getElementById('model').value=s.model;if(s.theme)setTheme(s.theme)}).catch(function(){})}
  var sourcePanel=document.getElementById('diagnosticsPanel');if(sourcePanel)sourcePanel.classList.add('codex-diagnostic-source');var priceBtn=Array.prototype.slice.call(document.querySelectorAll('button')).find(function(b){return b.textContent.trim()==='获取商店资料和价格'});var saveBtn=Array.prototype.slice.call(document.querySelectorAll('button')).find(function(b){return b.textContent.indexOf('确认保存')===0});var ai=document.createElement('button');ai.id='codex-ai-toggle';ai.textContent='✨ AI 整理';ai.onclick=function(){var p=document.getElementById('codex-ai-panel');if(p){p.open=!p.open;if(p.open)p.scrollIntoView({behavior:'smooth',block:'center'})}};if(priceBtn&&priceBtn.parentElement){var bar=priceBtn.parentElement;var titleBox=bar.querySelector('#collectionTitle')?.parentElement;bar.classList.add('codex-main-heading');bar.style.flexWrap='nowrap';if(titleBox){var heading=titleBox.querySelector('#collectionTitle');if(heading){heading.style.margin='0';bar.insertBefore(heading,bar.firstChild)}var selectionCount=document.getElementById('selectionCount'),oldSelectionBar=selectionCount&&selectionCount.closest('.toolbar');bar.insertBefore(ai,priceBtn);if(selectionCount){if(!selectionCount.textContent.trim())selectionCount.textContent='已选 0 款（跨页保留）';selectionCount.style.margin='0';selectionCount.style.whiteSpace='nowrap';bar.insertBefore(selectionCount,priceBtn);if(oldSelectionBar&&oldSelectionBar!==bar)oldSelectionBar.style.display='none'}var preservedMeta=titleBox.querySelector('#collectionMeta');if(preservedMeta){bar.appendChild(preservedMeta);preservedMeta.style.display='none'}titleBox.remove()}else bar.insertBefore(ai,priceBtn);if(removalBtn){removalBtn.style.display='';if(removalBtn.parentElement!==bar)bar.insertBefore(removalBtn,priceBtn)}}var collectionMeta=document.getElementById('collectionMeta');if(collectionMeta)collectionMeta.style.display='none';
  var bulkTarget=document.getElementById('bulkTarget'),applySelected=Array.prototype.slice.call(document.querySelectorAll('button')).find(function(b){return b.textContent.trim()==='应用到勾选游戏'}),pageSelect=Array.prototype.slice.call(document.querySelectorAll('button')).find(function(b){return b.textContent.trim()==='勾选本页'}),clearSelect=Array.prototype.slice.call(document.querySelectorAll('button')).find(function(b){return b.textContent.trim()==='清空勾选'});[bulkTarget,applySelected,pageSelect,clearSelect].forEach(function(e){if(e)e.style.display='none'});
  var pv=document.getElementById('preview');if(pv&&pv.closest('section'))pv.closest('section').classList.add('codex-hidden-preview');var h=document.querySelector('#historyView h2');if(h)h.textContent='历史';
  var vs=document.querySelector('.view-switch'),left=document.querySelector('#libraryView aside.panel.sticky');if(vs&&left){vs.style.margin='8px 0 12px';var switchButtons=vs.querySelectorAll('button');if(switchButtons[0])switchButtons[0].textContent='当前收藏';if(switchButtons[1])switchButtons[1].textContent='待保存';left.insertBefore(vs,left.querySelector('#dynamicTree')||left.firstChild);var leftSave=Array.prototype.slice.call(document.querySelectorAll('button')).find(function(b){return b.textContent.indexOf('确认保存')===0});if(leftSave){leftSave.textContent='确认保存收藏';var lh=left.querySelector('h2'),note=left.querySelector('.note');if(note)note.style.display='none';if(lh){var lhWrap=document.createElement('div');lhWrap.id='codex-collection-head';lhWrap.className='codex-heading-row';lhWrap.style.cssText='justify-content:flex-start;gap:6px';lh.parentNode.insertBefore(lhWrap,lh);lh.style.margin='0';lhWrap.appendChild(lh);var info=document.createElement('span');info.id='codex-collection-hint';info.textContent='ⓘ';info.title=(note&&note.textContent.trim())||'动态收藏由你在 Steam 中创建和维护，程序只读取它们来计算重叠，不会自动创建、改名或修改动态规则。';info.dataset.tip=info.title;info.setAttribute('aria-label',info.title);info.tabIndex=0;info.style.cssText='color:#8ecdf2;cursor:help;font-size:15px;line-height:1';lhWrap.appendChild(info);lhWrap.appendChild(leftSave);leftSave.style.margin='0 0 0 auto'}else left.appendChild(leftSave)}}
  if(typeof window.api==='function'){window.apply=async function(){if(!confirm('确认将待保存方案写入 Steam？保存前会自动备份。'))return;try{var x=await window.api('/api/apply',{confirm:'APPLY'});if(x.ok){browsingActual=true;await window.loadState()}window.status(x.ok?'已保存：'+x.backup:x.error,x.ok)}catch(e){window.status('保存失败：'+e.message,false)}}}
  var sort=document.getElementById('gameSort'),pub=document.getElementById('publisherFilter'),search=document.getElementById('search');if(sort){sort.innerHTML='<option value="name">按名称</option>';sort.style.flex='0 0 150px';sort.style.width='150px';var filterBar=sort.closest('.toolbar');var col=document.createElement('div');col.id='codex-filter-column';if(filterBar){filterBar.style.display='none';filterBar.parentNode.insertBefore(col,filterBar.nextSibling)}if(pub)pub.style.display='none';var searchToggle=document.createElement('button');searchToggle.type='button';searchToggle.id='codex-search-toggle';searchToggle.className='secondary';searchToggle.textContent='🔍 搜索';searchToggle.title='显示或隐藏搜索框';if(search){search.classList.remove('codex-search-hidden');search.style.flex='0 1 320px';search.style.width='320px';search.style.maxWidth='42%';searchToggle.onclick=function(){var hidden=search.classList.contains('codex-search-hidden');search.classList.toggle('codex-search-hidden',!hidden);if(hidden)search.focus()};col.appendChild(search);col.appendChild(searchToggle)}else col.appendChild(searchToggle);col.appendChild(sort)}
  var gamesTable=document.querySelector('#games')?.closest('table');if(gamesTable)gamesTable.classList.add('codex-games-table');var head=gamesTable?.querySelector('thead th:first-child');if(head){head.innerHTML='<input id="codex-select-all" type="checkbox" aria-label="全选" title="全选当前筛选结果">';var allBox=document.getElementById('codex-select-all');allBox.onchange=function(){var ids=(visibleGames||[]).map(function(g){return Number(g.appid)});if(allBox.checked)ids.forEach(function(id){checkedGames.add(id)});else ids.forEach(function(id){checkedGames.delete(id)});renderGames()}}
  function syncSelectAll(){var b=document.getElementById('codex-select-all');if(!b)return;var ids=(visibleGames||[]).map(function(g){return Number(g.appid)});b.checked=!!ids.length&&ids.every(function(id){return checkedGames.has(id)});b.indeterminate=!b.checked&&ids.some(function(id){return checkedGames.has(id)})}
  function attachDragTargets(){document.querySelectorAll('#staticTree .collection-row').forEach(function(row){var b=row.querySelector('button');if(!b||b.dataset.dragBound)return;b.dataset.dragBound='1';b.addEventListener('dragover',function(e){e.preventDefault();row.classList.add('codex-drag-target')});b.addEventListener('dragleave',function(){row.classList.remove('codex-drag-target')});b.addEventListener('drop',function(e){e.preventDefault();row.classList.remove('codex-drag-target');var n=b.textContent.replace(/\s·\s\d+$/,'').trim();var ids=(visibleGames||[]).map(function(g){return Number(g.appid)}).filter(function(id){return checkedGames.has(id)});if(!ids.length&&e.dataTransfer.getData('text/plain'))ids=[Number(e.dataTransfer.getData('text/plain'))];if(!ids.length)return;if(bulkTarget)bulkTarget.value=n;if(window.applySelectedTarget)window.applySelectedTarget()})})}
  function attachDraggableRows(){document.querySelectorAll('#games tr').forEach(function(row){var cb=row.querySelector('input[type=checkbox]');if(!cb||row.dataset.dragBound)return;row.dataset.dragBound='1';row.draggable=true;row.addEventListener('dragstart',function(e){var id=Number(cb.dataset.appid);if(!checkedGames.has(id)){checkedGames.clear();checkedGames.add(id);cb.checked=true;syncSelectAll()}e.dataTransfer.setData('text/plain',String(id));e.dataTransfer.effectAllowed='move'})})}
  function bindCollectionHint(){var info=document.getElementById('codex-collection-hint');if(!info||info.dataset.tipBound)return;info.dataset.tipBound='1';var tip=null;function hide(){if(tip){tip.remove();tip=null}}function show(){hide();tip=document.createElement('div');tip.id='codex-collection-tooltip';tip.textContent=info.dataset.tip||info.title||'';document.body.appendChild(tip);var r=info.getBoundingClientRect(),w=tip.offsetWidth,h=tip.offsetHeight,left=Math.min(Math.max(8,r.left),Math.max(8,window.innerWidth-w-8)),top=r.bottom+8;if(top+h>window.innerHeight-8)top=Math.max(8,r.top-h-8);tip.style.left=left+'px';tip.style.top=top+'px'}info.addEventListener('mouseenter',show);info.addEventListener('mouseleave',hide);info.addEventListener('focus',show);info.addEventListener('blur',hide)}
  function formatGameTable(){document.querySelectorAll('#games tr').forEach(function(row){var cells=row.children;if(cells[2]&&!cells[2].dataset.codexTargetFormatted){cells[2].textContent=cells[2].textContent.replace(/\s*；\s*/g,'\n');cells[2].dataset.codexTargetFormatted='1'}if(cells[4]&&!cells[4].dataset.codexDateFormatted){var raw=cells[4].textContent.trim();if(raw&&raw!=='无记录'){var d=new Date(raw);if(!isNaN(d.getTime()))cells[4].textContent=d.toLocaleDateString('zh-CN')}cells[4].dataset.codexDateFormatted='1'}})}
  var dragObs=new MutationObserver(function(){attachDragTargets();attachDraggableRows();formatGameTable();bindCollectionHint();syncSelectAll()});dragObs.observe(document.body,{childList:true,subtree:true});attachDragTargets();attachDraggableRows();formatGameTable();bindCollectionHint();syncSelectAll();
  function updateLeftCount(){var b=document.querySelector('#dynamicTree button');if(!b||b.textContent.indexOf('全部游戏')!==0)return;var n=0;try{n=browsingActual?(stateData.snapshot.apps||[]).length:(stateData.manifest.games||[]).length}catch(e){}if(n){var want='全部游戏 · '+n+'款';if(b.textContent!==want)b.textContent=want}}
  function cleanMeta(){var title=document.getElementById('collectionTitle'),meta=document.getElementById('collectionMeta');if(!title||!meta)return;var v=meta.textContent.replace(/\s*·\s*动态成员只读，可另加静态归属/g,'');if(title.textContent.trim()==='全部游戏')v=v.replace(/^\s*\d+\s*款\s*·\s*/,'');if(meta.textContent!==v)meta.textContent=v}
  updateLeftCount();cleanMeta();var obs=new MutationObserver(function(){var hh=document.querySelector('#historyView h2');if(hh&&hh.textContent!=='历史')hh.textContent='历史';updateLeftCount();cleanMeta()});obs.observe(document.body,{childList:true,subtree:true,characterData:true});
}
if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',setup);else setTimeout(setup,0);
})();
</script>`

func main() {
	exe, err := os.Executable()
	if err != nil {
		fail("无法定位应用程序", err)
	}
	appRoot := filepath.Clean(filepath.Join(filepath.Dir(exe), ".."))
	core := filepath.Join(appRoot, "Resources", "SteamCollectionImporterCore")
	if _, err := os.Stat(core); err != nil {
		fail("缺少核心程序", err)
	}
	args := append([]string(nil), os.Args[1:]...)
	dataDir, explicitDataDir := findDataDirArg(args)
	if !explicitDataDir {
		home, err := os.UserHomeDir()
		if err != nil {
			fail("无法定位用户目录", err)
		}
		dataDir = filepath.Join(home, ".steam-collection-organizer")
		args = append(args, "-data-dir", dataDir)
	} else if !filepath.IsAbs(dataDir) {
		dataDir, err = filepath.Abs(dataDir)
		if err != nil {
			fail("无法解析数据目录", err)
		}
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		fail("无法创建用户数据目录", err)
	}
	if !explicitDataDir {
		if err := migrateLegacyData(appRoot, dataDir); err != nil {
			fail("无法迁移历史和设置", err)
		}
	}
	fmt.Println("用户数据目录:", dataDir)
	// The core also tries to open its raw GUI. Put a small open shim first in
	// PATH so that only Steam launch requests pass through; the wrapper opens
	// the themed proxy below, leaving a single browser page visible to users.
	openShimDir, err := os.MkdirTemp("", "steam-collection-importer-open-")
	if err != nil {
		fail("无法准备浏览器启动控制", err)
	}
	defer os.RemoveAll(openShimDir)
	openShim := "#!/bin/sh\nprev=''\nfor arg in \"$@\"; do\n  if [ \"$prev\" = \"-a\" ] && { [ \"$arg\" = \"Steam\" ] || [ \"$arg\" = \"Steam.app\" ] || [ \"$arg\" = \"com.valvesoftware.steam\" ]; }; then\n    exec /usr/bin/open \"$@\"\n  fi\n  case \"$arg\" in\n    steam://*) exec /usr/bin/open \"$@\" ;;\n  esac\n  prev=\"$arg\"\ndone\nexit 0\n"
	if err := os.WriteFile(filepath.Join(openShimDir, "open"), []byte(openShim), 0o755); err != nil {
		fail("无法准备浏览器启动控制", err)
	}
	cmd := exec.Command(core, args...)
	pathValue := openShimDir
	if currentPath := os.Getenv("PATH"); currentPath != "" {
		pathValue += string(os.PathListSeparator) + currentPath
	}
	env := os.Environ()
	pathSet := false
	for i, value := range env {
		if strings.HasPrefix(value, "PATH=") {
			env[i] = "PATH=" + pathValue
			pathSet = true
			break
		}
	}
	if !pathSet {
		env = append(env, "PATH="+pathValue)
	}
	cmd.Env = env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fail("无法读取核心输出", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fail("无法启动核心程序", err)
	}
	urlCh := make(chan string, 1)
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			line := s.Text()
			fmt.Println(line)
			if m := guiURL.FindStringSubmatch(line); len(m) == 2 {
				select {
				case urlCh <- m[1]:
				default:
				}
			}
		}
	}()
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	var upstream string
	select {
	case upstream = <-urlCh:
	case <-scanDone:
		if err := <-waitCh; err != nil {
			os.Exit(exitCode(err))
		}
		return
	}
	settingsPath := filepath.Join(dataDir, "settings.json")
	proxyURL, closeProxy, err := serveThemeProxy(upstream, settingsPath)
	if err != nil {
		fail("无法启动主题代理", err)
	}
	defer closeProxy()
	fmt.Println("GUI（已启用主题切换）:", proxyURL)
	if err := exec.Command("open", proxyURL).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "无法自动打开浏览器，请手动访问", proxyURL, ":", err)
	}
	if err := <-waitCh; err != nil {
		os.Exit(exitCode(err))
	}
}

func findDataDirArg(args []string) (string, bool) {
	for i, arg := range args {
		if arg == "-data-dir" || arg == "--data-dir" {
			if i+1 < len(args) && args[i+1] != "" {
				return args[i+1], true
			}
		}
		for _, prefix := range []string{"-data-dir=", "--data-dir="} {
			if strings.HasPrefix(arg, prefix) && len(arg) > len(prefix) {
				return strings.TrimPrefix(arg, prefix), true
			}
		}
	}
	return "", false
}

func hasFlag(args []string, names ...string) bool {
	for _, arg := range args {
		for _, name := range names {
			if arg == name || strings.HasPrefix(arg, name+"=") {
				return true
			}
		}
	}
	return false
}

func latestPlan(dataDir string) string {
	if path := filepath.Join(dataDir, "classification.json"); fileExists(path) {
		return path
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "history"))
	if err != nil {
		return ""
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if !entries[i].IsDir() {
			continue
		}
		path := filepath.Join(dataDir, "history", entries[i].Name(), "classification.json")
		if fileExists(path) {
			return path
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func migrateLegacyData(appRoot, dataDir string) error {
	legacySettings := filepath.Join(appRoot, "..", "..", "settings.json")
	if err := copyFileIfAbsent(legacySettings, filepath.Join(dataDir, "settings.json")); err != nil {
		return err
	}
	legacyHistory := []string{
		filepath.Join(appRoot, "..", "..", "history"),
		filepath.Join(appRoot, "..", "history"),
		filepath.Join(appRoot, "Resources", "history"),
	}
	for _, source := range legacyHistory {
		if err := copyTreeIfAbsent(source, filepath.Join(dataDir, "history")); err != nil {
			return err
		}
	}
	return nil
}

func copyFileIfAbsent(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if _, err := os.Stat(destination); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	defer output.Close()
	_, err = io.Copy(output, input)
	return err
}

func copyTreeIfAbsent(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return filepath.Walk(source, func(path string, item os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if item.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFileIfAbsent(path, target)
	})
}

func serveThemeProxy(raw string, settingsPath string) (string, func(), error) {
	target, err := url.Parse(raw)
	if err != nil {
		return "", nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	original := proxy.Director
	proxy.Director = func(r *http.Request) { original(r); r.Header.Del("Accept-Encoding") }
	proxy.ModifyResponse = func(resp *http.Response) error {
		if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
			return nil
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		resp.Body.Close()
		text := string(body)
		idx := strings.LastIndex(strings.ToLower(text), "</head>")
		if idx < 0 {
			idx = len(text)
		}
		text = text[:idx] + themeInjection + text[idx:]
		resp.Body = io.NopCloser(strings.NewReader(text))
		resp.ContentLength = int64(len(text))
		resp.Header.Set("Content-Length", fmt.Sprint(len(text)))
		resp.Header.Del("Content-Encoding")
		return nil
	}
	store := &settingsStore{path: settingsPath}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ui-settings" {
			store.serve(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)
	closeFn := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		ln.Close()
	}
	return "http://" + ln.Addr().String() + "/", closeFn, nil
}

type settingsStore struct {
	path string
	mu   sync.Mutex
}
type userSettings struct {
	AIURL  string `json:"ai_url"`
	APIKey string `json:"api_key"`
	Model  string `json:"model"`
	Theme  string `json:"theme"`
}

func (s *settingsStore) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Method == http.MethodGet {
		b, _ := os.ReadFile(s.path)
		var v userSettings
		if json.Unmarshal(b, &v) != nil {
			v.Theme = "light"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
		return
	}
	if r.Method == http.MethodPost {
		var v userSettings
		if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if v.Theme != "light" && v.Theme != "dark" && v.Theme != "system" {
			v.Theme = "light"
		}
		b, _ := json.MarshalIndent(v, "", "  ")
		if err := os.WriteFile(s.path, append(b, '\n'), 0600); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
		return
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
}
func fail(msg string, err error) { fmt.Fprintln(os.Stderr, msg+":", err); os.Exit(1) }
func exitCode(err error) int {
	if e, ok := err.(*exec.ExitError); ok {
		return e.ExitCode()
	}
	return 1
}
