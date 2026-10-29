// SEC-D01: transport ignores AbortSignal intentionally; generation must still win.
const fs=require('node:fs'),vm=require('node:vm'),path=require('node:path'),assert=require('node:assert/strict');
const ts=require('../web/node_modules/typescript');
const compile=(name,scope)=>{vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(__dirname,'../web/src',name),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,scope);return scope.exports};
const diagnostic=compile('diagnostics.ts',{exports:{},URL,Date,Intl});
let resolveFetch,calls=0,signal,lastHeaders;
const a=compile('api.ts',{exports:{},require:name=>name==='./i18n'?require('./test-i18n.cjs'):diagnostic,performance,AbortController,DOMException,fetch:(_url,opts)=>{calls++;signal=opts.signal;lastHeaders=opts.headers;return new Promise(resolve=>resolveFetch=resolve)}});
const response=(payload,status=200)=>({status,ok:status===200,headers:new Headers(),json:async()=>payload});
(async()=>{
 a.advanceSession();const old=a.sessionScope();a.setCSRF('synthetic-A-csrf');
 const p=old.api('/projects/A');const rejected=assert.rejects(p,e=>e.name==='AbortError'&&!e.message.includes('PRIVATE'));
 assert.equal(lastHeaders['X-CSRF-Token'],'synthetic-A-csrf');a.advanceSession();assert(signal.aborted);resolveFetch(response({title:'PRIVATE-A'}));await rejected;
 const before=calls;await assert.rejects(old.api('/import','POST',{title:'PRIVATE-A'}),{name:'AbortError'});assert.equal(calls,before,'old continuation cannot issue a request');
 let resolveJSON;const body=a.api('/projects/A');const bodyRejected=assert.rejects(body,{name:'AbortError'});resolveFetch({status:200,ok:true,headers:new Headers(),json:()=>new Promise(r=>resolveJSON=r)});await new Promise(setImmediate);assert(resolveJSON);a.advanceSession();resolveJSON({title:'PRIVATE-A'});await bodyRejected;
 const error=a.api('/projects/A');const errorRejected=assert.rejects(error,e=>e.name==='AbortError'&&!e.message.includes('PRIVATE'));a.advanceSession();resolveFetch(response({error:{message:'PRIVATE-A-ERROR'}},403));await errorRejected;
 const fresh=a.api('/projects');assert(!lastHeaders['X-CSRF-Token']);resolveFetch(response([]));assert.deepEqual(await fresh,[]);
 const epoch=a.sessionEpoch();a.advanceSession();assert(a.sessionEpoch()>epoch,'same account boundary is always new generation');
 console.log('SEC-D01 unit PASS: stale headers/body/errors rejected; old scope cannot fetch; abort, CSRF reset and repeated identity generation verified.');
})().catch(e=>{console.error(e);process.exitCode=1});
