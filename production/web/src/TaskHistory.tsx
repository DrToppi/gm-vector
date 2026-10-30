import {useEffect,useRef,useState} from 'react';
import {ApiError} from './api';
import {useSessionScope} from './useSessionScope';
import {tr} from './i18n';
import {hours,type Employee,type TimeEntry,type User} from './types';
import {historyBase,historyFilters,historyQuery,canDeleteHistory,type HistoryPage,type HistoryFilters} from './history';
import {HistoryExportActions,historyError} from './HistoryExportActions';
import {firstHistoryPosition,nextHistoryPosition,previousHistoryPosition,type HistoryPosition} from './historyNavigation';
import './history.css';

export function TaskHistory({projectID,taskID,revision,employees,user,canWrite,canManage,reload,onDelete}:{projectID:string;taskID:string;revision:number;employees:Employee[];user:User;canWrite:boolean;canManage:boolean;reload:number;onDelete:(entry:TimeEntry)=>Promise<void>}){
 const scope=useSessionScope();
 const[filters,setFilters]=useState<HistoryFilters>(()=>historyFilters(taskID)),[draft,setDraft]=useState<HistoryFilters>(()=>historyFilters(taskID));
 const[pager,setPager]=useState<HistoryPosition>(()=>firstHistoryPosition(revision));
 const[data,setData]=useState<HistoryPage|null>(null),[busy,setBusy]=useState(false),[deleting,setDeleting]=useState(false),[error,setError]=useState(''),[changed,setChanged]=useState(false);
 const sequence=useRef(0),generation=useRef(0),mounted=useRef(false),cursor=pager.revision===revision?pager.cursor:null,signature=projectID+'/'+taskID+'/'+revision+'/'+reload+'/'+JSON.stringify(filters)+'/'+cursor+'/'+pager.reload,current=useRef(signature);current.current=signature;
 const traversalKey=projectID+'/'+taskID+'/'+revision+'/'+reload+'/'+JSON.stringify(filters)+'/'+pager.reload;
 const traversal=useRef<{key:string;revision:number|null;firstNext:string|null}>({key:traversalKey,revision:null,firstNext:null});
 if(traversal.current.key!==traversalKey)traversal.current={key:traversalKey,revision:null,firstNext:null};
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false;generation.current++;sequence.current++}},[]);
 useEffect(()=>{
  const version=++sequence.current,key=signature,controller=new AbortController();let active=true;
  setBusy(true);setError('');setData(null);setChanged(false);
  scope.api<HistoryPage>(historyBase(projectID)+'?'+historyQuery(filters,cursor),'GET',undefined,controller.signal).then(page=>{
   if(active&&mounted.current&&scope.current()&&version===sequence.current&&current.current===key){
    if(page.project_id!==projectID||page.revision<revision)return;
    const fence=traversal.current;
    if(fence.revision!==null&&page.revision!==fence.revision){setError(tr('history.changed'));setChanged(true);return}
    if(fence.revision===null){fence.revision=page.revision;fence.firstNext=page.next_cursor}
    // Re-reading page1 authenticates again but must not renew the original
    // continuation chain or silently accept a changed revision.
    setData(cursor===null?{...page,next_cursor:fence.firstNext}:page);
    if(pager.revision!==revision)setPager(value=>firstHistoryPosition(revision,value.reload));
   }
  }).catch(cause=>{
   if(active&&mounted.current&&scope.current()&&version===sequence.current&&current.current===key){setError(historyError(cause));setChanged(cause instanceof ApiError&&['history_changed','invalid_cursor','cursor_expired'].includes(cause.code))}
  }).finally(()=>{if(active&&mounted.current&&scope.current()&&version===sequence.current&&current.current===key)setBusy(false)});
  return()=>{active=false;controller.abort()};
 },[signature]);
 function reset(next=filters){setFilters(next);setPager(firstHistoryPosition(revision,pager.reload+1));setChanged(false)}
 async function remove(entry:TimeEntry){
  if(deleting)return;const key=signature,version=generation.current;setDeleting(true);
  try{await onDelete(entry);if(mounted.current&&scope.current()&&version===generation.current&&current.current===key)reset()}
  catch(cause){if(mounted.current&&scope.current()&&version===generation.current&&current.current===key)setError(historyError(cause))}
  finally{if(mounted.current&&scope.current()&&version===generation.current)setDeleting(false)}
 }
 return <section className="task-history" data-history-project={projectID}>
  <h4>{tr('mc04df59e2d')}</h4>
  <form className="history-filters" onSubmit={event=>{event.preventDefault();reset(draft)}}>
   <label>{tr('history.employeeFilter')}<select data-history-filter="employee_id" value={draft.employee_id} onChange={e=>setDraft({...draft,employee_id:e.target.value})}><option value="">{tr('history.allEmployees')}</option>{employees.map(employee=><option value={employee.id} key={employee.id}>{employee.name}</option>)}</select></label>
   <label>{tr('history.dateFrom')}<input data-history-filter="date_from" type="date" value={draft.date_from} onChange={e=>setDraft({...draft,date_from:e.target.value})}/></label>
   <label>{tr('history.dateTo')}<input data-history-filter="date_to" type="date" value={draft.date_to} onChange={e=>setDraft({...draft,date_to:e.target.value})}/></label>
   <button type="submit" className="secondary">{tr('history.apply')}</button><button type="button" className="secondary" onClick={()=>{const next=historyFilters(taskID);setDraft(next);reset(next)}}>{tr('history.reset')}</button>
  </form>
  {busy&&<p role="status">{tr('history.loading')}</p>}
  {error&&<p className="error" role="alert">{error}</p>}
  {changed&&<button type="button" className="secondary" onClick={()=>reset()}>{tr('history.restart')}</button>}
  {data&&<>
   <p data-history-totals>{tr('history.records')}: {data.total_count} · {tr('history.recorded')}: {hours(data.recorded_minutes)} {tr('m40831c5d98')}</p>
   <p data-history-legacy>{tr('history.legacy')}: {hours(data.legacy_unattributed.minutes)} {tr('m40831c5d98')}. {tr('history.legacyNote')}</p>
   <p className="muted">{tr('clock.zone')}: {data.planning_timezone} · {tr('history.currentLabels')}</p>
   <ul className="task-time-entries">{data.items.map(entry=><li key={entry.id} data-time-entry={entry.id}><span><b>{entry.task_title}</b> · {entry.date} · {entry.employee_name||tr('history.unknownEmployee')} · {hours(entry.minutes)} {tr('m40831c5d98')} · {entry.comment}</span>{canDeleteHistory(canWrite,canManage,user.employee_id||'',entry.employee_id)&&<button type="button" className="text-button danger" disabled={deleting} aria-label={tr('m3438c516ab')+entry.date} onClick={()=>void remove(entry)}>{tr('mbe99b13612')}</button>}</li>)}</ul>
   {!data.items.length&&<p>{tr('m5e1e0ac2d3')}</p>}
   <div className="toolbar history-pagination">
    <button type="button" className="secondary" data-history-previous disabled={busy||!pager.trail.length||pager.revision!==revision} onClick={()=>setPager(previousHistoryPosition(pager))}>{tr('history.previous')}</button>
    <button type="button" className="secondary" disabled={busy||!cursor} onClick={()=>reset()}>{tr('history.first')}</button>
    <button type="button" className="secondary" data-history-next disabled={busy||!data.next_cursor} onClick={()=>{if(data.next_cursor)setPager(nextHistoryPosition(pager,data.next_cursor,revision))}}>{tr('history.next')}</button>
    <span>{tr('history.pageCount',{count:data.items.length,total:data.total_count})}</span>
   </div>
   {pager.discarded&&<p className="notice amber" data-history-back-discarded>{tr('history.backDiscarded')}</p>}
  </>}
  {canManage&&<HistoryExportActions projectID={projectID} filters={filters} revision={revision}/>}
 </section>;
}
