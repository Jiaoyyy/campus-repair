import { useEffect, useRef, useState } from 'react';
import { api } from './api';
import type { Conversation } from './assistant-api';
import { Drawer, Loading } from './ui';

const pageSize = 20;
const dateFormat = new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', year: 'numeric', month: 'long', day: 'numeric' });
const timeFormat = new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });

export function ConversationHistory({ currentId, onClose, onSelect }: { currentId?: number; onClose: () => void; onSelect: (id: number) => void }) {
 const [query, setQuery] = useState('');
 const [page, setPage] = useState(1);
 const [result, setResult] = useState<{ items: Conversation[]; total: number }>();
 const [error, setError] = useState('');
 const [revision, setRevision] = useState(0);
 const keyword = query.trim();
 const search = useRef<HTMLInputElement>(null);

 useEffect(() => {
  const abort = new AbortController();
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize), q: keyword });
  const timer = window.setTimeout(() => {
   void api<{ items: Conversation[]; total: number }>(`/api/conversations?${params}`, { signal: abort.signal })
    .then(data => { if (!abort.signal.aborted) setResult(data); })
    .catch(e => { if (!abort.signal.aborted) setError((e as Error).message); });
  }, keyword ? 200 : 0);
  return () => { window.clearTimeout(timer); abort.abort(); };
 }, [keyword, page, revision]);

 const groups = new Map<string, Conversation[]>();
 for (const conversation of result?.items || []) {
  const day = dateFormat.format(new Date(conversation.updated_at));
  const group = groups.get(day);
  if (group) group.push(conversation);
  else groups.set(day, [conversation]);
 }

 function goTo(next: number) {
  setError('');
  setResult(undefined);
  setPage(next);
  search.current?.focus();
 }

 return <Drawer title="对话历史" onClose={onClose}>
  <div className="conversation-history">
   <label className="sr-only" htmlFor="conversation-search">搜索对话标题</label>
   <input ref={search} id="conversation-search" type="search" placeholder="搜索对话标题" maxLength={100} value={query} onChange={e => {
    const value = e.target.value;
    if (value.trim() !== keyword) { setResult(undefined); setError(''); setPage(1); }
    setQuery(value);
   }}/>
   {error ? <div role="alert" className="error">{error} <button type="button" onClick={() => { setError(''); setRevision(r => r + 1); }}>重试</button></div>
    : !result ? <Loading text="正在读取对话…"/>
    : !result.total ? <p className="muted" role="status">{keyword ? '没有匹配的对话' : '还没有历史对话'}</p>
    : <>{[...groups].map(([day, items]) => <section className="conversation-history-group" key={day}>
     <h3>{day}</h3>
     <ul>{items.map(c => <li key={c.id}>
      <button type="button" className="conversation-history-item" aria-current={currentId === c.id ? 'true' : undefined} onClick={() => onSelect(c.id)}>
       <span>{c.title}</span><time dateTime={c.updated_at}>{timeFormat.format(new Date(c.updated_at))}</time>
      </button>
     </li>)}</ul>
    </section>)}
    {result.total > pageSize && <nav className="pagination" aria-label="历史对话分页">
     <button type="button" disabled={page === 1} onClick={() => goTo(page - 1)}>上一页</button>
     <span>第 {page} / {Math.ceil(result.total / pageSize)} 页</span>
     <button type="button" disabled={page * pageSize >= result.total} onClick={() => goTo(page + 1)}>下一页</button>
    </nav>}</>}
  </div>
 </Drawer>;
}
