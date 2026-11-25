import { useState } from "react";
import { PreReviewCard } from "./PreReviewCard";
import { WorklistCard } from "./WorklistCard";
import { api, categoryNames, statusNames, time } from './api';
import type { BusinessResult, OrderDetailResult, TechnicianHandoff, RequesterHandoff } from './assistant-api';
import { Badge, Icon } from './ui';
export function BusinessResultCard({ result, viewKey, onReview,onCompletion, onRequesterFollowup, pageUrl, disabled, onOpen, onQuestion, onManual, onRefreshDispatch, canPage = false }:  {viewKey?:string;onReview?:(id:number,number:string)=>void;result: BusinessResult; onCompletion?: (draft:TechnicianHandoff)=>void; onRequesterFollowup?: (draft:RequesterHandoff)=>void; pageUrl?:string; disabled: boolean; onOpen: (id:number,section?:'visit')=>void; onQuestion: (q:string)=>void;onManual?:()=>void;canPage?:boolean;onRefreshDispatch?:()=>void}) {
 if(result.unavailable) return <p className="assistant-notice">这张工单当前不可查看。</p>;
 if (result.kind === 'prepare_requester_followup') return <RequesterFollowupCard result={result} disabled={disabled} onChoose={onRequesterFollowup} onOpen={onOpen}/>;
 if (result.kind === 'prepare_completion' || result.kind === 'prepare_wait_parts') return <CompletionCard result={result} disabled={disabled} onCompletion={onCompletion}/>;
 if (result.kind === 'discuss_pre_review') return null;
 if (result.kind === 'pre_review') return <PreReviewCard viewKey={viewKey} onReview={onReview} result={result} pageUrl={pageUrl} disabled={disabled || !onRefreshDispatch} onOpen={onOpen}/>;
 if (result.kind === 'worklist') return <WorklistCard viewKey={viewKey} result={result} pageUrl={pageUrl} disabled={disabled} onOpen={onOpen} onRefresh={onRefreshDispatch}/>;
 if(result.detail) return <OrderDetailCard result={result} detail={result.detail} disabled={disabled} onOpen={onOpen}/>;
 if(result.kind==='analysis' && result.analysis) return <AnalysisResultCard result={result} canPage={canPage} disabled={disabled} onOpen={onOpen} onQuestion={onQuestion}/>;
 if(result.kind !== 'candidates') return <OrderQueryCard pageUrl={pageUrl} result={result} disabled={disabled} onOpen={onOpen}/>;
 return <section className="business-result" aria-label="业务查询结果">
  <div className="section-heading"><h3>可派人员</h3><span className="badge">只读查询</span></div>
  <p className="field-help">查询于 {time(result.queried_at)} · 查询时的快照，最新进度请打开工单核对。</p>
  <div className="candidate-results">{result.candidates?.map((c,i)=><div key={c.id} className="candidate-result"><div><strong>{c.display_name}</strong>{i===0&&<span className="badge">可优先考虑</span>}</div><small>{c.categories.map(k=>categoryNames[k]).join('、')} · 待接单 {c.pending_count} · 处理中 {c.in_progress_count}</small></div>)}</div>
  <p className="field-help">按当前待接单与处理中总量排序，实际安排还需你在工单页面确认。</p>
  {onManual&&<button type="button" disabled={disabled} onClick={onManual}>前往报修管理</button>}
 </section>;
}

function RequesterFollowupCard({result, disabled, onChoose, onOpen}: {
 result:BusinessResult; disabled:boolean; onChoose?: (draft:RequesterHandoff)=>void; onOpen:(id:number)=>void;
}) {
 const draft=result.followup;
 if (!draft) return null;
 const recurrence=draft.decision==='recurrence';
 const title=recurrence?'故障反馈草稿':'核对维修结果';
 return <section className="business-result" aria-label={title}>
  <h3>{title}</h3>
  {recurrence && draft.reason && <p><strong>反馈现象</strong> · {draft.reason}</p>}
  {result.items.map(order=>{
   const action=recurrence?(order.status==='closed'?'recurrence':'reopen'):'confirm';
   const label=action==='recurrence'?'填写关联报修':action==='reopen'?'核对并填写未解决反馈':'核对并前往确认';
   const record=order.latest_repair;
   return <article key={order.id} className="business-result requester-followup-order">
    <div className="section-heading"><strong>{order.location}</strong><Badge status={order.status}/></div>
    <small>{order.number} · {order.description}</small>
    {record && <dl className="visit-facts"><dt>处理措施</dt><dd>{record.measures}</dd><dt>维修结果</dt><dd>{record.result}</dd></dl>}
    {order.recurrence_exists ? <button type="button" disabled={disabled} onClick={()=>onOpen(order.id)}>查看已关联的复发报修</button> : order.version && record && (!recurrence || draft.reason) && onChoose
     ? <button className="primary" type="button" disabled={disabled} onClick={()=>onChoose({
      id:order.id,version:order.version!,number:order.number,category:order.category,location:order.location,reason:draft.reason??'',action,
     })}>{label}</button>
     : !order.version && <p className="muted small">工单已更新，请重新核对。</p>}
   </article>;
  })}
 </section>;
}

function CompletionCard({result, disabled, onCompletion}: {
 result:BusinessResult; disabled:boolean; onCompletion?: (draft:TechnicianHandoff)=>void;
}) {
 const draft = result.completion, parts = result.parts;
 const resumeAt = parts?.resume_at;
 const partsReady = parts?.reason && resumeAt && Date.parse(resumeAt) > Date.now();
 const title = parts ? '待备件说明草稿' : '完工上报草稿';
 return <section className="business-result" aria-label={title}>
  <div className="section-heading"><h3>{title}</h3><span className="badge">待核对</span></div>
  <dl className="visit-facts">
   <dt>{parts ? '等待原因' : '处理措施'}</dt><dd>{(parts ? parts.reason : draft?.measures) || '待补充'}</dd>
   <dt>{parts ? '预计恢复时间' : '维修结果'}</dt><dd>{(parts ? resumeAt && `${time(resumeAt)}（北京时间）` : draft?.result) || '待补充'}</dd>
  </dl>
  {resumeAt && Date.parse(resumeAt) <= Date.now() && <p className="muted small">预计时间已过，请重新准备说明。</p>}
  {result.total > 0 && result.total <= 5 && <ul className="business-orders">
   {result.items.map(order=>{
    const target = {id:order.id,version:order.version ?? 0};
    const handoff = parts && partsReady && resumeAt ? {...target,reason:parts.reason,resume_at:resumeAt}
     : draft?.measures && draft.result ? {...target,...draft} : undefined;
    return <li key={order.id}>
    <div><strong>{order.location}</strong><p className="muted small">{order.number} · {order.description}</p></div>
    {handoff && order.version && onCompletion
     ? <button type="button" className="primary" disabled={disabled} onClick={()=>onCompletion(handoff)}>{parts ? '核对并填写待备件表单' : '核对并填写完工表单'}</button>
     : !order.version && <p className="muted small">工单已更新，请重新核对。</p>}
   </li>})}
  </ul>}
 </section>;
}

const queueNames: Record<string, string> = { action: '待你处理', active: '未结束', finished: '已结束' };

function OrderQueryCard({ result, pageUrl, disabled, onOpen }: { pageUrl?:string; result: BusinessResult; disabled: boolean; onOpen: (id: number,section?:'visit') => void }) {
 type LivePage = {items:BusinessResult['items'];total:number;page:number};
 const [live,setLive]=useState<LivePage|null>(null);
 const [loading,setLoading]=useState(false),[error,setError]=useState('');
 const page=live?.page??1, total=live?.total??result.total, rows=live?.items??result.items;
 async function loadPage(n:number){
  if(!pageUrl || loading)return;
  setLoading(true);setError('');
  try {setLive(await api<LivePage>(pageUrl+'&page='+n));}
  catch(e){setError(e instanceof Error?e.message:'读取工单失败');}
  finally{setLoading(false);}
 }
 if (result.total === 0) return result.warning ? <p className="assistant-notice" role="status">{result.warning}</p> : null;
 const { query } = result;
 const visits=query.date_basis==='visit';
 const filters = [
  query.location && `位置含「${query.location}」`,
  query.keyword && `描述含「${query.keyword}」`,
  query.category && categoryNames[query.category],
  query.status ? statusNames[query.status] : query.queue && queueNames[query.queue],
  query.number,
  ((query.period && query.period !== 'all') || query.date_from) && result.time_basis,
 ].filter(Boolean);
 const statuses = Object.entries(statusNames).filter(([key]) => result.statuses[key] > 0);
 const previewCount = result.items.length;

 return <section className="business-result order-query-result" aria-label="业务查询结果">
  <div className="order-query-heading">
   <h3 className="business-total">{visits?'上门日程':'匹配工单'} <span><strong>{result.total}</strong> 单</span></h3>
   <time className="order-query-time" dateTime={result.queried_at}>查询于 {time(result.queried_at)}</time>
  </div>
  {filters.length > 0 && <p className="order-query-filters">{filters.join(' · ')}</p>}
  {statuses.length > 0 && <ul className="order-query-statuses" aria-label="状态分布">
   {statuses.map(([key, label]) => <li key={key}>{label} <strong>{result.statuses[key]}</strong></li>)}
  </ul>}
  {result.warning && <p className="assistant-notice" role="status">{result.warning}</p>}
  {(previewCount > 0 || result.total>5 && pageUrl) ? <details className="business-order-disclosure" open={visits||undefined}>
   <summary>
    <span className="business-order-disclosure-label"><Icon kind="orders"/>{visits?'查看上门安排':'查看相关工单'}</span>
    {previewCount < result.total && <span className="business-order-disclosure-count">每页 5 单</span>}
    <Icon kind="arrow"/>
   </summary>
   <ul className="business-orders business-order-preview">
    {rows.map(o => <li key={o.id}><button className="business-order" type="button" disabled={disabled} onClick={() => onOpen(o.id,visits?'visit':undefined)}>
     <span><strong>{o.location}</strong>{visits&&o.visit_start&&o.visit_end&&<small>预约 {time(o.visit_start)} — {time(o.visit_end)}</small>}{o.next_step ? <small>{o.next_step.reason}</small> : o.description && <small>{o.description}</small>}<small>{o.number} · {categoryNames[o.category]}</small></span>
     <Badge status={o.status}/>
    </button></li>)}
   </ul>
   {pageUrl && result.total>5 && <nav className="query-pager" aria-label="相关工单分页">
    <button type="button" disabled={disabled||loading||page<=1} onClick={()=>void loadPage(page-1)}>上一页</button>
    <small aria-live="polite">{loading?'读取中…':(live?'当前匹配 '+total+' 单 · ':'')+'第 '+page+' / '+Math.max(1,Math.ceil(total/5))+' 页'}</small>
    <button type="button" disabled={disabled||loading||page*5>=total} onClick={()=>void loadPage(page+1)}>下一页</button>
   </nav>}
   {error && <p role="alert">{error}</p>}
  </details> : result.total > 0 && <p className="order-query-filters">这次查询没有可查看的工单明细。</p>}
 </section>;
}

function AnalysisResultCard({result,disabled,onOpen,onQuestion,canPage}:{result:BusinessResult;canPage:boolean;disabled:boolean;onOpen:(id:number)=>void;onQuestion:(q:string)=>void}) {
 const a=result.analysis!;
 const ids=result.query.analysis==='no_show'?['no_show','exception_total']:result.query.analysis==='exceptions'?['exception_total','overdue','reschedule','parts']:result.query.analysis==='frequent'?['category:top','symptom:top','location:top']:['category:top','exception_total','aged'];
 function concise(f:typeof a.facts[number]) {
  if(f.id==='no_show')return `${f.count} 次，涉及 ${a.facts.find(x=>x.id==='no_show_orders')?.count||0} 单。`;
  if(f.id==='exception_total')return `当前 ${f.count} 单（已去重）。`;
  if(f.id==='aged')return `${f.count} 单创建已满7天且未结束。`;
  const groups=f.id==='category:top'?a.categories:f.id==='symptom:top'?a.symptoms:f.id==='location:top'?a.locations:null;
  if(groups){const top=groups.filter(g=>g.count===f.count);return `${top.map(g=>[...g.label].slice(0,70).join('')+([...g.label].length>70?'…':'')).join('、')} · ${top.length>1?'各 ':''}${f.count} 单${top.length>1?'（并列）':''}`;}
  return f.text;
 }
 const findings=ids.map(id=>a.facts.find(f=>f.id===id)).filter(f=>!!f);
 return <section className="business-result insight-result" aria-label="问题分析结果">
  <div className="section-heading"><h3>{a.title}</h3><time>{time(result.queried_at)}</time></div>
  <p className="insight-scope">{a.scope} · {result.time_basis}</p>
  <div className="insight-findings">{findings.map(f=><div className={`insight-finding ${f.count?'has-issue':''}`} key={f.id}><strong>{f.title}</strong><p>{concise(f)}</p>{f.time_basis&&<small>{f.time_basis}</small>}</div>)}</div>
  {result.total===0&&<p>这个时间范围内的工单总数为 0。当前异常及积压采用各自标注的范围。</p>}
  {result.warning&&<p className="assistant-notice" role="status">{result.warning}</p>}
  {!result.natural&&(a.model_status==='ready'&&a.notes.length>0?<section className="insight-advice" aria-label="分析建议"><h4>建议关注</h4>{a.notes.map((note,i)=><div key={i}><p>{note.recommendation}</p><small>依据：{note.evidence.map(id=>a.facts.find(f=>f.id===id)?.title).filter(Boolean).join('、')}</small></div>)}<small>建议待现场核实。</small></section>:!result.warning&&<p className="assistant-notice" role="status">{a.warning||'本轮仅展示统计，未生成模型建议。'}</p>)}
  <details className="insight-details"><summary>查看统计明细</summary><p>{result.total} 单纳入本次分析；排名最多列5项。上门异常允许重叠，当前阻塞和积压不限创建日期；积压关注线不代表校方维修时限。</p><dl className="insight-fact-details">{a.facts.filter(f=>!ids.includes(f.id)&&f.id!=='total').map(f=><div key={f.id}><dt>{f.title}</dt><dd>{f.text}{f.time_basis&&<small> {f.time_basis}</small>}</dd></div>)}</dl>{[{title:'类别',items:a.categories},{title:'准确位置',items:a.locations},{title:'相同故障描述',items:a.symptoms}].map(group=><div key={group.title}><h4>{group.title}</h4><ol>{group.items.map((g,i)=><li key={i}><span>{g.label}</span><strong>{g.count} 单</strong></li>)}</ol></div>)}</details>
  {Object.entries(a.related || {}).map(([id, page]) => page.items.length>0 && <details className="insight-details" key={id}>
   <summary>{a.facts.find(f=>f.id===id)?.title || id} · {page.total} 单</summary>
   {page.next_cursor&&<p className="muted small">本页显示 {page.items.length} 单。 {canPage&&<button type="button" className="text-button" disabled={disabled} onClick={()=>onQuestion(`查看${a.facts.find(f=>f.id===id)?.title || id}的下一页工单`)}>继续查下一页</button>}</p>}
   <div className="business-orders">{page.items.map(order=><button type="button" className="business-order" key={order.id} disabled={disabled} onClick={()=>onOpen(order.id)}>
    <span><strong>{order.location}</strong><small>{order.number}{order.responsible&&` · ${order.responsible}`}</small></span><Badge status={order.status}/>
   </button>)}</div>
  </details>)}
  {result.items.length>0&&<details className="insight-details"><summary>相关工单 · {result.items.length}</summary><p className="muted small">最近更新的示例；排名按全部匹配记录统计。</p><div className="business-orders">{result.items.map(o=><button className="business-order" key={o.id} disabled={disabled} onClick={()=>onOpen(o.id)}><span><strong>{o.location}</strong><small>{o.description}</small><small>{o.number}</small></span><Badge status={o.status}/></button>)}</div></details>}
  <div className="business-followups">{['总结当前有哪些问题','最近出现最多的问题是什么','现在有几单白跑了'].filter(q=>result.query.analysis!== (q.includes('白跑')?'no_show':q.includes('最多')?'frequent':'overview')).map(q=><button key={q} disabled={disabled} onClick={()=>onQuestion(q)}>{q}</button>)}</div>
 </section>;
}


function OrderDetailCard({result,detail,disabled,onOpen}:{result:BusinessResult;detail:OrderDetailResult;disabled:boolean;onOpen:(id:number,section?:'visit')=>void}) {
 const visit=detail.visit;
 const appointment=visit?.visit;
 const stages:Record<string,string>={scheduled:'已确认预约',departed:'已出发',arrived:'已到场',no_show:'已记录白跑',completed:'已结束',cancelled:'已取消'};
 return <section className="business-result" aria-label="工单详情查询结果">
  <div className="section-heading"><h3>{detail.order.location}</h3><Badge status={detail.order.status}/></div>
  <p className="field-help">{detail.order.number} · {categoryNames[detail.order.category]}</p>
  {result.warning&&<p className="assistant-notice" role="status">{result.warning}</p>}
  {visit&&<dl className="insight-fact-details">
   <div><dt>方便上门时间</dt><dd>{visit.availability_start ? `${time(visit.availability_start)} 至 ${time(visit.availability_end!)}` : '尚未提供'}</dd></div>
   <div><dt>确认预约</dt><dd>{appointment ? `${time(appointment.start)} 至 ${time(appointment.end)} · ${stages[appointment.stage] || appointment.stage}` : '尚未确认'}</dd></div>
   {detail.latest_visit&&<div><dt>最近上门记录</dt><dd>{time(detail.latest_visit.start)} · {stages[detail.latest_visit.stage] || detail.latest_visit.stage}{detail.latest_visit.no_show_at ? ` · 白跑记录于 ${time(detail.latest_visit.no_show_at)}` : ''}</dd></div>}
   {visit.reason&&<div><dt>上门说明</dt><dd>{visit.reason}</dd></div>}
   {visit.expected_resume_at&&<div><dt>预计恢复时间</dt><dd>{time(visit.expected_resume_at)}</dd></div>}
  </dl>}
  {detail.latest_repair&&<details className="insight-details"><summary>最近维修记录 · {time(detail.latest_repair.created_at)}</summary><p>{detail.latest_repair.measures}</p><p>{detail.latest_repair.result}</p></details>}
  {detail.events.length>0&&<details className="insight-details"><summary>处理时间线 · {detail.events.length} 条</summary>{detail.events.map(event=><div key={event.id}><small>{time(event.created_at)} · {event.actor_name}</small><p>{statusNames[event.to_status] || event.to_status}{event.note ? ` · ${event.note}` : ''}</p></div>)}{detail.has_more&&<p>还有更早记录，可打开工单查看。</p>}</details>}
  <div className="drawer-actions"><button type="button" disabled={disabled} onClick={()=>onOpen(detail.order.id)}>打开工单</button>{detail.next_step.actions.some(action=>action==='visit_availability'||action==='order_visit')&&<button type="button" disabled={disabled} onClick={()=>onOpen(detail.order.id,'visit')}>查看上门安排</button>}</div>
  <small>查询于 {time(result.queried_at)} · {result.historical?'历史快照':'查询时快照'}，打开工单核对最新情况。</small>
 </section>;
}
