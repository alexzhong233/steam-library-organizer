// Runs only inside Steam's SharedJSContext. Steam performs serialization and syncing.
async function steamImporter(a) {
  const cs = globalThis.collectionStore, apps = globalThis.appStore;
  if (!cs || !apps || typeof cs.GetCollection !== 'function' ||
      typeof cs.NewUnsavedCollection !== 'function' || typeof cs.SaveCollection !== 'function') {
    throw Error('Steam collection interface unavailable. Open Library; unsupported versions are not written.');
  }
  const system = id => ['favorite','hidden','uncategorized','local-install','my-games','play-next'].includes(id) || id.startsWith('type-');
  const members = c => Array.from(c.allApps || [], x => x.appid).filter(x => Number.isSafeInteger(x) && x>0).sort((x,y)=>x-y);
  const equal = (x,y) => JSON.stringify([...new Set(x)].sort((a,b)=>a-b)) === JSON.stringify([...new Set(y)].sort((a,b)=>a-b));
  const lookup = id => {try {return cs.GetCollection(id) || null;} catch {return null;}};
  const getAll = () => {
    const map = new Map();
    for (const c of cs.userCollections || []) if(c && typeof c.id==='string') map.set(c.id,c);
    for (const id of ['favorite','hidden']) {const c=lookup(id); if(c) map.set(id,c);}
    return [...map.values()];
  };
  const named = n => getAll().filter(c => String(c.displayName || c.id).toLowerCase()===n.toLowerCase() && !system(c.id));
  const dynamic = c => !!(c.bIsDynamic || c.m_filterSpec || c.internalAppFilter);
  const resolve = ids => ids.map(id => {const v=apps.GetAppOverviewByAppID(id);if(!v || v.appid!==id)throw Error('Unresolvable AppID '+id); return v;});
  const dd = c => {let v;try{v=c.AsDragDropCollection();}catch{} if(!v || typeof v.AddApps!=='function' || typeof v.RemoveApps!=='function')throw Error('Collection is not editable: '+c.id);return v;};
  const save = async c => {if(typeof c.Save==='function') await c.Save(); else await cs.SaveCollection(c);};
  const removeCollection = async c => {
    if(system(c.id))throw Error('Refusing to delete a system collection');
    const d=c.AsDeletableCollection?.();
    if(d && typeof d.Delete==='function') await d.Delete();
    else if(typeof cs.DeleteCollection==='function') await cs.DeleteCollection(c.id);
    else throw Error('Collection delete API unavailable');
    if(lookup(c.id))throw Error('Deletion did not take effect: '+c.id);
  };
  const summarize = c => {
    let editable=false,deletable=false;
    try {const d=c.AsDragDropCollection?.();editable=!!d && typeof d.AddApps==='function' && typeof d.RemoveApps==='function';}catch{}
    try {const d=c.AsDeletableCollection?.();deletable=!!d && typeof d.Delete==='function';}catch{}
    if(c.id==='favorite')editable=typeof cs.SetAppsAsFavorite==='function';
    const f=c.m_filterSpec || c.filterSpec || null;
    let filter=null;try{if(f)filter=JSON.parse(JSON.stringify(f));}catch{}
    return {id:c.id,name:c.displayName||c.id,dynamic:dynamic(c),system:system(c.id),editable,
      deletable:!system(c.id) && (deletable || typeof cs.DeleteCollection==='function'),filter,appids:members(c)};
  };
  if(a.op==='snapshot') {
    const map=new Map();
    const push=c=>{for(const v of c?.allApps || [])if(Number.isSafeInteger(v.appid)&&v.appid>0) map.set(v.appid,{appid:v.appid,name:v.display_name||String(v.appid),last_played:Number(v.rt_last_time_played)||0});};
    push(cs.allAppsCollection);
    for(const id of ['type-games','type-software','type-music','type-videos','type-tools'])push(cs.appTypeCollectionMap?.get(id));
    // Hidden owned apps can be absent from an active library view.
    push(lookup('hidden'));
    if(!map.size)throw Error('Library not loaded. Open Steam Library and retry.');
    return {schema:1,apps:[...map.values()],collections:getAll().map(summarize)};
  }
  if(a.op==='delete') {
    const c=lookup(a.id); if(!c)return {deleted:true};
    if(a.expected_name && (c.displayName||c.id)!==a.expected_name)throw Error('Collection changed since backup');
    await removeCollection(c);return {deleted:true};
  }
  if(a.op!=='put')throw Error('Unknown operation');
  if(!Array.isArray(a.appids)||a.appids.some(x=>!Number.isSafeInteger(x)||x<=0))throw Error('Invalid AppIDs');
  const wanted=resolve(a.appids);
  let c=a.id ? lookup(a.id) : null;
  if(!c) {
    const matches=named(a.name);
    if(matches.length>1)throw Error('Duplicate target collection name: '+a.name);
    c=matches[0] || null;
  }
  if(c && (c.displayName||c.id)!==a.name)throw Error('Collection name changed after preview');
  if(named(a.name).length>1)throw Error('Duplicate collection names');
  if(typeof cs.GetCollectionIDByUserTag==='function') {
    const existing=cs.GetCollectionIDByUserTag(a.name);
    if(typeof existing==='string' && lookup(existing) && existing!==c?.id)throw Error('Steam name lookup collision; re-preview');
  }
  if(c && system(c.id) && c.id!=='favorite')throw Error('Refusing system collection edit');
  if(c && a.expected_before && !equal(members(c),a.expected_before))throw Error('Collection changed after preview: '+a.name);
  if(a.id==='favorite' || c?.id==='favorite') {
    const before=c?members(c):[];
    if(a.expected_before && !equal(before,a.expected_before))throw Error('Favorites changed after preview');
    if(typeof cs.SetAppsAsFavorite!=='function')throw Error('Favorites API unavailable; no favorite edits made');
    const add=a.appids.filter(id=>!before.includes(id)), drop=before.filter(id=>!a.appids.includes(id));
    if(add.length)await cs.SetAppsAsFavorite(add,true);
    if(drop.length)await cs.SetAppsAsFavorite(drop,false);
    const fresh=lookup('favorite');
    if(!equal(fresh?members(fresh):[],a.appids))throw Error('Favorites verification failed');
    return fresh?summarize(fresh):{id:'favorite',name:a.name,system:true,dynamic:false,editable:true,deletable:false,filter:null,appids:[]};
  }
  const restoringDynamic=!!a.restore_dynamic;
  if(dynamic(c||{}) && !restoringDynamic && !a.freeze_dynamic)throw Error('Unexpected dynamic collection; re-preview');
  if(a.id && !c && (a.expected_before||[]).length)throw Error('Original collection missing; re-preview');
  // Validate the cached dynamic recipe before discarding its live counterpart.
  // The temporary proof is covered by the started-operation recovery journal.
  if(c && dynamic(c) && !restoringDynamic) {
    if(!a.filter || !Array.isArray(a.filter.filterGroups))throw Error('Dynamic filter backup unavailable');
    const proofName='__SCI_'+a.run_id+'_'+a.sequence+'_check';
    if(named(proofName).length)throw Error('Unfinished dynamic check; use recovery');
    const proof=cs.NewUnsavedCollection(proofName,a.filter,resolve(a.explicit_added||[]));
    if((a.explicit_removed||[]).length)await dd(proof).RemoveApps(resolve(a.explicit_removed));
    await cs.SaveCollection(proof);
    if(!lookup(proof.id)||!equal(members(proof),members(c)))throw Error('Cached dynamic rule differs from live members; original kept');
    await removeCollection(proof);
  }
  // Empty former user collections can disappear on Save. Delete explicitly so
  // the outcome is journaled and recoverable; system favorites stay intact.
  if(c && !system(c.id) && !a.appids.length && !restoringDynamic){const result=summarize(c);await removeCollection(c);result.appids=[];return result;}

  if(!c && cs.BIsSystemCollectionName?.(a.name))throw Error('Reserved collection name');
  const replaceKind=c && (dynamic(c) || restoringDynamic);
  // Stage a replacement before deleting the old collection. Never create over
  // an existing name: Steam can implicitly delete that same-name collection.
  if(replaceKind || (!c && restoringDynamic)) {
    const temp='__SCI_'+a.run_id+'_'+a.sequence;
    if(named(temp).length)throw Error('Staging name already exists; restore or remove prior unfinished staging first.');
    const initial=resolve(restoringDynamic ? (a.explicit_added || []) : a.appids);
    const stage=cs.NewUnsavedCollection(temp,restoringDynamic ? a.filter : undefined,initial);
    if(!stage)throw Error('New collection API returned no object');
    const rename=typeof stage.SetName==='function' ? n=>stage.SetName(n) : typeof stage.m_strName==='string' ? n=>{stage.m_strName=n;} : null;
    if(!rename)throw Error('Cannot rename staged collection on this Steam version; original kept');
    if(restoringDynamic && (a.explicit_removed||[]).length) {
      await dd(stage).RemoveApps(resolve(a.explicit_removed));
    }
    await cs.SaveCollection(stage);
    // A changed library/filter during restore is an error, never reported as success.
    if(!equal(members(stage),a.appids))throw Error('Staged collection membership differs; original kept, staging remains for recovery: '+temp);
    if(c)await removeCollection(c);
    await rename(a.name);await save(stage);
    const fresh=lookup(stage.id);
    if(!fresh || (fresh.displayName||fresh.id)!==a.name || !equal(members(fresh),a.appids))throw Error('Replacement save failed; use recovery backup');
    return summarize(fresh);
  }
  if(!c) {
    if(cs.BIsSystemCollectionName?.(a.name))throw Error('Reserved collection name');
    c=cs.NewUnsavedCollection(a.name,undefined,wanted);
    await cs.SaveCollection(c);
  } else {
    const d=dd(c),want=new Set(a.appids),old=members(c);
    const add=wanted.filter(v=>!old.includes(v.appid));
    if(add.length)await d.AddApps(add);
    const drop=old.filter(id=>!want.has(id));
    if(drop.length)await d.RemoveApps(resolve(drop));
    await save(c);
  }
  const fresh=lookup(c.id);
  if(!fresh || !equal(members(fresh),a.appids))throw Error('Collection membership verification failed: '+a.name);
  return summarize(fresh);
}
