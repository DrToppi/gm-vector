#!/usr/bin/env python3
"""Prepare separate runtime/migration roles. Dry-run unless --apply; never logs credentials."""
import argparse,json,os,re,secrets,subprocess,sys
from pathlib import Path
from urllib.parse import urlsplit,urlunsplit,quote,unquote

TABLES=('schema_versions','settings','users','enrollments','sessions','login_limits','employees','calendars','projects','memberships','participation','tasks','dependencies','assignments','time_entries','snapshots','audit','api_tokens','outbox','event_mappings','task_comments','employee_invitations','directory_entries')
RUNTIME='planner_runtime'
MIGRATOR='planner_migrator'

def docker_command():
 # Honor explicit Docker context/host; otherwise prefer the existing local rootless socket.
 socket=Path('/run/user')/str(os.getuid())/'docker.sock' if hasattr(os,'getuid') else None
 if os.environ.get('DOCKER_HOST') or os.environ.get('DOCKER_CONTEXT'):return ['docker']
 return ['docker','--host','unix://'+str(socket)] if socket and socket.exists() else ['docker']

def literal(value):
 return "'"+value.replace("'","''")+"'"

def url_for(original,user,password):
 u=urlsplit(original)
 if u.scheme not in ('postgres','postgresql') or not u.hostname or u.path!='/planner':raise ValueError('unexpected database URL')
 host='['+u.hostname+']' if ':' in u.hostname else u.hostname
 return urlunsplit((u.scheme,quote(user,safe='')+':'+quote(password,safe='')+'@'+host+(':'+str(u.port) if u.port else ''),u.path,u.query,''))

def private_json(path,data,exclusive=False):
 if exclusive:
  fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
  with os.fdopen(fd,'w') as f:json.dump(data,f,indent=2);f.flush();os.fsync(f.fileno())
  return
 tmp=path.with_name(path.name+'.roles-tmp')
 private_json(tmp,data,True);os.replace(tmp,path)

def prepare(config_dir,apply):
 source=config_dir/'config.json';config=json.loads(source.read_text());current=urlsplit(config['database_url'])
 if current.username not in ('planner',RUNTIME):raise ValueError('unsupported runtime role')
 identity=config_dir/'instance.id'
 if identity.is_symlink() or not identity.is_file() or identity.stat().st_size>128:raise ValueError('missing installation identity file')
 instance=identity.read_text().strip()
 if not isinstance(instance,str) or not re.fullmatch(r'[A-Za-z0-9_-]{1,128}',instance):raise ValueError('missing instance identity')
 migration_path=config_dir/'migration-config.json';pending=config_dir/'runtime-config.pending.json'
 if current.username==RUNTIME:
  runtime=config
  migration=json.loads(migration_path.read_text())
 elif pending.exists() and migration_path.exists():
  runtime=json.loads(pending.read_text());migration=json.loads(migration_path.read_text())
  if urlsplit(runtime.get('database_url','')).username!=RUNTIME:raise ValueError('pending config has wrong runtime principal')
 else:
  if pending.exists() or migration_path.exists():raise ValueError('incomplete role setup files; review before retry')
  runtime=dict(config);runtime['database_url']=url_for(config['database_url'],RUNTIME,secrets.token_urlsafe(36))
  migration={'database_url':url_for(config['database_url'],MIGRATOR,secrets.token_urlsafe(36)),'runtime_role':RUNTIME}
  if apply:
   private_json(pending,runtime,True);private_json(migration_path,migration,True)
 ru,mu=urlsplit(runtime['database_url']),urlsplit(migration['database_url'])
 if ru.username!=RUNTIME or mu.username!=MIGRATOR or migration.get('runtime_role')!=RUNTIME:raise ValueError('wrong configured principals')
 if not ru.password or not mu.password or ru.password==mu.password:raise ValueError('independent passwords required')
 if (ru.hostname,ru.port,ru.path,ru.query)!=(mu.hostname,mu.port,mu.path,mu.query):raise ValueError('database destinations do not match')
 if (ru.hostname,ru.port,ru.path,ru.query)!=(current.hostname,current.port,current.path,current.query):raise ValueError('pending database destination changed')
 return config,runtime,migration,instance

def setup_sql(runtime,migration,instance):
 out=["SET standard_conforming_strings=on; BEGIN; SELECT pg_advisory_xact_lock(789503);"]
 allowed=','.join(literal(v) for v in TABLES+('audit_id_seq',))
 out.append("DO $guard$ BEGIN IF NOT (SELECT rolsuper FROM pg_roles WHERE rolname=current_user) THEN RAISE EXCEPTION 'bootstrap role required'; END IF; IF EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p','S','v','m','f') AND c.relname NOT IN ("+allowed+")) THEN RAISE EXCEPTION 'unexpected objects in application schema'; END IF; END $guard$;")
 for role,config,kind in [(MIGRATOR,migration,'migrator'),(RUNTIME,runtime,'runtime')]:
  marker='GMA Planner role v1 '+instance+' '+kind
  password=unquote(urlsplit(config['database_url']).password)
  out.append("DO $role$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname="+literal(role)+") THEN IF (SELECT shobj_description(oid,'pg_authid') FROM pg_roles WHERE rolname="+literal(role)+") IS DISTINCT FROM "+literal(marker)+" THEN RAISE EXCEPTION 'existing role is not managed by this instance'; END IF; ELSE CREATE ROLE "+role+"; END IF; IF EXISTS(SELECT 1 FROM pg_auth_members m JOIN pg_roles r ON r.oid=m.member WHERE r.rolname="+literal(role)+") THEN RAISE EXCEPTION 'unexpected role membership'; END IF; END $role$;")
  out.append('ALTER ROLE '+role+' LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT PASSWORD '+literal(password)+';')
  out.append('COMMENT ON ROLE '+role+' IS '+literal(marker)+';')
 out.extend(['REVOKE CREATE,TEMPORARY ON DATABASE planner FROM PUBLIC, '+RUNTIME+','+MIGRATOR+';', 'GRANT CONNECT ON DATABASE planner TO '+RUNTIME+','+MIGRATOR+';', 'ALTER SCHEMA public OWNER TO '+MIGRATOR+';', 'REVOKE ALL ON SCHEMA public FROM PUBLIC;', 'REVOKE CREATE ON SCHEMA public FROM '+RUNTIME+';', 'GRANT USAGE ON SCHEMA public TO '+RUNTIME+';'])
 out.append("DO $objects$ DECLARE item record; BEGIN FOR item IN SELECT c.relname,c.relkind FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p') LOOP EXECUTE format('ALTER TABLE public.%I OWNER TO planner_migrator',item.relname); EXECUTE format('REVOKE ALL ON TABLE public.%I FROM planner_runtime',item.relname); IF item.relname='schema_versions' THEN EXECUTE format('GRANT SELECT ON TABLE public.%I TO planner_runtime',item.relname); ELSE EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON TABLE public.%I TO planner_runtime',item.relname); END IF; END LOOP; FOR item IN SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='S' LOOP EXECUTE format('ALTER SEQUENCE public.%I OWNER TO planner_migrator',item.relname); EXECUTE format('GRANT USAGE,SELECT ON SEQUENCE public.%I TO planner_runtime',item.relname); END LOOP; END $objects$;")
 out.append('COMMIT;')
 return '\n'.join(out)

def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--config',type=Path,required=True);p.add_argument('--container',default='gma-planner-v01-db-1');p.add_argument('--apply',action='store_true');a=p.parse_args()
 try:
  original,runtime,migration,instance=prepare(a.config,a.apply)
  if not a.apply:
   print('DRY RUN: create/check planner_runtime and planner_migrator; move only known public application objects; write runtime DSN after successful transaction. No database or config changes.');return 0
  command=docker_command()+['exec','-i',a.container,'psql','-X','-q','-U','planner','-d','planner','-v','ON_ERROR_STOP=1']
  result=subprocess.run(command,input=setup_sql(runtime,migration,instance).encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=60)
  if result.returncode:raise RuntimeError('database setup failed')
  before=a.config/'config.before-db-roles.json'
  if not before.exists():private_json(before,original,True)
  private_json(a.config/'config.json',runtime)
  print('Database roles configured; runtime config switched. Sync secrets and run the migration service before starting API/worker. Credentials were not printed.')
  return 0
 except (Exception,KeyboardInterrupt):
  print('Role setup failed. Existing data were not dropped. Inspect configuration/role ownership in a protected environment; credentials and SQL were not logged. Pending files are retained for safe retry.',file=sys.stderr);return 1
if __name__=='__main__':sys.exit(main())
