import { Children, isValidElement, useEffect, useId, useRef, useState } from 'react';
import type { ReactNode, SelectHTMLAttributes, CSSProperties, KeyboardEvent } from 'react';
import { createPortal } from 'react-dom';
import { Icon } from './ui';
type Props = Omit<SelectHTMLAttributes<HTMLSelectElement>, 'onChange' | 'multiple' | 'size'> & {onChange?: (event:{target:{value:string}})=>void};
type Option = {value:string;label:ReactNode;disabled:boolean};
export function Select({children,value,onChange,disabled,required,className='',...rest}:Props) {
 const options:Option[]=Children.toArray(children).flatMap(child=>isValidElement<{value?:string|number;children:ReactNode;disabled?:boolean}>(child)?[{value:String(child.props.value??''),label:child.props.children,disabled:!!child.props.disabled}]:[]);
 const selected=options.find(o=>o.value===String(value??''));
 const [invalid,setInvalid]=useState(false),[open,setOpen]=useState(false),[active,setActive]=useState(0),[position,setPosition]=useState<CSSProperties>({});
 const id=useId(), trigger=useRef<HTMLButtonElement>(null), native=useRef<HTMLSelectElement>(null), list=useRef<HTMLDivElement>(null),search=useRef({text:'',time:0});
 const unavailable=()=>!!disabled||!!trigger.current?.closest('fieldset:disabled');
 function close(focus=false){setOpen(false);if(focus)trigger.current?.focus();}
 function expand(){if(unavailable())return;setActive(Math.max(0,options.findIndex(o=>o.value===String(value??''))));setOpen(true);}
 function choose(index:number){const option=options[index];if(!option||option.disabled||unavailable())return;setInvalid(false);onChange?.({target:{value:option.value}});close(true);}
 useEffect(()=>{if(disabled)setOpen(false)},[disabled]);
 useEffect(()=>{
  if(!open)return;
  function place(){const r=trigger.current?.getBoundingClientRect();if(!r)return;const below=window.innerHeight-r.bottom-10,above=r.top-10,up=below<180&&above>below;setPosition({left:Math.max(8,Math.min(r.left,window.innerWidth-r.width-8)),width:Math.min(r.width,window.innerWidth-16),maxHeight:Math.max(80,Math.min(300,up?above:below)),...(up?{bottom:window.innerHeight-r.top+5}:{top:r.bottom+5})});}
  function navigation(){setOpen(false)}
  function outside(e:Event){if(!trigger.current?.contains(e.target as Node)&&!list.current?.contains(e.target as Node))setOpen(false);}
  function focusOutside(e:FocusEvent){if(!trigger.current?.contains(e.target as Node)&&!list.current?.contains(e.target as Node))setOpen(false);}
  place();window.addEventListener('popstate',navigation);window.addEventListener('campus-navigation',navigation);document.addEventListener('pointerdown',outside);document.addEventListener('focusin',focusOutside);window.addEventListener('resize',place);window.addEventListener('scroll',place,true);
  return()=>{window.removeEventListener('popstate',navigation);window.removeEventListener('campus-navigation',navigation);document.removeEventListener('pointerdown',outside);document.removeEventListener('focusin',focusOutside);window.removeEventListener('resize',place);window.removeEventListener('scroll',place,true)};
 },[open]);
 useEffect(()=>{if(open)list.current?.querySelector(`[data-index="${active}"]`)?.scrollIntoView({block:'nearest'})},[active,open]);
 function keydown(e:KeyboardEvent<HTMLButtonElement>){
  if(e.key==='Tab'){close();return;}
  if(e.key==='Escape'){if(open){e.preventDefault();e.stopPropagation();close(true)}return;}
  if(['Enter',' '].includes(e.key)){e.preventDefault();if(open)choose(active);else expand();return;}
  if(['ArrowDown','ArrowUp','Home','End'].includes(e.key)){
   e.preventDefault();if(!open){expand();return;}
   const step=e.key==='ArrowUp'||e.key==='End'?-1:1;
   let next=e.key==='Home'?0:e.key==='End'?options.length-1:Math.max(0,Math.min(options.length-1,active+step));
   while(options[next]?.disabled&&next>=0&&next<options.length)next+=step;
   if(options[next])setActive(next);return;
  }
  if(e.key.length===1){e.preventDefault();const now=Date.now();search.current={text:(now-search.current.time<700?search.current.text:'')+e.key,time:now};const index=options.findIndex(o=>!o.disabled&&String(o.label).toLowerCase().startsWith(search.current.text.toLowerCase()));if(index>=0){if(!open)expand();setActive(index);}}
 }
 return <span className={`select-control ${className}`}>
  <select {...rest} ref={native} value={value} disabled={disabled} required={required} tabIndex={-1} aria-hidden="true" className="select-native" onChange={e=>onChange?.({target:{value:e.target.value}})} onInvalid={e=>{e.preventDefault();setInvalid(true);trigger.current?.focus()}}>{children}</select>
  <button ref={trigger} type="button" role="combobox" aria-label={rest['aria-label']} aria-labelledby={rest['aria-labelledby']} aria-required={required||undefined} aria-invalid={invalid||undefined} aria-describedby={invalid?`${id}-error`:rest['aria-describedby']} aria-controls={`${id}-list`} aria-expanded={open} aria-haspopup="listbox" aria-activedescendant={open?`${id}-${active}`:undefined} disabled={disabled} className="select-trigger" onClick={()=>open?close():expand()} onKeyDown={keydown}><span>{selected?.label||'请选择'}</span><Icon kind="arrow"/></button>
  {invalid&&<span className="select-error" id={`${id}-error`} role="alert">请选择一项后继续。</span>}
  {open&&createPortal(<div ref={list} id={`${id}-list`} role="listbox" aria-label={rest['aria-label']||'选项'} className="select-popover" style={position}>{options.map((o,index)=><div key={o.value} id={`${id}-${index}`} role="option" aria-selected={o.value===String(value??'')} aria-disabled={o.disabled||undefined} data-index={index} className={`select-option ${index===active?'focused':''}`} onPointerMove={()=>!o.disabled&&setActive(index)} onMouseDown={e=>e.preventDefault()} onClick={()=>choose(index)}><span>{o.label}</span>{o.value===String(value??'')&&<Icon kind="check"/>}</div>)}</div>,document.body)}
 </span>;
}
