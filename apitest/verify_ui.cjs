const fs=require('fs');const path=require('path');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(__dirname,'..');
const env=Object.fromEntries(fs.readFileSync(path.join(root,process.env.TEST_ENV||'tmp/check.env'),'utf8').split('\n').filter(l=>l.includes('=')&&!l.startsWith('#')).map(l=>[l.split('=')[0],l.slice(l.indexOf('=')+1)]));
const base=`http://127.0.0.1:${env.WEB_PORT}`;
let step='launch',browser;const errors=[];const location='演示 · 图书馆阅览室 '+Date.now();
const logoutButton=page=>page.locator('button').filter({hasText:/退出/}).first();

async function login(name,key){
  const context=await browser.newContext({viewport:{width:1440,height:960}});
  const page=await context.newPage();
  page.on('pageerror',()=>errors.push('pageerror'));
  await page.goto(base);
  await page.getByLabel('账号',{exact:true}).fill(name);
  await page.getByLabel('密码',{exact:true}).fill(env[key]);
  await page.getByRole('button',{name:'登录',exact:true}).click();
  await logoutButton(page).waitFor({timeout:20000});
  return {context,page};
}
// 当前界面刷新后会停留在原路由，所以直接等工单标题，不要求先回到列表。
async function waitOrder(page,name){
  await page.getByRole('heading',{name,exact:true}).waitFor({timeout:20000});
}
async function openOrder(page,name){
  const row=page.getByRole('button',{name,exact:true}).filter({hasText:name}).first();
  await row.click();
  await waitOrder(page,name);
}
async function pickTechnician(page){
  const combo=page.getByRole('combobox',{name:'维修人员'});
  await combo.click();
  await page.getByRole('option',{name:/维修人员一/}).first().click();
}

async function main(){
  browser=await chromium.launch({channel:'chrome',headless:true});
  fs.mkdirSync(path.join(root,'tmp/ui'),{recursive:true});

  step='requester login';
  const requester=await login('requester','SEED_REQUESTER_PASSWORD');const p=requester.page;
  step='create form';
  await p.getByRole('button',{name:'新建报修',exact:true}).last().click();
  await p.getByLabel('具体位置').fill(location);
  await p.getByLabel('故障描述').fill('靠窗照明灯不亮，请安排检查。');
  await p.getByRole('button',{name:'确认提交报修'}).click();
  await waitOrder(p,location);
  step='refresh authenticated page';
  await p.reload();
  await waitOrder(p,location);
  step='administrator review';
  const admin=await login('admin','SEED_ADMIN_PASSWORD');
  await admin.page.getByRole('button',{name:'工单列表',exact:true}).first().click().catch(()=>{});
  await openOrder(admin.page,location);
  await admin.page.getByRole('button',{name:'审核通过',exact:true}).click();
  await pickTechnician(admin.page);
  await admin.page.getByRole('button',{name:'派单',exact:true}).click();
  step='technician handling';
  const tech=await login('tech','SEED_TECH_PASSWORD');
  await tech.page.getByRole('button',{name:'我的任务',exact:true}).first().click();
  await openOrder(tech.page,location);
  await tech.page.getByRole('button',{name:'接单',exact:true}).click();
  await tech.page.getByLabel('处理措施').fill('检查后更换照明灯具');
  await tech.page.getByLabel('处理结果').fill('照明恢复正常');
  await tech.page.getByRole('button',{name:'提交维修结果',exact:true}).click();
  step='requester confirmation';
  await p.getByRole('button',{name:'刷新进度'}).click();
  await p.getByRole('button',{name:'确认问题已解决',exact:true}).click();
  await p.getByRole('heading',{name:'问题已确认解决，工单已关闭',exact:true}).waitFor();
  await p.screenshot({path:path.join(root,'tmp/ui/manual-flow-desktop.png'),fullPage:true});
  step='mobile layout';
  await p.setViewportSize({width:390,height:844});
  await p.screenshot({path:path.join(root,'tmp/ui/manual-flow-mobile.png'),fullPage:true});
  const overflow=await p.evaluate(()=>document.documentElement.scrollWidth>window.innerWidth);
  if(overflow)throw new Error('mobile overflow');
  step='expired session';
  await p.evaluate(()=>sessionStorage.setItem('campus-repair-token','0'.repeat(64)));
  await p.reload();
  await p.getByRole('heading',{name:'登录工作台'}).waitFor();
  await p.getByRole('alert').waitFor();
  await p.getByLabel('账号',{exact:true}).fill('requester');
  await p.getByLabel('密码',{exact:true}).fill(env.SEED_REQUESTER_PASSWORD);
  await p.getByRole('button',{name:'登录',exact:true}).click();
  await logoutButton(p).waitFor({timeout:20000});
  step='logout';
  await p.setViewportSize({width:1440,height:960});
  await logoutButton(p).click();
  await p.getByRole('heading',{name:'登录工作台'}).waitFor();
  step='login layout';
  await p.screenshot({path:path.join(root,'tmp/ui/login-desktop.png'),fullPage:true});
  if(errors.length)throw new Error('browser errors');
  await browser.close();
  console.log('PASS: browser three-role flow, refresh keeps route, confirmation, expired session, logout and mobile layout. No credentials printed.');
}
main().catch(async err=>{
  let message=String(err.stack);
  for(const [key,value] of Object.entries(env)){if(value&&(key.includes('PASSWORD')||key.includes('SECRET')))message=message.split(value).join('[redacted]')}
  fs.writeFileSync(path.join(root,'tmp/ui-error.txt'),message,{mode:0o600});
  console.error('UI verification failed at step:',step,'(details saved locally)');
  if(browser)await browser.close();
  process.exit(1);
});
