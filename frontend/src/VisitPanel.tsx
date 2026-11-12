import {useEffect,useRef,useState} from 'react';
import {api,APIError,time} from './api';
import type {Order,User} from './api';
import {visitIssue} from './visit-presentation';
export type VisitContext={revisit_required?:boolean;availability_start:string|null;availability_end:string|null;exception:string;reason:string;expected_resume_at:string|null;needs_availability:boolean;visit:{id:number;start:string;end:string;stage:string;departed_at:string|null;arrived_at:string|null}|null};
type Response={context:VisitContext;version:number;allowed_actions:string[];as_of:string};
export const visitLabels:Record<string,string>={availability:'补充或调整方便上门时间',schedule:'确认上门时间',depart:'出发前往',arrive:'已到达现场','no-show':'报修人不在','wait-parts':'等待备件',resume:'恢复处理'};
const stages:Record<string,string>={scheduled:'已安排上门',departed:'维修人员已出发',arrived:'已到达报修位置'};
export function visitCopy(c:VisitContext){return c.exception==='waiting_parts'?'等待备件':c.visit?stages[c.visit.stage]||'上门处理中':c.exception==='no_show'?'需要重新协调上门时间':'尚未安排上门'}
export function VisitPanel({o,u,disabled,onSaved,onDirtyChange,onContextChange}:{o:Order;u:User;disabled:boolean;onSaved:()=>void;onDirtyChange:(v:boolean)=>void;onContextChange:(v:VisitContext|null)=>void}){
 const [data,setData]=useState<Response|null>(null),[error,setError]=useState(''),[loading,setLoading]=useState(true),[refresh,setRefresh]=useState(0),[stale,setStale]=useState(false);
 const [action,setAction]=useState(''),[start,setStart]=useState(''),[end,setEnd]=useState(''),[reason,setReason]=useState(''),[resume,setResume]=useState(''),[version,setVersion]=useState(0),[busy,setBusy]=useState(false),[message,setMessage]=useState('');const working=useRef(false);
 useEffect(()=>{let live=true;setLoading(true);setStale(true);onContextChange(null);api<Response>(`/api/work-orders/${o.id}/visit`).then(v=>{if(live){setData(v);setError('');setStale(false);onContextChange(v.context)}}).catch(e=>{if(live){setError(e.message);if(e instanceof APIError&&[401,403,404].includes(e.status))setData(null)}}).finally(()=>{if(live)setLoading(false)});return()=>{live=false}},[o.id,o.version,refresh,onContextChange]);
 useEffect(()=>{onDirtyChange(!!action||busy);return()=>onDirtyChange(false)},[action,busy,onDirtyChange]);
 useEffect(()=>{if(!action&&!busy)return;const guard=(e:BeforeUnloadEvent)=>{e.preventDefault();e.returnValue=''};window.addEventListener('beforeunload',guard);return()=>window.removeEventListener('beforeunload',guard)},[action,busy]);
 const c=data?.context;const issue=c&&data?visitIssue(o,c,data.as_of):null;const ended=['awaiting_confirmation','closed','cancelled','rejected'].includes(o.status);const blocked=disabled||loading||stale||busy||!data||data.version!==o.version;
 function edit(key:string){if(!data)return;setAction(key);setVersion(data.version);setReason('');setResume('');setError('');setMessage('');const from=c?.availability_start,to=c?.availability_end;setStart(from?localInput(from):'');setEnd(to?localInput(to):'')}
 async function save(e:React.FormEvent){e.preventDefault();if(blocked||working.current||!data)return;working.current=true;setBusy(true);setError('');try{
 const body:Record<string,unknown>={version,reason};if(action==='availability'||action==='schedule'){if((start&&!end)||(!start&&end))throw new Error('请同时填写开始和结束时间');body.start=start?`${start}:00+08:00`:null;body.end=end?`${end}:00+08:00`:null}
 if(action==='wait-parts')body.expected_resume_at=resume?`${resume}:00+08:00`:null;
 await api(`/api/work-orders/${o.id}/visit/${action==='availability'?'availability':`actions/${action}`}`,{method:action==='availability'?'PUT':'POST',body:JSON.stringify(body)});setAction('');setMessage('上门信息已保存。');onSaved();setRefresh(v=>v+1)
 }catch(e){setError((e as Error).message);if(e instanceof APIError&&e.status===409)setStale(true)}finally{setBusy(false);working.current=false}}
 return <section className="panel visit-panel" aria-label="上门安排"><div className="section-heading"><h2>上门安排</h2><button type="button" disabled={loading||busy} onClick={()=>{setRefresh(v=>v+1);onSaved()}}>读取最新安排</button></div>
 {loading&&<p className="muted">正在读取上门安排…</p>}{error&&<div className="error" role="alert">{error}</div>}{message&&<p role="status" className="success">{message}</p>}
 {c&&<><h3>{o.status==='awaiting_confirmation'?'维修已提交结果，等待本人确认':ended?'本次上门安排已结束':visitCopy(c)}</h3>{stale&&<p className="error">以下为上次读取的信息，读取成功后才能操作。</p>}<dl className="visit-facts"><dt>{ended?'曾提供的方便时间':'方便上门时间'}</dt><dd>{c.availability_start&&c.availability_end?`${time(c.availability_start)} — ${time(c.availability_end)}`:'尚未提供（可选）'}</dd><dt>已确认预约</dt><dd>{c.visit?`${time(c.visit.start)} — ${time(c.visit.end)}`:ended?'无待执行预约':'尚未安排'}</dd><dt>当前负责方</dt><dd>{o.status==='awaiting_confirmation'?(o.requester_name||'报修人'):ended?'流程已结束':c.exception==='no_show'&&c.needs_availability?'报修人，请提供新时间':o.assignee_name||'等待管理员安排人员'}</dd></dl>
 {issue&&<div className="visit-next-step"><strong>{issue.title}</strong><p>{issue.problem}</p><dl><dt>下一步由谁处理</dt><dd>{issue.role} · {issue.owner}</dd><dt>需要做什么</dt><dd>{issue.next}</dd></dl>{u.role==='admin'&&<p className="muted small">管理员可查看原因和进展，具体上门记录由上述人员更新。</p>}</div>}{c.reason&&<p className="visit-reason">说明：{c.reason}</p>}{c.expected_resume_at&&<p>预计恢复：{time(c.expected_resume_at)}{new Date(c.expected_resume_at)<new Date(data!.as_of)?'（预计时间已过，请维修人员更新）':''}</p>}
 {c.visit?.departed_at&&<p>出发：{time(c.visit.departed_at)}</p>}{c.visit?.arrived_at&&<p>到场：{time(c.visit.arrived_at)}</p>}
 {u.role==='requester'&&!c.availability_start&&data?.allowed_actions.includes('availability')&&<p className="muted">报修已受理流程不受影响。可补充你方便在场的时间，供维修人员确认。</p>}
 <p className="muted small">北京时间 · 信息读取于 {time(data!.as_of)} · 刷新查看最新进展</p>
 {!action&&<div className="visit-buttons">{data?.allowed_actions.map(key=><button key={key} type="button" disabled={blocked} className={['availability','schedule','depart','arrive','resume'].includes(key)?'primary':''} onClick={()=>edit(key)}>{visitLabels[key]}</button>)}</div>}
 </>}
 {action&&<form onSubmit={save}><h3>{visitLabels[action]}</h3><fieldset disabled={blocked}>
 {(action==='availability'||action==='schedule')&&<><p className="field-help">未来14天内开始，30分钟至4小时。时间以北京时间填写。</p><div className="visit-time-fields"><label>开始时间<input type="datetime-local" value={start} onInput={e=>setStart(e.currentTarget.value)} onChange={e=>setStart(e.target.value)} required={action==='schedule'}/></label><label>结束时间<input type="datetime-local" value={end} onInput={e=>setEnd(e.currentTarget.value)} onChange={e=>setEnd(e.target.value)} required={action==='schedule'}/></label></div>{action==='availability'&&<button type="button" onClick={()=>{setStart('');setEnd('')}}>清除时间（暂不安排）</button>}{c?.visit&&<p className="confirmation-note">保存调整会撤销原预约，需要重新确认。请填写原因。</p>}</>}
 {action==='wait-parts'&&<label>预计恢复时间<input type="datetime-local" value={resume} required onInput={e=>setResume(e.currentTarget.value)} onChange={e=>setResume(e.target.value)}/></label>}
 {['depart','arrive'].includes(action)?<p>确认后记录当前时间。{action==='arrive'?'到场不代表获得进入房间的授权。':''}</p>:<label>{action==='resume'?'恢复处理说明':'原因或说明'}<textarea maxLength={2000} value={reason} onChange={e=>setReason(e.target.value)} required={['no-show','wait-parts','resume'].includes(action)||!!c?.visit} rows={3}/></label>}
 <button className="primary" disabled={version!==data?.version}>确认保存</button></fieldset>
 {version!==data?.version&&!stale&&!loading&&<p className="confirmation-note">安排已更新，输入已保留。请核对上方最新信息。<button type="button" onClick={()=>setVersion(data!.version)}>已核对，使用最新版本</button></p>}
 <button type="button" disabled={busy} onClick={()=>{setAction('');setError('')}}>取消编辑</button></form>}
 </section>
}
function localInput(v:string){return new Date(new Date(v).getTime()+8*3600000).toISOString().slice(0,16)}
