import {tr} from './i18n';
import {today,type Task,type Project,type Employee} from './types';
export function validDate(value:string):boolean{return /^\d{4}-\d{2}-\d{2}$/.test(value)&&Number.isFinite(Date.parse(value+'T00:00:00Z'))&&new Date(value+'T00:00:00Z').toISOString().slice(0,10)===value}
export function scheduleAvailable(task:Task,project:Project):boolean{return !project.calculation_state||project.calculation_state==='ready'||task.status==='done'||(!task.summary&&task.responsibility==='customer')}
export function taskStatus(task:Task,project:Project,date=today()){
 if(task.status==='done')return {kind:'done',label:tr('mef05d57959')};
 if(scheduleAvailable(task,project)&&validDate(task.end)&&task.end<date)return {kind:'overdue',label:tr('mc2d42e7984')};
 if(task.blocked)return {kind:'blocked',label:tr('md600a7d525')};
 if(task.status==='doing')return {kind:'doing',label:tr('m59a28f1b11')};
 return {kind:'todo',label:tr('mc6cc849619')};
}
export function taskExecutors(task:Task,employees:Employee[]):string{return task.responsibility==='customer'?task.customer_owner||tr('ma517cfaefe'):task.assignments.map(a=>employees.find(e=>e.id===a.employee_id)?.name||tr('m0ca505f224')).join(', ')||tr('ma517cfaefe')}
