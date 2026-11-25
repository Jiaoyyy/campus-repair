import { APIError, tokenKey } from './api.ts';
export type Citation = { number: number; chunk_id: number; material_id: number; generation: number; title: string; demo: boolean; sequence: number; start: number; end: number; text: string };
export type SkillBinding = {skill_id: string; enabled: boolean};
export type SkillConfig = {id: string; name: string; definition_version: number; roles: string[]; purpose: string; trigger: string; required_fields: string[]; allowed_tools: string[]; instruction: string; output_contract: string; enabled: boolean; version: number};
export type Agent = { id: number; name: string; role: string; prompt?: string; enabled: boolean; version: number; material_ids: number[]; tool_names?: string[]; skill_bindings?: SkillBinding[] };
export type Conversation = { id: number; agent_id: number; title: string; agent_name: string; updated_at: string };
export type RepairDraft = { message_id: number; conversation_id: number; category: string; location: string; description: string; version: number; status: string; idempotency_key: string; order_id?: number; order_number?: string };
export type BusinessAnalysis = {title:string; scope:string; facts:{id:string;title:string;text:string;count:number;time_basis?:string}[]; categories:{label:string;count:number}[]; locations:{label:string;count:number}[]; symptoms:{label:string;count:number}[]; notes:{evidence:string[];recommendation:string}[]; model_status:string; warning?:string; related?:Record<string,{total:number; items:BusinessResult["items"]; next_cursor?:string}>};
export type SkillRun = {skill_id: string; skill_version: number; config_version: number};
export type OrderStep = {reason: string; responsible_role: string; actions: string[]; evidence_id: string};
export type VisitRecord = {
  start: string;
  end: string;
  stage: string;
  departed_at: string | null;
  arrived_at: string | null;
  no_show_at: string | null;
  end_reason: string;
};
export type OrderDetailResult = {
  order: { id: number; number: string; status: string; category: string; location: string; description: string };
  version: number;
  updated_at: string;
  next_step: OrderStep;
  latest_repair?: { id: number; measures: string; result: string; created_at: string };
  events: {
    id: number;
    action: string;
    from_status: string;
    to_status: string;
    note: string;
    actor_name: string;
    created_at: string;
  }[];
  has_more: boolean;
  next_cursor?: string;
  latest_visit?: VisitRecord;
  visit?: {
    availability_start: string | null;
    availability_end: string | null;
    exception: string;
    reason: string;
    expected_resume_at: string | null;
    needs_availability: boolean;
    visit: VisitRecord | null;
  };
};
export type CompletionText = { measures: string; result: string };
export type CompletionHandoff = CompletionText & { id: number; version: number };
export type PartsHandoff = { id:number; version:number; reason:string; resume_at:string };
export type TechnicianHandoff = CompletionHandoff | PartsHandoff;
export type RequesterHandoff = { id:number; version:number; number:string; category:string; location:string; action:'confirm'|'reopen'|'recurrence'; reason:string };
export type BusinessResult = { kind: 'prepare_requester_followup' | 'prepare_completion' | 'prepare_wait_parts' | 'orders' | 'discuss_pre_review' | 'pre_review' | 'worklist' | 'candidates' | 'analysis' | 'detail' | 'visit'; detail?:OrderDetailResult; completion?:CompletionText; followup?:{decision:'confirm'|'recurrence';reason?:string}; parts?:{ reason:string; resume_at?:string }; natural?:boolean; additional?:BusinessResult[]; unavailable?:boolean; warning?:string; analysis?: BusinessAnalysis; total: number; statuses: Record<string,number>; items: {review_address_missing?:boolean;current_status?:string;current_version?:number;recurrence_exists?:boolean;latest_repair?:{id:number;measures:string;result:string;created_at:string};visit_start?:string;visit_end?:string;review?:{decision:"approve"|"supplement"|"manual";reason:string;question?:string};id:number; number:string; location:string; description?:string; advice?:string; priority?:string; version?:number; recommended?:{id:number; display_name:string; pending_count:number; in_progress_count:number}; category:string; status:string;next_step?:OrderStep; responsible?:string}[]; candidates?: {id:number; display_name:string; categories:string[]; pending_count:number; in_progress_count:number}[]; queried_at:string; time_basis?:string; historical:boolean; query:{date_basis?:string;analysis?:string; queue?:string; status?:string; category?:string; number?:string; period?:string; location?:string; keyword?:string; date_from?:string; date_to?:string} };
export type Message = { id: number; role: string; content: string; status: string; warning: string; citations: Citation[]; created_at?: string; draft?: RepairDraft; business?: BusinessResult; phase?: string; skill?:SkillRun };
export type StreamEvent = { event: string; data: { skill?: SkillRun | string; skill_version?:number; text?: string; answer?: string; message_id?: number; citations?: Citation[]; warning?: string; message?: string; code?: string; partial?: string; draft?: RepairDraft; business?: BusinessResult } };
export class StreamFailure extends Error {
 constructor(message: string, public code = 'stream_invalid') { super(message); }
}
export type ReviewReference = {message_id:number; order_id:number};
export async function streamAnswer(id: number, content: string, signal: AbortSignal, onEvent: (e: StreamEvent) => void, review?:ReviewReference, refreshed=false) {
 const res = await fetch(`/api/conversations/${id}/messages`, { method: 'POST', signal, headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${sessionStorage.getItem(tokenKey) || ''}` }, body: JSON.stringify({ content, review_ref:review, review_refreshed:refreshed }) });
 if (!res.ok) { const v = await res.json().catch(() => null); if (res.status === 401) { sessionStorage.removeItem(tokenKey); window.dispatchEvent(new Event('session-expired')); } throw new APIError(v?.error?.message || '对话服务暂不可用', res.status); }
 if (!res.body || !res.headers.get('Content-Type')?.startsWith('text/event-stream')) throw new StreamFailure('未收到有效回答流，请重新读取历史。');
 const reader = res.body.getReader(), decoder = new TextDecoder('utf-8', {fatal:true});
 let pending = '', terminal = false, meta = false, messageID = 0;
 const invalid = () => new StreamFailure('回答流格式异常，本轮未完成。请重新读取历史后重试。');
 try {
  while (!terminal) {
   const { value, done } = await reader.read();
   try { pending = (pending + decoder.decode(value, { stream: !done })).replace(/\r\n/g, '\n'); } catch { throw invalid(); }
   if (pending.length > 1048576) throw invalid();
   const batch: StreamEvent[] = []; let split: number; let failure: StreamFailure | undefined;
   while ((split = pending.indexOf('\n\n')) >= 0) {
    const block = pending.slice(0, split); pending = pending.slice(split + 2);
    let event = ''; const data: string[] = [];
    for (const line of block.split('\n')) {
     if (line.startsWith('event:')) event = line.slice(6).trim();
     if (line.startsWith('data:')) data.push(line.slice(5).replace(/^ /, ''));
    }
    if (!event && !data.length) continue; // SSE comments/heartbeats.
    if (!event || !data.length || terminal) throw invalid();
    let v: StreamEvent['data'] & {conversation_id?:number};
    try { v = JSON.parse(data.join('\n')); } catch { throw invalid(); }
    if (!v || typeof v !== 'object' || Array.isArray(v)) throw invalid();
    if (event === 'meta') {
     if (meta || v.conversation_id !== id || !Number.isSafeInteger(v.message_id) || v.message_id! < 1) throw invalid();
     meta = true; messageID = v.message_id!;
    } else {
     if (!meta) throw invalid();
     if (event === 'delta' && typeof v.text !== 'string' || event === 'phase' && typeof v.message !== 'string') throw invalid();
     if (event === 'done') {
      if (v.message_id !== messageID || typeof v.answer !== 'string' || !v.answer.trim() || v.citations !== undefined && !Array.isArray(v.citations)) throw invalid();
      terminal = true;
     } else if (event === 'error') {
      if (v.message_id !== messageID || typeof v.message !== 'string' || v.code !== undefined && typeof v.code !== 'string') throw invalid();
      terminal = true; failure = new StreamFailure(v.message || '回答未完成，请重试。', v.code || 'generation_failed');
     }
    }
    if (['meta','phase','delta','done','error'].includes(event)) batch.push({event, data:v});
   }
   // Validate the complete batch before publishing a terminal event.
   for (const e of batch) onEvent(e);
   if (failure) throw failure;
   if (done && !terminal) throw new StreamFailure('连接已中断，回答未完成。请重新读取历史后重试。', 'stream_interrupted');
  }
 } finally { await reader.cancel().catch(() => {}); reader.releaseLock(); }
}
