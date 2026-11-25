import { ActionFeedback, useActionFeedback } from './ActionFeedback';
import { Select } from './Select';
import { useNavigation } from './useNavigation';
import { homeView } from './navigation';
import type { View } from './navigation';
import React, { useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { api, APIError, tokenKey, statusNames, categoryNames, roleNames, actionNames, time } from './api';
import type { User, Order, Detail, Page } from './api';
import { primaryAction, statusCopy, progressSteps, progressIndex, queueNames, successCopy } from './presentation';
import type { Queue, Summary } from './presentation';
import './style.css';
import './design.css';
import {VisitPanel} from './VisitPanel';
import type {VisitContext} from './VisitPanel';
import {RepairWorkbench} from './RepairWorkbench';
import { Knowledge } from './Knowledge';
import { Assistant } from './Assistant';
import type { CompletionHandoff, PartsHandoff, TechnicianHandoff, RequesterHandoff } from './assistant-api';
import { AgentSettings } from './AgentSettings';
import { Audit } from './Audit';
import { OrderMediaGallery, OrderMediaInput, orderMediaBody } from './OrderMedia';
import type { SelectedMedia } from './OrderMedia';
import { Badge, LeaveConfirmation, Icon, Loading, PageHeading } from './ui';

// Bottom tabs are only mounted on narrow viewports, so desktop keeps the
// existing sidebar markup and the DOM stays free of duplicate navigation.
function useCompactLayout() {
  const [compact, setCompact] = useState(() => window.matchMedia('(max-width: 800px)').matches);
  useEffect(() => {
    const query = window.matchMedia('(max-width: 800px)');
    const onChange = () => setCompact(query.matches);
    query.addEventListener('change', onChange);
    return () => query.removeEventListener('change', onChange);
  }, []);
  return compact;
}

function ErrorMessage({ text }: { text: string }) { return text ? <div className="error" role="alert">{text}</div> : null; }


function Login({ onLogin, message }: { onLogin: (u: User) => void; message: string }) {
  const [username, setUsername] = useState(''); const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false); const [error, setError] = useState('');
  async function submit(e: React.FormEvent) {
    e.preventDefault(); setBusy(true); setError('');
    try { const v = await api<{ access_token: string; user: User }>('/api/login', { method: 'POST', body: JSON.stringify({ username, password }) }); sessionStorage.setItem(tokenKey, v.access_token); onLogin(v.user); }
    catch (e) { setError((e as Error).message); } finally { setBusy(false); }
  }
  return <main className="login">
    <section className="login-brand" aria-labelledby="campus-repair-title">
      <div className="login-brand-content">
        <span className="brand-icon" aria-hidden="true"><Icon kind="tool" /></span>
        <h1 id="campus-repair-title">校园报修</h1>
        <p>提交报修，查看维修进度。</p>
      </div>
    </section>
    <div className="login-main">
      <form className="login-form" onSubmit={submit} aria-labelledby="login-title">
        <h2 id="login-title">登录</h2>
        <ErrorMessage text={error || message} />
        <label>账号<input autoComplete="username" value={username} onChange={e => setUsername(e.target.value)} required disabled={busy} /></label>
        <label>密码<input type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} required disabled={busy} /></label>
        <button className="primary full" disabled={busy}>{busy ? '正在登录…' : '登录'}</button>
        <p className="login-note">账号由管理员提供</p>
      </form>
    </div>
  </main>;
}

function CreateOrder({ recurrence, onCreated, onCancel, onDirtyChange }: { recurrence?:RequesterHandoff; onCreated: (o: Order) => void; onCancel: () => void; onDirtyChange: (dirty:boolean) => void }) {
  const feedback = useActionFeedback();
  const [input, setInput] = useState({ category: recurrence?.category ?? '', location: recurrence?.location ?? '', description: recurrence?.reason ?? '' }); const key = useRef(crypto.randomUUID());
  const [busy, setBusy] = useState(false); const [error, setError] = useState('');
  const [files, setFiles] = useState<SelectedMedia[]>([]);
  const [mediaStatus, setMediaStatus] = useState('');
  const dirty = !!(input.location || input.description || input.category || files.length);
  useEffect(() => { onDirtyChange(dirty || busy); return () => onDirtyChange(false); }, [dirty, busy, onDirtyChange]);
  useEffect(() => { if (!dirty && !busy) return; const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = ''; }; window.addEventListener('beforeunload', warn); return () => window.removeEventListener('beforeunload', warn); }, [dirty,busy]);
  function update(k: string, v: string) { setInput(i => ({ ...i, [k]: v })); key.current = crypto.randomUUID(); }
  async function submit(e: React.FormEvent) {
    e.preventDefault(); setBusy(true); setError('');
    try {
      const payload = recurrence ? {...input,source_order_id:String(recurrence.id),source_version:String(recurrence.version)} : input;
      const body = files.length
        ? await orderMediaBody(payload, files, (position, count) => setMediaStatus(`正在压缩照片 ${position}/${count}…`))
        : JSON.stringify(recurrence ? {...input,source_order_id:recurrence.id,source_version:recurrence.version} : input);
      if (files.length) setMediaStatus('正在上传并检查附件…');
      const v = await api<{ order: Order }>('/api/work-orders', { method: 'POST', headers: { 'Idempotency-Key': key.current }, body });
      onCreated(v.order);
      feedback({title:"报修提交成功，等待管理员审核",detail:v.order.number});
    } catch (e) { setError((e as Error).message); }
    finally { setBusy(false); setMediaStatus(''); }
  }
  return <div className="create-layout"><div className="panel form-panel"><form onSubmit={submit}><ErrorMessage text={error} /><fieldset disabled={busy}>{recurrence && <p className="confirmation-note">复发报修关联原工单 {recurrence.number}，提交后由管理员审核。</p>}
    <div className="field-section"><h2><span className="step-number">1</span>选择问题类型</h2><label className="create-category-field"><span className="sr-only">问题类型</span><Select aria-label="问题类型" value={input.category} required onChange={e => update('category', e.target.value)}><option value="">请选择问题类型</option>{Object.entries(categoryNames).map(([key, name]) => <option key={key} value={key}>{name}</option>)}</Select></label></div>
    <div className="field-section"><h2><span className="step-number">2</span>填写准确位置</h2><label>具体位置<input value={input.location} onChange={e => update('location', e.target.value)} maxLength={200} required placeholder="例如：3 号楼 201 室，靠窗照明灯" /></label><p className="field-help">写明楼栋、房间或公共区域，便于维修人员找到故障点。</p></div>
    <div className="field-section"><h2><span className="step-number">3</span>描述故障现象</h2><label>故障描述<textarea value={input.description} onChange={e => update('description', e.target.value)} maxLength={2000} required rows={5} placeholder="出现了什么问题？何时发现？目前是什么情况？" /></label><div className="field-bottom"><span>请描述现象，不必判断专业故障原因。</span><span>{[...input.description].length}/2000</span></div></div>
    <OrderMediaInput files={files} status={mediaStatus} onChange={value => { setFiles(value); key.current = crypto.randomUUID(); }} />
    <div className="form-actions"><button type="button" onClick={onCancel}>返回</button><button className="primary">{busy ? '正在提交…' : '确认提交报修'}</button></div></fieldset></form></div>
    <aside className="submission-guide"><h3>提交之后会怎样？</h3><ol><li><strong>管理员审核信息</strong><p>信息不足时，会请你补充。</p></li><li><strong>安排维修人员</strong><p>在工单详情查看人员与进度。</p></li><li><strong>你确认处理结果</strong><p>问题未解决，可以申请重新处理。</p></li></ol><div className="safety-note">如有漏电、冒烟等危险现象，请停止使用并联系现场工作人员。</div></aside></div>;
}

function actionsFor(u: User, o: Order) {
  const a: string[] = [];
  if (u.role === 'admin') { if (o.status === 'submitted') a.push('review'); if (o.status === 'unassigned') a.push('assign'); if (o.status === 'assigned') a.push('reassign'); }
  if (u.role === 'technician') { if (o.status === 'assigned') a.push('accept'); if (o.status === 'in_progress') a.push('complete', 'records'); }
  if (u.role === 'requester') { if (o.status === 'needs_info') a.push('resubmit'); if (o.status === 'awaiting_confirmation') a.push('confirm', 'reopen'); if (['submitted', 'needs_info', 'unassigned', 'assigned'].includes(o.status)) a.push('cancel'); }
  return a;
}
function ActionForm({ u, o, completion, followup, latestRecord, disabled, onDone, onConflict, visitBlocked, onDirtyChange }: { completion?:CompletionHandoff; followup?:RequesterHandoff; latestRecord?:Detail['records'][number]; onDirtyChange:(dirty:boolean)=>void; u: User; o: Order; disabled: boolean; onDone: (message: string) => void; onConflict: () => void; visitBlocked: boolean }) {
  const mainAction = visitBlocked && o.status === 'in_progress' && u.role === 'technician' ? 'records' : primaryAction(u, o); const available = actionsFor(u, o).filter(a => !visitBlocked || a !== 'complete');
  const [chosenAction, setAction] = useState(followup?.action==='reopen'?'reopen':mainAction); const [decision, setDecision] = useState('approve'); const [reason, setReason] = useState(followup?.reason ?? '');
  const [measures, setMeasures] = useState(completion?.measures ?? ''); const [result, setResult] = useState(completion?.result ?? ''); const [tech, setTech] = useState('');
  const [editVersion, setEditVersion] = useState(o.version);
  const hasRepairText = Boolean(measures || result);
  const action = visitBlocked && chosenAction === 'complete' && !hasRepairText ? 'records' : chosenAction;
  const repairForm = action === 'complete' || action === 'records';
  const needsVersionReview = repairForm && hasRepairText && editVersion !== o.version;
  const completionBlocked = action === 'complete' && visitBlocked;

  const [techs, setTechs] = useState<{ id: number; display_name: string; pending_count: number }[]>([]); const [staffLoading, setStaffLoading] = useState(false);
  const [input, setInput] = useState({ category: o.category, location: o.location, description: o.description }); const [error, setError] = useState(''); const [busy, setBusy] = useState(false);
  const dirty = !!(reason || measures || result || tech || decision !== 'approve' || (action === 'resubmit' && (input.category !== o.category || input.location !== o.location || input.description !== o.description)));
  useEffect(() => { onDirtyChange(dirty || busy); return () => onDirtyChange(false); }, [dirty,busy,onDirtyChange]);
  useEffect(() => { if(!dirty&&!busy)return;const warn=(e:BeforeUnloadEvent)=>{e.preventDefault();e.returnValue='';};window.addEventListener('beforeunload',warn);return()=>window.removeEventListener('beforeunload',warn); },[dirty,busy]);
  useEffect(() => {
    let active = true;
    if (action === 'assign' || action === 'reassign') { setStaffLoading(true); api<{ items: typeof techs }>(`/api/technicians?category=${o.category}`).then(v => { if (active) setTechs(v.items.filter(t => action !== 'reassign' || t.id !== o.assignee_id)); }).catch(e => { if (active) setError(e.message); }).finally(() => { if (active) setStaffLoading(false); }); }
    return () => { active = false; };
  }, [action, o.category, o.assignee_id]);

  function choose(a: string) { setAction(a); setError(''); setReason(''); setTech(''); }
  function updateRepair(setter: (value: string) => void, value: string) {
    if (!hasRepairText) {
      setEditVersion(o.version);
      setAction(action);
    }
    setter(value);
  }
  const feedback = useActionFeedback();
  if (!available.length) return null;
  const needsReason = ['reassign', 'reopen', 'cancel'].includes(action) || (action === 'review' && decision !== 'approve');
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (busy || disabled || needsVersionReview || !available.includes(action)) return;
    feedback(null); setBusy(true); setError('');
    try { const {order} = await api<{order:Order}>(`/api/work-orders/${o.id}/${action}`, { method: 'POST', body: JSON.stringify({ version: o.version, decision, reason, technician_id: Number(tech), measures, result, ...input }) });
      if (repairForm) { setMeasures(''); setResult(''); setEditVersion(order.version); setAction(mainAction); }
      const assigned = (action === 'assign' || action === 'reassign') && order.assignee_name;
      onDone(assigned ? `${action === 'assign' ? '派单成功' : '改派成功'}，等待${order.assignee_name}接单。` : successCopy(action, decision));
    }
    catch (e) { setError((e as Error).message); if (e instanceof APIError && e.status === 409) onConflict(); } finally { setBusy(false); }
  }
  function chooseReview(next: string) { setDecision(next); setReason(''); setError(''); }
  const choosingReview = action === 'review' && !needsReason;
  const buttonName = action === 'review' ? { request_info: '发送补充要求', reject: '确认驳回' }[decision] : actionNames[action];
  const heading = action === 'review' ? { approve: '审核', request_info: '要求补充', reject: '驳回报修' }[decision] : actionNames[action];
  const versionReview = needsVersionReview && !disabled && <section className="confirmation-note repair-version-review" aria-label="核对最新工单进展">
    <strong>工单已更新，已保留你填写的内容。</strong>
    {latestRecord ? <dl className="visit-facts">
      <dt>最近记录</dt><dd>{latestRecord.actor_name} · {time(latestRecord.created_at)}</dd>
      <dt>处理措施</dt><dd>{latestRecord.measures}</dd>
      <dt>处理结果</dt><dd>{latestRecord.result}</dd>
    </dl> : <p>这次更新没有新的维修记录，请核对当前状态和上门安排。</p>}
    <button type="button" onClick={() => { setEditVersion(o.version); setError(''); }}>已核对，使用最新版本</button>
  </section>;
  const form = action ? <form onSubmit={submit}>{action !== 'accept' && <h2>{heading}</h2>}<ErrorMessage text={error} />{versionReview}
    {completionBlocked && !disabled && <p className="confirmation-note">请先处理上门异常。<button type="button" onClick={() => choose('records')}>改为记录处理进展</button></p>}
    <fieldset disabled={busy || disabled}>
      {choosingReview && <div className="review-actions">
        <button className="primary" type="submit"><Icon kind="check" />{busy ? '正在保存…' : '审核通过'}</button>
        <button type="button" onClick={() => chooseReview('request_info')}>要求补充</button>
        <button type="button" className="danger-link" onClick={() => chooseReview('reject')}>驳回</button>
      </div>}
      {(action === 'assign' || action === 'reassign') && <><label>维修人员<Select aria-label="维修人员" value={tech} onChange={e => setTech(e.target.value)} required disabled={staffLoading}><option value="">{staffLoading ? '正在读取人员…' : techs.length ? '请选择维修人员' : '暂无匹配的可派人员'}</option>{techs.map(t => <option key={t.id} value={t.id}>{t.display_name} · 待处理 {t.pending_count} 单</option>)}</Select></label><p className="field-help">仅显示负责{categoryNames[o.category]}、当前可用的维修人员。</p></>}
      {needsReason && <label>{action === 'review' && decision === 'request_info' ? '需补充的信息' : action === 'reopen' ? '仍未解决的问题' : action === 'cancel' ? '取消原因' : '原因说明'}<textarea autoFocus={action === 'review'} required value={reason} onChange={e => setReason(e.target.value)} maxLength={2000} rows={3} /></label>}
      {['records', 'complete'].includes(action) && <><label>处理措施<textarea required value={measures} onChange={e => updateRepair(setMeasures, e.target.value)} rows={3} maxLength={2000} placeholder="说明已经完成的检查或维修。" /></label><label>处理结果<textarea required value={result} onChange={e => updateRepair(setResult, e.target.value)} rows={3} maxLength={2000} placeholder={action === 'complete' ? '说明维修后的情况，供报修人确认。' : '说明当前进展和仍需处理的问题。'} /></label></>}
      {action === 'resubmit' && <><label>类别<Select aria-label="故障类别" value={input.category} onChange={e => setInput({ ...input, category: e.target.value })}>{Object.entries(categoryNames).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</Select></label><label>位置<input required value={input.location} onChange={e => setInput({ ...input, location: e.target.value })} maxLength={200} /></label><label>故障描述<textarea required value={input.description} onChange={e => setInput({ ...input, description: e.target.value })} maxLength={2000} rows={4} /></label></>}
      {action === 'confirm' && <p className="confirmation-note">请先检查故障是否已解决。确认后工单将关闭。</p>}
      {action === 'cancel' && <p className="confirmation-note">取消后本次工单结束。如仍需维修，需要重新提交报修。</p>}
      {!choosingReview && <div className="order-submit-actions">
        <button className={`full ${action === 'cancel' || (action === 'review' && decision === 'reject') ? 'danger' : 'primary'}`} disabled={staffLoading || busy || disabled || needsVersionReview || completionBlocked || ((action === 'assign' || action === 'reassign') && !tech)}>{busy ? '正在保存…' : buttonName}</button>
        {action === 'review' && <button type="button" className="text-button" onClick={() => chooseReview('approve')}>返回审核选项</button>}
      </div>}
      {action !== mainAction && <button type="button" className="text-button full" onClick={() => choose(mainAction)}>{mainAction ? '返回主要操作' : '收起编辑'}</button>}
    </fieldset></form> : null;
  if (!mainAction) return <div className={`compact-order-actions ${action ? 'editing' : ''}`} aria-label="工单次要操作">
    {!action && available.map(a => <button key={a} type="button" className={`text-button ${a === 'cancel' ? 'danger-link' : ''}`} disabled={busy || disabled} onClick={() => choose(a)}>{a === 'reassign' ? '调整维修人员' : actionNames[a]}</button>)}
    {form}
  </div>;
  return <section className="action-panel" aria-label="工单操作">
    {form}

    {available.some(a => a !== mainAction) && <details className="secondary-actions"><summary>其他操作</summary><div className="action-options">{available.filter(a => a !== mainAction).map(a => a === action ? <span key={a} className="selected-tag">{actionNames[a]}</span> : <button type="button" key={a} className={a === 'cancel' ? 'danger-link' : ''} disabled={busy || disabled} onClick={() => choose(a)}>{a === 'cancel' ? '需要取消报修？' : a === 'reassign' ? '调整维修人员' : actionNames[a]}</button>)}</div></details>}
  </section>;
}

function OrderDetail({ id, u, completion, parts, followup, onFollowup, onOpenRelated, onBack, onSaved, onDirtyChange, section }: { completion?:CompletionHandoff; parts?:PartsHandoff; followup?:RequesterHandoff; onFollowup:(draft:RequesterHandoff)=>void; onOpenRelated:(id:number)=>void; section?:'visit'; id: number; u: User; onBack: () => void; onSaved: () => void; onDirtyChange: (v: boolean) => void }) {
  const visitTarget=useRef<HTMLDivElement>(null), visitFocused=useRef(false);
  const [visitSnapshot, setVisitSnapshot] = useState<{context: VisitContext; version: number} | null>(null);
  const [actionDirty,setActionDirty]=useState(false),[visitDirty,setVisitDirty]=useState(false);
  useEffect(()=>{onDirtyChange(actionDirty||visitDirty);return()=>onDirtyChange(false);},[actionDirty,visitDirty,onDirtyChange]);
  const [detail, setDetail] = useState<Detail | null>(null); const [error, setError] = useState(''); const [refresh, setRefresh] = useState(0);
  const [loading, setLoading] = useState(true); const [stale, setStale] = useState(false); const feedback = useActionFeedback();
  useEffect(() => {
    let active = true; setLoading(true);
    api<Detail>(`/api/work-orders/${id}`).then(v => { if (active) { setDetail(v); setError(''); setStale(false); } }).catch(e => { if (active) { setError(e.message); setStale(true); if (e instanceof APIError && [401, 403, 404].includes(e.status)) setDetail(null); } }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [id, refresh]);
  useEffect(()=>{if(section!=='visit'||!detail||loading||visitFocused.current)return;visitFocused.current=true;const frame=requestAnimationFrame(()=>{visitTarget.current?.focus({preventScroll:true});visitTarget.current?.scrollIntoView({block:'start',behavior:'instant'});});return()=>cancelAnimationFrame(frame);},[section,detail,loading]);
  const o = detail?.order;
  const visitContext = visitSnapshot?.version === o?.version ? visitSnapshot?.context : undefined;
  const visitBlocked = visitContext ? !!visitContext.exception : null;
  const lastEvent = detail?.events.at(-1);
  const lastRecord = detail?.records.at(-1);
  const needsVisitAttention = o?.status === 'in_progress' && visitBlocked;
  const terminal = !!o && ['closed', 'rejected', 'cancelled'].includes(o.status);
  const copy = o && statusCopy(u, o);
  const owner = needsVisitAttention && visitContext?.exception === 'no_show' && visitContext.needs_availability
    ? o?.requester_name || '报修人' : copy?.owner;
  const mainAction = o && primaryAction(u, o);
  const reasonLabel = o && ({ needs_info: '补充要求', rejected: '驳回原因', cancelled: '取消原因' } as Record<string, string>)[o.status];
  const reason = reasonLabel && lastEvent?.note;
  const beforeReview = o?.status === 'submitted' || o?.status === 'needs_info';
  const waitingForVisit = o?.status === 'in_progress' && u.role === 'technician' && visitBlocked === null;
  const [completionSubmitted,setCompletionSubmitted]=useState(false);
  const validCompletion = o?.status === 'in_progress' && o.version === completion?.version && visitBlocked !== true ? completion : undefined;
  const validFollowup = o?.status==='awaiting_confirmation' && followup?.version===o.version ? followup : undefined;
  const repairEditable = o?.status === 'in_progress' && u.role === 'technician';
  const actionForm = o && actionsFor(u, o).length > 0 ? <ActionForm key={repairEditable ? `${id}-repair` : `${id}-${o.version}`} completion={validCompletion} followup={validFollowup} latestRecord={lastRecord} u={u} o={o} onDirtyChange={setActionDirty} visitBlocked={!!visitBlocked} disabled={loading || stale || waitingForVisit} onConflict={() => setStale(true)} onDone={msg => { if(validCompletion || validFollowup)setCompletionSubmitted(true); feedback({title:msg,detail:o.number}); setRefresh(v => v + 1); onSaved(); }} /> : null;
  const visitPanel = o && (!beforeReview || section === 'visit' || u.role === 'requester') && <div ref={visitTarget} className="visit-target" tabIndex={-1} aria-label="工单上门记录">
    <VisitPanel o={o} u={u} partsDraft={parts} onContextChange={setVisitSnapshot} disabled={loading || stale} onSaved={() => {setRefresh(v => v + 1); onSaved();}} onDirtyChange={setVisitDirty} />
  </div>;
  const rank = o ? progressIndex(o.status) : -1;
  const showResultFirst = o?.status === 'awaiting_confirmation' || o?.status === 'closed';
  const latestResult = detail && lastRecord && <section className={`latest-result ${showResultFirst ? 'inline-result' : 'panel'}`}>
    <div className="section-heading"><h2>{showResultFirst ? '最近维修结果' : '最近处理记录'}</h2><span>{time(lastRecord.created_at)}</span></div>
    <p className="result-text">{lastRecord.result}</p>
    <dl><dt>处理措施</dt><dd>{lastRecord.measures}</dd><dt>处理人员</dt><dd>{lastRecord.actor_name}</dd></dl>
    {detail.records.length > 1 && <details className="past-records"><summary>查看此前 {detail.records.length - 1} 次处理记录</summary>{detail.records.slice(0, -1).reverse().map(r => <div className="repair-record" key={r.id}><p className="muted small">{r.actor_name} · {time(r.created_at)}</p><p>措施：{r.measures}</p><p>结果：{r.result}</p></div>)}</details>}
  </section>;

  return <>
    <div className="detail-toolbar"><button className="back-button" onClick={onBack}><Icon kind="left" />返回</button><button onClick={() => setRefresh(v => v + 1)} disabled={loading}><Icon kind="refresh" />刷新进度</button></div>
    <ErrorMessage text={error} />{stale && detail && <div className="error" role="alert">当前仅显示上次读取的信息，请刷新后再操作。</div>}
    {!detail ? loading ? <Loading /> : <div className="panel empty"><h3>暂时无法读取工单</h3><p>请重试，或返回列表查看可访问的工单。</p><button onClick={() => setRefresh(v => v + 1)}>重新读取</button></div> : o && copy && <div className="order-detail">
      <section className="panel order-work" aria-labelledby="order-category">
        <div className="order-overview">
          <header className="order-overview-heading">
            <div className="order-overview-fields">
              <h1 id="order-category"><span>问题类型</span><strong>{categoryNames[o.category] || '报修工单'}</strong></h1>
              <div className="order-location"><span>报修位置</span><strong>{o.location}</strong></div>
            </div>
            <span role="status" aria-label={`当前状态：${statusNames[o.status] || '状态待确认'}`}><Badge status={o.status} /></span>
          </header>
          <section className="order-fault-section" aria-labelledby="order-fault-label">
            <h2 id="order-fault-label">故障详情</h2>
            <p className="order-fault">{o.description}</p>
          </section>
          <OrderMediaGallery orderID={o.id} />
          <dl className="order-facts" aria-label="报修基本信息">
            <div><dt>报修人</dt><dd>{o.requester_name}</dd></div>
            <div><dt>提交时间</dt><dd>{time(o.created_at)}</dd></div>
            {!terminal && <div><dt>当前处理方</dt><dd>{owner}</dd></div>}
            <div><dt>工单编号</dt><dd>{o.number}</dd></div>
          </dl>
          {needsVisitAttention ? <p className="order-context order-visit-alert">上门安排需要跟进，见下方上门安排。</p> : !terminal && !mainAction && !reason && <p className="order-context">{copy.description}</p>}
        </div>
        {reason && <p className="order-reason"><strong>{reasonLabel}：</strong>{reason}</p>}
        {showResultFirst && latestResult}
        {detail.recurrence_source && <p className="order-context"><button className="text-button" onClick={()=>onOpenRelated(detail.recurrence_source!.id)}>查看原工单 {detail.recurrence_source.number}</button></p>}
        {detail.recurrence_followup && <p className="order-context"><button className="text-button" onClick={()=>onOpenRelated(detail.recurrence_followup!.id)}>查看复发报修 {detail.recurrence_followup.number}</button></p>}
        {u.role==='requester' && o.status==='closed' && lastRecord && !detail.recurrence_followup && <p className="order-context">
          <button onClick={()=>onFollowup({id:o.id,version:o.version,number:o.number,category:o.category,location:o.location,action:'recurrence',reason:''})}>反馈故障复发</button>
        </p>}
        {followup && !validFollowup && !completionSubmitted && <p role="status" className="order-context">工单已更新，请核对最新结果后再操作。</p>}
        {completion && !completionSubmitted && !waitingForVisit && !validCompletion && <p role="status" className="order-context">当前工单状态或上门安排已变化，请核对后重新准备上报。</p>}
        {validCompletion && <p className="order-context">已填入助手整理的草稿，请核对处理措施和维修结果。</p>}
        {actionForm}
      </section>

      {beforeReview && section !== 'visit' && visitPanel
        ? <details className="panel order-process visit-preference"><summary>方便上门时间（可选）</summary>{visitPanel}</details>
        : visitPanel}
      {!showResultFirst && latestResult}
      <section className="panel order-history"><div className="section-heading"><h2>处理时间线</h2><span>{detail.events.length} 条记录</span></div><ol className="timeline">{[...detail.events].reverse().map((e, i) => <li key={e.id} className={i === 0 ? 'latest' : ''}><span className="dot" /><div><div className="event-heading"><strong>{actionNames[e.action] || '工单操作'}</strong><time>{time(e.created_at)}</time></div><p>{e.actor_name} · {statusNames[e.to_status]}</p>{e.note && <div className="event-note">{e.note}</div>}</div></li>)}</ol></section>
      <details className="panel order-process">
        <summary>{rank >= 0 ? '处理流程与工单信息' : '工单信息'}</summary>
        {rank >= 0 && <ol className="progress-track" aria-label="处理流程">{progressSteps.map((label, i) => <li key={label} className={i < rank ? 'done' : i === rank ? 'current' : ''} aria-current={i === rank ? 'step' : undefined}><span>{i < rank ? <Icon kind="check" /> : i + 1}</span><strong>{label}</strong></li>)}</ol>}
        <dl>
          <div><dt>维修人员</dt><dd>{o.assignee_name || '未安排'}</dd></div>
          <div><dt>最近更新</dt><dd>{time(o.updated_at)}</dd></div>
        </dl>
      </details>

    </div>}
  </>;
}

const technicianStatuses = ['assigned', 'in_progress', 'awaiting_confirmation', 'closed', 'cancelled'];
const technicianQueues = ['all', 'assigned', 'in_progress', 'awaiting_confirmation', 'finished'] as const;
type TaskQueue = Queue | typeof technicianQueues[number];

function OrderList({ u, active, revision, onOpen, onNew }: { u: User; active: boolean; revision: number; onOpen: (id: number) => void; onNew: () => void }) {
  const [data, setData] = useState<Page | null>(null); const [summary, setSummary] = useState<Summary | null>(null); const [error, setError] = useState('');
  const todoTitle=useRef<HTMLHeadingElement>(null);const [todoJump,setTodoJump]=useState(0),[todoFeedback,setTodoFeedback]=useState('');
  useEffect(()=>{if(!todoJump)return;const frame=requestAnimationFrame(()=>{todoTitle.current?.focus({preventScroll:true});todoTitle.current?.scrollIntoView({block:'start',behavior:'instant'});});return()=>cancelAnimationFrame(frame);},[todoJump]);
  const technician = u.role === 'technician';
  const [queue, setQueue] = useState<TaskQueue>(technician ? 'assigned' : u.role === 'requester' ? 'all' : 'action'); const [status, setStatus] = useState(''); const [category, setCategory] = useState('');
  const [location, setLocation] = useState(''); const [search, setSearch] = useState(''); const [page, setPage] = useState(1); const [reload, setReload] = useState(0); const [loading, setLoading] = useState(true);
  const queueStatus = technicianStatuses.includes(queue) ? queue : '';
  const listQueue = queueStatus ? 'all' : queue;
  const options = technician ? (queue === 'finished' ? ['closed', 'cancelled'] : technicianStatuses) : Object.keys(statusNames);
  const label = (key: TaskQueue) => technician
    ? key === 'all' ? '全部任务' : key === 'awaiting_confirmation' ? '待报修人确认' : statusNames[key] || queueNames[key as Queue]
    : queueNames[key as Queue];
  const count = (key: TaskQueue) => summary
    ? technicianStatuses.includes(key) ? summary.statuses[key] ?? 0 : summary[key === 'all' ? 'total' : key as Queue & keyof Summary]
    : '—';
  useEffect(() => {
    if (!active) return; let live = true; setLoading(true); setError('');
    const q = new URLSearchParams({ page: String(page), queue: listQueue, status: queueStatus || status, category, location: search });
    Promise.all([api<Page>(`/api/work-orders?${q}`), api<Summary>('/api/work-orders/summary')]).then(([v, counts]) => { if (live) { if (v.total > 0 && page > Math.ceil(v.total / 20)) { setPage(Math.ceil(v.total / 20)); return; } setData(v); setSummary(counts); } }).catch(e => { if (live) { setError(e.message); setData(null); setSummary(null); } }).finally(() => { if (live) setLoading(false); });
    return () => { live = false; };
  }, [active, page, listQueue, queueStatus, status, category, search, reload, revision]);
  const filtered = !!(status || category || search);
  function choose(q: TaskQueue) { setTodoFeedback(''); setQueue(q); setStatus(''); setPage(1); }
  function showTodo(){setTodoFeedback(queue==='action'&&!filtered&&page===1?'当前已是待办列表，已定位到工单区域。':'已定位到待办列表，并清除位置、类别和状态筛选。');setQueue('action');setStatus('');setCategory('');setLocation('');setSearch('');setPage(1);setTodoJump(v=>v+1);}
  function clear() { setStatus(''); setCategory(''); setLocation(''); setSearch(''); setPage(1); }
  const title = u.role === 'requester' ? '我的报修' : u.role === 'technician' ? '维修任务' : '报修管理';
  return <><PageHeading title={title}>{u.role === 'requester' && <button className="primary" onClick={onNew}><Icon kind="plus"/>新建报修</button>}</PageHeading>
    <div className={`overview order-queues ${technician ? 'technician-queues' : ''}`} aria-label="工单概览">{(technician ? technicianQueues : (['all', 'action', 'active', 'finished'] as const)).map(q => <button className={`overview-card ${q === 'action' ? 'attention' : ''} ${queue === q ? 'chosen' : ''}`} key={q} onClick={q === 'action' ? showTodo : () => choose(q)} aria-pressed={queue === q}><span>{label(q)}</span><strong>{count(q)}</strong></button>)}</div>
    <section className="panel list-panel order-list-panel" aria-busy={loading}><div className="list-title"><h2 ref={todoTitle} tabIndex={-1} className="list-section-title">{label(queue)}<span className="count">{data && !loading ? data.total : '—'}</span></h2><button onClick={() => setReload(x => x + 1)} disabled={loading}>{loading ? '正在读取…' : '刷新'}</button></div>
      {todoFeedback&&<p className="todo-feedback" role="status" key={todoJump}>{todoFeedback}</p>}
      <form className="filters" onSubmit={e => { e.preventDefault(); setPage(1); setSearch(location.trim()); }}><input aria-label="按位置查找" value={location} onChange={e => setLocation(e.target.value)} placeholder="搜索楼栋、房间或位置" maxLength={200} /><Select aria-label="按类别筛选" value={category} onChange={e => { setCategory(e.target.value); setPage(1); }}><option value="">全部类别</option>{Object.entries(categoryNames).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</Select>{!queueStatus && <Select aria-label="按状态筛选" value={status} onChange={e => { setStatus(e.target.value); setPage(1); }}><option value="">全部状态</option>{options.map(k => <option key={k} value={k}>{k==='awaiting_confirmation' && technician ? '待报修人确认' : statusNames[k]}</option>)}</Select>}<button>查找</button>{filtered && <button type="button" className="text-button" onClick={clear}>清除筛选</button>}</form>
      {loading && !data ? <Loading /> : error ? <div className="empty error-state"><h3>工单暂时无法读取</h3><ErrorMessage text={error} /><button onClick={() => setReload(x => x + 1)}>重新读取</button></div> : data?.items.length ? <>
        <div className="table-scroll desktop-orders"><table><thead><tr><th>位置与报修信息</th><th>状态</th>{!technician && <th>维修人员</th>}<th>最近更新</th><th /></tr></thead><tbody>{data.items.map(o => <tr key={o.id} onClick={event => { if (!(event.target instanceof Element) || !event.target.closest('button, a')) onOpen(o.id); }}><td><button className="order-link" onClick={() => onOpen(o.id)}>{o.location}</button><p className="order-excerpt">{o.description}</p><span className="order-number">{categoryNames[o.category]} · {o.number}</span></td><td><Badge status={o.status} /></td>{!technician && <td>{o.assignee_name || '待安排'}</td>}<td className="muted nowrap">{time(o.updated_at)}</td><td><button className="row-action" aria-label={`查看 ${o.number}`} onClick={() => onOpen(o.id)}>{primaryAction(u,o) ? actionNames[primaryAction(u,o)] : '查看进度'}<Icon kind="arrow" /></button></td></tr>)}</tbody></table></div>
        <div className="mobile-orders">{data.items.map(o => <button className="order-card" key={o.id} onClick={() => onOpen(o.id)} aria-label={`查看 ${o.number}`}><div><strong>{o.location}</strong><Badge status={o.status} /></div><p>{o.description}</p><span>{categoryNames[o.category]}{!technician && ` · ${o.assignee_name || '等待安排维修人员'}`}</span><footer><span>{time(o.updated_at)}</span><strong>{primaryAction(u, o) ? actionNames[primaryAction(u, o)] : '查看进度'} →</strong></footer></button>)}</div>
      </> : <div className="empty"><span className="empty-icon"><Icon kind={queue === 'action' ? 'check' : 'box'} /></span><h3>{filtered ? '没有匹配的工单' : queue === 'action' ? '暂无需要你处理的事项' : queue === 'finished' ? '暂无已结束工单' : queue === 'active' ? '暂无未结束工单' : queueStatus ? `暂无${label(queue)}的任务` : '还没有工单'}</h3><p>{filtered ? '换个位置关键词，或清除筛选条件。' : queue === 'action' ? '其他工单的进度可在“全部工单”查看。' : queue === 'all' && u.role === 'requester' ? '遇到设施问题时，提交报修并在这里跟进。' : '符合条件的工单会在这里显示。'}</p>{filtered ? <button onClick={clear}>清除筛选</button> : queue !== 'all' ? <button onClick={() => choose('all')}>查看全部工单</button> : u.role === 'requester' ? <button onClick={onNew}>提交第一份报修</button> : null}</div>}
      {!loading && !error && data && data.total > 0 && <div className="pagination"><span>筛选结果 {data.total} 单 · 第 {page} / {Math.ceil(data.total / 20)} 页</span><div><button disabled={page === 1} onClick={() => setPage(p => p - 1)}>上一页</button><button disabled={page * 20 >= data.total} onClick={() => setPage(p => p + 1)}>下一页</button></div></div>}
    </section></>;
}

function App() {
  const [u, setU] = useState<User | null>(null), [checking, setChecking] = useState(!!sessionStorage.getItem(tokenKey)), [message, setMessage] = useState('');
  const compact = useCompactLayout();
  const [menuOpen, setMenuOpen] = useState(false), [draftDirty,setDraftDirty]=useState(false), [visitDirty,setVisitDirty]=useState(false), [settingsDirty,setSettingsDirty]=useState(false), [createDirty,setCreateDirty]=useState(false);
  const navigation=useNavigation(u?.role || null, draftDirty || visitDirty || settingsDirty || createDirty);
  const {view,id:selected,from,section}=navigation.route;
  const [technicianDraft,setTechnicianDraft]=useState<TechnicianHandoff>();
  const [requesterDraft,setRequesterDraft]=useState<RequesterHandoff>();
  useEffect(()=>{if(view!=='detail' && view!=='create')setRequesterDraft(undefined);},[view]);
  useEffect(()=>{if(view!=='detail')setTechnicianDraft(undefined);},[view]);
  useEffect(()=>{if(!u){setTechnicianDraft(undefined);setRequesterDraft(undefined);}},[u]);
  const [assistantDiscard,setAssistantDiscard]=useState(0);
  const [visited,setVisited]=useState<Set<View>>(new Set(['list']));
  const [logoutError,setLogoutError]=useState(''),[revision,setRevision]=useState(0);
  const menuButton=useRef<HTMLButtonElement>(null), menuPanel=useRef<HTMLElement>(null);
  const content=useRef<HTMLElement>(null);
  useEffect(()=>{setVisited(v=>v.has(view)?v:new Set([...v,view]));setMenuOpen(false);document.title=`${view==='detail'?'工单详情':view==='assistant'?'报修助手':'工作台'} · 校园报修`;},[view,selected]);
  useEffect(()=>{
    if(!menuOpen)return;
    const overflow=document.body.style.overflow; document.body.style.overflow='hidden';
    menuPanel.current?.querySelector<HTMLElement>('[data-nav-close]')?.focus();
    const keys=(e:KeyboardEvent)=>{
      if(e.key==='Escape'){e.preventDefault();setMenuOpen(false);}
      if(e.key!=='Tab')return;
      const nodes=[...(menuPanel.current?.querySelectorAll<HTMLElement>('button:not(:disabled),a[href],[tabindex="0"]')||[])].filter(n=>n.getClientRects().length>0);
      const first=nodes[0],last=nodes.at(-1);
      if(e.shiftKey&&document.activeElement===first){e.preventDefault();last?.focus();}
      if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus();}
    };
    const desktop=window.matchMedia('(min-width:801px)'),resize=()=>{if(desktop.matches)setMenuOpen(false);};
    document.addEventListener('keydown',keys);desktop.addEventListener('change',resize);
    return()=>{document.removeEventListener('keydown',keys);desktop.removeEventListener('change',resize);document.body.style.overflow=overflow;if(desktop.matches)content.current?.focus({preventScroll:true});else menuButton.current?.focus({preventScroll:true});};
  },[menuOpen]);
  useEffect(()=>{const expired=()=>{setU(null);setMessage('登录已失效，请重新登录。');};window.addEventListener('session-expired',expired);if(sessionStorage.getItem(tokenKey))api<{user:User}>('/api/me').then(v=>setU(v.user)).catch(e=>setMessage(e.message)).finally(()=>setChecking(false));return()=>window.removeEventListener('session-expired',expired);},[]);
  // Use the visual viewport so the composer also fits above an on-screen keyboard.
  useEffect(()=>{const update=()=>document.documentElement.style.setProperty('--app-height',`${window.visualViewport?.height || window.innerHeight}px`);update();window.visualViewport?.addEventListener('resize',update);window.addEventListener('resize',update);return()=>{window.visualViewport?.removeEventListener('resize',update);window.removeEventListener('resize',update);};},[]);
  function clearDirty(){setDraftDirty(false);setVisitDirty(false);setSettingsDirty(false);setCreateDirty(false);}
  async function doLogout(){try{await api('/api/logout',{method:'POST'});sessionStorage.removeItem(tokenKey);history.replaceState(null,'','#/login');setU(null);setVisited(new Set(['list']));clearDirty();setMessage('');setLogoutError('');}catch(e){setLogoutError((e as Error).message);}}
  function navigate(v:View){setMenuOpen(false);navigation.navigate({view:v});}
  function open(id:number,section?:'visit'){navigation.navigate({view:'detail',id,section,from:view==='detail'?from:view});}
  function openRequesterFollowup(draft:RequesterHandoff){
    setRequesterDraft(draft);
    if(draft.action==='recurrence') navigation.navigate({view:'create',from:view});
    else open(draft.id);
  }

  if(checking)return <Loading text="正在恢复登录…"/>;
  if(!u)return <Login message={message} onLogin={user=>{clearDirty();setU(user);setVisited(new Set(['list']));setMessage('');}}/>;
  const listName=u.role==='technician'?'我的任务':u.role==='admin'?'报修管理':'我的报修';
  const viewNames:Record<View,string>={list:listName,detail:'工单详情',create:'新建报修',assistant:'报修助手',agents:'助手设置',knowledge:'知识库',audit:'操作审计',workbench:u.role==='admin'?'上门异常':'维修工作台'};
  const parent=view==='detail'?(from || homeView(u.role)):view==='create'?(from || 'list'):homeView(u.role);
  const sectionView=view==='detail'?parent:view;
  const activeView=u.role==='admin'&&sectionView==='workbench'?'list':sectionView;
  const navItem=(key:View,label:string,icon:string)=><button className={activeView===key?'active':''} aria-current={activeView===key?'page':undefined} onClick={()=>navigate(key)}><Icon kind={icon}/><span>{label}</span></button>;
  const mobileTabs:{key:View;label:string;icon:string}[]=u.role==='requester'
    ?[{key:'list',label:'我的报修',icon:'orders'},{key:'create',label:'新建报修',icon:'plus'},{key:'assistant',label:'报修助手',icon:'chat'}]
    :u.role==='technician'
    ?[{key:'workbench',label:'工作台',icon:'tool'},{key:'list',label:'我的任务',icon:'orders'},{key:'assistant',label:'报修助手',icon:'chat'}]
    :[{key:'list',label:'报修管理',icon:'orders'},{key:'workbench',label:'上门异常',icon:'tool'},{key:'assistant',label:'报修助手',icon:'chat'},{key:'knowledge',label:'知识库',icon:'book'}];

  return <ActionFeedback key={u.id}><div className={`app-shell ${view==='assistant'?'assistant-mode':''}`} key={u.id}>
    <a className="skip-link" href="#main-content" onClick={e=>{e.preventDefault();content.current?.focus();}}>跳到主要内容</a>
    {menuOpen&&<button className="nav-backdrop" aria-label="关闭导航" onClick={()=>setMenuOpen(false)}/>}
    <aside ref={menuPanel} className={`sidebar ${menuOpen?'mobile-open':''}`} id="main-navigation" aria-label="主导航"><button className="nav-close" data-nav-close aria-label="关闭导航菜单" onClick={()=>setMenuOpen(false)}><Icon kind="close"/>关闭菜单</button><div className="brand"><span className="brand-icon"><Icon kind="tool"/></span><div>校园报修<small>校园设施服务平台</small></div></div>
      <p className="sidebar-label">日常工作</p><nav aria-label="业务导航">{u.role==='technician'&&navItem('workbench','维修工作台','tool')}{navItem('list',listName,'orders')}{u.role==='requester'&&navItem('create','新建报修','plus')}{navItem('assistant','报修助手','chat')}</nav>
      {u.role==='admin'&&<><p className="sidebar-label">平台管理</p><nav aria-label="管理导航">{navItem('knowledge','知识库','book')}{navItem('agents','助手设置','settings')}{navItem('audit','操作审计','audit')}</nav></>}
      <div className="sidebar-bottom"><span className="avatar">{u.display_name.slice(0,1)}</span><div><strong>{u.display_name}</strong><span>{roleNames[u.role]}</span></div></div>
    </aside>
    <div className="workspace" inert={menuOpen}><header className="app-header"><div className="header-location"><button ref={menuButton} className="icon-button mobile-menu" aria-label="打开导航" aria-controls="main-navigation" aria-expanded={menuOpen} onClick={()=>setMenuOpen(v=>!v)}><Icon kind="menu"/></button><nav className="header-path" aria-label="当前位置"><button onClick={()=>navigate(homeView(u.role))}>工作台</button><Icon kind="arrow"/>{(view==='detail'||view==='create')&&<><button onClick={()=>navigate(parent)}>{viewNames[parent]}</button><Icon kind="arrow"/></>}{u.role==='admin'&&view==='workbench'&&<><button onClick={()=>navigate('list')}>报修管理</button><Icon kind="arrow"/></>}<strong aria-current="page">{viewNames[view]}</strong></nav></div><div className="account"><span>{u.display_name}</span><button className="logout-button" onClick={()=>navigation.guard(()=>void doLogout())} aria-label="退出登录"><Icon kind="logout"/><span>退出</span></button></div></header><main id="main-content" tabIndex={-1} ref={content}><ErrorMessage text={logoutError}/>{u.role==='admin'&&(view==='list'||view==='workbench')&&<nav className="management-tabs" aria-label="报修管理视图"><button aria-current={view==='list'?'page':undefined} className={view==='list'?'active':''} onClick={()=>navigate('list')}>工单列表</button><button aria-current={view==='workbench'?'page':undefined} className={view==='workbench'?'active':''} onClick={()=>navigate('workbench')}>上门异常</button></nav>}
      <div hidden={view!=='list'}><OrderList u={u} active={view==='list'} revision={revision} onOpen={open} onNew={()=>navigate('create')}/></div>
      {visited.has('workbench')&&u.role!=='requester'&&<div hidden={view!=='workbench'}><RepairWorkbench u={u} active={view==='workbench'} onOpen={open}/></div>}
      {view==='create'&&<><div className="page-back"><button className="back-button" onClick={navigation.back}><Icon kind="left"/>返回{listName}</button></div><PageHeading title={requesterDraft?.action==='recurrence'?'反馈故障复发':'新建报修'} description="填写位置与故障现象，提交后即可跟进处理。"/><CreateOrder recurrence={requesterDraft?.action==='recurrence'?requesterDraft:undefined} onDirtyChange={setCreateDirty} onCancel={navigation.back} onCreated={o=>{setRequesterDraft(undefined);clearDirty();navigation.navigate({view:'detail',id:o.id,from:'list'},true,true);setRevision(v=>v+1);}}/></>}
      {view==='detail'&&selected&&<OrderDetail key={`${selected}-${section||'order'}`} followup={requesterDraft?.id===selected && requesterDraft.action!=='recurrence'?requesterDraft:undefined} onFollowup={openRequesterFollowup} onOpenRelated={open} completion={u.role==='technician' && selected===technicianDraft?.id && 'measures' in technicianDraft ? technicianDraft : undefined} parts={u.role==='technician' && selected===technicianDraft?.id && 'resume_at' in technicianDraft ? technicianDraft : undefined} section={section} id={selected} u={u} onBack={navigation.back} onDirtyChange={setVisitDirty} onSaved={()=>setRevision(v=>v+1)}/>}
      {view==='knowledge'&&u.role==='admin'&&<Knowledge/>}
      {visited.has('assistant')&&<div className="assistant-page" hidden={view!=='assistant'}><Assistant onRequesterFollowup={openRequesterFollowup} orderRevision={revision} onCompletion={draft=>{setTechnicianDraft(draft);open(draft.id, 'resume_at' in draft ? 'visit' : undefined);}} role={u.role} active={view==='assistant'} onManual={()=>navigate('list')} onOpen={open} onDirtyChange={setDraftDirty} storageKey={`campus-assistant-${u.id}`} discardSignal={assistantDiscard}/></div>}
      {view==='agents'&&u.role==='admin'&&<AgentSettings onDirtyChange={setSettingsDirty}/>}
      {view==='audit'&&u.role==='admin'&&<Audit onOpen={open}/>}
    </main>{compact&&<nav className="mobile-tabs" aria-label="快速导航">{mobileTabs.map(item=><button key={item.key} className={sectionView===item.key?'active':''} aria-current={sectionView===item.key?'page':undefined} onClick={()=>navigate(item.key)}><Icon kind={item.icon}/><span>{item.label}</span></button>)}</nav>}</div>{navigation.pending&&<LeaveConfirmation title="有未保存的更改" description="离开会放弃未保存的编辑。正在生成或提交时，请等待完成后再离开。" onCancel={navigation.cancel} onDiscard={() => {
     if(draftDirty)setAssistantDiscard(v=>v+1);
     clearDirty();
     navigation.proceed();
    }}/>}</div></ActionFeedback>;
}
createRoot(document.getElementById('root')!).render(<React.StrictMode><App/></React.StrictMode>);
