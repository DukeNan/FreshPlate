#!/usr/bin/env python3
"""Exercise hidden CLI input, process exclusion and recovery on disposable data."""
import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import pty
import select
import signal
import socket
import sqlite3
import subprocess
import tempfile
import termios
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]


def hidden_command(args, password, env, expected=0):
    master, slave = pty.openpty()
    process = subprocess.Popen(args, stdin=slave, stdout=slave, stderr=slave, env=env)
    os.close(slave)
    output = b''
    try:
        for prompt in (b'Password: ', b'Confirm password: '):
            deadline = time.monotonic() + 20
            while prompt not in output:
                assert time.monotonic() < deadline, 'hidden password prompt timed out'
                if select.select([master], [], [], 0.2)[0]:
                    chunk = os.read(master, 4096)
                    if not chunk: break
                    output += chunk
            # Synchronize on the terminal's actual hidden-input mode, not prompt timing.
            deadline = time.monotonic() + 20
            while termios.tcgetattr(master)[3] & termios.ECHO:
                assert process.poll() is None and time.monotonic() < deadline, 'CLI did not disable echo'
                time.sleep(0.002)
            os.write(master, password.encode() + b'\n')
        deadline = time.monotonic() + 20
        while process.poll() is None:
            assert time.monotonic() < deadline, 'CLI did not finish'
            if select.select([master], [], [], 0.2)[0]:
                try:
                    output += os.read(master, 4096)
                except OSError:
                    break
        process.wait(timeout=10)
        while select.select([master], [], [], 0)[0]:
            try:
                chunk = os.read(master, 4096)
                if not chunk:
                    break
                output += chunk
            except OSError:
                break
        assert process.returncode == expected, 'unexpected CLI exit code'
        assert password.encode() not in output, 'password echoed to terminal'
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        os.close(master)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--go', default='go')
    args = parser.parse_args()
    env = os.environ.copy()
    env.pop('GOROOT', None)
    env['GOTOOLCHAIN'] = 'go1.26.4'
    env.setdefault('GOPROXY', 'https://goproxy.cn')
    with tempfile.TemporaryDirectory(prefix='freshplate-cli-') as tmp:
        tmp = Path(tmp)
        admin, server, db, key, jwt_key = (tmp / p for p in ('admin', 'server', 'db', 'key', 'jwt.key'))
        for cmd, destination in (('admin', admin), ('server', server)):
            subprocess.run([args.go, 'build', '-o', str(destination), f'./cmd/{cmd}'], cwd=ROOT/'server', env=env, check=True)
        common = ['--db', str(db), '--key', str(key), '--jwt-key', str(jwt_key)]
        first, temporary, final = 'Initial-fixture-2026', 'Temporary-fixture-2026', 'Final-fixture-2026'
        hidden_command([str(admin), 'init', '--username', ' Admin_One ', *common], first, env)
        hidden_command([str(admin), 'init', '--username', 'other_admin', *common], first, env, expected=1)
        with sqlite3.connect(db) as connection:
            assert connection.execute('select username,role from users').fetchall() == [('admin_one', 'admin')]
            assert connection.execute('select count(*) from user_preferences').fetchone()[0] == 1
            assert connection.execute("select actor_source,action from audit_logs").fetchall() == [('cli', 'admin_init')]
        print('CLI-001: hidden input, normalization, atomic defaults/audit and repeated initialization passed')
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        origin = f'http://127.0.0.1:{port}'
        jar = http.cookiejar.CookieJar()
        client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))

        access_token = None

        def request(method, path, body=None, csrf=None, user=None):
            headers = {'Origin': origin}
            if access_token: headers['Authorization'] = 'Bearer ' + access_token
            if csrf: headers['X-CSRF-Token'] = csrf
            if user: headers['X-Expected-User-ID'] = str(user)
            if body is not None: headers['Content-Type'] = 'application/json'
            req = urllib.request.Request(origin+'/api'+path, data=None if body is None else json.dumps(body).encode(), headers=headers, method=method)
            try: response = client.open(req, timeout=10)
            except urllib.error.HTTPError as error: response = error
            with response:
                payload = response.read()
                return response.status, json.loads(payload) if payload else None

        def csrf():
            status, payload = request('GET', '/auth/csrf')
            assert status == 200
            return payload['data']['token']

        server_args = [str(server), *common, '--addr', f'127.0.0.1:{port}', '--origin', origin, '--web', str(ROOT/'web/dist')]
        with open(tmp/'service.log', 'wb') as output:
            def start():
                process = subprocess.Popen(server_args, stdout=output, stderr=output, env=env)
                deadline = time.monotonic()+15
                while time.monotonic() < deadline:
                    assert process.poll() is None, 'server exited during startup'
                    try:
                        if request('GET', '/auth/csrf')[0] == 200: return process
                    except (OSError, urllib.error.URLError): pass
                    time.sleep(0.05)
                process.kill(); process.wait()
                raise AssertionError('server startup timed out')

            def stop(process):
                if process.poll() is None: process.send_signal(signal.SIGTERM)
                process.wait(timeout=15)

            process = start()
            try:
                status, payload = request('POST', '/auth/login', {'username':'admin_one','password':first}, csrf())
                assert status == 200
                access_token = payload['data']['access_token']
                user = payload['data']['user']['id']
                for duplicate in (server_args, [str(admin), 'recover-password', '--username', 'admin_one', *common]):
                    result = subprocess.run(duplicate, stdin=subprocess.DEVNULL, capture_output=True, env=env, timeout=15)
                    assert result.returncode != 0 and b'database in use' in result.stderr
                status, _ = request('GET', '/me')
                assert status == 200
                for file in (db, key, jwt_key, Path(str(db)+'-wal'), Path(str(db)+'-shm')):
                    assert file.stat().st_mode & 0o777 == 0o600, 'database/key permissions too broad'
                print('DEP-001/SEC-004: real server/CLI lock exclusion, DB/key/WAL/SHM permissions passed')
            finally: stop(process)
            process = start()
            try:
                assert request('GET', '/me')[0] == 200, 'valid access JWT/session lost on restart'
                status, payload = request('POST', '/auth/refresh', csrf=csrf())
                assert status == 200
                access_token = payload['data']['access_token']
                print('SES-002: valid browser session survived a real service restart')
            finally: stop(process)
            hidden_command([str(admin), 'recover-password', '--username', ' ADMIN_ONE ', *common], temporary, env)
            process = start()
            try:
                assert request('GET', '/me')[0] == 401, 'old session survived recovery'
                assert request('POST', '/auth/login', {'username':'admin_one','password':first}, csrf())[0] == 401
                status, payload = request('POST', '/auth/login', {'username':'admin_one','password':temporary}, csrf())
                assert status == 200 and payload['data']['user']['must_change_password']
                access_token = payload['data']['access_token']
                user = payload['data']['user']['id']
                token = payload['data']['csrf_token']
                assert request('GET', '/me/preferences')[0] == 403
                assert request('PUT', '/me/password', {'current_password':temporary,'new_password':final}, token, user)[0] == 204
                assert request('POST', '/auth/login', {'username':'admin_one','password':temporary}, csrf())[0] == 401
                status, payload = request('POST', '/auth/login', {'username':'admin_one','password':final}, csrf())
                assert status == 200 and not payload['data']['user']['must_change_password']
                print('CLI-002/ADM-002/SES-005: recovery revoked sessions, forced change and new login passed')
            finally: stop(process)
        jwt_key.unlink()
        result = subprocess.run(server_args, capture_output=True, env=env, timeout=15)
        assert result.returncode != 0 and b'JWT key missing' in result.stderr and not jwt_key.exists()
        print('JWT-KEY: missing existing-deployment JWT key rejected without regeneration')
        jwt_key.write_bytes(os.urandom(32))
        jwt_key.chmod(0o600)
        key.unlink()
        result = subprocess.run(server_args, capture_output=True, env=env, timeout=15)
        assert result.returncode != 0 and b'CSRF key missing' in result.stderr and not key.exists()
        print('SEC-004: missing existing-deployment key rejected without regeneration')
        print('PASS: disposable CLI/process verification complete; no credentials or tokens retained')


if __name__ == '__main__':
    main()
