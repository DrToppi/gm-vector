// Real built React UI with synthetic loopback API; never connects to production.
const fs=require('node:fs/promises'),path=require('node:path'),http=require('node:http'),os=require('node:os'),{spawn}=require('node:child_process'),assert=require('node:assert/strict');
const root=path.resolve(__dirname,'../..'),sleep=ms=>new Promise(r=>setTimeout(r,ms));
let chrome,socket,server,profile,identity='A',scenario='',armed=false,held=[],seq=0,sameLogin=false;
const pending=new Map(),errors=[],requests=[];
const marker='SYNTHETIC-OLD-A-PRIVATE';
const project=(old=false)=>({id:'synthetic-a',title:old?marker:'Current synthetic project',brief:'',revision:1,tasks:[],people:[],links:[],loads:[],warnings:[],start:'2026-10-28',duration_mode:'calendar',calculation_state:'ready',calendar_id:'synthetic-calendar'});
function holdRoute(route,method){return armed&&identity==='A'&&((scenario==='list'&&route==='/projects')||(['project','error','same'].includes(scenario)&&route==='/projects/synthetic-a'&&method==='GET')||(scenario==='save'&&route==='/projects/synthetic-a'&&method==='PUT')||(['export','print'].includes(scenario)&&route==='/projects/synthetic-a/export'))}
async function main(){
 profile=await fs.mkdtemp(path.join(os.tmpdir(),'gma-identity-regression-'));
 server=http.createServer(async(req,res)=>{
  const pathname=new URL(req.url,'http://localhost').pathname;res.setHeader('Cache-Control','no-store');
  const json=(v,status=200)=>{res.statusCode=status;res.setHeader('Content-Type','application/json');res.end(JSON.stringify(v))};
  if(pathname.startsWith('/api/v1/')){
   const route=pathname.slice(7);requests.push({identity,route,method:req.method});
   if(holdRoute(route,req.method)){armed=false;held.push(()=>json(scenario==='error'?{error:{message:marker}}:scenario==='list'?[{id:'synthetic-a',title:marker,task_count:0}]:['save','export','print'].includes(scenario)?project(true):{project:project(true),role:'manager'},scenario==='error'?403:200));return}
   if(route==='/auth/me')return identity?json({user:{id:identity,name:'Synthetic '+identity,admin:true},csrf:'synthetic'}):json({error:{message:'login'}},401);
   if(route==='/auth/logout'){identity='';return json({})}
   if(route==='/auth/login'){identity=sameLogin?'A':'B';return json({})}
   if(route==='/auth/status')return json({setup_required:false});
   if(route==='/projects')return json(identity==='A'?[{id:'synthetic-a',title:'Current synthetic project',task_count:0}]:[]);
   if(route==='/projects/synthetic-a')return json({project:project(),role:'manager'});
   if(route==='/projects/synthetic-a/preview')return json(project());
   if(route==='/projects/synthetic-a/export')return json(project());
   if(route==='/calendars')return json([{id:'synthetic-calendar',title:'Synthetic',year:2026,days:{}}]);
   if(route==='/diagnostics/key')return json({configured:false});
   if(route==='/import')return json({message:'synthetic preview'});
   return json([]);
  }
  try{const rel=pathname==='/'?'index.html':pathname.slice(1);assert(!rel.includes('..'));const data=await fs.readFile(path.join(root,'production/web/dist',rel));res.setHeader('Content-Type',rel.endsWith('.js')?'text/javascript':rel.endsWith('.css')?'text/css':'text/html');res.end(data)}catch{res.statusCode=404;res.end()}
 });await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin='http://127.0.0.1:'+server.address().port;
 chrome=spawn(process.env.GMA_CHROME_BIN||'google-chrome',['--headless=new','--no-first-run','--disable-background-networking','--disable-component-update','--disable-sync','--remote-debugging-port=0','--remote-debugging-address=127.0.0.1','--user-data-dir='+profile,'about:blank'],{stdio:'ignore'});let launchError=false;chrome.on('error',()=>launchError=true);
 let port;for(let i=0;i<150;i++){if(launchError||chrome.exitCode!==null)throw Error('Chrome unavailable');try{port=Number((await fs.readFile(path.join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]);if(port)break}catch{}await sleep(100)}assert(port,'fresh Chrome start');
 const pages=await(await fetch('http://127.0.0.1:'+port+'/json/list')).json();socket=new WebSocket(pages.find(p=>p.type==='page').webSocketDebuggerUrl);await new Promise((r,j)=>{socket.onopen=r;socket.onerror=j});
 socket.onmessage=e=>{const m=JSON.parse(e.data);if(m.id){const p=pending.get(m.id);if(p){pending.delete(m.id);clearTimeout(p.timer);m.error?p.reject(Error('CDP '+p.method)):p.resolve(m.result)}}else if(m.method==='Runtime.exceptionThrown')errors.push('uncaught browser exception')};
 const send=(method,params={})=>new Promise((resolve,reject)=>{const id=++seq,timer=setTimeout(()=>{pending.delete(id);reject(Error('CDP timeout '+method))},10000);pending.set(id,{resolve,reject,timer,method});socket.send(JSON.stringify({id,method,params}))});
 const evaluate=async expression=>{const r=await send('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});if(r.exceptionDetails)throw Error('evaluation failed');return r.result.value};
 const wait=async(expression,label)=>{for(let i=0;i<150;i++){if(await evaluate(expression))return;await sleep(40)}throw Error('condition timeout '+label)};
 const click=async text=>{const selector=`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()===${JSON.stringify(text)})`;await wait(`!!(${selector})&&!(${selector}).disabled`,text);await evaluate(`(${selector}).click()`)};
 const login=async()=>{await wait("!!document.querySelector('input[autocomplete=username]')",'login');for(const [selector,value] of [['input[autocomplete=username]','synthetic-user'],['input[autocomplete=current-password]','SYNTHETIC-password'],['input[autocomplete=one-time-code]','123456']])await evaluate(`(()=>{const x=document.querySelector(${JSON.stringify(selector)});Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(x,${JSON.stringify(value)});x.dispatchEvent(new Event('input',{bubbles:true}));})()`);await click('Войти');await wait(`document.querySelector('.identity')?.textContent.includes('Synthetic ${sameLogin?'A':'B'}')`,'new identity')};
 await send('Page.enable');await send('Page.addScriptToEvaluateOnNewDocument',{source:`try{localStorage.setItem('gm-vector.locale','ru')}catch{}`});await send('Runtime.enable');
 // Ignore transport abort to exercise generation checks even if response arrives.
 await send('Page.addScriptToEvaluateOnNewDocument',{source:`{const real=window.fetch.bind(window);window.fetch=(url,options={})=>{const {signal,...rest}=options;return real(url,rest)};window.downloads=[];HTMLAnchorElement.prototype.click=function(){window.downloads.push(this.download)};window.printWrites=[];window.printClosed=false;window.open=()=>({closed:false,opener:null,document:{open(){},write(v){window.printWrites.push(v)},close(){}},focus(){},print(){},close(){this.closed=true;window.printClosed=true}});}`});
 for(const test of ['project','list','error','save','export','print','same','file','modal']){
  scenario=test;identity='A';sameLogin=test==='same';armed=test==='list';held=[];requests.length=0;
  await send('Page.navigate',{url:origin});await wait("document.querySelector('.identity')?.textContent.includes('Synthetic A')",'identity A');
  if(test!=='list')await wait("document.querySelector('.project-header h1')?.textContent==='Current synthetic project'",'project A');
  if(['project','error','same'].includes(test)){armed=true;await evaluate("document.querySelector('nav button').click()")}
  if(test==='save'){
   await click('Настройки проекта');await click('Предпросмотр изменений');await wait("document.querySelector('dialog h2')?.textContent==='Проверка изменений'",'preview');armed=true;await click('Применить изменения');
   await evaluate("document.querySelector('dialog .icon').click()");
  }
  if(['export','print'].includes(test)){armed=true;await evaluate(`document.querySelector('[data-export-format="${test==='print'?'pdf':'json'}"]').click()`)}
  if(test==='modal')await click('Настройки проекта');
  if(test==='file'){
   await click('＋ Новый проект');
   await evaluate(`(()=>{const input=document.querySelector('.new-project-import input[type=file]');const file=new File(['{}'],'synthetic.json',{type:'application/json'});file.text=()=>new Promise(resolve=>window.releaseImportFile=()=>resolve(JSON.stringify({title:'${marker}'})));const transfer=new DataTransfer();transfer.items.add(file);input.files=transfer.files;input.dispatchEvent(new Event('change',{bubbles:true}));})()`);
   await wait("!!window.releaseImportFile",'file read delayed');await evaluate("document.querySelector('dialog .icon').click()");
  }else if(test!=='modal'){for(let i=0;i<150&&!held.length;i++)await sleep(20);assert(held.length,'request held for '+test)}
  await click('Выйти');await login();
  if(test==='file')await evaluate('window.releaseImportFile()');else held.splice(0).forEach(release=>release());
  await sleep(250);
  assert(!await evaluate(`document.body.textContent.includes(${JSON.stringify(marker)})`),'old data hidden: '+test);
  assert.equal(await evaluate('window.downloads.length'),0,'no old download: '+test);assert.equal(await evaluate('window.printWrites.length'),0,'no old print: '+test);
  if(test==='print')assert(await evaluate('window.printClosed'),'old print window closed');
  assert(!await evaluate("!!document.querySelector('dialog[open]')"),'old modal reset: '+test);
  if(test==='file')assert(!requests.some(r=>r.route==='/import'),'old file continuation never sends import');
  if(test!=='same')assert(!requests.some(r=>r.identity==='B'&&r.route.includes('synthetic-a')),'no stale project request using B cookie: '+test);
  console.log('SEC-D01 browser PASS: '+test);
 }
 assert.deepEqual(errors,[],'no uncaught stale operation');
 console.log('SEC-D01 browser PASS: project/list/error/save/export/print/same-user/file-read/modal isolation in built React; fixture API only.');
}
const timer=setTimeout(()=>{console.error('browser timeout');chrome?.kill('SIGKILL');process.exit(1)},90000);
async function stopChrome(){
 if(!chrome?.pid||chrome.exitCode!==null||chrome.signalCode!==null)return;
 await new Promise((resolve,reject)=>{
  const force=setTimeout(()=>chrome.kill('SIGKILL'),4000);
  const deadline=setTimeout(()=>{chrome.removeListener('close',closed);clearTimeout(force);reject(Error('Chrome did not exit during cleanup'))},8000);
  function closed(){clearTimeout(force);clearTimeout(deadline);resolve()}
  chrome.once('close',closed);chrome.kill('SIGTERM');
 });
}
main().catch(e=>{console.error(e.message);process.exitCode=1}).finally(async()=>{
 clearTimeout(timer);for(const p of pending.values())clearTimeout(p.timer);socket?.close();server?.closeAllConnections();server?.close();
 // Chrome must stop writing the fresh profile before removal. Retry only transient
 // filesystem races (Node's bounded rm retries); genuine cleanup errors still fail CI.
 await stopChrome();if(profile)await fs.rm(profile,{recursive:true,force:true,maxRetries:8,retryDelay:150});
}).catch(e=>{console.error('Browser cleanup failed:',e.code||e.message);process.exitCode=1});
