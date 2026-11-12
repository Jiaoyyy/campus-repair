import { categoryNames, statusNames, time } from './api';
import type { BusinessResult } from './assistant-api';
export function BusinessResultCard({ result, disabled, onOpen, onQuestion }: {result: BusinessResult; disabled: boolean; onOpen: (id:number)=>void; onQuestion: (q:string)=>void}) {
 if(result.kind==='analysis' && result.analysis) return <AnalysisResultCard result={result} disabled={disabled} onOpen={onOpen} onQuestion={onQuestion}/>;
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

function AnalysisResultCard({result,disabled,onOpen,onQuestion}:{result:BusinessResult;disabled:boolean;onOpen:(id:number)=>void;onQuestion:(q:string)=>void}) {
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
  <div className="insight-findings">{findings.map(f=><div className={`insight-finding ${f.count?'has-issue':''}`} key={f.id}><strong>{f.title}</strong><p>{concise(f)}</p></div>)}</div>
  {result.total===0&&<p>本次分析范围没有匹配工单。</p>}
  {a.model_status==='ready'&&a.notes.length>0?<section className="insight-advice" aria-label="分析建议"><h4>建议关注</h4>{a.notes.map((note,i)=><div key={i}><p>{note.recommendation}</p><small>依据：{note.evidence.map(id=>a.facts.find(f=>f.id===id)?.title).filter(Boolean).join('、')}</small></div>)}<small>建议待现场核实。</small></section>:<p className="assistant-notice" role="status">{a.warning||'本轮仅展示统计，未生成模型建议。'}</p>}
  <details className="insight-details"><summary>查看统计明细</summary><p>{result.total} 单纳入本次分析；排名最多列5项。上门异常允许重叠，当前阻塞和积压不限创建日期；积压关注线不代表校方维修时限。</p><dl className="insight-fact-details">{a.facts.filter(f=>!ids.includes(f.id)&&f.id!=='total').map(f=><div key={f.id}><dt>{f.title}</dt><dd>{f.text}</dd></div>)}</dl>{[{title:'类别',items:a.categories},{title:'准确位置',items:a.locations},{title:'相同故障描述',items:a.symptoms}].map(group=><div key={group.title}><h4>{group.title}</h4><ol>{group.items.map((g,i)=><li key={i}><span>{g.label}</span><strong>{g.count} 单</strong></li>)}</ol></div>)}</details>
  {result.items.length>0&&<details className="insight-details"><summary>相关工单 · {result.items.length}</summary><p className="muted small">最近更新的示例；排名按全部匹配记录统计。</p><div className="business-orders">{result.items.map(o=><button className="business-order" key={o.id} disabled={disabled} onClick={()=>onOpen(o.id)}><span><strong>{o.location}</strong><small>{o.description}</small><small>{o.number}</small></span><span className="badge">{statusNames[o.status]}</span></button>)}</div></details>}
  <div className="business-followups">{['总结当前有哪些问题','最近出现最多的问题是什么','现在有几单白跑了'].filter(q=>result.query.analysis!== (q.includes('白跑')?'no_show':q.includes('最多')?'frequent':'overview')).map(q=><button key={q} disabled={disabled} onClick={()=>onQuestion(q)}>{q}</button>)}</div>
 </section>;
}
