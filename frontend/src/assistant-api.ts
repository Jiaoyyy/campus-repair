import { APIError, tokenKey } from './api';
export type Citation = { number: number; chunk_id: number; material_id: number; generation: number; title: string; demo: boolean; sequence: number; start: number; end: number; text: string };
export type Agent = { id: number; name: string; role: string; prompt?: string; enabled: boolean; version: number; material_ids: number[] };
export type Conversation = { id: number; agent_id: number; title: string; agent_name: string; updated_at: string };
export type Message = { id: number; role: string; content: string; status: string; warning: string; citations: Citation[]; created_at?: string };
export type StreamEvent = { event: string; data: { text?: string; answer?: string; message_id?: number; citations?: Citation[]; warning?: string; message?: string } };
export async function streamAnswer(id: number, content: string, mode: string, signal: AbortSignal, onEvent: (e: StreamEvent) => void) {
 const res = await fetch(`/api/conversations/${id}/messages`, { method: 'POST', signal, headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${sessionStorage.getItem(tokenKey) || ''}` }, body: JSON.stringify({ content, mode }) });
 if (!res.ok) { const v = await res.json().catch(() => null); if (res.status === 401) { sessionStorage.removeItem(tokenKey); window.dispatchEvent(new Event('session-expired')); } throw new APIError(v?.error?.message || '对话服务暂不可用', res.status); }
 if (!res.body || !res.headers.get('Content-Type')?.startsWith('text/event-stream')) throw new Error('未收到有效回答流，请重新读取历史。');
 const reader = res.body.getReader(), decoder = new TextDecoder(); let pending = '', terminal = false;
 try {
  while (!terminal) {
   const { value, done } = await reader.read(); pending += decoder.decode(value, { stream: !done }).replace(/\r\n/g, '\n');
   let split: number;
   while ((split = pending.indexOf('\n\n')) >= 0) {
    const block = pending.slice(0, split); pending = pending.slice(split + 2); let event = '', data = '';
    for (const line of block.split('\n')) { if (line.startsWith('event:')) event = line.slice(6).trim(); if (line.startsWith('data:')) data += line.slice(5).trim(); }
    if (!event || !data) continue;
    if (terminal) throw new Error('收到重复终止事件');
    const v = JSON.parse(data) as StreamEvent['data']; onEvent({ event, data: v });
    if (event === 'done' || event === 'error') terminal = true;
    if (event === 'error') throw new Error(v.message || '回答未完成，请重试。');
   }
   if (done) { if (!terminal) throw new Error('连接已中断，回答未完成。请重新读取历史后重试。'); break; }
  }
 } finally { await reader.cancel().catch(() => {}); reader.releaseLock(); }
}
