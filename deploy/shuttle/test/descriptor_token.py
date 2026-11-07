"""Rotate a local descriptor token; retry safe reads without exposing credentials."""
import base64
import http.client
import json
import os
import signal
import ssl
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        raise RuntimeError('redirect forbidden')


def atomic(path, value):
    temporary = path.with_suffix('.next')
    temporary.write_text(value, encoding='utf-8')
    os.chmod(temporary, 0o600)
    os.replace(temporary, path)


def transient(error):
    if isinstance(error, urllib.error.HTTPError):
        return error.code in (408, 429, 500, 502, 503, 504)
    if isinstance(error, urllib.error.URLError):
        return not isinstance(error.reason, (ssl.SSLError, ssl.CertificateError))
    return isinstance(error, (TimeoutError, ConnectionError, http.client.IncompleteRead))


def refresh(config, state, opener, stop):
    credentials = (config['client_id'] + ':' + config['client_secret']).encode()
    request = urllib.request.Request(config['token_url'],
        urllib.parse.urlencode({'grant_type': 'client_credentials', 'scope': config['scope']}).encode(),
        {'Content-Type': 'application/x-www-form-urlencoded',
         'Authorization': 'Basic ' + base64.b64encode(credentials).decode()})
    issued_at = time.time()
    with opener.open(request, timeout=20) as response:
        reply = json.load(response)
    token = reply['access_token']
    ttl = int(reply['expires_in'])
    if not isinstance(token, str) or not token or ttl < 60 or reply.get('token_type', '').lower() != 'bearer':
        raise ValueError('invalid token reply')
    request = urllib.request.Request('http://loom:8080/v1/capabilities',
        headers={'Authorization': 'Bearer ' + token})
    deadline = time.monotonic() + 15
    while not stop.is_set():
        try:
            with opener.open(request, timeout=20) as response:
                capabilities = json.load(response)
            break
        except urllib.error.HTTPError as error:
            error.close()
            if error.code != 401 or time.monotonic() >= deadline:
                raise
            if stop.wait(0.5):
                return None
    else:
        return None
    expires_at = issued_at + ttl
    if 'native_files' not in capabilities['execution_profiles'] or expires_at - time.time() < 30:
        raise ValueError('token capability or lifetime invalid')
    if stop.is_set():
        return None
    atomic(state / 'actor.token', token)
    atomic(state / 'ready.json', json.dumps({'refreshed_at': time.time(), 'expires_at': expires_at,
        'native_files': True, 'subject': config['subject']}))
    return min(300, ttl // 3)


def rotate(config, state, opener, stop):
    backoff = 1
    while not stop.is_set():
        try:
            delay = refresh(config, state, opener, stop)
        except (urllib.error.URLError, TimeoutError, ConnectionError, http.client.IncompleteRead) as error:
            if isinstance(error, urllib.error.HTTPError):
                error.close()
            if not transient(error):
                raise
            # URLs, bodies and exception strings can contain authentication material.
            print('descriptor token temporarily unavailable; retry in ' + str(backoff) + 's', flush=True)
            if stop.wait(backoff):
                return
            backoff = min(30, backoff * 2)
            continue
        if delay is None:
            return
        backoff = 1
        print('descriptor token refreshed; native_files authenticated', flush=True)
        if stop.wait(delay):
            return


def make_opener(cafile):
    # Keep explicit CA verification compatible with the local IAM certificate chain.
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.load_verify_locations(cafile=cafile)
    return urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect(),
        urllib.request.HTTPSHandler(context=context))


def main():
    stop = threading.Event()
    for name in (signal.SIGTERM, signal.SIGINT):
        signal.signal(name, lambda *_: stop.set())
    config = json.loads(Path('/secrets/machine.json').read_text(encoding='utf-8'))
    rotate(config, Path('/state'), make_opener('/tls/ca-bundle.pem'), stop)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print('descriptor token service stopped: ' + type(error).__name__, flush=True)
        raise SystemExit(1)
