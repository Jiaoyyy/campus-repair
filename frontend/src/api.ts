export type User={id:number;org_id:number;username:string;display_name:string;role:'requester'|'technician'|'admin'};
export type Order={id:number;number:string;org_id:number;requester_id:number;category:string;location:string;description:string;status:string;assignee_id:number|null;version:number;created_at:string;updated_at:string;requester_name:string;assignee_name:string};
export type Detail={order:Order;events:{id:number;action:string;from_status:string;to_status:string;note:string;actor_name:string;created_at:string}[];records:{id:number;measures:string;result:string;actor_name:string;created_at:string}[]};
export type Page={items:Order[];total:number;page:number;page_size:number};
export const tokenKey='campus-repair-token';
export async function api<T>(path:string,options:RequestInit={}):Promise<T>{
 const headers=new Headers(options.headers);const token=sessionStorage.getItem(tokenKey);if(token)headers.set('Authorization',`Bearer ${token}`);if(options.body)headers.set('Content-Type','application/json');
 let res:Response;try{res=await fetch(path,{...options,headers})}catch{throw new Error('网络连接失败，请重试。提交报修时重试不会重复创建工单。')}
 const data=await res.json().catch(()=>({error:{message:'服务暂不可用，请稍后重试'}}));
 if(!res.ok){if(res.status===401&&token){sessionStorage.removeItem(tokenKey);window.dispatchEvent(new Event('session-expired'))}throw new Error(data.error?.message||'请求失败')}
 return data as T;
}
export const statusNames:Record<string,string>={submitted:'待审核',needs_info:'待补充',unassigned:'待派单',assigned:'待接单',in_progress:'处理中',awaiting_confirmation:'待确认',closed:'已关闭',rejected:'已驳回',cancelled:'已取消'};
export const categoryNames:Record<string,string>={lighting:'照明',electrical:'电器',plumbing:'给排水'};
export const roleNames={requester:'报修人',technician:'维修人员',admin:'后勤管理员'};
export const actionNames:Record<string,string>={create:'提交报修',review:'审核',resubmit:'补充并重新提交',assign:'派单',reassign:'改派',accept:'接单',records:'记录处理进展',complete:'提交维修结果',confirm:'确认问题已解决',reopen:'申请重新处理',cancel:'取消报修'};
export function time(v:string){return new Date(v).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false})}
