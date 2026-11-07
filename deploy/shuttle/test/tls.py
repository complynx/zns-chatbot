"""Issue test certificates from one reusable local root. Never install trust automatically."""
import argparse
import subprocess
from pathlib import Path

STATE = Path(__file__).resolve().parents[3] / '.cache/shuttle-live-test'
IMAGE = 'alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6'

# Fixed reserved local names. Wildcard covers one subdomain level without CA re-import.
ISSUE = r'''
apk add --no-cache openssl >/dev/null
umask 077
if [ ! -e /certs/root-ca.crt ] && [ ! -e /certs/root-ca.key ]; then
  openssl req -x509 -newkey rsa:3072 -sha256 -nodes -days 1825 \
    -keyout /certs/root-ca.key -out /certs/root-ca.crt \
    -subj "/CN=Zinusya LOCAL TEST Root CA" \
    -addext "basicConstraints=critical,CA:TRUE,pathlen:0" \
    -addext "keyUsage=critical,keyCertSign,cRLSign" >/dev/null 2>&1
fi
# Refuse partial/mismatched/expiring roots; rotating a trusted root needs an explicit decision.
test -s /certs/root-ca.crt && test -s /certs/root-ca.key
openssl verify -CAfile /certs/root-ca.crt /certs/root-ca.crt >/dev/null
openssl x509 -checkend 7776000 -noout -in /certs/root-ca.crt >/dev/null
openssl x509 -pubkey -noout -in /certs/root-ca.crt > /certs/root-public.cert.tmp
openssl pkey -pubout -in /certs/root-ca.key > /certs/root-public.key.tmp
cmp /certs/root-public.cert.tmp /certs/root-public.key.tmp
openssl x509 -outform DER -in /certs/root-ca.crt -out /certs/root-ca.cer
openssl req -new -newkey rsa:2048 -nodes \
  -keyout /certs/tls.next.key -out /certs/tls.next.csr \
  -subj /CN=host.docker.internal >/dev/null 2>&1
cat > /certs/leaf.ext.tmp <<'EXT'
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=DNS:host.docker.internal,DNS:zinusya.localhost,DNS:*.zinusya.localhost,DNS:localhost,IP:127.0.0.1
EXT
openssl x509 -req -in /certs/tls.next.csr -CA /certs/root-ca.crt \
  -CAkey /certs/root-ca.key -set_serial "0x$(openssl rand -hex 16)" \
  -days 90 -sha256 -extfile /certs/leaf.ext.tmp -out /certs/tls.next.crt >/dev/null 2>&1
openssl verify -CAfile /certs/root-ca.crt /certs/tls.next.crt >/dev/null
if [ -e /certs/tls.crt ]; then
  cp /certs/tls.crt /certs/tls.previous.crt
  cp /certs/tls.key /certs/tls.previous.key
fi
mv /certs/tls.next.crt /certs/tls.crt
mv /certs/tls.next.key /certs/tls.key
cat /etc/ssl/certs/ca-certificates.crt /certs/root-ca.crt > /certs/ca-bundle.pem
# One transitional old leaf remains trusted inside this disposable stand for rollback.
if [ -e /certs/tls.previous.crt ]; then cat /certs/tls.previous.crt >> /certs/ca-bundle.pem; fi
chown 1000:1000 /certs/tls.key
chmod 600 /certs/tls.key /certs/root-ca.key
chmod 644 /certs/root-ca.crt /certs/root-ca.cer /certs/tls.crt /certs/ca-bundle.pem
openssl x509 -in /certs/root-ca.crt -noout -subject -fingerprint -sha256
'''


def issue(replace=False):
    STATE.mkdir(parents=True, exist_ok=True)
    if (STATE / 'tls.crt').exists() and not replace:
        return
    subprocess.run(['docker', 'run', '--rm', '--mount',
                    'type=bind,source=' + str(STATE.resolve()) + ',target=/certs',
                    IMAGE, 'sh', '-ceu', ISSUE], check=True)
    print('Import only the public root-ca.cer into browser trust; keep root-ca.key private.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--replace-leaf', action='store_true', help='Explicitly renew the leaf using the same root')
    args = parser.parse_args()
    issue(args.replace_leaf)
