// Only allowlisted metadata enters this in-memory journal. Never accept messages,
// request/response bodies, arbitrary URLs, headers, DOM or storage snapshots.
export type RequestEvent={kind:'request';time:string;method:string;route:string;status:number;duration_ms:number;request_id:string};
export type ScriptEvent={kind:'script';time:string;type:'error'|'unhandledrejection';file:string;line:number;column:number};
export type DiagnosticEvent=RequestEvent|ScriptEvent;
const MAX=100,TTL=15*60*1000;
let events:DiagnosticEvent[]=[];let owner='';let epoch=0;
const templates=[
 '/auth/me','/auth/status','/auth/login','/auth/logout','/auth/password','/auth/activation','/auth/bootstrap','/auth/bootstrap/resume','/auth/bootstrap/confirm',
 '/projects','/projects/{id}','/projects/{id}/preview','/projects/{id}/undo','/projects/{id}/export','/projects/{id}/members','/projects/{id}/member-candidates',
 '/projects/{id}/time-entries','/projects/{id}/time-entries/{entry}','/projects/{id}/time-history','/projects/{id}/time-history/export','/projects/{id}/portable/export','/import/portable','/projects/{id}/tasks/{task}/comments',
 '/employees','/employees/{id}/invite','/calendars','/integrations','/diagnostics','/diagnostics/events','/diagnostics/key','/audit','/trash','/trash/{id}/restore','/users','/users/{id}','/import','/tokens','/tokens/{id}','/directory/people','/directory/status','/directory/sync','/directory/ldap/sync','/openapi.json'
];
export function safeRoute(path:unknown):string{
 if(typeof path!=='string'||!path.startsWith('/')||path.startsWith('//'))return 'unknown';
 const clean=path.split(/[?#]/,1)[0].replace(/^\/api\/v1(?=\/)/,'');
 const parts=clean.split('/');
 const found=templates.find(t=>{const shape=t.split('/');return shape.length===parts.length&&shape.every((p,i)=>p.startsWith('{')?!!parts[i]&&parts[i].length<=200:p===parts[i])});
 return found?'/api/v1'+found:'unknown';
}
const integer=(n:unknown,max:number)=>typeof n==='number'&&Number.isFinite(n)?Math.max(0,Math.min(max,Math.round(n))):0;
const safeMethod=(m:unknown)=>typeof m==='string'&&['GET','HEAD','POST','PUT','PATCH','DELETE','OPTIONS'].includes(m)?m:'unknown';
const requestID=(s:unknown)=>typeof s==='string'&&/^[A-Za-z0-9_-]{16,64}$/.test(s)?s:'';
function prune(now:number){events=events.filter(e=>Date.parse(e.time)>now-TTL).slice(-MAX)}
function append(e:DiagnosticEvent,now:number){prune(now);events.push(e);events=events.slice(-MAX)}
export function diagnosticEpoch(){return epoch}
export function clearDiagnostics(){events=[];epoch++}
export function setDiagnosticIdentity(id:string){if(id!==owner){owner=id;clearDiagnostics()}}
export function recordRequest(input:{path:unknown;method:unknown;status:unknown;duration_ms:unknown;request_id?:unknown},startedEpoch=epoch,now=Date.now()){
 if(startedEpoch!==epoch)return;
 append({kind:'request',time:new Date(now).toISOString(),method:safeMethod(input.method),route:safeRoute(input.path),status:integer(input.status,599),duration_ms:integer(input.duration_ms,3600000),request_id:requestID(input.request_id)},now);
}
export function ownAsset(filename:unknown,origin:string):string{
 if(typeof filename!=='string')return '';
 try{const u=new URL(filename,origin);return u.origin===origin&&/^\/assets\/index-[A-Za-z0-9_-]{6,80}\.(?:js|css)$/.test(u.pathname)?u.pathname.split('/').pop()!:''}catch{return ''}
}
export function recordScript(type:'error'|'unhandledrejection',filename:unknown,line:unknown,column:unknown,origin:string,now=Date.now()){
 const file=ownAsset(filename,origin);
 append({kind:'script',time:new Date(now).toISOString(),type,file,line:file?integer(line,1e7):0,column:file?integer(column,1e7):0},now);
}
export function diagnosticSnapshot(now=Date.now()):DiagnosticEvent[]{prune(now);return events.map(e=>({...e}))}
export function installDiagnostics(){
 const onError=(e:ErrorEvent)=>recordScript('error',e.filename,e.lineno,e.colno,window.location.origin);
 const onRejection=()=>recordScript('unhandledrejection','',0,0,window.location.origin);
 window.addEventListener('error',onError);window.addEventListener('unhandledrejection',onRejection);
 return()=>{window.removeEventListener('error',onError);window.removeEventListener('unhandledrejection',onRejection)};
}
export function safeServerEvents(value:unknown):RequestEvent[]{
 if(!value||typeof value!=='object'||!Array.isArray((value as any).events))return [];
 return (value as any).events.slice(-500).filter((e:any)=>e&&typeof e==='object'&&typeof e.time==='string'&&Number.isFinite(Date.parse(e.time))).map((e:any)=>({kind:'request',time:new Date(e.time).toISOString(),route:safeRoute(e.route),method:safeMethod(e.method),status:integer(e.status,599),duration_ms:integer(e.duration_ms,3600000),request_id:requestID(e.request_id)}));
}
export function safeServerState(value:unknown){
 const v=value&&typeof value==='object'?value as Record<string,unknown>:{};
 return {version:typeof v.version==='string'&&/^\d+\.\d+\.\d+(?:-[a-z0-9.-]+)?$/.test(v.version)?v.version:'unknown',database:v.database==='ready'?'ready':'unknown',teams_enabled:v.teams_enabled===true};
}
export function browserFamily(ua:string){return /Edg\//.test(ua)?'Edge':/Firefox\//.test(ua)?'Firefox':/Chrome\//.test(ua)?'Chrome':/Safari\//.test(ua)?'Safari':'other'}
export function safeView(view:string){return ['gantt','map','list','people','admin','login','loading'].includes(view)?view:'other'}
export function diagnosticEnvironment(){
 const language=/^[a-z]{2,3}(?:-[A-Za-z0-9]{2,8}){0,2}$/.test(navigator.language)?navigator.language:'unknown';
 const zone=Intl.DateTimeFormat().resolvedOptions().timeZone;
 return {browser:browserFamily(navigator.userAgent),language,timezone:/^[A-Za-z_+-]+(?:\/[A-Za-z_+-]+){0,2}$/.test(zone)?zone:'unknown',viewport:{width:integer(window.innerWidth,20000),height:integer(window.innerHeight,20000)},assets:Array.from(document.querySelectorAll('script[src]')).map(s=>ownAsset((s as HTMLScriptElement).src,window.location.origin)).filter(Boolean)};
}
