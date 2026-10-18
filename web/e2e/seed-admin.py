"""Seed only the disposable browser-test DB using the real hidden-input CLI."""
import json
import os
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
from verify_cli import hidden_command

fixture = json.loads((Path(__file__).parent / 'fixtures.json').read_text())['admin']
hidden_command([sys.argv[1], 'init', '--username', fixture['username'], '--db', sys.argv[2], '--key', sys.argv[3], '--jwt-key', sys.argv[4]], fixture['password'], os.environ.copy())
