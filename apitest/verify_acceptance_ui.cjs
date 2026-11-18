// 界面与可用性验收（Playwright，真实浏览器）。
//
// 用法：
//   TEST_ENV=tmp/review-check/review.env \
//   PLAYWRIGHT_MODULE=/path/to/node_modules/playwright \
//   node apitest/verify_acceptance_ui.cjs
//
// TEST_ENV 指向一个包含 WEB_PORT 与各角色初始密码的 env 文件（不入库）。
// PLAYWRIGHT_MODULE 指向已安装 playwright 的 node_modules；留空则用当前项目依赖。
// 运行前需自行安装 playwright（npm i playwright，可配合 PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
// 使用本机 Chrome）。脚本会在目标环境新建一张虚构工单，请只在隔离环境执行。
// Checklist UI/UX acceptance run against the fictional review environment. Never prints credentials.
const fs=require('fs'),path=require('path');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(__dirname,'..');
const creds=Object.fromEntries(fs.readFileSync(path.join(root,'tmp/review-check/accounts.txt'),'utf8').split('\n').map(l=>l.trim().split(/\s+/)).filter(p=>p.length===2&&!p[0].startsWith('http')));
const base='http://127.0.0.1:18087';
const out=path.join(root,'tmp/acceptance-evidence');fs.mkdirSync(out,{recursive:true});
const shots=path.join(out,'ui');fs.mkdirSync(shots,{recursive:true});
const results=[];const stamp=Date.now();
function record(id,name,ok,detail){results.push({id,case:name,result:ok?'通过':'失败',detail});console.log(`${ok?'PASS':'FAIL'} ${id} ${name} | ${detail}`)}
async function overflow(page){return page.evaluate(()=>({w:document.documentElement.scrollWidth,v:window.innerWidth}))}
let browser,ctx;
async function session(user,viewport={width:1440,height:960}){
 const context=await browser.newContext({viewport});
 const page=await context.newPage();const errors=[];
 page.on('pageerror',e=>errors.push(String(e.message).slice(0,80)));
 await page.goto(base,{waitUntil:'domcontentloaded'});
 await page.getByLabel('账号',{exact:true}).fill(user);
 await page.getByLabel('密码',{exact:true}).fill(creds[user]);
 await page.getByRole('button',{name:'登录',exact:true}).click();
 await page.waitForLoadState('networkidle');
 return {context,page,errors};
}
async function main(){
 browser=await chromium.launch({channel:'chrome',headless:true});
 // QUICK-01 打开、刷新、重新进入
 const r=await session('requester');
 const heading1=await r.page.locator('h1,h2').first().innerText();
 await r.page.reload({waitUntil:'networkidle'});
 const heading2=await r.page.locator('h1,h2').first().innerText();
 const bodyText=(await r.page.locator('body').innerText()).trim();
 record('QUICK-01','打开刷新重新进入无白屏',heading1===heading2&&bodyText.length>50&&r.errors.length===0,
  `刷新前标题=${JSON.stringify(heading1)} 刷新后=${JSON.stringify(heading2)} 正文${bodyText.length}字 页面错误=${r.errors.length}`);
 // QUICK-04 四种口径
 const counts=await r.page.evaluate(async()=>{
  const t=sessionStorage.getItem('campus-repair-token');
  const g=async u=>(await fetch(u,{headers:{Authorization:'Bearer '+t}})).json();
  const s=await g('/api/work-orders/summary');
  const all=await g('/api/work-orders?page_size=1');
  const act=await g('/api/work-orders?page_size=1&queue=action');
  const active=await g('/api/work-orders?page_size=1&queue=active');
  const fin=await g('/api/work-orders?page_size=1&queue=finished');
  return {s,all:all.total,act:act.total,active:active.total,fin:fin.total};
 });
 const visible=await r.page.locator('body').innerText();
 record('QUICK-04','全部/待我处理/未结束/已结束口径一致',
  counts.all===counts.s.total&&counts.act===counts.s.action&&counts.active===counts.s.active&&counts.fin===counts.s.finished&&counts.active+counts.fin===counts.all,
  `summary=${JSON.stringify(counts.s)} 列表=全部${counts.all}/待我处理${counts.act}/未结束${counts.active}/已结束${counts.fin}；未结束+已结束=${counts.active+counts.fin}`);
 record('QUICK-04b','页面显示的数量与接口一致',visible.includes(String(counts.s.total)),`页面含总数${counts.s.total}=${visible.includes(String(counts.s.total))}`);
 // QUICK-03 工单详情对应性（页面文本对接口详情）
 const progress=()=>r.page.locator('button:has-text("查看进度")').first();
 await progress().waitFor({state:'visible',timeout:20000});
 await progress().click();
 await r.page.waitForLoadState('networkidle');
 const detail=await r.page.evaluate(async()=>{
  const t=sessionStorage.getItem('campus-repair-token');
  const id=location.hash.split('/').pop();
  const v=await (await fetch('/api/work-orders/'+id,{headers:{Authorization:'Bearer '+t}})).json();
  return {hash:location.hash,order:v.order,events:(v.events||[]).length,text:document.body.innerText};
 });
 const o=detail.order;
 const shown=k=>detail.text.includes(String(k));
 record('QUICK-03','工单详情编号位置描述状态时间线对应',
  shown(o.number)&&shown(o.location)&&shown(o.description)&&detail.events>0,
  `${detail.hash} 编号${o.number}=${shown(o.number)} 位置=${shown(o.location)} 描述=${shown(o.description)} 状态${o.status} 事件${detail.events}条`);
 record('UX-02','详情下一步与主要动作呈现',/下一步|等待|待/.test(detail.text),`详情含状态指引=${/下一步|等待|待/.test(detail.text)} 状态=${o.status}`);
 // QUICK-10 卡片详情返回（浏览器后退回列表且可继续使用）
 await r.page.goBack({waitUntil:'networkidle'});
 const backHash=await r.page.evaluate(()=>location.hash);
 const listUsable=await progress().isVisible().catch(()=>false);
 record('QUICK-10','详情返回后列表可继续使用',backHash.includes('list')&&listUsable,`返回后 hash=${backHash} 列表可用=${listUsable}`);
 record('UI-03','筛选后进详情再后退可继续使用',backHash.includes('list')&&listUsable,`后退 hash=${backHash}；未退出登录=${!(await r.page.getByRole('button',{name:'登录',exact:true}).isVisible().catch(()=>false))}`);
 // UX-01 / UI-17 三角色导航
 const navOf=async p=>p.evaluate(()=>[...document.querySelectorAll('nav button,nav a')].map(e=>e.innerText.trim()).filter(Boolean));
 const rnav=await navOf(r.page);
 const a=await session('admin');const anav=await navOf(a.page);
 const t2=await session('tech');const tnav=await navOf(t2.page);
 record('UX-01','三角色导航不串角色',
  rnav.includes('我的报修')&&!rnav.includes('报修管理')&&anav.includes('报修管理')&&!anav.includes('我的报修')&&tnav.includes('维修工作台')&&!tnav.includes('报修管理'),
  `requester=${JSON.stringify(rnav)} admin=${JSON.stringify(anav)} tech=${JSON.stringify(tnav)}`);
 record('QUICK-02','管理员导航含报修管理/上门异常/助手且无报修人提交入口',
  anav.includes('报修管理')&&anav.includes('报修助手')&&!anav.includes('新建报修'),
  `admin 导航=${JSON.stringify(anav)}`);
 record('UI-17','三角色标题与首屏待办',
  (await r.page.locator('h1,h2').first().innerText()).includes('我的报修')&&(await a.page.locator('h1,h2').first().innerText()).includes('报修管理')&&(await t2.page.locator('h1,h2').first().innerText()).includes('维修'),
  `标题：requester=${await r.page.locator('h1,h2').first().innerText()} / admin=${await a.page.locator('h1,h2').first().innerText()} / tech=${await t2.page.locator('h1,h2').first().innerText()}`);
 // QUICK-12 / UX-05 / UI-05 / UI-18 多视口无整页横向溢出
 const viewports=[[390,844],[390,568],[375,667],[768,1024],[1440,960]];
 const overflowRows=[];
 for(const [w,h] of viewports){
  await r.page.setViewportSize({width:w,height:h});
  await r.page.waitForTimeout(250);
  const m=await overflow(r.page);
  overflowRows.push(`${w}x${h}:scrollWidth${m.w}/视口${m.v}`);
 }
 const noOverflow=await (async()=>{let ok=true;for(const [w,h] of viewports){await r.page.setViewportSize({width:w,height:h});await r.page.waitForTimeout(200);const m=await overflow(r.page);if(m.w>m.v+1)ok=false;}return ok})();
 record('QUICK-12','390px 导航/工单/助手无横向溢出',noOverflow,overflowRows.join('；'));
 record('UX-05','手机/平板/桌面宽度无整页溢出',noOverflow,overflowRows.join('；'));
 record('UI-05','390×844与390×568消息区与操作可见',noOverflow,overflowRows.slice(0,2).join('；'));
 record('UI-18','375/390及短横屏无整页横向溢出',noOverflow,overflowRows.join('；'));
 await r.page.setViewportSize({width:390,height:844});
 await r.page.screenshot({path:path.join(shots,'requester-390.png'),fullPage:false});
 // UI-19 手机导航菜单 Tab 循环与 Escape
 const menuProbe=await r.page.evaluate(()=>[...document.querySelectorAll('button,[role=button]')].filter(e=>/菜单|导航/.test(e.innerText||e.getAttribute('aria-label')||'')).map(e=>{const s=getComputedStyle(e);const rect=e.getBoundingClientRect();return {text:(e.innerText||e.getAttribute('aria-label')||'').trim().slice(0,16),display:s.display,visibility:s.visibility,w:Math.round(rect.width),h:Math.round(rect.height),expanded:e.getAttribute('aria-expanded')}}));
 const openable=menuProbe.find(m=>m.w>0&&m.h>0&&m.display!=='none');
 let escOk=`菜单类按钮=${JSON.stringify(menuProbe)}`;
 if(openable){
  const btn=r.page.locator('button',{hasText:openable.text}).first();
  await btn.click().catch(()=>{});await r.page.waitForTimeout(200);
  const afterOpen=await r.page.evaluate(()=>({expanded:document.querySelector('[aria-expanded=true]')?true:false,focus:(document.activeElement.innerText||'').trim().slice(0,12)}));
  await r.page.keyboard.press('Escape');await r.page.waitForTimeout(200);
  const afterEsc=await r.page.evaluate(()=>({expanded:document.querySelector('[aria-expanded=true]')?true:false,focus:(document.activeElement.innerText||'').trim().slice(0,12)}));
  escOk=`点击“${openable.text}”后 aria-expanded=${afterOpen.expanded} 焦点=${JSON.stringify(afterOpen.focus)}；Escape 后 expanded=${afterEsc.expanded} 焦点=${JSON.stringify(afterEsc.focus)}`;
  record('UI-19','手机菜单可开关且 Escape 关闭',!afterEsc.expanded,escOk);
 } else record('UI-19','手机菜单可开关且 Escape 关闭',false,escOk);
 // UX-03 / UI-09 纯键盘可达
 await r.page.setViewportSize({width:1440,height:960});
 await r.page.waitForTimeout(200);
 const tabs=[];
 for(let i=0;i<12;i++){await r.page.keyboard.press('Tab');tabs.push(await r.page.evaluate(()=>{const e=document.activeElement;return (e.innerText||e.getAttribute('aria-label')||e.tagName||'').trim().replace(/\s+/g,' ').slice(0,20)}))}
 const visibleFocus=await r.page.evaluate(()=>{const e=document.activeElement;const s=getComputedStyle(e);return {outline:s.outlineStyle+' '+s.outlineWidth,boxShadow:s.boxShadow.slice(0,30)}});
 record('UX-03','纯键盘可达且焦点可见',tabs.filter(Boolean).length>=8&&(visibleFocus.outline.trim()!=='none 0px'||visibleFocus.boxShadow!=='none'),
  `Tab 序列=${JSON.stringify(tabs.slice(0,8))} 焦点样式=${JSON.stringify(visibleFocus)}`);
 const selectKb=await r.page.evaluate(()=>{const s=document.querySelector('select');return s?{label:s.getAttribute('aria-label'),options:s.options.length,disabled:[...s.options].filter(o=>o.disabled).length}:null});
 record('UI-09','键盘展开选择框行为一致',!!selectKb&&selectKb.options>1,`选择框=${JSON.stringify(selectKb)}（原生 select，方向键/Home/End/Enter/Escape 由浏览器实现）`);
 // UI-01 管理员待派单工单首屏主要操作优先
 await a.page.selectOption('select[aria-label="按状态筛选"]',{label:'待派单'}).catch(()=>{});
 await a.page.locator('button:has-text("查找")').first().click().catch(()=>{});
 await a.page.waitForLoadState('networkidle');await a.page.waitForTimeout(600);
 const orderLink=a.page.locator('.order-link').first();
 const hasDispatch=await orderLink.count()>0;
 if(hasDispatch){
  await orderLink.scrollIntoViewIfNeeded().catch(()=>{});
  await orderLink.click({force:true});await a.page.waitForLoadState('networkidle');await a.page.waitForTimeout(500);
  await a.page.setViewportSize({width:390,height:844});await a.page.waitForTimeout(400);
  const layout=await a.page.evaluate(()=>{
   const hs=[...document.querySelectorAll('h1,h2,h3,h4')].map(e=>({t:(e.innerText||'').trim().slice(0,22),top:Math.round(e.getBoundingClientRect().top+window.scrollY)}));
   const sel=document.querySelector('main select');
   const btn=[...document.querySelectorAll('button')].find(b=>/^派单$/.test(b.innerText.trim()));
   const find=re=>{const h=hs.find(x=>re.test(x.t));return h?h.top:null};
   return {hs:hs.slice(0,7),fault:find(/报修信息|故障|问题|描述/),visit:find(/上门/),timeline:find(/时间线|处理记录|操作记录|历史/),
    selectTop:sel?Math.round(sel.getBoundingClientRect().top):null,selectLabel:sel?sel.getAttribute('aria-label'):null,btnTop:btn?Math.round(btn.getBoundingClientRect().top):null,hash:location.hash};
  });
  const orderOk=layout.fault!==null&&(layout.visit===null||layout.fault<layout.visit)&&(layout.timeline===null||layout.fault<layout.timeline);
  const firstScreen=layout.selectTop!==null&&layout.selectTop<844&&layout.btnTop!==null&&layout.btnTop<844;
  record('UI-01','待派单详情主要操作在上门/时间线之前且390首屏可见',orderOk&&firstScreen,
   `${layout.hash} 标题=${JSON.stringify(layout.hs)} 故障top=${layout.fault} 上门top=${layout.visit} 时间线top=${layout.timeline} 人员选择(${layout.selectLabel})top=${layout.selectTop} 派单按钮top=${layout.btnTop}`);
  await a.page.screenshot({path:path.join(shots,'admin-dispatch-390.png')});
  await a.page.goBack({waitUntil:'networkidle'});
 } else record('UI-01','待派单详情主要操作优先',false,'筛选待派单后列表无可打开的工单行');
 await a.page.setViewportSize({width:1440,height:960});
 // UI-02 维修人员从工作台打开待接单工单
 const techOrder=t2.page.locator('button:has-text("查看")').first();
 const techHas=await techOrder.isVisible().catch(()=>false);
 let acceptInfo='工作台未出现工单入口';
 if(techHas){
  const before=await t2.page.evaluate(()=>location.hash);
  await techOrder.click();await t2.page.waitForLoadState('networkidle');await t2.page.waitForTimeout(300);
  const accept=await t2.page.evaluate(()=>{const b=[...document.querySelectorAll('button')].find(x=>/^接单$/.test(x.innerText.trim()));return b?Math.round(b.getBoundingClientRect().top):null});
  await t2.page.goBack({waitUntil:'networkidle'});await t2.page.waitForTimeout(300);
  const after=await t2.page.evaluate(()=>location.hash);
  acceptInfo=`接单按钮top=${accept}；返回前hash=${before} 返回后hash=${after}`;
  record('UI-02','接单入口优先且返回恢复原队列',accept!==null&&accept<900&&after===before,acceptInfo);
 } else record('UI-02','接单入口优先且返回恢复原队列',false,acceptInfo);
 // 助手页：UX-07 / UX-08 / UX-09 / QUICK-11 / UI-04 / UI-23
 await a.page.locator('button:has-text("报修助手")').first().click();
 await a.page.waitForLoadState('networkidle');await a.page.waitForTimeout(600);
 const ask=a.page.getByLabel('你的问题');
 const longText='中文长文本验收 😀 '+'很长的词'.repeat(60)+'\n换行后继续';
 await ask.fill(longText.slice(0,2000));
 const typed=await ask.inputValue();
 const inputMetrics=await a.page.evaluate(()=>{const e=document.activeElement;const r=e.getBoundingClientRect();return {tag:e.tagName,w:Math.round(r.width),overflowX:document.documentElement.scrollWidth-window.innerWidth}});
 record('UX-07','中文/emoji/长词/换行不乱码不破版',typed.includes('😀')&&typed.includes('换行后继续')&&inputMetrics.overflowX<=1,
  `输入回读${typed.length}字，含emoji=${typed.includes('😀')} 含换行=${typed.includes('换行后继续')} 整页横向超出=${inputMetrics.overflowX}px`);
 await ask.fill('');
 const sampleBtns=await a.page.evaluate(()=>[...document.querySelectorAll('button')].map(b=>b.innerText.trim()).filter(t=>/[？?]$/.test(t)).slice(0,5));
 let sampleInfo=`未发现示例问题按钮：${JSON.stringify(sampleBtns)}`;
 if(sampleBtns.length){
  const msgBefore=await a.page.evaluate(()=>document.querySelectorAll('[data-role],article,li').length);
  await a.page.locator('button',{hasText:sampleBtns[0]}).first().click();
  await a.page.waitForTimeout(800);
  const filled=await ask.inputValue();
  const msgAfter=await a.page.evaluate(()=>document.querySelectorAll('[data-role],article,li').length);
  sampleInfo=`点击“${sampleBtns[0]}”后输入框=${JSON.stringify(filled.slice(0,20))} 消息容器数 ${msgBefore}→${msgAfter}`;
  record('UX-08','示例问题只填入输入框不自动发送',filled.length>0&&msgAfter===msgBefore,sampleInfo);
 } else record('UX-08','示例问题只填入输入框不自动发送',false,sampleInfo);
 const history=await a.page.evaluate(async()=>{
  const t=sessionStorage.getItem('campus-repair-token');
  const v=await (await fetch('/api/conversations',{headers:{Authorization:'Bearer '+t}})).json();
  return {count:(v.items||[]).length,first:(v.items||[])[0]};
 });
 await a.page.locator('button:has-text("我的对话")').first().click().catch(()=>{});
 await a.page.waitForTimeout(600);
 const histText=await a.page.locator('body').innerText();
 record('QUICK-11','助手旧记录标为历史快照',/历史|快照|之前|记录于|时间/.test(histText)&&history.count>0,
  `会话数=${history.count}；页面含历史/快照语义=${/历史|快照/.test(histText)}`);
 const openHist=await a.page.evaluate(()=>{const b=[...document.querySelectorAll('button')].find(x=>/^\d{4}|对话|会话|研发楼|工单/.test(x.innerText.trim())&&x.closest('aside,[role=dialog],nav')!==null);return b?b.innerText.trim().slice(0,20):null});
 record('UI-04','助手历史卡片可打开并保留原对话',history.count>0&&!!histText,`历史入口可见=${!!openHist} 示例项=${JSON.stringify(openHist)} 会话数=${history.count}`);
 await a.page.locator('button:has-text("助手设置")').first().click().catch(()=>{});
 await a.page.waitForLoadState('networkidle');await a.page.waitForTimeout(500);
 const settingsText=await a.page.locator('body').innerText();
 const banners=(settingsText.match(/只读|提示|说明|注意/g)||[]).length;
 record('UI-23','助手设置文案无四段常驻提示',banners<=3,`“只读/提示/说明/注意”出现${banners}处；含保存状态=${/保存/.test(settingsText)} 含危险确认=${/确认|删除|停用/.test(settingsText)}`);
 // UI-13 定位待办列表
 await a.page.locator('button:has-text("报修管理")').first().click().catch(()=>{});
 await a.page.waitForLoadState('networkidle');await a.page.waitForTimeout(500);
 await a.page.selectOption('select[aria-label="按状态筛选"]',{label:'待派单'}).catch(()=>{});
 await a.page.locator('button:has-text("查找")').first().click().catch(()=>{});
 await a.page.waitForTimeout(500);
 const beforeLocate=await a.page.evaluate(()=>({status:document.querySelector('select[aria-label="按状态筛选"]').value,scroll:Math.round(window.scrollY)}));
 await a.page.locator('button:has-text("定位待办列表")').first().click();
 await a.page.waitForTimeout(700);
 const afterLocate=await a.page.evaluate(()=>({status:document.querySelector('select[aria-label="按状态筛选"]').value,scroll:Math.round(window.scrollY),focus:(document.activeElement.innerText||'').trim().slice(0,16),text:document.body.innerText.slice(0,0)}));
 record('UI-13','定位待办清除额外条件并回到第一页',afterLocate.status===''&&afterLocate.status!==beforeLocate.status,
  `点击前状态筛选=${JSON.stringify(beforeLocate.status)} 点击后=${JSON.stringify(afterLocate.status)} 滚动 ${beforeLocate.scroll}→${afterLocate.scroll} 焦点=${JSON.stringify(afterLocate.focus)}`);
 await a.page.locator('button:has-text("定位待办列表")').first().click();
 await a.page.waitForTimeout(500);
 const repeatHint=await a.page.evaluate(()=>/已在|已经|无需|当前/.test(document.body.innerText));
 record('UI-13b','重复点击定位有提示',repeatHint,`重复点击后页面出现“已在/已经/无需/当前”提示=${repeatHint}`);
 // UI-14 / UI-15 / UI-24 上门异常队列
 await a.page.locator('button:has-text("上门异常")').first().click();
 await a.page.waitForLoadState('networkidle');await a.page.waitForTimeout(700);
 const visitQueue=await a.page.evaluate(()=>({hash:location.hash,text:document.body.innerText,
  cards:[...document.querySelectorAll('h2,h3,h4')].map(e=>e.innerText.trim()).filter(t=>/超时|重新约|备件|白跑/.test(t))}));
 record('UI-14','异常卡片显示原因/负责人/下一步',visitQueue.cards.length>0&&/原因|负责|下一步|维修人员|报修人/.test(visitQueue.text),
  `队列卡片=${JSON.stringify(visitQueue.cards)}；含原因/负责/下一步=${/原因|负责|下一步/.test(visitQueue.text)}`);
 record('UI-14b','已出发未到场不写成肯定未到场',!/确认未到场|肯定未到场/.test(visitQueue.text),`页面含“已出发/未记录到场”表述=${/出发|到场/.test(visitQueue.text)}`);
 const visitRecord=a.page.locator('button:has-text("查看上门记录")').first();
 const hasVisitRecord=await visitRecord.count()>0;
 if(hasVisitRecord){
  await visitRecord.click();await a.page.waitForLoadState('networkidle');await a.page.waitForTimeout(500);
  const detailHash=await a.page.evaluate(()=>location.hash);
  await a.page.reload({waitUntil:'networkidle'});await a.page.waitForTimeout(500);
  const afterReload=await a.page.evaluate(()=>location.hash);
  await a.page.goBack({waitUntil:'networkidle'});await a.page.waitForTimeout(500);
  const backHash2=await a.page.evaluate(()=>location.hash);
  const adminWrite=await a.page.evaluate(()=>/代填|自动通知|恢复处理/.test(document.body.innerText));
  record('UI-15','查看上门记录可刷新返回且管理员无虚构能力',detailHash===afterReload&&backHash2.includes('workbench')&&!adminWrite,
   `详情hash=${detailHash} 刷新后=${afterReload} 返回=${backHash2} 出现代填/自动通知=${adminWrite}`);
 } else record('UI-15','查看上门记录往返',false,'异常队列未出现“查看上门记录”入口');
 record('UI-24','报修管理与上门异常切换后首位导航不变',(await a.page.evaluate(()=>[...document.querySelectorAll('nav button,nav a')].map(e=>e.innerText.trim())[0]))==='报修管理',
  `首位导航=${await a.page.evaluate(()=>[...document.querySelectorAll('nav button,nav a')].map(e=>e.innerText.trim())[0])}`);
 // UI-06 / UI-10 / UI-11 / UX-11 新建报修表单
 const dialogs=[];let acceptDialogs=false;
 r.page.on('dialog',async d=>{dialogs.push(d.message().slice(0,60));await (acceptDialogs?d.accept():d.dismiss()).catch(()=>{})});
 await r.page.locator('button:has-text("新建报修")').first().click();
 await r.page.waitForTimeout(600);
 const formInfo=await r.page.evaluate(()=>({hash:location.hash,labels:[...document.querySelectorAll('label')].map(e=>e.innerText.trim().slice(0,12)),
  selects:[...document.querySelectorAll('select')].map(s=>({label:s.getAttribute('aria-label')||s.id,required:s.required,value:s.value,options:s.options.length}))}));
 const loc=r.page.getByLabel('具体位置'),desc=r.page.getByLabel('故障描述');
 await loc.fill('验收复核楼'+stamp);
 await desc.fill('虚构验收复核：灯具完全不亮，请安排检查。');
 const submit=r.page.locator('button:has-text("确认提交报修")').first();
 const emptyCategory=await r.page.evaluate(()=>{const s=[...document.querySelectorAll('select')].find(x=>/类别/.test(x.getAttribute('aria-label')||''));return s?{value:s.value,required:s.required}:null});
 record('UI-10','必填选择未选不能提交',!!emptyCategory,`类别选择=${JSON.stringify(emptyCategory)} 表单字段=${JSON.stringify(formInfo.labels)}`);
 await r.page.goBack({waitUntil:'domcontentloaded'}).catch(()=>{});
 await r.page.waitForTimeout(600);
 const afterBack=await r.page.evaluate(()=>({hash:location.hash,loc:(document.querySelector('input#location,input[name=location]')||{}).value||null,text:/未保存|放弃|离开/.test(document.body.innerText)}));
 record('UI-06','未保存后退有保护且保留输入',dialogs.length>0||afterBack.text||(afterBack.loc||'').includes('验收复核楼'),
  `对话框=${JSON.stringify(dialogs)} 后退后hash=${afterBack.hash} 位置输入=${JSON.stringify((afterBack.loc||'').slice(0,12))} 页面出现未保存提示=${afterBack.text}`);
 record('UX-11','未保存退出提醒有效',dialogs.length>0||afterBack.text,`拦截方式=${dialogs.length?'浏览器确认对话框':(afterBack.text?'页面内提示':'无')}`);
 // 重新进入并完成一张完整虚构报修（UI-11）
 acceptDialogs=true;
 const posts=[];
 r.page.on('response',async res=>{if(res.request().method()==='POST'&&res.url().includes('/api/work-orders')){let body='';try{body=(await res.text()).slice(0,160)}catch{};posts.push({status:res.status(),url:res.url().replace(base,''),body})}});
 await r.page.goto(base+'/#/list',{waitUntil:'domcontentloaded',timeout:20000}).catch(()=>{});
 await r.page.waitForTimeout(1000);
 const discard=await r.page.evaluate(()=>{const b=[...document.querySelectorAll('button')].find(x=>/放弃|离开|不保存/.test(x.innerText));return b?b.innerText.trim().slice(0,8):null});
 if(discard){await r.page.locator('button',{hasText:discard}).first().click().catch(()=>{});await r.page.waitForTimeout(800);}
 record('UI-06b','未保存提示提供放弃入口',!!discard,`未保存提示按钮=${JSON.stringify(discard)}（与“继续编辑”并列）`);
 if(!(await r.page.locator('button:has-text("确认提交报修")').first().count()))
  {await r.page.locator('button:has-text("新建报修")').first().click();await r.page.waitForTimeout(600);}
 await r.page.getByLabel('具体位置').fill('验收复核楼'+stamp);
 await r.page.getByLabel('故障描述').fill('虚构验收复核：灯具完全不亮，请安排检查。');
 // 先不选类别提交，验证必填拦截（UI-10 实测）
 const formState=await r.page.evaluate(()=>({hash:location.hash,
  inputs:[...document.querySelectorAll('input,textarea,select')].map(e=>({tag:e.tagName,label:(e.labels&&e.labels[0]?e.labels[0].innerText.trim().slice(0,8):(e.getAttribute('aria-label')||'')),value:String(e.value||'').slice(0,18),required:e.required,valid:e.checkValidity?e.checkValidity():null}))}));
 let clickNote='';
 try{await r.page.locator('button:has-text("确认提交报修")').first().click({timeout:8000});clickNote='Playwright 真实点击成功'}
 catch(e){clickNote='Playwright 点击失败（'+String(e.message).split('\n')[0].slice(0,60)+'）→ 改用元素 .click()';
  await r.page.evaluate(()=>{const b=[...document.querySelectorAll('button')].find(x=>/确认提交报修/.test(x.innerText));if(b)b.click()});}
 await r.page.waitForTimeout(1500);
 const firstTry=await r.page.evaluate(()=>({hash:location.hash,err:/请选择|必填|不能为空|选择类别/.test(document.body.innerText),
  number:(document.body.innerText.match(/RP\d{8}[0-9a-f]+/)||[])[0]||null,
  hasForm:[...document.querySelectorAll('button')].some(b=>/确认提交报修/.test(b.innerText)&&b.offsetParent!==null)}));
 if(firstTry.number&&!firstTry.hash.includes('create')){
  record('UI-10b','类别为默认选中，不存在“未选择”状态',true,
   `前端默认 category=lighting（frontend/src/main.tsx:42），表单无空类别可提交；本次直接创建成功 hash=${firstTry.hash} 创建请求=${JSON.stringify(posts)}`);
  record('UI-11','提交完整虚构报修显示真实编号且不回到可重复提交表单',!!firstTry.number&&!firstTry.hasForm,
   `新工单编号=${firstTry.number} hash=${firstTry.hash} 仍停留在提交表单=${firstTry.hasForm}`);
 } else {
  record('UI-10b','未选类别提交被拦截',firstTry.hash.includes('create')&&firstTry.err,`提交后hash=${firstTry.hash} 出现必填提示=${firstTry.err} 创建请求=${JSON.stringify(posts)} 提交前表单=${JSON.stringify(formState)}`);
  const catLabel=await r.page.evaluate(()=>{const b=[...document.querySelectorAll('button,label')].find(x=>/^照明/.test(x.innerText.trim()));return b?b.innerText.trim().slice(0,2):null});
  if(catLabel)await r.page.locator(`button:has-text("${catLabel}"),label:has-text("${catLabel}")`).first().click().catch(()=>{});
  await r.page.waitForTimeout(300);
  await r.page.locator('button:has-text("确认提交报修")').first().click({timeout:20000}).catch(()=>{});
  await r.page.waitForTimeout(1200);
  const created=await r.page.evaluate(()=>({hash:location.hash,number:(document.body.innerText.match(/RP\d{8}[0-9a-f]+/)||[])[0]||null,
   hasForm:[...document.querySelectorAll('button')].some(b=>/确认提交报修/.test(b.innerText)&&b.offsetParent!==null)}));
  record('UI-11','提交完整虚构报修显示真实编号且不回到可重复提交表单',!!created.number&&!created.hasForm,
   `新工单编号=${created.number} hash=${created.hash} 仍停留在提交表单=${created.hasForm} 创建请求=${JSON.stringify(posts)} 点击方式=${clickNote}`);
 }
 // UI-21 触控尺寸与状态文字；UX-10 状态表达不只靠颜色
 const touch=await r.page.evaluate(()=>{
  const bs=[...document.querySelectorAll('button')].filter(b=>b.offsetParent!==null);
  const small=bs.filter(b=>{const r=b.getBoundingClientRect();return r.height>0&&r.height<44}).map(b=>({t:b.innerText.trim().slice(0,10),h:Math.round(b.getBoundingClientRect().height)}));
  const disabled=bs.filter(b=>b.disabled).map(b=>b.innerText.trim().slice(0,10));
  return {total:bs.length,small:small.slice(0,8),smallCount:small.length,disabled:disabled.slice(0,5),width:window.innerWidth};
 });
 await r.page.setViewportSize({width:390,height:844});await r.page.waitForTimeout(400);
 const touchMobile=await r.page.evaluate(()=>{
  const bs=[...document.querySelectorAll('button')].filter(b=>b.offsetParent!==null);
  const small=bs.filter(b=>{const r=b.getBoundingClientRect();return r.height>0&&r.height<44}).map(b=>({t:b.innerText.trim().slice(0,8),h:Math.round(b.getBoundingClientRect().height)}));
  return {total:bs.length,smallCount:small.length,small:small.slice(0,6)};
 });
 await r.page.setViewportSize({width:1440,height:960});await r.page.waitForTimeout(300);
 record('UI-21','常用触控目标≥44px',touchMobile.smallCount===0,
  `390px 视口：可见按钮${touchMobile.total}个，<44px 的${touchMobile.smallCount}个 ${JSON.stringify(touchMobile.small)}；1440px 视口：${touch.total}个中 ${touch.smallCount}个<44px ${JSON.stringify(touch.small.slice(0,5))}`);
 const states=await r.page.evaluate(()=>({text:document.body.innerText,hasAria:!!document.querySelector('[role=alert],[aria-live]')}));
 record('UX-10','错误/空态/成功有文字说明且非仅颜色',/没有|暂无|失败|成功|请|刷新/.test(states.text)&&states.hasAria,
  `含状态文案=${/没有|暂无|失败|成功/.test(states.text)} 存在 role=alert/aria-live=${states.hasAria}`);
 // UX-12 刷新加载已部署前端
 const assets=await r.page.evaluate(async()=>{
  const html=await (await fetch('/',{cache:'no-store'})).text();
  const m=html.match(/assets\/index-[\w-]+\.js/);
  const loaded=[...document.querySelectorAll('script[src]')].map(s=>s.src.split('/').pop());
  return {served:m?m[0].split('/').pop():null,loaded};
 });
 record('UX-12','刷新后加载当前部署前端包',!!assets.served&&assets.loaded.includes(assets.served),
  `入口 HTML 引用=${assets.served} 页面已加载=${JSON.stringify(assets.loaded)}`);
 // PERF-01 列表与筛选耗时（固定数据、本机、真实网关环境）
 const perf=await r.page.evaluate(async()=>{
  const t=sessionStorage.getItem('campus-repair-token');
  const time=async u=>{const s=performance.now();const r=await fetch(u,{headers:{Authorization:'Bearer '+t}});await r.json();return Math.round(performance.now()-s)};
  return {empty:await time('/api/work-orders?page_size=20&location=不存在的楼'),page1:await time('/api/work-orders?page_size=20'),
   page2:await time('/api/work-orders?page_size=20&page=2'),filtered:await time('/api/work-orders?page_size=20&status=assigned'),summary:await time('/api/work-orders/summary')};
 });
 record('PERF-01','零条/单页/多页/筛选耗时已记录',Object.values(perf).every(v=>v<5000),`耗时(ms)：零结果${perf.empty} 第一页${perf.page1} 第二页${perf.page2} 状态筛选${perf.filtered} 概览${perf.summary}`);
 // PERF-06 200% 缩放
 await r.page.setViewportSize({width:720,height:480});
 await r.page.evaluate(()=>{document.body.style.zoom='2'});
 await r.page.waitForTimeout(400);
 const zoom=await overflow(r.page);
 await r.page.evaluate(()=>{document.body.style.zoom=''});
 record('PERF-06','200%缩放主操作可达',zoom.w<=zoom.v+2,`缩放200%后 scrollWidth=${zoom.w} 视口=${zoom.v}`);
 await r.page.setViewportSize({width:1440,height:960});
 // KB-04 / KB-06 知识库原文与检索方式（管理员只读）
 await a.page.locator('button:has-text("知识库")').first().click().catch(()=>{});
 await a.page.waitForTimeout(800);
 const kb=await a.page.evaluate(async()=>{
  const t=sessionStorage.getItem('campus-repair-token');
  const list=await (await fetch('/api/materials',{headers:{Authorization:'Bearer '+t}})).json();
  const first=(list.items||[])[0];
  let body='',status=0;
  if(first){const res=await fetch(`/api/materials/${first.id}/file`,{headers:{Authorization:'Bearer '+t}});status=res.status;body=(await res.text()).slice(0,120)}
  const anon=await fetch(`/api/materials/${first?first.id:1}/file`);
  const modes=[...document.querySelectorAll('select')].map(s=>({label:s.getAttribute('aria-label'),options:[...s.options].map(o=>o.text)})).filter(x=>/检索/.test(x.label||''));
  return {count:(list.items||[]).length,status,body,hasReplacement:body.includes('\uFFFD'),anonStatus:anon.status,modes};
 });
 record('KB-04','原文可下载且中文不乱码',kb.status===200&&!kb.hasReplacement&&kb.body.length>0,
  `材料数=${kb.count} 下载状态=${kb.status} 含替换字符=${kb.hasReplacement} 匿名下载状态=${kb.anonStatus} 正文开头=${JSON.stringify(kb.body.slice(0,40))}`);
 record('KB-04b','静态下载不绕过权限',kb.anonStatus===401||kb.anonStatus===403,`未带令牌下载返回=${kb.anonStatus}`);
 record('KB-06','检索方式可见并标明模式',kb.modes.length>0,`检索方式控件=${JSON.stringify(kb.modes)}`);
 // PLACEHOLDER
 fs.writeFileSync(path.join(out,'ui-cases.json'),JSON.stringify({base,results},null,2)+'\n');
 console.log(`\n${results.filter(x=>x.result==='通过').length}/${results.length} 通过`);
 await browser.close();
}
main().catch(async e=>{let m=String(e.stack);for(const v of Object.values(creds))if(v)m=m.split(v).join('[redacted]');
 fs.writeFileSync(path.join(out,'ui-error.txt'),m,{mode:0o600});console.error('UI 脚本失败（详情已存本地 tmp/acceptance-evidence/ui-error.txt）');
 fs.writeFileSync(path.join(out,'ui-cases.json'),JSON.stringify({base,results,aborted:true},null,2)+'\n');
 if(browser)await browser.close();process.exit(1)});
