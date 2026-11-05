#!/usr/bin/env python3
"""Claim and configure an empty local Zing installation. Never edits an existing owner."""
import http.cookiejar
import json
import os
from pathlib import Path
import re
import secrets
import time
import urllib.error
import urllib.parse
import urllib.request

ORIGIN = 'http://127.0.0.1:3000'
ACCESS = Path('.lidza/local-access.json')

def main():
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    def request(path, body=None, form=False):
        data = urllib.parse.urlencode(body or {}).encode() if form else (json.dumps(body).encode() if body is not None else None)
        headers = {'Content-Type': 'application/x-www-form-urlencoded' if form else 'application/json', 'Origin': ORIGIN, 'Sec-Fetch-Site': 'same-origin'}
        req = urllib.request.Request(ORIGIN+path, data=data, headers=headers)
        with opener.open(req, timeout=15) as res:
            raw = res.read()
            if res.headers.get_content_type() == 'application/json':
                return json.loads(raw) if raw else None
            return raw.decode()
    state = request('/api/v1/installation')
    if state['ownerClaimed']:
        if ACCESS.exists():
            if state['configured']:
                print('Local operator already exists; preserved existing access and settings.')
                return
            access = json.loads(ACCESS.read_text())
            request('/api/v1/auth/login', {'email': access['email'], 'password': access['password']})
            response = request('/admin/installation/complete', {}, form=True)
            if not request('/api/v1/installation')['configured']:
                raise SystemExit('Setup validation is incomplete. Open /admin/installation.')
            print('Resumed and completed the existing local operator installation.')
            return
        raise SystemExit('An operator already owns this instance. Sign in with that account; nothing was changed.')
    ACCESS.parent.mkdir(parents=True, exist_ok=True)
    if ACCESS.exists():
        access = json.loads(ACCESS.read_text())
    else:
        access = {'url': ORIGIN, 'email': 'operator@zing.local', 'password': secrets.token_urlsafe(24), 'mailbox': 'http://127.0.0.1:8025/'}
        fd = os.open(ACCESS, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        with os.fdopen(fd, 'w') as file:
            json.dump(access, file, indent=2)
    try:
        request('/api/v1/auth/register', {'email': access['email'], 'password': access['password'], 'name': 'Local operator'})
    except urllib.error.HTTPError as error:
        if error.code != 409:
            raise
        request('/api/v1/auth/login', {'email': access['email'], 'password': access['password']})
    token = None
    for _ in range(50):
        with urllib.request.urlopen('http://127.0.0.1:8025/messages', timeout=5) as res:
            inbox = json.load(res)
        for mail in inbox:
            if access['email'] in mail['to'] and '/verify?' in mail['text']:
                match = re.search(r'[?&]token=([A-Za-z0-9_-]+)', mail['text'])
                if match:
                    token = match[1]
                    break
        if token:
            break
        time.sleep(0.1)
    if not token:
        raise SystemExit('Verification email did not reach the local SMTP inbox; installation remains unfinished.')
    request('/api/v1/auth/verify', {'token': token})
    claim = Path('config/owner-claim/token').read_text().strip()
    request('/api/v1/auth/owner/claim', {'token': claim})
    response = request('/admin/installation/complete', {}, form=True)
    if 'settings saved encrypted in PostgreSQL' not in response:
        raise SystemExit('Operator claimed, but setup validation did not complete. Open /admin/installation.')
    if not request('/api/v1/installation')['configured']:
        raise SystemExit('Setup did not persist.')
    print('Local setup complete: private operator account, real local SMTP delivery, and database-backed settings.')
    print('Credentials saved privately in .lidza/local-access.json; no credentials were printed.')

if __name__ == '__main__':
    main()
