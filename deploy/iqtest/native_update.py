#!/usr/bin/env python3
"""Restricted deployment for api.yongdh.top's existing systemd installation.

Keep the actual sub2api_v2 database and the original configuration. In particular,
the compose project's default database is an unused empty database on this host.
"""
import fcntl
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request

from update import run, validate_image

ROOT = Path('/opt/sub2api')
STATE = Path('/opt/sub2api-iqtest-migration')
MAINTENANCE = Path('/etc/nginx/iqtest-api.yongdh.top-maintenance')
VALIDATION = Path('/etc/systemd/system/sub2api.service.d/91-iqtest-validation.conf')
SOURCE = 'https://github.com/yongdenghuang/sub2api-iqtest'


class NativeDeployment:
    def __init__(self, root=ROOT, state=STATE, maintenance=MAINTENANCE, validation=VALIDATION):
        self.root, self.state = root, state
        self.maintenance, self.validation = maintenance, validation
        self.backup = None
        self.stopped = False
        self.switched = False
        self.committed = False
        self.before = None

    def pg(self, command, **kwargs):
        return run(['docker', 'exec', '-i', 'sub2api-postgres', 'sh', '-c', command], **kwargs)

    def query(self, sql):
        result = self.pg('psql -X -At -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d sub2api_v2', input=sql.encode(), stdout=subprocess.PIPE)
        return json.loads(result.stdout)

    def fingerprints(self):
        return self.query("""SELECT json_build_object(
          'accounts', (SELECT json_agg(t) FROM (SELECT id,md5(credentials::text) AS fingerprint FROM accounts ORDER BY id) t),
          'users', (SELECT md5(jsonb_agg(jsonb_build_object('id',id,'email',email,'password',password_hash,'balance',balance) ORDER BY id)::text) FROM users),
          'api_keys', (SELECT md5(jsonb_agg(jsonb_build_object('id',id,'key',key,'user_id',user_id,'group_id',group_id) ORDER BY id)::text) FROM api_keys),
          'secrets', (SELECT md5(jsonb_agg(jsonb_build_object('key',key,'value',value) ORDER BY key)::text) FROM security_secrets)
        );""")

    def version(self):
        with urllib.request.urlopen('http://127.0.0.1:8081/api/v1/settings/public', timeout=15) as response:
            return json.load(response)['data']['version']

    def service(self, action):
        run(['systemctl', action, 'sub2api.service'])

    def verify(self, expected):
        for attempt in range(90):
            try:
                if self.version() == expected:
                    with urllib.request.urlopen('http://127.0.0.1:8081/health', timeout=3) as response:
                        if response.status == 200:
                            break
            except Exception:
                pass
            time.sleep(1)
        else:
            raise RuntimeError('Application did not become healthy with the expected version')
        if '-iqtest.' in expected:
            for endpoint in ('summary', 'defaults', 'plans'):
                try:
                    urllib.request.urlopen('http://127.0.0.1:8081/api/v1/admin/iqtest/' + endpoint, timeout=10).close()
                except urllib.error.HTTPError as error:
                    if error.code != 401:
                        raise RuntimeError('Unexpected IQTest authentication response') from None
                else:
                    raise RuntimeError('IQTest endpoint is not protected')
            self.pg('psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d sub2api_v2', input=b'SELECT id FROM iq_plans LIMIT 0; SELECT kind,state FROM iq_account_state LIMIT 0;', stdout=subprocess.DEVNULL)

    def pull(self, image, token):
        validate_image(image)
        if shutil.disk_usage(self.root).free < 2 * 1024 * 1024 * 1024:
            raise RuntimeError('Less than 2 GiB free for image extraction and backups; production was not changed')
        with tempfile.TemporaryDirectory(prefix='iqtest-registry-') as auth:
            run(['docker', '--config', auth, 'login', 'ghcr.io', '-u', 'yongdenghuang', '--password-stdin'], input=token, stdout=subprocess.DEVNULL)
            run(['docker', '--config', auth, 'pull', image])
        labels = json.loads(run(['docker', 'image', 'inspect', image, '--format', '{{json .Config.Labels}}'], stdout=subprocess.PIPE).stdout)
        revision = labels.get('org.opencontainers.image.revision', '')
        version = labels.get('org.opencontainers.image.version', '')
        if labels.get('org.opencontainers.image.source') != SOURCE or not re.fullmatch('[0-9a-f]{40}', revision) or '-iqtest.' not in version:
            raise RuntimeError('Unexpected image source, revision, or version')
        candidate = self.state / 'releases' / revision
        candidate.mkdir(mode=0o700, parents=True, exist_ok=True)
        container = run(['docker', 'create', image], stdout=subprocess.PIPE).stdout.decode().strip()
        try:
            run(['docker', 'cp', container + ':/app/sub2api', str(candidate / 'sub2api')])
            run(['docker', 'cp', container + ':/app/resources', str(candidate)])
        finally:
            run(['docker', 'rm', '-v', container], stdout=subprocess.DEVNULL)
        return candidate, {'image': image, 'revision': revision, 'version': version}

    def backup_state(self):
        with (self.backup / 'database.dump').open('wb') as out:
            self.pg('pg_dump -Fc -U "$POSTGRES_USER" sub2api_v2', stdout=out)
        with (self.backup / 'database.dump').open('rb') as inp:
            self.pg('pg_restore --list', stdin=inp, stdout=subprocess.DEVNULL)
        shutil.copy2(self.root / 'sub2api', self.backup / 'sub2api.previous')
        with tarfile.open(self.backup / 'configuration-data.tar.gz', 'w:gz') as archive:
            for name in ('config.yaml', '.env', '.installed', 'data', 'resources'):
                if (self.root / name).exists():
                    archive.add(self.root / name, arcname=name)
        with tarfile.open(self.backup / 'configuration-data.tar.gz') as archive:
            self.validate_archive(archive)
        self.before = self.fingerprints()
        (self.backup / 'fingerprints.json').write_text(json.dumps(self.before))
        (self.backup / 'BACKUP_COMPLETE').touch()

    @staticmethod
    def validate_archive(archive):
        allowed = {'config.yaml', '.env', '.installed', 'data', 'resources'}
        for member in archive.getmembers():
            path = Path(member.name)
            if path.is_absolute() or '..' in path.parts or not path.parts or path.parts[0] not in allowed or not (member.isfile() or member.isdir()):
                raise RuntimeError('Unsupported backup entry; refusing application replacement')

    def switch(self, candidate):
        self.validation.parent.mkdir(parents=True, exist_ok=True)
        self.validation.write_text('[Service]\nEnvironment=TOKEN_REFRESH_ENABLED=false\n')
        run(['systemctl', 'daemon-reload'])
        self.switched = True
        temporary = self.root / '.sub2api.iqtest-new'
        shutil.copy2(candidate / 'sub2api', temporary)
        temporary.chmod(0o755)
        temporary.replace(self.root / 'sub2api')
        if (self.root / 'resources').exists():
            shutil.rmtree(self.root / 'resources')
        shutil.copytree(candidate / 'resources', self.root / 'resources')
        # Image files are readable by the existing systemd service user (uid 997).
        for directory, _, files in os.walk(self.root / 'resources'):
            Path(directory).chmod(0o755)
            for name in files:
                (Path(directory) / name).chmod(0o644)
        self.service('start')

    def restore(self):
        if self.switched:
            self.service('stop')
            if not (self.backup / 'BACKUP_COMPLETE').exists():
                raise RuntimeError('Complete backup is required for rollback')
            self.pg('psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d sub2api_v2', input=b'DROP SCHEMA public CASCADE; CREATE SCHEMA public AUTHORIZATION sub2api_v2;', stdout=subprocess.DEVNULL)
            with (self.backup / 'database.dump').open('rb') as inp:
                self.pg('pg_restore --exit-on-error --no-owner --role sub2api_v2 -U "$POSTGRES_USER" -d sub2api_v2', stdin=inp, stdout=subprocess.DEVNULL)
            with tarfile.open(self.backup / 'configuration-data.tar.gz') as archive:
                self.validate_archive(archive)
                for name in ('data', 'resources'):
                    if (self.root / name).exists():
                        shutil.rmtree(self.root / name)
                archive.extractall(self.root)
            shutil.copy2(self.backup / 'sub2api.previous', self.root / 'sub2api')
        self.validation.unlink(missing_ok=True)
        run(['systemctl', 'daemon-reload'])
        if self.stopped:
            self.service('start')
            self.verify(self.previous_version)

    def deploy(self, candidate, release):
        self.image = release.get('image')
        if (self.state / 'UPDATE_BLOCKED').exists() or self.maintenance.exists() or self.validation.exists():
            raise RuntimeError('Resolve the previous update / maintenance state before retrying')
        if shutil.disk_usage(self.root).free < 750 * 1024 * 1024:
            raise RuntimeError('Less than 750 MiB free; production was not changed')
        self.previous_version = self.version()
        current = self.state / 'current-release.json'
        if current.exists() and self.previous_version == release['version'] and json.loads(current.read_text()).get('image') == release.get('image'):
            print('This tested release is already running; no restart needed.', flush=True)
            return
        self.backup = self.state / 'backups' / ('native-' + time.strftime('%Y%m%d-%H%M%S', time.gmtime()))
        self.backup.mkdir(mode=0o700, parents=True)
        (self.backup / 'release.json').write_text(json.dumps(dict(release, previous_version=self.previous_version)))
        try:
            self.maintenance.touch()
            time.sleep(1)
            self.stopped = True
            self.service('stop')
            self.backup_state()
            self.switch(candidate)
            self.verify(release['version'])
            if self.fingerprints() != self.before:
                raise RuntimeError('Account credentials, users, API keys, or signing keys changed during validation')
            # OAuth refresh is disabled until this point, so a failed validation
            # can safely restore the snapshot without reviving a rotated token.
            self.committed = True
            self.validation.unlink()
            run(['systemctl', 'daemon-reload'])
            self.service('restart')
            self.verify(release['version'])
        except BaseException:
            (self.state / 'UPDATE_BLOCKED').write_text('Update failed. Review backups and logs before clearing this file.\n')
            if not self.committed:
                try:
                    self.restore()
                    self.maintenance.unlink(missing_ok=True)
                    print('Validation failed; previous application and database restored.', flush=True)
                except BaseException:
                    print('Rollback needs attention; maintenance and backups preserved.', flush=True)
            else:
                # Normal token refresh may already have rotated credentials.
                # Never replace those with an older database snapshot.
                print('Normal startup needs attention; current credentials and maintenance preserved.', flush=True)
            raise
        (self.state / 'current-release.json').write_text(json.dumps(dict(release, backup=str(self.backup))))
        self.maintenance.unlink()
        print('Native deployment verified; account credentials, users, API keys and signing keys preserved. Backup: ' + str(self.backup), flush=True)
        self.cleanup(candidate)

    def cleanup(self, candidate):
        try:
            backups = sorted((self.state / 'backups').glob('native-*'), reverse=True)
            for old in backups[3:]:
                if (old / 'BACKUP_COMPLETE').exists():
                    shutil.rmtree(old)
            releases = self.state / 'releases'
            if releases.exists():
                for old in releases.iterdir():
                    if old != candidate and re.fullmatch('[0-9a-f]{40}', old.name):
                        shutil.rmtree(old)
            if self.image:
                # The native service runs the extracted binary; image layers are
                # delivery artifacts. Backups retain the previous native binaries.
                images = run(['docker', 'image', 'ls', '--no-trunc', '--filter', 'label=org.opencontainers.image.source=' + SOURCE, '--format', '{{.ID}}'], stdout=subprocess.PIPE).stdout.decode().splitlines()
                for image_id in set(images):
                    subprocess.run(['docker', 'image', 'rm', image_id], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
        except Exception:
            print('Backup cleanup deferred; site is healthy.', flush=True)


def main():
    os.umask(0o077)
    if len(sys.argv) != 2:
        raise ValueError('Expected one immutable custom image digest')
    image = validate_image(sys.argv[1])
    with (STATE / '.update.lock').open('w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        token = sys.stdin.buffer.read(16385)
        if not token or len(token) > 16384:
            raise ValueError('Missing or oversized registry token')
        deployment = NativeDeployment()
        if (STATE / 'UPDATE_BLOCKED').exists() or MAINTENANCE.exists():
            raise RuntimeError('A previous update requires attention')
        current = STATE / 'current-release.json'
        if current.exists():
            previous = json.loads(current.read_text())
            if previous.get('image') == image and deployment.version() == previous.get('version'):
                print('This tested release is already running; no download or restart needed.', flush=True)
                return
        candidate, release = deployment.pull(image, token)
        deployment.deploy(candidate, release)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        detail = str(error) if type(error) in (ValueError, RuntimeError) else type(error).__name__
        print('Deployment stopped: ' + detail, file=sys.stderr)
        sys.exit(1)
