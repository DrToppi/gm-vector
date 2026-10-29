#!/usr/bin/env python3
"""Offline support key setup/decryption. Never prints secrets or report contents."""
import argparse,base64,getpass,hashlib,json,os,re,sys
from pathlib import Path
from cryptography.hazmat.primitives import hashes,serialization
from cryptography.hazmat.primitives.asymmetric import padding,rsa
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

MAX_FILE=3*1024*1024

def b64(data):return base64.urlsafe_b64encode(data).decode('ascii').rstrip('=')
def unb64(value):
 if not isinstance(value,str) or not re.fullmatch(r'[A-Za-z0-9_-]+',value):raise ValueError('invalid encoding')
 raw=base64.urlsafe_b64decode(value+'='*((-len(value))%4))
 if b64(raw)!=value:raise ValueError('noncanonical encoding')
 return raw

def public_jwk(key):
 public=key.public_key() if isinstance(key,rsa.RSAPrivateKey) else key
 if not isinstance(public,rsa.RSAPublicKey) or public.key_size!=3072 or public.public_numbers().e!=65537:raise ValueError('unsupported key')
 n=b64(public.public_numbers().n.to_bytes(384,'big'))
 canonical=json.dumps({'e':'AQAB','kty':'RSA','n':n},separators=(',',':')).encode('ascii')
 return {'kty':'RSA','n':n,'e':'AQAB','alg':'RSA-OAEP-256','use':'enc','kid':b64(hashlib.sha256(canonical).digest())}

def decrypt_report(compact,key):
 if not isinstance(compact,str) or len(compact)>MAX_FILE:raise ValueError('report too large')
 parts=compact.strip().split('.')
 if len(parts)!=5:raise ValueError('invalid JWE')
 header=json.loads(unb64(parts[0]))
 expected={'alg':'RSA-OAEP-256','enc':'A256GCM','kid':public_jwk(key)['kid'],'typ':'gma-bug-report+jwe','cty':'application/json'}
 if header!=expected:raise ValueError('unsupported header or key')
 wrapped,iv,ciphertext,tag=map(unb64,parts[1:])
 if len(wrapped)!=384 or len(iv)!=12 or len(tag)!=16:raise ValueError('invalid JWE length')
 cek=key.decrypt(wrapped,padding.OAEP(mgf=padding.MGF1(hashes.SHA256()),algorithm=hashes.SHA256(),label=None))
 if len(cek)!=32:raise ValueError('invalid content key')
 plaintext=AESGCM(cek).decrypt(iv,ciphertext+tag,parts[0].encode('ascii'))
 if len(plaintext)>2*1024*1024:raise ValueError('report too large')
 report=json.loads(plaintext)
 if not isinstance(report,dict):raise ValueError('report is not an object')
 return report

def write_new(path,data):
 # O_EXCL prevents overwriting existing keys/reports or following an existing symlink.
 fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'wb') as f:f.write(data)

def password(confirm=False):
 if not sys.stdin.isatty():raise ValueError('interactive terminal required; passwords must not be piped')
 value=getpass.getpass('Пароль закрытого ключа: ')
 if not value or (confirm and len(value)<16):raise ValueError('use a passphrase of at least 16 characters')
 if confirm and value!=getpass.getpass('Повторите пароль: '):raise ValueError('passphrases do not match')
 return value.encode('utf-8')

def main():
 parser=argparse.ArgumentParser(description=__doc__)
 sub=parser.add_subparsers(dest='command',required=True)
 gen=sub.add_parser('keygen',help='Create encrypted private PKCS8 and public JWK; never overwrite files')
 gen.add_argument('--private',type=Path,required=True);gen.add_argument('--public',type=Path,required=True)
 dec=sub.add_parser('decrypt',help='Decrypt locally to a new JSON file; never print its contents')
 dec.add_argument('--private',type=Path,required=True);dec.add_argument('--input',type=Path,required=True);dec.add_argument('--output',type=Path,required=True)
 args=parser.parse_args()
 try:
  if args.command=='keygen':
   if args.private.resolve()==args.public.resolve() or args.private.exists() or args.public.exists():raise ValueError('destination exists')
   secret=password(True)
   key=rsa.generate_private_key(public_exponent=65537,key_size=3072)
   private=key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.BestAvailableEncryption(secret))
   public=json.dumps(public_jwk(key),indent=2).encode('ascii')
   write_new(args.private,private)
   write_new(args.public,public)
   print('Ключи созданы. В приложение переносите только публичный JWK. Сохраните резервную копию закрытого ключа отдельно.')
  else:
   if args.output.exists():raise ValueError('destination exists')
   if args.private.stat().st_size>16384 or args.input.stat().st_size>MAX_FILE:raise ValueError('file too large')
   pem=args.private.read_bytes()
   if not pem.startswith(b'-----BEGIN ENCRYPTED PRIVATE KEY-----'):raise ValueError('encrypted PKCS8 required')
   key=serialization.load_pem_private_key(pem,password=password())
   report=decrypt_report(args.input.read_text(encoding='ascii'),key)
   write_new(args.output,json.dumps(report,ensure_ascii=False,indent=2).encode('utf-8'))
   print('Отчёт расшифрован в указанный локальный файл. Его содержимое не отправлено в сеть.')
 except (Exception,KeyboardInterrupt):
  # Exception strings may contain filenames or contents; do not log them.
  print('Операция не выполнена. Проверьте пароль, формат/целостность файла, ключ и доступность нового пути; существующие файлы не перезаписываются.',file=sys.stderr)
  return 1
 return 0
if __name__=='__main__':sys.exit(main())
