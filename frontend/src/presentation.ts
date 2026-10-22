import type {User,Order} from './api';
export type Queue='all'|'action'|'active'|'finished';
export type Summary={total:number;action:number;active:number;finished:number;statuses:Record<string,number>};
export const queueNames:Record<Queue,string>={all:'全部工单',action:'待我处理',active:'未结束',finished:'已结束'};
export function primaryAction(u:User,o:Order){
 if(u.role==='admin')return o.status==='submitted'?'review':o.status==='unassigned'?'assign':'';
 if(u.role==='technician')return o.status==='assigned'?'accept':o.status==='in_progress'?'complete':'';
 return o.status==='needs_info'?'resubmit':o.status==='awaiting_confirmation'?'confirm':'';
}
export function statusCopy(u:User,o:Order):{title:string;description:string;owner:string}{
 const technician=o.assignee_name||'维修人员';
 switch(o.status){
 case 'submitted':return {title:u.role==='admin'?'这份报修等待审核':'报修已提交，等待审核',description:u.role==='admin'?'核对故障位置与描述后，通过、要求补充或驳回。':'后勤管理员将核对信息并安排后续处理。',owner:'后勤管理员'};
 case 'needs_info':return {title:u.role==='requester'?'请补充报修信息':'等待报修人补充信息',description:u.role==='requester'?'按下方补充要求完善信息，再提交审核。':'报修人补充后，工单会重新进入审核。',owner:o.requester_name||'报修人'};
 case 'unassigned':return {title:u.role==='admin'?'审核已通过，请安排维修人员':'等待管理员安排维修',description:u.role==='admin'?'选择负责该故障类别的人员，参考当前待处理数量。':'管理员派单后，你可以在这里查看维修人员。',owner:'后勤管理员'};
 case 'assigned':return {title:u.role==='technician'?'有一份工单等待你接单':'已派单，等待维修人员接单',description:u.role==='technician'?'确认任务信息后接单，开始维修处理。':`当前任务已指派给${technician}，接单后进入维修。`,owner:technician};
 case 'in_progress':return {title:u.role==='technician'?'记录处理情况，完成后提交结果':'维修人员正在处理',description:u.role==='technician'?'填写处理措施与结果；尚未完成时可先记录进展。':`${technician}正在处理，最新进展会显示在下方。`,owner:technician};
 case 'awaiting_confirmation':return {title:u.role==='requester'?'维修已提交结果，请确认是否解决':'维修已完成，等待报修人确认',description:u.role==='requester'?'先查看维修结果。已解决可关闭工单，仍有问题可申请重新处理。':'报修人确认后关闭；未解决则重新进入派单。',owner:o.requester_name||'报修人'};
 case 'closed':return {title:'问题已确认解决，工单已关闭',description:'报修信息与维修记录已保留，可随时查看。',owner:'已完成'};
 case 'rejected':return {title:'本次报修已驳回',description:'请查看驳回原因。如需重新报修，可提交新的工单。',owner:'已结束'};
 case 'cancelled':return {title:'本次报修已取消',description:'取消原因与已有处理记录已保留。',owner:'已结束'};
 default:return {title:'工单状态待确认',description:'请刷新进度获取最新信息。',owner:'—'};
 }
}
export const progressSteps=['审核信息','安排人员','维修处理','确认结果'];
export function progressIndex(status:string){return {submitted:0,needs_info:0,unassigned:1,assigned:2,in_progress:2,awaiting_confirmation:3,closed:4}[status]??-1}
export function auditLabel(action:string):string{const knowledge:Record<string,string>={'agent.update':'更新助手配置','conversation.create':'创建私有对话','conversation.generate':'开始生成回答','conversation.finish':'结束本轮回答','knowledge.upload':'上传知识材料','knowledge.reindex':'重建材料索引','knowledge.index':'后台建立索引','knowledge.search':'检索知识材料'};if(knowledge[action])return knowledge[action];if(action==='login')return '登录';if(action==='logout')return '退出登录';if(action==='access.denied')return '访问被拒绝';const map:Record<string,string>={create:'提交报修',review:'审核报修',resubmit:'补充报修信息',assign:'派单',reassign:'改派',accept:'接单',records:'记录处理进展',complete:'提交维修结果',confirm:'确认解决',reopen:'申请重新处理',cancel:'取消报修'};return action.startsWith('work_order.')?(map[action.slice(11)]||'工单操作'):'其他操作'}
export function successCopy(action:string,decision=''){
 if(action==='review')return {approve:'审核通过，工单已进入待派单。',request_info:'补充要求已记录，等待报修人完善信息。',reject:'报修已驳回，原因已记录。'}[decision]||'审核结果已保存。';
 const copy:Record<string,string>={assign:'派单成功，等待维修人员接单。',reassign:'维修人员已调整，改派原因已记录。',accept:'接单成功，可以开始记录维修情况。',records:'处理进展已保存。',complete:'维修结果已提交，等待报修人确认。',confirm:'确认完成，工单已关闭。',reopen:'已申请重新处理，等待管理员安排维修。',cancel:'报修已取消。',resubmit:'补充信息已提交，等待重新审核。'};
 return copy[action]||'操作已保存。';
}
