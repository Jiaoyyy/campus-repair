import { useEffect, useState } from 'react';
import { api, categoryNames, statusNames, time } from './api';
import { Select } from './Select';
import { auditLabel } from './presentation';
import { Icon, Loading, PageHeading } from './ui';

type Entry = { id: number; action: string; actor_id: number | null; actor_name: string; resource_id: number | null; order_number: string; outcome: string; created_at: string; request_id: string };
type Stats = { total: number; statuses: Record<string, number>; categories: Record<string, number> };

const filterDefaults = { action: '', outcome: '', actor_id: '', resource_id: '', date_from: '', date_to: '', request_id: '' };
const actions = {
 'work_order.*': '工单操作', 'work_order.assign': '派单', 'work_order.visit.*': '上门操作',
 'knowledge.*': '知识库', 'agent.update': '助手配置', 'skill.*': '技能', 'mcp.*': 'MCP 工具调用',
 'conversation.*': '对话', 'login': '登录', 'logout': '退出登录', 'access.denied': '访问拒绝',
};
const outcomes: Record<string, string> = { success: '成功', denied: '已拒绝', failed: '失败', failure: '依赖失败', started: '已开始', cancelled: '已取消', recorded: '已记录', degraded: '已降级' };

export function Audit({ onOpen }: { onOpen: (id: number) => void }) {
 const [statistics, setStatistics] = useState(false);
 const [revision, setRevision] = useState(0);
 return <>
  <PageHeading title="操作审计"><button onClick={() => setRevision(v => v + 1)}><Icon kind="refresh"/>刷新</button></PageHeading>
  <nav className="management-tabs" aria-label="审计视图">
   <button type="button" aria-pressed={!statistics} className={!statistics ? 'active' : ''} onClick={() => setStatistics(false)}>操作记录</button>
   <button type="button" aria-pressed={statistics} className={statistics ? 'active' : ''} onClick={() => setStatistics(true)}>工单统计</button>
  </nav>
  {statistics ? <OrganizationStats revision={revision}/> : <AuditRecords revision={revision} onOpen={onOpen}/>}
 </>;
}

function AuditRecords({ revision, onOpen }: { revision: number; onOpen: (id: number) => void }) {
 const [values, setValues] = useState(filterDefaults);
 const [filter, setFilter] = useState('');
 const [page, setPage] = useState(1);
 const [result, setResult] = useState<{ items: Entry[]; total: number }>();
 const [error, setError] = useState('');
 useEffect(() => {
  let active = true;
  const query = new URLSearchParams(filter);
  query.set('page', String(page));
  setError('');
  setResult(undefined);
  void api<{ items: Entry[]; total: number }>(`/api/audit?${query}`)
   .then(data => { if (active) setResult(data); })
   .catch(e => { if (active) setError(e.message); });
  return () => { active = false; };
 }, [filter, page, revision]);

 const field = (key: keyof typeof filterDefaults, label: string, type = 'text') =>
  <label>{label}<input type={type} className={type === 'date' ? 'date-control' : undefined} inputMode={key.endsWith('_id') && key !== 'request_id' ? 'numeric' : undefined} value={values[key]} onChange={e => setValues(v => ({ ...v, [key]: e.target.value }))}/></label>;

 return <section className="panel list-panel audit-records">
  <form className="audit-filters" onSubmit={e => {
   e.preventDefault();
   setPage(1);
   setFilter(new URLSearchParams(Object.entries(values).filter(([, value]) => value.trim()).map(([key, value]) => [key, value.trim()])).toString());
  }}>
   <label>操作类型<Select aria-label="操作类型" value={values.action} onChange={e => setValues(v => ({...v, action: e.target.value}))}><option value="">全部</option>{Object.entries(actions).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</Select></label>
   <label>操作结果<Select aria-label="操作结果" value={values.outcome} onChange={e => setValues(v => ({...v, outcome: e.target.value}))}><option value="">全部</option>{Object.entries(outcomes).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</Select></label>
   {field('date_from', '开始日期', 'date')}
   {field('date_to', '结束日期', 'date')}
   {field('actor_id', '操作人 ID')}
   {field('resource_id', '资源 ID')}
   {field('request_id', '请求标识')}
   <div className="audit-filter-actions"><button className="primary">筛选</button><button type="button" onClick={() => { setValues(filterDefaults); setPage(1); setFilter(''); }}>清除</button></div>
  </form>
  {error ? <p className="error" role="alert">{error}</p> : !result ? <Loading text="正在读取操作记录…"/> : result.items.length ? <>
   <div className="table-scroll"><table><thead><tr><th>操作与时间</th><th>操作人</th><th>相关工单</th><th>结果</th><th>请求标识</th></tr></thead>
    <tbody>{result.items.map(v => <tr key={v.id}>
     <td><strong>{auditLabel(v.action)}</strong><span className="order-number">{time(v.created_at)}</span></td>
     <td>{v.actor_name || (v.action === 'knowledge.index' ? '后台索引服务' : v.actor_id ? `账号 #${v.actor_id}` : '未登录用户')}{v.actor_name && v.actor_id && <small className="order-number">#{v.actor_id}</small>}</td>
     <td>{v.order_number && v.resource_id ? <button className="order-link" onClick={() => onOpen(v.resource_id!)}>{v.order_number}</button> : '—'}</td>
     <td><span className={`badge ${v.outcome === 'success' ? 'closed' : v.outcome === 'denied' || v.outcome === 'failed' ? 'rejected' : ''}`}>{outcomes[v.outcome] || v.outcome}</span></td>
     <td><code>{v.request_id}</code></td>
    </tr>)}</tbody>
   </table></div>
   <div className="pagination"><span>共 {result.total} 条 · 第 {page} 页</span><div><button disabled={page === 1} onClick={() => setPage(p => p - 1)}>上一页</button><button disabled={page * 20 >= result.total} onClick={() => setPage(p => p + 1)}>下一页</button></div></div>
  </> : <div className="empty"><h3>暂无匹配的操作记录</h3></div>}
 </section>;
}

function OrganizationStats({ revision }: { revision: number }) {
 const [stats, setStats] = useState<Stats>();
 const [error, setError] = useState('');
 useEffect(() => {
  let active = true;
  setStats(undefined);
  setError('');
  void api<Stats>('/api/work-orders/stats').then(data => { if (active) setStats(data); })
   .catch(e => { if (active) setError(e.message); });
  return () => { active = false; };
 }, [revision]);
 if (error) return <p className="error" role="alert">{error}</p>;
 if (!stats) return <Loading text="正在读取工单统计…"/>;
 return <section className="panel audit-statistics">
  <h2>本组织工单 <strong>{stats.total}</strong> 单</h2>
  <div className="audit-statistics-groups">
   <div><h3>按状态</h3><dl>{Object.entries(statusNames).map(([key, label]) => <div key={key}><dt>{label}</dt><dd>{stats.statuses[key] ?? 0}</dd></div>)}</dl></div>
   <div><h3>按故障类别</h3><dl>{Object.entries(categoryNames).map(([key, label]) => <div key={key}><dt>{label}</dt><dd>{stats.categories[key] ?? 0}</dd></div>)}</dl></div>
  </div>
 </section>;
}
