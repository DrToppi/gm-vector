"""Host updater IPC regressions. Synthetic engine and local Unix socket only."""
import http.client
import json
from pathlib import Path
import socket
import sys
import tempfile
import threading
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
import update_service as service


class Local(http.client.HTTPConnection):
    def __init__(self, path):
        self.path = str(path)
        super().__init__('localhost', timeout=3)

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX)
        self.sock.settimeout(3)
        self.sock.connect(self.path)


class FakeEngine:
    def __init__(self):
        self.calls = []
        self.lock = threading.Lock()

    def status(self):
        return {'state': 'idle', 'phase': 'complete'}

    def submit(self, action, data):
        if not self.lock.acquire(False):
            raise BlockingIOError()
        try:
            self.calls.append((action, data))
            return {'state': 'checking', 'phase': 'queued'}
        finally:
            self.lock.release()


class UpdaterHTTPTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.path = Path(self.directory.name) / 'agent.sock'
        self.engine = FakeEngine()
        self.server = service.Server(str(self.path), service.Handler)
        self.server.token = 'synthetic-updater-proof-not-a-real-secret'
        self.server.engine = self.engine
        self.thread = threading.Thread(target=self.server.serve_forever, kwargs={'poll_interval': .01}, daemon=True)
        self.thread.start()

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(2)
        self.directory.cleanup()

    def request(self, method, path, raw=None, headers=None):
        client = Local(self.path)
        try:
            value = {'Authorization': 'Bearer ' + self.server.token}
            value.update(headers or {})
            try:
                client.request(method, path, body=raw, headers=value)
            except BrokenPipeError:
                # Early header rejection may close the write side before the body
                # is sent. The actual HTTP error response must still be asserted.
                pass
            response = client.getresponse()
            return response.status, response.read()
        finally:
            client.close()

    def test_status_and_check_are_the_only_requested_operations(self):
        code, raw = self.request('GET', '/status')
        self.assertEqual(code, 200)
        self.assertEqual(json.loads(raw)['state'], 'idle')
        code, _ = self.request('POST', '/check', b'{"ref":"v0.2.0"}')
        self.assertEqual(code, 202)
        self.assertEqual(self.engine.calls, [('check', {'ref': 'v0.2.0'})])

    def test_bearer_is_required_and_not_returned(self):
        for auth in ('', 'Bearer wrong', 'Bearer é', self.server.token):
            code, raw = self.request('GET', '/status', headers={'Authorization': auth})
            self.assertEqual(code, 401)
            self.assertNotIn(self.server.token.encode(), raw)
        self.assertEqual(self.engine.calls, [])

    def test_duplicate_authorization_and_content_length_are_rejected(self):
        for name, value, expected in [('Authorization', 'Bearer ' + self.server.token, 401),
                                      ('Content-Length', '12', 400)]:
            with self.subTest(name=name):
                client = Local(self.path)
                try:
                    client.putrequest('POST', '/check')
                    client.putheader('Authorization', 'Bearer ' + self.server.token)
                    client.putheader('Content-Length', '12')
                    client.putheader(name, value)
                    try:
                        client.endheaders(b'{"ref":"a"}')
                    except BrokenPipeError:
                        # Header rejection may precede the separate body write.
                        # Still require the server's precise HTTP error below.
                        pass
                    response = client.getresponse()
                    self.assertEqual(response.status, expected)
                    response.read()
                finally:
                    client.close()
        self.assertEqual(self.engine.calls, [])

    def test_duplicate_keys_types_missing_and_unknown_fields(self):
        invalid = [b'{"ref":"a","ref":"b"}', b'{"ref":1}', b'{"ref":null}',
                   b'[]', b'{}', b'{"ref":"main","repository":"https://evil.invalid"}',
                   b'{"ref":"' + b'a' * 161 + b'"}', b'not-json']
        for raw in invalid:
            with self.subTest(raw=raw[:40]):
                code, _ = self.request('POST', '/check', raw)
                self.assertEqual(code, 400)
        self.assertEqual(self.engine.calls, [])

    def test_size_and_transfer_encoding_rejected_before_engine(self):
        code, _ = self.request('POST', '/check', b'a' * (service.MAX_BODY + 1))
        self.assertEqual(code, 400)
        code, _ = self.request('POST', '/check', b'{}', {'Transfer-Encoding': 'chunked'})
        self.assertEqual(code, 400)
        self.assertEqual(self.engine.calls, [])

    def test_exact_routes_and_unsupported_methods_do_not_submit(self):
        for path in ('/check?ref=main', '//check', '/check/', '/apply/../check', '/unknown'):
            code, _ = self.request('POST', path, b'{"ref":"main"}')
            self.assertEqual(code, 404)
        code, _ = self.request('PUT', '/apply', b'{"commit":"x"}')
        self.assertIn(code, (405, 501))
        self.assertEqual(self.engine.calls, [])

    def test_conflict_and_engine_failure_are_redacted(self):
        self.engine.lock.acquire()
        try:
            code, raw = self.request('POST', '/check', b'{"ref":"main"}')
            self.assertEqual((code, json.loads(raw)), (409, {'error': 'conflict'}))
        finally:
            self.engine.lock.release()
        def fail(*args):
            raise RuntimeError('SYNTHETIC_PRIVATE_DIAGNOSTIC')
        self.engine.submit = fail
        code, raw = self.request('POST', '/check', b'{"ref":"main"}')
        self.assertEqual(code, 503)
        self.assertNotIn(b'SYNTHETIC_PRIVATE_DIAGNOSTIC', raw)


if __name__ == '__main__':
    unittest.main()
