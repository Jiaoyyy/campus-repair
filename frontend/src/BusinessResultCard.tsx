import { categoryNames, statusNames, time } from './api';
import type { BusinessResult } from './assistant-api';
export function BusinessResultCard({ result, disabled, onOpen, onQuestion }: {result: BusinessResult; disabled: boolean; onOpen: (id:number)=>void; onQuestion: (q:string)=>void}) {
 return <section className="business-result" aria-label="业务查询结果"><div className="section-heading"><h3>{result.kind === 'candidates' ? '可派人员' : '工单查询结果'}</h3><span className="badge">只读查询</span></div>
  <p className="field-help">查询于 {time(result.queried_at)} · 查询时的快照，最新进度请打开工单核对。</p>
  {result.kind === 'candidates' ? <><div className="candidate-results">{result.candidates?.map((c,i)=><div key={c.id} className="candidate-result"><div><strong>{c.display_name}</strong>{i===0&&<span className="badge">可优先考虑</span>}</div><small>{c.categories.map(k=>categoryNames[k]).join('、')} · 待接单 {c.pending_count} · 处理中 {c.in_progress_count}</small></div>)}</div><p className="field-help">按当前待接单与处理中总量排序，实际安排还需你在工单页面确认。</p></> : <>
   {(result.query.location||result.query.keyword)&&<p className="field-help">{result.query.location&&`位置包含“${result.query.location}”`}{result.query.keyword&&` 故障包含“${result.query.keyword}”`}</p>}
   <div className="business-total"><strong>{result.total}</strong><span>单符合条件</span></div>
   <div className="business-statuses">{Object.entries(result.statuses).filter(([,n])=>n>0).map(([s,n])=><span key={s}>{statusNames[s]} {n}</span>)}</div>
   {result.time_basis&&<p className="field-help">{result.time_basis}</p>}
   <div className="business-orders">{result.items.map(o=><button className="business-order" type="button" key={o.id} disabled={disabled} onClick={()=>onOpen(o.id)}><span><strong>{o.location}</strong>{o.description&&<small>{o.description}</small>}<small>{o.number} · {categoryNames[o.category]}</small></span><span className="badge">{statusNames[o.status]}</span></button>)}</div>
   {result.total>result.items.length&&<p className="field-help">卡片仅列出最近更新且当前仍可查看的 {result.items.length} 单，数量按查询时全部匹配记录统计。</p>}
   <div className="business-followups" aria-label="继续筛选">{['只看今天','那照明的呢','全部类别'].map(q=><button type="button" key={q} disabled={disabled} onClick={()=>onQuestion(q)}>{q}</button>)}</div>
  </>}
 </section>;
}
