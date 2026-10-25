#!/usr/bin/env python3
"""Restricted deployment entry point, installed root-owned outside the checkout.

SSH supplies only an immutable image digest; stdin is a short-lived GHCR token.
No shell command, arbitrary path, tag, host, or repository is accepted.
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

ROOT = Path('/opt/sub2api-iqtest')
MAINTENANCE = Path('/opt/1panel/apps/openresty/openresty/www/sites/sapi.yongdh.top/iqtest-maintenance')
SOURCE = 'https://github.com/yongdenghuang/sub2api-iqtest'
IMAGE_PATTERN = r'ghcr\.io/yongdenghuang/sub2api-iqtest@sha256:[0-9a-f]{64}'


def validate_image(image):
    if not re.fullmatch(IMAGE_PATTERN, image):
        raise ValueError('Only an immutable IQTest GHCR image digest is allowed')
    return image


def run(args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


class Deployment:
    def __init__(self, root=ROOT, maintenance=MAINTENANCE):
        self.root = root
        self.maintenance = maintenance
        self.override = root / 'compose.iqtest-image.json'
        self.blocked = root / 'UPDATE_BLOCKED'
        self.backup = None
        self.previous_override = None
        self.new_started = False
        self.stopped = False
        self.previous_image_id = None

    def compose(self, *args, **kwargs):
        command = ['docker', 'compose', '-f', str(self.root / 'compose.yaml')]
        if self.override.exists():
            command += ['-f', str(self.override)]
        return run(command + list(args), cwd=self.root, **kwargs)

    def prepare(self, image, token):
        if self.blocked.exists():
            raise RuntimeError('A previous update failed. Resolve UPDATE_BLOCKED before retrying.')
        if self.maintenance.exists():
            raise RuntimeError('Site is already in maintenance; refusing to change it')
        if shutil.disk_usage(self.root).free < 750 * 1024 * 1024:
            raise RuntimeError('Less than 750 MiB free; production was not changed')
        self.previous_override = self.override.read_bytes() if self.override.exists() else None
        container = self.compose('ps', '-q', 'app', stdout=subprocess.PIPE).stdout.decode().strip()
        if not container:
            raise RuntimeError('No running app is available for rollback')
        self.previous_image_id = run(['docker', 'inspect', container, '--format', '{{.Image}}'], stdout=subprocess.PIPE).stdout.decode().strip()
        with tempfile.TemporaryDirectory(prefix='iqtest-registry-') as auth:
            run(['docker', '--config', auth, 'login', 'ghcr.io', '-u', 'yongdenghuang', '--password-stdin'], input=token, stdout=subprocess.DEVNULL)
            run(['docker', '--config', auth, 'pull', image])
        labels = json.loads(run(['docker', 'image', 'inspect', image, '--format', '{{json .Config.Labels}}'], stdout=subprocess.PIPE).stdout)
        if labels.get('org.opencontainers.image.source') != SOURCE:
            raise RuntimeError('Image source does not match the custom repository')
        if not re.fullmatch('[0-9a-f]{40}', labels.get('org.opencontainers.image.revision', '')):
            raise RuntimeError('Image has no valid source revision')
        stamp = time.strftime('%Y%m%d-%H%M%S', time.gmtime())
        self.backup = self.root / 'backups' / ('auto-' + stamp)
        self.backup.mkdir(mode=0o700, parents=True)
        shutil.copy2(self.root / 'compose.yaml', self.backup / 'compose.yaml')
        if self.previous_override is not None:
            (self.backup / self.override.name).write_bytes(self.previous_override)
        (self.backup / 'release.json').write_text(json.dumps({'image': image, 'revision': labels['org.opencontainers.image.revision'], 'previous_image_id': self.previous_image_id}))

    def backup_state(self):
        with (self.backup / 'database.dump').open('wb') as output:
            self.compose('exec', '-T', 'postgres', 'sh', '-c', 'pg_dump -Fc -U "$POSTGRES_USER" "$POSTGRES_DB"', stdout=output)
        if (self.backup / 'database.dump').stat().st_size < 32:
            raise RuntimeError('Database backup is empty')
        with (self.backup / 'database.dump').open('rb') as backup:
            self.compose('exec', '-T', 'postgres', 'pg_restore', '--list', stdin=backup, stdout=subprocess.DEVNULL)
        with tarfile.open(self.backup / 'data.tar.gz', 'w:gz') as archive:
            archive.add(self.root / 'data', arcname='data')
        (self.backup / 'BACKUP_COMPLETE').touch()

    def switch(self, image):
        candidate = self.override.with_suffix('.tmp')
        candidate.write_text(json.dumps({'services': {'app': {'image': image, 'pull_policy': 'never'}}}))
        candidate.replace(self.override)
        self.new_started = True
        self.start()

    def start(self):
        self.compose('up', '-d', '--no-deps', '--no-build', '--wait', '--wait-timeout', '150', 'app')

    def verify(self):
        base = 'http://127.0.0.1:8080'
        with urllib.request.urlopen(base + '/health', timeout=15) as response:
            if response.status != 200:
                raise RuntimeError('Health check failed')
        env = {}
        for line in (self.root / '.env').read_text().splitlines():
            if line and not line.startswith('#') and '=' in line:
                key, value = line.split('=', 1)
                env[key] = value.strip().strip('"').strip("'")
        token = None

        def request(path, body=None):
            headers = {'Content-Type': 'application/json'}
            if token:
                headers['Authorization'] = 'Bearer ' + token
            data = None if body is None else json.dumps(body).encode()
            req = urllib.request.Request(base + '/api/v1' + path, data=data, headers=headers)
            with urllib.request.urlopen(req, timeout=20) as response:
                return json.load(response)['data']

        # Also prove the custom endpoints still require authentication.
        try:
            request('/admin/iqtest/summary')
        except urllib.error.HTTPError as error:
            if error.code != 401:
                raise RuntimeError('Unexpected IQTest authentication response') from None
        else:
            raise RuntimeError('IQTest endpoint is not protected')
        login = request('/auth/login', {'email': env['ADMIN_EMAIL'], 'password': env['ADMIN_PASSWORD']})
        token = login['access_token']
        try:
            defaults = request('/admin/iqtest/defaults')
            if defaults['default_model'] != 'gpt-6-astra' or set(defaults['check_kinds']) != {'candy', 'pelican_svg'}:
                raise RuntimeError('IQTest defaults were lost')
            summary = request('/admin/iqtest/summary')
            if summary['total'] != sum(summary[k] for k in ('quality_fail', 'infra_error', 'normal', 'untested')):
                raise RuntimeError('IQTest summary does not balance')
            request('/admin/iqtest/plans')
        finally:
            request('/auth/logout', {})

    def restore(self):
        if self.new_started:
            self.compose('stop', '-t', '90', 'app')
            if not (self.backup / 'BACKUP_COMPLETE').exists():
                raise RuntimeError('Refusing rollback without complete backup')
            # HTTP traffic remains blocked throughout rollback. This database is
            # dedicated to this compose project; no other service is modified.
            self.compose('exec', '-T', 'postgres', 'sh', '-c', 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"', stdout=subprocess.DEVNULL)
            with (self.backup / 'database.dump').open('rb') as backup:
                self.compose('exec', '-T', 'postgres', 'sh', '-c', 'pg_restore --exit-on-error --no-owner -U "$POSTGRES_USER" -d "$POSTGRES_DB"', stdin=backup, stdout=subprocess.DEVNULL)
            with tarfile.open(self.backup / 'data.tar.gz') as archive:
                # Our backup contains only regular files and directories.
                for member in archive.getmembers():
                    path = Path(member.name)
                    if path.is_absolute() or '..' in path.parts or path.parts[0] != 'data' or not (member.isfile() or member.isdir()):
                        raise RuntimeError('Unsupported data backup member')
                shutil.rmtree(self.root / 'data')
                archive.extractall(self.root)
        if self.previous_override is None:
            self.override.unlink(missing_ok=True)
        else:
            self.override.write_bytes(self.previous_override)
        if self.stopped:
            self.start()
            self.verify()

    def deploy(self, image, token):
        validate_image(image)
        self.prepare(image, token)
        try:
            self.maintenance.touch()
            # Allow Nginx to observe maintenance, then drain existing requests.
            time.sleep(1)
            self.stopped = True
            self.compose('stop', '-t', '90', 'app')
            self.backup_state()
            self.switch(image)
            self.verify()
        except BaseException:
            self.blocked.write_text('Automatic update failed. Review the workflow and backups before clearing this file.\n')
            try:
                self.restore()
                self.maintenance.unlink(missing_ok=True)
                print('Update failed; previous application and data restored. Future updates are blocked.', flush=True)
            except BaseException:
                print('Rollback needs attention; maintenance remains enabled. Complete backup is preserved.', flush=True)
            raise
        self.maintenance.unlink()
        (self.root / 'current-release.json').write_text((self.backup / 'release.json').read_text())
        print('Deployment verified; database/data backup: ' + str(self.backup), flush=True)
        self.cleanup()

    def cleanup(self):
        # Keep the three newest backups. Never prune unrelated Docker data.
        backups = sorted((self.root / 'backups').glob('auto-*'), reverse=True)
        for backup in backups[3:]:
            if (backup / 'BACKUP_COMPLETE').is_file():
                shutil.rmtree(backup)
        # Image cleanup is deliberately best-effort after a successful deploy.
        try:
            container = self.compose('ps', '-q', 'app', stdout=subprocess.PIPE).stdout.decode().strip()
            current = run(['docker', 'inspect', container, '--format', '{{.Image}}'], stdout=subprocess.PIPE).stdout.decode().strip()
            keep = {current, self.previous_image_id}
            images = run(['docker', 'image', 'ls', '--no-trunc', '--filter', 'reference=ghcr.io/yongdenghuang/sub2api-iqtest', '--format', '{{.ID}}'], stdout=subprocess.PIPE).stdout.decode().splitlines()
            for image_id in set(images) - keep:
                subprocess.run(['docker', 'image', 'rm', image_id], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
        except Exception:
            print('Old image cleanup deferred; deployment is healthy.', flush=True)


def main():
    os.umask(0o077)
    if len(sys.argv) != 2:
        raise ValueError('Expected one immutable image digest')
    validate_image(sys.argv[1])
    with (ROOT / '.update.lock').open('w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        token = sys.stdin.buffer.read(16385)
        if not token or len(token) > 16384:
            raise ValueError('Missing or oversized registry token')
        Deployment().deploy(sys.argv[1], token)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # Do not print subprocess arguments, HTTP bodies or credentials.
        print('Deployment stopped: ' + type(error).__name__, file=sys.stderr)
        sys.exit(1)
