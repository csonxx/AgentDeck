'use strict';
(() => {
  let token=location.hash.slice(1);
  // This is only the short-lived owner-console capability, never a phone root
  // or session record. Tab-scoped storage keeps ordinary reloads usable.
  try{if(token)sessionStorage.setItem('agentdeck.console',token);else token=sessionStorage.getItem('agentdeck.console')||'';}catch{}
  history.replaceState(null,'','/');
  const $=id=>document.getElementById(id);
  let refreshing=false, posting=false, requests=new Map(), previousLogin='', lastState='';
  const states={unlinked:'尚未关联',preparing:'正在联系网关',authorizing:'等待飞书授权',failed:'关联未完成',online:'已连接 · 加密隧道在线',reconnecting:'已关联 · 正在连接网关',direct:'直连模式'};
  const connectionErrors={clock_unavailable:'无法校准网关时间，请检查网络及 HTTPS 证书。',clock_authentication_rejected:'网关拒绝工作站时间校准，请查看网关认证日志。',tunnel_authentication_rejected:'网关拒绝工作站认证，请查看网关日志。',workstation_already_connected:'同一工作站已有连接，请检查是否启动了重复的本地后端。',gateway_unavailable:'网关暂时不可用，后端会自动重试。',tunnel_network_or_tls_failed:'无法建立加密隧道，请检查网络、证书及代理 WebSocket 配置。',tunnel_identity_rejected:'网关隧道身份校验失败，请查看网关日志。'};
  function note(text){$('notice').textContent=text; $('notice').hidden=!text;}
  async function api(path,body){
    if(!token)throw new Error('管理页面访问凭据已失效，请重新运行 agentdeck console 打开。');
    const res=await fetch('/api/'+path,{method:body===undefined?'GET':'POST',headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(22000)});
    const raw=await res.text(); let data; try{data=JSON.parse(raw);}catch{}
    if(!res.ok){if(res.status===401)throw new Error(path==='approve'?'校验码不一致或请求已过期，请核对手机上的码后重试。':'管理页面访问凭据已失效，请重新打开本机管理页面。');throw new Error(data?.error||'本机请求失败（HTTP '+res.status+'）');}
    return data;
  }
  function date(value){return value?new Date(value).toLocaleString('zh-CN',{hour12:false}):'—';}
  function updateStatus(s){
    const authorizing=['preparing','authorizing'].includes(s.state);
    $('connection-state').textContent=states[s.state]||s.state;
    $('live').textContent=s.online?'工作站在线':s.linked?'网关重连中':s.mode==='direct'?'本机已连接':'本机已就绪';
    $('live').className='badge '+(s.online?'online':s.linked||authorizing?'busy':'neutral');
    $('connection-detail').hidden=!s.linked; $('link-form').hidden=s.mode!=='relay'||s.linked||authorizing;
    $('direct-note').hidden=s.mode!=='direct'; $('authorization').hidden=!authorizing;
    $('station-name').textContent=s.name||'本机工作站'; $('gateway-url').textContent=s.url||'—';
    $('connected-at').textContent=date(s.connection?.connected_at); $('attempts').textContent=s.connection?.attempts??'—';
    $('connection-error').textContent=s.error||(s.connection?.last_error?(connectionErrors[s.connection.last_error]||'连接断开，后端正在自动重试。请检查网络或网关日志。'):''); $('connection-error').hidden=!$('connection-error').textContent;
    $('authorization-message').textContent=s.state==='preparing'?'正在发起飞书关联…':'请打开飞书授权页面，确认关联本机。';
    if(s.login_url){
      const target=new URL(s.login_url);
      if(target.protocol==='https:'){$('authorization-link').href=s.login_url;$('authorization-link').hidden=false;}
      if(previousLogin!==s.login_url){previousLogin=s.login_url;note('授权链接已准备好，点击「打开飞书授权页面」继续。');}
    }else{$('authorization-link').hidden=true;}
    if(s.linked&&lastState==='authorizing')note('工作站关联成功，正在自动建立加密连接。');
    lastState=s.state;
  }
  function renderPending(rows,s){
    const ids=new Set(rows.map(r=>r.grant));
    for(const [id,node]of requests){if(!ids.has(id)){node.remove();requests.delete(id);}}
    $('pending-count').textContent=String(rows.length);
    let empty=$('pending').querySelector('.empty');
    if(rows.length){empty?.remove();}else if(!empty){empty=document.createElement('div');empty.className='empty';empty.innerHTML='<strong></strong><p></p>';$('pending').append(empty);}
    if(empty&&!rows.length){empty.querySelector('strong').textContent=s.online?'暂无待确认的手机':s.linked?'连接网关后显示手机请求':'关联工作站后即可配对手机';empty.querySelector('p').textContent='在 App 选择「通过网关 · 飞书登录」，使用同一账号登录并选择这台工作站。';}
    rows.forEach((r,i)=>{
      let node=requests.get(r.grant);
      if(!node){
        node=document.createElement('div');node.className='request';
        node.innerHTML='<div class="request-meta"><strong></strong><span></span></div><form><label>手机显示的校验码<input autocomplete="off" spellcheck="false" maxlength="14" placeholder="xxxx-xxxx-xxxx" required aria-label="手机显示的校验码"></label><button type="submit">校验并批准配对</button></form><p class="error" role="alert" hidden></p>';
        node.querySelector('form').addEventListener('submit',async event=>{
          event.preventDefault();const input=node.querySelector('input'),button=node.querySelector('button'),error=node.querySelector('.error');
          const code=input.value.trim().replaceAll('-','').toLowerCase();
          if(!/^[a-f0-9]{12}$/.test(code)){error.hidden=false;error.textContent='请输入手机显示的完整 12 位校验码。';return;}
          button.disabled=true;error.hidden=true;
          try{await api('approve',{grant:r.grant,code});input.value='';note('校验一致，已批准手机配对。请在手机点击「两边一致，等待工作站确认」。');await refresh();}
          catch(e){error.textContent=e.message;error.hidden=false;}
          finally{button.disabled=false;}
        });requests.set(r.grant,node);$('pending').append(node);
      }
      node.querySelector('strong').textContent=r.approved?'已批准 · 等待手机连接':'手机配对请求 '+(i+1);
      node.querySelector('.request-meta span').textContent='有效至 '+date(r.expires*1000);
      node.querySelector('form').hidden=r.approved;
    });
  }
  async function refresh(){
    if(refreshing)return;refreshing=true;
    try{const s=await api('status');updateStatus(s);const rows=s.online?await api('pending'):[];renderPending(rows,s);$('refreshed').textContent='状态更新于 '+new Date().toLocaleTimeString('zh-CN',{hour12:false});}
    catch(e){$('live').textContent='本机连接异常';$('live').className='badge neutral';note(e.name==='TimeoutError'?'连接本地后端超时，请确认后端仍在运行。':e.message);}
    finally{refreshing=false;}
  }
  $('link-form').addEventListener('submit',async event=>{
    event.preventDefault();if(posting)return;posting=true;$('link-button').disabled=true;
    try{await api('login',{url:$('url').value.trim(),name:$('name').value.trim()});note('正在准备飞书授权页面…');await refresh();}
    catch(e){note(e.message);}finally{posting=false;$('link-button').disabled=false;}
  });
  $('cancel').addEventListener('click',async()=>{try{await api('cancel',{});note('已取消此次授权。');await refresh();}catch(e){note(e.message);}});
  $('refresh').addEventListener('click',refresh);
  refresh();setInterval(refresh,2000);
})();
