#!/usr/bin/env python3
"""Optional host updater. Unix IPC only; this process owns Docker privileges."""
import argparse, hmac, http.server, json, os, re, signal, socket, socketserver, threading
from pathlib import Path
from update_engine import Engine, private_tree

MAX_BODY = 16384
FIELDS = {'check':{'ref'},'apply':{'commit','confirmation'},'recover':{'operation_id','confirmation'}}


class Server(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True
    request_queue_size = 8
    def __init__(self,*args,**kwargs):
        self.slots=threading.BoundedSemaphore(8); super().__init__(*args,**kwargs)
    def process_request(self,request,address):
        if not self.slots.acquire(blocking=False):
            self.shutdown_request(request); return
        try: super().process_request(request,address)
        except BaseException:
            self.slots.release(); raise
    def process_request_thread(self,request,address):
        try: super().process_request_thread(request,address)
        finally: self.slots.release()


class Handler(http.server.BaseHTTPRequestHandler):
    server_version = 'gmVector-updater'; sys_version = ''
    def log_message(self, *args): pass
    def setup(self):
        super().setup(); self.connection.settimeout(5)
    def reply(self, code, value):
        raw=json.dumps(value).encode(); self.send_response(code)
        self.send_header('Content-Type','application/json'); self.send_header('Content-Length',str(len(raw)))
        self.end_headers(); self.wfile.write(raw)
    def parse_request(self):
        ok=super().parse_request()
        if ok and self.requestline.split()[1] != self.path:
            self.send_error(404); return False
        return ok
    def authorized(self):
        values=self.headers.get_all('Authorization',[])
        return len(values)==1 and hmac.compare_digest(values[0].encode('utf-8'), ('Bearer '+self.server.token).encode('ascii'))
    def do_GET(self):
        if not self.authorized(): return self.reply(401,{'error':'unauthorized'})
        if self.path!='/status': return self.reply(404,{'error':'not_found'})
        self.reply(200,self.server.engine.status())
    def do_POST(self):
        if not self.authorized(): return self.reply(401,{'error':'unauthorized'})
        action=self.path.removeprefix('/')
        if self.path != '/'+action or action not in FIELDS: return self.reply(404,{'error':'not_found'})
        try:
            lengths=self.headers.get_all('Content-Length',[])
            if len(lengths)!=1 or self.headers.get('Transfer-Encoding'): raise ValueError()
            n=int(lengths[0])
            if not 0<n<=MAX_BODY: raise ValueError()
            raw=self.rfile.read(n)
            if len(raw)!=n: raise ValueError()
            def unique(pairs):
                out={}
                for k,v in pairs:
                    if k in out: raise ValueError()
                    out[k]=v
                return out
            value=json.loads(raw,object_pairs_hook=unique)
            if not isinstance(value,dict) or set(value)!=FIELDS[action] or any(not isinstance(v,str) or len(v)>160 for v in value.values()): raise ValueError()
            self.reply(202,self.server.engine.submit(action,value))
        except BlockingIOError: self.reply(409,{'error':'conflict'})
        except (ValueError,TypeError): self.reply(400,{'error':'invalid_request'})
        except Exception: self.reply(503,{'error':'unavailable'})


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('command',choices=['serve','status','recover'])
    p.add_argument('--state',type=Path,required=True)
    p.add_argument('--source',type=Path,default=Path(__file__).resolve().parents[2])
    p.add_argument('--operation-id'); a=p.parse_args()
    state=private_tree(a.state); shared=state/'updater/shared'; path=shared/'agent.sock'
    token=(state/'config/updater.token').read_text().strip()
    if not re.fullmatch(r'[A-Za-z0-9_-]{32,128}',token): raise ValueError('invalid credential')
    if a.command!='serve':
        import http.client
        class Local(http.client.HTTPConnection):
            def connect(self):
                self.sock=socket.socket(socket.AF_UNIX); self.sock.settimeout(5); self.sock.connect(str(path))
        client=Local('localhost'); payload=None
        if a.command=='recover':
            if not a.operation_id or not re.fullmatch('[a-f0-9]{32}',a.operation_id): p.error('valid operation ID required')
            payload=json.dumps({'operation_id':a.operation_id,'confirmation':a.operation_id})
        client.request('GET' if payload is None else 'POST','/status' if payload is None else '/recover',body=payload,headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'})
        response=client.getresponse(); data=json.loads(response.read(16384)); client.close()
        if response.status not in (200,202): raise RuntimeError('operation rejected')
        print(json.dumps(data)); return
    engine=Engine(state,a.source)
    shared.mkdir(mode=0o755,exist_ok=True)
    if shared.is_symlink() or shared.stat().st_uid != os.getuid(): raise ValueError('unsafe socket directory')
    shared.chmod(0o755)
    if path.exists() or path.is_symlink():
        import stat
        if path.is_symlink() or not stat.S_ISSOCK(path.stat().st_mode): raise ValueError('unsafe socket path')
        path.unlink()  # Engine lock guarantees no other service owns this installation.
    with Server(str(path),Handler) as server:
        server.token=token; server.engine=engine; os.chmod(path,0o666)
        server.serve_forever(poll_interval=.25)


if __name__=='__main__':
    try: main()
    except (Exception,KeyboardInterrupt):
        raise SystemExit('Updater unavailable; inspect installation state locally. No secrets are printed.')
