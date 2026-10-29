// Real updater component with synthetic loopback API. Never calls the host updater.
const assert=require('node:assert/strict'),fs=require('node:fs/promises'),path=require('node:path'),os=require('node:os'),http=require('node:http'),{spawn}=require('node:child_process'),vm=require('node:vm');
const ts=require('../web/node_modules/typescript'),{buildSync}=require('../web/node_modules/esbuild');
const root=path.resolve(__dirname,'../web'),sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
const commit='a'.repeat(40),previous='b'.repeat(40),operation='synthetic-operation';
let chrome,socket,server,profile,seq=0,held=[],mode='idle',loseApply=false,holdNext=false,apiCalls=[],uncaught=[];
const pending=new Map();
let state={enabled:true,state:'idle',phase:'',current_commit:previous,candidate_commit:'',operation_id:'',recovery_available:false,message:'SYNTHETIC_PRIVATE_SERVER_PATH'};
function unit(){
 const sandbox={exports:{},AbortController};
 vm.runInNewContext(ts.transpileModule(require('node:fs').readFileSync(path.join(root,'src/updaterState.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,sandbox);
 const a=sandbox.exports,ready=a.updateStatus({...state,state:'ready',candidate_commit:commit});
 assert(a.updateCanApply(ready,commit));
 for(const confirmation of ['',commit.slice(0,12),commit.toUpperCase(),' '+commit,commit+' '])assert(!a.updateCanApply(ready,confirmation),'exact full commit required');
 assert(!a.updateCanApply({...ready,state:'updating'},commit));
 const recovery=a.updateStatus({...state,state:'recovery_required',operation_id:operation,recovery_available:true});
 assert(a.updateCanRecover(recovery,operation));assert(!a.updateCanRecover({...recovery,recovery_available:false},operation));assert(!a.updateCanRecover({...recovery,state:'failed'},operation));
 assert.equal(a.updateStatus({...state,current_commit:'/PRIVATE',candidate_commit:'<script>',operation_id:'../../PRIVATE',phase:'/PRIVATE'}).operation_id,'');
 assert.equal(a.updateStatus({...state,message:'PRIVATE',token:'PRIVATE'}).message,undefined);
 assert.equal(a.updateStatus({enabled:false,state:'bogus',message:'PRIVATE'}).state,'disabled');assert.throws(()=>a.updateStatus({...state,state:'bogus'}));
 const gate=new a.UpdateRequests(),old=gate.begin(),fresh=gate.begin();assert(old.signal.aborted);assert(!old.current());assert(fresh.current());gate.close();assert(!fresh.current());gate.open();const next=gate.begin();assert(next.current());assert(!old.current());
 console.log('Updater unit PASS: exact confirmations, safe status projection, invalid states, stale/unmounted generation rejection.');
}
async function main(){
 unit();
 const entry=`import React from 'react';import {createRoot} from 'react-dom/client';import {UpdatesPanel} from './src/UpdatesPanel';import {setCSRF,advanceSession} from './src/api';import {changeLocale} from './src/i18n';setCSRF('synthetic-csrf');void changeLocale('en');let root;window.mountUpdater=()=>{root=createRoot(document.getElementById('root'));root.render(React.createElement(UpdatesPanel))};window.unmountUpdater=()=>root.unmount();window.flipSession=()=>advanceSession();window.mountUpdater();`;
 const bundled=buildSync({stdin:{contents:entry,resolveDir:root,loader:'tsx'},bundle:true,platform:'browser',format:'iife',write:false,loader:{'.css':'empty'}}).outputFiles[0].text;
 server=http.createServer(async(req,res)=>{
  const pathname=new URL(req.url,'http://localhost').pathname;res.setHeader('Cache-Control','no-store');
  const json=(body,status=200)=>{res.statusCode=status;res.setHeader('Content-Type','application/json');res.end(JSON.stringify(body))};
  if(pathname==='/test.js'){res.setHeader('Content-Type','text/javascript');return res.end(bundled)}
  if(pathname==='/'){res.setHeader('Content-Type','text/html');return res.end('<div id="root"></div><script src="/test.js"></script>')}
  if(pathname.startsWith('/api/v1/admin/updates')){
   apiCalls.push({method:req.method,path:pathname});
   if(req.method==='GET'){
    if(holdNext){holdNext=false;const old={...state};held.push(()=>json(old));return}
    if(mode==='unavailable')return json({error:{message:'SYNTHETIC_PRIVATE_SERVER_PATH'}},503);
    return json(mode==='disabled'?{enabled:false,state:'disabled'}:state);
   }
   assert.equal(req.headers['x-csrf-token'],'synthetic-csrf','mutations preserve CSRF');
   let text='';for await(const chunk of req)text+=chunk;const body=JSON.parse(text);
   if(pathname.endsWith('/check')){assert.equal(body.ref,'main');state={...state,state:'ready',phase:'complete',candidate_commit:commit};return json(state)}
   if(pathname.endsWith('/apply')){assert.deepEqual(body,{commit,confirmation:commit});state={...state,state:'updating',phase:'backup',operation_id:operation};if(loseApply){loseApply=false;req.socket.destroy();return}return json(state)}
   if(pathname.endsWith('/recover')){assert.deepEqual(body,{operation_id:operation,confirmation:operation});state={...state,state:'recovering',phase:'restore'};return json(state)}
  }
  res.statusCode=404;res.end();
 });
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const origin='http://127.0.0.1:'+server.address().port;
 profile=await fs.mkdtemp(path.join(os.tmpdir(),'gm-vector-updater-ui-'));
 chrome=spawn(process.env.GMA_CHROME_BIN||'google-chrome',['--headless=new','--no-first-run','--disable-background-networking','--disable-component-update','--disable-sync','--remote-debugging-port=0','--remote-debugging-address=127.0.0.1','--user-data-dir='+profile,'about:blank'],{stdio:'ignore'});
 let launchError=false;chrome.on('error',()=>launchError=true);let port;
 for(let i=0;i<150;i++){if(launchError||chrome.exitCode!==null)throw Error('Chrome unavailable');try{port=Number((await fs.readFile(path.join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]);if(port)break}catch{}await sleep(100)}assert(port,'fresh Chrome');
 const pages=await(await fetch('http://127.0.0.1:'+port+'/json/list')).json();socket=new WebSocket(pages.find(page=>page.type==='page').webSocketDebuggerUrl);await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject});
 socket.onmessage=event=>{const message=JSON.parse(event.data);if(message.id){const request=pending.get(message.id);if(request){pending.delete(message.id);clearTimeout(request.timer);message.error?request.reject(Error('CDP request failed')):request.resolve(message.result)}}else if(message.method==='Runtime.exceptionThrown')uncaught.push('browser exception')};
 const send=(method,params={})=>new Promise((resolve,reject)=>{const id=++seq,timer=setTimeout(()=>{pending.delete(id);reject(Error('CDP timeout '+method))},10000);pending.set(id,{resolve,reject,timer});socket.send(JSON.stringify({id,method,params}))});
 const evaluate=async expression=>{const result=await send('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});if(result.exceptionDetails)throw Error('browser evaluation failed');return result.result.value};
 const wait=async(expression,label)=>{for(let i=0;i<200;i++){if(await evaluate(expression))return;await sleep(40)}throw Error('condition timeout '+label)};
 const click=async label=>{const selector=`[...document.querySelectorAll('button')].find(button=>button.textContent.trim()===${JSON.stringify(label)})`;await wait(`!!(${selector})&&!(${selector}).disabled`,label);await evaluate(`(${selector}).click()`)};
 const fill=async(selector,value)=>{await wait(`!!document.querySelector(${JSON.stringify(selector)})`,'input');await evaluate(`(()=>{const element=document.querySelector(${JSON.stringify(selector)});Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(element,${JSON.stringify(value)});element.dispatchEvent(new Event('input',{bubbles:true}));})()`);await sleep(20)};
 await send('Page.enable');await send('Runtime.enable');await send('Page.addScriptToEvaluateOnNewDocument',{source:`{const real=window.fetch.bind(window);window.fetch=(url,options={})=>{const {signal,...rest}=options;return real(url,rest)};}`});
 mode='disabled';await send('Page.navigate',{url:origin});await wait("!!document.querySelector('[data-updates-disabled]')",'disabled service');
 assert.equal(apiCalls.filter(call=>call.method==='POST').length,0,'mount never checks/applies automatically');assert(!await evaluate("!!document.querySelector('.updates-check')"));
 mode='idle';await click('Refresh status');await wait("document.querySelector('[data-update-state]')?.dataset.updateState==='idle'",'idle');await click('Check revision');await wait("!!document.querySelector('[data-update-confirm]')",'candidate');
 assert.equal(await evaluate("[...document.querySelectorAll('button')].find(button=>button.textContent==='Install update').disabled"),true);
 await fill('[data-update-confirm] input',commit.slice(0,12));assert.equal(await evaluate("[...document.querySelectorAll('button')].find(button=>button.textContent==='Install update').disabled"),true);
 await fill('[data-update-confirm] input',commit);await wait("![...document.querySelectorAll('button')].find(button=>button.textContent==='Install update').disabled",'exact confirmation');
 holdNext=true;await click('Refresh status');await wait("true",'request');for(let i=0;i<30&&!held.length;i++)await sleep(20);assert.equal(held.length,1,'old status held');
 loseApply=true;await click('Install update');await wait("document.body.textContent.includes('command acknowledgement')",'lost acknowledgement');held.shift()();
 await wait("document.querySelector('[data-update-state]')?.dataset.updateState==='updating'",'status reconciliation');
 assert.equal(apiCalls.filter(call=>call.path.endsWith('/apply')).length,1,'lost acknowledgement never retries mutation');assert(!await evaluate("!!document.querySelector('[data-update-confirm]')"),'stale ready response cannot resurrect candidate');
 assert(!await evaluate("document.body.textContent.includes('SYNTHETIC_PRIVATE_SERVER_PATH')"),'raw service message hidden');
 state={...state,state:'recovery_required',phase:'migrate',recovery_available:true};await click('Refresh status');await wait("!!document.querySelector('[data-update-recovery] input')", 'recovery');
 await fill('[data-update-recovery] input','wrong');assert.equal(await evaluate("[...document.querySelectorAll('button')].find(button=>button.textContent==='Restore previous installation').disabled"),true);
 await fill('[data-update-recovery] input',operation);await click('Restore previous installation');await wait("document.querySelector('[data-update-state]')?.dataset.updateState==='recovering'",'recovering');
 assert.equal(apiCalls.filter(call=>call.path.endsWith('/recover')).length,1);
 await evaluate('window.unmountUpdater()');const afterUnmount=apiCalls.length;await sleep(2300);assert.equal(apiCalls.length,afterUnmount,'unmounted panel stops polling');
 mode='unavailable';await evaluate('window.mountUpdater()');await wait("!!document.querySelector('[role=alert]')",'service unavailable');assert(!await evaluate("document.body.textContent.includes('SYNTHETIC_PRIVATE_SERVER_PATH')"),'error response details hidden');
 assert.deepEqual(uncaught,[]);
 console.log('Updater browser PASS: disabled service, explicit check, exact apply/recovery confirmation, lost-ack reconciliation, no destructive retry, stale-response rejection, privacy and unmount polling cleanup.');
}
const timeout=setTimeout(()=>{console.error('Updater UI timeout');chrome?.kill('SIGKILL');process.exit(1)},70000);
async function stopChrome(){if(!chrome?.pid||chrome.exitCode!==null||chrome.signalCode!==null)return;await new Promise((resolve,reject)=>{const force=setTimeout(()=>chrome.kill('SIGKILL'),4000),deadline=setTimeout(()=>reject(Error('Chrome cleanup timeout')),8000);chrome.once('close',()=>{clearTimeout(force);clearTimeout(deadline);resolve()});chrome.kill('SIGTERM')})}
main().catch(error=>{console.error(error.message);process.exitCode=1}).finally(async()=>{clearTimeout(timeout);for(const request of pending.values())clearTimeout(request.timer);for(const release of held)release();socket?.close();server?.closeAllConnections();server?.close();await stopChrome();if(profile)await fs.rm(profile,{recursive:true,force:true,maxRetries:8,retryDelay:150})}).catch(error=>{console.error('Updater UI cleanup failed:',error.message);process.exitCode=1});
