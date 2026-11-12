import { useEffect, useRef, useState } from 'react';
import { allowedView, homeView, parseRoute, routeHash } from './navigation';
import type { Route } from './navigation';
type Entry = { campus: true; index: number; route: Route; scrollY?:number };
export function useNavigation(role: string | null, blocked: boolean) {
 const [route, setRoute] = useState<Route>({view:'list'});
 const [pending, setPending] = useState<(() => void) | null>(null);
 const current = useRef<Entry>({campus:true,index:0,route:{view:'list'}});
 const blockedRef = useRef(blocked); blockedRef.current = blocked;
 const replay = useRef(false), restoring = useRef<(() => void) | null>(null);
 useEffect(() => {
  setPending(null);restoring.current=null;replay.current=false;
  if (!role) return;
  const source=(from?:Route['from'])=>from && allowedView(from,role) && parseRoute(`#/${from}`,role).view===from ? from : undefined;
  const initial = parseRoute(location.hash,role);
  // Reloads retain this tab's history index and detail origin; URLs contain no session data.
  const prior = history.state as Entry | null;
  const entry: Entry = {campus:true,index:prior?.campus ? prior.index : 0,route: {...initial, from:prior?.campus && prior.route && routeHash(prior.route)===routeHash(initial) ? source(prior.route.from) : undefined}};
  entry.scrollY=prior?.campus ? prior.scrollY : 0;
  const previousRestoration=history.scrollRestoration;history.scrollRestoration='manual';
  current.current=entry; history.replaceState(entry,'',routeHash(entry.route)); setRoute(entry.route);
  const remember=()=>{if(location.hash!==routeHash(current.current.route))return;current.current={...current.current,scrollY:window.scrollY};history.replaceState(current.current,'',location.hash);};
  window.addEventListener('scroll',remember,{passive:true});
  const pop = () => {
   if (restoring.current) { const action=restoring.current; restoring.current=null; setPending(()=>action); return; }
   const target = history.state as Entry | null;
   const next: Entry = target?.campus ? {...target,route:{...parseRoute(location.hash,role),from:source(target.route?.from)}} : {campus:true,index:0,route:parseRoute(location.hash,role)};
   const delta=next.index-current.current.index;
   if (blockedRef.current && !replay.current && delta) {
    restoring.current=()=>{replay.current=true;history.go(delta);}; history.go(-delta); return;
   }
   replay.current=false; if(location.hash!==routeHash(next.route))history.replaceState(next,'',routeHash(next.route)); current.current=next; setRoute(next.route); requestAnimationFrame(()=>window.scrollTo(0,next.scrollY || 0));
  };
  window.addEventListener('popstate',pop);
  return ()=>{window.removeEventListener('popstate',pop);window.removeEventListener('scroll',remember);history.scrollRestoration=previousRestoration;};
 },[role]);
 function guard(action:()=>void) { if(blockedRef.current)setPending(()=>action);else action(); }
 function navigate(next:Route,replace=false,confirmed=false) {
  if(routeHash(next)===routeHash(current.current.route))return;
  const apply=()=>{
   current.current={...current.current,scrollY:window.scrollY};history.replaceState(current.current,'',routeHash(current.current.route));
   const entry:Entry={campus:true,scrollY:0,index:current.current.index+(replace?0:1),route:next};
   history[replace?'replaceState':'pushState'](entry,'',routeHash(next)); current.current=entry;setRoute(next);window.dispatchEvent(new Event('campus-navigation'));window.scrollTo(0,0);
  };
  if(confirmed)apply();else guard(apply);
 }
 function back() {
  const r=current.current.route;
  if(current.current.index>0)history.back();
  else navigate({view:r.from || homeView(role || '')},true);
 }
 return {route,navigate,back,guard,pending,cancel:()=>setPending(null),proceed:()=>{const action=pending;setPending(null);action?.();}};
}
