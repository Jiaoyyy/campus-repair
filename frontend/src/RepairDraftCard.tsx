import { useActionFeedback } from './ActionFeedback';
import { Select } from './Select';
import { useEffect, useRef, useState } from 'react';
import { api, APIError, categoryNames } from './api';
import type { Order } from './api';
import type { RepairDraft } from './assistant-api';

import { OrderMediaInput, orderMediaBody } from './OrderMedia';
import type { SelectedMedia } from './OrderMedia';

type Fields = Pick<RepairDraft, 'category' | 'location' | 'description'>;
const fields = (d: Fields): Fields => ({ category: d.category, location: d.location, description: d.description });
const same = (a: Fields, b: Fields) => a.category === b.category && a.location === b.location && a.description === b.description;
export function RepairDraftCard({ draft, disabled, onChange, onDirtyChange, onBusyChange, onOpen }: {
 draft: RepairDraft; disabled: boolean; onChange: (d: RepairDraft) => void; onDirtyChange: (v: boolean) => void; onBusyChange: (v: boolean) => void; onOpen: (id: number) => void;
}) {
 const [input, setInput] = useState<Fields>(() => fields(draft)), [saved, setSaved] = useState(draft);
 const [busy, setBusy] = useState(false), [error, setError] = useState('');
 const [files, setFiles] = useState<SelectedMedia[]>([]);
 const [mediaStatus, setMediaStatus] = useState('');
 const feedback = useActionFeedback();
 const lock = useRef(false);
 const dirty = saved.status === 'active' && !same(input, fields(saved));
 useEffect(() => { setSaved(draft); setInput(fields(draft)); }, [draft]);
 useEffect(() => { onDirtyChange(dirty || (saved.status === 'active' && files.length > 0)); return () => onDirtyChange(false); }, [dirty, saved.status, files.length, onDirtyChange]);
 const path = `/api/conversations/${saved.conversation_id}/drafts/${saved.message_id}`;
 const valid = !!input.category && !!input.location.trim() && !!input.description.trim() && [...input.location].length <= 200 && [...input.description].length <= 2000;
 function remember(d: RepairDraft) { if (d.status !== 'active') setFiles([]); setSaved(d); setInput(fields(d)); onChange(d); }
 async function save(status = 'active') {
  const v = await api<{ draft: RepairDraft }>(path, { method: 'PUT', body: JSON.stringify({ ...input, version: saved.version, status }) });
  remember(v.draft); return v.draft;
 }
 async function run(action: 'save' | 'cancel' | 'submit' | 'reload') {
  if (lock.current) return; lock.current = true; feedback(null); setBusy(true); onBusyChange(true); setError('');
  try {
   if (action === 'reload') { const v = await api<{ draft: RepairDraft }>(path); remember(v.draft); return; }
   const d = await save(action === 'cancel' ? 'cancelled' : 'active');
   if (action === 'submit') {
    const payload = { ...fields(d), draft_message_id: d.message_id, draft_version: d.version };
    const body = files.length ? await orderMediaBody({ ...fields(d), draft_message_id: String(d.message_id), draft_version: String(d.version) }, files,
      (position, count) => setMediaStatus(`正在压缩照片 ${position}/${count}…`)) : JSON.stringify(payload);
    if (files.length) setMediaStatus('正在上传并检查附件…');
    const v = await api<{ order: Order }>('/api/work-orders', { method: 'POST', headers: { 'Idempotency-Key': d.idempotency_key }, body });
    feedback({title:'报修提交成功，等待管理员审核',detail:v.order.number});
    remember({ ...d, status: 'submitted', order_id: v.order.id, order_number: v.order.number });
   } else if (action === 'save') feedback({title:'草稿修改已保存',detail:'确认提交后生成工单。'});
  } catch (e) {
   // A dropped response can follow a committed create. Reconcile without inventing success.
   let recovered = false;
   try {
    const v = await api<{ draft: RepairDraft }>(path);
    if (v.draft.status === 'submitted' || (action === 'cancel' && v.draft.status === 'cancelled')) {
     remember(v.draft); recovered = true;
     if (v.draft.status === 'submitted' && e instanceof APIError && e.status === 409) setError('草稿已由另一笔提交确认，请在工单详情核对最终内容和附件。');
    }
    else if (v.draft.status === 'active' && same(fields(v.draft), { ...input, location: input.location.trim(), description: input.description.trim() })) { remember(v.draft); recovered = action === 'save'; }
   } catch { /* Preserve the user's current inputs. */ }
   if (!recovered) setError((e as Error).message + '。输入已保留，可重试；如有版本冲突，请重新读取草稿。');
  } finally { setMediaStatus(''); lock.current = false; setBusy(false); onBusyChange(false); }
 }
 if (saved.status !== 'active') return <section className="repair-draft draft-resolved" aria-label="报修草稿">
  {error && <p role="status">{error}</p>}
  <strong>{saved.status === 'submitted' ? '报修已提交' : saved.status === 'cancelled' ? '草稿已取消' : '已由后续草稿替代'}</strong>
  {saved.status === 'submitted' && saved.order_id && <><p>工单编号：{saved.order_number}</p><button type="button" onClick={() => onOpen(saved.order_id!)}>查看工单详情</button></>}
 </section>;
 return <section className="repair-draft" aria-label="报修草稿"><div className="section-heading"><h3>报修草稿</h3><span className="badge">尚未提交</span></div>
  <p className="muted">核对信息后确认提交，管理员收到工单后会进行审核。</p>
  <form onSubmit={e => e.preventDefault()}><fieldset disabled={busy || disabled}>
   <label>故障类别<Select aria-label="故障类别" value={input.category} onChange={e => setInput(v => ({ ...v, category: e.target.value }))}><option value="">请选择类别</option>{Object.entries(categoryNames).map(([k, v]) => <option value={k} key={k}>{v}</option>)}</Select></label>
   <label>具体位置<input aria-label="具体位置" value={input.location} onChange={e => setInput(v => ({ ...v, location: e.target.value }))} maxLength={400} placeholder="例如：三号楼201"/></label>
   <label>故障描述<textarea aria-label="故障描述" rows={3} value={input.description} onChange={e => setInput(v => ({ ...v, description: e.target.value }))} maxLength={4000} placeholder="请描述故障现象"/></label>
   <p className="field-help">位置 {[...input.location].length}/200 字 · 描述 {[...input.description].length}/2000 字{dirty ? ' · 有未保存的修改' : ''}</p>
   {!valid && <p className="field-help">请补齐类别、位置与故障描述，并确认字数在限制内。</p>}
   <OrderMediaInput draft files={files} status={mediaStatus} onChange={setFiles} />
   <div className="draft-actions"><button type="button" onClick={() => void run('cancel')}>取消草稿</button><button type="button" disabled={!dirty} onClick={() => void run('save')}>保存修改</button><button type="button" className="primary" disabled={!valid} onClick={() => void run('submit')}>{busy ? '正在处理…' : '确认提交报修'}</button></div>
  </fieldset></form>
  {error && <div className="error" role="alert">{error}<button type="button" disabled={busy || disabled} onClick={() => { if (!dirty || window.confirm('重新读取将放弃尚未保存的修改，是否继续？')) void run('reload'); }}>重新读取草稿</button></div>}
 </section>;
}
