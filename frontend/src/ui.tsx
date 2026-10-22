import { useEffect, useId, useRef } from 'react';
import type { ReactNode } from 'react';

export function Icon({ kind = 'orders' }: { kind?: string }) {
 const paths: Record<string, ReactNode> = {
  orders: <><rect x="5" y="3" width="14" height="18" rx="2"/><path d="M9 8h6M9 12h6M9 16h3"/></>,
  plus: <path d="M12 5v14M5 12h14"/>, arrow: <path d="m9 5 7 7-7 7"/>, left: <path d="m15 5-7 7 7 7"/>, check: <path d="m5 12 4 4L19 6"/>,
  tool: <><path d="m14 5 5 5-9 9-5-5 9-9Z"/><path d="m15 4 5 5M4 20l3-3"/></>,
  logout: <><path d="M10 4H5v16h5M9 12h12m-4-4 4 4-4 4"/></>,
  chat: <path d="M5 4h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H9l-6 4V6a2 2 0 0 1 2-2Z"/>,
  settings: <><path d="M4 7h16M4 17h16"/><circle cx="8" cy="7" r="3" fill="currentColor" stroke="none"/><circle cx="16" cy="17" r="3" fill="currentColor" stroke="none"/></>,
  book: <><path d="M12 5v16M3 4h5a4 4 0 0 1 4 2 4 4 0 0 1 4-2h5v15h-5a4 4 0 0 0-4 2 4 4 0 0 0-4-2H3Z"/></>,
  audit: <><path d="M9 3h9a2 2 0 0 1 2 2v16H4V5a2 2 0 0 1 2-2h3M8 8h8M8 12h8M8 16h5"/></>,
  close: <path d="m6 6 12 12M6 18 18 6"/>, menu: <path d="M4 6h16M4 12h16M4 18h16"/>,
  refresh: <><path d="M20 7v5h-5M4 17v-5h5"/><path d="M6 7a7 7 0 0 1 12-1l2 3M4 15l2 3a7 7 0 0 0 12-1"/></>,
  search: <><circle cx="10" cy="10" r="6"/><path d="m15 15 5 5"/></>,
  file: <><path d="M14 3H5v18h14V8Z M14 3v5h5M9 13h6M9 17h4"/></>,
  upload: <><path d="M12 16V3m-5 5 5-5 5 5M4 16v5h16v-5"/></>,
  send: <><path d="m3 3 18 9-18 9 3-9-3-9ZM6 12h15"/></>,
  stop: <rect x="6" y="6" width="12" height="12" rx="2"/>,
  more: <><circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/></>,
 };
 return <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[kind] || paths.orders}</svg>;
}
export function PageHeading({ title, description, children }: { title: string; description?: string; children?: ReactNode }) {
 return <div className="page-heading"><div><h1>{title}</h1>{description && <p className="muted">{description}</p>}</div>{children && <div className="heading-actions">{children}</div>}</div>;
}
export function Loading({ text = '正在读取…' }: { text?: string }) { return <div className="loading" role="status"><span className="spinner"/>{text}</div>; }
export function Empty({ title, description, children, icon = 'file' }: { title: string; description?: string; children?: ReactNode; icon?: string }) {
 return <div className="empty"><span className="empty-icon"><Icon kind={icon}/></span><h3>{title}</h3>{description && <p>{description}</p>}{children}</div>;
}
export function Drawer({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
 const heading = useId(), panel = useRef<HTMLElement>(null), close = useRef(onClose); close.current = onClose;
 useEffect(() => {
  const previous = document.activeElement as HTMLElement | null, overflow = document.body.style.overflow;
  document.body.style.overflow = 'hidden';
  panel.current?.querySelector<HTMLElement>('[data-drawer-close]')?.focus();
  function keys(e: KeyboardEvent) {
   if (e.key === 'Escape') { e.preventDefault(); close.current(); }
   if (e.key !== 'Tab') return;
   const nodes = [...(panel.current?.querySelectorAll<HTMLElement>('button:not(:disabled),a[href],input:not(:disabled),select:not(:disabled),textarea:not(:disabled),summary,[tabindex="0"]') || [])].filter(el => el.getClientRects().length > 0);
   const first = nodes[0], last = nodes.at(-1);
   if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus(); }
   if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus(); }
  }
  document.addEventListener('keydown', keys);
  return () => { document.removeEventListener('keydown', keys); document.body.style.overflow = overflow; if (previous?.isConnected) previous.focus({ preventScroll: true }); };
 }, []);
 return <div className="drawer-layer"><div className="drawer-backdrop" onClick={onClose}/><section ref={panel} className="drawer" role="dialog" aria-modal="true" aria-labelledby={heading}><div className="drawer-heading"><h2 id={heading}>{title}</h2><button type="button" data-drawer-close className="icon-button" aria-label="关闭侧栏" onClick={onClose}><Icon kind="close"/></button></div><div className="drawer-content">{children}</div></section></div>;
}
export function OriginalText({ text, start, end }: { text: string; start?: number; end?: number }) {
 const pre = useRef<HTMLPreElement>(null);
 useEffect(() => { const mark = pre.current?.querySelector('mark'); if (pre.current && mark) pre.current.scrollTop = mark.offsetTop - 60; }, [text, start, end]);
 const chars = [...text];
 return <pre className="original-text" ref={pre} tabIndex={0}>{start !== undefined && end !== undefined ? <>{chars.slice(0, start).join('')}<mark>{chars.slice(start, end).join('')}</mark>{chars.slice(end).join('')}</> : text}</pre>;
}
