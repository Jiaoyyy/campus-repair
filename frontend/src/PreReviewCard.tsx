import { useEffect, useMemo, useRef, useState } from 'react';
import { api, categoryNames, statusNames, time, type Order } from './api';
import type { BusinessResult } from './assistant-api';
import { WorklistCard } from './WorklistCard';
import { useActionFeedback } from './ActionFeedback';

import {readView,saveView,useCardView} from './assistant-view';

import {loadDispatchPeople,type DispatchPerson,type DispatchPeople} from './dispatch-people';

type Receipt = Pick<Order,'id'|'version'>;
type Row = BusinessResult['items'][number];
const pageSize = 5;
const labels = {approve:'建议通过',supplement:'建议补充',manual:'优先人工查看'};

function ApprovedBatch({receipts,rows,viewKey,disabled,onOpen}:{receipts:Receipt[];rows:Row[];viewKey?:string;disabled:boolean;onOpen:(id:number)=>void}) {
 const orders=useMemo(()=>receipts.flatMap(receipt=>{
  const row=rows.find(o=>o.id===receipt.id);
  if(!row)return [];
  const status=row.current_status??row.status;
  return [{...row,status,version:row.current_version===receipt.version?receipt.version:0}];
 }),[receipts,rows]);
 const [loaded,setLoaded]=useState<{result:BusinessResult;people:DispatchPeople}|null>(null);
 const [error,setError]=useState('');
 const [reload,setReload]=useState(0);
 useEffect(()=>{
  let live=true;
  async function load(){
   setError('');
   const categories=[...new Set(orders.map(o=>o.category))];
   const peopleKey=viewKey && viewKey+'/candidates';
   const picks=readView<Record<number,number>>(peopleKey,{});
   const people=await loadDispatchPeople(categories);
   const planned=new Map<number,number>();
   const loadFor=(t:DispatchPerson)=>t.assigned_count+t.in_progress_count+(planned.get(t.id)??0);
   const items=orders.map(o=>{
    const list=people[o.category]??[];
    const candidate=picks[o.id] ? list.find(person=>person.id===picks[o.id]) : list.slice().sort((a,b)=>loadFor(a)-loadFor(b)||a.id-b.id)[0];
    if(candidate)picks[o.id]=candidate.id;
    if(!candidate)return {...o,advice:'暂无符合该类别的维修人员，请打开工单安排。'};
    planned.set(candidate.id,(planned.get(candidate.id)??0)+1);
    return {...o,recommended:{id:candidate.id,display_name:candidate.display_name,pending_count:candidate.assigned_count,in_progress_count:candidate.in_progress_count}};
   });
   if(live)saveView(peopleKey,picks);
   if(live)setLoaded({people,result:{kind:'worklist',query:{status:'unassigned'},total:items.length,items,queried_at:new Date().toISOString(),time_basis:'',statuses:{},historical:false}});
  }
  void load().catch(e=>{if(live)setError(e.message)});
  return ()=>{live=false};
 },[orders,reload,viewKey]);
 if(error)return <p role="alert">审核已通过，读取维修人员失败。<button onClick={()=>setReload(n=>n+1)}>重试人选查询</button></p>;
 if(!loaded)return <p role="status">审核已通过，正在查询匹配的维修人员…</p>;
 return <WorklistCard viewKey={viewKey} result={loaded.result} people={loaded.people} approvedBatch disabled={disabled} onOpen={onOpen} onRefresh={()=>setReload(n=>n+1)}/>;
}

export function PreReviewCard({result,pageUrl,disabled,onOpen,onReview,viewKey}:{viewKey?:string;onReview?:(id:number,number:string)=>void;result:BusinessResult;pageUrl?:string;disabled:boolean;onOpen:(id:number)=>void}) {
 const [refreshed,setRefreshed]=useState<BusinessResult|null>(null);
 useEffect(()=>{setRefreshed(null);setOutcomes({});},[result]);
 const data=refreshed??result;
 const [savedPage,setPage]=useCardView(viewKey,'page',1);
 const pages=Math.max(1,Math.ceil(data.items.length/pageSize));
 const page=Math.min(pages,Math.max(1,savedPage));
 const visible=data.items.slice((page-1)*pageSize,page*pageSize);
 const [checked,setChecked]=useCardView<number[]>(viewKey,'checked',[]);
 const [outcomes,setOutcomes]=useState<Record<number,string>>({});
 const [batches,setBatches]=useCardView<Receipt[][]>(viewKey,'batches',[]);
 const passedHere=useMemo(()=>new Set(batches.flat().map(receipt=>receipt.id)),[batches]);
 const [busy,setBusy]=useState(false),[error,setError]=useState('');
 const lock=useRef(false);
 const feedback=useActionFeedback();
 const eligible=(o:Row)=>(o.current_status??o.status)==='submitted' && !!o.version && (o.current_version??o.version)===o.version && o.review?.decision==='approve' && !o.review_address_missing && !outcomes[o.id];
 const selected=data.items.filter(o=>eligible(o)&&checked.includes(o.id));
 async function refresh(){
  if(lock.current||disabled||!pageUrl)return;
  lock.current=true;setBusy(true);setError('');
  try{
   const next=await api<BusinessResult>(pageUrl+'&refresh=1');
   setChecked(ids=>next.items.filter(o=>eligible(o)&&ids.includes(o.id)).map(o=>o.id));
   setRefreshed(next);setOutcomes({});
  }catch(e){setError((e as Error).message)}
  finally{lock.current=false;setBusy(false)}
 }
 async function approve(){
  if(lock.current||disabled||!selected.length)return;
  lock.current=true;setBusy(true);setError('');
  const passed:Order[]=[];
  try{
   for(const o of selected){
    let text:string;
    try{
     const {order}=await api<{order:Order}>(`/api/work-orders/${o.id}/review`,{method:'POST',body:JSON.stringify({version:o.version,decision:'approve'})});
     passed.push(order);text='本次审核已通过';
     const receipt={id:order.id,version:order.version};
     const batchId=passed[0].id;
     setBatches(prior=>prior.some(batch=>batch[0].id===batchId)
      ?prior.map(batch=>batch[0].id===batchId?[...batch,receipt]:batch)
      :[...prior,[receipt]]);
     setRefreshed(prior=>{const card=prior??result;return {...card,items:card.items.map(item=>item.id===order.id?{...item,current_status:order.status,current_version:order.version}:item)};});
    }catch(e){text=(e as Error).message+'。请更新工单状态后核对。'}
    setOutcomes(prior=>({...prior,[o.id]:text}));
   }
   const remaining=selected.length-passed.length;
   feedback({title:`本次审核通过 ${passed.length} 单`,detail:remaining?`${remaining} 单需核对，详见逐单结果。`:'请核对下方派单建议。',warning:remaining>0});
   setChecked([]);
  }finally{lock.current=false;setBusy(false)}
 }
 return <section className="business-result pre-review" aria-label="待审核工单初审">
  <div className="section-heading"><h3>工单初审</h3><span>共 {data.total} 单</span></div>
  <p className="field-help">初审于 {time(data.queried_at)}{data.total>data.items.length && ` · 此记录可查看 ${data.items.length} 单。`}</p>
  <div className="dispatch-suggestions">{visible.map(o=><article key={o.id} className="dispatch-suggestion">
   <label className="review-choice">{eligible(o)&&<input type="checkbox" disabled={busy||disabled} checked={checked.includes(o.id)}
      onChange={e=>setChecked(ids=>e.target.checked?[...ids,o.id]:ids.filter(id=>id!==o.id))} aria-label={'选择通过'+o.location}/>}
    <strong>{o.location}</strong><span className="badge" data-decision={o.review?.decision??'manual'}>{labels[o.review?.decision??'manual']}</span></label>
   <p>{o.description}</p><small>当前状态：{statusNames[o.current_status??o.status]??o.status} · {categoryNames[o.category]} · {o.number}</small>
   <p>{o.review?.reason??'请打开工单核实。'}</p>
   {o.current_status==='submitted' && o.review_address_missing && o.review?.decision==='approve' && <small>原初审建议通过，当前地址缺少可定位的楼栋，请在详情中要求补充。</small>}
   {o.current_status==='submitted' && o.current_version!==o.version && <small>工单已更新，请打开详情核对后审核。</small>}
   {o.review?.question&&<p>建议追问：{o.review.question}</p>}
   {(outcomes[o.id] || (passedHere.has(o.id) && (o.current_status??o.status)!=='submitted')) && <strong role="status">{outcomes[o.id]??'本次审核已通过'}</strong>}
   <div className="review-links"><button type="button" className="text-button" disabled={busy||disabled} onClick={()=>onOpen(o.id)}>查看工单</button>
   {onReview&&<button type="button" className="text-button" disabled={busy||disabled} onClick={()=>onReview(o.id,o.number)}>追问这单</button>}</div>
  </article>)}</div>
  <div className="review-controls">
   {(visible.some(eligible)||selected.length>0)&&<button className="primary" disabled={busy||disabled||!selected.length} onClick={()=>void approve()}>确认通过所选工单（{selected.length}）</button>}
   {pageUrl&&<button disabled={busy||disabled} onClick={()=>void refresh()}>更新工单状态</button>}
  </div>
  {pages>1&&<nav className="query-pager" aria-label="初审分页"><button disabled={busy||disabled||page<=1} onClick={()=>setPage(page-1)}>上一页</button><span>第 {page} / {pages} 页</span><button disabled={busy||disabled||page>=pages} onClick={()=>setPage(page+1)}>下一页</button></nav>}
  {busy&&<p role="status">正在处理…</p>}{error&&<p role="alert">{error}</p>}
  {batches.map(orders=><ApprovedBatch key={orders[0].id} receipts={orders} rows={data.items} viewKey={viewKey && viewKey+'/batch/'+orders[0].id} disabled={busy||disabled} onOpen={onOpen}/>)}
 </section>;
}
