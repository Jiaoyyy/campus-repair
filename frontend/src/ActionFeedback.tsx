import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';
import { Icon } from './ui';

type Result = { title: string; detail?: string; warning?: boolean };
const Feedback = createContext<(result: Result | null) => void>(() => {});
export const useActionFeedback = () => useContext(Feedback);

export function ActionFeedback({ children }: { children: ReactNode }) {
 const [result, setResult] = useState<Result | null>(null);
 const [paused, setPaused] = useState(false);
 const clear = useCallback(() => setResult(null), []);
 useEffect(() => {
  window.addEventListener('campus-navigation', clear);
  window.addEventListener('popstate', clear);
  return () => {
   window.removeEventListener('campus-navigation', clear);
   window.removeEventListener('popstate', clear);
  };
 }, [clear]);
 useEffect(() => {
  if (!result || paused) return;
  const timer = window.setTimeout(clear, 9000);
  return () => window.clearTimeout(timer);
 }, [result, paused, clear]);
 useEffect(() => { if (!result) setPaused(false); }, [result]);
 return <Feedback.Provider value={setResult}>
  {children}
  <div className="action-feedback-region" aria-live="polite" aria-atomic="true">
   {result && <div className={'action-feedback' + (result.warning ? ' needs-attention' : '')}
     onMouseEnter={() => setPaused(true)} onMouseLeave={() => setPaused(false)}
     onFocus={() => setPaused(true)} onBlur={() => setPaused(false)}>
    <span className="feedback-symbol" aria-hidden="true">{result.warning ? '!' : <Icon kind="check"/>}</span>
    <div><strong>{result.title}</strong>{result.detail && <small>{result.detail}</small>}</div>
    <button type="button" aria-label="关闭操作提示" onClick={clear}><Icon kind="close"/></button>
   </div>}
  </div>
 </Feedback.Provider>;
}
