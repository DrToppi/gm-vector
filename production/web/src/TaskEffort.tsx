import {usePlanningClock} from './PlanningClock';
import {tr} from './i18n';
import {useSessionScope} from './useSessionScope';
import {useEffect,useRef,useState} from 'react';
import {TaskHistory} from './TaskHistory';
import {hours,type Task,type Project,type Employee,type User} from './types';

export function TaskEffort({task,project,employees,user,canWrite,canManage,onRefresh,onBusy}:{task:Task;project:Project;employees:Employee[];user:User;canWrite:boolean;canManage:boolean;onRefresh:()=>Promise<void>;onBusy:(busy:boolean)=>void}){
 const clock=usePlanningClock(),scope=useSessionScope(),api=scope.api;
 const[error,setError]=useState(''),[busy,setBusy]=useState(false),[employee,setEmployee]=useState(''),[date,setDate]=useState(()=>clock.today()),[minutes,setMinutes]=useState(60),[comment,setComment]=useState(''),[reload,setReload]=useState(0);
 const saving=useRef(false),mounted=useRef(false),generation=useRef(0),key=project.id+'/'+task.id,currentKey=useRef(key);currentKey.current=key;
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false;generation.current++}},[key]);
 const assigned=employees.filter(e=>task.assignments.some(a=>a.employee_id===e.id)&&(canManage||e.id===user.employee_id));
 const chosen=assigned.some(e=>e.id===employee)?employee:assigned[0]?.id||'';
 async function mutate(action:()=>Promise<unknown>,clear=false):Promise<boolean>{
  if(saving.current)return false;const version=generation.current,context=key;
  const current=()=>mounted.current&&scope.current()&&generation.current===version&&currentKey.current===context;
  saving.current=true;setBusy(true);onBusy(true);setError('');
  try{
   await action();if(!current())return false;if(clear)setComment('');
   try{await onRefresh();if(current())setReload(value=>value+1)}
   catch{if(current())setError(tr('mb232b36b7c'))}
   return true;
  }catch(cause){if(current())setError((cause as Error).message+tr('m767fb55e5f'));return false}
  finally{if(current()){saving.current=false;setBusy(false);onBusy(false)}}
 }
 const actual=project.tasks.find(t=>t.id===task.id)?.actual_minutes||0;
 return <section className="task-effort" data-task-effort={task.id} aria-label={tr('mb420b0b6d4')}>
  <h3>{tr('m49b39a3346')}</h3>
  <p className={actual>task.planned_minutes?'effort-overrun':''} data-task-fact>{tr('m20b9cdc34d')} {hours(task.planned_minutes)} {tr('mc9a521ce2e')} {hours(actual)} {tr('m40831c5d98')}</p>
  {error&&<p className="error" role="alert">{error}</p>}
  {task.summary?<p>{tr('m31ee0f8e95')}</p>:canWrite?<form onSubmit={e=>{e.preventDefault();if(chosen)void mutate(()=>api('/projects/'+project.id+'/time-entries','POST',{task_id:task.id,employee_id:chosen,date,minutes,comment}),true)}}>
   <fieldset disabled={busy||!chosen||!clock.ready}><legend>{tr('m8d6ba6bac1')}</legend>
    <label>{tr('mc0b744cf1c')}<select aria-label={tr('m0c9786c9ac')} value={chosen} onChange={e=>setEmployee(e.target.value)}>{assigned.map(e=><option key={e.id} value={e.id}>{e.name}</option>)}</select></label>
    <div className="form-grid"><label>{tr('m232a0ead18')}<input aria-label={tr('m4253f95514')} type="date" required value={date} onChange={e=>setDate(e.target.value)}/></label><label>{tr('m7e6da98020')}<input aria-label={tr('mcd1575da89')} type="number" min={0.25} max={24} step={0.25} required value={hours(minutes)} onChange={e=>setMinutes(Math.round(Number(e.target.value)*60))}/></label></div>
    <label>{tr('m80492ccc34')}<textarea aria-label={tr('m80492ccc34')} maxLength={4000} value={comment} onChange={e=>setComment(e.target.value)}/></label>
    <button type="submit">{busy?tr('m9c27c381dd'):tr('m8d6ba6bac1')}</button>
   </fieldset>{!chosen&&<p className="notice amber">{tr('ma3f88d6150')}{!canManage?tr('m5e22813e75'):''}{tr('m721a69f395')}</p>}
  </form>:<p>{tr('me86e7423f9')}</p>}
  <TaskHistory key={key} projectID={project.id} taskID={task.id} revision={project.revision} employees={employees} user={user} canWrite={canWrite} canManage={canManage} reload={reload} onDelete={async entry=>{if(!await mutate(()=>api('/projects/'+project.id+'/time-entries/'+encodeURIComponent(entry.id),'DELETE')))throw Error(tr('history.changeFailed'))}}/>
 </section>;
}
