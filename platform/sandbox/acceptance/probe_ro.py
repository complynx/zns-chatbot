"""Read the exact clean-C input bytes as the app UID and test RO opens."""
import argparse
import errno
import hashlib
import json
import os
import stat
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
    private_dac = path in {'/private/app.env','/private/owner.env'}
    if private_dac:
        if (os.getuid()!=10001 or os.geteuid()!=10001 or os.getgid()!=10001
                or os.getegid()!=10001 or not set(os.getgroups()).issubset({10001,10002})):
            raise ValueError('actual private app UID required for DAC precedence')
        descriptor = os.open(path,os.O_RDONLY|os.O_NOFOLLOW)
        try:
            observed = os.fstat(descriptor)
            mount = os.fstatvfs(descriptor)
        finally:
            os.close(descriptor)
        if (not stat.S_ISREG(observed.st_mode) or observed.st_uid!=0
                or observed.st_gid!=10001 or stat.S_IMODE(observed.st_mode)!=0o640
                or not mount.f_flag & os.ST_RDONLY):
            raise ValueError('exact private DAC file on read-only mount required')
    try:
        descriptor = os.open(path,os.O_RDWR|os.O_NOFOLLOW)
    except OSError as error:
        if error.errno != errno.EROFS and not (private_dac and error.errno==errno.EACCES):
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


def private_access(directory='/private'):
    if (os.getuid()!=10001 or os.geteuid()!=10001 or os.getgid()!=10001
            or os.getegid()!=10001 or not set(os.getgroups()).issubset({10001,10002})):
        raise ValueError('actual app/owner UID')
    observed_directory = os.stat(directory,follow_symlinks=False)
    if (not stat.S_ISDIR(observed_directory.st_mode) or observed_directory.st_uid!=0
            or observed_directory.st_gid!=10001 or stat.S_IMODE(observed_directory.st_mode)!=0o750):
        raise ValueError('actual private native directory boundary')
    denied = 0
    names = {role+'.password' for role in ('postgres','app','meter','inventory','fake','operator')}
    names.update(role+'.env' for role in ('app','owner','fake','operator','media','sticker','roles','inventory'))
    if set(os.listdir(directory))!=names:
        raise ValueError('complete private native file inventory')
    for name in sorted(names):
        path = directory+'/'+name
        observed = os.stat(path,follow_symlinks=False)
        group = 10001 if name in {'app.env','owner.env'} else 70 if name.endswith('.password') else 0
        if (not stat.S_ISREG(observed.st_mode) or observed.st_uid!=0
                or observed.st_gid!=group or stat.S_IMODE(observed.st_mode)!=(0o640 if group else 0o600)):
            raise ValueError('exact per-role private native access')
        if name not in {'app.env','owner.env'}:
            try:
                descriptor = os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
            except OSError as error:
                if error.errno!=errno.EACCES:
                    raise ValueError('private other-role read must be denied') from error
                denied += 1
            else:
                os.close(descriptor)
                raise ValueError('private other-role secret readable')
    if denied!=12:
        raise ValueError('complete private other-role denial inventory')
    return denied


def probe(config_sha, app_sha, owner_sha):
    denied = private_access()
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
                      'private_other_role_denied_count':denied,
                      'binary_sha256':digest.hexdigest(),
                      'clock_sha256':hashlib.sha256(clock_raw).hexdigest()}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--config-sha256',required=True)
    parser.add_argument('--app-sha256',required=True)
    parser.add_argument('--owner-sha256',required=True)
    args = parser.parse_args()
    probe(args.config_sha256,args.app_sha256,args.owner_sha256)
