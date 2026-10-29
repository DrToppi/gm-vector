#!/usr/bin/env python3
"""Synthetic Docker setup regression: no production container, config or port."""
import importlib.util,json,os,secrets,subprocess,sys,tempfile,time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('roles',ROOT/'scripts/database-roles.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
DOCKER=module.docker_command()
IMAGE='postgres:18.6-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873'
def run(args,data=None,ok=True):
 p=subprocess.run(DOCKER+args,input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=60)
 if ok and p.returncode:raise RuntimeError('synthetic Docker step failed (output withheld)')
 return p
name='gma-role-check-'+secrets.token_hex(6)
try:
 run(['run','-d','--name',name,'--network','none','--tmpfs','/var/lib/postgresql','-e','POSTGRES_HOST_AUTH_METHOD=trust','-e','POSTGRES_USER=planner','-e','POSTGRES_DB=planner',IMAGE])
 for _ in range(40):
  if run(['exec',name,'pg_isready','-h','127.0.0.1','-U','planner'],ok=False).returncode==0:break
  time.sleep(.25)
 def sql(value,user='planner',ok=True):return run(['exec','-i',name,'psql','-X','-qAt','-v','ON_ERROR_STOP=1','-U',user,'-d','planner'],value.encode(),ok)
 sql((ROOT/'internal/planner/schema.sql').read_text()+"\nINSERT INTO settings VALUES('synthetic-preserved','true');")
 with tempfile.TemporaryDirectory(prefix='gma-role-config-') as tmp:
  config=Path(tmp);original={'database_url':'postgres://planner:synthetic-bootstrap@db:5432/planner?sslmode=disable'}
  (config/'instance.id').write_text('synthetic-role-test');(config/'instance.id').chmod(0o600)
  module.private_json(config/'config.json',original,True)
  before=set(config.iterdir());module.prepare(config,False);assert set(config.iterdir())==before
  # Exercise the operator entrypoint including atomic config switching.
  result=subprocess.run([sys.executable,str(ROOT/'scripts/database-roles.py'),'--config',str(config),'--container',name,'--apply'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=60)
  assert result.returncode==0, 'operator apply failed (output withheld)'
  assert json.loads((config/'config.json').read_text())['database_url'].startswith('postgres://planner_runtime:')
  assert json.loads((config/'config.before-db-roles.json').read_text())==original
  source,runtime,migration,instance=module.prepare(config,True)
  assert sql("SELECT value FROM settings WHERE key='synthetic-preserved'",module.RUNTIME).stdout.strip()==b'true'
  for statement in ['CREATE ROLE forbidden_role','CREATE DATABASE forbidden_database','CREATE TABLE forbidden(v text)',"UPDATE schema_versions SET checksum='bad'"]:
   assert sql(statement,module.RUNTIME,False).returncode!=0
  # New tables must remain compatible with repeated role setup after migrations.
  for migration_file in sorted((ROOT/'internal/planner').glob('schema-*.sql')):sql(migration_file.read_text(),module.MIGRATOR)
  # Repair explicit drift as well as PUBLIC defaults on a repeated setup.
  sql('GRANT CREATE,TEMPORARY ON DATABASE planner TO planner_runtime; GRANT CREATE ON SCHEMA public TO planner_runtime')
  sql(module.setup_sql(runtime,migration,instance))
  assert sql("SELECT has_database_privilege('planner_runtime','planner','CREATE') OR has_database_privilege('planner_runtime','planner','TEMPORARY') OR has_schema_privilege('planner_runtime','public','CREATE')").stdout.strip()==b'f'
  assert sql("SELECT count(*) FROM directory_entries",module.RUNTIME).stdout.strip()==b'0'
  # Existing role names with a foreign marker fail closed.
  sql("COMMENT ON ROLE planner_runtime IS 'synthetic-foreign-owner'")
  assert sql(module.setup_sql(runtime,migration,instance),ok=False).returncode!=0
  assert sql("SELECT value FROM settings WHERE key='synthetic-preserved'",module.RUNTIME).stdout.strip()==b'true'
  print('PASS: dry-run untouched; legacy data retained; ownership/grants separated; runtime ROLE/DB/DDL/schema metadata denied; idempotent setup; foreign role collision rejected.')
finally:
 run(['rm','-f',name],ok=False)
