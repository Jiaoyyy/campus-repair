import type {Order} from './api';
import type {VisitContext} from './VisitPanel';
export const visitQueueCopy:Record<string,{label:string;meaning:string}>={
 overdue:{label:'上门超时',meaning:'已超过约定结束时间，系统尚未记录到场。请当前维修人员核实情况、更新记录或重新约定时间。'},
 reschedule:{label:'需要重新约时间',meaning:'曾记录到场无人，目前还没有新预约。先由报修人提供有效的新时间，再由维修人员确认。'},
 parts:{label:'等待备件',meaning:'维修人员已记录缺少备件，维修暂未恢复。由当前维修人员跟进备件，备件到位后填写恢复处理说明。'},
};
export type VisitIssue={title:string;problem:string;owner:string;role:string;next:string};
export function visitIssue(o:Order,c:VisitContext,asOf:string):VisitIssue|null{
 if(o.status!=='in_progress')return null;
 const now=Date.parse(asOf),tech=o.assignee_name||'当前维修人员',requester=o.requester_name||'本人报修人';
 if(c.exception==='waiting_parts'){
  const late=!!c.expected_resume_at&&Date.parse(c.expected_resume_at)<now;
  return {title:late?'已过预计恢复时间':'维修因缺备件暂停',problem:late?'维修人员填写的预计恢复时间已过，系统尚未记录恢复处理。':'维修人员记录了缺少备件，当前仍在等待恢复。',owner:tech,role:'维修人员',next:'核对备件到位情况，填写说明后恢复处理。'+(c.revisit_required?'恢复后仍需重新预约并记录到场。':'')};
 }
 if(c.exception==='no_show'&&!c.visit){
  const needsTime=c.needs_availability||!c.availability_start||!c.availability_end||Date.parse(c.availability_end)<=now+30*60_000;
  return {title:needsTime?'等待报修人提供新时间':'等待维修人员确认新预约',problem:'此前到场时记录了报修人不在，当前还没有新的上门预约。',owner:needsTime?requester:tech,role:needsTime?'报修人':'维修人员',next:needsTime?'报修人填写新的方便上门时间，再由维修人员确认预约。':'在报修人提供的新时间范围内确认预约，随后记录出发和到场。'};
 }
 if(c.visit&&['scheduled','departed'].includes(c.visit.stage)&&Date.parse(c.visit.end)<now){
  const departed=c.visit.stage==='departed';
  return {title:departed?'已记录出发，尚未记录到场':'约定时间已过，尚未记录到场',problem:'已超过约定上门结束时间；页面以已记录的进度为准。',owner:tech,role:'维修人员',next:departed?'核实现场情况；已到场则记录到场，遇到异常则说明原因。':'核实未记录到场的原因，并重新确认未来上门时间；原时间失效时由报修人补充新时间。'};
 }
 return null;
}
