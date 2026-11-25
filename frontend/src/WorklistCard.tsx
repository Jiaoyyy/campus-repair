import { useActionFeedback } from './ActionFeedback';
import { useEffect, useRef, useState } from 'react';
import { api, APIError, categoryNames, statusNames, time, type Detail } from './api';
import type { BusinessResult } from './assistant-api';

import {useCardView} from './assistant-view';
import {loadDispatchPeople,type DispatchPeople} from './dispatch-people';
import {Select} from './Select';
type Outcome = { assigned: boolean; stale?:boolean; text: string };
type WorklistPage = { items: BusinessResult['items']; page: number; total: number; supplemental: boolean };

export function WorklistCard({ result, disabled, onOpen, onRefresh, pageUrl, viewKey, people, approvedBatch = false }: {
 result: BusinessResult; pageUrl?:string; viewKey?:string; people?:DispatchPeople; disabled: boolean; onOpen: (id: number) => void; onRefresh?: () => void; approvedBatch?: boolean;
}) {
 const [excluded, setExcluded] = useCardView<number[]>(viewKey,'excluded',[]);
 const [outcomes, setOutcomes] = useCardView<Record<number, Outcome>>(viewKey,'outcomes',{});
 const [sending, setSending] = useState(false);
 const feedback = useActionFeedback();
 const lock = useRef(false);
 const [choices,setChoices]=useCardView<Record<number,number>>(viewKey,'chosen-technicians',{});
 const [editing,setEditing]=useState<number|null>(null);
 const [pool,setPool]=useState<DispatchPeople>({});
 const [loadError,setLoadError]=useState('');
 const [reload,setReload]=useState(0);
 const [savedPage,savePage]=useCardView(viewKey,'worklist-page',1);
 const [requested,setRequested]=useState(savedPage>1?savedPage:0);
 const [loaded,setLoaded]=useState<WorklistPage>();
 const [loadingPage,setLoadingPage]=useState(!!pageUrl && requested>0);
 const [pageError,setPageError]=useState('');
 const [pageRetry,setPageRetry]=useState(0);
 const items=loaded?.items??result.items;
 const page=loaded?.page??1;
 const pages=Math.max(1,Math.ceil((loaded?.total??result.total)/5));
 const paginated=!!pageUrl && result.total>5;
 const blocked=disabled||sending||loadingPage;
 useEffect(()=>{
  if(!paginated || !pageUrl || !requested)return;
  const abort=new AbortController();
  const target=new URL(pageUrl,window.location.origin);
  target.searchParams.set('page',String(requested));
  setPageError('');
  setLoadingPage(true);
  api<WorklistPage>(target.pathname+target.search,{signal:abort.signal}).then(value=>{
   if(abort.signal.aborted)return;
   setLoaded(value);
   savePage(value.page);
  }).catch(error=>{
   if(!abort.signal.aborted)setPageError(error.message);
  }).finally(()=>{
   if(!abort.signal.aborted)setLoadingPage(false);
  });
  return ()=>abort.abort();
 },[pageUrl,paginated,requested,pageRetry,savePage]);
 function goToPage(next:number) {
  setEditing(null);
  setRequested(next);
  setPageRetry(n=>n+1);
 }
 const categories=onRefresh ? [...new Set(items.filter(o=>o.status==='unassigned' && o.version && (o.current_version??o.version)===o.version).map(o=>o.category))].sort().join(',') : '';
 useEffect(()=>{
  if(people || !categories)return;
  let live=true;
  setPool({});setLoadError('');
  loadDispatchPeople(categories.split(',')).then(v=>{if(live)setPool(v)}).catch(e=>{if(live)setLoadError(e.message)});
  return ()=>{live=false};
 },[categories,people,reload]);
 const lists=people??pool;
 const rows=items.map(order=>{
  const changed=(order.current_status??order.status)!==order.status || (order.current_version??order.version)!==order.version;
  const editable=!!(onRefresh && order.status==='unassigned' && order.version && !changed && !outcomes[order.id]?.assigned && !outcomes[order.id]?.stale);
  const options=lists[order.category];
  const chosen=choices[order.id]??order.recommended?.id;
  const person=options?.find(p=>p.id===chosen);
  const candidate=person ? {id:person.id,display_name:person.display_name,pending_count:person.assigned_count,in_progress_count:person.in_progress_count}
   : !editable && chosen===order.recommended?.id ? order.recommended : undefined;
  return {order,editable,options,candidate,changed};
 });
 const eligible=rows.filter(row=>row.editable && row.candidate);
 const selected=eligible.filter(({order})=>!excluded.includes(order.id));
 const legacy = items.some(o => o.status === 'unassigned' && !o.recommended && o.advice?.startsWith('建议 '));

 async function confirm() {
  if (blocked || lock.current || !selected.length) return;
  lock.current = true;
  setSending(true);
  feedback(null);
  let assigned = 0;
  try {
   for (const {order,candidate} of selected) {
    const technician = candidate!;
    let outcome: Outcome;
    try {
     await api('/api/work-orders/'+order.id+'/assign', {method:'POST', body:JSON.stringify({version:order.version, technician_id:technician.id})});
     outcome = {assigned:true,text:'已派给'+technician.display_name};
    } catch (error) {
     // A response may have been lost after commit. Read the current order before offering retry.
     const current = await api<Detail>('/api/work-orders/'+order.id).catch(()=>null);
     if (!(error instanceof APIError && error.status<500) && current?.order.assignee_id===technician.id && current.order.status!=='unassigned') {
      outcome={assigned:true,text:'当前已指派给'+technician.display_name};
     } else {
      outcome={assigned:false,stale:!!current && (current.order.status!=='unassigned' || current.order.version!==order.version),text:error instanceof Error ? error.message : '提交结果未知，请打开工单核对'};
     }
    }
    if (outcome.assigned) assigned++;
    setOutcomes(prior=>({...prior,[order.id]:outcome}));
   }
   const remaining = selected.length - assigned;
   feedback({title:`已确认派单 ${assigned} 单`, detail:remaining ? `${remaining} 单需核对，请查看逐单结果。` : '等待维修人员接单。',warning:remaining > 0});
  } finally { lock.current=false; setSending(false); }
 }

 if (!items.length && !paginated) return null;
 return <section className="business-result" aria-label="逐单办理建议">
  <header className="worklist-heading">
   <h3>{approvedBatch?'本次通过的工单 · 派单建议':result.query.status==='unassigned'?'派单建议':'工单处理建议'}</h3>
   {!approvedBatch && <p className="field-help">原查询本组织{result.query.status==='unassigned'?'待派':'未结'}工单共 {result.total} 单 · {result.time_basis} · 查询于 {time(result.queried_at)}</p>}
   {eligible.length>0 && <p className="field-help">人员状态按在手工单统计，出勤待核实。</p>}
  </header>
  {paginated && <nav className="query-pager" aria-label="逐单建议分页">
   <button type="button" disabled={blocked||page<=1} onClick={()=>goToPage(page-1)}>上一页</button>
   <span aria-live="polite">{loadingPage?'正在读取…':`第 ${page} / ${pages} 页`}</span>
   <button type="button" disabled={blocked||page>=pages} onClick={()=>goToPage(page+1)}>下一页</button>
  </nav>}
  {pageError && <p role="alert">{pageError} <button type="button" disabled={blocked} onClick={()=>setPageRetry(n=>n+1)}>重试翻页</button></p>}
  {loaded?.supplemental && <p className="field-help">此页按原查询范围补查。请打开工单核对当前状态及办理。</p>}
  {result.warning && <p className="field-help" role="status">{result.warning}</p>}
  {loadError && <p role="alert">{loadError} <button type="button" onClick={()=>setReload(n=>n+1)}>重试读取人选</button></p>}
  {!items.length && <p className="field-help">本页暂无可查看的工单。</p>}
  <div className="dispatch-suggestions">{rows.map(({order,editable,options,candidate,changed})=>{
   const selectable=editable && !!candidate;
   return <article key={order.id} className="dispatch-suggestion">
    {candidate && <label className="dispatch-person">
     {selectable && <input type="checkbox" checked={!excluded.includes(order.id)} disabled={blocked}
      aria-label={'选择'+order.location+'派给'+candidate.display_name} onChange={e=>setExcluded(ids=>e.target.checked?ids.filter(id=>id!==order.id):[...ids,order.id])}/>}
     <span className="dispatch-name"><small>{choices[order.id]?'所选维修人员':'推荐维修人员'}</small><strong>{candidate.display_name}</strong></span>
     <span className="dispatch-load">{candidate.in_progress_count>0?'处理中 '+candidate.in_progress_count+' 单':candidate.pending_count>0?'暂无处理中工单':'暂无在手工单'}{candidate.pending_count>0?' · 待接 '+candidate.pending_count+' 单':''}</span></label>}
    {editable && options && !candidate && <small>{options.length?'当前人选不可用，请重新选择。':'暂无符合类别的可派人员。'}</small>}
    {editable && !options && !loadError && <small role="status">正在核对可派人员…</small>}
    {editable && !!options?.length && (editing===order.id
     ? <Select aria-label={'选择'+order.location+'的维修人员'} value={candidate?.id??''} disabled={blocked} onChange={e=>{
        const id=Number(e.target.value);if(!options.some(p=>p.id===id))return;
        setChoices(prior=>({...prior,[order.id]:id}));setEditing(null);
       }}>
        {!candidate && <option value="" disabled>请选择维修人员</option>}
        {options.map(p=><option key={p.id} value={p.id}>{p.display_name} · 待接 {p.assigned_count} 单 · 处理中 {p.in_progress_count} 单</option>)}
       </Select>
     : <button type="button" className="text-button" disabled={blocked} onClick={()=>setEditing(order.id)}>{candidate?'更换人选':'选择人选'}</button>)}
    <button type="button" className="text-button" disabled={blocked} onClick={()=>onOpen(order.id)}>{order.location} · {categoryNames[order.category]} ›</button>
    <small>{order.number} · {order.current_status?'当前':'查询时'}{statusNames[order.current_status??order.status]}</small>
    {changed && order.version && !outcomes[order.id] && <small>工单已变更，请打开详情核对。</small>}
    <span>{order.description}</span>
    {order.priority && <strong>{order.priority}</strong>}
    {!candidate && !editable && !outcomes[order.id] && <small>{order.advice}</small>}
    {outcomes[order.id] && <strong role="status">{outcomes[order.id].text}</strong>}
   </article>;
  })}</div>
  {eligible.length>0 && <button type="button" className="primary" disabled={blocked||!selected.length} onClick={()=>void confirm()}>{sending?'正在派单…':'确认派单（'+selected.length+' 单）'}</button>}
  {onRefresh && (legacy || Object.values(outcomes).some(o=>!o.assigned)) && <button type="button" disabled={blocked} onClick={onRefresh}>重新获取派单建议</button>}
 </section>;
}
