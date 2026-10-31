import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
const fn=(0,eval)('('+readFileSync(new URL('./bridge.js',import.meta.url),'utf8')+')');
test('explicit deletion checks membership and name before removing an obsolete collection',async()=>{
 const x=steam();x.add('old','Old',[1]);
 await assert.rejects(fn({op:'delete',id:'old',expected_name:'Old',expected_before:[]}),/changed before deletion/);
 assert(x.state.has('old'));
 await assert.rejects(fn({op:'delete',id:'old',expected_name:'Other',expected_before:[1]}),/changed since backup/);
 assert(x.state.has('old'));
 await fn({op:'delete',id:'old',expected_name:'Old',expected_before:[1]});
 assert(!x.state.has('old'));
 x.add('empty','Empty',[]);await fn({op:'delete',id:'empty',expected_name:'Empty',expected_before:[]});
 assert(!x.state.has('empty'));
});
function steam(){
 const catalog=new Map([1,2,3,4,99].map(id=>[id,{appid:id,display_name:'Game '+id,rt_last_time_played:id===2?12345:0}]));
 const state=new Map(),disk=new Map();let next=0,writes=0,collisionDeletes=0,deleted=[];let failSave=null;
 const validate=vs=>{for(const v of vs)assert.equal(typeof v,'object','API requires overview objects')};
 class Col{
  constructor(id,name,filter,vs){validate(vs);this.id=id;this.m_strName=name;this.m_filterSpec=filter;this.added=new Set(vs.map(v=>v.appid));this.removed=new Set();}
  get displayName(){return this.m_strName}
  get bIsDynamic(){return !!this.m_filterSpec}
  get allApps(){let ids=[...this.added];if(this.bIsDynamic)ids.push(...(this.m_filterSpec.filterGroups[0]?.rgOptions||[]));return [...new Set(ids)].filter(id=>!this.removed.has(id)).sort((a,b)=>a-b).map(id=>catalog.get(id));}
  AsDragDropCollection(){return this}
  async AddApps(vs){validate(vs);for(const v of vs){this.added.add(v.appid);this.removed.delete(v.appid)}await this.Save()}
  async RemoveApps(vs){validate(vs);for(const v of vs){this.added.delete(v.appid);this.removed.add(v.appid)}await this.Save()}
  AsDeletableCollection(){return ['hidden','favorite'].includes(this.id)?null:{Delete:async()=>{deleted.push(this.id);state.delete(this.id);disk.delete(this.id)}}}
  SetName(n){this.m_strName=n}
  async Save(){return cs.SaveCollection(this)}
 }
 const cs={
  get userCollections(){return [...state.values()].filter(c=>!['favorite','hidden'].includes(c.id))},
  GetCollection(id){return state.get(id)},
  NewUnsavedCollection(name,filter,vs){return new Col('new'+(++next),name,filter,vs)},
  async SaveCollection(c){writes++;if(failSave&&failSave(c))throw Error('injected save failure');for(const x of state.values())if(x.id!==c.id&&x.displayName===c.displayName){collisionDeletes++;state.delete(x.id);disk.delete(x.id)};state.set(c.id,c);disk.set(c.id,JSON.parse(JSON.stringify({id:c.id,name:c.displayName,filter:c.m_filterSpec,added:[...c.added],removed:[...c.removed]})));},
  BIsSystemCollectionName(n){return ['Favorites','Hidden'].includes(n)},
  async SetAppsAsFavorite(ids,value){for(const id of ids)assert.equal(typeof id,'number');let c=state.get('favorite');if(!c){c=new Col('favorite','Favorites',undefined,[]);state.set('favorite',c)};if(value)await c.AddApps(ids.map(id=>catalog.get(id)));else await c.RemoveApps(ids.map(id=>catalog.get(id)));},
  allAppsCollection:{allApps:[...catalog.values()]},
  appTypeCollectionMap:new Map()
 };
 const add=(id,name,ids,filter)=>{const c=new Col(id,name,filter,ids.map(id=>catalog.get(id)));state.set(id,c);disk.set(id,{id,name,filter,added:ids,removed:[]});return c};
 add('favorite','Favorites',[1,99]);add('hidden','Hidden',[2,99]);add('old','Old',[1,2,99]);
 globalThis.collectionStore=cs;globalThis.appStore={GetAppOverviewByAppID:id=>catalog.get(id)};
 return {cs,state,disk,catalog,add,get writes(){return writes},get collisionDeletes(){return collisionDeletes},deleted,setFailSave:f=>{failSave=f},restart(){state.clear();for(const v of disk.values()){const c=new Col(v.id,v.name,v.filter,v.added.map(id=>catalog.get(id)));c.removed=new Set(v.removed);state.set(c.id,c)}}};
}
const put=(name,appids,other={})=>fn({op:'put',name,appids,expected_before:[],run_id:'test',sequence:1,...other});
const ids=c=>c.allApps.map(v=>v.appid);
test('native saves survive simulated restart, outside games and hidden preserved',async()=>{const x=steam();const hidden=ids(x.state.get('hidden'));let r=await put('Series',[1,3]);assert.deepEqual(r.appids,[1,3]);await put('Old',[99],{id:'old',expected_before:[1,2,99]});await put('Favorites',[99],{id:'favorite',expected_before:[1,99]});x.restart();assert.deepEqual(ids(x.state.get(r.id)),[1,3]);assert.deepEqual(ids(x.state.get('old')),[99]);assert.deepEqual(ids(x.state.get('hidden')),hidden);assert.equal(x.collisionDeletes,0)});
test('existing same-name static collection reused and exact members saved',async()=>{const x=steam();const r=await put('Old',[2,3,99],{expected_before:[1,2,99]});assert.equal(r.id,'old');assert.deepEqual(r.appids,[2,3,99]);assert.equal(x.collisionDeletes,0)});
test('dynamic freeze validates recipe and stages before delete, then restores filter and exclusions',async()=>{const x=steam();const filter={filterGroups:[{rgOptions:[1,2,3]}]};const c=x.add('dyn','Dynamic',[99],filter);c.removed.add(2);await c.Save();assert.deepEqual(ids(c),[1,3,99]);const frozen=await put('Dynamic',[99],{id:'dyn',expected_before:[1,3,99],freeze_dynamic:true,filter,explicit_added:[99],explicit_removed:[2]});assert.equal(frozen.dynamic,false);assert.deepEqual(frozen.appids,[99]);assert(!x.state.has('dyn'));const restored=await put('Dynamic',[1,3,99],{id:frozen.id,expected_before:[99],restore_dynamic:true,filter,explicit_added:[99],explicit_removed:[2],sequence:2});assert(restored.dynamic);assert.deepEqual(restored.filter,filter);const live=x.state.get(restored.id);assert.deepEqual([...live.added],[99]);assert.deepEqual([...live.removed],[2]);assert.equal(x.collisionDeletes,0);x.restart();assert.deepEqual(ids(x.state.get(restored.id)),[1,3,99])});
test('stale dynamic recipe refuses original deletion',async()=>{const x=steam();const filter={filterGroups:[{rgOptions:[1,2]}]};x.add('dyn','Dynamic',[],filter);await assert.rejects(put('Dynamic',[99],{id:'dyn',expected_before:[1,2],freeze_dynamic:true,filter:{filterGroups:[{rgOptions:[1]}]},explicit_added:[]}),/differs/);assert(x.state.has('dyn'));assert.deepEqual(ids(x.state.get('dyn')),[1,2]);assert(!x.deleted.includes('dyn'))});
test('save failure leaves original dynamic collection',async()=>{const x=steam();const filter={filterGroups:[{rgOptions:[1,2]}]};x.add('dyn','Dynamic',[],filter);x.setFailSave(c=>c.displayName==='__SCI_test_1');await assert.rejects(put('Dynamic',[99],{id:'dyn',expected_before:[1,2],freeze_dynamic:true,filter}),/injected/);assert(x.state.has('dyn'));assert.deepEqual(ids(x.state.get('dyn')),[1,2]);assert(!x.deleted.includes('dyn'))});
test('unsupported rename keeps original',async()=>{const x=steam();const filter={filterGroups:[{rgOptions:[1,2]}]};x.add('dyn','Dynamic',[],filter);const orig=x.cs.NewUnsavedCollection;x.cs.NewUnsavedCollection=(...a)=>{const c=orig(...a);c.SetName=null;delete c.m_strName;return c};await assert.rejects(put('Dynamic',[99],{id:'dyn',expected_before:[1,2],freeze_dynamic:true,filter}),/rename/);assert(x.state.has('dyn'))});
test('unknown AppIDs, duplicate names, changed membership and hidden writes stop',async()=>{let x=steam();await assert.rejects(put('New',[500]),/Unresolvable/);assert.equal(x.writes,0);await assert.rejects(put('Old',[1],{expected_before:[2]}),/changed/);assert.equal(x.writes,0);x.add('duplicate','Old',[3]);await assert.rejects(put('Old',[1]),/Duplicate/);assert.equal(x.writes,0);await assert.rejects(put('Hidden',[1],{id:'hidden',expected_before:[2,99]}),/system/);assert.equal(x.writes,0)});
test('empty old user collection is explicitly removed, favorite remains',async()=>{const x=steam();await put('Old',[],{id:'old',expected_before:[1,2,99]});assert(!x.state.has('old'));await put('Favorites',[],{id:'favorite',expected_before:[1,99]});assert(x.state.has('favorite'));assert.deepEqual(ids(x.state.get('favorite')),[])});
test('snapshot reports last played and dynamic filter without changing state',async()=>{const x=steam();x.add('dyn','Dynamic',[],{filterGroups:[]});const s=await fn({op:'snapshot'});assert.equal(s.apps.find(a=>a.appid===2).last_played,12345);assert(s.collections.find(c=>c.id==='dyn').dynamic);assert.equal(x.writes,0)});

test('favorite recreation uses system flag API when empty view is absent',async()=>{const x=steam();x.state.delete('favorite');x.disk.delete('favorite');const r=await put('Favorites',[1,99],{id:'favorite'});assert(r.system);assert.equal(r.id,'favorite');assert.deepEqual(r.appids,[1,99]);assert.equal(x.collisionDeletes,0)});

test('case-only name collision refuses replacement',async()=>{const x=steam();await assert.rejects(put('OLD',[1]),/name changed/);assert.equal(x.writes,0);assert(x.state.has('old'))});
test('snapshot reads arbitrary user dynamic names and ignores legacy prescribed overlays without creating previews',async()=>{const x=steam();const c=x.add('custom','自定义动态收藏',[],{filterGroups:[{rgOptions:[1,2]}]});c.removed.add(2);let previews=0;const make=x.cs.NewUnsavedCollection;x.cs.NewUnsavedCollection=(...a)=>{previews++;return make(...a)};const s=await fn({op:'snapshot',overlays:[{name:'unused legacy name',filter:{filterGroups:[]}}]});assert.equal(x.writes,0);assert.equal(previews,0);assert.deepEqual(s.collections.find(c=>c.id==='custom').appids,[1]);assert.equal(s.overlay_projections,undefined)});
test('overlay rename keeps ID, filter and manual exclusions, remains dynamic',async()=>{const x=steam();const filter={filterGroups:[{rgOptions:[1,2]}]};const c=x.add('adult','色情内容',[],filter);c.removed.add(2);const r=await put('00-01｜色情内容',[1],{id:'adult',expected_name:'色情内容',expected_before:[1],overlay:true,restore_dynamic:true,filter});assert.equal(r.id,'adult');assert(r.dynamic);assert.deepEqual([...x.state.get('adult').removed],[2]);assert.equal(x.collisionDeletes,0)});
test('new overlay is saved as dynamic and can overlap a static primary',async()=>{const x=steam();const filter={filterGroups:[{rgOptions:[1,2]}]};const r=await put('00-03｜VR 游戏',[1,2],{overlay:true,restore_dynamic:true,filter});assert(r.dynamic);assert.deepEqual(r.appids,[1,2]);assert(x.state.get('old').allApps.some(a=>a.appid===1));x.restart();assert(x.state.get(r.id).bIsDynamic)});
test('recovery can restore a renamed dynamic overlay to its old static kind',async()=>{const x=steam();const filter={filterGroups:[{rgOptions:[1,2]}]};x.add('a','00-03｜VR 游戏',[],filter);const r=await put('Old VR',[1,2],{id:'a',expected_name:'00-03｜VR 游戏',expected_before:[1,2],recovery:true,freeze_dynamic:true});assert(!r.dynamic);assert.equal(r.name,'Old VR');assert.equal(x.collisionDeletes,0)});

function freeLicenseService(x,{paid=[],master=[],result=1}={}){
 const cancelled=[],unregistered=[];
 globalThis.SteamClient={Apps:{RegisterForAppDetails(id,callback){queueMicrotask(()=>callback({unAppID:id,bFreeRemovableLicense:!paid.includes(id),unMasterSubAppID:master.includes(id)?99:0,bIsSubscribedTo:true}));return{unregister(){unregistered.push(id)}}}}};
 const service={async CancelLicenseForApp(transport,body){assert.equal(transport,'test-transport');cancelled.push(body.appid);return{GetEResult:()=>result}}};
 function moduleFactory(){/* CUserAccount_CancelLicenseForApp_Request UserAccount.CancelLicenseForApp#1 */}
 const require=()=>({randomMinifiedExport:service});require.m={randomModuleNumber:moduleFactory};
 globalThis.webpackChunksteamui=[];globalThis.webpackChunksteamui.push=chunk=>{chunk[2](require)};
 globalThis.appDetailsStore={CMInterface(){return{GetServiceTransport(){return 'test-transport'}}}};
 return {cancelled,unregistered};
}
test('free license preview is read-only and uses native paid/master evidence',async()=>{
 const x=steam(),s=freeLicenseService(x,{paid:[3],master:[4]});
 const r=await fn({op:'removal-check',appids:[1,3,4]});
 assert(r.items[0].free_removable);assert(!r.items[1].free_removable);assert.equal(r.items[2].master_appid,99);
 assert.deepEqual(s.cancelled,[]);assert.equal(s.unregistered.length,3);assert.equal(x.writes,0);
});
test('native cancellation refuses unconfirmed paid master or played games and never edits collections',async()=>{
 for(const kind of ['confirmation','paid','master','played','success','server failure']){
  const x=steam(),s=freeLicenseService(x,{paid:kind==='paid'?[1]:[],master:kind==='master'?[1]:[],result:kind==='server failure'?2:1});
  const arg={op:'remove-free-app',appid:kind==='played'?2:1,confirm:kind==='confirmation'?'':'REMOVE_FREE_APP'};
  if(kind==='success'){assert((await fn(arg)).removed);assert.deepEqual(s.cancelled,[1])}
  else {await assert.rejects(fn(arg));assert.equal(s.cancelled.length,kind==='server failure'?1:0)}
  assert.equal(x.writes,0);assert.deepEqual(ids(x.state.get('old')),[1,2,99]);
 }
});
test('snapshot preserves missing versus zero playtime',async()=>{
 const x=steam();x.catalog.get(1).minutes_playtime_forever=0;x.catalog.get(2).minutes_playtime_forever=75;
 const s=await fn({op:'snapshot'});
 assert.equal(s.apps.find(a=>a.appid===1).playtime_minutes,0);assert.equal(s.apps.find(a=>a.appid===2).playtime_minutes,75);assert(!('playtime_minutes' in s.apps.find(a=>a.appid===3)));
});
