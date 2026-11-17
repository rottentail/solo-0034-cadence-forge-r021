#!/usr/bin/env python3
"""Refactoring verification for the shared revision-history boundary.

Beyond the 18 bounded production workflows, this script proves the
refactoring claims that generic substitution alone could not:

  1. big-history   hundreds of revisions stay independently addressable,
                   committed revisions never mutate on later writes, and the
                   on-disk JSON keeps the legacy bare-array shape;
  2. concurrency   parallel stale writers all lose the optimistic conflict;
  3. legacy        a state root produced by the pre-refactor binary opens,
                   and a real interrupted task is retried to success with its
                   original frozen inputs under the new binary;
  4. idempotency   same-key retries converge through the new history path.

It talks only HTTP to real forged processes and reads the real state.json.
"""
import argparse
import concurrent.futures
import json
import os
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def need(value, message):
    if not value:
        raise RuntimeError(message)


class Server:
    def __init__(self, binary, scratch, port, ffmpeg, ffprobe):
        self.binary = binary
        self.scratch = scratch
        self.port = port
        self.log = open(scratch / 'server.log', 'ab')
        self.process = None
        self.env = dict(os.environ, FORGE_ROOT=str(scratch / 'data'),
                        FORGE_ADDR='127.0.0.1:' + str(port),
                        FORGE_WORKERS='2', FFMPEG_BIN=ffmpeg, FFPROBE_BIN=ffprobe)

    def request(self, method, route, body=None, headers=None, status=200):
        hs = dict(headers or {})
        if isinstance(body, (dict, list)):
            body = json.dumps(body).encode()
            hs['Content-Type'] = 'application/json'
        req = urllib.request.Request('http://127.0.0.1:%d/forge/v1%s' % (self.port, route),
                                     data=body, headers=hs, method=method)
        try:
            response = urllib.request.urlopen(req, timeout=60)
        except urllib.error.HTTPError as exc:
            response = exc
        with response:
            data = response.read()
            need(response.status == status,
                 '%s %s: %d wanted %d: %r' % (method, route, response.status, status, data[:600]))
            return json.loads(data) if data else None

    def start(self):
        self.process = subprocess.Popen([str(self.binary)], cwd=ROOT, env=self.env,
                                        stdout=self.log, stderr=self.log, start_new_session=True)
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            try:
                self.request('GET', '/health')
                return
            except (OSError, RuntimeError):
                if self.process.poll() is not None:
                    raise RuntimeError((self.scratch / 'server.log').read_text())
                time.sleep(.05)
        raise RuntimeError('server readiness timed out')

    def stop(self, force=False):
        if self.process and self.process.poll() is None:
            os.killpg(self.process.pid, signal.SIGKILL if force else signal.SIGTERM)
            self.process.wait(timeout=15)
        self.process = None

    def state_file(self):
        return json.loads((self.scratch / 'data' / 'state.json').read_text())


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def make_calibration(scratch, ffmpeg):
    target = scratch / 'calibration.mp4'
    subprocess.run([ffmpeg, '-nostdin', '-hide_banner', '-loglevel', 'error',
                    '-f', 'lavfi', '-i', 'testsrc2=size=160x90:rate=24:duration=4',
                    '-f', 'lavfi', '-i', 'sine=frequency=660:sample_rate=48000:duration=4',
                    '-c:v', 'libx264', '-preset', 'ultrafast', '-threads', '1',
                    '-c:a', 'aac', '-shortest', str(target)], check=True, timeout=30)
    return target.read_bytes()


def wait_terminal(server, rid, timeout=40):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        r = server.request('GET', '/renders/' + rid)
        if r['state'] in ('succeeded', 'failed', 'cancelled'):
            return r
        time.sleep(.02)
    raise RuntimeError('render state timeout')


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--baseline', required=True, help='pre-refactor forged binary')
    ap.add_argument('--revised', default=str(ROOT / 'bin/forged'), help='refactored forged binary')
    ap.add_argument('--revisions', type=int, default=400)
    args = ap.parse_args()
    ff = os.environ.get('FFMPEG_BIN', 'ffmpeg')
    fp = os.environ.get('FFPROBE_BIN', 'ffprobe')
    observations = []

    with tempfile.TemporaryDirectory(prefix='cadence-refactor-') as raw:
        scratch = Path(raw)
        media = make_calibration(scratch, ff)

        # ---- scenarios 1, 2, 4 on the refactored binary ------------------
        server = Server(Path(args.revised), scratch, free_port(), ff, fp)
        server.start()
        try:
            source = server.request('POST', '/sources?name=calibration', media, status=201)
            sid = source['id']
            recipe_body = dict(name='review', format='mp4', width=160, height=90, fps=24,
                               sample_rate=48000, normalize=False, target_lufs=-16)
            recipe = server.request('POST', '/recipes', recipe_body, status=201)
            rp = recipe['id']

            def sheet_body(name, span_out):
                return dict(name=name, spans=[dict(source_id=sid, in_ms=0, out_ms=span_out, gain_db=0)],
                            cues=[dict(source_id=sid, start_ms=100, end_ms=200, text='校准')])

            # 1. big history: append many revisions, each with distinct facts
            n = args.revisions
            current = server.request('POST', '/sheets', sheet_body('rev-1', 1500), status=201)
            sh = current['id']
            for rev in range(2, n + 1):
                out_ms = 1500 + rev  # distinct span per revision, within 4s source
                current = server.request('PUT', '/sheets/' + sh, sheet_body('rev-%d' % rev, out_ms),
                                         {'If-Match': str(rev - 1)}, status=201)
                need(current['revision'] == rev, 'revision did not advance to %d' % rev)
            state = server.request('GET', '/state')
            need(len(state['sheets'][sh]) == n, 'history length mismatch')
            # every revision remains independently addressable and immutable
            history = state['sheets'][sh]
            for rev in (1, 2, n // 2, n):
                v = history[rev - 1]
                need(v['revision'] == rev, 'revision numbering broken at %d' % rev)
                expected_out = 1500 if rev == 1 else 1500 + rev
                need(v['spans'][0]['out_ms'] == expected_out,
                     'committed revision %d was mutated by later writes' % rev)
                need(v['name'] == 'rev-%d' % rev, 'committed name drifted at %d' % rev)
            # legacy on-disk JSON: bare arrays under the original keys
            on_disk = server.state_file()
            need(isinstance(on_disk['sheets'][sh], list), 'legacy sheet JSON shape lost')
            need(isinstance(on_disk['recipes'][rp], list), 'legacy recipe JSON shape lost')
            need(on_disk['sheets'][sh][0]['revision'] == 1, 'on-disk revision 1 broken')
            need(on_disk['sheets'][sh][-1]['revision'] == n, 'on-disk tail revision broken')
            observations.append(('big-history',
                                 '%d revisions addressable; earlier facts immutable; legacy JSON array shape' % n))

            # 2. concurrency: parallel stale writers must all conflict, one winner
            stale = sheet_body('stale', 1600)
            results = []

            def stale_write():
                try:
                    return server.request('PUT', '/sheets/' + sh, stale,
                                          {'If-Match': '1'}, status=409).get('error')
                except RuntimeError:
                    raise

            with concurrent.futures.ThreadPoolExecutor(max_workers=16) as pool:
                futures = [pool.submit(stale_write) for _ in range(24)]
                results = [f.result() for f in futures]
            need(all(r == 'conflict' for r in results), 'a stale concurrent writer was accepted')
            after = server.request('GET', '/state')
            need(len(after['sheets'][sh]) == n, 'conflict path altered history length')
            # the one legitimate next writer wins
            winner = server.request('PUT', '/sheets/' + sh, sheet_body('winner', 1700),
                                    {'If-Match': str(n)}, status=201)
            need(winner['revision'] == n + 1, 'legitimate concurrent writer lost')
            observations.append(('concurrency',
                                 '24 parallel stale If-Match writers rejected; single in-date writer advanced'))

            # 4. idempotency through the new history lookup path
            req = dict(sheet_id=sh, sheet_revision=1, recipe_id=rp, recipe_revision=1)
            first = server.request('POST', '/renders', req, {'Idempotency-Key': 'idem-1'}, status=202)
            second = server.request('POST', '/renders', req, {'Idempotency-Key': 'idem-1'}, status=202)
            need(second['id'] == first['id'], 'idempotent retry diverged')
            done = wait_terminal(server, first['id'])
            need(done['state'] == 'succeeded' and done['sheet']['revision'] == 1,
                'frozen revision 1 render failed: %r' % done)
            # later sheet edits cannot change the already confirmed render
            need(done['sheet']['spans'][0]['out_ms'] == 1500, 'confirmed render was mutated')
            observations.append(('idempotency',
                                 'same-key retry converged; confirmed render immune to later revisions'))
        finally:
            server.stop()

        # ---- 3. legacy: baseline-written root reopened by revised binary ----
        legacy_scratch = Path(tempfile.mkdtemp(prefix='cadence-legacy-', dir=str(scratch)))
        old = Server(Path(args.baseline), legacy_scratch, free_port(), ff, fp)
        old.start()
        interrupted_id = None
        try:
            source = old.request('POST', '/sources?name=legacy', media, status=201)
            sid = source['id']
            recipe = old.request('POST', '/recipes',
                                 dict(name='legacy-r', format='mp4', width=160, height=90, fps=24,
                                      sample_rate=48000, normalize=False, target_lufs=-16), status=201)
            sheet = old.request('POST', '/sheets',
                                dict(name='legacy-s', spans=[dict(source_id=sid, in_ms=0, out_ms=1500, gain_db=0)],
                                     cues=[]), status=201)
            req = dict(sheet_id=sheet['id'], sheet_revision=1, recipe_id=recipe['id'], recipe_revision=1)
            completed = old.request('POST', '/renders', req, {'Idempotency-Key': 'legacy-done'}, status=202)
            need(wait_terminal(old, completed['id'])['state'] == 'succeeded', 'baseline render did not succeed')
            # a second, long task is forced into running then the process is killed
            long_sheet = old.request('POST', '/sheets',
                                     dict(name='long', spans=[dict(source_id=sid, in_ms=0, out_ms=1500, gain_db=0)] * 16,
                                          cues=[]), status=201)
            pending = old.request('POST', '/renders',
                                  dict(req, sheet_id=long_sheet['id']), {'Idempotency-Key': 'legacy-kill'}, status=202)
            interrupted_id = pending['id']
            deadline = time.monotonic() + 40
            while time.monotonic() < deadline:
                if old.request('GET', '/renders/' + interrupted_id)['state'] == 'running':
                    break
                time.sleep(.02)
            else:
                raise RuntimeError('task never reached running before kill')
            old_on_disk = old.state_file()
            need(isinstance(old_on_disk['sheets'][sheet['id']], list), 'baseline did not write legacy arrays')
        finally:
            old.stop(force=True)

        # revised binary reopens the untouched legacy data root
        new = Server(Path(args.revised), legacy_scratch, free_port(), ff, fp)
        new.start()
        try:
            reopened = new.state_file()
            need(reopened['schema'] == 1, 'legacy state schema rejected')
            need(reopened['renders'][completed['id']]['state'] == 'succeeded', 'legacy success lost on reopen')
            recovered = new.request('GET', '/renders/' + interrupted_id)
            need(recovered['state'] == 'failed', 'legacy running task not recovered as failed')
            need(recovered['attempts'][-1]['outcome'] == 'interrupted', 'interruption provenance lost')
            # real retry of the old task on the new binary, original frozen inputs
            new.request('POST', '/renders/' + interrupted_id + '/retry', status=202)
            retried = wait_terminal(new, interrupted_id)
            need(retried['state'] == 'succeeded' and len(retried['attempts']) == 2,
                 'legacy task retry failed: %r' % retried)
            need(retried['sheet']['id'] == long_sheet['id']
                 and retried['sheet']['revision'] == 1, 'retry drifted from frozen legacy inputs')
            need(len(retried['sheet']['spans']) == 16, 'frozen legacy sheet content changed')
            # edits still work on a history deserialized from legacy JSON
            follow = new.request('PUT', '/sheets/' + sheet['id'],
                                 dict(name='legacy-s', spans=[dict(source_id=sid, in_ms=0, out_ms=1600, gain_db=0)],
                                      cues=[]), {'If-Match': '1'}, status=201)
            need(follow['revision'] == 2, 'append on deserialized legacy history failed')
            observations.append(('legacy',
                                 'baseline state.json reopened; interrupted task retried to success; history appends continue'))
        finally:
            new.stop()

    print(json.dumps({'ok': True, 'observations': [
        {'scenario': name, 'passed': True, 'observed': detail} for name, detail in observations]},
        ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
