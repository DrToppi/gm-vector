// Pure layout checks: no browser, network, credentials or application data.
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const ts=require('../web/node_modules/typescript');
const source=fs.readFileSync(require.resolve('../web/src/mapLayout.ts'),'utf8');
const compiled=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText;
const sandbox={exports:{},require:name=>{assert.equal(name,'./i18n');return require('./test-i18n.cjs')}};vm.runInNewContext(compiled,sandbox);const {mapLayout:layout,PROJECT_ROOT:root}=sandbox.exports;
const tasks=[{id:'a',parent:''},{id:'b',parent:''},{id:'a1',parent:'a'},{id:'a2',parent:'a'},{id:'b1',parent:'b'}];
const p={tasks,links:[{from:'a1',to:'b1'}]},ids=tasks.map(t=>t.id);
const boxes=layout(p,p.links,ids,{});
assert(boxes.get('a').x>boxes.get(root).x);assert(boxes.get('b').x<boxes.get(root).x);
assert(boxes.get('a1').x>boxes.get('a').x);assert(boxes.get('b1').x<boxes.get('b').x);
assert.equal(boxes.get('a').y,(boxes.get('a1').y+boxes.get('a2').y)/2);
for(const [id,a] of boxes)for(const [other,b] of boxes)if(id!==other)assert(a.x+a.w<=b.x||b.x+b.w<=a.x||a.y+a.h<=b.y||b.y+b.h<=a.y,'no overlap');
const folded=layout(p,p.links,['a','b'],{});assert.equal(folded.size,3);assert.equal(folded.get('a').y,folded.get('b').y);
const empty=layout({tasks:[],links:[]},[],[],{});assert.equal(empty.size,1);
const moved=layout(p,p.links,ids,{a:{x:1200,y:600}});assert.equal(moved.get('a').x,1200);assert.equal(moved.get('a').y,600);
assert.deepEqual(layout({...p,links:[]},[],ids,{}),boxes,'links do not change tree layout');
console.log('Map layout: bilateral branches, subtree centering, no overlap, folding, empty project, manual positions passed.');

const {flowLayout,stageLayout}=sandbox.exports;
function noOverlap(result,label){for(const[id,a]of result)for(const[other,b]of result)if(id!==other){assert(Number.isFinite(a.x)&&Number.isFinite(a.y),label+' finite coordinates');assert(a.x+a.w<=b.x||b.x+b.w<=a.x||a.y+a.h<=b.y||b.y+b.h<=a.y,label+' no overlap: '+id+'/'+other)}}
const flowProject={tasks:[{id:'first',parent:''},{id:'left',parent:''},{id:'right',parent:''},{id:'last',parent:''},{id:'unconnected',parent:''}],links:[]};
const flowEdges=[{from:'first',to:'left'},{from:'first',to:'right'},{from:'left',to:'last'},{from:'right',to:'last'}];
const flowIDs=flowProject.tasks.map(t=>t.id),flow=flowLayout(flowProject,flowEdges,flowIDs,{});
for(const edge of flowEdges)assert(flow.get(edge.from).x+flow.get(edge.from).w<flow.get(edge.to).x,'dependency advances to a later column');
assert.equal(flow.get('left').x,flow.get('right').x,'parallel branches share a column');
assert(flow.has('unconnected'),'disconnected task retained');noOverlap(flow,'flow');
assert.deepEqual(flow,flowLayout(flowProject,[...flowEdges].reverse(),flowIDs,{}),'edge order does not alter geometry');
assert.equal(flowLayout({tasks:[],links:[]},[],[],{}).size,1,'empty flow keeps project card');
// A valid leaf DAG can become cyclic after projection onto collapsed stages:
// A1 -> B1 and B2 -> A2. Such display groups must not hang or lose tasks.
const foldedProject={tasks:[{id:'A',parent:''},{id:'A1',parent:'A'},{id:'A2',parent:'A'},{id:'B',parent:''},{id:'B1',parent:'B'},{id:'B2',parent:'B'},{id:'C',parent:''}],links:[]};
const foldedFlow=flowLayout(foldedProject,[{from:'A',to:'B'},{from:'B',to:'A'},{from:'B',to:'C'}],['A','B','C'],{});
assert.equal(foldedFlow.size,4);assert.equal(foldedFlow.get('A').x,foldedFlow.get('B').x,'folded cycle shares a column');assert(foldedFlow.get('C').x>foldedFlow.get('B').x,'external successor remains downstream');noOverlap(foldedFlow,'folded flow');
const stageProject={tasks:[...tasks,{id:'nested',parent:'a1'},{id:'loose',parent:''}],links:p.links};
const stageIDs=stageProject.tasks.map(t=>t.id),stages=stageLayout(stageProject,stageIDs,{});
assert.equal(stages.bands.length,3);assert(stages.bands.some(b=>b.id==='__stage_ungrouped__'&&b.title==='Без этапа'));
assert(stages.boxes.get('a').x<stages.boxes.get('b').x,'top-level stages form columns');
assert(stages.boxes.get('nested').y>stages.boxes.get('a1').y,'nested child follows parent');
for(const[id,stage]of [['a','a'],['a1','a'],['a2','a'],['nested','a'],['b','b'],['b1','b'],['loose','__stage_ungrouped__']]){const box=stages.boxes.get(id),band=stages.bands.find(b=>b.id===stage);assert(box.x>=band.x&&box.y>=band.y&&box.x+box.w<=band.x+band.w&&box.y+box.h<=band.y+band.h,'card stays in its stage');}
noOverlap(stages.boxes,'stages');assert.deepEqual(stages,stageLayout(stageProject,stageIDs,{}),'stages deterministic');
const stageFolded=stageLayout(stageProject,['a','b','loose'],{});assert.equal(stageFolded.boxes.size,4);noOverlap(stageFolded.boxes,'folded stages');
const noStages=stageLayout(flowProject,flowIDs,{});assert.equal(noStages.bands.length,1);assert.equal(noStages.bands[0].id,'__stage_ungrouped__');noOverlap(noStages.boxes,'no stages');
const noTasks=stageLayout({tasks:[],links:[]},[],{});assert.equal(noTasks.boxes.size,1);assert.equal(noTasks.bands.length,0);
const before=JSON.stringify(stageProject),manualFlow=flowLayout(flowProject,flowEdges,flowIDs,{first:{x:1500,y:800}}),manualStage=stageLayout(stageProject,stageIDs,{a:{x:1600,y:900}});
assert.equal(manualFlow.get('first').x,1500);assert.equal(manualStage.boxes.get('a').x,1600);assert.deepEqual(flow,flowLayout(flowProject,flowEdges,flowIDs,{}),'manual flow has no global state');assert.deepEqual(stages,stageLayout(stageProject,stageIDs,{}),'manual stages have no global state');assert.equal(JSON.stringify(stageProject),before,'layout does not mutate project');
console.log('Map modes: dependency ranking, folded quotient cycles, stages, disconnected/empty projects, deterministic non-overlap and isolated manual positions passed.');
