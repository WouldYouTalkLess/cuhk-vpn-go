/* All assets are bundled; only Go/Wails owns credentials and the VPN process. */
const $ = id => document.getElementById(id);
const api = window.go?.main?.App;
const reducedMotion = matchMedia('(prefers-reduced-motion: reduce)');
const pending = new Set();
const states = {disconnected:'尚未连接',connecting:'正在连接…',connected:'校园网络已连接',disconnecting:'正在断开…',preparing:'正在准备核心…'};
let view, initialized=false, activePage='home', lastLogs='', uiError='';

function animate(element,frames,duration=200) {
  if(!reducedMotion.matches && element.animate)element.animate(frames,{duration,easing:'cubic-bezier(.2,.8,.2,1)'});
}
function showPage(page,{focus=false}={}) {
  const changed=activePage!==page;activePage=page;
  document.querySelectorAll('[data-page]').forEach(button=>{const selected=button.dataset.page===page;button.setAttribute('aria-selected',String(selected));button.tabIndex=selected?0:-1;});
  document.querySelectorAll('[role=tabpanel]').forEach(panel=>panel.hidden=panel.id!=='page-'+page);
  if(changed)animate($('page-'+page),[{opacity:.45,transform:'translateY(6px)'},{opacity:1,transform:'translateY(0)'}]);
  if(focus)$('tab-'+page).focus();
  if(page==='logs' && $('follow').checked)$('log-output').scrollTop=$('log-output').scrollHeight;
}
document.querySelectorAll('[data-page]').forEach(button=>{
  button.addEventListener('click',()=>showPage(button.dataset.page));
  button.addEventListener('keydown',event=>{
    const pages=['home','settings','logs'];let index=pages.indexOf(button.dataset.page);
    if(event.key==='ArrowRight')index=(index+1)%pages.length;
    else if(event.key==='ArrowLeft')index=(index+pages.length-1)%pages.length;
    else if(event.key==='Home')index=0;else if(event.key==='End')index=pages.length-1;else return;
    event.preventDefault();showPage(pages[index],{focus:true});
  });
});
function notice(text) { $('notice').textContent=text||'';$('notice').hidden=!text; }
function showError() {const text=uiError||view?.error||'';$('error').textContent=text;$('error').hidden=!text;}
function settings() {
  return {server:$('server').value.trim(),group:$('group').value.trim(),username:$('username').value.trim(),cidrs:$('cidrs').value.split(/[\n,]+/).map(x=>x.trim()).filter(Boolean),keepDNS:$('keep-dns').checked,corePath:$('core-custom').value.trim(),remember:$('remember').checked,passwordStorage:$('password-storage').value};
}
function passwordHint() {
  const local=$('password-storage').value==='portable';
  $('password-hint').textContent=local?'请妥善保管软件目录，持有完整目录的人也能读取密码。':'密码由当前用户的系统凭据存储保护，换电脑后重新输入。';
  $('password').placeholder=$('remember').checked?'留空使用已保存的密码':'输入 VPN 密码';
  renderSummary();
}
function renderSummary() {
  if(!initialized)return;
  const s=settings();
  $('account-name').textContent=s.username||'尚未设置学校账号';
  $('password-summary').textContent=s.remember?'记住密码已开启 · '+(s.passwordStorage==='portable'?'随软件目录保存':'系统凭据存储'):'连接时输入密码';
  $('route-summary').textContent=s.cidrs.length<=2?s.cidrs.join(' · ')||'尚未配置网段':s.cidrs[0]+' 等 '+s.cidrs.length+' 个网段';
  $('connection-target').textContent=(s.group||'ANYCONNECT')+' / VPN';
}
function setStage(id,done,current) {
  const element=$(id);element.classList.toggle('is-done',done);element.classList.toggle('is-current',!done&&current);
  element.querySelector('.stage-marker').textContent=done?'[ OK ]':current?'[ .. ]':'[ -- ]';
  element.querySelector('.stage-state').textContent=done?'已完成':current?'正在进行':'等待';
  if(!done&&current)element.setAttribute('aria-current','step');else element.removeAttribute('aria-current');
}
function label(id,text) { const button=$(id);const element=button.querySelector('.button-label');if(element)element.textContent=text; }
function render(v) {
  const previousState=view?.state;view=v;
  if(!initialized) {
    const s=v.settings;
    $('server').value=s.server;$('group').value=s.group;$('username').value=s.username;
    $('cidrs').value=(s.cidrs||[]).join('\n');$('keep-dns').checked=s.keepDNS;
    $('remember').checked=s.remember;$('password-storage').value=s.passwordStorage||'system';$('core-custom').value=s.corePath;
    initialized=true;passwordHint();if(!s.username)showPage('settings');
  }
  const c=v.core||{}, state=v.state||'disconnected', phase=v.phase||'', running=state!=='disconnected';
  $('app').dataset.state=state;$('app').dataset.coreReady=String(!!c.ready);
  $('status').textContent=states[state]||state;
  const detail=state==='connected'?(v.ip?'VPN '+v.ip+(v.interface?' · '+v.interface:''):'校园连接已建立'):
    state==='preparing'?'正在校验、下载并准备便携核心…':state==='disconnecting'?'正在清理隧道与分流路由…':
    state==='connecting'?(phase==='authorize'?'请完成系统管理员授权…':phase==='core'?'正在检查便携核心…':'正在认证并建立校园隧道…'):'校园网段通过 VPN，其余流量直连';
  $('connection-detail').textContent=detail;
  $('header-state').textContent=state==='connected'?'分流已启用':state==='connecting'||state==='disconnecting'?'连接处理中':'校园分流';
  $('core-short-status').textContent=c.ready?'CORE READY':state==='preparing'?'PREPARING':'CORE PENDING';
  const authorized=phase==='connect'||phase==='connected'||phase==='disconnect'||state==='connected'||(state==='disconnecting'&&!!v.pid);
  setStage('stage-core',!!c.ready,state==='preparing'||(state==='connecting'&&phase==='core'));
  setStage('stage-authorize',authorized,state==='connecting'&&phase==='authorize');
  setStage('stage-connect',state==='connected',state==='connecting'&&phase==='connect'||state==='disconnecting');
  label('connect',state==='preparing'?'正在准备核心…':state==='connecting'?'取消连接':state==='disconnecting'?'重试断开':state==='connected'?'断开连接':'连接 VPN');
  $('connect').setAttribute('aria-busy',String(state==='preparing'||state==='connecting'||state==='disconnecting'));
  $('connect').disabled=!api||state==='preparing'||pending.has('connect')||[...pending].some(name=>name!=='probe');
  $('probe').disabled=!api||state!=='connected'||pending.has('probe');
  for(const id of ['save','install','refresh','forget','server','group','username','password','show-password','remember','password-storage','cidrs','keep-dns','core-custom'])$(id).disabled=running||pending.size>0||!api;
  $('install').setAttribute('aria-busy',String(state==='preparing'||pending.has('install')));
  if(!pending.has('install'))label('install',state==='preparing'?'正在准备…':'下载 / 修复核心');
  $('core-badge').textContent=c.ready?'已就绪':state==='preparing'?'准备中':'待准备';$('core-badge').classList.toggle('ready',!!c.ready);
  $('core-version').textContent=c.version||c.error||'正在检查便携核心…';$('core-path').textContent=c.path||'';$('install-method').textContent=c.source||'按当前系统和架构自动准备便携核心。';
  const os={windows:'Windows',darwin:'macOS',linux:'Linux'}[c.os]||c.os||'';$('platform').textContent=[os,c.arch].filter(Boolean).join(' · ');
  $('footer-version').textContent=c.version?c.version.replace(/^OpenConnect version\s*/i,'OpenConnect '):'OpenConnect';
  $('footer-state').textContent=c.ready?'核心就绪':state==='preparing'?'正在准备':'核心待准备';
  $('pid').textContent=v.pid?'OpenConnect PID '+v.pid:'';
  const logs=(v.logs||[]).join('\n');
  if(logs!==lastLogs) {
    const output=$('log-output'),scroll=output.scrollTop;
    if(logs)output.textContent=logs;
    else {output.replaceChildren();const empty=document.createElement('span');empty.className='log-empty';empty.textContent='便携核心与连接活动将显示在这里。';output.append(empty);}
    output.scrollTop=$('follow').checked?output.scrollHeight:scroll;lastLogs=logs;
  }
  $('log-count').textContent=(v.logs||[]).length||'';renderSummary();showError();
  if(previousState&&previousState!==state&&state==='connected')animate($('status'),[{opacity:.65,transform:'translateY(3px)'},{opacity:1,transform:'translateY(0)'}],300);
}
async function action(name,fn,{button,busyLabel}={}) {
  if(pending.has(name))return;
  uiError='';showError();pending.add(name);
  const element=button?$(button):null,oldLabel=element?.querySelector('.button-label')?.textContent;
  if(element){element.setAttribute('aria-busy','true');if(busyLabel)label(button,busyLabel);}
  if(view)render(view);
  try{return await fn();}catch(error){uiError=error?.message||String(error);showError();}
  finally {pending.delete(name);if(element){element.setAttribute('aria-busy','false');if(oldLabel)label(button,oldLabel);}if(view)render(view);}
}
function focusAccount(field='username') {showPage('settings');$(field).focus();}
$('manage-account').addEventListener('click',()=>focusAccount());
$('edit-routes').addEventListener('click',()=>{showPage('settings');$('route-settings').open=true;$('cidrs').focus();});
$('back-home').addEventListener('click',()=>showPage('home'));
$('footer-core').addEventListener('click',()=>{showPage('settings');$('core-settings').open=true;});
$('connect').addEventListener('click',()=>action('connect',async()=>{
  notice('');
  if(view.state!=='disconnected'){await api.Disconnect();return;}
  const s=settings();
  if(!s.username){focusAccount();notice('请先填写学校账号。');return;}
  if(!$('password').value&&!s.remember){focusAccount('password');notice('请输入 VPN 密码后连接。');return;}
  await api.Connect(s,$('password').value);$('password').value='';
}));
$('settings-form').addEventListener('submit',event=>{event.preventDefault();action('save',async()=>{
  const savingPassword=$('remember').checked&&!!$('password').value;
  await api.SaveProfile(settings(),$('password').value);if(savingPassword)$('password').value='';
  notice(savingPassword?'设置及密码已保存，下次可直接连接。':'设置已保存。');render(await api.GetView());
},{button:'save',busyLabel:'正在保存…'});});
$('probe').addEventListener('click',()=>action('probe',async()=>notice(await api.Probe()),{button:'probe',busyLabel:'正在检测…'}));
$('install').addEventListener('click',()=>action('install',()=>api.PrepareCore(),{button:'install',busyLabel:'正在准备…'}));
$('refresh').addEventListener('click',()=>action('refresh',async()=>{await api.SaveSettings(settings());await api.RefreshCore();render(await api.GetView());},{button:'refresh',busyLabel:'检测中…'}));
$('forget').addEventListener('click',()=>action('forget',async()=>{
  if(!$('username').value.trim())throw Error('请先填写要清除密码的学校账号。');
  await api.SaveSettings(settings());await api.ForgetPassword();$('remember').checked=false;$('password').value='';passwordHint();notice('所选保存方式中的密码已清除。');render(await api.GetView());
}));
$('remember').addEventListener('change',passwordHint);$('password-storage').addEventListener('change',passwordHint);
for(const id of ['username','group','cidrs'])$(id).addEventListener('input',renderSummary);
$('clear').addEventListener('click',()=>action('clear',async()=>{await api.ClearLogs();render(await api.GetView());}));
$('show-password').addEventListener('click',()=>{const input=$('password'),show=input.type==='password';input.type=show?'text':'password';$('show-password').textContent=show?'隐藏':'显示';$('show-password').setAttribute('aria-label',show?'隐藏密码':'显示密码');$('show-password').setAttribute('aria-pressed',String(show));});
$('log-output').addEventListener('wheel',()=>{$('follow').checked=false;},{passive:true});
$('follow').addEventListener('change',()=>{if($('follow').checked)$('log-output').scrollTop=$('log-output').scrollHeight;});
reducedMotion.addEventListener('change',()=>{if(reducedMotion.matches)$('app').getAnimations({subtree:true}).forEach(animation=>animation.cancel());});
if(api) {
  window.runtime.EventsOn('vpn:update',render);
  action('startup',async()=>{render(await api.GetView());await api.RefreshCore();});
} else {
  render({settings:{server:'https://vpn.cuhk.edu.cn',group:'CUHK(SZ)',username:'',cidrs:['10.20.0.0/16'],keepDNS:false,remember:false,corePath:'',passwordStorage:'portable'},state:'disconnected',phase:'',logs:[],core:{}});
  notice('静态界面预览，请使用桌面程序操作 VPN。');
}
