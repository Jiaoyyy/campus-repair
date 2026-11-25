import {useEffect,useState} from 'react';
import {api,categoryNames,time} from './api';
import type {Order,User} from './api';
import {visitCopy,visitLabels} from './VisitPanel';
import type {VisitContext} from './VisitPanel';
import {visitIssue,visitQueueCopy} from './visit-presentation';
import {Badge,PageHeading,Loading} from './ui';
type Item={order:Order;context:VisitContext;allowed_actions:string[]};
type Data={items:Item[];counts:Record<string,number>;total:number;page_size:number;as_of:string};
type Metrics={counts:Record<string,number>;date_from:string;date_to:string;as_of:string};
const names:Record<string,string>={pending:'待接单',unscheduled:'待安排',today:'今日上门',processing:'处理中',...Object.fromEntries(Object.entries(visitQueueCopy).map(([k,v])=>[k,v.label]))};
function AdminVisitCard({item,asOf,onOpen}:{item:Item;asOf:string;onOpen:(id:number,section?:'visit')=>void}){
 const {order:o,context:c}=item,issue=visitIssue(o,c,asOf);
 return <article className="workbench-card visit-issue-card"><div className="section-heading"><h3>{o.location}</h3><Badge status={o.status}/></div>
  <p className="issue-fault">{o.description}</p>
  <div className="issue-body"><div className="issue-problem"><strong>{issue?.title||'上门信息已更新'}</strong><p>{issue?.problem||'请打开上门记录核对最新进展，并刷新当前列表。'}</p></div>
  <dl className="issue-steps"><dt>下一步由谁处理</dt><dd>{issue?`${issue.role} · ${issue.owner}`:'请查看最新记录'}</dd><dt>需要做什么</dt><dd>{issue?.next||'查看最新安排，刷新后核对是否仍存在异常。'}</dd></dl></div>
  {c.visit&&<p className="issue-time">约定上门：{time(c.visit.start)} — {time(c.visit.end)}<br/>已记录进度：{visitCopy(c)}</p>}
  {c.reason&&<p className="issue-note">已记录原因：{c.reason}</p>}
  {c.expected_resume_at&&<p className="issue-time">预计恢复：{time(c.expected_resume_at)}</p>}
  <p className="muted small issue-reference">{categoryNames[o.category]} · {o.number}</p>
  <button onClick={()=>onOpen(o.id,'visit')}>查看上门记录</button>
 </article>;
}
export function RepairWorkbench({u,onOpen,active}:{u:User;active:boolean;onOpen:(id:number,section?:'visit')=>void}){
 const admin=u.role==='admin',keys=admin?['overdue','reschedule','parts']:['pending','unscheduled','today','processing'];
 const cacheKey=`campus-workbench-${u.id}`;
 const [initial]=useState(()=>{try{const saved=JSON.parse(sessionStorage.getItem(cacheKey)||'null');if(saved&&keys.includes(saved.view)&&Number.isSafeInteger(saved.page)&&saved.page>0&&saved.page<=1000000)return saved as {view:string;page:number};}catch{/* Ignore invalid local navigation preferences. */}return {view:keys[0],page:1};});
 const [view,setView]=useState(initial.view),[page,setPage]=useState(initial.page),[data,setData]=useState<Data|null>(null),[error,setError]=useState(''),[loading,setLoading]=useState(true),[reload,setReload]=useState(0),[metrics,setMetrics]=useState<Metrics|null>(null);
 useEffect(()=>{sessionStorage.setItem(cacheKey,JSON.stringify({view,page}));},[cacheKey,view,page]);
 useEffect(()=>{if(!active)return;let live=true;setLoading(true);setError('');Promise.all([api<Data>(`/api/repair-workbench?view=${view}&page=${page}`),admin?api<Metrics>('/api/repair-workbench/metrics'):Promise.resolve(null)]).then(([d,m])=>{if(live){setData(d);setMetrics(m)}}).catch(e=>{if(live){setError(e.message);setData(null);setMetrics(null)}}).finally(()=>{if(live)setLoading(false)});return()=>{live=false}},[view,page,reload,admin,active]);
 function choose(key:string){if(key!==view||page!==1)setData(null);setView(key);setPage(1);}
 return <><PageHeading title={admin?'上门异常':'维修工作台'}/>
 <div className="overview order-queues" aria-label={admin?'异常队列':'维修队列'} style={{gridTemplateColumns:`repeat(${keys.length}, minmax(0, 1fr))`}}>
  {keys.map(k=><button className={`overview-card ${k===view?'chosen':''}`} aria-pressed={k===view} key={k} onClick={()=>choose(k)}><span>{names[k]}</span><strong>{!loading&&data?data.counts[k]:'—'}</strong></button>)}
 </div>
 <section className="panel list-panel visit-results" aria-busy={loading}>
  <div className="list-title"><h2 className="list-section-title">{names[view]}<span className="count">{!loading&&data?data.total:'—'}</span></h2><button onClick={()=>setReload(v=>v+1)} disabled={loading}>{loading?'正在读取…':'刷新'}</button></div>
  <p className="visit-queue-description">{admin?visitQueueCopy[view].meaning:'今日上门按预约时间排序，其他队列按最近更新排序。任务可能同时出现在多个队列。'}</p>
 {loading&&!data?<Loading/>:error?<div className="error" role="alert">{error}<button onClick={()=>setReload(v=>v+1)}>重新读取</button></div>:!data?.items.length?<div className="empty"><h3>暂无{names[view]}的工单</h3><p>读取成功，符合条件的任务会显示在这里。</p>{page>1&&<button onClick={()=>setPage(1)}>返回第一页</button>}</div>:<div className={`workbench-cards ${admin?'visit-issue-list':''}`}>{data.items.map(item=>{
  if(admin)return <AdminVisitCard key={item.order.id} item={item} asOf={data.as_of} onOpen={onOpen}/>;
  const {order:o,context:c,allowed_actions:a}=item;
  const visitAction=o.status==='in_progress' && ['resume','depart','arrive','schedule'].find(action=>a.includes(action));
  const visitLabel=visitAction==='schedule'?'安排上门':visitAction?visitLabels[visitAction]:undefined;
  const timeWindow = c.visit
   ? `${time(c.visit.start)} — ${time(c.visit.end)}`
   : c.availability_start && c.availability_end ? `方便上门 ${time(c.availability_start)} — ${time(c.availability_end)}` : '';
  const showVisit = Boolean(timeWindow || c.exception || c.revisit_required || o.status !== 'assigned');
  return <article key={o.id} className="workbench-card technician-task">
   <div className="technician-task-summary">
    <div className="section-heading"><h3>{o.location}</h3><Badge status={o.status}/></div>
    <p>{o.description}</p>
    <small>{categoryNames[o.category]} · {o.number}</small>
    {showVisit && <div className="technician-task-visit"><strong>{visitCopy(c)}</strong>{timeWindow && <span>{timeWindow}</span>}</div>}
    {c.visit&&c.visit.stage!=='arrived'&&new Date(c.visit.end)<new Date(data.as_of)&&<p className="visit-reason">约定时间已过，请核对并更新上门记录。</p>}
    {c.reason&&<p>说明：{c.reason}</p>}
    {c.expected_resume_at&&<p>预计恢复：{time(c.expected_resume_at)}</p>}
   </div>
   <div className="technician-task-actions">{visitLabel&&<button className="primary" onClick={()=>onOpen(o.id,'visit')}>{visitLabel}</button>}
    <button className={visitLabel?'':'primary'} onClick={()=>onOpen(o.id)}>{o.status==='assigned'?'查看并接单':c.exception==='waiting_parts'?'查看维修记录':'查看并记录处理情况'}</button>
   </div></article>;
 })}</div>}
 {!loading&&data&&<><p className="visit-results-time">读取于 {time(data.as_of)}（北京时间）</p>{data.total>0&&<div className="pagination"><button disabled={page===1} onClick={()=>setPage(v=>v-1)}>上一页</button><span>第 {page} 页</span><button disabled={page*data.page_size>=data.total} onClick={()=>setPage(v=>v+1)}>下一页</button></div>}</>}
 </section>
 {admin&&<details className="visit-statistics"><summary>异常统计口径{metrics&&` · 已记录白跑 ${metrics.counts.no_show} 次`}</summary>
  {metrics&&<p>{metrics.date_from} 至 {metrics.date_to}，按到场后记录“报修人不在”的预约计数。</p>}
  <ul>{Object.values(visitQueueCopy).map(c=><li key={c.label}><strong>{c.label}</strong> · {c.meaning}</li>)}</ul>
 </details>}</>;
}
