import { api } from './api';

export type DispatchPerson = {id:number;display_name:string;assigned_count:number;in_progress_count:number};
export type DispatchPeople = Record<string,DispatchPerson[]>;

export async function loadDispatchPeople(categories:string[]):Promise<DispatchPeople> {
 const lists=await Promise.all([...new Set(categories)].map(async category=>{
  const {items}=await api<{items:DispatchPerson[]}>('/api/technicians?category='+encodeURIComponent(category));
  return [category,items] as const;
 }));
 return Object.fromEntries(lists);
}
