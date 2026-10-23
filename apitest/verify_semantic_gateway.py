"""Real native-tool planning, checked against isolated fictional orders and MCP audit."""
import datetime, json, time, uuid
from pathlib import Path
from verify_gateway import Verification, ROOT

class SemanticVerification(Verification):
    def verify(self):
        self.dc('up','-d','--no-build')
        for _ in range(120):
            try:
                self.request('GET','/health/ready');break
            except (RuntimeError,OSError):time.sleep(.5)
        tokens={}
        for name,key in [('admin','SEED_ADMIN_PASSWORD'),('requester','SEED_REQUESTER_PASSWORD'),('requester2','SEED_REQUESTER2_PASSWORD'),('other','SEED_OTHER_PASSWORD'),('tech','SEED_TECH_PASSWORD')]:
            tokens[name]=self.request('POST','/api/login',body={'username':name,'password':self.passwords[key]})['access_token']
        agents={name:self.request('GET','/api/agents',token)['items'][0]['id'] for name,token in tokens.items()}
        def create(name,location,description):
            return self.request('POST','/api/work-orders',tokens[name],{'category':'plumbing' if '漏水' in description else 'lighting','location':location,'description':description},201,{'Idempotency-Key':uuid.uuid4().hex})['order']
        a=create('requester','研发楼201','灯不亮，虚构验收')
        b=create('requester','3号楼走廊','水管漏水，虚构验收')
        c=create('requester','3号楼202','灯闪烁，虚构验收')
        create('other','研发楼201','其他组织测试漏水，不得可见')
        before=self.request('GET','/api/work-orders/summary',tokens['admin'])
        records=[]
        self.evidence["semantic_queries"]=records
        def query(name,text,cid=None):
            cid,meta,done=self.turn(agents[name],tokens[name],text,cid)
            result=done.get('business',{})
            self.check(result.get('kind')=='orders','实际成功工具结果：'+text)
            audit=self.request('GET','/api/audit?page_size=100',tokens['admin'])['items']
            matches=[x for x in audit if x['action']=='mcp.call' and x['request_id']==meta['request_id'] and x['outcome']=='success']
            self.check(len(matches)==1,'本轮恰好一次真实MCP成功调用')
            records.append({'question':text,'request_id':meta['request_id'],'result':result,'answer':done['answer']})
            return cid,result,done
        cid,r,d=query('admin','研发楼最近有什么故障')
        self.check(r['query'].get('location')=='研发楼' and r['query'].get('period')=='recent' and r['total']==1,'用户原话提取地点及近7天，跨组织排除')
        self.check(r['items'][0]['id']==a['id'] and '灯不亮' in d['answer'] and '至' in r['time_basis'],'返回真实故障描述及具体日期')
        cid,r,_=query('admin','那3号楼呢',cid)
        self.check(r['query'].get('location')=='3号楼' and r['query'].get('period')=='recent' and r['total']==2,'换地点保留时间条件')
        cid,r,_=query('admin','只看漏水',cid)
        self.check(r['query'].get('keyword')=='漏水' and r['total']==1 and r['items'][0]['id']==b['id'],'现象关键词不扩展成整个类别')
        cid,r,_=query('admin','再往前一周',cid)
        self.check(r['query'].get('date_from') and r['query'].get('date_to') and r['total']==0 and r['query'].get('keyword')=='漏水','往前一周生成明确日期并保留地点现象')
        _,r,_=query('admin','查一下不存在的测试楼过去一周有哪些故障')
        self.check(r['total']==0 and not r['items'] and r['query'].get('location'),'真实空结果有实际地点查询证据')
        _,r,_=query('requester2','研发楼最近有什么故障')
        self.check(r['total']==0,'同组织其他报修人不能查询他人工单')
        _,r,_=query('tech','研发楼最近有什么故障')
        self.check(r['total']==0,'维修人员仅当前指派范围')
        _,r,_=query('admin','研发楼上周有哪些报修')
        self.check(r['query'].get('period')=='last_week' and r['total']==0,'上周完整北京时间范围')
        _,r,_=query('admin','查位置包含百分号%的工单')
        self.check(r['query'].get('location') and '%' in r['query']['location'] and r['total']==0,'位置百分号按字面匹配')
        # Ordinary conversation clears a recent query snapshot.
        self.turn(agents['admin'],tokens['admin'],'你好',cid)
        _,_,plain=self.turn(agents['admin'],tokens['admin'],'报修需要填什么信息',cid)
        self.check(not plain.get('business'),'流程问答不误查工单')
        self.check(before==self.request('GET','/api/work-orders/summary',tokens['admin']),'全部对话不改变业务数量')
        self.evidence['semantic_queries']=records
        self.verify_conversational(agents['requester'],tokens)

if __name__=='__main__':
    v=SemanticVerification(18085)
    try:
        v.verify();v.evidence['result']='passed'
    finally:
        v.dc('down')
        path=ROOT/'tmp'/(v.project+'-semantic.json')
        path.write_text(json.dumps(v.evidence,ensure_ascii=False,indent=2)+'\n')
    print(f'真实语义查询验证通过，证据：{path}')
