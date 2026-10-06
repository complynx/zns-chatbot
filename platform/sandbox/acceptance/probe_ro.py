"""Read the exact clean-C input bytes as the app UID and test RO opens."""
import argparse
import errno
import hashlib
import json
import os
from urllib.parse import parse_qs, urlsplit


BINARY = '488a8accc0f4072236a6b779837a28fa6312bf0769573023271ad85d767e1261'


def read_small(path):
    descriptor = os.open(path,os.O_RDONLY|os.O_NOFOLLOW)
    with os.fdopen(descriptor,'rb') as stream:
        raw = stream.read(1048577)
    if len(raw)>1048576:
        raise ValueError('input exceeds original 1MiB bound')
    return raw


def readonly_open(path):
    # Opening without O_TRUNC never writes or changes the protected input bytes.
    try:
        descriptor = os.open(path,os.O_RDWR|os.O_NOFOLLOW)
    except OSError as error:
        if error.errno != errno.EROFS:
            raise ValueError('native RO open must report EROFS') from error
        return
    os.close(descriptor)
    raise ValueError('protected source unexpectedly opened writable')


def environment(raw):
    result = {}
    for line in raw.decode('ascii').splitlines():
        key,separator,value = line.partition('=')
        if not separator or not key or key in result:
            raise ValueError('exact generated environment records')
        result[key] = value
    return result


def probe(config_sha, app_sha, owner_sha):
    if os.getuid()!=10001 or os.geteuid()!=10001:
        raise ValueError('actual app/owner UID')
    files = [('/etc/zns/runtime.yaml',config_sha),
             ('/private/app.env',app_sha),('/private/owner.env',owner_sha)]
    records = {}
    for path,pin in files:
        raw = read_small(path)
        if hashlib.sha256(raw).hexdigest()!=pin:
            raise ValueError('exact current protected input bytes')
        readonly_open(path)
        records[path] = raw
    app,owner = environment(records['/private/app.env']),environment(records['/private/owner.env'])
    for values in (app,owner):
        url = urlsplit(values['ZNS_DATABASE__URL'])
        if (url.scheme!='postgres' or url.username!='zns_app' or not url.password
                or url.hostname!='postgres' or url.port!=5432
                or url.path!='/synthetic_qa_zns_registration_fixture'
                or parse_qs(url.query)!= {'sslmode':['disable']}):
            raise ValueError('actual narrow app/owner database configuration')
    for key in ('ZNS_DATABASE__URL','ZNS_AUTH__SIGNING_KEY','ZNS_TELEGRAM__TOKEN'):
        if app[key]!=owner[key]:
            raise ValueError('actual app/owner shared configuration')
    digest = hashlib.sha256()
    count = 0
    descriptor = os.open('/current/zns',os.O_RDONLY|os.O_NOFOLLOW)
    with os.fdopen(descriptor,'rb') as stream:
        for part in iter(lambda:stream.read(1048576),b''):
            count += len(part)
            if count > 64553589:
                raise ValueError('original current artifact byte bound')
            digest.update(part)
    if count!=64553589 or digest.hexdigest()!=BINARY:
        raise ValueError('current source-built product binary')
    readonly_open('/current/zns')
    clock_raw = read_small('/run/registration-clock/state.json')
    readonly_open('/run/registration-clock/state.json')
    import bootstrap
    class Contract:
        Failure = ValueError
        @staticmethod
        def same(left,right):
            return json.dumps(left,sort_keys=True)==json.dumps(right,sort_keys=True)
    bootstrap.clock_readback(Contract,clock_raw)
    print(json.dumps({'uid':os.getuid(),'input_count':5,'native_ro_open_count':5,
                      'binary_sha256':digest.hexdigest(),
                      'clock_sha256':hashlib.sha256(clock_raw).hexdigest()}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--config-sha256',required=True)
    parser.add_argument('--app-sha256',required=True)
    parser.add_argument('--owner-sha256',required=True)
    args = parser.parse_args()
    probe(args.config_sha256,args.app_sha256,args.owner_sha256)
