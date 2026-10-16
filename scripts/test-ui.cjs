// UI integration checks use an in-memory Wails fixture, never real credentials/VPN.
// Usage: NODE_PATH=<playwright modules> node scripts/test-ui.cjs
const {chromium}=require('playwright');
const http=require('node:http');
const fs=require('node:fs');
const path=require('node:path');
const assert=require('node:assert/strict');
const frontend=path.resolve(__dirname,'../frontend');
const output=path.resolve(__dirname,'../build/ui-review');
let server,browser;
async function fixture(page,emptyUser=false){
  await page.addInitScript(({emptyUser})=>{
    const clone=value=>JSON.parse(JSON.stringify(value));
    const core={os:'windows',arch:'amd64',ready:true,version:'OpenConnect version v9.21',path:'C:\\app\\cores\\openconnect.exe',source:'应用内便携核心'};
    const state={settings:{server:'https://vpn.cuhk.edu.cn',group:'CUHK(SZ)',username:emptyUser?'':'student',cidrs:['10.20.0.0/16'],keepDNS:false,remember:!emptyUser,corePath:'',passwordStorage:'portable'},core,state:'disconnected',phase:'',error:'',ip:'',interface:'',pid:0,logs:[]};
    let onUpdate=()=>{};
    const calls=[];
    const emit=patch=>{Object.assign(state,patch);onUpdate(clone(state));};
    window.uiFixture={state,calls,emit,rejectNext:false};
    window.runtime={EventsOn:(name,callback)=>{onUpdate=callback;}};
    window.go={main:{App:{
      GetView:async()=>clone(state),RefreshCore:async()=>{emit({});return clone(core);},
      SaveSettings:async settings=>{state.settings=clone(settings);calls.push({method:'SaveSettings'});},
      SaveProfile:async(settings,password)=>{state.settings=clone(settings);calls.push({method:'SaveProfile',hasPassword:!!password,storage:settings.passwordStorage});},
      ForgetPassword:async()=>{state.settings.remember=false;calls.push({method:'ForgetPassword'});},
      Connect:async(settings,password)=>{calls.push({method:'Connect',hasPassword:!!password});if(window.uiFixture.rejectNext){window.uiFixture.rejectNext=false;throw Error('测试连接错误');}state.settings=clone(settings);emit({state:'connecting',phase:'core'});},
      Disconnect:async()=>{calls.push({method:'Disconnect'});emit({state:'disconnecting',phase:'disconnect'});},
      Probe:async()=>{await new Promise(resolve=>setTimeout(resolve,180));calls.push({method:'Probe'});return '1 个分流网段的路由已就绪';},
      PrepareCore:async()=>{calls.push({method:'PrepareCore'});emit({state:'preparing',phase:'core',core:{...core,ready:false}});},
      ClearLogs:async()=>{calls.push({method:'ClearLogs'});emit({logs:[]});}
    }}};
  },{emptyUser});
}
(async()=>{
  fs.mkdirSync(output,{recursive:true});
  server=http.createServer((request,response)=>{
    const route=decodeURIComponent(new URL(request.url,'http://localhost').pathname);
    const file=path.resolve(frontend,'.'+(route==='/'?'/index.html':route));
    if(!file.startsWith(frontend+path.sep)){response.writeHead(403);response.end();return;}
    fs.readFile(file,(error,data)=>{if(error){response.writeHead(404);response.end();return;}const type={'.html':'text/html; charset=utf-8','.js':'text/javascript; charset=utf-8','.css':'text/css; charset=utf-8','.png':'image/png'}[path.extname(file)]||'text/plain';response.writeHead(200,{'Content-Type':type});response.end(data);});
  });
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const origin='http://127.0.0.1:'+server.address().port;
  browser=await chromium.launch({headless:true});
  const context=await browser.newContext({viewport:{width:520,height:820}});
  const errors=[],externalRequests=[],overflow=[];
  await context.route('**/*',route=>{if(new URL(route.request().url()).origin!==origin){externalRequests.push(route.request().url());return route.abort();}return route.continue();});
  const page=await context.newPage();page.on('pageerror',error=>errors.push(error.message));
  page.on('console',message=>{if(message.type()==='error'&&!message.text().includes('favicon'))errors.push(message.text());});
  await fixture(page);await page.goto(origin);
  await page.waitForFunction(()=>!document.getElementById('connect').disabled);
  await page.screenshot({path:path.join(output,'home.png'),fullPage:true});
  const icons=await page.evaluate(async()=>{
    const urls=[...new Set([...document.querySelectorAll('.pixel-icon')].map(icon=>getComputedStyle(icon).maskImage))];
    return Promise.all(urls.map(async value=>{const url=value.match(/url\(["']?(.*?)["']?\)/)[1];const image=new Image();image.src=url;await image.decode();return image.width===16&&image.height===16;}));
  });
  assert(icons.every(Boolean),'bundled pixel icons missing');
  await page.locator('#connect').click();
  await page.waitForFunction(()=>document.getElementById('app').dataset.state==='connecting');
  await page.evaluate(()=>window.uiFixture.emit({phase:'authorize'}));
  assert(await page.locator('#stage-authorize').getAttribute('aria-current')==='step');
  await page.screenshot({path:path.join(output,'authorization.png'),fullPage:true});
  await page.waitForTimeout(3400);
  assert.equal(await page.locator('#app').getAttribute('data-state'),'connecting','UI must not invent timed success');
  await page.locator('#connect').click();
  assert.equal(await page.locator('#app').getAttribute('data-state'),'disconnecting');
  await page.evaluate(()=>window.uiFixture.emit({state:'disconnected',phase:''}));
  await page.locator('#connect').click();
  await page.evaluate(()=>window.uiFixture.emit({phase:'connect',pid:1234}));
  assert.equal(await page.locator('#stage-authorize .stage-marker').textContent(),'[ OK ]');
  await page.evaluate(()=>window.uiFixture.emit({state:'connected',phase:'connected',ip:'10.20.1.2',interface:'CUHK VPN Go',pid:1234}));
  assert.equal(await page.locator('#connect').getAttribute('aria-busy'),'false');
  assert.equal(await page.locator('#stage-connect .stage-marker').textContent(),'[ OK ]');
  await page.screenshot({path:path.join(output,'connected.png'),fullPage:true});
  await page.locator('#probe').click();await page.locator('#notice').filter({hasText:'路由已就绪'}).waitFor();
  await page.evaluate(()=>window.uiFixture.emit({logs:['<img src=x onerror=alert(1)>','normal log']}));
  await page.locator('#tab-logs').click();assert.equal(await page.locator('#log-output img').count(),0);
  await page.locator('#clear').click();await page.waitForFunction(()=>window.uiFixture.calls.some(call=>call.method==='ClearLogs'));
  await page.locator('#tab-home').click();await page.locator('#connect').click();
  await page.evaluate(()=>window.uiFixture.emit({state:'disconnected',phase:'',pid:0,ip:'',interface:''}));
  await page.locator('#manage-account').click();await page.locator('#password').fill('test-only-password');
  await page.locator('#save').click();await page.locator('#notice').filter({hasText:'密码已保存'}).waitFor();
  assert.equal(await page.locator('#password').inputValue(),'');
  assert(await page.evaluate(()=>window.uiFixture.calls.some(call=>call.method==='SaveProfile'&&call.hasPassword&&call.storage==='portable')));
  await page.locator('#forget').click();await page.locator('#notice').filter({hasText:'已清除'}).waitFor();assert.equal(await page.locator('#remember').isChecked(),false);
  await page.locator('#footer-core').click();await page.locator('#install').click();
  await page.waitForFunction(()=>document.getElementById('app').dataset.state==='preparing');assert(await page.locator('#connect').isDisabled());
  await page.emulateMedia({reducedMotion:'reduce'});
  assert.equal(await page.locator('.screen-beam').evaluate(element=>getComputedStyle(element).animationName),'none');
  await page.emulateMedia({reducedMotion:'no-preference'});
  await page.evaluate(()=>window.uiFixture.emit({state:'disconnected',phase:'',core:{...window.uiFixture.state.core,ready:true}}));
  await page.locator('#core-settings summary').click();
  await page.locator('#tab-home').click();await page.locator('#tab-home').press('ArrowRight');assert.equal(await page.locator('#tab-settings').getAttribute('aria-selected'),'true');
  for(const width of [520,400,320]){
    await page.setViewportSize({width,height:820});
    for(const tab of ['home','settings','logs']){
      await page.locator('#tab-'+tab).click();await page.waitForTimeout(220);
      const size=await page.locator('#app').evaluate(element=>({width:element.clientWidth,scroll:element.scrollWidth,outer:document.documentElement.clientWidth,outerScroll:document.documentElement.scrollWidth}));
      if(size.scroll>size.width+1||size.outerScroll>size.outer+1)overflow.push({width,tab,size});
      if(width===520)await page.screenshot({path:path.join(output,tab+'-final.png'),fullPage:true});
    }
  }
  const empty=await context.newPage();empty.on('pageerror',error=>errors.push(error.message));await fixture(empty,true);await empty.goto(origin);
  await empty.locator('#page-settings').waitFor();await empty.locator('#username').fill('new-student');await empty.locator('#password').fill('test-only-password');
  await empty.locator('#back-home').click();await empty.evaluate(()=>window.uiFixture.rejectNext=true);await empty.locator('#connect').click();await empty.locator('#error').filter({hasText:'测试连接错误'}).waitFor();assert(!(await empty.locator('#connect').isDisabled()));
  const report={errors,externalRequests,overflow,pixelIcons:icons.length,realStateDriven:true,credentialActions:true,cancelAndDisconnect:true};
  fs.writeFileSync(path.join(output,'report.json'),JSON.stringify(report,null,2));console.log(JSON.stringify(report));
  assert.equal(errors.length,0);assert.equal(externalRequests.length,0);assert.equal(overflow.length,0);
})().catch(error=>{console.error(error);process.exitCode=1;}).finally(async()=>{if(browser)await browser.close();if(server)await new Promise(resolve=>server.close(resolve));});
