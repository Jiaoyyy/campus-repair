import {useEffect,useState} from 'react';
import {api,statusNames,categoryNames,time} from './api';
import type {Order,User} from './api';
import {visitCopy,visitLabels} from './VisitPanel';
import type {VisitContext} from './VisitPanel';
import {visitIssue,visitQueueCopy} from './visit-presentation';
import {PageHeading,Loading} from './ui';
type Item={order:Order;context:VisitContext;allowed_actions:string[]};
type Data={items:Item[];counts:Record<string,number>;total:number;page_size:number;as_of:string};
type Metrics={counts:Record<string,number>;date_from:string;date_to:string;as_of:string};
const names:Record<string,string>={pending:'待接单',unscheduled:'待安排',today:'今日上门',processing:'处理中',...Object.fromEntries(Object.entries(visitQueueCopy).map(([k,v])=>[k,v.label]))};
function AdminVisitCard({item,asOf,onOpen}:{item:Item;asOf:string;onOpen:(id:number,section?:'visit')=>void}){
 const {order:o,context:c}=item,issue=visitIssue(o,c,asOf);
 return <article className="workbench-card visit-issue-card"><div className="section-heading"><h3>{o.location}</h3><span className="badge">{statusNames[o.status]}</span></div>
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
 return <><PageHeading title={admin?'上门异常':'维修工作台'} description={admin?'查看上门过程中卡住的工单，明确原因和下一步负责的人。':'先看今天要去哪里，再处理待接单和未安排的任务。'}><button onClick={()=>setReload(v=>v+1)} disabled={loading}>{loading?'正在读取…':'刷新'}</button></PageHeading>
 {admin&&<section className="visit-workflow" aria-label="上门异常处理说明"><details><summary>异常统计口径</summary><ul>{Object.values(visitQueueCopy).map(c=><li key={c.label}><strong>{c.label}：</strong>{c.meaning}</li>)}</ul><p>人员更新上门记录后，刷新本页可核对变化。这里不会自动通知或代替本人修改安排。</p></details></section>}
 <div className="overview workbench-overview" style={{gridTemplateColumns:`repeat(${keys.length},minmax(0,1fr))`}}>{keys.map(k=><button className={`overview-card ${k===view?'chosen':''}`} aria-pressed={k===view} key={k} onClick={()=>choose(k)}><span>{names[k]}</span><strong>{!loading&&data?data.counts[k]:'—'}</strong></button>)}</div>
 <section className="panel" aria-busy={loading}><h2>{names[view]}</h2><p className="muted">{admin?visitQueueCopy[view].meaning:'队列可能重叠；今日上门按预约时间排序，其余按最近更新排序。'}</p>
 {loading&&!data?<Loading/>:error?<div className="error" role="alert">{error}<button onClick={()=>setReload(v=>v+1)}>重新读取</button></div>:!data?.items.length?<div className="empty"><h3>暂无{names[view]}的工单</h3><p>读取成功，符合条件的任务会显示在这里。</p>{page>1&&<button onClick={()=>setPage(1)}>返回第一页</button>}</div>:<div className={`workbench-cards ${admin?'visit-issue-list':''}`}>{data.items.map(item=>{
  if(admin)return <AdminVisitCard key={item.order.id} item={item} asOf={data.as_of} onOpen={onOpen}/>;
  const {order:o,context:c,allowed_actions:a}=item;
  return <article key={o.id} className="workbench-card"><div className="section-heading"><h3>{o.location}</h3><span className="badge">{statusNames[o.status]}</span></div><p>{o.description}</p><p className="muted small">{categoryNames[o.category]} · {o.number} · {o.assignee_name||'等待接单'}</p><strong>{visitCopy(c)}</strong><p>{c.visit?`${time(c.visit.start)} — ${time(c.visit.end)}`:c.availability_start?`方便上门：${time(c.availability_start)} — ${time(c.availability_end!)}`:'报修人尚未提供时间'}</p>{c.visit&&c.visit.stage!=='arrived'&&new Date(c.visit.end)<new Date(data!.as_of)&&<p className="visit-reason">约定时间已过，请核对并更新上门记录。</p>}{c.reason&&<p>说明：{c.reason}</p>}{c.expected_resume_at&&<p>预计恢复：{time(c.expected_resume_at)}</p>}
  <button className="primary" onClick={()=>onOpen(o.id)}>{o.status==='assigned'?'查看并接单':a.includes('depart')?visitLabels.depart:a.includes('arrive')?visitLabels.arrive:a.includes('schedule')?'安排上门':a.includes('resume')?'查看并恢复处理':'查看并处理'}</button></article>;
 })}</div>}
 {!loading&&data&&<><p className="muted small">共 {data.total} 单 · 读取于 {time(data.as_of)}（北京时间）</p>{data.total>0&&<div className="pagination"><button disabled={page===1} onClick={()=>setPage(v=>v-1)}>上一页</button><span>第 {page} 页</span><button disabled={page*data.page_size>=data.total} onClick={()=>setPage(v=>v+1)}>下一页</button></div>}</>}
 </section>
 {metrics&&<section className="panel workbench-metrics"><h2>已记录白跑 {metrics.counts.no_show} 次</h2><p className="muted">{metrics.date_from} 至 {metrics.date_to}，按到场后记录“报修人不在”的预约计数。</p><p className="muted small">当前上门超时 {metrics.counts.overdue} 单 · 等待备件 {metrics.counts.parts} 单 · {time(metrics.as_of)} 读取</p></section>}</>;
}
