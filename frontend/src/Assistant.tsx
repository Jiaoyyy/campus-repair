import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { api, time } from './api';
import type { User } from './api';
import { Drawer, Empty, Icon, Loading, OriginalText, PageHeading } from './ui';
import { streamAnswer } from './assistant-api';
import type { Agent, Citation, Conversation, Message } from './assistant-api';
const stateNames: Record<string, string> = { pending: '等待回答', generating: '生成中 · 尚未核验', failed: '未完成', cancelled: '已停止 · 未完成' };
export function Assistant({ onManual, role }: { onManual: () => void; role: User['role'] }) {
 const [agents, setAgents] = useState<Agent[]>([]), [agent, setAgent] = useState<number>(0);
 const [conversations, setConversations] = useState<Conversation[]>([]), [conversation, setConversation] = useState<Conversation | null>(null);
 const [items, setItems] = useState<Message[]>([]), [page, setPage] = useState(1), [total, setTotal] = useState(0);
 const [input, setInput] = useState(''), [busy, setBusy] = useState(false), [loading, setLoading] = useState(true), [error, setError] = useState(''), [configured, setConfigured] = useState(false), [chatConfigured, setChatConfigured] = useState(false);
 const [source, setSource] = useState<{ evidence: Citation; text: string } | null>(null), [sourceError, setSourceError] = useState('');
 const [sourceOpen, setSourceOpen] = useState(false), [opening, setOpening] = useState(false), [historyOpen, setHistoryOpen] = useState(false);
 const controller = useRef<AbortController | null>(null), messagesPanel = useRef<HTMLDivElement>(null), question = useRef<HTMLTextAreaElement>(null), serial = useRef(0), sourceSerial = useRef(0), temporaryID = useRef(-1);
 useEffect(() => { refresh(); return () => { controller.current?.abort(); serial.current++; }; }, []);
 useEffect(() => { if (busy && messagesPanel.current) messagesPanel.current.scrollTop = messagesPanel.current.scrollHeight; }, [items, busy]);
 function closeSource() { sourceSerial.current++; setSourceOpen(false); setSource(null); setSourceError(''); }
 async function refresh() { setLoading(true); setError(''); try { const [a, c, caps] = await Promise.all([api<{ items: Agent[] }>('/api/agents'), api<{ items: Conversation[] }>('/api/conversations?page_size=100'), api<{ assistant: boolean; chat: string }>('/api/capabilities')]); setAgents(a.items); setAgent(v => a.items.some(a => a.id === v) ? v : a.items[0]?.id || 0); setConversations(c.items); setConfigured(caps.assistant); setChatConfigured(caps.chat === 'configured'); } catch (e) { setError((e as Error).message); } finally { setLoading(false); } }
 async function select(c: Conversation, p = 1) { const request = ++serial.current; setLoading(true); setError(''); closeSource(); setHistoryOpen(false); try { const v = await api<{ items: Message[]; total: number; conversation: Conversation }>(`/api/conversations/${c.id}/messages?page=${p}&page_size=20`); if (request !== serial.current) return; setConversation(v.conversation); setAgent(c.agent_id); setItems(v.items); setTotal(v.total); setPage(p); } catch (e) { if (request === serial.current) setError((e as Error).message); } finally { if (request === serial.current) setLoading(false); } }
 function newChat() { serial.current++; setConversation(null); setItems([]); setTotal(0); setPage(1); closeSource(); setError(''); setInput(''); question.current?.focus(); }
 async function submit(e: FormEvent) {
  e.preventDefault(); if (busy || !input.trim() || !agent) return; const text = input.trim(); setBusy(true); setError(''); closeSource(); let c = conversation;
  const abort = new AbortController(); controller.current = abort;
  const userID = temporaryID.current--, pendingID = temporaryID.current--; let assistantID = pendingID; const patch = (fn: (m: Message) => Message) => setItems(v => v.map(m => m.id === assistantID ? fn(m) : m));
  try {
   if (!c) { c = (await api<{ conversation: Conversation }>('/api/conversations', { method: 'POST', body: JSON.stringify({ agent_id: agent }) })).conversation; setConversation(c); }
   setItems(v => [...(page === 1 ? v : []), { id: userID, role: 'user', content: text, status: 'pending', warning: '', citations: [] }, { id: pendingID, role: 'assistant', content: '', status: 'generating', warning: '', citations: [] }]); setInput(''); setPage(1);
   await streamAnswer(c.id, text, abort.signal, ({ event, data }) => {
    if (event === 'meta' && data.message_id) { const id = data.message_id; setItems(v => v.map(m => m.id === pendingID ? { ...m, id } : m)); assistantID = id; }
    if (event === 'delta') patch(m => ({ ...m, content: m.content + (data.text || '') }));
    if (event === 'done') setItems(v => v.map(m => m.id === userID ? { ...m, status: 'complete' } : m));
    if (event === 'done') patch(m => ({ ...m, content: data.answer || '', status: 'complete', citations: data.citations || [], warning: data.warning || '' }));
    if (event === 'error') patch(m => ({ ...m, status: 'failed', warning: data.message || '回答未完成', citations: [] }));
   });
   await select(c); await refresh();
  } catch (e) {
   const stopped = abort.signal.aborted; setItems(v => v.map(m => m.id === userID ? { ...m, status: stopped ? 'cancelled' : 'failed' } : m)); patch(m => ({ ...m, status: stopped ? 'cancelled' : 'failed', warning: stopped ? '生成已停止，内容未完成、未核验' : (e as Error).message, citations: [] }));
   if (!stopped) setError((e as Error).message);
  } finally { setBusy(false); controller.current = null; }
 }
 async function openSource(m: Message, cite: Citation) {
  if (!conversation) return;
  const request = ++sourceSerial.current; setSourceOpen(true); setOpening(true); setSource(null); setSourceError('');
  try { const result = await api<{ evidence: Citation; text: string }>(`/api/conversations/${conversation.id}/citations/${m.id}/${cite.chunk_id}`); if (request === sourceSerial.current) setSource(result); }
  catch { if (request === sourceSerial.current) setSourceError('该来源已失效或无权查看，请重新提问获取当前资料。'); }
  finally { if (request === sourceSerial.current) setOpening(false); }
 }
 const examples = ['宿舍灯坏了，不知道怎么报修', role === 'admin' ? '照明类可派人员有哪些？' : role === 'technician' ? '我的已分配任务' : '我的工单进度'];
 return <><PageHeading title="报修助手" description="描述问题、梳理报修信息，或查询工单进度。"><button onClick={onManual}><Icon kind="left"/>返回工单</button></PageHeading>
 {error && <div className="error" role="alert">{error}</div>}
 {!loading && !configured && <div className="assistant-notice" role="status">对话服务尚未配置。可查看已有对话，或返回工单继续办理。</div>}
 {!loading && configured && !chatConfigured && <div className="assistant-notice" role="status">目前可查询已绑定的实时工单信息，资料问答暂不可用。</div>}
 <div className="assistant-layout">
  <aside className="panel conversation-list"><div className="section-heading"><h2>我的对话</h2><button className="icon-button" aria-label="刷新对话列表" disabled={busy || loading} onClick={refresh}><Icon kind="refresh"/></button></div>
   <button className="primary full" disabled={busy || loading} onClick={newChat}><Icon kind="plus"/>新建对话</button>
   <button className="history-toggle text-button" aria-expanded={historyOpen} onClick={() => setHistoryOpen(v => !v)}>{historyOpen ? '收起' : '查看'}历史对话 · {conversations.length}</button>
   <div className={`conversation-items ${historyOpen ? 'open' : ''}`}>{conversations.length ? conversations.map(c => <button key={c.id} disabled={busy || loading} aria-pressed={c.id === conversation?.id} className={`conversation-item ${c.id === conversation?.id ? 'selected' : ''}`} onClick={() => select(c)}><strong>{c.title}</strong><small>{time(c.updated_at)}</small></button>) : <p className="sidebar-empty">提问后，对话将保存在这里。</p>}</div>
  </aside>
  <section className="panel chat-panel" aria-label="对话区"><div className="chat-toolbar"><div className="assistant-avatar"><Icon kind="chat"/></div><label className="assistant-select"><span>当前助手</span><select aria-label="当前助手" value={conversation?.agent_id || agent} disabled={busy || !!conversation} onChange={e => setAgent(Number(e.target.value))}>{!agents.some(a => a.id === conversation?.agent_id) && conversation && <option value={conversation.agent_id}>{conversation.agent_name}（当前不可用）</option>}{agents.map(a => <option value={a.id} key={a.id}>{a.name}</option>)}</select></label>
   <div className="chat-toolbar-actions">{conversation && <button className="icon-button" aria-label="重新读取历史" disabled={busy || loading} onClick={() => select(conversation, page)}><Icon kind="refresh"/></button>}
   </div>
  </div>
  <div className="chat-messages" ref={messagesPanel}>{loading ? <Loading text="正在读取对话…"/> : !items.length ? <div className="chat-empty"><span className="chat-welcome-icon"><Icon kind="chat"/></span><h2>有什么需要帮忙？</h2><p>直接说说遇到了什么问题，我会帮你一起梳理。</p><div className="suggested-questions">{examples.map(text => <button key={text} disabled={busy || !agent} onClick={() => { setInput(text); question.current?.focus(); }}><Icon kind={text.includes('信息') ? 'book' : 'orders'}/>{text}<Icon kind="arrow"/></button>)}</div><small>实际报修和处理操作仍由你确认。</small></div> : items.map(m => <article key={m.id} className={`chat-message ${m.role}`}><div className="message-label"><span className="message-author">{m.role === 'user' ? '你' : <><Icon kind="chat"/>报修助手</>}</span>{stateNames[m.status] && <span className="message-state">{stateNames[m.status]}</span>}</div><div className="message-content">{m.content || (m.status === 'generating' ? '正在查询，请稍候…' : '本轮未产生完整回答。')}</div>{m.warning && <p className="citation-warning" role="status">{m.warning}</p>}{m.status === 'complete' && m.citations?.length > 0 && <div className="citation-list" aria-label="回答来源">{m.citations.map(c => <button key={c.number} onClick={() => openSource(m, c)}><Icon kind="file"/><span>[{c.number}] {c.title}</span><Icon kind="arrow"/></button>)}</div>}</article>)}</div>
  {conversation && total > 20 && <div className="pagination"><button disabled={busy || loading || page * 20 >= total} onClick={() => select(conversation, page + 1)}>更早消息</button><span>第 {page} 页</span><button disabled={busy || loading || page === 1} onClick={() => select(conversation, page - 1)}>较新消息</button></div>}
  <form className="chat-composer" onSubmit={submit}><label htmlFor="chat-question" className="sr-only">你的问题</label><textarea ref={question} id="chat-question" rows={2} required maxLength={4000} value={input} onChange={e => setInput(e.target.value)} disabled={busy} placeholder="输入问题，描述得越具体越有帮助…"/><div><small>{[...input].length}/2000 字 <span>· 生成中的内容尚未核验</span></small>{busy ? <button type="button" onClick={() => controller.current?.abort()}><Icon kind="stop"/>停止生成</button> : <button className="primary" disabled={!agent || !configured || loading || !input.trim() || [...input].length > 2000}><Icon kind="send"/>发送</button>}</div></form>
  </section>
 </div>
 {sourceOpen && <Drawer title="来源核对" onClose={closeSource}>{opening ? <Loading text="正在读取原文…"/> : sourceError ? <div className="error" role="alert">{sourceError}</div> : source ? <><div className="source-meta"><span className="badge">引用来源</span>{source.evidence.demo && <span className="badge">演示材料</span>}</div><h3 className="source-title">{source.evidence.title}</h3><p className="muted">片段 {source.evidence.sequence} · 第 {source.evidence.start + 1}–{source.evidence.end} 个字符</p><OriginalText text={source.text} start={source.evidence.start} end={source.evidence.end}/></> : <Empty title="暂无来源"/>}</Drawer>}
 </>;
}
