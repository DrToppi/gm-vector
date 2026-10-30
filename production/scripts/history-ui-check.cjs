// Synthetic React history/clock/portable regressions. No live integration/data.
const fs=require('node:fs/promises'),path=require('node:path'),os=require('node:os'),http=require('node:http'),assert=require('node:assert/strict'),{spawn}=require('node:child_process');
const {build}=require('../web/node_modules/esbuild'),web=path.resolve(__dirname,'../web');
const sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
let server,chrome,socket,profile,seq=0,requests=[],caseName='',revision=1,role='manager',created=false,exportOverride=null,dataOverride=null,delivery=0,firstGeneration=0;
const pending=new Map(),gates=[],held=[];
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
const task=(id,parent='',summary=false)=>({id,parent,title:'Synthetic '+id,description:'',result:'',brief:'',status:'todo',requested_start:'2026-10-29',start:'2026-10-29',end:'2026-10-29',duration_days:1,planned_minutes:60,actual_minutes:60,legacy_actual_minutes:id==='root'?7:id==='old'?3:0,responsibility:'internal',customer_owner:'',customer_due:'',owner_legacy:'',legacy_start:'',legacy_end:'',assignments:summary?[]:[{employee_id:'e',daily_minutes:60}],summary,blocked:false});
const project={id:'p',title:'Synthetic project',brief:'',start:'2026-10-29',duration_mode:'working',calendar_id:'fixture',revision:1,tasks:[task('root','',true),task('old','root'),task('recent','root')],links:[],people:[{employee_id:'e',week:{'4':{active:true,start:'09:00',end:'10:00',daily_minutes:60}},exceptions:{}}],calculation_state:'ready',warnings:[],loads:[]};
const rows=[...Array.from({length:1000},(_,i)=>({id:'recent-'+String(i).padStart(4,'0'),task_id:'recent',employee_id:'e',date:'2026-10-29',minutes:60,comment:'Recent '+i})),{id:'old-entry',task_id:'old',employee_id:'',date:'2025-01-01',minutes:60,comment:'Retained old fact'},{id:'root-entry',task_id:'root',employee_id:'e',date:'2024-01-01',minutes:60,comment:'Root own fact'}];
const common=filters=>{const selected=(dataOverride||rows).filter(r=>(!filters.task_id||filters.task_id==='root'||r.task_id===filters.task_id)&&(!filters.employee_id||r.employee_id===filters.employee_id)&&(!filters.date_from||r.date>=filters.date_from)&&(!filters.date_to||r.date<=filters.date_to));return {selected,project_id:'p',project_title:project.title,revision,planning_timezone:'America/New_York',filters,order:'work_date_desc_id_asc',total_count:selected.length,recorded_minutes:selected.reduce((n,r)=>n+r.minutes,0),legacy_unattributed:{minutes:filters.task_id==='old'?3:10,scope:filters.task_id?'task_subtree':'project',employee_date_attribution:'unavailable'}}};
const label=r=>({...r,task_title:'Synthetic '+r.task_id,employee_name:r.employee_id?'Synthetic employee':''});
const send=(method,params={})=>new Promise((resolve,reject)=>{const id=++seq,timer=setTimeout(()=>{pending.delete(id);reject(Error('CDP timeout'))},10000);pending.set(id,{resolve,reject,timer});socket.send(JSON.stringify({id,method,params}))});
const evaluate=async expression=>{const r=await send('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});if(r.exceptionDetails)throw Error('synthetic browser evaluation failed');return r.result.value};
async function wait(expression,label){for(let i=0;i<200;i++){if(await evaluate(expression))return;await sleep(25)}throw Error('condition timeout '+label)}
async function settle(){await evaluate('new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))')}
async function click(text){const q='[...document.querySelectorAll("button")].find(b=>b.textContent.trim()==='+JSON.stringify(text)+')';await wait('!!('+q+')&&!('+q+').disabled','button');await evaluate('('+q+').click()');await settle()}
async function reset(options={}){for(const gate of held.splice(0))gate.release();gates.length=0;requests=[];revision=1;firstGeneration=0;role=options.member?'member':'manager';created=false;exportOverride=null;dataOverride=null;await send('Page.navigate',{url:'http://127.0.0.1:'+server.address().port+(options.rejectZone?'/?rejectZone=1':'')});try{await wait('document.querySelector(".project-header h1")?.textContent==="Synthetic project"','project')}catch(error){console.error('Fixture readiness diagnostic: '+JSON.stringify({routes:requests.map(r=>r.route),headingCount:await evaluate('document.querySelectorAll("h1").length')}));throw error}await settle()}
async function openTask(title){await click('Задачи');await click(title);await wait('!!document.querySelector("[data-task-effort]")','task effort');await wait('!!document.querySelector("[data-history-totals]")','history data');await settle()}
async function field(selector,value){await evaluate('(()=>{const x=document.querySelector('+JSON.stringify(selector)+');Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,"value").set.call(x,'+JSON.stringify(value)+');x.dispatchEvent(new Event("input",{bubbles:true}));})()');await settle()}
function hold(cursor,status=200,code='history_changed'){const gate={cursor,status,code};gates.push(gate);return gate}
async function waitHeld(gate){for(let i=0;i<200&&!gate.release;i++)await sleep(10);assert(gate.release,'history body held')}
async function release(gate){await waitHeld(gate);gate.release();await wait('window.__deliveries.includes('+gate.delivery+')','held JSON parsed');await settle()}
async function screenshot(name){
 if(!process.argv.includes('--screenshots'))return;
 const response=await send('Page.captureScreenshot',{format:'png'});await fs.writeFile('/tmp/gm-vector-wp04-'+name+'.png',Buffer.from(response.data,'base64'),{mode:0o600});
}
async function main(){
 const bundled=(await build({entryPoints:[path.join(web,'src/main.tsx')],bundle:true,platform:'browser',format:'iife',write:false,loader:{'.css':'empty'}})).outputFiles[0].text;
 const styles=(await Promise.all(['style.css','history.css','comments.css','updates.css'].map(name=>fs.readFile(path.join(web,'src',name),'utf8')))).join('\n');
 server=http.createServer(async(req,res)=>{
  res.setHeader('Cache-Control','no-store');const u=new URL(req.url,'http://localhost'),route=u.pathname;
  if(route==='/fixture.js'){res.setHeader('Content-Type','application/javascript');return res.end(bundled)}
  if(route==='/fixture.css'){res.setHeader('Content-Type','text/css');return res.end(styles)}
  if(!route.startsWith('/api/')){res.setHeader('Content-Type','text/html; charset=utf-8');return res.end('<link rel="stylesheet" href="/fixture.css"><div id="root"></div><script src="/fixture.js"></script>')}
  let raw='';for await(const chunk of req)raw+=chunk;const body=raw?JSON.parse(raw):null;requests.push({route,query:u.search,method:req.method,body});
  let out=[],status=200;
  if(route==='/api/v1/auth/me')out={user:{id:'user',name:'Synthetic administrator',admin:role==='manager',active:true,locale:'ru',employee_id:''},csrf:'synthetic',planning_timezone:'America/New_York'};
  else if(route==='/api/v1/projects')out=[{id:'p',title:project.title,task_count:3},...(created?[{id:'imported',title:'Imported project',task_count:3}]:[])];
  else if(route==='/api/v1/projects/p'||route==='/api/v1/projects/imported')out={project:{...project,revision,...(route.endsWith('/imported')?{id:'imported',title:'Imported project'}:{})},role};
  else if(route==='/api/v1/employees')out=[{id:'e',name:'Synthetic employee',daily_minutes:480,user_id:'',provider:'',external_id:''}];
  else if(route==='/api/v1/calendars')out=[{id:'fixture',year:2026,title:'Synthetic',days:{'2026-10-29':{working:true}}}];
  else if(route==='/api/v1/projects/p/time-entries')out=rows.slice(0,1000);
  else if(route==='/api/v1/projects/p/time-history'||route==='/api/v1/projects/p/time-history/export'){
   const filters=Object.fromEntries(['task_id','employee_id','date_from','date_to'].map(k=>[k,u.searchParams.get(k)||''])),{selected,...base}=common(filters);
   if(route.endsWith('/export'))out={...base,format:'gm-vector-time-history',schema_version:1,generated_at:'2026-10-30T01:00:00Z',items:exportOverride||selected.map(label),...(exportOverride?{total_count:exportOverride.length,recorded_minutes:exportOverride.reduce((n,r)=>n+r.minutes,0)}:{})};
   else{const limit=Number(u.searchParams.get('limit')||100),token=u.searchParams.get('cursor'),match=token&&/^r(\d+)(?:-g(\d+))?-page-(\d+)$/.exec(token),offset=match?Number(match[3]):0,chain=match?match[2]:++firstGeneration;out={...base,limit,items:selected.slice(offset,offset+limit).map(label),next_cursor:offset+limit<selected.length?'r'+revision+'-g'+chain+'-page-'+(offset+limit):null};if(match&&Number(match[1])!==revision){status=409;out={error:{code:'history_changed',message:'Synthetic changed history'}}}}
  }else if(route.endsWith('/comments'))out={comments:[],can_comment:false,email_enabled:false,customer:false,customer_email:''};
  else if(route==='/api/v1/diagnostics/key')out={configured:false};
  else if(route==='/api/v1/projects/p/export')out=project;
  else if(route==='/api/v1/projects/p/portable/export')out={format:'gm-vector-project',schema_version:1,metadata:{planning_timezone:'America/New_York',generated_at:'2026-10-30T01:00:00Z'},project};
  else if(route==='/api/v1/import'){if(body.project.format){status=422;out={error:{code:'validation',message:'Synthetic legacy importer rejects portable envelope'}}}else out={project,message:'Synthetic legacy preview'}}
  else if(route==='/api/v1/import/portable'){
   if(body.preview)out={project,message:'Synthetic preview',source_planning_timezone:body.document.metadata.planning_timezone,target_planning_timezone:'America/New_York',timezone_acknowledgement_required:body.document.metadata.planning_timezone!=='America/New_York'};
   else if(body.document.metadata.planning_timezone!=='America/New_York'&&JSON.stringify(body.timezone_acknowledgement)!==JSON.stringify({source:body.document.metadata.planning_timezone,target:'America/New_York'})){status=422;out={error:{code:'validation',message:'Synthetic acknowledgement mismatch'}}}
   else{created=true;out={id:'imported'}}
  }
  const gate=route==='/api/v1/projects/p/time-history'?gates.find(g=>!g.release&&g.cursor===(u.searchParams.get('cursor')||'').replace(/-g\d+/,'')):null;
  if(gate?.status!==undefined&&gate.status!==200){status=gate.status;out={error:{code:gate.code,message:'Synthetic held history error'}}}
  res.statusCode=status;res.setHeader('Content-Type','application/json');res.setHeader('X-Fixture-Delivery',String(++delivery));res.flushHeaders();
  const response=JSON.stringify(out);
  if(gate){gate.delivery=delivery;gate.release=()=>{if(!gate.released){gate.released=true;res.end(response)}};held.push(gate)}else res.end(response);
 });await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 profile=await fs.mkdtemp(path.join(os.tmpdir(),'gm-vector-history-ui-'));
 chrome=spawn(process.env.GMA_CHROME_BIN||'google-chrome',['--headless=new','--no-first-run','--disable-background-networking','--disable-component-update','--disable-sync','--remote-debugging-port=0','--remote-debugging-address=127.0.0.1','--user-data-dir='+profile,'about:blank'],{stdio:['ignore','ignore','pipe']});
 const port=await chromePort(chrome,profile);
 const pages=await(await fetch('http://127.0.0.1:'+port+'/json')).json();socket=new WebSocket(pages.find(p=>p.type==='page').webSocketDebuggerUrl);await new Promise(resolve=>socket.onopen=resolve);
 socket.onmessage=e=>{const m=JSON.parse(e.data);if(m.method==='Runtime.exceptionThrown'){const x=m.params.exceptionDetails;console.error('Fixture exception: '+(x.exception?.className||'Error')+'; line='+x.lineNumber)}if(m.id){const p=pending.get(m.id);if(p){pending.delete(m.id);clearTimeout(p.timer);m.error?p.reject(Error('CDP failed')):p.resolve(m.result)}}};
 await send('Page.enable');await send('Runtime.enable');await send('Page.addScriptToEvaluateOnNewDocument',{source:'{localStorage.setItem("gm-vector.locale","ru");window.__fixtureNow="2026-10-30T01:00:00Z";const RealDate=Date;window.Date=class extends RealDate{constructor(...args){super(...(args.length?args:[window.__fixtureNow]))}static now(){return RealDate.parse(window.__fixtureNow)}};window.__deliveries=[];const real=fetch.bind(window);window.fetch=async(url,options={})=>{const {signal,...rest}=options;const response=await real(url,rest),json=response.json.bind(response);response.json=async()=>{const body=await json();window.__deliveries.push(Number(response.headers.get("X-Fixture-Delivery")));return body};return response};window.__downloads=[];const blobs=new Map(),create=URL.createObjectURL.bind(URL);URL.createObjectURL=blob=>{const url=create(blob);blobs.set(url,blob);return url};HTMLAnchorElement.prototype.click=function(){const name=this.download;void blobs.get(this.href)?.text().then(text=>window.__downloads.push({name,text}))};if(new URL(location.href).searchParams.has("rejectZone")){const original=Intl.DateTimeFormat;Intl.DateTimeFormat=new Proxy(original,{construct(target,args){if(args[1]?.timeZone==="America/New_York")throw new RangeError("Synthetic unsupported zone");return Reflect.construct(target,args)}})}}'});
 await send('Emulation.setDeviceMetricsOverride',{width:1440,height:1800,deviceScaleFactor:1,mobile:false});
 const tests={
  async old_task(){await reset();await openTask('Synthetic old');await wait('!!document.querySelector("[data-task-effort] ul")','history');assert(await evaluate('document.querySelector("[data-task-effort]").textContent.includes("Retained old fact")'),'record beyond project1000 must be visible')},
  async milestone(){await reset();await openTask('Synthetic root');assert(await evaluate('document.querySelector("[data-task-effort]").textContent.includes("Recent 0")'),'root-inclusive history must show descendant records')},
  async planning_zone(){await reset();await click('＋ Новый проект');assert.equal(await evaluate('document.querySelector("dialog input[type=date]").value'),'2026-10-29','configured NewYork day must replace Moscow default')},
  async portable(){await reset();await click('＋ Новый проект');const document={format:'gm-vector-project',schema_version:1,metadata:{planning_timezone:'UTC',generated_at:'2026-10-30T00:00:00Z'},project};await evaluate('(()=>{const input=document.querySelector(".new-project-import input[type=file]"),transfer=new DataTransfer();transfer.items.add(new File(['+JSON.stringify(JSON.stringify(document))+'],"synthetic-portable.json",{type:"application/json"}));input.files=transfer.files;input.dispatchEvent(new Event("change",{bubbles:true}));})()');await wait('!!document.querySelector("[data-timezone-ack]")','portable preview');assert(requests.some(r=>r.route==='/api/v1/import/portable'),'portable format must use its strict endpoint');assert(await evaluate('document.querySelector(".new-project-import .toolbar button").disabled'),'mismatch blocks import before acknowledgement');await evaluate('document.querySelector("[data-timezone-ack]").click()');await settle();await evaluate('document.querySelector(".new-project-import .toolbar button").click()');await wait('document.querySelector(".project-header h1")?.textContent==="Imported project"','portable imported');const request=requests.find(r=>r.route==='/api/v1/import/portable'&&r.body.preview===false);assert.deepEqual(request.body.timezone_acknowledgement,{source:'UTC',target:'America/New_York'});assert.equal(request.body.document.project.start,'2026-10-29','portable labels not converted')},
  async pages_and_totals(){
   await reset();await openTask('Synthetic root');const seen=new Set();
   for(let page=0;page<11;page++){const ids=await evaluate('[...document.querySelectorAll(".task-history [data-time-entry]")].map(x=>x.dataset.timeEntry)');assert(ids.length<=100,'bounded displayed page');for(const id of ids){assert(!seen.has(id),'no duplicate '+id);seen.add(id)}assert(await evaluate('document.querySelector("[data-history-totals]").textContent.includes("1002")'),'totals independent of loaded pages');if(page<10){await evaluate('document.querySelector("[data-history-next]").click()');await wait('!!document.querySelector("[data-history-totals]")&&!document.querySelector(".task-history [role=status]")','next page');await settle()}}
   assert.equal(seen.size,1002);assert(seen.has('root-entry')&&seen.has('old-entry'));assert(await evaluate('document.querySelector("[data-history-legacy]").textContent.includes("0.17")'),'raw root-inclusive legacy10minutes not derived from Actual');
  },
  async previous_and_original_chain(){
   await reset();await openTask('Synthetic root');await evaluate('document.querySelector("[data-history-next]").click()');await wait('!!document.querySelector("[data-time-entry=recent-0100]")','second page oracle');const original=new URLSearchParams(requests.filter(r=>r.route.endsWith('/time-history')&&r.query.includes('cursor=')).at(-1).query).get('cursor');assert(!await evaluate('document.querySelector("[data-history-previous]").disabled'));await evaluate('document.querySelector("[data-history-previous]").click()');await wait('!!document.querySelector("[data-time-entry=recent-0000]")','Previous returns first page');await evaluate('document.querySelector("[data-history-next]").click()');await wait('!!document.querySelector("[data-time-entry=recent-0100]")','next after return');assert.equal(new URLSearchParams(requests.filter(r=>r.route.endsWith('/time-history')&&r.query.includes('cursor=')).at(-1).query).get('cursor'),original,'back navigation does not renew original continuation expiry');
  },
  async bounded_back_positions(){
   await reset();dataOverride=Array.from({length:10300},(_,i)=>({...rows[0],id:'nav-'+String(i).padStart(5,'0')}));await openTask('Synthetic root');
   for(let n=1;n<=101;n++){await evaluate('document.querySelector("[data-history-next]").click()');await wait('!!document.querySelector("[data-time-entry=nav-'+String(n*100).padStart(5,'0')+']")','forward page '+n);if(n===100)assert(!await evaluate('!!document.querySelector("[data-history-back-discarded]")'),'100 positions fit')}
   assert(await evaluate('!!document.querySelector("[data-history-back-discarded]")'),'101st position reports discard');assert.equal(await evaluate('document.querySelectorAll(".task-history [data-time-entry]").length'),100,'forward traversal remains bounded/complete');await click('Первая страница');await wait('!!document.querySelector("[data-time-entry=nav-00000]")','First always available');assert(!await evaluate('!!document.querySelector("[data-history-back-discarded]")'));
  },
  async filters_and_unknown(){
   await reset();await openTask('Synthetic root');await field('[data-history-filter=date_from]','2025-01-01');await field('[data-history-filter=date_to]','2025-12-31');await click('Применить фильтры');await wait('!!document.querySelector("[data-time-entry=old-entry]")','old historical date row');assert.equal(await evaluate('document.querySelectorAll(".task-history [data-time-entry]").length'),1);assert(await evaluate('document.querySelector("[data-time-entry=old-entry]").textContent.includes("Неизвестный сотрудник")'));assert(await evaluate('document.querySelector("[data-history-legacy]").textContent.includes("0.17")'),'legacy unaffected by date filters');await evaluate('(()=>{const x=document.querySelector("[data-history-filter=employee_id]");Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype,"value").set.call(x,"e");x.dispatchEvent(new Event("change",{bubbles:true}));})()');await settle();await click('Применить фильтры');await wait('!!document.querySelector("[data-history-totals]")&&!document.querySelector("[data-time-entry]")','empty filtered history');assert(await evaluate('document.querySelector("[data-history-legacy]").textContent.includes("0.17")'));
  },
  async stale_page(){
   await reset();await openTask('Synthetic root');const old=hold('r1-page-100');await evaluate('document.querySelector("[data-history-next]").click()');await waitHeld(old);const next=hold('');await field('[data-history-filter=date_from]','2025-01-01');await field('[data-history-filter=date_to]','2025-12-31');await click('Применить фильтры');await waitHeld(next);await release(old);assert(await evaluate('!!document.querySelector(".task-history [role=status]")'),'stale finally does not clear current loading');await release(next);assert.equal(await evaluate('document.querySelectorAll(".task-history [data-time-entry]").length'),1);assert(await evaluate('!!document.querySelector("[data-time-entry=old-entry]")'));
  },
  async stale_page_error(){
   await reset();await openTask('Synthetic root');const old=hold('r1-page-100',422,'cursor_expired');await evaluate('document.querySelector("[data-history-next]").click()');await waitHeld(old);await field('[data-history-filter=date_from]','2025-01-01');await field('[data-history-filter=date_to]','2025-12-31');await click('Применить фильтры');await wait('!!document.querySelector("[data-time-entry=old-entry]")','fresh filtered page');await release(old);assert(!await evaluate('!!document.querySelector(".task-history .error")'),'stale error ignored');
  },
  async changed_preserves_draft(){
   await reset();await openTask('Synthetic recent');await field('dialog form input','Retained title draft');await field('dialog input[max="1000000"]','17');revision=2;await evaluate('document.querySelector("[data-history-next]").click()');await wait('!!document.querySelector(".task-history .error")','history changed');assert.equal(await evaluate('document.querySelector("dialog form input").value'),'Retained title draft');assert.equal(await evaluate('document.querySelector("dialog input[max=\\"1000000\\"]").value'),'17');await click('Загрузить первую страницу');await wait('!!document.querySelector("[data-history-totals]")','restarted history');assert.equal(await evaluate('document.querySelector("dialog form input").value'),'Retained title draft');
  },
  async midnight_and_date_draft(){
   await reset();await openTask('Synthetic recent');await field('[aria-label="Дата записи времени"]','2025-11-11');await evaluate('window.__fixtureNow="2026-10-30T04:30:00Z"');await field('dialog form input','Midnight draft');assert.equal(await evaluate('document.querySelector("[aria-label=\\"Дата записи времени\\"]").value'),'2025-11-11','midnight does not overwrite date draft');await evaluate('document.querySelector("dialog header button").click()');await click('Сотрудники');await click('Задачи');assert(await evaluate('[...document.querySelectorAll("[data-task-status]")].every(x=>x.dataset.taskStatus==="overdue")'),'overdue uses current configured day');await click('＋ Новый проект');assert.equal(await evaluate('document.querySelector("dialog input[type=date]").value'),'2026-10-30');
  },
  async unsupported_zone(){
   await reset({rejectZone:true});assert(await evaluate('document.querySelector("[data-planning-zone-error]")?.textContent.includes("America/New_York")'),'unsupported Intl zone visible');await click('＋ Новый проект');assert.equal(await evaluate('document.querySelector("dialog input[type=date]").value'),'');assert(await evaluate('document.querySelector("dialog form button").disabled'),'no silent planning fallback');
  },
  async null_employee_ownership(){
   await reset({member:true});await openTask('Synthetic old');assert(await evaluate('document.querySelector("[data-time-entry=old-entry]").textContent.includes("Неизвестный сотрудник")'));assert(!await evaluate('!!document.querySelector("[data-time-entry=old-entry] button")'),'two empty IDs never imply ownership');
  },
  async complete_export(){
   await reset();await openTask('Synthetic root');assert.equal(await evaluate('document.querySelectorAll(".task-history [data-time-entry]").length'),100);await evaluate('document.querySelector(".task-history [data-history-export=json]").click()');await wait('window.__downloads.length===1','complete history download');const download=JSON.parse(await evaluate('window.__downloads[0].text'));assert.equal(download.items.length,1002);assert.equal(download.total_count,1002);assert.equal(download.format,'gm-vector-time-history');assert.equal(requests.filter(r=>r.route.endsWith('/time-history/export')).length,1);assert(!requests.some(r=>r.route.endsWith('/time-history')&&r.query.includes('cursor=')),'export is not cursor traversal');
  },
  async pdf_guard_before_dom(){
   await reset();await openTask('Synthetic root');exportOverride=Array.from({length:10001},(_,i)=>label({...rows[0],id:'export-'+i,comment:'Short'}));await evaluate('window.__printWrites=0;window.__printClosed=false;window.open=()=>({closed:false,opener:null,document:{open(){window.__printWrites++},write(){window.__printWrites++},close(){}},focus(){},print(){},close(){this.closed=true;window.__printClosed=true}})');await evaluate('document.querySelector(".task-history [data-history-export=pdf]").click()');await wait('!!document.querySelector(".task-history .history-export .error")','PDF conversion limit');assert.equal(await evaluate('window.__printWrites'),0);assert(await evaluate('window.__printClosed'));
  },
  async pdf_ten_thousand(){
   await reset();await openTask('Synthetic root');exportOverride=Array.from({length:10000},(_,i)=>label({...rows[0],id:'export-'+i,comment:'Short'}));await evaluate('window.__printedRows=0;window.open=()=>{const doc=document.implementation.createHTMLDocument("Synthetic print");return{closed:false,opener:null,document:doc,focus(){},print(){window.__printedRows=doc.querySelectorAll("tbody tr").length},close(){this.closed=true}}}');await evaluate('document.querySelector(".task-history [data-history-export=pdf]").click()');await wait('window.__printedRows===10000','ordinary complete10k PDF DOM');
  },
  async pdf_owned_windows(){
   for(const ending of ['logout','unmount']){
    await reset();await openTask('Synthetic root');
    await evaluate('window.__ownedPrints=[];window.open=()=>{const doc=document.implementation.createHTMLDocument("Synthetic owned print");const owned={closed:false,opener:null,document:doc,focus(){},print(){this.printed=true},close(){this.closed=true}};window.__ownedPrints.push(owned);return owned}');
    await evaluate('document.querySelector(".task-history [data-history-export=pdf]").click()');await wait('window.__ownedPrints[0]?.printed','first PDF printed');
    await evaluate('document.querySelector(".task-history [data-history-export=csv]").click()');await wait('window.__downloads.length===1','CSV after PDF');
    await evaluate('document.querySelector(".task-history [data-history-export=pdf]").click()');await wait('window.__ownedPrints[1]?.printed','second PDF printed');
    assert.equal(await evaluate('window.__ownedPrints.filter(x=>!x.closed).length'),2,'two print windows remain user-owned until close');
    if(ending==='logout')await click('Выйти');else await evaluate('document.querySelector("dialog header button").click()');
    await settle();assert(await evaluate('window.__ownedPrints.every(x=>x.closed)'),ending+' closes every previous-session print window after PDF→CSV→PDF');
   }
  },
  async seven_locales(){
   await reset();await openTask('Synthetic old');for(const locale of ['en','ru','de','fr','es','pt-BR','zh-Hans']){await evaluate('(()=>{const x=document.querySelector(".language-picker select");Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype,"value").set.call(x,'+JSON.stringify(locale)+');x.dispatchEvent(new Event("change",{bubbles:true}));})()');await wait('document.documentElement.lang==='+JSON.stringify(locale),'locale');const catalog=JSON.parse(await fs.readFile(path.join(web,'src/locales',locale+'.json'),'utf8'));await wait('document.querySelector("[data-time-entry=old-entry]")?.textContent.includes('+JSON.stringify(catalog['history.unknownEmployee'])+')','localized unknown employee');assert(await evaluate('document.querySelector("[data-history-legacy]").textContent.includes('+JSON.stringify(catalog['history.legacy'])+')'))}
  },
  async screenshots(){
   if(!process.argv.includes('--screenshots'))return;await reset();await openTask('Synthetic old');await evaluate('document.querySelector(".task-history").scrollIntoView({block:"center"})');await settle();await screenshot('task-history');await reset();await click('＋ Новый проект');const document={format:'gm-vector-project',schema_version:1,metadata:{planning_timezone:'UTC',generated_at:'2026-10-30T00:00:00Z'},project};await evaluate('(()=>{const input=document.querySelector(".new-project-import input[type=file]"),transfer=new DataTransfer();transfer.items.add(new File(['+JSON.stringify(JSON.stringify(document))+'],"synthetic-portable.json",{type:"application/json"}));input.files=transfer.files;input.dispatchEvent(new Event("change",{bubbles:true}));})()');await wait('!!document.querySelector("[data-timezone-ack]")','zone confirmation screenshot');await screenshot('portable-confirmation');
  }
 };
 const index=process.argv.indexOf('--case'),names=index>=0?[process.argv[index+1]]:Object.keys(tests),failures=[];
 for(const name of names){caseName=name;try{await tests[name]();console.log('History UI PASS: '+name)}catch(e){failures.push(name);console.error('History UI FAIL: '+name+'; '+e.message)}}
 assert.deepEqual(failures,[],'history UI regression cases');
}
const deadline=setTimeout(()=>{console.error('History UI timeout');chrome?.kill('SIGKILL');process.exit(1)},90000);
async function stopChrome(){if(!chrome?.pid||chrome.exitCode!==null||chrome.signalCode!==null)return;await new Promise(resolve=>{const force=setTimeout(()=>chrome.kill('SIGKILL'),4000);chrome.once('close',()=>{clearTimeout(force);resolve()});chrome.kill('SIGTERM')})}
main().catch(e=>{console.error(e.message);process.exitCode=1}).finally(async()=>{clearTimeout(deadline);for(const p of pending.values())clearTimeout(p.timer);for(const gate of held)gate.release();socket?.close();server?.closeAllConnections();server?.close();await stopChrome();if(profile)await fs.rm(profile,{recursive:true,force:true,maxRetries:8,retryDelay:150})});
