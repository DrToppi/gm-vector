// Pure WP-04 contracts, conversion bounds and planning-clock reference cases.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const {buildSync}=require('../web/node_modules/esbuild'),web=path.resolve(__dirname,'../web');
let now='2026-10-30T01:00:00Z';
const RealDate=Date,ClockDate=class extends RealDate{constructor(...args){super(...(args.length?args:[now]))}static now(){return RealDate.parse(now)}};
const code=buildSync({stdin:{contents:"export * from './src/planningTime';export * from './src/history';export * from './src/historyNavigation';export * from './src/historyReport';export * from './src/projectTransfer';export {changeLocale} from './src/i18n';",resolveDir:web,loader:'ts'},bundle:true,platform:'node',format:'cjs',write:false}).outputFiles[0].text;
const sandbox={module:{exports:{}},exports:{},require,Date:ClockDate,Intl,TextEncoder,URLSearchParams,setTimeout,clearTimeout,console};vm.runInNewContext(code,sandbox);const app=sandbox.module.exports;
const item={id:'entry',task_id:'source',task_title:'Source task',employee_id:'',employee_name:'',date:'2026-10-29',minutes:61,comment:'Short note'};
const document={format:'gm-vector-time-history',schema_version:1,generated_at:'2026-10-30T01:00:00Z',project_id:'p',project_title:'Synthetic project',revision:7,planning_timezone:'America/New_York',filters:{task_id:'root',employee_id:'',date_from:'',date_to:''},order:'work_date_desc_id_asc',total_count:1,recorded_minutes:61,legacy_unattributed:{minutes:37,scope:'task_subtree',employee_date_attribution:'unavailable'},items:[item]};
const limited=fn=>assert.throws(fn,error=>error.name==='HistoryConversionLimit');
async function main(){
 const cases=[
  ['2026-10-30T01:00:00Z',['2026-10-30','2026-10-30','2026-10-29','2026-10-30']],
  ['2026-10-29T18:30:00Z',['2026-10-29','2026-10-29','2026-10-29','2026-10-30']],
  ['2026-12-31T23:30:00Z',['2026-12-31','2027-01-01','2026-12-31','2027-01-01']],
  ['2028-02-29T23:30:00Z',['2028-02-29','2028-03-01','2028-02-29','2028-03-01']],
 ];
 const zones=['UTC','Europe/Moscow','America/New_York','Asia/Kathmandu'];
 const originalTZ=process.env.TZ;
 for(const host of ['UTC','Pacific/Honolulu']){
  process.env.TZ=host;
  for(const [instant,expected]of cases)for(let i=0;i<zones.length;i++)assert.equal(app.planningToday(zones[i],new RealDate(instant)),expected[i],host+'/'+instant+'/'+zones[i]);
 }
 if(originalTZ===undefined)delete process.env.TZ;else process.env.TZ=originalTZ;
 now='2026-10-30T03:59:00Z';assert.equal(app.planningToday('America/New_York'),'2026-10-29');now='2026-10-30T04:01:00Z';assert.equal(app.planningToday('America/New_York'),'2026-10-30','today is not cached across midnight');
 assert(!app.supportsPlanningZone('Invalid/Synthetic'));assert(!app.supportsPlanningZone('Local'));assert.throws(()=>app.planningToday('Invalid/Synthetic'),error=>error.name==='RangeError');
 const foldA=app.planningTimestamp('2026-11-01T05:30:00Z','America/New_York','en'),foldB=app.planningTimestamp('2026-11-01T06:30:00Z','America/New_York','en');assert.notEqual(foldA,foldB,'fold instants retain distinct offsets');assert(foldA.includes('GMT-4')&&foldB.includes('GMT-5'));
 assert(app.planningTimestamp('2026-03-08T06:30:00Z','America/New_York','en').includes('01:30'));assert(app.planningTimestamp('2026-03-08T07:30:00Z','America/New_York','en').includes('03:30'));
 let position=app.firstHistoryPosition(1);position=app.nextHistoryPosition(position,'page1',1);assert.equal(position.trail.length,1);position=app.previousHistoryPosition(position);assert.equal(position.cursor,null,'page2 Previous returns page1');
 for(let i=1;i<=100;i++)position=app.nextHistoryPosition(position,'page'+i,1);assert.equal(position.trail.length,100);assert.equal(position.discarded,false);position=app.nextHistoryPosition(position,'page101',1);assert.equal(position.trail.length,100);assert.equal(position.discarded,true);assert.equal(app.previousHistoryPosition(position).cursor,'page100');position=app.firstHistoryPosition(1,1);assert.equal(position.trail.length,0);assert.equal(position.discarded,false);
 const query=app.historyQuery(document.filters,'opaque cursor +/',500),parsed=new URLSearchParams(query);assert.equal(parsed.get('cursor'),'opaque cursor +/');assert.equal(parsed.get('limit'),'500');assert.equal(parsed.get('task_id'),'root');
 assert(!app.canDeleteHistory(true,false,'',''));assert(!app.canDeleteHistory(true,false,'e',''));assert(app.canDeleteHistory(true,false,'e','e'));assert(app.canDeleteHistory(true,true,'',''));assert(!app.canDeleteHistory(false,true,'e','e'));
 const legacy={id:'p',tasks:[],calendar_id:'c',external_reference:'kept-for-server-normalization'},portable={format:'gm-vector-project',schema_version:1,metadata:{planning_timezone:'UTC',generated_at:'2026-10-30T00:00:00Z'},project:legacy};
 const old=app.projectImportRequest(app.detectProjectDocument(legacy),true);assert.equal(old.path,'/import');assert.equal(old.body.project,legacy);
 const first=app.projectImportRequest(app.detectProjectDocument(portable),true);assert.equal(first.path,'/import/portable');assert(!('timezone_acknowledgement'in first.body));assert.equal(first.body.document,portable);
 const ack=app.projectImportRequest(app.detectProjectDocument(portable),false,{source:'UTC',target:'America/New_York'});assert.equal(ack.body.timezone_acknowledgement.source,'UTC');assert.equal(ack.body.timezone_acknowledgement.target,'America/New_York');
 for(const bad of [{...portable,schema_version:2},{format:'gm-vector-time-history',items:[]},null,[]])assert.throws(()=>app.detectProjectDocument(bad));
 const memoryBefore=process.memoryUsage().rss;
 for(const locale of ['en','ru','de','fr','es','pt-BR','zh-Hans']){
  await app.changeLocale(locale);
  const hostile={...document,project_title:'<script>hostile()</script>',items:[{...item,task_title:'<img src=x>',employee_name:'=formula',comment:'<script>secret()</script>'}]};
  const html=app.historyReport(hostile,'html');assert(html.includes('lang="'+locale+'"'));assert(!html.includes('<script>'));assert(html.includes('&lt;script&gt;'));assert(html.includes('America/New_York'));
  const csv=app.historyReport(hostile,'csv');assert(csv.startsWith('\ufeff'));assert(csv.includes('"\''+'=formula'+'"'));assert(csv.includes('61'));assert(csv.includes('37')||csv.includes('0.62'));
  assert.deepEqual(JSON.parse(app.historyReport(document,'json')),document,'history JSON is a separate complete schema');
 }
 const byteExact=new app.BoundedText(4);byteExact.add('😀');assert.equal(byteExact.finish(),'😀');limited(()=>byteExact.add('x'));
 const tenThousand={...document,total_count:10000,recorded_minutes:610000,items:Array.from({length:10000},(_,i)=>({...item,id:'e'+i}))};
 const pdf=app.historyReport(tenThousand,'pdf');assert(new TextEncoder().encode(pdf).length<=app.HISTORY_PDF_BYTES);assert.equal((pdf.match(/<tr>/g)||[]).length,10001,'full10k PDF rows plus header');limited(()=>app.historyReport({...tenThousand,items:[...tenThousand.items,item]},'pdf'));
 const expanded={...document,items:Array(4000).fill({...item,comment:'&'.repeat(4000)})};limited(()=>app.historyReport(expanded,'html'));limited(()=>app.historyReport({...expanded,items:expanded.items.slice(0,1000)},'pdf'));
 limited(()=>app.historyReport({...document,items:Array(10000).fill({...item,comment:'"'.repeat(4000)})},'csv'));
 limited(()=>app.historyReport({...document,items:Array(3000).fill({...item,comment:'\u0000'.repeat(4000)})},'json'));
 console.log('History/clock unit PASS: four planning zones and host independence, midnight/leap/year/DST, strict portable routes/ack, ownership, seven locale escaping/formula controls, complete10k PDF and incremental64MiB/8MiB expansion rejection. RSS delta diagnostic bytes: '+(process.memoryUsage().rss-memoryBefore));
}
main().catch(error=>{console.error(error.message);process.exitCode=1});
