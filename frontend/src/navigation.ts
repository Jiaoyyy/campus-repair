export type View = 'list' | 'workbench' | 'create' | 'detail' | 'assistant' | 'knowledge' | 'agents' | 'audit';
export type Route = { view: View; id?: number; from?: View; section?: 'visit' };
export function homeView(role: string): View { return role === 'technician' ? 'workbench' : 'list'; }
export function allowedView(view: View, role: string) {
 if (['knowledge', 'agents', 'audit'].includes(view)) return role === 'admin';
 if (view === 'create') return role === 'requester';
 if (view === 'workbench') return role !== 'requester';
 return true;
}
export function routeHash(route: Route) { return route.view === 'detail' ? `#/orders/${route.id}${route.section==='visit'?'/visit':''}` : `#/${route.view}`; }
export function parseRoute(hash: string, role: string): Route {
 const match = /^#\/orders\/([1-9]\d*)(\/visit)?$/.exec(hash);
 if (match && Number.isSafeInteger(Number(match[1]))) return { view: 'detail', id: Number(match[1]), ...(match[2]?{section:'visit' as const}:{}) };
 const view = hash.replace(/^#\//, '') as View;
 if (['list','workbench','create','assistant','knowledge','agents','audit'].includes(view) && allowedView(view,role)) return { view };
 return { view: homeView(role) };
}
