import { useEffect, useState } from 'react';
import { api, roleNames } from './api';
import type { Agent } from './assistant-api';
import { Drawer, Empty, Icon, Loading, PageHeading } from './ui';

type Material = { id: number; title: string; demo: boolean; generation: number };
type MCPTool = { name: string; role: string; description: string };
const toolTitles: Record<string, string> = { get_my_work_orders: '查询本人工单', get_assigned_work_orders: '查询分配给我的任务', list_dispatch_candidates: '查询可派维修人员' };
function fingerprint(a?: Agent | null) { return a ? JSON.stringify([a.name, a.role, a.prompt || '', a.enabled, [...a.material_ids].sort((x,y) => x-y), [...(a.tool_names || [])].sort()]) : ''; }
function clone(a: Agent): Agent { return { ...a, material_ids: [...a.material_ids], tool_names: [...(a.tool_names || [])] }; }

export function AgentSettings({ onDirtyChange }: { onDirtyChange: (dirty: boolean) => void }) {
 const [agents, setAgents] = useState<Agent[]>([]), [materials, setMaterials] = useState<Material[]>([]), [tools, setTools] = useState<MCPTool[]>([]);
 const [toolError, setToolError] = useState(''), [editing, setEditing] = useState<Agent | null>(null);
 const [loading, setLoading] = useState(true), [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('');
 const [query, setQuery] = useState(''), [pending, setPending] = useState<Agent | 'refresh' | null>(null);
 const dirty = !!editing && fingerprint(editing) !== fingerprint(agents.find(a => a.id === editing.id));
 useEffect(() => { void load(); }, []);
 useEffect(() => { onDirtyChange(dirty); return () => onDirtyChange(false); }, [dirty, onDirtyChange]);
 useEffect(() => { if (!dirty) return; const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = ''; }; window.addEventListener('beforeunload', warn); return () => window.removeEventListener('beforeunload', warn); }, [dirty]);
 async function load() {
  setLoading(true); setError(''); setNotice('');
  try {
   const a = await api<{ items: Agent[] }>('/api/agents?manage=1');
   const all: Material[] = []; let page = 1, total = 0;
   do { const v = await api<{ items: Material[]; total: number }>(`/api/materials?page_size=100&page=${page++}`); all.push(...v.items); total = v.total; } while (all.length < total);
   setAgents(a.items); setMaterials(all); setEditing(a.items[0] ? clone(a.items[0]) : null); setQuery('');
   try { const found = await api<{ items: MCPTool[] }>('/api/mcp/tools'); setTools(found.items); setToolError(''); }
   catch { setTools([]); setToolError('实时查询工具暂时无法读取，已绑定工具会保留，材料和提示词仍可编辑。'); }
  } catch (e) { setError((e as Error).message); } finally { setLoading(false); }
 }
 function change(next: Agent | 'refresh') {
  if (busy) return;
  if (dirty) { setPending(next); return; }
  if (next === 'refresh') void load(); else { setEditing(clone(next)); setQuery(''); setNotice(''); setError(''); }
 }
 async function save(e: React.FormEvent) {
  e.preventDefault(); if (!editing) return; setBusy(true); setError(''); setNotice('');
  try { const v = await api<{ agent: Agent }>(`/api/agents/${editing.id}`, { method: 'PUT', body: JSON.stringify(editing) }); setAgents(items => items.map(a => a.id === v.agent.id ? v.agent : a)); setEditing(clone(v.agent)); setNotice('配置已保存，下次提问按新配置执行。'); }
  catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 }
 const available = tools.filter(t => t.role === editing?.role), filtered = materials.filter(m => m.title.toLowerCase().includes(query.toLowerCase().trim()));
 return <><PageHeading title="助手设置" description="配置各角色助手的资料范围与查询能力。"><button disabled={busy || loading} onClick={() => change('refresh')}><Icon kind="refresh"/>刷新配置</button></PageHeading>
 {error && <div className="error" role="alert">{error}</div>}{notice && <div className="success" role="status"><Icon kind="check"/>{notice}</div>}
 {loading ? <Loading text="正在读取助手配置…"/> : <div className="agent-settings"><aside className="panel agent-options"><h2>角色助手</h2>{agents.map(a => <button key={a.id} disabled={busy} aria-pressed={editing?.id === a.id} className={`agent-option ${editing?.id === a.id ? 'selected' : ''}`} onClick={() => { if (a.id !== editing?.id) change(a); }}><span className="agent-option-top"><strong>{a.name}</strong><span className={`status-dot ${a.enabled ? 'enabled' : ''}`}/></span><span>{roleNames[a.role as keyof typeof roleNames]} · {a.enabled ? '已启用' : '已停用'}</span><small>{a.material_ids.length} 份材料 · {(a.tool_names || []).length} 项查询</small></button>)}</aside>
 {editing ? <section className="panel agent-form"><form onSubmit={save}><fieldset disabled={busy}>
  <section className="settings-section"><div className="section-heading"><div><h2>基本信息</h2><p>设置助手名称、使用角色与回答要求。</p></div><label className="switch-label"><input type="checkbox" checked={editing.enabled} onChange={e => setEditing({ ...editing, enabled: e.target.checked })}/><span>{editing.enabled ? '已启用' : '已停用'}</span></label></div><div className="form-grid"><label>助手名称<input required maxLength={100} value={editing.name} onChange={e => setEditing({ ...editing, name: e.target.value })}/></label><label>适用角色<select value={editing.role} onChange={e => setEditing({ ...editing, role: e.target.value, tool_names: [] })}>{Object.entries(roleNames).map(([k, name]) => <option value={k} key={k}>{name}</option>)}</select></label></div><label>角色提示词<textarea required rows={4} maxLength={4000} value={editing.prompt || ''} onChange={e => setEditing({ ...editing, prompt: e.target.value })}/></label><p className="field-help">用于补充回答要求，不会改变用户权限或允许助手修改工单。</p></section>
  <section className="settings-section"><div className="section-heading"><div><h2>绑定材料 <span className="count">已选 {editing.material_ids.length}</span></h2><p>助手只使用已绑定、已发布的本组织资料。</p></div></div><label className="search-field"><span className="sr-only">搜索绑定材料</span><Icon kind="search"/><input aria-label="搜索绑定材料" value={query} onChange={e => setQuery(e.target.value)} placeholder="按材料名称搜索"/></label><div className="material-choices">{filtered.length ? filtered.map(m => <label key={m.id} className="material-choice"><input type="checkbox" checked={editing.material_ids.includes(m.id)} onChange={e => setEditing({ ...editing, material_ids: e.target.checked ? [...editing.material_ids, m.id] : editing.material_ids.filter(id => id !== m.id) })}/><span><strong>{m.title}</strong><small>{m.demo ? '演示材料 · ' : ''}{m.generation ? '可用于检索' : '等待索引'}</small></span></label>) : <p className="choice-empty">{materials.length ? '没有匹配的材料，试试其他名称。' : '还没有材料，请先到知识库上传。'}</p>}</div></section>
  <section className="settings-section"><div className="section-heading"><div><h2>实时查询工具 <span className="count">已选 {(editing.tool_names || []).length}</span></h2><p>仅提供适用于当前角色的只读查询。</p></div></div>{toolError && <div className="assistant-notice" role="status">{toolError}</div>}<div className="tool-choices">{available.map(tool => <label key={tool.name} className="material-choice"><input type="checkbox" checked={(editing.tool_names || []).includes(tool.name)} onChange={e => setEditing({ ...editing, tool_names: e.target.checked ? [...(editing.tool_names || []), tool.name] : (editing.tool_names || []).filter(name => name !== tool.name) })}/><span><strong>{toolTitles[tool.name] || tool.name}</strong><small>{tool.description}</small></span></label>)}</div><details className="technical-details"><summary>查看 MCP 技术信息</summary><p>{available.length ? available.map(t => t.name).join('、') : '暂无可用工具'}。工具通过内部 MCP 服务发现并二次校验权限。</p></details></section>
  <div className="settings-savebar">{error && <p className="savebar-error" role="status">{error}</p>}<span role="status">{dirty ? <><span className="unsaved-dot"/>有未保存的更改</> : '配置已同步'}</span><div><button type="button" disabled={!dirty || busy} onClick={() => change(agents.find(a => a.id === editing.id)!)}>撤销更改</button><button className="primary" disabled={!dirty || busy}>{busy ? '正在保存…' : '保存配置'}</button></div></div>
 </fieldset></form></section> : <section className="panel"><Empty title="暂无可配置助手"/></section>}</div>}
 {pending && <Drawer title="有未保存的更改" onClose={() => setPending(null)}><p>继续操作将放弃当前编辑。你可以返回保存，或放弃这些更改。</p><div className="drawer-actions"><button onClick={() => setPending(null)}>继续编辑</button><button className="danger" onClick={() => { const next = pending; setPending(null); if (next === 'refresh') void load(); else { setEditing(clone(next)); setQuery(''); setNotice(''); setError(''); } }}>放弃更改</button></div></Drawer>}
 </>;
}
