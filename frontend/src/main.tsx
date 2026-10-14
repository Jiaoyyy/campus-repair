import React, { useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { api, APIError, tokenKey, statusNames, categoryNames, roleNames, actionNames, time } from './api';
import type { User, Order, Detail, Page } from './api';
import { primaryAction, statusCopy, progressSteps, progressIndex, queueNames, auditLabel, successCopy } from './presentation';
import type { Queue, Summary } from './presentation';
import './style.css';

function Icon({ kind = 'box' }: { kind?: string }) {
  return <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    {kind === 'plus' ? <path d="M12 5v14M5 12h14" /> : kind === 'arrow' ? <path d="m9 5 7 7-7 7" /> : kind === 'check' ? <path d="m5 12 4 4L19 6" /> : kind === 'tool' ? <><path d="m14 5 5 5-9 9-5-5 9-9Z" /><path d="m15 4 5 5M4 20l3-3" /></> : kind === 'logout' ? <><path d="M10 4H5v16h5M9 12h12m-4-4 4 4-4 4" /></> : <><rect x="5" y="3" width="14" height="18" rx="2" /><path d="M9 8h6M9 12h6M9 16h3" /></>}
  </svg>;
}
function Badge({ status }: { status: string }) { return <span className={`badge ${status}`}>{statusNames[status] || '状态待确认'}</span>; }
function ErrorMessage({ text }: { text: string }) { return text ? <div className="error" role="alert">{text}</div> : null; }
function Loading({ text = '正在读取工单…' }: { text?: string }) { return <div className="loading" role="status"><span className="spinner" />{text}</div>; }

function Login({ onLogin, message }: { onLogin: (u: User) => void; message: string }) {
  const [username, setUsername] = useState(''); const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false); const [error, setError] = useState('');
  async function submit(e: React.FormEvent) {
    e.preventDefault(); setBusy(true); setError('');
    try { const v = await api<{ access_token: string; user: User }>('/api/login', { method: 'POST', body: JSON.stringify({ username, password }) }); sessionStorage.setItem(tokenKey, v.access_token); onLogin(v.user); }
    catch (e) { setError((e as Error).message); } finally { setBusy(false); }
  }
  return <div className="login"><section className="login-intro"><div className="brand"><span className="brand-icon"><Icon kind="tool" /></span>校园报修</div>
    <div className="intro-copy"><span className="eyebrow">校园设施服务</span><h1>报修有记录，<br />进度看得见。</h1><p>提交故障、安排维修、确认结果。<br />在一个工作台完成每一步。</p><div className="flow"><span>提交报修</span><Icon kind="arrow" /><span>维修处理</span><Icon kind="arrow" /><span>确认完成</span></div></div><div className="intro-foot">报修人 · 维修人员 · 后勤管理员</div></section>
    <section className="login-form"><form onSubmit={submit}><span className="eyebrow">欢迎使用</span><h2>登录工作台</h2><p className="muted">使用你的账号查看并处理报修。</p><ErrorMessage text={error || message} />
      <label>账号<input autoComplete="username" value={username} onChange={e => setUsername(e.target.value)} required disabled={busy} placeholder="输入账号" /></label>
      <label>密码<input type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} required disabled={busy} placeholder="输入密码" /></label>
      <button className="primary full" disabled={busy}>{busy ? '正在登录…' : '登录'}</button><p className="login-note">账号由平台管理员提供。</p></form></section></div>;
}

function CreateOrder({ onCreated, onCancel }: { onCreated: (o: Order) => void; onCancel: () => void }) {
  const [input, setInput] = useState({ category: 'lighting', location: '', description: '' }); const key = useRef(crypto.randomUUID());
  const [busy, setBusy] = useState(false); const [error, setError] = useState('');
  function update(k: string, v: string) { setInput(i => ({ ...i, [k]: v })); key.current = crypto.randomUUID(); }
  async function submit(e: React.FormEvent) {
    e.preventDefault(); setBusy(true); setError('');
    try { const v = await api<{ order: Order }>('/api/work-orders', { method: 'POST', headers: { 'Idempotency-Key': key.current }, body: JSON.stringify(input) }); onCreated(v.order); }
    catch (e) { setError((e as Error).message); } finally { setBusy(false); }
  }
  return <div className="create-layout"><div className="panel form-panel"><form onSubmit={submit}><ErrorMessage text={error} /><fieldset disabled={busy}>
    <div className="field-section"><h2><span className="step-number">1</span>选择故障类别</h2><div className="category-options">{Object.entries(categoryNames).map(([k, v]) => <label className={input.category === k ? 'selected' : ''} key={k}><input type="radio" name="category" checked={input.category === k} onChange={() => update('category', k)} /><strong>{v}</strong><small>{{ lighting: '灯具与照明设施', electrical: '电器与用电设备', plumbing: '水管与排水设施' }[k]}</small></label>)}</div></div>
    <div className="field-section"><h2><span className="step-number">2</span>填写准确位置</h2><label>具体位置<input value={input.location} onChange={e => update('location', e.target.value)} maxLength={200} required placeholder="例如：3 号楼 201 室，靠窗照明灯" /></label><p className="field-help">写明楼栋、房间或公共区域，便于维修人员找到故障点。</p></div>
    <div className="field-section"><h2><span className="step-number">3</span>描述故障现象</h2><label>故障描述<textarea value={input.description} onChange={e => update('description', e.target.value)} maxLength={2000} required rows={5} placeholder="出现了什么问题？何时发现？目前是什么情况？" /></label><div className="field-bottom"><span>请描述现象，不必判断专业故障原因。</span><span>{[...input.description].length}/2000</span></div></div>
    <div className="form-actions"><button type="button" onClick={onCancel}>返回列表</button><button className="primary">{busy ? '正在提交…' : '确认提交报修'}</button></div></fieldset></form></div>
    <aside className="submission-guide"><h3>提交之后会怎样？</h3><ol><li><strong>管理员审核信息</strong><p>信息不足时，会请你补充。</p></li><li><strong>安排维修人员</strong><p>在工单详情查看人员与进度。</p></li><li><strong>你确认处理结果</strong><p>问题未解决，可以申请重新处理。</p></li></ol><div className="safety-note">如有漏电、冒烟等危险现象，请停止使用并联系现场工作人员。</div></aside></div>;
}

function actionsFor(u: User, o: Order) {
  const a: string[] = [];
  if (u.role === 'admin') { if (o.status === 'submitted') a.push('review'); if (o.status === 'unassigned') a.push('assign'); if (o.status === 'assigned') a.push('reassign'); }
  if (u.role === 'technician') { if (o.status === 'assigned') a.push('accept'); if (o.status === 'in_progress') a.push('complete', 'records'); }
  if (u.role === 'requester') { if (o.status === 'needs_info') a.push('resubmit'); if (o.status === 'awaiting_confirmation') a.push('confirm', 'reopen'); if (['submitted', 'needs_info', 'unassigned', 'assigned'].includes(o.status)) a.push('cancel'); }
  return a;
}
function ActionForm({ u, o, disabled, onDone, onConflict }: { u: User; o: Order; disabled: boolean; onDone: (message: string) => void; onConflict: () => void }) {
  const mainAction = primaryAction(u, o); const available = actionsFor(u, o);
  const [action, setAction] = useState(mainAction); const [decision, setDecision] = useState('approve'); const [reason, setReason] = useState('');
  const [measures, setMeasures] = useState(''); const [result, setResult] = useState(''); const [tech, setTech] = useState('');
  const [techs, setTechs] = useState<{ id: number; display_name: string; pending_count: number }[]>([]); const [staffLoading, setStaffLoading] = useState(false);
  const [input, setInput] = useState({ category: o.category, location: o.location, description: o.description }); const [error, setError] = useState(''); const [busy, setBusy] = useState(false);
  useEffect(() => {
    let active = true;
    if (action === 'assign' || action === 'reassign') { setStaffLoading(true); api<{ items: typeof techs }>(`/api/technicians?category=${o.category}`).then(v => { if (active) setTechs(v.items.filter(t => action !== 'reassign' || t.id !== o.assignee_id)); }).catch(e => { if (active) setError(e.message); }).finally(() => { if (active) setStaffLoading(false); }); }
    return () => { active = false; };
  }, [action, o.category, o.assignee_id]);
  if (!available.length) return null;
  function choose(a: string) { setAction(a); setError(''); setReason(''); setTech(''); }
  const needsReason = ['reassign', 'reopen', 'cancel'].includes(action) || (action === 'review' && decision !== 'approve');
  async function submit(e: React.FormEvent) {
    e.preventDefault(); setBusy(true); setError('');
    try { await api(`/api/work-orders/${o.id}/${action}`, { method: 'POST', body: JSON.stringify({ version: o.version, decision, reason, technician_id: Number(tech), measures, result, ...input }) }); onDone(successCopy(action, decision)); }
    catch (e) { setError((e as Error).message); if (e instanceof APIError && e.status === 409) onConflict(); } finally { setBusy(false); }
  }
  const buttonName = action === 'review' ? { approve: '审核通过', request_info: '要求补充信息', reject: '确认驳回' }[decision] : actionNames[action];
  return <section className={`panel action-panel ${!action ? 'secondary-only' : ''}`} aria-label="工单操作">
    <div className="action-options">{available.filter(a => a !== mainAction).map(a => a === action ? <span key={a} className="selected-tag">{actionNames[a]}</span> : <button type="button" key={a} className={a === 'cancel' ? 'danger-link' : ''} disabled={busy || disabled} onClick={() => choose(a)}>{a === 'cancel' ? '需要取消报修？' : a === 'reassign' ? '调整维修人员' : actionNames[a]}</button>)}</div>
    {action && <form onSubmit={submit}><h3>{actionNames[action]}</h3><ErrorMessage text={error} /><fieldset disabled={busy || disabled}>
      {action === 'review' && <div className="review-options" role="group" aria-label="审核决定">{[['approve', '通过'], ['request_info', '要求补充'], ['reject', '驳回']].map(([k, v]) => <button key={k} type="button" aria-pressed={decision === k} className={decision === k ? 'selected' : ''} onClick={() => { setDecision(k); setReason(''); }}>{v}</button>)}</div>}
      {(action === 'assign' || action === 'reassign') && <><label>维修人员<select aria-label="维修人员" value={tech} onChange={e => setTech(e.target.value)} required disabled={staffLoading}><option value="">{staffLoading ? '正在读取人员…' : techs.length ? '请选择维修人员' : '暂无匹配的可派人员'}</option>{techs.map(t => <option key={t.id} value={t.id}>{t.display_name} · 待处理 {t.pending_count} 单</option>)}</select></label><p className="field-help">仅显示负责{categoryNames[o.category]}、当前可用的维修人员。</p></>}
      {needsReason && <label>{action === 'review' && decision === 'request_info' ? '需补充的信息' : action === 'reopen' ? '仍未解决的问题' : action === 'cancel' ? '取消原因' : '原因说明'}<textarea required value={reason} onChange={e => setReason(e.target.value)} maxLength={2000} rows={3} /></label>}
      {['records', 'complete'].includes(action) && <><label>处理措施<textarea required value={measures} onChange={e => setMeasures(e.target.value)} rows={3} maxLength={2000} placeholder="说明已经完成的检查或维修。" /></label><label>处理结果<textarea required value={result} onChange={e => setResult(e.target.value)} rows={3} maxLength={2000} placeholder={action === 'complete' ? '说明维修后的情况，供报修人确认。' : '说明当前进展和仍需处理的问题。'} /></label></>}
      {action === 'resubmit' && <><label>类别<select aria-label="故障类别" value={input.category} onChange={e => setInput({ ...input, category: e.target.value })}>{Object.entries(categoryNames).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select></label><label>位置<input required value={input.location} onChange={e => setInput({ ...input, location: e.target.value })} maxLength={200} /></label><label>故障描述<textarea required value={input.description} onChange={e => setInput({ ...input, description: e.target.value })} maxLength={2000} rows={4} /></label></>}
      {action === 'confirm' && <p className="confirmation-note">请先检查故障是否已解决。确认后工单将关闭。</p>}
      {action === 'cancel' && <p className="confirmation-note">取消后本次工单结束。如仍需维修，需要重新提交报修。</p>}
      <button className={`full ${action === 'cancel' || (action === 'review' && decision === 'reject') ? 'danger' : 'primary'}`} disabled={staffLoading || busy || disabled || ((action === 'assign' || action === 'reassign') && !tech)}>{busy ? '正在保存…' : buttonName}</button>
      {action !== mainAction && <button type="button" className="text-button full" onClick={() => choose(mainAction)}>返回{mainAction ? '主要操作' : '查看进度'}</button>}
    </fieldset></form>}
  </section>;
}

function OrderDetail({ id, u, onBack, notice, onSaved }: { id: number; u: User; onBack: () => void; notice: string; onSaved: () => void }) {
  const [detail, setDetail] = useState<Detail | null>(null); const [error, setError] = useState(''); const [refresh, setRefresh] = useState(0);
  const [loading, setLoading] = useState(true); const [stale, setStale] = useState(false); const [message, setMessage] = useState(notice);
  useEffect(() => {
    let active = true; setLoading(true);
    api<Detail>(`/api/work-orders/${id}`).then(v => { if (active) { setDetail(v); setError(''); setStale(false); } }).catch(e => { if (active) { setError(e.message); setStale(true); if (e instanceof APIError && [401, 403, 404].includes(e.status)) setDetail(null); } }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [id, refresh]);
  const o = detail?.order; const copy = o ? statusCopy(u, o) : null; const lastEvent = detail?.events.at(-1); const lastRecord = detail?.records.at(-1);
  const rank = o ? progressIndex(o.status) : -1;
  return <><div className="detail-toolbar"><button className="back-button" onClick={onBack}>← 返回工单列表</button><button onClick={() => setRefresh(v => v + 1)} disabled={loading}>刷新进度</button></div>
    <ErrorMessage text={error} />{stale && detail && <div className="error" role="alert">当前仅显示上次读取的信息，请刷新后再操作。</div>}
    {message && <div className="success" role="status"><Icon kind="check" />{message}</div>}
    {!detail ? loading ? <Loading /> : <div className="panel empty"><h3>暂时无法读取工单</h3><p>请重试，或返回列表查看可访问的工单。</p><button onClick={() => setRefresh(v => v + 1)}>重新读取</button></div> : o && copy && <>
      <div className="detail-title"><div><span className="order-reference">工单 {o.number}</span><h1>{o.location}</h1><p className="muted">{categoryNames[o.category]} · {o.requester_name}提交 · {time(o.created_at)}</p></div><Badge status={o.status} /></div>
      <section className={`status-summary ${['closed', 'cancelled', 'rejected'].includes(o.status) ? 'terminal' : ''}`} aria-label="当前处理状态"><div className="status-icon"><Icon kind={o.status === 'closed' ? 'check' : 'box'} /></div><div><h2>{copy.title}</h2><p>{copy.description}</p>{['needs_info', 'rejected', 'cancelled'].includes(o.status) && lastEvent?.note && <div className="status-reason"><strong>{o.status === 'needs_info' ? '补充要求' : o.status === 'rejected' ? '驳回原因' : '取消原因'}：</strong>{lastEvent.note}</div>}</div><div className="responsible"><span>当前处理方</span><strong>{copy.owner}</strong></div></section>
      {rank >= 0 && <ol className="progress-track" aria-label="处理流程">{progressSteps.map((label, i) => <li key={label} className={i < rank ? 'done' : i === rank ? 'current' : ''} aria-current={i === rank ? 'step' : undefined}><span>{i < rank ? <Icon kind="check" /> : i + 1}</span><strong>{label}</strong></li>)}</ol>}
      <div className={`detail-grid ${actionsFor(u, o).length ? '' : 'no-actions'}`}><div className="detail-content">
        {lastRecord && <section className="panel latest-result"><div className="section-heading"><h2>{o.status === 'awaiting_confirmation' || o.status === 'closed' ? '最近维修结果' : '最近处理记录'}</h2><span>{time(lastRecord.created_at)}</span></div><p className="result-text">{lastRecord.result}</p><dl><dt>处理措施</dt><dd>{lastRecord.measures}</dd><dt>处理人员</dt><dd>{lastRecord.actor_name}</dd></dl>{(detail.records.length > 1) && <details className="past-records"><summary>查看此前 {detail.records.length - 1} 次处理记录</summary>{detail.records.slice(0, -1).reverse().map(r => <div className="repair-record" key={r.id}><p className="muted small">{r.actor_name} · {time(r.created_at)}</p><p>措施：{r.measures}</p><p>结果：{r.result}</p></div>)}</details>}</section>}
        <section className="panel report-information"><h2 className="section-title">报修信息</h2><div className="info-grid"><div><span>故障类别</span><strong>{categoryNames[o.category]}</strong></div><div><span>报修人</span><strong>{o.requester_name}</strong></div><div><span>维修人员</span><strong>{o.assignee_name || (['cancelled', 'rejected'].includes(o.status) ? '未安排' : '等待安排')}</strong></div><div><span>最近更新</span><strong>{time(o.updated_at)}</strong></div></div><div className="description">{o.description}</div></section>
        <section className="panel order-history"><div className="section-heading"><h2>处理时间线</h2><span>{detail.events.length} 条记录</span></div><ol className="timeline">{[...detail.events].reverse().map((e, i) => <li key={e.id} className={i === 0 ? 'latest' : ''}><span className="dot" /><div><div className="event-heading"><strong>{actionNames[e.action] || '工单操作'}</strong><time>{time(e.created_at)}</time></div><p>{e.actor_name} · {statusNames[e.to_status]}</p>{e.note && <div className="event-note">{e.note}</div>}</div></li>)}</ol></section>
      </div>{actionsFor(u, o).length > 0 && <aside className="detail-actions"><ActionForm key={`${id}-${o.version}`} u={u} o={o} disabled={loading || stale} onConflict={() => setStale(true)} onDone={msg => { setMessage(msg); setRefresh(v => v + 1); onSaved(); }} /><div className="detail-tip"><strong>进度与记录会持续保留</strong><p>工单变更会显示在处理时间线。刷新可查看最新进展。</p></div></aside>}</div>
    </>}
  </>;
}

function OrderList({ u, active, revision, onOpen, onNew }: { u: User; active: boolean; revision: number; onOpen: (id: number) => void; onNew: () => void }) {
  const [data, setData] = useState<Page | null>(null); const [summary, setSummary] = useState<Summary | null>(null); const [error, setError] = useState('');
  const [queue, setQueue] = useState<Queue>('all'); const [status, setStatus] = useState(''); const [category, setCategory] = useState('');
  const [location, setLocation] = useState(''); const [search, setSearch] = useState(''); const [page, setPage] = useState(1); const [reload, setReload] = useState(0); const [loading, setLoading] = useState(true);
  useEffect(() => {
    if (!active) return; let live = true; setLoading(true); setError('');
    const q = new URLSearchParams({ page: String(page), queue, status, category, location: search });
    Promise.all([api<Page>(`/api/work-orders?${q}`), api<Summary>('/api/work-orders/summary')]).then(([v, counts]) => { if (live) { if (v.total > 0 && page > Math.ceil(v.total / 20)) { setPage(Math.ceil(v.total / 20)); return; } setData(v); setSummary(counts); } }).catch(e => { if (live) { setError(e.message); setData(null); setSummary(null); } }).finally(() => { if (live) setLoading(false); });
    return () => { live = false; };
  }, [active, page, queue, status, category, search, reload, revision]);
  const filtered = !!(status || category || search);
  function choose(q: Queue) { setQueue(q); setStatus(''); setPage(1); }
  function clear() { setStatus(''); setCategory(''); setLocation(''); setSearch(''); setPage(1); }
  const title = u.role === 'requester' ? '我的报修' : u.role === 'technician' ? '维修任务' : '报修管理';
  const summaryHint = u.role === 'requester' ? '需要补充或确认的工单' : u.role === 'technician' ? '需要接单或处理的任务' : '需要审核或派单的工单';
  return <><div className="page-heading"><div><span className="eyebrow">{roleNames[u.role]}工作台</span><h1>{title}</h1><p className="muted">{u.role === 'requester' ? '先看需要你处理的事项，再跟进其他报修。' : u.role === 'technician' ? '接单、记录维修、提交结果，在这里完成。' : '优先处理审核与派单，跟进每份工单的进展。'}</p></div>{u.role === 'requester' && <button className="primary" onClick={onNew}><Icon kind="plus" />新建报修</button>}</div>
    <div className="overview" aria-label="工单概览">{(['all', 'action', 'active', 'finished'] as Queue[]).map(q => <button className={`overview-card ${q === 'action' ? 'attention' : ''} ${queue === q ? 'chosen' : ''}`} key={q} onClick={() => choose(q)} aria-pressed={queue === q}><span>{queueNames[q]}</span><strong>{summary ? summary[q === 'all' ? 'total' : q] : '—'}</strong><small>{q === 'action' ? summaryHint : q === 'all' ? '当前可查看的全部工单' : q === 'active' ? '包括等待及正在处理' : '已关闭、驳回或取消'}</small></button>)}</div>
    <section className="panel list-panel"><div className="list-title"><div className="queue-tabs" role="group" aria-label="工单队列">{(['all', 'action', 'active', 'finished'] as Queue[]).map(q => <button className={queue === q ? 'active' : ''} aria-pressed={queue === q} key={q} onClick={() => choose(q)}>{queueNames[q]}</button>)}</div><button onClick={() => setReload(x => x + 1)} disabled={loading}>刷新</button></div>
      <form className="filters" onSubmit={e => { e.preventDefault(); setPage(1); setSearch(location.trim()); }}><input aria-label="按位置查找" value={location} onChange={e => setLocation(e.target.value)} placeholder="搜索楼栋、房间或位置" maxLength={200} /><select aria-label="按类别筛选" value={category} onChange={e => { setCategory(e.target.value); setPage(1); }}><option value="">全部类别</option>{Object.entries(categoryNames).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select><select aria-label="按状态筛选" value={status} onChange={e => { setStatus(e.target.value); setPage(1); }}><option value="">全部状态</option>{Object.entries(statusNames).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select><button>查找</button>{filtered && <button type="button" className="text-button" onClick={clear}>清除筛选</button>}</form>
      {loading ? <Loading /> : error ? <div className="empty error-state"><h3>工单暂时无法读取</h3><ErrorMessage text={error} /><button onClick={() => setReload(x => x + 1)}>重新读取</button></div> : data?.items.length ? <>
        <div className="table-scroll desktop-orders"><table><thead><tr><th>位置与报修信息</th><th>状态 / 下一步</th><th>维修人员</th><th>最近更新</th><th /></tr></thead><tbody>{data.items.map(o => <tr key={o.id}><td><button className="order-link" onClick={() => onOpen(o.id)}>{o.location}</button><p className="order-excerpt">{o.description}</p><span className="order-number">{categoryNames[o.category]} · {o.number}</span></td><td><Badge status={o.status} /><span className="next-owner">{primaryAction(u, o) ? actionNames[primaryAction(u, o)] : ['closed', 'rejected', 'cancelled'].includes(o.status) ? '流程已结束' : `等待${statusCopy(u, o).owner}`}</span></td><td>{o.assignee_name || '待安排'}</td><td className="muted nowrap">{time(o.updated_at)}</td><td><button className="row-action" aria-label={`查看 ${o.number}`} onClick={() => onOpen(o.id)}>查看<Icon kind="arrow" /></button></td></tr>)}</tbody></table></div>
        <div className="mobile-orders">{data.items.map(o => <button className="order-card" key={o.id} onClick={() => onOpen(o.id)} aria-label={`查看 ${o.number}`}><div><strong>{o.location}</strong><Badge status={o.status} /></div><p>{o.description}</p><span>{categoryNames[o.category]} · {o.assignee_name || '等待安排维修人员'}</span><footer><span>{time(o.updated_at)}</span><strong>{primaryAction(u, o) ? actionNames[primaryAction(u, o)] : '查看进度'} →</strong></footer></button>)}</div>
      </> : <div className="empty"><span className="empty-icon"><Icon kind={queue === 'action' ? 'check' : 'box'} /></span><h3>{filtered ? '没有匹配的工单' : queue === 'action' ? '暂无需要你处理的事项' : queue === 'finished' ? '暂无已结束工单' : queue === 'active' ? '暂无未结束工单' : '还没有工单'}</h3><p>{filtered ? '换个位置关键词，或清除筛选条件。' : queue === 'action' ? '其他工单的进度可在“全部工单”查看。' : queue === 'all' && u.role === 'requester' ? '遇到设施问题时，提交报修并在这里跟进。' : '符合条件的工单会在这里显示。'}</p>{filtered ? <button onClick={clear}>清除筛选</button> : queue !== 'all' ? <button onClick={() => choose('all')}>查看全部工单</button> : u.role === 'requester' ? <button onClick={onNew}>提交第一份报修</button> : null}</div>}
      {!loading && !error && data && data.total > 0 && <div className="pagination"><span>筛选结果 {data.total} 单 · 第 {page} / {Math.ceil(data.total / 20)} 页</span><div><button disabled={page === 1} onClick={() => setPage(p => p - 1)}>上一页</button><button disabled={page * 20 >= data.total} onClick={() => setPage(p => p + 1)}>下一页</button></div></div>}
    </section></>;
}

function Audit({ onOpen }: { onOpen: (id: number) => void }) {
  type Entry = { id: number; action: string; actor_id: number | null; actor_name: string; resource_id: number | null; order_number: string; outcome: string; created_at: string; request_id: string };
  const [items, setItems] = useState<Entry[]>([]); const [page, setPage] = useState(1); const [total, setTotal] = useState(0); const [error, setError] = useState(''); const [loading, setLoading] = useState(true); const [reload, setReload] = useState(0);
  useEffect(() => { let active = true; setLoading(true); setError(''); api<{ items: Entry[]; total: number }>(`/api/audit?page=${page}`).then(v => { if (active) { setItems(v.items); setTotal(v.total); } }).catch(e => { if (active) setError(e.message); }).finally(() => { if (active) setLoading(false); }); return () => { active = false; }; }, [page, reload]);
  return <><div className="page-heading"><div><span className="eyebrow">管理记录</span><h1>操作审计</h1><p className="muted">按时间查看当前组织的操作人、相关工单与处理结果。</p></div><button onClick={() => setReload(v => v + 1)} disabled={loading}>刷新</button></div><div className="panel list-panel">{loading ? <Loading text="正在读取操作记录…" /> : error ? <div className="empty"><ErrorMessage text={error} /><button onClick={() => setReload(v => v + 1)}>重新读取</button></div> : items.length ? <><div className="table-scroll"><table><thead><tr><th>操作与时间</th><th>操作人</th><th>相关工单</th><th>结果</th></tr></thead><tbody>{items.map(v => <tr key={v.id}><td><strong>{auditLabel(v.action)}</strong><span className="order-number">{time(v.created_at)}</span></td><td>{v.actor_name || (v.actor_id ? `账号 #${v.actor_id}` : '未登录用户')}</td><td>{v.order_number && v.resource_id ? <button className="order-link" onClick={() => onOpen(v.resource_id!)}>{v.order_number}</button> : '—'}</td><td><span className={`badge ${v.outcome === 'success' ? 'closed' : 'rejected'}`}>{v.outcome === 'success' ? '成功' : v.outcome === 'denied' ? '已拒绝' : '未完成'}</span></td></tr>)}</tbody></table></div><div className="pagination"><span>共 {total} 条 · 第 {page} 页</span><div><button disabled={page === 1} onClick={() => setPage(p => p - 1)}>上一页</button><button disabled={page * 20 >= total} onClick={() => setPage(p => p + 1)}>下一页</button></div></div></> : <div className="empty"><h3>暂无操作记录</h3><p>后续业务操作会自动保留在这里。</p></div>}</div></>;
}

function App() {
  const [u, setU] = useState<User | null>(null); const [checking, setChecking] = useState(!!sessionStorage.getItem(tokenKey)); const [message, setMessage] = useState('');
  const [view, setView] = useState('list'); const [selected, setSelected] = useState<number | null>(null); const [logoutError, setLogoutError] = useState(''); const [notice, setNotice] = useState(''); const [revision, setRevision] = useState(0);
  useEffect(() => { const expired = () => { setU(null); setView('list'); setSelected(null); setMessage('登录已失效，请重新登录。'); }; window.addEventListener('session-expired', expired); if (sessionStorage.getItem(tokenKey)) api<{ user: User }>('/api/me').then(v => setU(v.user)).catch(e => setMessage(e.message)).finally(() => setChecking(false)); return () => window.removeEventListener('session-expired', expired); }, []);
  async function logout() { try { await api('/api/logout', { method: 'POST' }); sessionStorage.removeItem(tokenKey); setU(null); setView('list'); setSelected(null); setMessage(''); setLogoutError(''); } catch (e) { setLogoutError((e as Error).message); } }
  if (checking) return <Loading text="正在恢复登录…" />;
  if (!u) return <Login message={message} onLogin={user => { setU(user); setMessage(''); }} />;
  function navigate(v: string) { setView(v); setSelected(null); setNotice(''); }
  function open(id: number) { setSelected(id); setView('detail'); setNotice(''); window.scrollTo(0, 0); }
  return <div className="app-shell"><aside className="sidebar"><div className="brand"><span className="brand-icon"><Icon kind="tool" /></span>校园报修</div><p className="sidebar-label">业务工作台</p><nav><button className={view === 'list' || view === 'detail' ? 'active' : ''} onClick={() => navigate('list')}><Icon />{u.role === 'technician' ? '我的任务' : u.role === 'admin' ? '报修管理' : '我的报修'}</button>{u.role === 'requester' && <button className={view === 'create' ? 'active' : ''} onClick={() => navigate('create')}><Icon kind="plus" />新建报修</button>}{u.role === 'admin' && <button className={view === 'audit' ? 'active' : ''} onClick={() => navigate('audit')}><Icon />操作审计</button>}</nav><div className="sidebar-bottom"><span className="small">当前角色</span><strong>{roleNames[u.role]}</strong></div></aside>
    <div className="workspace"><header><span className="header-path">校园设施服务 <span>/</span> {roleNames[u.role]}工作台</span><div className="account"><span className="avatar">{u.display_name.slice(0, 1)}</span><span>{u.display_name}</span><button className="icon-button" onClick={logout} aria-label="退出登录"><Icon kind="logout" /></button></div></header><main><ErrorMessage text={logoutError} />
      <div hidden={view !== 'list'}><OrderList u={u} active={view === 'list'} revision={revision} onOpen={open} onNew={() => navigate('create')} /></div>
      {view === 'create' && <><div className="page-heading"><div><span className="eyebrow">提交故障</span><h1>新建报修</h1><p className="muted">填写位置与故障现象，提交后即可跟进处理。</p></div></div><CreateOrder onCancel={() => navigate('list')} onCreated={o => { setSelected(o.id); setView('detail'); setNotice('报修已提交，等待管理员审核。'); setRevision(v => v + 1); window.scrollTo(0, 0); }} /></>}
      {view === 'detail' && selected && <OrderDetail key={selected} id={selected} u={u} onBack={() => navigate('list')} notice={notice} onSaved={() => setRevision(v => v + 1)} />}
      {view === 'audit' && <Audit onOpen={open} />}
    </main></div></div>;
}
createRoot(document.getElementById('root')!).render(<React.StrictMode><App /></React.StrictMode>);
