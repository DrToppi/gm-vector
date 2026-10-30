// Started only by TestBrowserE2E. All data, TLS server and browser are disposable.
const fs = require('node:fs/promises');
const path = require('node:path');
const {spawn} = require('node:child_process');
const assert = require('node:assert/strict');
const {createHmac,privateDecrypt,createDecipheriv,constants} = require('node:crypto');
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
let chrome, socket;
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => {chrome?.kill('SIGKILL');process.exit(1)});
const pending = new Map();
let sequence = 0;
const deadline = setTimeout(() => { console.error('E2E exceeded 120 seconds'); chrome?.kill('SIGKILL'); process.exit(1); }, 120000);
async function main() {
  let input = ''; for await (const chunk of process.stdin) input += chunk;
  const config = JSON.parse(input);
  const origin = new URL(config.origin);
  assert.equal(origin.protocol, 'https:');
  assert.equal(origin.hostname, '127.0.0.1');
  assert(origin.port && !['9443', '19443', '29443'].includes(origin.port), 'requires disposable test server');
  const args = ['--headless=new', '--no-first-run', '--no-default-browser-check', '--disable-background-networking', '--disable-component-update', '--disable-sync', '--disable-dev-shm-usage', '--ignore-certificate-errors', '--remote-debugging-port=0', '--remote-debugging-address=127.0.0.1', `--user-data-dir=${config.profile}`, 'about:blank'];
  // Sandboxing stays enabled by default. Containers may explicitly opt out for
  // this disposable synthetic browser; no running/user browser is reused.
  if (process.env.GMA_E2E_NO_SANDBOX === '1') args.unshift('--no-sandbox');
  chrome = spawn(config.chrome, args, {stdio: 'ignore'});
  let launchError; chrome.on('error', () => {launchError = true});
  let port;
  for (let i=0;i<150;i++) {
    if (launchError || chrome.exitCode !== null) throw Error('Chrome failed to start');
    try { port = Number((await fs.readFile(path.join(config.profile, 'DevToolsActivePort'), 'utf8')).split('\n')[0]); if (port) break; } catch {}
    await sleep(100);
  }
  assert(port, 'Chrome CDP startup timeout');
  const pages = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
  const page = pages.find(p => p.type === 'page'); assert(page, 'Chrome page missing');
  socket = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {socket.onopen=resolve;socket.onerror=()=>reject(Error('CDP connection failed'))});
  const browserErrors = [];
  const serverFailures = [];
  const memberRefresh = new Map();
  let captureMemberRefresh = false;
  socket.onmessage = event => {
    const m = JSON.parse(event.data);
    if (m.id) {const p=pending.get(m.id);if(p){pending.delete(m.id);clearTimeout(p.timer);m.error?p.reject(Error('CDP command failed: '+p.method+' ('+m.error.code+')')):p.resolve(m.result)}}
    else if (m.method === 'Runtime.exceptionThrown') browserErrors.push('uncaught browser exception');
    else if (m.method === 'Network.responseReceived') {
      const responseURL=new URL(m.params.response.url);
      if(responseURL.origin===origin.origin){
        if(m.params.response.status>=500)serverFailures.push({path:responseURL.pathname,status:m.params.response.status});
        if(captureMemberRefresh && ['/api/v1/projects','/api/v1/employees','/api/v1/calendars'].includes(responseURL.pathname))memberRefresh.set(responseURL.pathname,m.params.response.status);
      }
    }
  };
  socket.onclose=()=>{for(const p of pending.values()){clearTimeout(p.timer);p.reject(Error('CDP closed'))}pending.clear()};
  const send = (method, params={}) => new Promise((resolve,reject)=>{const id=++sequence;const timer=setTimeout(()=>{pending.delete(id);reject(Error('CDP command timeout'))},15000);pending.set(id,{resolve,reject,timer,method});socket.send(JSON.stringify({id,method,params}))});
  const evaluate = async expression => {const r=await send('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});if(r.exceptionDetails)throw Error('Browser evaluation failed');return r.result.value};
  const wait = async (expression, label) => {for(let i=0;i<150;i++){try{if(await evaluate(expression))return}catch(e){if(!e.message.includes('Runtime.evaluate (-32000)'))throw e}await sleep(100)}throw Error('Timed out: '+label+'; '+await evaluate(`JSON.stringify({heading:document.querySelector('h1')?.textContent,dialog:document.querySelector('dialog h2')?.textContent,error:document.querySelector('.error')?.textContent})`))};
  const selectButton = text => `[...document.querySelectorAll('button')].find(x=>x.textContent.trim()===${JSON.stringify(text)})`;
  const click = async text => {await wait(`!!(${selectButton(text)}) && !(${selectButton(text)}).disabled`,text);await evaluate(`(${selectButton(text)}).click()`)};
  const field = async (selector,value) => {await wait(`!!document.querySelector(${JSON.stringify(selector)})`, 'input exists');await evaluate(`(()=>{const x=document.querySelector(${JSON.stringify(selector)});Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(x,${JSON.stringify(value)});x.dispatchEvent(new Event('input',{bubbles:true}));return new Promise(resolve=>requestAnimationFrame(()=>resolve(true)));})()`)};
  const read = suffix => evaluate(`fetch('/api/v1'+${JSON.stringify(suffix)}).then(async r=>{if(!r.ok)throw Error('read failed');return r.json()})`);
  await send('Network.enable');await send('Runtime.enable');await send('Page.enable');await send('Page.addScriptToEvaluateOnNewDocument',{source:`try{localStorage.setItem('gm-vector.locale','ru')}catch{}`});await send('Page.addScriptToEvaluateOnNewDocument',{source:`{const originalFetch=window.fetch.bind(window);let release;const gate=new Promise(resolve=>release=resolve);window.releaseCalendarFixture=release;window.fetch=async(...args)=>{if(String(args[0]).endsWith('/api/v1/calendars'))await gate;return originalFetch(...args)};}`});await send('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false});await send('Page.navigate',{url:config.origin});
  await wait(`!!document.querySelector('input[autocomplete="username"]')`, 'login screen');
  assert.equal(await evaluate('document.title'),'gmVector','application tab name');
  assert.equal(await evaluate("document.querySelector('.brand').textContent"),'gmVector','login branding');
  assert(await evaluate(`!!document.querySelector('[data-bug-report="open"]')`),'Bug Report available before login');
  const setLanguage=async locale=>{await wait(`!!document.querySelector('.language-picker select') && !document.querySelector('.language-picker select').disabled`,'language picker');await evaluate(`(()=>{const x=document.querySelector('.language-picker select');Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype,'value').set.call(x,${JSON.stringify(locale)});x.dispatchEvent(new Event('change',{bubbles:true}));})()`);await wait(`document.documentElement.lang===${JSON.stringify(locale)}`,'selected language');};
  for(const locale of ['en','de','fr','es','pt-BR','zh-Hans','ru']){
   await setLanguage(locale);const catalog=JSON.parse(await fs.readFile(path.join(__dirname,'../web/src/locales',locale+'.json'),'utf8'));
   await wait(`document.querySelector('.auth-card h2')?.textContent===${JSON.stringify(catalog.md74489dc8f)}`,'localized first-run heading '+locale);
  }

  assert.equal(await evaluate(`document.querySelector('.auth-card input[autocomplete="off"]') !== null`),false,'no installation key field');
  await field('input[autocomplete="username"]',config.username);
  assert.equal(await evaluate('location.hash'),'','first run has no secret URL');
  await field('.auth-card input:not([autocomplete])','E2E Admin');
  await field('input[autocomplete="new-password"]',config.password);
  await click('Настроить MFA');
  await wait(`!!document.querySelector('.totp-qr svg') && !!document.querySelector('code.secret')?.textContent`,'first-run QR and manual secret');
  const setupSecret=await evaluate(`document.querySelector('code.secret').textContent`);
  const setupBits=[...setupSecret].map(c=>'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'.indexOf(c).toString(2).padStart(5,'0')).join('');
  const setupBytes=Buffer.from(setupBits.match(/.{8}/g).map(byte=>parseInt(byte,2)));
  const setupCounter=Buffer.alloc(8);setupCounter.writeBigUInt64BE(BigInt(Math.floor(Date.now()/30000)));
  const setupDigest=createHmac('sha1',setupBytes).update(setupCounter).digest(),setupOffset=setupDigest.at(-1)&15;
  const setupOTP=String((setupDigest.readUInt32BE(setupOffset)&0x7fffffff)%1000000).padStart(6,'0');
  await field('input[autocomplete="one-time-code"]',setupOTP);await click('Подтвердить');
  await wait(`!!document.querySelector('.auth-card pre')`,'first-run recovery codes');
  config.code=await evaluate(`document.querySelector('.auth-card pre').textContent.trim().split(/\\s+/)[0]`);
  assert(config.code.length>=20,'recovery code shown only after MFA');
  await click('Коды сохранены — открыть планировщик');await wait(`!!document.querySelector('.sidebar-new')`,'new administrator workspace');
  assert.equal(await evaluate("document.querySelector('.brand').textContent"),'gmVector','workspace branding');
  const bootstrapStatus=await read('/auth/status');assert.equal(bootstrapStatus.setup_required,false,'first-run consumed');
  await click('＋ Новый проект');await field('dialog form input','E2E synthetic project');await field('dialog input[type=date]','2026-10-23');
  // deliberately open the form before calendar data is available.
  assert(await evaluate(`(${selectButton('Создать')}).disabled`),'no calendar blocks submission');
  await evaluate(`window.releaseCalendarFixture()`);
  await wait(`document.querySelector('dialog select')?.options.length>0`,'calendar options loaded');
  assert(await evaluate(`!!document.querySelector('dialog select').value`),'late calendar gets a valid default');
  // Later reloads should not delay calendars again.
  await send('Page.addScriptToEvaluateOnNewDocument',{source:`queueMicrotask(()=>window.releaseCalendarFixture?.())`});
  await click('Создать');
  await wait(`document.querySelector('.project-header h1')?.textContent==='E2E synthetic project'`,'project created');
  await setLanguage('en');assert.equal((await read('/auth/me')).user.locale,'en','profile language saved');
  await send('Page.reload');await wait(`document.documentElement.lang==='en' && !!document.querySelector('.project-header')`,'profile language survives reload');
  assert.equal((await read('/auth/me')).user.locale,'en');await setLanguage('ru');assert.equal((await read('/auth/me')).user.locale,'ru');

  await evaluate(`document.querySelector('.project-actions').open=true`);await click('＋ Задача');
  await field('dialog form input','E2E parent');await field('dialog input[type=number]','2');await click('Проверить и сохранить');await click('Применить изменения');
  await wait(`!document.querySelector('dialog[open]')`,'task saved');
  const projects=await read('/projects');assert.equal(projects.length,1,'isolated project list');const id=projects[0].id;
  let saved=(await read('/projects/'+id)).project;
  assert.equal(saved.tasks.length,1);assert.equal(saved.tasks[0].planned_minutes,120);const parentID=saved.tasks[0].id;
  await click('Задачи');await evaluate(`document.querySelector('[aria-label="Добавить подзадачу: E2E parent"]').click()`);
  await field('dialog form input','E2E child');await field('dialog input[type=number]','3.25');await field('dialog input[type=number][data-duration-days]','2');await click('Проверить и сохранить');await click('Применить изменения');await wait(`!document.querySelector('dialog[open]')`,'child saved');
  saved=(await read('/projects/'+id)).project;
  assert.equal(saved.tasks.length,2);const parent=saved.tasks.find(t=>t.id===parentID),child=saved.tasks.find(t=>t.parent===parentID);
  assert(parent.summary,'first subtask creates summary milestone');assert.equal(child.planned_minutes,195);assert.equal(parent.planned_minutes,195,'summary rolls up child hours');
  await send('Page.reload');await wait(`document.querySelector('.project-header h1')?.textContent==='E2E synthetic project'`,'project persists after reload');
  await click('Задачи');await wait(`document.body.innerText.includes('E2E child')`,'child persists in UI');
  saved=(await read('/projects/'+id)).project;assert.equal(saved.tasks.find(t=>t.id===parentID).planned_minutes,195);
  // project-start changes move existing tasks and their summary,
  // preserve effort/manual duration, and do not move twice after preview.
  assert.equal(child.start,'2026-10-23');assert.equal(child.end,'2026-10-26');
  const shiftProject = async (start, end) => {
    const before=(await read('/projects/'+id)).project;
    await click('Настройки проекта');await field('dialog input[type=date]',start);
    await click('Предпросмотр изменений');
    await wait(`document.querySelector('dialog h2')?.textContent==='Проверка изменений'`,'project shift preview');
    assert(await evaluate(`document.querySelector('.preview-list').textContent.includes(${JSON.stringify(start)}) && document.querySelector('.preview-list').textContent.includes(${JSON.stringify(end)})`),'preview shows shifted dates');
    const uncommitted=(await read('/projects/'+id)).project;
    assert.equal(uncommitted.start,before.start,'preview does not persist project start');
    assert.equal(uncommitted.tasks.find(t=>t.id===child.id).start,before.tasks.find(t=>t.id===child.id).start,'preview does not persist child dates');
    await click('Применить изменения');await wait(`!document.querySelector('dialog[open]')`,'project shift saved');
    const after=(await read('/projects/'+id)).project;
    assert.equal(after.start,start);
    for(const taskID of [parentID,child.id]) {
      const task=after.tasks.find(t=>t.id===taskID);
      assert.equal(task.start,start,'task/summary start shifted once');assert.equal(task.end,end,'task/summary end respects weekend');
      assert.equal(task.planned_minutes,195,'project shift preserves planned hours');assert.equal(task.actual_minutes,0,'project shift preserves actual hours');
    }
    assert.equal(after.tasks.find(t=>t.id===child.id).duration_days,2,'project shift preserves manual duration');
  };
  await shiftProject('2026-10-30','2026-11-02');
  await send('Page.reload');await wait(`document.querySelector('.project-header h1')?.textContent==='E2E synthetic project'`,'shifted project reload');
  await click('Задачи');await wait(`document.body.innerText.includes('2026-10-30') && document.body.innerText.includes('2026-11-02')`,'shifted dates persist in UI');
  saved=(await read('/projects/'+id)).project;assert.equal(saved.tasks.find(t=>t.id===child.id).start,'2026-10-30');assert.equal(saved.tasks.find(t=>t.id===child.id).end,'2026-11-02');
  await shiftProject('2026-10-23','2026-10-26');
  // summaries remain visibly distinct in every view, with selection
  // and folding layered over the task state instead of erasing it.
  const milestoneSelector=`[data-milestone="${parentID}"]`;
  const leafMarker=`[data-milestone="${child.id}"]`;
  const selectedCard=`[data-task="${parentID}"]>.task-card`;
  const viewRevision=(await read('/projects/'+id)).project.revision;
  await click('Задачи');await wait(`!!document.querySelector('tr${milestoneSelector} .milestone-badge')`,'list milestone badge');
  assert(await evaluate(`!document.querySelector('${leafMarker}')`),'ordinary list task has no milestone marker');
  assert(await evaluate(`![...document.querySelectorAll('.tabs button')].some(b=>b.textContent==='Трудозатраты')`),'one task workspace without obsolete effort tab');
  assert(await evaluate(`!document.querySelector('table [aria-label^="Trace"]') && !document.querySelector('table').textContent.includes('Trace')`),'unified table has no Trace button');
  assert(await evaluate(`document.querySelectorAll('table [data-task-status]').length===2`),'each task has an accessible status circle');
  const screenshot=async name=>{
    if(process.env.GMA_E2E_SCREENSHOTS!=='1')return;
    const dir=path.resolve(__dirname,'../test-results');await fs.mkdir(dir,{recursive:true});
    const file=path.join(dir,`gma-e2e-milestones-${name}.png`);
    await fs.writeFile(file,Buffer.from((await send('Page.captureScreenshot',{format:'png'})).data,'base64'),{mode:0o600});
    console.log('Synthetic screenshot: '+file);
  };
  await click('Гант');await wait(`!!document.querySelector('[data-task="${parentID}"]${milestoneSelector}')`,'Gantt milestone marker');
  assert(await evaluate(`!!document.querySelector('[data-milestone-label="${parentID}"]') && !!document.querySelector('[data-milestone-band="${parentID}"]')`),'Gantt milestone label and band');
  assert(await evaluate(`!document.querySelector('${leafMarker}') && !document.querySelector('[data-milestone-label="${child.id}"]')`),'ordinary Gantt task has no milestone decoration');
  const fillBeforeSelection=await evaluate(`document.querySelector('${selectedCard}').getAttribute('fill')`);
  await evaluate(`document.querySelector('${selectedCard}').dispatchEvent(new MouseEvent('click',{bubbles:true}))`);
  await wait(`document.querySelector('${selectedCard}').getAttribute('stroke')==='#086ad8'`,'selected milestone blue outline');
  assert.equal(await evaluate(`document.querySelector('${selectedCard}').getAttribute('fill')`),fillBeforeSelection,'selection preserves state fill');
  await screenshot('gantt');
  await click('Майнд-мэп');await wait(`!!document.querySelector('.map-viewport [data-milestone-label="${parentID}"]')`,'map milestone label');
  assert(await evaluate(`!document.querySelector('${leafMarker}') && !document.querySelector('[data-milestone-label="${child.id}"]')`),'ordinary map task has no milestone decoration');
  assert.equal(await evaluate(`document.querySelector('${selectedCard}').getAttribute('stroke')`),'#086ad8','map preserves blue selection');
  assert.equal(await evaluate(`document.querySelector('${selectedCard}').getAttribute('fill')`),fillBeforeSelection,'map preserves state fill');
  await click('Вписать');await screenshot('map');
  await evaluate(`document.querySelector('[data-collapse="${parentID}"]').dispatchEvent(new MouseEvent('click',{bubbles:true}))`);
  await wait(`!document.querySelector('[data-task="${child.id}"]')`,'milestone hides child on collapse');
  assert(await evaluate(`!!document.querySelector('${milestoneSelector}') && !!document.querySelector('[data-milestone-label="${parentID}"]')`),'collapsed milestone retains label');
  await evaluate(`document.querySelector('[data-collapse="${parentID}"]').dispatchEvent(new MouseEvent('click',{bubbles:true}))`);
  await wait(`!!document.querySelector('[data-task="${child.id}"]')`,'milestone expands child');
  assert.equal((await read('/projects/'+id)).project.revision,viewRevision,'view changes and folding do not save project');
  // map modes keep independent cameras; collapse and Trace do not zoom.
  const mapTransform=()=>evaluate(`document.querySelector('[data-map-content]').getAttribute('transform')`);
  const treeCamera=await mapTransform();
  await click('Flow');await wait(`!!document.querySelector('.chart-panel[data-map-mode="flow"]')`,'flow view');
  assert(await evaluate(`!!document.querySelector('[data-task="${child.id}"]')`),'flow includes child');
  await click('+');const flowCamera=await mapTransform();await screenshot('flow');
  await click('Этапы');await wait(`!!document.querySelector('[data-stage-id="${parentID}"]')`,'stage column');
  const stageCamera=await mapTransform();await screenshot('stages');
  const scale=camera=>camera.match(/scale\(([^)]+)\)/)[1];
  await evaluate(`document.querySelector('[data-collapse="${parentID}"]').dispatchEvent(new MouseEvent('click',{bubbles:true}))`);
  await wait(`!document.querySelector('[data-task="${child.id}"]')`,'stage collapse');
  assert.equal(scale(await mapTransform()),scale(stageCamera),'collapse preserves stage zoom');
  await evaluate(`document.querySelector('[data-collapse="${parentID}"]').dispatchEvent(new MouseEvent('click',{bubbles:true}))`);
  await wait(`!!document.querySelector('[data-task="${child.id}"]')`,'stage expand');
  await click('Дерево');assert.equal(await mapTransform(),treeCamera,'tree camera restored');
  await click('Flow');assert.equal(await mapTransform(),flowCamera,'flow camera restored');
  await evaluate(`document.querySelector('[aria-label="Trace E2E child"]').dispatchEvent(new MouseEvent('click',{bubbles:true}))`);
  await wait(`!!document.querySelector('.chart-panel[data-map-mode="trace"]')`,'trace view');
  assert(await evaluate(`!document.querySelector('[data-map-mode-switch]')`),'trace has no mode switch');
  await click('Выйти из Trace');await wait(`!!document.querySelector('.chart-panel[data-map-mode="flow"]')`,'trace restores mode');
  assert.equal(await mapTransform(),flowCamera,'trace restores camera');
  assert.equal((await read('/projects/'+id)).project.revision,viewRevision,'map modes do not change project revision');

  // configure real per-project weekly capacity in the UI.
  const employeeName='Тестов Макет'; // Synthetic fixture for the accepted surname-first initials contract.
  const choose=async(selector,value)=>{
    await wait(`!!document.querySelector(${JSON.stringify(selector)})`,'select exists');
    await evaluate(`(()=>{const x=document.querySelector(${JSON.stringify(selector)});Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype,'value').set.call(x,${JSON.stringify(value)});x.dispatchEvent(new Event('change',{bubbles:true}));return new Promise(resolve=>requestAnimationFrame(()=>resolve(true)));})()`);
  };
  const toggle=async selector=>evaluate(`(()=>{document.querySelector(${JSON.stringify(selector)}).click();return new Promise(resolve=>requestAnimationFrame(()=>resolve(true)));})()`);
  await click('Сотрудники');await field('input[placeholder="ФИО"]',employeeName);await click('Создать сотрудника');
  await wait(`document.querySelector('[aria-label="Сотрудник для проекта"]').textContent.includes(${JSON.stringify(employeeName)})`,'employee created');
  const employee=(await read('/employees')).find(e=>e.name===employeeName);assert(employee);
  await choose('[aria-label="Сотрудник для проекта"]',employee.id);await click('Добавить в проект');
  await wait(`document.querySelector('dialog h2')?.textContent==='Настроить участие перед добавлением'`,'required participation editor');
  assert.equal((await read('/projects/'+id)).project.people.length,0,'opening configuration must not bind employee');
  await click('Предпросмотр изменений');
  await wait(`document.querySelector('dialog .error')?.textContent.includes('день участия')`,'unconfigured participation rejected');
  assert.equal((await read('/projects/'+id)).project.people.length,0,'invalid configuration not persisted');
  for(const day of ['Пн','Вт','Ср','Чт','Пт']) {
    await toggle(`[aria-label="${day} активен"]`);
    await field(`[aria-label="${day} начало"]`,day==='Пн'?'10:00':'09:00');
    await field(`[aria-label="${day} окончание"]`,day==='Пн'?'12:00':'13:00');
    await field(`[aria-label="${day} квота, часов"]`,day==='Пн'?'2':'4');
  }
  await click('Предпросмотр изменений');await click('Применить изменения');
  await wait(`!!document.querySelector('[data-participant="${employee.id}"]') && !document.querySelector('dialog[open]')`,'configured participant saved');
  const participant=(await read('/projects/'+id)).project.people[0];
  assert.equal(participant.week['1'].daily_minutes,120);assert.equal(participant.week['1'].start,'10:00');assert.equal(participant.week['1'].end,'12:00');assert.equal(participant.week['5'].daily_minutes,240);
  assert.equal(await evaluate(`document.querySelector('[data-participant="${employee.id}"] .avatar').textContent`),'МТ','employee avatar is given-name then family-name initial');
  assert(await evaluate(`![...document.querySelectorAll('button')].some(b=>b.textContent==='Лимит и аккаунт')`),'project cannot edit global maximum');
  await click('⚙ Управление');
  await wait(`(()=>{const row=document.querySelector('[data-employee-admin="${employee.id}"]');return row && !row.querySelector('input').disabled && !row.querySelector('button[type="submit"]').disabled})()`,'employee administration permissions loaded');
  await field(`[data-employee-admin="${employee.id}"] [aria-label="Общий дневной лимит, часов"]`,'4');
  await evaluate(`[...document.querySelector('[data-employee-admin="${employee.id}"]').querySelectorAll('button')].find(b=>b.textContent==='Сохранить общий максимум').click()`);
  await wait(`fetch('/api/v1/employees').then(r=>r.json()).then(rows=>rows.find(e=>e.id===${JSON.stringify(employee.id)})?.daily_minutes===240)`,'global maximum saved through administration');
  await evaluate(`document.querySelector('nav button').click()`);
  await send('Page.reload');await wait(`document.querySelector('.project-header h1')?.textContent==='E2E synthetic project'`,'project reopens with refreshed global limit');
  await click('Задачи');await click('E2E child');
  await field('dialog input[type=number]','16');
  await toggle(`[aria-label="Назначить ${employeeName}"]`);
  assert.equal(await evaluate(`document.querySelector('[aria-label="Часы в день ${employeeName}"]').value`),'4','assignment default follows configured capacity');
  assert(await evaluate(`document.querySelector('dialog').textContent.includes('10:00') && document.querySelector('dialog').textContent.includes('12:00')`),'task exposes employee participation windows');
  await click('Проверить и сохранить');await click('Применить изменения');await wait(`!document.querySelector('dialog[open]')`,'capacity-based plan saved');
  const allocated=(await read('/projects/'+id)).project;const work=allocated.tasks.find(t=>t.id===child.id);
  assert.equal(allocated.calculation_state,'ready');assert.equal(work.planned_minutes,960);assert.equal(work.duration_days,2,'manual minimum retained');assert.equal(work.start,'2026-10-23');assert.equal(work.end,'2026-10-29','actual duration extends to fit weekly quotas');
  assert.equal(allocated.loads.reduce((n,l)=>n+l.minutes,0),960);
  for(const l of allocated.loads)assert(l.minutes<=(l.date==='2026-10-26'?120:240),'allocation respects employee/project day maximum');
  await send('Page.reload');await wait(`document.querySelector('.project-header h1')?.textContent==='E2E synthetic project'`,'capacity plan reload');
  await click('Сотрудники');await wait(`document.querySelector('[data-participant="${employee.id}"]')?.textContent.includes('10:00–12:00')`,'weekly schedule persists');
  const write=async(suffix,method,body)=>evaluate(`(async()=>{const me=await fetch('/api/v1/auth/me').then(r=>r.json());const r=await fetch('/api/v1'+${JSON.stringify(suffix)},{method:${JSON.stringify(method)},headers:{'Content-Type':'application/json','X-CSRF-Token':me.csrf},body:JSON.stringify(${JSON.stringify(body)})});if(!r.ok)throw Error('fixture write failed');return r.json()})()`);
  // A second synthetic project makes actual daily totals exceed the global cap.
  // Observe the original screen updating even though its own revision is unchanged.
  const originalRevision=(await read('/projects/'+id)).project.revision;
  const other=await write('/projects','POST',{title:'E2E competing quota',start:'2026-10-23',duration_mode:'working',calendar_id:allocated.calendar_id});
  other.people=[participant];other.tasks=[{...work,id:'quota-other',parent:'',title:'Competing work',requested_start:'2026-10-23',start:'',end:'',duration_days:1,planned_minutes:240,summary:false}];other.links=[];
  const conflicting=await write('/projects/'+other.id,'PUT',{project:other});
  assert.equal(conflicting.calculation_state,'blocked_capacity','conflicting configuration saved with blocked calculation');
  await wait(`document.querySelector('header[data-calculation-state]')?.dataset.calculationState==='blocked_capacity'`,'other project conflict updates current UI');
  const blocked=(await read('/projects/'+id)).project;assert.equal(blocked.revision,originalRevision);assert.equal(blocked.tasks.find(t=>t.id===child.id).start,'','conflict hides invalid calculated dates');assert.equal(blocked.loads?.length||0,0);
  await click('Гант');await wait(`document.querySelector('[data-duration="${child.id}"]')?.textContent==='Не рассчитано'`,'Gantt does not present an invalid duration');
  await write('/projects/'+other.id,'DELETE',{revision:conflicting.revision});
  await wait(`document.querySelector('header[data-calculation-state]')?.dataset.calculationState==='ready'`,'capacity recovers after conflict removed');
  const recovered=(await read('/projects/'+id)).project;assert.equal(recovered.revision,originalRevision);assert.equal(recovered.tasks.find(t=>t.id===child.id).end,'2026-10-29');
  // fact recording refreshes the revision without erasing task drafts.
  await click('Задачи');await click('E2E child');
  await wait(`!!document.querySelector('[data-task-effort="${child.id}"]')`,'effort embedded in task editor');
  await field('dialog form input','E2E child draft title');await field('dialog input[type=number]','17');
  await wait(`document.querySelector('[aria-label="Сотрудник для записи времени"]')?.value===${JSON.stringify(employee.id)}`,'fact defaults to assigned employee');
  await choose('[aria-label="Сотрудник для записи времени"]',employee.id);
  await field('[aria-label="Дата записи времени"]','2026-10-23');
  await field('[aria-label="Часы записи времени"]','1.25');
  await click('Записать время');
  await wait(`document.querySelectorAll('[data-time-entry]').length===1 && document.querySelector('[data-task-fact]')?.textContent.includes('1.25')`,'fact saved in task without closing editor');
  assert.equal(await evaluate(`document.querySelector('dialog form input').value`),'E2E child draft title','fact refresh preserves draft title');
  assert.equal(await evaluate(`document.querySelector('dialog input[type=number]').value`),'17','fact refresh preserves draft plan');
  let timeFacts=await read('/projects/'+id+'/time-entries');assert.equal(timeFacts.length,1);assert.equal(timeFacts[0].minutes,75);assert.equal(timeFacts[0].employee_id,employee.id);
  assert.equal((await read('/projects/'+id)).project.tasks.find(t=>t.id===child.id).title,'E2E child','fact action does not implicitly save task draft');
  await click('Проверить и сохранить');await click('Применить изменения');await wait(`!document.querySelector('dialog[open]')`,'task draft saves against refreshed revision');
  const withFact=(await read('/projects/'+id)).project.tasks.find(t=>t.id===child.id);
  assert.equal(withFact.title,'E2E child draft title');assert.equal(withFact.planned_minutes,1020);assert.equal(withFact.actual_minutes,75);
  await click('E2E child draft title');await field('dialog form input','E2E child');
  await click('Проверить и сохранить');await click('Применить изменения');await wait(`!document.querySelector('dialog[open]')`,'fixture title restored without losing fact');
  // customer address persists; internal messages never silently email.
  await click('Задачи');await click('E2E child');
  await evaluate(`[...document.querySelectorAll('dialog label')].find(x=>x.textContent==='Задачу выполняет заказчик').querySelector('input').click()`);
  await field('.customer-fields input','E2E customer owner');
  await field('[aria-label="Почта заказчика"]','customer@example.test');
  await click('Проверить и сохранить');await click('Применить изменения');await wait(`!document.querySelector('dialog[open]')`,'customer address saved');
  assert.equal((await read('/projects/'+id)).project.tasks.find(t=>t.id===child.id).customer_email,'customer@example.test');
  await click('E2E child');
  await wait(`!!document.querySelector('[data-email-disabled]')`,'SMTP disabled notice');
  assert(await evaluate(`document.querySelector('[aria-label="Отправить заказчику"]').disabled`),'SMTP not configured prevents external sending');
  const commentText='<b>E2E internal note</b>';
  const commentField=async value=>evaluate(`(()=>{const x=document.querySelector('textarea[aria-label="Новый комментарий"]');Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype,'value').set.call(x,${JSON.stringify(value)});x.dispatchEvent(new Event('input',{bubbles:true}));return new Promise(resolve=>requestAnimationFrame(()=>resolve(true)));})()`);
  await commentField(commentText);await click('Добавить внутренний комментарий');
  await wait(`document.querySelector('.task-comment-text')?.textContent===${JSON.stringify(commentText)}`,'comment appears');
  assert(await evaluate(`!document.querySelector('.task-comment-text b')`),'comment is rendered as text, not HTML');
  let discussion=await read('/projects/'+id+'/tasks/'+child.id+'/comments');
  assert.equal(discussion.comments.length,1);assert.equal(discussion.comments[0].delivery_state,'internal');assert.equal(discussion.comments[0].send_to_customer,false);
  await commentField('Draft must not send when task is saved');
  await click('Проверить и сохранить');await click('Применить изменения');await wait(`!document.querySelector('dialog[open]')`,'task saved without sending draft');
  discussion=await read('/projects/'+id+'/tasks/'+child.id+'/comments');assert.equal(discussion.comments.length,1,'task save does not submit draft comment');
  await send('Page.reload');await wait(`document.querySelector('.project-header h1')?.textContent==='E2E synthetic project'`,'comments reload');
  await click('Задачи');await click('E2E child');
  await wait(`document.querySelector('.task-comment-text')?.textContent===${JSON.stringify(commentText)}`,'comment persists after reload');
  assert.equal(await evaluate(`document.querySelector('[aria-label="Почта заказчика"]').value`),'customer@example.test');
  await evaluate(`document.querySelector('dialog .icon').click()`);await wait(`!document.querySelector('dialog[open]')`,'comment editor closed');
  // exercise actual browser downloads, not only HTML/CSV generators.
  const downloadDir=path.join(config.profile,'downloads');await fs.mkdir(downloadDir,{recursive:true});
  await send('Browser.setDownloadBehavior',{behavior:'allow',downloadPath:downloadDir});
  const exportButton=async format=>{await evaluate(`document.querySelector('[data-export-format="${format}"]').closest('details').open=true`);await evaluate(`document.querySelector('[data-export-format="${format}"]').click()`)};
  const downloaded=async extension=>{const file=path.join(downloadDir,'gma-project.'+extension);for(let i=0;i<100;i++){try{return await fs.readFile(file,'utf8')}catch{}await sleep(100)}throw Error('Download did not complete: '+extension)};
  await exportButton('json');const jsonExport=JSON.parse(await downloaded('json'));assert.equal(jsonExport.id,id);assert.equal(jsonExport.tasks.find(t=>t.id===child.id).actual_minutes,75);
  await exportButton('csv');const csvExport=await downloaded('csv');assert(csvExport.includes('E2E child') && csvExport.includes('E2E customer owner'),'CSV contains task and customer executor');
  await exportButton('html');const htmlExport=await downloaded('html');assert(htmlExport.includes('E2E synthetic project') && htmlExport.includes('E2E child'),'HTML contains project report');assert(!htmlExport.includes(commentText),'report does not leak discussion history');
  // Headless print dialogs do not produce a user-selected PDF file. Verify the
  // real UI print action writes a report and requests printing, without claiming
  // OS PDF saving is covered here.
  await evaluate(`window.__originalOpen=window.open;window.__printCalled=false;window.__printDocument=document.implementation.createHTMLDocument('');window.open=()=>({document:window.__printDocument,closed:false,focus(){},close(){},print(){window.__printCalled=true},opener:null})`);
  await exportButton('pdf');await wait(`window.__printCalled && window.__printDocument.body.textContent.includes('E2E child')`,'PDF action requests report print');
  await evaluate(`window.open=window.__originalOpen`);
  // download real JWE, decrypt only with disposable fixture private key.
  const secretMarker='sensitive-marker-must-not-export';
  await evaluate(`history.replaceState(null,'','/?private=${secretMarker}#setup=${secretMarker}'); console.error('${secretMarker}'); window.dispatchEvent(new ErrorEvent('error',{message:'${secretMarker}',filename:location.origin+'/private/${secretMarker}.js',lineno:7})); fetch('/api/v1/projects/${secretMarker}?email=${secretMarker}').then(r=>r.text())`);
  await click('Bug Report');
  assert(await evaluate(`document.querySelector('[data-bug-report="prepare"]').disabled`),'empty report blocked');
  for(const name of ['title','steps','expected','actual']){
    await evaluate(`(()=>{const x=document.querySelector('[data-bug-report-field="${name}"]'); Object.getOwnPropertyDescriptor(x.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype,'value').set.call(x,'Synthetic ${name}');x.dispatchEvent(new Event('input',{bubbles:true}));})()`);
  }
  await evaluate(`document.querySelector('[data-bug-report="issue-preview"]').click()`);
  await wait(`!!document.querySelector('[data-bug-report="issue-body"]')`,'public issue preview');
  const publicText=await evaluate(`document.querySelector('[data-bug-report="issue-body"]').textContent`);
  assert(publicText.includes('Synthetic steps') && publicText.includes('Synthetic expected'),'public draft uses explicit fields');
  for(const prohibited of [secretMarker,config.origin,config.password,config.code,'request_id','events_status','server.events'])assert(!publicText.includes(prohibited),'public issue excludes diagnostics and secrets');
  assert(!(await evaluate(`!!document.querySelector('[data-bug-report="issue-open"]')`)),'public consent required');
  await evaluate(`document.querySelector('[data-bug-report="issue-reviewed"]').click()`);
  const issueLink=await evaluate(`(()=>{const a=document.querySelector('[data-bug-report="issue-open"]');return {href:a.href,rel:a.rel,target:a.target}})()`);
  const issueURL=new URL(issueLink.href);assert.equal(issueURL.hostname,'github.com');assert.equal(issueURL.pathname,'/DrToppi/gm-vector/issues/new');assert.equal(issueURL.searchParams.get('body'),publicText);assert(issueLink.rel.includes('noopener') && issueLink.rel.includes('noreferrer'));assert.equal(issueLink.target,'_blank');
  await click('Подготовить и посмотреть');
  await wait(`!!document.querySelector('[data-bug-report="preview"]')`,'diagnostic preview');
  const shot=await send('Page.captureScreenshot',{format:'png'});await fs.writeFile('/tmp/gma-bug-report-preview.png',Buffer.from(shot.data,'base64'));
  const preview=JSON.parse(await evaluate(`document.querySelector('[data-bug-report="preview"]').textContent`));
  assert.equal(preview.server.events_status,'available'); assert.equal(preview.server.state_status,'available');
  assert(preview.server.events.length>0,'server technical journal included');
  for(const prohibited of [secretMarker,'E2E synthetic project','E2E child',commentText,config.password,config.code,config.origin])assert(!JSON.stringify(preview).includes(prohibited),'sensitive values excluded');
  assert(await evaluate(`document.querySelector('[data-bug-report="download"]').disabled`),'explicit review required');
  assert(await evaluate(`document.querySelector('[data-bug-report="store"]').disabled`),'server upload also requires explicit review');
  await evaluate(`document.querySelector('[data-bug-report="reviewed"]').click()`);
  await click('Скачать зашифрованный отчёт');
  const readBug=async count=>{for(let i=0;i<100;i++){const files=(await fs.readdir(downloadDir)).filter(f=>f.endsWith('.gma-bug')).sort();if(files.length>=count)return fs.readFile(path.join(downloadDir,files.at(-1)),'utf8');await sleep(100)}throw Error('Encrypted bug download missing')};
  const decryptBug=raw=>{const parts=raw.split('.');assert.equal(parts.length,5);const header=JSON.parse(Buffer.from(parts[0],'base64url'));assert.equal(header.alg,'RSA-OAEP-256');assert.equal(header.enc,'A256GCM');const key=privateDecrypt({key:config.report_private_key,padding:constants.RSA_PKCS1_OAEP_PADDING,oaepHash:'sha256'},Buffer.from(parts[1],'base64url'));const decipher=createDecipheriv('aes-256-gcm',key,Buffer.from(parts[2],'base64url'));decipher.setAAD(Buffer.from(parts[0]));decipher.setAuthTag(Buffer.from(parts[4],'base64url'));return JSON.parse(Buffer.concat([decipher.update(Buffer.from(parts[3],'base64url')),decipher.final()]).toString())};
  const encrypted=await readBug(1);assert(!encrypted.includes('Synthetic title'),'download encrypted');assert.deepEqual(decryptBug(encrypted),preview);
  // Surface only fixed safe storage errors, never arbitrary server text.
  await evaluate(`window.bugErrorOriginalFetch=window.fetch;window.fetch=(...args)=>String(args[0]).endsWith('/diagnostics/reports')?Promise.resolve(new Response(JSON.stringify({error:{code:'report_storage_full',message:'Хранилище отчётов заполнено. Скачайте файл и обратитесь к администратору.'}}),{status:507,headers:{'Content-Type':'application/json'}})):window.bugErrorOriginalFetch(...args)`);
  await click('Сохранить на сервере');
  await wait(`document.querySelector('[data-bug-report="message"]')?.textContent==='Хранилище отчётов заполнено. Скачайте файл и обратитесь к администратору.'`,'safe quota error displayed');
  assert.equal((await fs.readdir(config.report_directory)).filter(f=>f.endsWith('.gma-bug')).length,0,'mock quota failure stores nothing');
  await evaluate(`window.fetch=(...args)=>String(args[0]).endsWith('/diagnostics/reports')?Promise.resolve(new Response(JSON.stringify({error:{message:'UNTRUSTED-SENSITIVE-SERVER-TEXT'}}),{status:500,headers:{'Content-Type':'application/json'}})):window.bugErrorOriginalFetch(...args)`);
  await click('Сохранить на сервере');
  await wait(`document.querySelector('[data-bug-report="message"]')?.textContent.includes('Сервер не подтвердил')`,'unknown error replaced with safe fallback');
  assert(!(await evaluate(`document.querySelector('dialog').textContent`)).includes('UNTRUSTED-SENSITIVE-SERVER-TEXT'),'arbitrary server message suppressed');
  await evaluate(`window.fetch=window.bugErrorOriginalFetch`);
  // The real server saves JWE, but the browser loses the acknowledgement.
  // Retry must reuse the ciphertext so the server can return the existing ID.
  await evaluate(`window.bugUploadOriginalFetch=window.fetch;window.__bugUploadBody=null;window.fetch=async(...args)=>{if(String(args[0]).endsWith('/diagnostics/reports')){window.__bugUploadBody=args[1].body;const response=await window.bugUploadOriginalFetch(...args);window.__bugUploadStatus=response.status;throw new TypeError('synthetic lost acknowledgement')}return window.bugUploadOriginalFetch(...args)}`);
  await click('Сохранить на сервере');
  await wait(`window.__bugUploadStatus===201 && document.querySelector('[data-bug-report="message"]')?.textContent.includes('Сервер не подтвердил')`,'uncertain upload reported honestly');
  assert.equal(await evaluate(`document.querySelector('[data-bug-report-field="title"]').value`),'Synthetic title','failed upload preserves report form');
  assert.equal(await evaluate('window.__bugUploadStatus'),201,'real server accepted before simulated lost acknowledgement');
  const upload=JSON.parse(await evaluate('window.__bugUploadBody'));
  assert.deepEqual(Object.keys(upload),['encrypted'],'no plaintext fields posted');
  assert(!JSON.stringify(upload).includes('Synthetic title'),'upload encrypted');
  assert.deepEqual(decryptBug(upload.encrypted),preview);
  const storedFiles=await fs.readdir(config.report_directory);
  assert.equal(storedFiles.filter(f=>f.endsWith('.gma-bug')).length,1,'one persistent report after lost acknowledgement');
  const storedName=storedFiles.find(f=>f.endsWith('.gma-bug'));
  assert.equal(await fs.readFile(path.join(config.report_directory,storedName),'utf8'),upload.encrypted,'server preserves encrypted payload');
  await evaluate(`window.fetch=window.bugUploadOriginalFetch`);
  await click('Сохранить на сервере');
  await wait(`document.querySelector('[data-bug-report="message"]')?.textContent.includes('Отчёт сохранён на сервере. Номер:')`,'server receipt displayed');
  assert(await evaluate(`document.querySelector('[data-bug-report="store"]').disabled`),'saved report cannot be submitted again accidentally');
  assert.equal((await fs.readdir(config.report_directory)).filter(f=>f.endsWith('.gma-bug')).length,1,'retry does not duplicate stored report');
  // Existing public key allows local encrypted export if diagnostics become unavailable.
  await evaluate(`window.bugOriginalFetch=window.fetch;window.fetch=(...args)=>String(args[0]).endsWith('/diagnostics/events')||String(args[0]).endsWith('/diagnostics')?Promise.reject(new TypeError('synthetic offline')):window.bugOriginalFetch(...args)`);
  await click('Подготовить и посмотреть');await wait(`document.querySelector('[data-bug-report="preview"]').textContent.includes('"events_status": "unavailable"')`,'offline fallback');
  assert(await evaluate(`document.querySelector('[data-bug-report="download"]').disabled`),'re-preparation resets review');
  await evaluate(`document.querySelector('[data-bug-report="reviewed"]').click()`);await click('Скачать зашифрованный отчёт');
  assert.equal(decryptBug(await readBug(2)).server.events_status,'unavailable');
  await evaluate(`window.fetch=window.bugOriginalFetch; history.replaceState(null,'','/'); document.querySelector('dialog .icon').click()`);
  await click('Настройки проекта');await click('Удалить проект…');
  const confirm=selectButton('Удалить и переместить в корзину');assert(await evaluate(`(${confirm}).disabled`),'empty phrase blocks archive');
  await field('.danger-zone input','wrong');assert(await evaluate(`(${confirm}).disabled`),'wrong phrase blocks archive');
  assert.equal((await read('/projects')).length,1,'project unchanged before confirmation');
  await field('.danger-zone input','удалить');await click('Удалить и переместить в корзину');await wait(`!document.querySelector('dialog[open]')`,'project archived');
  assert.equal((await read('/projects')).length,0,'archived project absent');const trash=await read('/trash');assert(trash.some(p=>p.id===id),'project recoverable in trash');
  // invitation-link enrollment UI. The existing admin API supplies a
  // synthetic token; SMTP delivery is deliberately outside this browser check.
  const invitedUsername='e2e_invited_member';
  const invitation=await write('/users','POST',{username:invitedUsername,name:'Тестов Макет',admin:false});
  assert(typeof invitation.enrollment_token==='string' && invitation.enrollment_token.length>=32,'synthetic enrollment token issued');
  await click('Выйти');await wait(`!!document.querySelector('input[autocomplete="username"]')`,'admin logged out');
  await send('Page.navigate',{url:'about:blank'});await wait(`location.href==='about:blank'`,'new invitation navigation');
  await send('Page.navigate',{url:config.origin+'/#setup='+encodeURIComponent(invitation.enrollment_token)+'&login='+encodeURIComponent(invitedUsername)});
  await wait(`document.querySelector('.auth-card h2')?.textContent==='Настройка доступа'`,'invitation opens enrollment');
  assert.equal(await evaluate(`document.querySelector('input[autocomplete="username"]').value`),invitedUsername,'invitation prefills login');
  assert.equal(await evaluate('location.hash'),'','invitation secret removed from address bar');
  await field('.auth-card input:not([autocomplete])','Тестов Макет');
  await field('input[autocomplete="new-password"]','synthetic member chooses password 2026');
  await click('Настроить MFA');
  await wait(`!!document.querySelector('.totp-qr svg') && !!document.querySelector('code.secret')?.textContent`,'QR and manual TOTP key displayed');
  const enrollmentSecret=await evaluate(`document.querySelector('code.secret').textContent`);
  assert(/^[A-Z2-7]+$/.test(enrollmentSecret),'TOTP manual key is base32');
  const bits=[...enrollmentSecret].map(c=>'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'.indexOf(c).toString(2).padStart(5,'0')).join('');
  const secretBytes=Buffer.from(bits.match(/.{8}/g).map(byte=>parseInt(byte,2)));
  const counter=Buffer.alloc(8);counter.writeBigUInt64BE(BigInt(Math.floor(Date.now()/30000)));
  const digest=createHmac('sha1',secretBytes).update(counter).digest(),offset=digest[digest.length-1]&15;
  const otp=String((digest.readUInt32BE(offset)&0x7fffffff)%1000000).padStart(6,'0');
  await field('input[autocomplete="one-time-code"]',otp);await click('Подтвердить');
  await wait(`document.querySelector('.auth-card h2')?.textContent==='Сохраните коды восстановления'`,'recovery codes after MFA confirmation');
  assert.equal(await evaluate(`document.querySelector('.auth-card pre').textContent.trim().split(/\\s+/).length`),10,'ten recovery codes presented');
  captureMemberRefresh=true;
  await click('Коды сохранены — открыть планировщик');await wait(`!!document.querySelector('.identity')`,'new member authenticated');
  const memberSession=await read('/auth/me');assert.equal(memberSession.user.id,invitation.id);assert.equal(memberSession.user.username,invitedUsername);assert.equal(memberSession.user.admin,false);assert.equal(memberSession.user.active,true);
  assert.equal(await evaluate(`document.querySelector('.identity .avatar').textContent`),'МТ','new user avatar has correct initials');
  await wait(`!document.querySelector('.loading') && !!document.querySelector('.app aside')`,'new member workspace loaded');
  // Wait for all four actual non-admin list requests, rather than relying on
  // rendering the identity badge before refreshLists has finished.
  for(let i=0;i<100 && memberRefresh.size<3;i++)await sleep(100);
  assert.equal(memberRefresh.size,3,'all new member workspace lists completed');
  for(const [pathname,status] of memberRefresh)assert.equal(status,200,'new member refresh: '+pathname);
  assert.deepEqual(serverFailures,[],'no API server errors (paths/statuses only)');
  assert(await evaluate(`!document.querySelector('.global-error')`),'new member has no workspace refresh error');
  await evaluate(`window.noKeyOriginalFetch=window.fetch;window.fetch=(...args)=>String(args[0]).endsWith('/diagnostics/key')?Promise.resolve(new Response(JSON.stringify({configured:false}),{status:200,headers:{'Content-Type':'application/json'}})):window.noKeyOriginalFetch(...args)`);
  await click('Выйти');await wait(`!!document.querySelector('input[autocomplete="username"]')`,'logout for missing-key test');await click('Bug Report');
  await wait(`document.querySelector('[data-bug-report="key-status"]')?.textContent.includes('ещё не настроен')`,'missing public key explained');
  assert(await evaluate(`document.querySelector('[data-bug-report="download"]').disabled`),'missing key blocks plaintext fallback');
  assert.deepEqual(browserErrors,[],'no browser exceptions');
  console.log('E2E passed: fresh first-run credentials and QR/manual TOTP setup, seven UI languages and profile persistence, public issue preview/consent/privacy, project/task/subtask, summary hours, project start shift forward/back with preview and reload persistence, milestone markers with selection and folding, map modes/cameras/Trace, asynchronous calendar default, task comments/customer email with SMTP disabled, weekly quotas/windows, required binding, automatic duration extension, live shared-capacity conflict and recovery, initials, unified task/status workspace, embedded facts preserving drafts, global maximum administration, HTML/CSV/JSON downloads and PDF print action, typed archive confirmation and trash, invitation link username/hash cleanup, self-chosen password and QR/manual TOTP enrollment with recovery codes; encrypted Bug Report server persistence/decryption, lost-acknowledgement idempotent retry, download/decryption, privacy exclusions, review gate, offline diagnostics fallback and missing key block; real API/PostgreSQL.');
}
main().catch(error=>{console.error(error.message);process.exitCode=1}).finally(async()=>{
  socket?.close();for(const p of pending.values())clearTimeout(p.timer);pending.clear();
  if(chrome && chrome.exitCode===null){chrome.kill('SIGTERM');for(let i=0;i<30&&chrome.exitCode===null;i++)await sleep(100);if(chrome.exitCode===null)chrome.kill('SIGKILL')}
  clearTimeout(deadline);
});
