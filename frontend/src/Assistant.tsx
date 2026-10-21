import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { api, time } from './api';
import { streamAnswer } from './assistant-api';
import type { Agent, Citation, Conversation, Message } from './assistant-api';
const stateNames: Record<string, string> = { pending: '等待回答', generating: '生成中 · 尚未核验', failed: '未完成', cancelled: '已停止 · 未完成' };
export function Assistant({ onManual }: { onManual: () => void }) {
 const [agents, setAgents] = useState<Agent[]>([]), [agent, setAgent] = useState<number>(0);
 const [conversations, setConversations] = useState<Conversation[]>([]), [conversation, setConversation] = useState<Conversation | null>(null);
 const [items, setItems] = useState<Message[]>([]), [page, setPage] = useState(1), [total, setTotal] = useState(0);
 const [input, setInput] = useState(''), [mode, setMode] = useState('keyword'), [busy, setBusy] = useState(false), [loading, setLoading] = useState(true), [error, setError] = useState(''), [configured, setConfigured] = useState(false), [chatConfigured, setChatConfigured] = useState(false);
 const [source, setSource] = useState<{ evidence: Citation; text: string } | null>(null), [sourceError, setSourceError] = useState('');
 const controller = useRef<AbortController | null>(null), sourcePanel = useRef<HTMLElement>(null), bottom = useRef<HTMLDivElement>(null), serial = useRef(0), temporaryID = useRef(-1);
 useEffect(() => { refresh(); return () => { controller.current?.abort(); serial.current++; }; }, []);
 useEffect(() => { if (source || sourceError) sourcePanel.current?.scrollIntoView({ block: 'start', behavior: 'smooth' }); }, [source, sourceError]);
 async function refresh() { setLoading(true); setError(''); try { const [a, c, caps] = await Promise.all([api<{ items: Agent[] }>('/api/agents'), api<{ items: Conversation[] }>('/api/conversations?page_size=100'), api<{ assistant: boolean; chat: string }>('/api/capabilities')]); setAgents(a.items); setAgent(v => a.items.some(a => a.id === v) ? v : a.items[0]?.id || 0); setConversations(c.items); setConfigured(caps.assistant); setChatConfigured(caps.chat === 'configured'); } catch (e) { setError((e as Error).message); } finally { setLoading(false); } }
 async function select(c: Conversation, p = 1) { const request = ++serial.current; setLoading(true); setError(''); setSource(null); setSourceError(''); try { const v = await api<{ items: Message[]; total: number; conversation: Conversation }>(`/api/conversations/${c.id}/messages?page=${p}&page_size=20`); if (request !== serial.current) return; setConversation(v.conversation); setAgent(c.agent_id); setItems(v.items); setTotal(v.total); setPage(p); } catch (e) { if (request === serial.current) setError((e as Error).message); } finally { if (request === serial.current) setLoading(false); } }
 function newChat() { serial.current++; setConversation(null); setItems([]); setTotal(0); setPage(1); setSource(null); setSourceError(''); setError(''); setInput(''); }
 async function submit(e: FormEvent) {
  e.preventDefault(); if (busy || !input.trim() || !agent) return; const text = input.trim(); setBusy(true); setError(''); setSource(null); setSourceError(''); let c = conversation;
  const abort = new AbortController(); controller.current = abort;
  const userID = temporaryID.current--, pendingID = temporaryID.current--; let assistantID = pendingID; const patch = (fn: (m: Message) => Message) => setItems(v => v.map(m => m.id === assistantID ? fn(m) : m));
  try {
   if (!c) { c = (await api<{ conversation: Conversation }>('/api/conversations', { method: 'POST', body: JSON.stringify({ agent_id: agent }) })).conversation; setConversation(c); }
   setItems(v => [...(page === 1 ? v : []), { id: userID, role: 'user', content: text, status: 'pending', warning: '', citations: [] }, { id: pendingID, role: 'assistant', content: '', status: 'generating', warning: '', citations: [] }]); setInput(''); setPage(1);
   await streamAnswer(c.id, text, mode, abort.signal, ({ event, data }) => {
    if (event === 'meta' && data.message_id) { const id = data.message_id; setItems(v => v.map(m => m.id === pendingID ? { ...m, id } : m)); assistantID = id; }
    if (event === 'delta') patch(m => ({ ...m, content: m.content + (data.text || '') }));
    if (event === 'done') setItems(v => v.map(m => m.id === userID ? { ...m, status: 'complete' } : m));
    if (event === 'done') patch(m => ({ ...m, content: data.answer || '', status: 'complete', citations: data.citations || [], warning: data.warning || '' }));
    if (event === 'error') patch(m => ({ ...m, status: 'failed', warning: data.message || '回答未完成', citations: [] }));
    bottom.current?.scrollIntoView({ block: 'end' });
   });
   await select(c); await refresh();
  } catch (e) {
   const stopped = abort.signal.aborted; setItems(v => v.map(m => m.id === userID ? { ...m, status: stopped ? 'cancelled' : 'failed' } : m)); patch(m => ({ ...m, status: stopped ? 'cancelled' : 'failed', warning: stopped ? '生成已停止，内容未完成、未核验' : (e as Error).message, citations: [] }));
   if (!stopped) setError((e as Error).message);
  } finally { setBusy(false); controller.current = null; }
 }
 async function openSource(m: Message, cite: Citation) { if (!conversation) return; setSource(null); setSourceError(''); try { setSource(await api(`/api/conversations/${conversation.id}/citations/${m.id}/${cite.chunk_id}`)); } catch { setSourceError('该来源已失效或无权查看，请重新提问获取当前资料。'); } }
 return <><div className="page-heading"><div><span className="eyebrow">资料与报修辅助</span><h1>报修助手</h1><p className="muted">回答可核对出处，报修和处理操作仍由你确认。</p></div><button onClick={onManual}>返回工单</button></div>
 {error && <div className="error" role="alert">{error}</div>}{!loading && !configured && <div className="assistant-notice">对话服务尚未配置。你仍可查看已有对话，或返回工单继续办理。</div>}{!loading && configured && !chatConfigured && <div className="assistant-notice">当前仅支持已绑定的 MCP 实时工单查询。可问“我的工单进度”“我的已分配任务”或“照明类可派人员”；资料问答模型尚未配置。</div>}
 <div className="assistant-layout"><aside className="panel conversation-list"><div className="section-heading"><h2>我的对话</h2><button disabled={busy || loading} onClick={newChat}>新对话</button></div><button className="text-button" disabled={busy || loading} onClick={refresh}>刷新列表</button>{conversations.length ? conversations.map(c => <button key={c.id} disabled={busy} className={`conversation-item ${c.id === conversation?.id ? 'selected' : ''}`} onClick={() => select(c)}><strong>{c.title}</strong><small>{c.agent_name} · {time(c.updated_at)}</small></button>) : <p className="muted">开始提问后，对话会保存在这里。</p>}</aside>
 <section className="panel chat-panel"><div className="chat-toolbar"><label>当前助手<select aria-label="当前助手" value={conversation?.agent_id || agent} disabled={busy || !!conversation} onChange={e => setAgent(Number(e.target.value))}>{!agents.some(a => a.id === conversation?.agent_id) && conversation && <option value={conversation.agent_id}>{conversation.agent_name}（当前不可用）</option>}{agents.map(a => <option value={a.id} key={a.id}>{a.name}</option>)}</select></label><label>资料检索<select value={mode} onChange={e => setMode(e.target.value)} disabled={busy}><option value="keyword">关键词</option><option value="vector">向量</option><option value="hybrid">混合</option></select></label>{conversation && <button disabled={busy || loading} onClick={() => select(conversation, page)}>重读历史</button>}</div>
 {loading ? <p role="status">正在读取…</p> : !items.length ? <div className="chat-empty"><h2>先描述你的问题</h2><p>例如：照明报修需要填写哪些信息？</p><p className="muted">资料不足时会明确说明，不代替现场专业判断。</p></div> : <div className="chat-messages">{items.map(m => <article key={m.id} className={`chat-message ${m.role}`}><div className="message-label">{m.role === 'user' ? '你' : '助手'}{stateNames[m.status] && <span>{stateNames[m.status]}</span>}</div><div className="message-content">{m.content || (m.status === 'generating' ? '正在检索资料…' : '本轮未产生完整回答。')}</div>{m.warning && <p className="citation-warning" role="status">{m.warning}</p>}{m.status === 'complete' && m.citations?.length > 0 && <div className="citation-list" aria-label="回答来源">{m.citations.map(c => <button key={c.number} onClick={() => openSource(m, c)}><strong>[{c.number}] {c.title}</strong><small>{c.demo ? '演示材料 · ' : ''}片段 {c.sequence} · 查看原文</small></button>)}</div>}</article>)}<div ref={bottom} /></div>}
 {conversation && total > 20 && <div className="pagination"><button disabled={busy || loading || page * 20 >= total} onClick={() => select(conversation, page + 1)}>更早消息</button><span>第 {page} 页</span><button disabled={busy || loading || page === 1} onClick={() => select(conversation, page - 1)}>较新消息</button></div>}
 <form className="chat-composer" onSubmit={submit}><label htmlFor="chat-question">你的问题</label><textarea id="chat-question" rows={3} required maxLength={4000} value={input} onChange={e => setInput(e.target.value)} disabled={busy} placeholder="描述故障现象，或询问报修资料" /><div><small className="muted">{[...input].length}/2000 字 · 回答完成前的内容尚未核验</small>{busy ? <button type="button" onClick={() => controller.current?.abort()}>停止生成</button> : <button className="primary" disabled={!agent || !configured || loading || !input.trim() || [...input].length > 2000}>发送问题</button>}</div></form>
 </section></div>
 {(source || sourceError) && <section ref={sourcePanel} className="panel knowledge-original"><div className="section-heading"><h2>来源核对</h2><button onClick={() => { setSource(null); setSourceError(''); }}>关闭来源</button></div>{sourceError && <div className="error" role="alert">{sourceError}</div>}{source && <><h3>{source.evidence.title}</h3><p className="muted">{source.evidence.demo ? '演示材料 · ' : ''}片段 {source.evidence.sequence} · 第 {source.evidence.start + 1}–{source.evidence.end} 个字符</p><pre>{[...source.text].slice(0, source.evidence.start).join('')}<mark>{[...source.text].slice(source.evidence.start, source.evidence.end).join('')}</mark>{[...source.text].slice(source.evidence.end).join('')}</pre></>}</section>}
 </>;
}
