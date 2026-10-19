"""Seed only the disposable browser-test DB using the real hidden-input CLI."""
import json
import os
import sqlite3
import subprocess
import time
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
from verify_cli import hidden_command

fixtures_path = Path(__file__).parent / 'fixtures.json'
fixture = json.loads(fixtures_path.read_text())['admin']
hidden_command([sys.argv[1], 'init', '--username', fixture['username'], '--db', sys.argv[2], '--key', sys.argv[3], '--jwt-key', sys.argv[4]], fixture['password'], os.environ.copy())

# 三模块用例需要的普通用户（module_user_a / module_user_b）：直接写库，
# 不走 HTTP /auth/register——该接口限制每 IP 每小时 10 次，既有 user-module
# 用例已用满配额，任何额外注册都会挤掉既有用例。密码哈希由 argon2_hash.go
# 生成，格式与服务端 logic.Passwords 完全兼容；其余列走表默认值
# （status=active、must_change_password=0、auth_version=1），与注册路径一致。
go = os.environ.get('FRESHPLATE_E2E_GO', 'go')
users = json.loads(fixtures_path.read_text())['users']
now = int(time.time())
connection = sqlite3.connect(sys.argv[2])
try:
    for key in ('A', 'B'):
        user = users[key]
        password_hash = subprocess.run(
            [go, 'run', '../web/e2e/argon2_hash.go', user['password']],
            cwd=str(ROOT / 'server'), env=os.environ.copy(), check=True,
            capture_output=True, text=True, timeout=120,
        ).stdout.strip()
        with connection:
            cursor = connection.execute(
                'INSERT INTO users(username,password_hash,role,created_at,updated_at) VALUES(?,?,?,?,?)',
                (user['username'], password_hash, 'user', now, now),
            )
            # 注册路径（store/users.go CreateUser）会同步初始化一条
            # user_preferences 行；直接写库必须补上，否则 GET /api/me/preferences
            # 因缺行返回 500 INTERNAL_ERROR，设置页无法打开。
            connection.execute(
                'INSERT INTO user_preferences(user_id,updated_at) VALUES(?,?)',
                (cursor.lastrowid, now),
            )
        print('seeded user', user['username'])
finally:
    connection.close()
