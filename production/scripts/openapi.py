#!/usr/bin/env python3
"""Build the versioned API contract; no production data or secrets."""
import json,re
from pathlib import Path
root=Path(__file__).resolve().parents[1]
ref=lambda name:{'$ref':'#/components/schemas/'+name}
obj=lambda props,required=None:{'type':'object','properties':props,'additionalProperties':False,**({'required':required} if required else {})}
string={'type':'string'};integer={'type':'integer'};boolean={'type':'boolean'}
def typed(t):
 if t.startswith('[]'):return {'type':'array','items':typed(t[2:])}
 if t.startswith('map[string]'):return {'type':'object','additionalProperties':typed(t[11:])}
 return {'string':string,'int':integer,'int64':integer,'bool':boolean}.get(t,ref(t))
schemas={}
for name,body in re.findall(r'type (\w+) struct \{(.*?)\n\}',(root/'internal/planner/model.go').read_text(),re.S):
 props={m[2].split(',')[0]:typed(m[1]) for m in re.findall(r'(\w+)\s+([^\s]+)\s+`json:"([^"]+)"`',body)}
 if props:schemas[name]=obj(props)
schemas['Error']=obj({'error':obj({'code':string,'message':string},['code','message']),'request_id':string},['error'])
schemas['SaveProject']=obj({'project':ref('Project'),'override_reason':string},['project'])
schemas['Project']['properties']['calculation_state']={'type':'string','enum':['ready','needs_participation','blocked_capacity'],'description':'Computed by server. ready confirms schedule and shared capacity; other states withhold active internal task dates.'}
schemas['Participation']['properties']['week']={'type':'object','propertyNames':{'enum':['0','1','2','3','4','5','6']},'additionalProperties':ref('Activity'),'description':'Participation by weekday, 0 = Sunday. New participants require a schedule; missing legacy schedules remain drafts.'}
schemas['Activity']['properties']['daily_minutes']={'type':'integer','minimum':0,'maximum':1440,'description':'Active days need a positive quota within the start/end window. Date exceptions replace weekly settings.'}
schemas['Project']['properties']['calendar_versions']={'type':'object','propertyNames':{'pattern':'^(20[0-9]{2}|21[0-9]{2}|2200)$'},'additionalProperties':string,'maxProperties':201,'description':'Optional immutable annual calendar IDs. calendar_id is authoritative for its year. Omitted on update preserves stored pins; explicit empty map clears extra years.'}
schemas['Project']['properties']['provisional_years']={'type':'array','items':{'type':'integer','minimum':2000,'maximum':2200},'readOnly':True,'description':'Derived years actually calculated using provisional Monday-Friday days.'}
schemas['Project']['properties']['duration_mode']={'type':'string','enum':['working','calendar']}
schemas['Task']['properties']['status']={'type':'string','enum':['todo','doing','done']}
schemas['Link']['properties']['mode']={'type':'string','enum':['together','after','delay']}
schemas['Project']['properties']['tasks']['maxItems']=300
schemas['Project']['properties']['links']['maxItems']=1500
schemas['Task']['properties']['duration_days']={'type':'integer','minimum':0,'maximum':73414,'default':0,'description':'0 means capacity-derived duration. Positive values are a lower bound; insufficient availability extends duration without changing planned effort.'}
schemas['Task']['properties']['planned_minutes']={'type':'integer','minimum':0,'maximum':60000000}
for field in ['loads','warnings','provisional_years','calculation_state']:
 schemas['Project']['properties'][field]['readOnly']=True
for field in ['actual_minutes','summary','blocked']:schemas['Task']['properties'][field]['description']='Computed by the server.'
schemas['Task']['properties']['customer_email']={'type':'string','maxLength':254,'description':'One ASCII customer email address; empty disables recipient selection. Manager-editable.'}
schemas['TaskComment']['properties']['delivery_state']={'type':'string','enum':['internal','queued','sent','failed','uncertain']}
schemas['TaskComment']['properties']['created_at']={'type':'string','format':'date-time'}
schemas['Employee']['properties']['email']={'type':'string','description':'For local employees: administrator-supplied email. For external identities: read from the authoritative directory snapshot; submitted email is ignored.'}
schemas['EmployeeInvitation']=obj({'id':{'type':'string','format':'uuid'},'employee_id':string,'user_id':string,'username':string,'email':string,'state':{'type':'string','enum':['pending','active','revoked','expired']},'expires_at':{'type':'string','format':'date-time'},'delivery_state':{'type':'string','enum':['queued','sent','failed','uncertain']},'delivery_error':string})
schemas['DiagnosticEvent']=obj({'time':{'type':'string','format':'date-time'},'request_id':string,'route':string,'method':string,'status':integer,'duration_ms':integer},['time','request_id','route','method','status','duration_ms'])
schemas['DirectoryPerson']=obj({'id':string,'name':string,'email':string,'provider':string})
schemas['DirectoryEntry']=obj({'id':string,'name':string,'email':string,'username':string,'active':boolean,'groups':{'type':'array','items':string}},['id','name','active'])
schemas['Project']['properties']['external_reference']={'type':'string','maxLength':500,'description':'At most 500 UTF-8 bytes. Optional reference in an external system. No vendor-specific semantics or outbound request.'}
schemas['UpdaterStatus']=obj({'enabled':boolean,'state':{'type':'string','enum':['disabled','idle','checking','ready','updating','succeeded','failed','recovery_required','recovering']},'phase':{'type':'string','enum':['queued','fetch','build','preflight','backup','migrate','verify','restore','complete']},'message':string,'current_commit':{'type':'string','pattern':'^[a-f0-9]{40}$'},'candidate_commit':{'type':'string','pattern':'^[a-f0-9]{40}$'},'operation_id':{'type':'string','pattern':'^[a-f0-9]{32}$'},'recovery_available':boolean},['enabled','state','recovery_available'])
requests={
 'POST /admin/updates/check':obj({'ref':{'type':'string','minLength':1,'maxLength':121,'description':'Explicit official Git ref or commit; no arbitrary repository or revision expression'}},['ref']),
 'POST /admin/updates/apply':obj({'commit':{'type':'string','pattern':'^[a-f0-9]{40}$'},'confirmation':{'type':'string','description':'Must exactly equal commit'}},['commit','confirmation']),
 'POST /admin/updates/recover':obj({'operation_id':{'type':'string','pattern':'^[a-f0-9]{32}$'},'confirmation':{'type':'string','description':'Must exactly equal operation_id'}},['operation_id','confirmation']),
 'PUT /auth/preferences':obj({'locale':{'type':'string','enum':['en','ru','de','fr','es','pt-BR','zh-Hans']}},['locale']),
 'POST /directory/sync':obj({'provider':string,'entries':{'type':'array','maxItems':1000,'items':ref('DirectoryEntry')}},['provider','entries']),
 'POST /directory/ldap/sync':obj({}),
 'POST /diagnostics/reports':obj({'encrypted':{'type':'string','maxLength':262144,'description':'Compact JWE, RSA-OAEP-256/A256GCM, current support public key. Plaintext is rejected.'}},['encrypted']),
 'POST /employees/{id}/invite':obj({'id':{'type':'string','format':'uuid'},'reissue':{'type':'boolean','default':False}},['id']),
 'POST /projects/{id}/tasks/{task}/comments':obj({'id':{'type':'string','format':'uuid'},'text':{'type':'string','minLength':1,'maxLength':8000,'description':'At most 8000 UTF-8 bytes.'},'send_to_customer':boolean},['id','text','send_to_customer']),
 'POST /auth/bootstrap/resume':obj({'token':string},['token']),
 'POST /trash/{id}/restore':obj({'revision':integer},['revision']),
 'POST /auth/login':obj({'username':string,'password':string,'code':string},['username','password','code']),
 'POST /auth/bootstrap':obj({'token':{'type':'string','description':'Omit for initial administrator setup; required for invitation or recovery enrollment'},'username':string,'name':string,'password':{'type':'string','minLength':15,'maxLength':128}},['username','name','password']),
 'POST /auth/bootstrap/confirm':obj({'token':string,'code':string},['token','code']),
 'POST /auth/logout':obj({}),
 'POST /auth/password':obj({'old_password':string,'new_password':string,'code':string},['old_password','new_password','code']),
 'POST /projects':obj({'title':string,'start':string,'duration_mode':string,'calendar_id':string,'calendar_versions':schemas['Project']['properties']['calendar_versions']},['title','start','duration_mode','calendar_id']),
 'PUT /projects/{id}':ref('SaveProject'),'POST /projects/{id}/preview':ref('SaveProject'),
 'DELETE /projects/{id}':obj({'revision':integer},['revision']),'POST /projects/{id}/undo':obj({'revision':integer},['revision']),
 'POST /employees':ref('Employee'),'POST /calendars':ref('Calendar'),
 'POST /users':obj({'username':string,'name':string,'admin':boolean},['username','name']),
 'PATCH /users/{id}':obj({'active':boolean},['active']),
 'PUT /projects/{id}/members':obj({'user_id':string,'role':{'type':'string','enum':['manager','member','viewer','']}},['user_id','role']),
 'POST /projects/{id}/time-entries':ref('TimeEntry'),
 'POST /import':obj({'project':ref('Project'),'preview':boolean},['project','preview']),
 'POST /tokens':obj({'name':string,'project_ids':{'type':'array','items':string},'scopes':{'type':'array','items':{'type':'string','enum':['project:read','project:write','export','directory:read','directory:write']}},'days':{'type':'integer','minimum':1,'maximum':90}},['name','project_ids','scopes','days']),
}
source=(root/'internal/planner/app.go').read_text()
routes=sorted(set(re.findall(r'"(GET|POST|PUT|PATCH|DELETE) /api/v1([^" ]+)"',source)))
paths={}
for method,path in routes:
 key=method+' '+path
 public=path in ['/diagnostics/reports','/diagnostics/key','/auth/status','/auth/login','/auth/bootstrap','/auth/bootstrap/confirm','/auth/bootstrap/resume']
 params=[{'name':x,'in':'path','required':True,'schema':string} for x in re.findall(r'{(\w+)}',path)]
 if method not in ['GET','HEAD'] and not public:params.append({'name':'X-CSRF-Token','in':'header','required':False,'schema':string,'description':'Required for cookie mutations; not for Bearer tokens. Browser writes require the exact deployment Origin.'})
 if path=='/directory/people':
  params.extend([{'name':'q','in':'query','required':False,'schema':{'type':'string','maxLength':100}}, {'name':'source','in':'query','required':False,'schema':{'type':'string','enum':['ldap','api']},'description':'LDAP is the configured provider; all other namespaces are API sources.'}, {'name':'provider','in':'query','required':False,'schema':string,'description':'Exact provider namespace; may be combined with source.'}])
 if path=='/audit':params.append({'name':'before','in':'query','schema':integer,'description':'ID cursor, 100 records per page.'})
 success=ref('Project') if key in ['POST /projects','PUT /projects/{id}','POST /projects/{id}/preview','POST /projects/{id}/undo','GET /projects/{id}/export'] else {'type':'object'}
 if key=='POST /diagnostics/reports':success=obj({'id':string},['id'])
 if key=='GET /diagnostics/key':success=obj({'configured':boolean,'key':obj({k:string for k in ['kty','n','e','alg','use','kid']})},['configured'])
 if key=='GET /diagnostics/events':success=obj({'events':{'type':'array','maxItems':500,'items':ref('DiagnosticEvent')},'retention_minutes':{'type':'integer','const':15},'capacity':{'type':'integer','const':500}},['events','retention_minutes','capacity'])
 if key=='GET /employees/{id}/invite':success=obj({'invitation':{'anyOf':[ref('EmployeeInvitation'),{'type':'null'}]},'smtp_enabled':boolean,'account_active':boolean})
 if key=='POST /employees/{id}/invite':success=ref('EmployeeInvitation')
 if key=='GET /projects/{id}/tasks/{task}/comments':success=obj({'comments':{'type':'array','items':ref('TaskComment')},'email_enabled':boolean,'can_comment':boolean,'customer_email':string,'customer':boolean})
 if key=='POST /projects/{id}/tasks/{task}/comments':success=ref('TaskComment')
 if key=='GET /projects/{id}':success=obj({'project':ref('Project'),'role':string})
 for resource,model in [('/employees','Employee'),('/calendars','Calendar'),('/users','User'),('/projects/{id}/time-entries','TimeEntry')]:
  if path==resource and method=='GET':success={'type':'array','items':ref(model)}
 if path.startswith('/admin/updates'):success=ref('UpdaterStatus')
 if key=='GET /auth/me':success=obj({'user':ref('User'),'csrf':string,'version':string})
 if key=='GET /auth/status':success=obj({'setup_required':boolean,'version':string,})
 if key=='POST /auth/bootstrap':success=obj({'secret':string,'otpauth_uri':string,'enrollment_token':string})
 if key=='POST /auth/bootstrap/confirm':success=obj({'recovery_codes':{'type':'array','items':string}})
 if key in ['GET /projects/{id}/members','GET /projects/{id}/member-candidates','GET /trash','GET /tokens','GET /audit','GET /directory/people']:success={'type':'array','items':{'type':'object'}}
 if key=='GET /directory/status':success=obj({'ldap':obj({'configured':boolean,'enabled':boolean,'provider':string}),'sources':{'type':'array','items':string},'source_details':{'type':'array','items':obj({'provider':string,'kind':{'type':'string','enum':['ldap','api']}},['provider','kind'])}})
 if key in ['POST /directory/sync','POST /directory/ldap/sync']:success=obj({'processed':{'type':'integer','minimum':0},'disabled':{'type':'integer','minimum':0},'missing':{'type':'integer','minimum':0}},['processed','disabled','missing'])
 if key=='GET /directory/people':success={'type':'array','items':ref('DirectoryPerson')}
 if key=='GET /projects':success={'type':'array','items':obj({'id':string,'title':string,'brief':string,'revision':integer,'task_count':integer})}
 status='201' if key in ['POST /projects','POST /users','POST /calendars','POST /tokens','POST /projects/{id}/time-entries','POST /import'] else '200'
 operation={'operationId':method.lower()+'_'+re.sub(r'[^a-zA-Z0-9]+','_',path).strip('_'),'summary':key,'security':[] if public else [{'session':[]}],'parameters':params,'responses':{status:{'description':'Success','content':{'application/json':{'schema':success}}},**{str(n):{'description':label,'content':{'application/json':{'schema':ref('Error')}}} for n,label in [(401,'Authentication required'),(403,'Forbidden'),(404,'Not found or hidden by permissions'),(409,'Revision conflict'),(413,'Request too large'),(422,'Validation failed'),(429,'Rate limit'),(503,'External service unavailable')]}}}
 service_allowed=(key in ['GET /directory/people','POST /directory/sync','GET /projects'] or (path.startswith('/projects/') and not path.endswith(('/members','/member-candidates'))))
 if not public and service_allowed:operation['security'].append({'serviceToken':[]})
 if key=='POST /employees/{id}/invite':operation['responses']['201']={'description':'Invitation created; identical operation replay returns 200.','content':{'application/json':{'schema':success}}}
 if key=='POST /diagnostics/reports':
  operation['description']='Stores an encrypted report locally, including before login. Exact Origin required. No HTTP listing/download; operator accesses the persistent volume. Identical JWE retries return the same ID. Size/rate/storage limits apply; the server cannot authenticate ciphertext without the private key.'
 if key=='POST /diagnostics/reports':
  operation['responses']['201']={'description':'Report stored','content':{'application/json':{'schema':success}}}
  operation['responses']['507']={'description':'Storage full','content':{'application/json':{'schema':ref('Error')}}}
 if key=='GET /diagnostics/events':
  operation['security']=[{'session':[]}]
  operation['description']='In-memory API metadata: up to 500 events for 15 minutes. Users see their own events; administrators see all. No real URLs, IDs, queries, bodies, headers or error text. Service tokens rejected. Restart clears the buffer.'
 if key=='POST /employees/{id}/invite':
  operation['description']='Administrator only. Caller supplies an action UUID; identical retries return 200. reissue revokes the previous pending invitation. Disabled SMTP returns 503 before account creation. Secret links are not returned by the API.'
  operation['responses']['201']={'description':'Account, binding and encrypted invitation queue created.','content':{'application/json':{'schema':success}}}
 if key=='POST /projects/{id}/tasks/{task}/comments':
  operation['responses']['201']={'description':'Comment created; email only with explicit opt-in. 200 denotes identical UUID/text/option retry.','content':{'application/json':{'schema':success}}}
 if path.startswith('/admin/updates'):
  operation['description']='Administrator cookie session only; service tokens rejected. Bounded Unix-socket relay to an explicitly configured host updater. No Docker access in the API. A local administrator audit request is written before mutation dispatch; audit failure prevents the host request. Maximum request JSON 1 KiB. Status messages are sanitized, with no paths or logs. Disabled GET returns enabled=false; configured but unavailable service returns 503.'
  if method=='POST':operation['responses']['202']={'description':'Asynchronous operation accepted','content':{'application/json':{'schema':success}}}
 if path.startswith('/directory/'):
  operation['description']='Administrator session or an explicitly scoped administrator service token (directory:read for search, directory:write for synchronization). LDAP configuration/status/sync require an administrator cookie. Explicit inactive records revoke linked access; missing records are reported without removal.'
 if key in requests:operation['requestBody']={'required':True,'content':{'application/json':{'schema':requests[key]}}}
 if method in ['POST','PUT','PATCH'] and key not in requests:raise ValueError('Missing request contract: '+key)
 paths.setdefault(path,{})[method.lower()]=operation
spec={'openapi':'3.1.2','info':{'title':'gmVector API','version':'0.2.0','description':'REST v1. Browser cookie + CSRF; scoped Bearer tokens for integrations. Permissions and project boundaries enforced server-side. Optional integrations are disabled until configured. Maximum JSON: 5 MiB. Revision conflict: 409; fetch the current project before retrying.'},'servers':[{'url':'/api/v1'}],'paths':paths,'components':{'schemas':schemas,'securitySchemes':{'session':{'type':'apiKey','in':'cookie','name':'__Host-gma'},'serviceToken':{'type':'http','scheme':'bearer','description':'Opaque random service token, constrained by scopes and project IDs at issuance. Not a JWT.'}}}}
for p in [root.parent/'docs/contracts/openapi.json',root/'web/dist/openapi.json']:
 p.parent.mkdir(parents=True,exist_ok=True);p.write_text(json.dumps(spec,ensure_ascii=False,indent=2)+'\n')
print('OpenAPI:',len(routes),'operations')
