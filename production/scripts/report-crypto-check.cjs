// Ephemeral synthetic keys only. Cross-runtime WebCrypto JWE -> Python cryptography.
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict'),{webcrypto}=require('node:crypto'),{spawnSync}=require('node:child_process');
const ts=require('../web/node_modules/typescript');
const source=fs.readFileSync(require.resolve('../web/src/reportCrypto.ts'),'utf8');
const compiled=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText;
const sandbox={exports:{},require:name=>{assert.equal(name,'./i18n');return require('./test-i18n.cjs')},crypto:webcrypto,TextEncoder,Uint8Array,btoa};vm.runInNewContext(compiled,sandbox);
(async()=>{
 const pair=await webcrypto.subtle.generateKey({name:'RSA-OAEP',modulusLength:3072,publicExponent:new Uint8Array([1,0,1]),hash:'SHA-256'},true,['wrapKey','unwrapKey']);
 const jwk=await webcrypto.subtle.exportKey('jwk',pair.publicKey);
 const canonical=JSON.stringify({e:jwk.e,kty:jwk.kty,n:jwk.n});
 const kid=Buffer.from(await webcrypto.subtle.digest('SHA-256',new TextEncoder().encode(canonical))).toString('base64url');
 const key={kty:'RSA',n:jwk.n,e:jwk.e,alg:'RSA-OAEP-256',use:'enc',kid};
 const report={schema:1,description:'Синтетический тест ✅',events:[{status:500}]};
 const encrypted=await sandbox.exports.encryptReport(report,key);
 const second=await sandbox.exports.encryptReport(report,key);assert.notEqual(encrypted,second,'fresh randomness per report');
 assert.equal(encrypted.split('.').length,5);assert(!encrypted.includes('Синтетический'));
 await assert.rejects(()=>sandbox.exports.encryptReport(report,{...key,kid:'a'.repeat(43)}));
 await assert.rejects(()=>sandbox.exports.encryptReport(report,{...key,alg:'RSA1_5'}));
 await assert.rejects(()=>sandbox.exports.encryptReport({text:'x'.repeat(2*1024*1024)},key));
 const privateDer=Buffer.from(await webcrypto.subtle.exportKey('pkcs8',pair.privateKey)).toString('base64');
 const script=`import importlib.util,json,sys,base64\nfrom cryptography.hazmat.primitives.serialization import load_der_private_key\nspec=importlib.util.spec_from_file_location('bug_report',sys.argv[1]);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)\na=json.load(sys.stdin);key=load_der_private_key(base64.b64decode(a['private']),None)\nassert m.decrypt_report(a['encrypted'],key)==a['report']\np=a['encrypted'].split('.')\nfor i in range(5):\n q=p.copy();raw=bytearray(m.unb64(q[i]));raw[-1]^=1;q[i]=m.b64(raw)\n try:m.decrypt_report('.'.join(q),key)\n except Exception:pass\n else:raise AssertionError('tampering accepted')\nprint('Cross-runtime roundtrip and all five JWE segment tampering checks passed')\n`;
 const r=spawnSync(process.env.GMA_PYTHON_BIN||'python3',['-c',script,require.resolve('./bug-report.py')],{input:JSON.stringify({private:privateDer,encrypted,report}),encoding:'utf8',timeout:20000});
 assert.equal(r.status,0,r.stderr);process.stdout.write(r.stdout);
 console.log('Report encryption: fresh randomness, invalid key/algorithm and size bound passed.');
})().catch(e=>{console.error(e);process.exit(1)});
