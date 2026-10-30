// Real React Workspace with synthetic loopback API and controlled response bodies.
// This harness never reads credentials or connects to a deployed application.
const assert=require('node:assert/strict'),fs=require('node:fs/promises'),path=require('node:path'),os=require('node:os'),http=require('node:http'),{spawn}=require('node:child_process'),vm=require('node:vm');
const {build,buildSync}=require('../web/node_modules/esbuild');
const {instrumentWorkspace,checkInstrumentation}=require('./workspace-fixture-source.cjs');
const web=path.resolve(__dirname,'../web'),sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
let server,chrome,socket,profile,sequence=0,delivery=0,scenario='',employeeName='Old synthetic employee',states={},gates=[],held=[],requests=[];
const pending=new Map(),browserErrors=[];
function chromeDiagnostic(stderr){return stderr.replace(/\x1b\[[0-9;]*[A-Za-z]/g,'').replace(/[\x00-\x08\x0b-\x1f\x7f]/g,'').replace(/\b[a-z][a-z0-9+.-]*:\/\/[^\s"'<>]+/gi,'<url>').replace(/\b[A-Z]:\\[^\s"'<>]+/gi,'<path>').replace(/\/[^\s"'<>\])]+/g,'<path>').replace(/[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/gi,'<email>').replace(/\b(token|password|secret|credential|authorization)\s*[:=]\s*[^\s,;]+/gi,'$1=<redacted>').trim().slice(-4096)}
async function chromePort(child,dir){
 let stderr='',spawnCode='';const started=performance.now();
 child.stderr.on('data',chunk=>{stderr=(stderr+chunk.toString('utf8')).slice(-8192)});
 child.on('error',error=>{spawnCode=/^[A-Z0-9_]+$/.test(error.code||'')?error.code:'SPAWN_ERROR'});
 const failure=reason=>Error('Chrome startup failed: '+reason+'; exit='+String(child.exitCode)+'; signal='+String(child.signalCode)+'; spawn='+(spawnCode||'none')+'; stderr='+(chromeDiagnostic(stderr)||'<empty>'));
 for(let i=0;i<150;i++){
  if(spawnCode)throw failure('spawn error');
  if(child.exitCode!==null||child.signalCode!==null)throw failure('process exited before DevTools became ready');
  try{const value=(await fs.readFile(path.join(dir,'DevToolsActivePort'),'utf8')).split('\n')[0];if(/^\d+$/.test(value)&&Number(value)>0&&Number(value)<=65535){const elapsed=Math.round(performance.now()-started);console.log('Chrome startup readiness: elapsed_ms='+elapsed+'; late_after_6000ms='+(elapsed>6000));return Number(value)}}catch{}
  await sleep(100);
 }
 throw failure('DevTools port unavailable after 15000ms');
}
function project(id,change={}){return {id,title:'Project '+id,brief:'',start:'2026-01-06',duration_mode:'working',calendar_id:'synthetic-calendar',revision:1,calculation_state:'ready',tasks:[{id:'task-'+id,title:'Task '+id,parent:'',status:'todo',responsibility:'internal',requested_start:'2026-01-06',start:'2026-01-06',end:'2026-01-06',duration_days:1,planned_minutes:60,actual_minutes:0,assignments:[{employee_id:'e',daily_minutes:60}]}],links:[],people:[{employee_id:'e',week:{'2':{active:true,start:'09:00',end:'10:00',daily_minutes:60}}}],loads:[],warnings:[],...change}}
function employee(){return {id:'e',name:employeeName,daily_minutes:480,user_id:'',provider:'',external_id:''}}
const calendar={id:'synthetic-calendar',year:2026,title:'Synthetic calendar',days:Object.fromEntries(Array.from({length:365},(_,n)=>{const d=new Date(Date.UTC(2026,0,n+1));return [d.toISOString().slice(0,10),{working:d.getUTCDay()!==0&&d.getUTCDay()!==6}]}))};
async function unit(){
 checkInstrumentation();
 const code=buildSync({stdin:{contents:"export * from './src/workspaceRefresh';",resolveDir:web,loader:'ts'},bundle:true,platform:'node',format:'cjs',write:false}).outputFiles[0].text;
 let timer=0,hidden=false,online=true,modal=false,session=true;
 const timers=new Map(),events=new Map(),document={get hidden(){return hidden},querySelector:()=>modal?{}:null,body:{},addEventListener:(name,fn)=>events.set(name,fn),removeEventListener:name=>events.delete(name)};
 const window={setTimeout:(fn,delay)=>{assert.equal(delay,5000);timers.set(++timer,fn);return timer},clearTimeout:id=>timers.delete(id),addEventListener:(name,fn)=>events.set(name,fn),removeEventListener:name=>events.delete(name)};
 const sandbox={module:{exports:{}},exports:{},AbortController,document,window,navigator:{get onLine(){return online}},MutationObserver:class{observe(){}disconnect(){}}};
 vm.runInNewContext(code,sandbox);const Coordinator=sandbox.module.exports.WorkspaceRefresh,c=new Coordinator(()=>session);
 const first=c.read('lists');const a=c.select('A');assert(!first.current()&&first.signal.aborted);c.select('B');c.select('A');assert(!a.current(),'A→B→A has a new intent');
 const oldProject=c.read('project'),newList=c.read('lists');assert(!oldProject.current()&&oldProject.signal.aborted);assert(newList.current(),'list supersedes project employees');
 const newerProject=c.read('project');assert(!newList.current()&&newList.signal.aborted);assert(c.accept(newerProject,{id:'A',revision:5}));assert(c.accept(newerProject,{id:'A',revision:5}),'current equal revision eligible');assert(!c.accept(newerProject,{id:'A',revision:4}),'lower revision rejected');
 const operation=c.beginOperation('A');assert(operation.current()&&!newerProject.current()&&newerProject.signal.aborted);assert.equal(c.beginOperation('A'),null,'duplicate operation blocked');c.select('B');const next=c.beginOperation('B');operation.finish();assert(c.writing&&next.current(),'old finally cannot finish current operation');next.finish();assert(!c.writing);
 const scoped=c.read('project');session=false;assert(!scoped.current());session=true;c.close();assert(!scoped.current()&&scoped.signal.aborted);
 const polling=new Coordinator(()=>session);polling.select('A');let active=0,calls=0;const completed=[];
 polling.connectPolling(async()=>{calls++;active++;assert.equal(active,1,'one background batch');await new Promise(resolve=>completed.push(resolve));active--},()=>true);
 assert.equal(timers.size,1);const tick=()=>{for(const [id,fn]of [...timers]){timers.delete(id);fn()}};
 tick();assert.equal(calls,1);assert.equal(timers.size,0,'next timer waits for completion');polling.updatePolling();assert.equal(calls,1);
 hidden=true;events.get('visibilitychange')();online=false;events.get('offline')();assert.equal(timers.size,0);hidden=false;events.get('visibilitychange')();online=true;events.get('online')();events.get('online')();assert.equal(calls,1,'resume waits for active batch');
 completed.shift()();await new Promise(setImmediate);assert.equal(calls,2,'resume coalesces one catch-up');completed.shift()();await new Promise(setImmediate);assert.equal(timers.size,1,'completion schedules five seconds');
 modal=true;polling.updatePolling();assert.equal(timers.size,0,'nested modal pauses polling');modal=false;polling.updatePolling();assert.equal(calls,3,'closing modal catches up once');polling.close();completed.shift()();await new Promise(setImmediate);assert.equal(timers.size,0,'unmount does not restart polling');assert.equal(events.size,0,'listeners removed');
 console.log('Workspace refresh unit PASS: intent/resource/revision/operation guards, session invalidation, single completion timer, hidden/offline/dialog catch-up and cleanup.');
}
const send=(method,params={})=>new Promise((resolve,reject)=>{const id=++sequence,timer=setTimeout(()=>{pending.delete(id);reject(Error('CDP timeout '+method))},10000);pending.set(id,{resolve,reject,timer});socket.send(JSON.stringify({id,method,params}))});
const evaluate=async expression=>{const r=await send('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});if(r.exceptionDetails)throw Error('Synthetic browser evaluation failed');return r.result.value};
async function wait(expression,label){for(let i=0;i<200;i++){if(await evaluate(expression))return;await sleep(25)}throw Error('Condition timeout '+label)}
async function settle(){await evaluate('new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))')}
async function click(text){const query='[...document.querySelectorAll("button")].find(x=>x.textContent.trim()==='+JSON.stringify(text)+')';await wait('!!('+query+')&&!('+query+').disabled','button '+text);await evaluate('('+query+').click()');await settle()}
async function chooseProject(id){await evaluate('[...document.querySelectorAll("nav button")].find(x=>x.textContent.includes('+JSON.stringify('Project '+id)+')).click()');await settle()}
async function field(selector,value){await evaluate('(()=>{const x=document.querySelector('+JSON.stringify(selector)+');Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,"value").set.call(x,'+JSON.stringify(value)+');x.dispatchEvent(new Event("input",{bubbles:true}));})()');await settle()}
const heading=()=>evaluate('document.querySelector(".project-header h1")?.textContent');
function hold(route,options={}){const gate={route,method:'GET',...options};gates.push(gate);return gate}
async function waitHeld(gate){for(let i=0;i<200&&!gate.release;i++)await sleep(10);assert(gate.release,'response body held '+gate.route)}
async function release(gate){await waitHeld(gate);gate.release();await wait('window.__fixtureDeliveries.includes('+gate.delivery+')','released JSON delivered');await settle()}
async function tick(){await evaluate('window.__tickPoll()');await settle()}
async function reset(){
 for(const gate of held.splice(0))gate.release();gates=[];requests=[];employeeName='Old synthetic employee';states={A:project('A'),B:project('B')};
 await send('Page.navigate',{url:'http://127.0.0.1:'+server.address().port});
 await wait('document.querySelector(".project-header h1")?.textContent==="Project A"','initial A');await settle();
}
async function main(){
 await unit();
 const bundle=(await build({entryPoints:[path.join(web,'src/main.tsx')],bundle:true,platform:'browser',format:'iife',write:false,loader:{'.css':'empty'},plugins:[{name:'synthetic-read-entrypoints',setup(build){let targets=0;build.onLoad({filter:/\/(main\.tsx|useWorkspaceController\.ts)$/},async args=>{const result=instrumentWorkspace(await fs.readFile(args.path,'utf8'),args.path);targets+=result.count;return{contents:result.contents,loader:args.path.endsWith('.tsx')?'tsx':'ts',resolveDir:path.dirname(args.path)}});build.onEnd(()=>{assert.equal(targets,1,'exactly one bundled workspace controller instrumented')})}}]})).outputFiles[0].text;
 server=http.createServer(async(req,res)=>{
  const route=new URL(req.url,'http://localhost').pathname;res.setHeader('Cache-Control','no-store');
  if(route==='/fixture.js'){res.setHeader('Content-Type','application/javascript');return res.end(bundle)}
  if(!route.startsWith('/api/v1/')){res.setHeader('Content-Type','text/html');return res.end('<div id="root"></div><script src="/fixture.js"></script>')}
  const endpoint=route.slice(7),id=endpoint.split('/')[2];requests.push({endpoint,method:req.method});
  let input='';for await(const chunk of req)input+=chunk;const bodyInput=input?JSON.parse(input):{};
  const gate=gates.find(item=>item.route===endpoint&&item.method===req.method&&!item.release);
  let value,code=200;const user={id:'synthetic-admin',name:'Synthetic administrator',admin:true,active:true,locale:'ru'};
  if(gate?.code>=400){code=gate.code;value={error:{code:'revision_conflict',message:'SYNTHETIC OLD OPERATION ERROR'}}}
  else if(endpoint==='/auth/me')value={user,csrf:'synthetic-csrf'};
  else if(endpoint==='/projects')value=Object.keys(states).map(id=>({id,title:'Project '+id,task_count:1}));
  else if(endpoint==='/employees'&&req.method==='POST'){employeeName='Fresh synthetic employee';value=employee()}
  else if(endpoint==='/employees')value=[employee()];
  else if(endpoint==='/calendars')value=[calendar];
  else if(endpoint==='/diagnostics/key')value={configured:false};
  else if(endpoint.endsWith('/comments'))value={comments:[],email_enabled:false,can_comment:false,customer_email:'',customer:false};
  else if(endpoint==='/projects/'+id+'/preview')value=bodyInput.project;
  else if(endpoint==='/projects/'+id+'/undo'){states[id]={...states[id],title:'Undo accepted '+id,revision:states[id].revision+1};value=states[id]}
  else if(endpoint==='/projects/'+id&&req.method==='PUT'){states[id]={...bodyInput.project,revision:states[id].revision+1};value=states[id]}
  else if(endpoint==='/projects/'+id&&req.method==='DELETE'){delete states[id];value={}}
  else if(endpoint==='/projects/'+id&&states[id])value={project:structuredClone(states[id]),role:'manager'};
  else value=[];
  const number=++delivery;res.statusCode=code;res.setHeader('X-Fixture-Delivery',String(number));res.setHeader('Content-Type','application/json');res.flushHeaders();
  const body=JSON.stringify(value);
  if(gate){gate.delivery=number;gate.release=()=>{if(!gate.released){gate.released=true;res.end(body)}};held.push(gate)}else res.end(body);
 });await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 profile=await fs.mkdtemp(path.join(os.tmpdir(),'gm-vector-refresh-ui-'));
 chrome=spawn(process.env.GMA_CHROME_BIN||'google-chrome',['--headless=new','--no-first-run','--disable-background-networking','--disable-component-update','--disable-sync','--remote-debugging-port=0','--remote-debugging-address=127.0.0.1','--user-data-dir='+profile,'about:blank'],{stdio:['ignore','ignore','pipe']});
 const port=await chromePort(chrome,profile);
 const pages=await(await fetch('http://127.0.0.1:'+port+'/json')).json();socket=new WebSocket(pages.find(page=>page.type==='page').webSocketDebuggerUrl);await new Promise(resolve=>socket.onopen=resolve);
 socket.onmessage=event=>{const m=JSON.parse(event.data);if(m.id){const p=pending.get(m.id);if(p){pending.delete(m.id);clearTimeout(p.timer);m.error?p.reject(Error('CDP failure')):p.resolve(m.result)}}else if(m.method==='Runtime.exceptionThrown')browserErrors.push('browser exception')};
 await send('Page.enable');await send('Runtime.enable');
 await send('Page.addScriptToEvaluateOnNewDocument',{source:'{localStorage.setItem("gm-vector.locale","ru");const real=window.fetch.bind(window);window.__fixtureDeliveries=[];window.fetch=async(url,options={})=>{const {signal,...rest}=options;const response=await real(url,rest),json=response.json.bind(response);response.json=async()=>{const value=await json();window.__fixtureDeliveries.push(Number(response.headers.get("X-Fixture-Delivery")));return value};return response};const timeout=window.setTimeout.bind(window),interval=window.setInterval.bind(window),clearTimeout=window.clearTimeout.bind(window),clearInterval=window.clearInterval.bind(window);let id=100000;const timers=new Map();window.setTimeout=(fn,delay,...args)=>delay===5000?(timers.set(++id,{fn:()=>fn(...args),once:true}),id):timeout(fn,delay,...args);window.setInterval=(fn,delay,...args)=>delay===5000?(timers.set(++id,{fn:()=>fn(...args),once:false}),id):interval(fn,delay,...args);window.clearTimeout=number=>{timers.delete(number);clearTimeout(number)};window.clearInterval=number=>{timers.delete(number);clearInterval(number)};window.__tickPoll=()=>{for(const [number,timer]of [...timers]){if(timer.once)timers.delete(number);timer.fn()}};window.__setHidden=value=>{Object.defineProperty(document,"hidden",{configurable:true,value});Object.defineProperty(document,"visibilityState",{configurable:true,value:value?"hidden":"visible"});document.dispatchEvent(new Event("visibilitychange"))};window.__setOnline=value=>{Object.defineProperty(navigator,"onLine",{configurable:true,value});window.dispatchEvent(new Event(value?"online":"offline"))};}'});
 const tests={
  async selection(){
   await reset();const old=hold('/projects/A');await chooseProject('A');await waitHeld(old);await chooseProject('B');await wait('document.querySelector(".project-header h1")?.textContent==="Project B"','new B');await release(old);assert.equal(await heading(),'Project B','old A must not replace B');
  },
  async repeated_selection(){
   await reset();states.A.title='Obsolete A';const old=hold('/projects/A');await chooseProject('A');await waitHeld(old);await chooseProject('B');await wait('document.querySelector(".project-header h1")?.textContent==="Project B"','B');states.A.title='Latest A';await chooseProject('A');await wait('document.querySelector(".project-header h1")?.textContent==="Latest A"','latest A');await release(old);assert.equal(await heading(),'Latest A','first A generation must not replace last A');
  },
  async equal_revision(){
   await reset();states.A.calculation_state='blocked_capacity';const old=hold('/projects/A');await tick();await waitHeld(old);states.A.calculation_state='ready';await chooseProject('A');await wait('document.querySelector("header[data-calculation-state]")?.dataset.calculationState==="ready"','new equal revision state');await release(old);assert.equal(await evaluate('document.querySelector("header[data-calculation-state]").dataset.calculationState'),'ready','old equal-revision derived state cannot replace fresh state');
  },
  async shared_catalog(){
   await reset();const old=hold('/projects/A');await tick();await waitHeld(old);await click('Сотрудники');await field('input[placeholder="ФИО"]','Synthetic employee');await click('Создать сотрудника');await wait('document.querySelector("[data-participant=e] h3")?.textContent==="Fresh synthetic employee"','newer list employees');await release(old);assert.equal(await evaluate('document.querySelector("[data-participant=e] h3").textContent'),'Fresh synthetic employee','old project poll cannot replace fresh list employee catalog');
  },
  async old_child_callback(){
   await reset();await click('Сотрудники');const oldWrite=hold('/employees',{method:'POST'});await field('input[placeholder="ФИО"]','Synthetic employee');await click('Создать сотрудника');await waitHeld(oldWrite);const next=hold('/projects/B');await chooseProject('B');await waitHeld(next);const lists=requests.filter(r=>r.endpoint==='/projects').length;await release(oldWrite);await release(next);assert.equal(requests.filter(r=>r.endpoint==='/projects').length,lists,'old child completion cannot adopt the new selection or issue fresh reads');assert.equal(await heading(),'Project B','old child callback cannot cancel the new project open');
  },
  async current_catalog_during_open(){
   await reset();const opening=hold('/projects/B');await chooseProject('B');await waitHeld(opening);const catalog=hold('/projects');await evaluate('void window.__fixtureRefreshLists()');await waitHeld(catalog);await release(opening);await release(catalog);await wait('document.querySelector(".project-header h1")?.textContent==="Project B"','current project read catches up after catalog');assert.equal(await heading(),'Project B');
  },
  async foreground_catalog_priority(){
   await reset();const catalog=hold('/projects');await evaluate('void window.__fixtureRefreshLists()');await waitHeld(catalog);const before=requests.filter(r=>r.endpoint==='/projects/A').length;await tick();await evaluate('window.__setHidden(true);window.__setHidden(false);window.__setOnline(false);window.__setOnline(true)');await settle();assert.equal(requests.filter(r=>r.endpoint==='/projects/A').length,before,'poll/reconnect cannot starve a current foreground catalog');await release(catalog);
  },
  async reverse_catalog(){
   await reset();await click('Сотрудники');const old=hold('/employees');await evaluate('void window.__fixtureRefreshLists()');await waitHeld(old);employeeName='Newest synthetic employee';const next=hold('/projects/A');await evaluate('void window.__fixtureLoadProject()');await waitHeld(next);await release(next);await wait('document.querySelector("[data-participant=e] h3")?.textContent==="Newest synthetic employee"','new current project employee batch');await release(old);assert.equal(await evaluate('document.querySelector("[data-participant=e] h3").textContent'),'Newest synthetic employee','old list cannot replace newer project employee catalog');
  },
  async lower_revision(){
   await reset();states.A=project('A',{revision:5,title:'Current revision'});await chooseProject('A');await wait('document.querySelector(".project-header h1")?.textContent==="Current revision"','revision5');states.A=project('A',{revision:4,title:'Stale revision'});const old=hold('/projects/A');await tick();await release(old);assert.equal(await heading(),'Current revision','lower revision rejected');states.A=project('A',{revision:5,title:'Current revision',calculation_state:'blocked_capacity'});const current=hold('/projects/A');await tick();await release(current);assert.equal(await evaluate('document.querySelector("header[data-calculation-state]").dataset.calculationState'),'blocked_capacity','current equal revision derived update allowed');
  },
  async bounded_background_visibility(){
   await reset();const old=hold('/projects/A');await tick();await waitHeld(old);const before=requests.filter(r=>r.endpoint==='/projects/A').length;await tick();await tick();assert.equal(requests.filter(r=>r.endpoint==='/projects/A').length,before,'one background batch while slow');await evaluate('window.__setHidden(true);window.__setOnline(false)');await tick();assert.equal(requests.filter(r=>r.endpoint==='/projects/A').length,before,'hidden/offline pause');states.A.title='Resumed project';await evaluate('window.__setHidden(false);window.__setOnline(true);window.__setOnline(true);window.__setHidden(false)');await settle();assert.equal(requests.filter(r=>r.endpoint==='/projects/A').length,before,'aborted background body still owns its slot');await release(old);await wait('document.querySelector(".project-header h1")?.textContent==="Resumed project"','coalesced visible catch-up');assert.equal(requests.filter(r=>r.endpoint==='/projects/A').length,before+1,'one catch-up after repeated reconnect');
  },
  async failed_open_reconnect(){
   await reset();await evaluate('window.__setOnline(false)');const failed=hold('/projects/B',{code:503});await chooseProject('B');await release(failed);assert.equal(await heading(),undefined);const before=requests.filter(r=>r.endpoint==='/projects/B').length;await tick();assert.equal(requests.filter(r=>r.endpoint==='/projects/B').length,before,'failed opening read does not retry offline');await evaluate('window.__setOnline(true);window.__setOnline(true)');await wait('document.querySelector(".project-header h1")?.textContent==="Project B"','reconnect recovers empty selected project');assert.equal(requests.filter(r=>r.endpoint==='/projects/B').length,before+1,'single reconnect read');assert(!await evaluate('!!document.querySelector(".global-error")'),'recovered read clears its own error');
  },
  async stale_operation(){
   await reset();const old=hold('/projects/A/undo',{method:'POST',code:409});await click('↶');await waitHeld(old);await chooseProject('B');await wait('document.querySelector(".project-header h1")?.textContent==="Project B"','B selected');await click('Задачи');await click('Task B');await field('dialog form input','B draft');const next=hold('/projects/B/preview',{method:'POST'});await click('Проверить и сохранить');await waitHeld(next);await release(old);assert(await evaluate('[...document.querySelectorAll("button")].find(b=>b.textContent.trim()==="↶").disabled'),'old finally cannot clear new busy state');assert(!await evaluate('document.body.textContent.includes("SYNTHETIC OLD OPERATION ERROR")'),'old mutation error cannot populate current selection');await release(next);await wait('document.querySelector("dialog h2")?.textContent==="Проверка изменений"','current preview');assert.equal(requests.filter(r=>r.endpoint==='/projects/A/undo'&&r.method==='POST').length,1,'failed mutation never retried');
  },
  async stale_commit(){
   await reset();await click('Задачи');await click('Task A');await field('dialog form input','Old A saved');await click('Проверить и сохранить');await wait('document.querySelector("dialog h2")?.textContent==="Проверка изменений"','A preview');const old=hold('/projects/A',{method:'PUT'});await click('Применить изменения');await waitHeld(old);await chooseProject('B');await wait('document.querySelector(".project-header h1")?.textContent==="Project B"','B while A writes');await release(old);assert.equal(await heading(),'Project B','old successful write cannot replace selected project');assert.equal(requests.filter(r=>r.endpoint==='/projects/A'&&r.method==='PUT').length,1,'accepted mutation never retried');
  },
  async stale_read_error(){
   await reset();const old=hold('/projects/A',{code:403});await chooseProject('A');await waitHeld(old);await chooseProject('B');await wait('document.querySelector(".project-header h1")?.textContent==="Project B"','B before stale error');await release(old);assert.equal(await heading(),'Project B');assert(!await evaluate('document.body.textContent.includes("SYNTHETIC OLD OPERATION ERROR")'),'stale read error cannot leak into B');assert(await evaluate('[...document.querySelectorAll("button")].some(b=>b.textContent.trim()==="Настройки проекта")'),'stale read cannot replace B role');
  },
  async drafts_and_preview(){
   await reset();const old=hold('/projects/A');await tick();await waitHeld(old);await click('Задачи');await click('Task A');await field('dialog form input','Retained draft');await field('dialog input[type=number]',17);await release(old);assert.equal(await evaluate('document.querySelector("dialog form input").value'),'Retained draft');assert.equal(await evaluate('document.querySelector("dialog input[type=number]").value'),'17');await click('Проверить и сохранить');await wait('document.querySelector("dialog h2")?.textContent==="Проверка изменений"','preview remains a draft');const count=requests.filter(r=>r.endpoint==='/projects/A').length;await tick();await tick();assert.equal(requests.filter(r=>r.endpoint==='/projects/A').length,count,'preview pauses background');assert.equal(states.A.tasks[0].title,'Task A','preview did not persist task draft');await click('Отмена');assert.equal(states.A.tasks[0].planned_minutes,60,'cancel retains authoritative hours');
  },
  async poll_after_write(){
   await reset();const old=hold('/projects/A');await tick();await waitHeld(old);await click('Задачи');await click('Task A');await field('dialog form input','Saved task revision2');await click('Проверить и сохранить');await wait('document.querySelector("dialog h2")?.textContent==="Проверка изменений"','preview before save');await click('Применить изменения');await wait('!document.querySelector("dialog[open]")','write acknowledged');assert.equal(states.A.revision,2);await release(old);assert(await evaluate('document.querySelector("table").textContent.includes("Saved task revision2")'),'old poll cannot visually revert saved task');assert.equal(states.A.tasks[0].title,'Saved task revision2');
  },
  async archive_next_project(){
   await reset();await click('Настройки проекта');await click('Удалить проект…');await field('.danger-zone input','удалить');const list=hold('/projects');await click('Удалить и переместить в корзину');await waitHeld(list);await release(list);await wait('document.querySelector(".project-header h1")?.textContent==="Project B"','next unarchived project');const deletedAt=requests.findIndex(r=>r.endpoint==='/projects/A'&&r.method==='DELETE');assert(deletedAt>=0,'actual archive issued');assert.equal(requests.slice(deletedAt+1).filter(r=>r.endpoint==='/projects/A'&&r.method==='GET').length,0,'archived project is not reopened from stale sidebar');
  }
 };
 const requested=process.argv.indexOf('--case'),names=requested>=0?[process.argv[requested+1]]:Object.keys(tests),failures=[];
 for(const name of names){assert(tests[name],'known scenario');scenario=name;try{await tests[name]();console.log('Workspace refresh browser PASS: '+name)}catch(error){failures.push(name);console.error('Workspace refresh browser FAIL: '+name+'; '+error.message)}}
 assert.deepEqual(browserErrors,[],'no browser exceptions');assert.deepEqual(failures,[],'workspace refresh regression cases');
}
const deadline=setTimeout(()=>{console.error('Workspace refresh harness timeout');chrome?.kill('SIGKILL');process.exit(1)},90000);
async function stopChrome(){if(!chrome?.pid||chrome.exitCode!==null||chrome.signalCode!==null)return;await new Promise((resolve,reject)=>{const force=setTimeout(()=>chrome.kill('SIGKILL'),4000),deadline=setTimeout(()=>{chrome.removeListener('close',closed);clearTimeout(force);reject(Error('Chrome cleanup timeout'))},8000);function closed(){clearTimeout(force);clearTimeout(deadline);resolve()}chrome.once('close',closed);chrome.kill('SIGTERM')})}
main().catch(error=>{console.error(error.message);process.exitCode=1}).finally(async()=>{clearTimeout(deadline);for(const p of pending.values())clearTimeout(p.timer);for(const gate of held)gate.release();socket?.close();server?.closeAllConnections();server?.close();await stopChrome();if(profile)await fs.rm(profile,{recursive:true,force:true,maxRetries:8,retryDelay:150})}).catch(()=>{console.error('Workspace refresh cleanup failed');process.exitCode=1});
