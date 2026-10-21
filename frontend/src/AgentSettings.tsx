import { useEffect, useState } from 'react';
import { api, roleNames } from './api';
import type { Agent } from './assistant-api';

type Material = { id: number; title: string; demo: boolean; generation: number };
type MCPTool = { name: string; role: string; description: string };

export function AgentSettings() {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [materials, setMaterials] = useState<Material[]>([]);
  const [tools, setTools] = useState<MCPTool[]>([]);
  const [toolError, setToolError] = useState('');
  const [editing, setEditing] = useState<Agent | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  useEffect(() => { void load(); }, []);

  async function load() {
    setLoading(true);
    setError('');
    try {
      const a = await api<{ items: Agent[] }>('/api/agents?manage=1');
      const all: Material[] = [];
      let page = 1, total = 0;
      do {
        const v = await api<{ items: Material[]; total: number }>(`/api/materials?page_size=100&page=${page++}`);
        all.push(...v.items);
        total = v.total;
      } while (all.length < total);
      setAgents(a.items);
      setMaterials(all);
      setEditing(null);
      try {
        const found = await api<{ items: MCPTool[] }>('/api/mcp/tools');
        setTools(found.items);
        setToolError('');
      } catch {
        setTools([]);
        setToolError('MCP 工具发现暂不可用。材料和提示词仍可编辑。');
      }
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }

  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (!editing) return;
    setBusy(true); setError(''); setNotice('');
    try {
      const v = await api<{ agent: Agent }>(`/api/agents/${editing.id}`, { method: 'PUT', body: JSON.stringify(editing) });
      setAgents(items => items.map(a => a.id === v.agent.id ? v.agent : a));
      setEditing(v.agent);
      setNotice('助手配置已保存，下次提问按新配置执行。');
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const available = tools.filter(t => t.role === editing?.role);
  return <>
    <div className="page-heading"><div><span className="eyebrow">管理员配置</span><h1>助手设置</h1><p className="muted">设置服务角色、提示词、材料与只读 MCP 工具。</p></div><button disabled={busy || loading} onClick={() => void load()}>刷新配置</button></div>
    {error && <div className="error" role="alert">{error}</div>}
    {notice && <div className="success" role="status">{notice}</div>}
    {loading ? <p>正在读取助手配置…</p> : <div className="agent-settings">
      <div className="agent-options">{agents.map(a => <button key={a.id} disabled={busy} className={`panel agent-option ${editing?.id === a.id ? 'selected' : ''}`} onClick={() => { setEditing({ ...a, material_ids: [...a.material_ids], tool_names: [...(a.tool_names || [])] }); setNotice(''); }}><strong>{a.name}</strong><span>{roleNames[a.role as keyof typeof roleNames]} · {a.enabled ? '已启用' : '已停用'}</span><small>{a.material_ids.length} 份材料 · {(a.tool_names || []).length} 项工具</small></button>)}</div>
      {editing ? <section className="panel agent-form"><form onSubmit={save}><fieldset disabled={busy}>
        <h2>{editing.name}</h2>
        <label>名称<input required maxLength={100} value={editing.name} onChange={e => setEditing({ ...editing, name: e.target.value })} /></label>
        <label>适用角色<select value={editing.role} onChange={e => setEditing({ ...editing, role: e.target.value, tool_names: [] })}>{Object.entries(roleNames).map(([k, name]) => <option value={k} key={k}>{name}</option>)}</select></label>
        <label>角色提示词<textarea required rows={5} maxLength={4000} value={editing.prompt || ''} onChange={e => setEditing({ ...editing, prompt: e.target.value })} /></label>
        <label className="demo-checkbox"><input type="checkbox" checked={editing.enabled} onChange={e => setEditing({ ...editing, enabled: e.target.checked })} />启用助手</label>
        <h3>绑定材料</h3><p className="muted">助手只检索已绑定且已发布的本组织材料。</p>
        <div className="material-choices">{materials.length ? materials.map(m => <label key={m.id} className="material-choice"><input type="checkbox" checked={editing.material_ids.includes(m.id)} onChange={e => setEditing({ ...editing, material_ids: e.target.checked ? [...editing.material_ids, m.id] : editing.material_ids.filter(id => id !== m.id) })} /><span><strong>{m.title}</strong><small>{m.demo ? '演示 · ' : ''}{m.generation ? '已有索引版本' : '等待索引'}</small></span></label>) : <p>还没有材料，请先到知识库上传。</p>}</div>
        <h3>只读 MCP 工具</h3><p className="muted">仅可绑定已由内部 MCP Server 发现、且适合该角色的工具；工具只能查询，不会创建或修改工单。</p>
        {toolError && <p className="muted" role="status">{toolError}</p>}
        <div className="material-choices">{available.map(tool => <label key={tool.name} className="material-choice"><input type="checkbox" checked={(editing.tool_names || []).includes(tool.name)} onChange={e => setEditing({ ...editing, tool_names: e.target.checked ? [...(editing.tool_names || []), tool.name] : (editing.tool_names || []).filter(name => name !== tool.name) })} /><span><strong>{tool.name}</strong><small>{tool.description}</small></span></label>)}</div>
        <button className="primary">{busy ? '正在保存…' : '保存助手配置'}</button>
      </fieldset></form></section> : <section className="panel agent-form"><h2>选择一个助手进行设置</h2><p className="muted">停用后不能继续提问，本人的历史仍会保留。</p></section>}
    </div>}
  </>;
}
