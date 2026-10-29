#!/usr/bin/env python3
import json,os,subprocess
from pathlib import Path
work=Path('/tmp/gma-security');work.mkdir(exist_ok=True,mode=0o700)
docker=['docker','--host',f'unix:///run/user/{os.getuid()}/docker.sock']
scanner='aquasec/trivy@sha256:790fd66d1697395d8693d1acaa1ea702406a20f109b6fc7bac8db623eca6c06c'
for label,image in [('planner','gm-vector:local'),('proxy','gm-vector-proxy:local'),('postgres','gm-vector-db:18.6')]:
 archive=work/(label+'.tar')
 subprocess.run(docker+['save','-o',str(archive),image],check=True)
 for fmt,suffix in [('json','vulns.json'),('cyclonedx','sbom.json')]:
  subprocess.run(docker+['run','--rm','-v',str(work)+':/scan','-v','gma-trivy-cache:/root/.cache/trivy',scanner,'image','--input','/scan/'+archive.name,'--quiet','--scanners','vuln','--format',fmt,'--output','/scan/'+label+'-'+suffix],check=True)
 report=json.loads((work/(label+'-vulns.json')).read_text());vulns=[v for r in report.get('Results',[]) for v in r.get('Vulnerabilities',[])];print(json.dumps({'image':label,'vulnerabilities':len(vulns),'high_critical':[{'id':v['VulnerabilityID'],'package':v['PkgName'],'fixed':v.get('FixedVersion',''),'severity':v['Severity']} for v in vulns if v['Severity'] in ['HIGH','CRITICAL']]}),flush=True)
