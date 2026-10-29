import {tr,currentLocale} from './i18n';
import {useSessionScope} from './useSessionScope';
import {useEffect, useRef, useState} from 'react';

import {hours, type Employee} from './types';

type Invitation = {id:string; username:string; email:string; state:string; expires_at:string; delivery_state:string; delivery_error:string};
type Access = {invitation:Invitation|null; smtp_enabled:boolean; account_active:boolean};

export function EmployeeAdministration({canWrite}:{canWrite:boolean}) {const scope=useSessionScope();const api=scope.api;
  const [employees,setEmployees]=useState<Employee[]>([]);
  const [error,setError]=useState('');
  useEffect(()=>{let active=true;api<Employee[]>('/employees').then(v=>{if(active)setEmployees(v)}).catch(e=>{if(active)setError(e.message)});return()=>{active=false}},[]);
  return <section className="panel" aria-label={tr('m32777494bb')}><h2>{tr('mdc4b92a7d8')}</h2>
    <p className="muted">{tr('m18d154950f')}</p>
    {error&&<p className="error" role="alert">{error}</p>}
    {!employees.length&&!error&&<p>{tr('m597815ee6e')}</p>}
    {employees.map(employee=><EmployeeRow key={employee.id} employee={employee} canWrite={canWrite} onSaved={saved=>setEmployees(old=>old.map(e=>e.id===saved.id?saved:e))}/>)}
  </section>;
}

function EmployeeRow({employee,canWrite,onSaved}:{employee:Employee;canWrite:boolean;onSaved:(e:Employee)=>void}) {const scope=useSessionScope();const api=scope.api;
  const [limit,setLimit]=useState(String(hours(employee.daily_minutes)));
  const [email,setEmail]=useState(employee.email||'');
  const [saving,setSaving]=useState(false);
  const [message,setMessage]=useState('');
  const [error,setError]=useState('');
  return <article className="employee-admin-row" data-employee-admin={employee.id}>
    <h3>{employee.name}</h3>
    <form onSubmit={async event=>{event.preventDefault();if(saving||!canWrite)return;setSaving(true);setError('');setMessage('');try{
      const current=(await api<Employee[]>('/employees')).find(e=>e.id===employee.id);
      if(!current)throw Error(tr('mb023b533d4'));
      const minutes=Math.round(Number(limit)*60);
      if(!Number.isFinite(minutes)||minutes<0||minutes>1440||limit.trim()==='')throw Error(tr('m1497b0827c'));
      const saved=await api<Employee>('/employees','POST',{...current,daily_minutes:minutes,...(!current.provider?{email}: {})});
      onSaved(saved);setLimit(String(hours(saved.daily_minutes)));setMessage(tr('meed374ba3f'));
    }catch(cause){setError((cause as Error).message)}finally{setSaving(false)}}}>
      <label>{tr('m366cd4cce2')}<input aria-label={tr('m366cd4cce2')} type="number" min={0} max={24} step={0.25} value={limit} onChange={e=>setLimit(e.target.value)} disabled={!canWrite||saving} required/></label>
      <label>{tr('mf75d7752e9')}<input type="email" value={email} maxLength={254} onChange={e=>setEmail(e.target.value)} disabled={!canWrite||saving||!!employee.provider}/></label>
      <button type="submit" disabled={!canWrite||saving}>{saving?tr('m9c27c381dd'):tr('m9ad0c204cb')}</button>
    </form>
    {message&&<p role="status">{message}</p>}{error&&<p className="error" role="alert">{error}</p>}
    <EmployeeAccess employee={employee} canWrite={canWrite}/>
  </article>;
}

function EmployeeAccess({employee,canWrite}:{employee:Employee;canWrite:boolean}) {const scope=useSessionScope();const api=scope.api;
  const [access,setAccess]=useState<Access|null>(null);
  const [error,setError]=useState('');
  const [busy,setBusy]=useState(false);
  const working=useRef(false);
  const version=useRef(0);
  const pending=useRef<{id:string;reissue:boolean}|null>(null);
  const path='/employees/'+encodeURIComponent(employee.id)+'/invite';
  useEffect(()=>{let mounted=true;let running=false;
    const refresh=async()=>{if(running||working.current)return;running=true;const ticket=++version.current;try{const value=await api<Access>(path);if(mounted&&ticket===version.current)setAccess(value)}catch(cause){if(mounted&&ticket===version.current)setError((cause as Error).message)}finally{running=false}};
    void refresh();const timer=window.setInterval(refresh,5000);return()=>{mounted=false;clearInterval(timer)};
  },[path]);
  async function invite(reissue:boolean) {
    if(working.current||!canWrite||!access?.smtp_enabled||access.account_active)return;
    working.current=true;setBusy(true);setError('');version.current++;
    if(!pending.current||pending.current.reissue!==reissue)pending.current={id:crypto.randomUUID(),reissue};
    try {
      const result=await api<Invitation>(path,'POST',pending.current);
      setAccess(old=>old?{...old,invitation:result}:old);pending.current=null;
    }catch(cause){setError((cause as Error).message)}finally{working.current=false;setBusy(false)}
  }
  const invitation=access?.invitation;
  const stateNames:Record<string,string>={pending:tr('m23bfc6dbf9'),active:tr('mcd0b786a28'),expired:tr('me672dd9930'),revoked:tr('m69c2e3c6c6')};
  const deliveryNames:Record<string,string>={queued:tr('mf72bbc74ec'),sending:tr('mb7e75d6f53'),sent:tr('mba7dcf50ab'),failed:tr('m731cd1e5d1'),uncertain:tr('m6ab466fbd5')};
  return <div className="employee-access" data-employee-access={employee.id}>
    <h4>{tr('m3c4354f395')}</h4>
    {access?.account_active?<p>{tr('mae24a63d50')}</p>:<>
      {invitation&&<p data-invitation-state={invitation.state}>{stateNames[invitation.state]||tr('mc8558deeb5')} · {invitation.username} · {invitation.email}<br/>{deliveryNames[invitation.delivery_state]||tr('m1381e73409')} {tr('me0b8747e03')} {new Date(invitation.expires_at).toLocaleString(currentLocale())}</p>}
      <button type="button" onClick={()=>void invite(!!invitation)} disabled={!canWrite||busy||!access?.smtp_enabled||!employee.email}>{busy?tr('m2059873933'):invitation?tr('mc18dc528f5'):tr('m1455c927c0')}</button>
      {invitation&&<p className="muted">{tr('m891d601017')}</p>}
    </>}
    {access&&!access.smtp_enabled&&<p className="muted" data-invite-email-disabled>{tr('mf375440304')}</p>}
    {!employee.email&&<p className="muted">{tr('mc5d8c49ca1')}</p>}
    <p className="muted">{tr('m558bf546d0')}</p>
    {error&&<p className="error" role="alert">{error}</p>}
  </div>;
}
