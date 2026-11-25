import { Select } from './Select';
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { api, time } from './api';
import type { User } from './api';
import { Drawer, Empty, Icon, LeaveConfirmation, Loading, OriginalText } from './ui';
import { ConversationHistory } from './ConversationHistory';
import { NaturalAnswer } from './NaturalAnswer';
import { BusinessResultCard } from './BusinessResultCard';
import { RepairDraftCard } from './RepairDraftCard';
import { streamAnswer } from './assistant-api';
import type { Agent, Citation, Conversation, Message, TechnicianHandoff, RequesterHandoff } from './assistant-api';
import {readView, saveView} from './assistant-view';
type SelectedReview = {cid:number; message_id:number; order_id:number; number:string};
type ReadingView = {page:number; top:number; history:boolean; expanded:string[]; input:string; review:SelectedReview|null};
const stateNames: Record<string, string> = { pending: '等待回答', generating: '生成中 · 尚未核验', failed: '未完成', cancelled: '已停止 · 未完成' };
export function Assistant({ onCompletion, onRequesterFollowup, onManual, orderRevision, role, onOpen, onDirtyChange, active, storageKey, discardSignal }: { onCompletion: (draft:TechnicianHandoff) => void; onRequesterFollowup:(draft:RequesterHandoff)=>void; storageKey:string; orderRevision:number; discardSignal:number; active: boolean; onManual: () => void; role: User['role']; onOpen: (id: number, section?: 'visit') => void; onDirtyChange: (v: boolean) => void }) {
 const [draftDirty, setDraftDirty] = useState(false), [draftBusy, setDraftBusy] = useState(false);
 const [pendingAction, setPendingAction] = useState<(() => void) | null>(null);
 const dirtyChanged = useCallback((v: boolean) => setDraftDirty(v), []);
 const busyChanged = useCallback((v: boolean) => setDraftBusy(v), []);
 function guard(action: () => void) { if (draftBusy) return; if (draftDirty) setPendingAction(() => action); else action(); }
 const [agents, setAgents] = useState<Agent[]>([]), [agent, setAgent] = useState<number>(0);
 const [conversation, setConversation] = useState<Conversation | null>(null);
 const [items, setItems] = useState<Message[]>([]), [page, setPage] = useState(1), [total, setTotal] = useState(0);
 const [review,setReview]=useState<SelectedReview|null>(null);
 const [input, setInput] = useState(''), [busy, setBusy] = useState(false), [loading, setLoading] = useState(true), [error, setError] = useState(''), [configured, setConfigured] = useState(false), [chatConfigured, setChatConfigured] = useState(false);
 const [source, setSource] = useState<{ evidence: Citation; text: string } | null>(null), [sourceError, setSourceError] = useState('');
 const [sourceOpen, setSourceOpen] = useState(false), [opening, setOpening] = useState(false), [historyOpen, setHistoryOpen] = useState(false);
 const [viewingHistory, setViewingHistory] = useState(false);
 const [retryRead, setRetryRead] = useState<{ id: number; page: number } | null>(null);
 const restoring=useRef<ReadingView|null>(null);
 const controller = useRef<AbortController | null>(null), messagesPanel = useRef<HTMLDivElement>(null), question = useRef<HTMLTextAreaElement>(null), serial = useRef(0), sourceSerial = useRef(0), temporaryID = useRef(-1);
 useEffect(() => { onDirtyChange(active && (draftDirty || draftBusy || busy)); return () => onDirtyChange(false); }, [active, draftDirty, draftBusy, busy, onDirtyChange]);
 useEffect(() => { if (!draftDirty) return; const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = ''; }; window.addEventListener('beforeunload', warn); return () => window.removeEventListener('beforeunload', warn); }, [draftDirty]);
 useEffect(() => {
  const abort = new AbortController();
  const options = { signal: abort.signal };
  void Promise.all([api<{ items: Agent[] }>('/api/agents', options), api<{ assistant: boolean; chat: string }>('/api/capabilities', options)])
   .then(([a, caps]) => {
    if (abort.signal.aborted) return;
    setAgents(a.items);
    setAgent(a.items[0]?.id || 0);
    setConfigured(caps.assistant);
    setChatConfigured(caps.chat === 'configured');
    const saved = Number(sessionStorage.getItem(storageKey));
    if (saved > 0) {
     const view=readView<ReadingView|null>(storageKey+'/'+saved+'/reading',null);
     restoring.current=view;
     setViewingHistory(view?.history??true);
     setInput(view?.input??'');
     void select(saved,view?.page||1);
     if(view?.review?.cid===saved)setReview(view.review);
    } else setLoading(false);
   })
   .catch(e => {
    if (abort.signal.aborted) return;
    setError((e as Error).message);
    setLoading(false);
   });
  return () => { abort.abort(); controller.current?.abort(); serial.current++; };
 }, [storageKey]);
 useLayoutEffect(() => {
  const field = question.current;
  if (!field || CSS.supports('field-sizing', 'content')) return;
  field.style.height = 'auto';
  field.style.height = `${field.scrollHeight}px`;
 }, [input]);
 useEffect(()=>{
  if (!conversation || loading || !active) return;
  const key=storageKey+'/'+conversation.id+'/reading';
  const save=()=>{
   const panel=messagesPanel.current;
   if(!panel)return;
   const expanded:string[]=[];
   panel.querySelectorAll('[data-message-id]').forEach(article=>{
    article.querySelectorAll('details').forEach((details,i)=>{if(details.open)expanded.push(article.getAttribute('data-message-id')+'/'+i);});
   });
   saveView(key,{page,top:panel.scrollTop,history:viewingHistory,expanded,input,review});
  };
  window.addEventListener('pagehide',save);
  return ()=>window.removeEventListener('pagehide',save);
 },[conversation,loading,active,storageKey,page,viewingHistory,input,review]);
 useLayoutEffect(() => {
  const panel=messagesPanel.current; if (!panel || !items.length) return;
  const view=restoring.current;
  if(view){
   restoring.current=null;
   panel.querySelectorAll('[data-message-id]').forEach(article=>{
    article.querySelectorAll('details').forEach((details,i)=>{details.open=view.expanded.includes(article.getAttribute('data-message-id')+'/'+i);});
   });
   const restore=()=>{panel.scrollTop=view.top;};
   restore();
   // A restored dispatch batch can grow after its read-only candidate request finishes.
   const observer=new ResizeObserver(restore);
   Array.from(panel.children).forEach(el=>observer.observe(el));
   const stop=()=>observer.disconnect();
   const events=['wheel','touchstart','pointerdown','keydown'];
   events.forEach(e=>window.addEventListener(e,stop,{passive:true}));
   return ()=>{stop();events.forEach(e=>window.removeEventListener(e,stop));};
  }
  const latest=items[items.length-1];
  if (!busy && (latest?.business || latest?.draft)) {
   const article=panel.querySelector<HTMLElement>(`[data-message-id="${latest.id}"]`);
   if(article) panel.scrollTop+=article.getBoundingClientRect().top-panel.getBoundingClientRect().top-12;
  } else if (busy || latest?.status==='failed' || latest?.status==='cancelled' || items.some(m=>m.draft?.status==='active')) panel.scrollTop=panel.scrollHeight;
 }, [items,busy]);
 useEffect(()=>{if(!discardSignal)return;controller.current?.abort();setDraftDirty(false);if(conversation)void select(conversation.id,page);},[discardSignal]);
 const lastOrderRevision = useRef(orderRevision);
 useEffect(() => {
  if (role === 'admin' || !active || busy || lastOrderRevision.current === orderRevision) return;
  lastOrderRevision.current = orderRevision;
  if (conversation && items.some(m => m.business?.kind === 'prepare_wait_parts' || m.business?.kind === 'prepare_completion' || m.business?.kind === 'prepare_requester_followup')) {
   void select(conversation.id, page, true);
  }
 }, [active, busy, conversation, items, orderRevision, page, role]);
 function closeSource() { sourceSerial.current++; setSourceOpen(false); setSource(null); setSourceError(''); }
 async function select(id: number, p = 1, quiet = false) {
  const request = ++serial.current;
  if (!quiet) {setLoading(true);setReview(null);}
  setError('');
  setRetryRead(null);
  closeSource();
  try {
   const v = await api<{ items: Message[]; total: number; conversation: Conversation }>(`/api/conversations/${id}/messages?page=${p}&page_size=20`);
   if (request !== serial.current) return;
   setConversation(v.conversation);
   sessionStorage.setItem(storageKey, String(v.conversation.id));
   setAgent(v.conversation.agent_id);
   setItems(v.items);
   setTotal(v.total);
   setPage(p);
  } catch (e) {
   if (request === serial.current) { setError((e as Error).message); setRetryRead({ id, page: p }); }
  } finally {
   if (request === serial.current) setLoading(false);
  }
 }
 function newChat() { setReview(null); sessionStorage.removeItem(storageKey); serial.current++; setConversation(null); setItems([]); setTotal(0); setPage(1); closeSource(); setError(''); setRetryRead(null); setInput(''); setViewingHistory(false); question.current?.focus(); }
 function submit(e: FormEvent) { e.preventDefault(); guard(() => { void sendMessage(); }); }
 async function sendMessage(retry?: string) {
  if (controller.current || busy || !(retry || input).trim() || !agent) return; const text = (retry || input).trim(); setBusy(true); setError(''); setRetryRead(null); setViewingHistory(false); closeSource(); let c = conversation;
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
    if (event === 'done') patch(m => ({ ...m, content: data.answer || '', status: 'complete', citations: data.citations || [], warning: data.warning || '', draft: data.draft, business: data.business, skill: typeof data.skill === 'object' ? data.skill : undefined }));
    if (event === 'error') patch(m => ({ ...m, status: 'failed', warning: data.message || '回答未完成', citations: [] }));
   }, review?.cid===c.id ? {message_id:review.message_id,order_id:review.order_id}:undefined);
   setReview(null);
   await select(c.id, 1, true);
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
  requester: {welcome:'需要报修，还是想看看进度？',description:'描述故障、确认报修，或查看你自己的工单与待办。',examples:['宿舍灯坏了，帮我报修','我的报修还有多少单未结束？','有哪些报修待我确认？','报修需要填写哪些信息？']},
  technician: {welcome:'今天要去哪里维修？',description:'查看上门预约、新派任务，或查询维修资料。',examples:['我今天有哪些上门预约？','今天派给我的有哪些活？','有哪些工单待接单？','准备完工上报']},
  admin: {welcome:'今天有哪些问题值得关注？',description:'发现高发问题、积压与上门阻塞。',examples:['总结当前有哪些问题','最近出现最多的问题是什么','最近哪些工单该先处理？','给待派工单推荐维修人员']},
 }[role];
 const latestBusiness = page === 1 ? [...items].reverse().find(m => m.business && !m.business.unavailable)?.id : undefined;
 const agentName = agents.find(a=>a.id===(conversation?.agent_id || agent))?.name || conversation?.agent_name || '报修助手';
 function fillQuestion(text:string) {setInput(text);question.current?.focus();}
 function questionBefore(index:number) { return items.slice(0,index).reverse().find(m=>m.role==='user')?.content; }
 function retryQuestion(index:number) {const text=questionBefore(index);if(text)guard(()=>{void sendMessage(text);});}

 return <>
 {error && <div className="error" role="alert">{error} {retryRead && <button type="button" disabled={busy || draftBusy || loading} onClick={() => guard(() => { void select(retryRead.id, retryRead.page); })}>重试读取对话</button>}</div>}
 {!loading && !configured && <div className="assistant-notice" role="status">对话尚未配置。仍可查看历史。</div>}
 {!loading && configured && !chatConfigured && <div className="assistant-notice" role="status">可查询实时工单，资料问答暂不可用。</div>}
 <section className="panel chat-panel" aria-label="对话区">
  <header className="chat-toolbar">
   <div className="assistant-heading">
    <h1>报修助手</h1>
    {!conversation && agents.length > 1 ? <Select className="assistant-picker" aria-label="当前助手" value={agent} disabled={busy || draftBusy} onChange={e => setAgent(Number(e.target.value))}>{agents.map(a => <option value={a.id} key={a.id}>{a.name}</option>)}</Select> : <span className="assistant-agent-name" title={agentName}>{agentName}</span>}
   </div>
   <div className="chat-toolbar-actions">
    <button type="button" aria-label="对话历史" aria-haspopup="dialog" disabled={busy || draftBusy || loading} onClick={event => { event.currentTarget.focus(); setHistoryOpen(true); }}><Icon kind="history"/><span>历史</span></button>
    <button type="button" aria-label="新建对话" disabled={busy || draftBusy || loading} onClick={() => guard(newChat)}><Icon kind="plus"/><span>新建对话</span></button>
    <button type="button" aria-label="返回工单" title="返回工单" onClick={onManual}><Icon kind="left"/><span>返回工单</span></button>
   </div>
  </header>
  <div className="chat-messages" ref={messagesPanel}>{conversation && items.length > 0 && (viewingHistory || page > 1) && <p className="chat-snapshot">历史对话 · {time(conversation.updated_at)}</p>}{loading ? <Loading text="正在读取对话…"/> : !items.length ? <div className="chat-empty"><h2>{scenes.welcome}</h2><p>{scenes.description}</p><div className="suggested-questions">{scenes.examples.map(text => <button key={text} disabled={busy || draftBusy || !agent} onClick={() => { fillQuestion(text); }}><Icon kind={text.includes('信息') ? 'book' : 'orders'}/>{text}<Icon kind="arrow"/></button>)}</div></div> : items.map((m, index) => <article key={m.id} data-message-id={m.id} className={`chat-message ${m.role}`}><div className="message-label"><span className="message-author">{m.role === 'user' ? '你' : <><Icon kind="chat"/>{agentName}</>}</span>{stateNames[m.status] && <span className="message-state">{stateNames[m.status]}</span>}</div>{m.business?.natural ? (m.business.kind==='worklist' && m.business.query.status==='unassigned' && /派单|指派|推荐维修人员/.test(questionBefore(index)||'') ? <details className="assistant-explanation"><summary>查看派单建议说明</summary><NaturalAnswer body={m.content}/></details> : <NaturalAnswer body={m.content}/>) : (!m.business || m.business.kind!=='analysis') && <div className="message-content">{m.content || (m.status === 'generating' ? (m.phase || '正在理解你的问题…') : '本轮未产生完整回答。')}</div>}{m.skill && <details className="assistant-explanation"><summary>{({repair_intake:'故障信息补全',safe_self_check:'安全自查建议',dispatch_advice:'派单辅助'} as Record<string,string>)[m.skill.skill_id] || m.skill.skill_id}</summary><small>技能版本 {m.skill.skill_version} · 配置版本 {m.skill.config_version}</small></details>}{m.warning && <p className="citation-warning" role="status">{m.warning}</p>}{m.status === 'complete' && m.citations?.length > 0 && <div className="citation-list" aria-label="回答来源">{m.citations.map(c => <button key={c.number} onClick={() => openSource(m, c)}><Icon kind="file"/><span>[{c.number}] {c.title}</span><Icon kind="arrow"/></button>)}</div>}{m.status === 'complete' && m.business && <BusinessResultCard viewKey={conversation && m.id>0 ? storageKey+'/'+conversation.id+'/'+m.id : undefined} onReview={conversation && role==='admin' ? (id,number)=>{setReview({cid:conversation.id,message_id:m.id,order_id:id,number});question.current?.focus();}:undefined} onRequesterFollowup={role==='requester'?draft=>guard(()=>onRequesterFollowup(draft)):undefined} onCompletion={role==='technician'?draft=>guard(()=>onCompletion(draft)):undefined} result={m.business} pageUrl={conversation && m.id>0 ? `/api/conversations/${conversation.id}/messages/${m.id}/orders?view=related`:undefined} onRefreshDispatch={role==='admin'?()=>guard(()=>{void sendMessage('给待派工单推荐维修人员');}):undefined} canPage={m.id === latestBusiness} disabled={busy || draftBusy || loading} onOpen={(id,section)=>guard(()=>onOpen(id,section))} onQuestion={fillQuestion} onManual={()=>guard(onManual)}/>}{m.business?.additional?.map((extra, i) => <BusinessResultCard key={i} result={extra} pageUrl={conversation && m.id>0 ? `/api/conversations/${conversation.id}/messages/${m.id}/orders?additional=${i}`:undefined} disabled={busy || draftBusy || loading} onOpen={(id, section)=>guard(()=>onOpen(id, section))} onQuestion={fillQuestion} onManual={()=>guard(onManual)}/>)}{(m.status === 'failed' || m.status === 'cancelled') && m.role === 'assistant' && <div className="answer-recovery"><button className="text-button retry-answer" disabled={busy || draftBusy || loading} onClick={()=>retryQuestion(index)}>重新提问</button><button className="text-button" disabled={busy || draftBusy || loading} onClick={()=>{const text=questionBefore(index);if(text)fillQuestion(text);}}>编辑问题</button><button className="text-button" disabled={busy || draftBusy || loading} onClick={()=>guard(onManual)}>前往工单</button></div>}{role === 'requester' && m.status === 'complete' && m.draft && <RepairDraftCard draft={m.draft} disabled={busy || loading} onDirtyChange={m.draft.status === 'active' ? dirtyChanged : () => {}} onBusyChange={busyChanged} onOpen={id => guard(() => onOpen(id))} onChange={draft => setItems(items => items.map(item => item.id === m.id ? { ...item, draft } : item))}/>}</article>)}</div>
  {conversation && total > 20 && <div className="pagination"><button disabled={busy || loading || page * 20 >= total} onClick={() => guard(() => { void select(conversation.id, page + 1); })}>更早消息</button><span>第 {page} 页</span><button disabled={busy || loading || page === 1} onClick={() => guard(() => { void select(conversation.id, page - 1); })}>较新消息</button></div>}
  {review && review.cid===conversation?.id && <div role="status">追问工单 {review.number} <button type="button" className="text-button" disabled={busy} onClick={()=>setReview(null)}>取消引用</button></div>}
  <form className="chat-composer" onSubmit={submit}>
   <label htmlFor="chat-question" className="sr-only">你的问题</label>
   <textarea ref={question} onKeyDown={e=>{
    if(e.key!=='Enter'||e.shiftKey||e.nativeEvent.isComposing||e.nativeEvent.keyCode===229)return;
    e.preventDefault();
    if(!e.repeat && !busy && !draftBusy && !loading && configured && agent && input.trim() && [...input].length<=2000)e.currentTarget.form?.requestSubmit();
   }} title="Enter 发送，Shift+Enter 换行" id="chat-question" rows={1} required maxLength={2000} value={input} onChange={e => setInput(e.target.value)} disabled={busy || draftBusy} placeholder="输入你的问题…"/>
   {input.length >= 1800 && <small className="chat-input-count">{[...input].length}/2000</small>}
   {busy ? <button key="stop" type="button" aria-label="停止生成" title="停止生成" onClick={() => controller.current?.abort()}><Icon kind="stop"/></button> : <button key="send" type="submit" className="primary" aria-label="发送" title="发送" disabled={draftBusy || !agent || !configured || loading || !input.trim() || [...input].length > 2000}><Icon kind="send"/></button>}
  </form>
 </section>
 {active && historyOpen && <ConversationHistory currentId={conversation?.id} onClose={() => setHistoryOpen(false)} onSelect={id => {
  setHistoryOpen(false);
  guard(() => { setViewingHistory(true); void select(id); });
 }}/>}
 {pendingAction && <LeaveConfirmation title="有未保存的报修草稿" description="继续会放弃这次尚未保存的编辑。已保存的草稿可以在历史对话中查看。" discardLabel="放弃修改并继续" onCancel={() => setPendingAction(null)} onDiscard={() => {
  const action = pendingAction;
  setPendingAction(null);
  setDraftDirty(false);
  action();
 }}/>}
 {sourceOpen && <Drawer title="来源核对" onClose={closeSource}>{opening ? <Loading text="正在读取原文…"/> : sourceError ? <div className="error" role="alert">{sourceError}</div> : source ? <><div className="source-meta"><span className="badge">引用来源</span>{source.evidence.demo && <span className="badge">演示材料</span>}</div><h3 className="source-title">{source.evidence.title}</h3><p className="muted">片段 {source.evidence.sequence} · 第 {source.evidence.start + 1}–{source.evidence.end} 个字符</p><OriginalText text={source.text} start={source.evidence.start} end={source.evidence.end}/></> : <Empty title="暂无来源"/>}</Drawer>}
 </>;
}
