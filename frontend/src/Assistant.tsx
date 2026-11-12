import { Select } from './Select';
import { useCallback, useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { api, time } from './api';
import type { User } from './api';
import { Drawer, Empty, Icon, Loading, OriginalText, PageHeading } from './ui';
import { BusinessResultCard } from './BusinessResultCard';
import { RepairDraftCard } from './RepairDraftCard';
import { streamAnswer } from './assistant-api';
import type { Agent, Citation, Conversation, Message } from './assistant-api';
const stateNames: Record<string, string> = { pending: '等待回答', generating: '生成中 · 尚未核验', failed: '未完成', cancelled: '已停止 · 未完成' };
export function Assistant({ onManual, role, onOpen, onDirtyChange, active, storageKey, discardSignal }: { storageKey:string; discardSignal:number; active: boolean; onManual: () => void; role: User['role']; onOpen: (id: number) => void; onDirtyChange: (v: boolean) => void }) {
 const [draftDirty, setDraftDirty] = useState(false), [draftBusy, setDraftBusy] = useState(false);
 const [pendingAction, setPendingAction] = useState<(() => void) | null>(null);
 const dirtyChanged = useCallback((v: boolean) => setDraftDirty(v), []);
 const busyChanged = useCallback((v: boolean) => setDraftBusy(v), []);
 function guard(action: () => void) { if (draftBusy) return; if (draftDirty) setPendingAction(() => action); else action(); }
 const [agents, setAgents] = useState<Agent[]>([]), [agent, setAgent] = useState<number>(0);
 const [conversations, setConversations] = useState<Conversation[]>([]), [conversation, setConversation] = useState<Conversation | null>(null);
 const [items, setItems] = useState<Message[]>([]), [page, setPage] = useState(1), [total, setTotal] = useState(0);
 const [input, setInput] = useState(''), [busy, setBusy] = useState(false), [loading, setLoading] = useState(true), [error, setError] = useState(''), [configured, setConfigured] = useState(false), [chatConfigured, setChatConfigured] = useState(false);
 const [source, setSource] = useState<{ evidence: Citation; text: string } | null>(null), [sourceError, setSourceError] = useState('');
 const [sourceOpen, setSourceOpen] = useState(false), [opening, setOpening] = useState(false), [historyOpen, setHistoryOpen] = useState(false);
 const controller = useRef<AbortController | null>(null), messagesPanel = useRef<HTMLDivElement>(null), question = useRef<HTMLTextAreaElement>(null), serial = useRef(0), sourceSerial = useRef(0), temporaryID = useRef(-1);
 useEffect(() => { onDirtyChange(active && (draftDirty || draftBusy || busy)); return () => onDirtyChange(false); }, [active, draftDirty, draftBusy, busy, onDirtyChange]);
 useEffect(() => { if (!draftDirty) return; const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = ''; }; window.addEventListener('beforeunload', warn); return () => window.removeEventListener('beforeunload', warn); }, [draftDirty]);
 useEffect(() => { void refresh().then(list => { const saved=sessionStorage.getItem(storageKey);const found=list?.find(c=>String(c.id)===saved);if(found)void select(found); }); return () => { controller.current?.abort(); serial.current++; }; }, []);
 useEffect(() => {
  const panel=messagesPanel.current; if (!panel) return;
  const latest=items[items.length-1];
  if (!busy && (latest?.business || latest?.draft)) {
   const article=panel.querySelector<HTMLElement>(`[data-message-id="${latest.id}"]`);
   if(article) panel.scrollTop+=article.getBoundingClientRect().top-panel.getBoundingClientRect().top-12;
  } else if (busy || latest?.status==='failed' || latest?.status==='cancelled' || items.some(m=>m.draft?.status==='active')) panel.scrollTop=panel.scrollHeight;
 }, [items,busy]);
 useEffect(()=>{if(!discardSignal)return;controller.current?.abort();setDraftDirty(false);if(conversation)void select(conversation,page);},[discardSignal]);
 function closeSource() { sourceSerial.current++; setSourceOpen(false); setSource(null); setSourceError(''); }
 async function refresh(quiet = false) { if (!quiet) setLoading(true); setError(''); try { const [a, c, caps] = await Promise.all([api<{ items: Agent[] }>('/api/agents'), api<{ items: Conversation[] }>('/api/conversations?page_size=100'), api<{ assistant: boolean; chat: string }>('/api/capabilities')]); setAgents(a.items); setAgent(v => a.items.some(a => a.id === v) ? v : a.items[0]?.id || 0); setConversations(c.items); setConfigured(caps.assistant); setChatConfigured(caps.chat === 'configured'); return c.items; } catch (e) { setError((e as Error).message); } finally { setLoading(false); } }
 async function select(c: Conversation, p = 1, quiet = false) { const request = ++serial.current; if (!quiet) setLoading(true); setError(''); closeSource(); setHistoryOpen(false); try { const v = await api<{ items: Message[]; total: number; conversation: Conversation }>(`/api/conversations/${c.id}/messages?page=${p}&page_size=20`); if (request !== serial.current) return; setConversation(v.conversation); sessionStorage.setItem(storageKey,String(v.conversation.id)); setAgent(c.agent_id); setItems(v.items); setTotal(v.total); setPage(p); return true; } catch (e) { if (request === serial.current) setError((e as Error).message); return false; } finally { if (request === serial.current) setLoading(false); } }
 function newChat() { sessionStorage.removeItem(storageKey); serial.current++; setConversation(null); setItems([]); setTotal(0); setPage(1); closeSource(); setError(''); setInput(''); question.current?.focus(); }
 function submit(e: FormEvent) { e.preventDefault(); guard(() => { void sendMessage(); }); }
 async function sendMessage(retry?: string) {
  if (controller.current || busy || !(retry || input).trim() || !agent) return; const text = (retry || input).trim(); setBusy(true); setError(''); closeSource(); let c = conversation;
  const abort = new AbortController(); controller.current = abort; let started = false, answered = false;
  const userID = temporaryID.current--, pendingID = temporaryID.current--; let assistantID = pendingID; const patch = (fn: (m: Message) => Message) => setItems(v => v.map(m => m.id === assistantID ? fn(m) : m));
  try {
   if (!c) { c = (await api<{ conversation: Conversation }>('/api/conversations', { method: 'POST', body: JSON.stringify({ agent_id: agent }), signal: abort.signal })).conversation; setConversation(c); sessionStorage.setItem(storageKey,String(c.id)); }
   setItems(v => [...(page === 1 ? v : []), { id: userID, role: 'user', content: text, status: 'pending', warning: '', citations: [] }, { id: pendingID, role: 'assistant', content: '', status: 'generating', warning: '', citations: [] }]); started = true; if (!retry) setInput(''); setPage(1);
   await streamAnswer(c.id, text, abort.signal, ({ event, data }) => {
    if (abort.signal.aborted || controller.current !== abort) return;
    if (event === 'done') answered = true;
    if (event === 'meta' && data.message_id) { const id = data.message_id; setItems(v => v.map(m => m.id === pendingID ? { ...m, id } : m)); assistantID = id; }
    if (event === 'phase') patch(m => ({ ...m, phase: data.message || '正在处理…' }));
    if (event === 'delta') patch(m => ({ ...m, content: m.content + (data.text || '') }));
    if (event === 'done') setItems(v => v.map(m => m.id === userID ? { ...m, status: 'complete' } : m));
    if (event === 'done') patch(m => ({ ...m, content: data.answer || '', status: 'complete', citations: data.citations || [], warning: data.warning || '', draft: data.draft, business: data.business }));
    if (event === 'error') patch(m => ({ ...m, status: 'failed', warning: data.message || '回答未完成', citations: [] }));
   });
   if (await select(c, 1, true)) await refresh(true);
  } catch (e) {
   if (answered) { setError('回答已完成，但记录刷新失败。请重新读取历史核对。'); return; }
   setInput(current => current || text);
   const stopped = abort.signal.aborted; setItems(v => v.map(m => m.id === userID ? { ...m, status: stopped ? 'cancelled' : 'failed' } : m)); patch(m => ({ ...m, status: stopped ? 'cancelled' : 'failed', warning: stopped ? '生成已停止，内容未完成、未核验' : (e as Error).message, citations: [] }));
   if (!stopped && !started) setError((e as Error).message);
  } finally { setBusy(false); if (controller.current === abort) controller.current = null; }
 }
 async function openSource(m: Message, cite: Citation) {
  if (!conversation) return;
  const request = ++sourceSerial.current; setSourceOpen(true); setOpening(true); setSource(null); setSourceError('');
  try { const result = await api<{ evidence: Citation; text: string }>(`/api/conversations/${conversation.id}/citations/${m.id}/${cite.chunk_id}`); if (request === sourceSerial.current) setSource(result); }
  catch { if (request === sourceSerial.current) setSourceError('该来源已失效或无权查看，请重新提问获取当前资料。'); }
  finally { if (request === sourceSerial.current) setOpening(false); }
 }
 const scenes = {
  requester: {welcome:'需要报修，还是想看看进度？',description:'描述故障、确认报修，或查看你自己的工单与待办。',placeholder:'例如：宿舍灯坏了，帮我报修；我的报修还有几单没结束？',examples:['宿舍灯坏了，帮我报修','我的报修还有多少单未结束？','有哪些报修待我确认？','报修需要填写哪些信息？']},
  technician: {welcome:'先看看待处理的维修任务？',description:'查询分配给你的工单，或核对维修资料与安全提醒。',placeholder:'例如：我现在有多少工单要处理？只看今天指派的',examples:['我现在有多少工单要处理？','今天派给我的有哪些活？','有哪些工单待接单？','照明维修有哪些安全要求？']},
  admin: {welcome:'今天有哪些问题值得关注？',description:'发现高发问题、积压与上门阻塞。',placeholder:'例如：总结当前有哪些问题，应该先关注什么？',examples:['总结当前有哪些问题','最近出现最多的问题是什么','现在有几单白跑了','研发楼有哪些未解决问题？']},
 }[role];
 const agentName = agents.find(a=>a.id===(conversation?.agent_id || agent))?.name || conversation?.agent_name || '报修助手';
 function fillQuestion(text:string) {setInput(text);question.current?.focus();}
 function questionBefore(index:number) { return items.slice(0,index).reverse().find(m=>m.role==='user')?.content; }
 function retryQuestion(index:number) {const text=questionBefore(index);if(text)guard(()=>{void sendMessage(text);});}

 return <><PageHeading title="报修助手" ><button onClick={onManual}><Icon kind="left"/>返回工单</button></PageHeading>
 {error && <div className="error" role="alert">{error}</div>}
 {!loading && !configured && <div className="assistant-notice" role="status">对话服务尚未配置。可查看已有对话，或返回工单继续办理。</div>}
 {!loading && configured && !chatConfigured && <div className="assistant-notice" role="status">目前可查询已绑定的实时工单信息，资料问答暂不可用。</div>}
 <div className="assistant-layout">
  <aside className="panel conversation-list"><div className="section-heading"><h2>我的对话</h2><button className="icon-button" aria-label="刷新对话列表" disabled={busy || draftBusy || loading} onClick={() => guard(() => { void refresh(); })}><Icon kind="refresh"/></button></div>
   <button className="primary full" disabled={busy || draftBusy || loading} onClick={() => guard(newChat)}><Icon kind="plus"/>新建对话</button>
   <button className="history-toggle text-button" aria-expanded={historyOpen} onClick={() => setHistoryOpen(v => !v)}>{historyOpen ? '收起' : '查看'}历史对话 · {conversations.length}</button>
   <div className={`conversation-items ${historyOpen ? 'open' : ''}`}>{conversations.length ? conversations.map(c => <button key={c.id} disabled={busy || draftBusy || loading} aria-pressed={c.id === conversation?.id} className={`conversation-item ${c.id === conversation?.id ? 'selected' : ''}`} onClick={() => guard(() => { void select(c); })}><strong>{c.title}</strong><small>{time(c.updated_at)}</small></button>) : <p className="sidebar-empty">提问后，对话将保存在这里。</p>}</div>
  </aside>
  <section className="panel chat-panel" aria-label="对话区"><div className="chat-toolbar"><div className="assistant-avatar"><Icon kind="chat"/></div><label className="assistant-select"><span>当前助手</span><Select aria-label="当前助手" value={conversation?.agent_id || agent} disabled={busy || draftBusy || !!conversation} onChange={e => setAgent(Number(e.target.value))}>{!agents.some(a => a.id === conversation?.agent_id) && conversation && <option value={conversation.agent_id}>{conversation.agent_name}（当前不可用）</option>}{agents.map(a => <option value={a.id} key={a.id}>{a.name}</option>)}</Select></label>
   <div className="chat-toolbar-actions">{conversation && <button className="icon-button" aria-label="重新读取历史" disabled={busy || draftBusy || loading} onClick={() => guard(() => { void select(conversation, page); })}><Icon kind="refresh"/></button>}
   </div>
  </div>
  <div className="chat-messages" ref={messagesPanel}>{loading ? <Loading text="正在读取对话…"/> : !items.length ? <div className="chat-empty"><span className="chat-welcome-icon"><Icon kind="chat"/></span><h2>{scenes.welcome}</h2><p>{scenes.description}</p><div className="suggested-questions">{scenes.examples.map(text => <button key={text} disabled={busy || draftBusy || !agent} onClick={() => { fillQuestion(text); }}><Icon kind={text.includes('信息') ? 'book' : 'orders'}/>{text}<Icon kind="arrow"/></button>)}</div><small>实际报修和处理操作仍由你确认。</small></div> : items.map((m, index) => <article key={m.id} data-message-id={m.id} className={`chat-message ${m.role}`}><div className="message-label"><span className="message-author">{m.role === 'user' ? '你' : <><Icon kind="chat"/>{agentName}</>}</span>{stateNames[m.status] && <span className="message-state">{stateNames[m.status]}</span>}</div>{!m.business && <div className="message-content">{m.content || (m.status === 'generating' ? (m.phase || '正在理解你的问题…') : '本轮未产生完整回答。')}</div>}{m.warning && <p className="citation-warning" role="status">{m.warning}</p>}{m.status === 'complete' && m.citations?.length > 0 && <div className="citation-list" aria-label="回答来源">{m.citations.map(c => <button key={c.number} onClick={() => openSource(m, c)}><Icon kind="file"/><span>[{c.number}] {c.title}</span><Icon kind="arrow"/></button>)}</div>}{m.status === 'complete' && m.business && <BusinessResultCard result={m.business} disabled={busy || draftBusy || loading} onOpen={id=>guard(()=>onOpen(id))} onQuestion={fillQuestion}/>} {m.business && m.business.kind!=='analysis' && m.content && <details className="assistant-explanation"><summary>查看回答说明</summary><div className="message-content">{m.content}</div></details>}{(m.status === 'failed' || m.status === 'cancelled') && m.role === 'assistant' && <div className="answer-recovery"><button className="text-button retry-answer" disabled={busy || draftBusy || loading} onClick={()=>retryQuestion(index)}>重新提问</button><button className="text-button" disabled={busy || draftBusy || loading} onClick={()=>{const text=questionBefore(index);if(text)fillQuestion(text);}}>编辑问题</button><button className="text-button" disabled={busy || draftBusy || loading} onClick={()=>guard(onManual)}>前往工单</button></div>}{role === 'requester' && m.status === 'complete' && m.draft && <RepairDraftCard draft={m.draft} disabled={busy || loading} onDirtyChange={m.draft.status === 'active' ? dirtyChanged : () => {}} onBusyChange={busyChanged} onOpen={id => guard(() => onOpen(id))} onChange={draft => setItems(items => items.map(item => item.id === m.id ? { ...item, draft } : item))}/>}</article>)}</div>
  {conversation && total > 20 && <div className="pagination"><button disabled={busy || loading || page * 20 >= total} onClick={() => guard(() => { void select(conversation, page + 1); })}>更早消息</button><span>第 {page} 页</span><button disabled={busy || loading || page === 1} onClick={() => guard(() => { void select(conversation, page - 1); })}>较新消息</button></div>}
  <form className="chat-composer" onSubmit={submit}><label htmlFor="chat-question" className="sr-only">你的问题</label><textarea ref={question} id="chat-question" rows={2} required maxLength={2000} value={input} onChange={e => setInput(e.target.value)} disabled={busy || draftBusy} placeholder={scenes.placeholder}/><div><small>{[...input].length}/2000 字 <span>· 生成中的内容尚未核验</span></small>{busy ? <button key="stop" type="button" onClick={e => { e.preventDefault(); controller.current?.abort(); }}><Icon kind="stop"/>停止生成</button> : <button key="send" type="submit" className="primary" disabled={draftBusy || !agent || !configured || loading || !input.trim() || [...input].length > 2000}><Icon kind="send"/>发送</button>}</div></form>
  </section>
 </div>
 {pendingAction && <Drawer title="有未保存的报修草稿" onClose={() => setPendingAction(null)}><p>继续将放弃卡片中尚未保存的修改，已保存的草稿仍在历史对话中。</p><div className="drawer-actions"><button onClick={() => setPendingAction(null)}>继续编辑</button><button className="danger" onClick={() => { const action = pendingAction; setPendingAction(null); setDraftDirty(false); action(); }}>放弃修改并继续</button></div></Drawer>}
 {sourceOpen && <Drawer title="来源核对" onClose={closeSource}>{opening ? <Loading text="正在读取原文…"/> : sourceError ? <div className="error" role="alert">{sourceError}</div> : source ? <><div className="source-meta"><span className="badge">引用来源</span>{source.evidence.demo && <span className="badge">演示材料</span>}</div><h3 className="source-title">{source.evidence.title}</h3><p className="muted">片段 {source.evidence.sequence} · 第 {source.evidence.start + 1}–{source.evidence.end} 个字符</p><OriginalText text={source.text} start={source.evidence.start} end={source.evidence.end}/></> : <Empty title="暂无来源"/>}</Drawer>}
 </>;
}
