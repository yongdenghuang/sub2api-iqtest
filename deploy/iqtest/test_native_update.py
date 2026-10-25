import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

from native_update import NativeDeployment


class NativeDeploymentTest(unittest.TestCase):
    def exercise(self, failure=None):
        with tempfile.TemporaryDirectory() as directory, patch('native_update.time.sleep'), patch('native_update.run'):
            root = Path(directory)
            d = NativeDeployment(root, root / 'state', root / 'maintenance', root / 'validation')
            d.state.mkdir()
            events = []
            count = 0
            d.version = lambda: '2.16.0'
            d.fingerprints = lambda: {'accounts': [1, 2]}
            d.cleanup = lambda _: events.append('cleanup')
            d.service = lambda action: events.append(action)
            d.restore = lambda: events.append('restore')

            def backup():
                events.append('backup')
                if failure == 'backup':
                    raise RuntimeError('backup')
                d.before = d.fingerprints()

            def switch(_):
                events.append('switch')
                d.switched = True
                d.validation.touch()
                if failure == 'fingerprints':
                    d.before = {'accounts': []}

            def verify(_):
                nonlocal count
                count += 1
                events.append('verify')
                if failure == 'validation' and count == 1 or failure == 'normal' and count == 2:
                    raise RuntimeError('health')

            d.backup_state, d.switch, d.verify = backup, switch, verify
            if failure:
                with self.assertRaises(RuntimeError):
                    d.deploy(root, {'version': '0.2.18-iqtest.test'})
            else:
                d.deploy(root, {'version': '0.2.18-iqtest.test'})
            return events, d.maintenance.exists(), (d.state / 'UPDATE_BLOCKED').exists()

    def test_back_up_and_validate_before_resuming_oauth_refresh(self):
        events, maintenance, blocked = self.exercise()
        self.assertEqual(events, ['stop', 'backup', 'switch', 'verify', 'restart', 'verify', 'cleanup'])
        self.assertFalse(maintenance)
        self.assertFalse(blocked)

    def test_backup_failure_never_replaces_binary(self):
        events, maintenance, blocked = self.exercise('backup')
        self.assertNotIn('switch', events)
        self.assertEqual(events[-1], 'restore')
        self.assertFalse(maintenance)
        self.assertTrue(blocked)

    def test_failed_validation_restores_snapshot_before_refresh_is_enabled(self):
        events, maintenance, blocked = self.exercise('validation')
        self.assertEqual(events[-1], 'restore')
        self.assertNotIn('restart', events)
        self.assertFalse(maintenance)
        self.assertTrue(blocked)

    def test_changed_credentials_stop_deployment(self):
        events, _, blocked = self.exercise('fingerprints')
        self.assertEqual(events[-1], 'restore')
        self.assertNotIn('restart', events)
        self.assertTrue(blocked)

    def test_never_restore_stale_refresh_tokens_after_commit(self):
        events, maintenance, blocked = self.exercise('normal')
        self.assertNotIn('restore', events)
        self.assertTrue(maintenance)
        self.assertTrue(blocked)

    def test_archive_cannot_overwrite_unrelated_paths(self):
        for name in ('../etc/passwd', '/etc/passwd', 'other/config'):
            with io.BytesIO() as stream:
                with tarfile.open(fileobj=stream, mode='w') as archive:
                    archive.addfile(tarfile.TarInfo(name))
                stream.seek(0)
                with tarfile.open(fileobj=stream) as archive, self.assertRaises(RuntimeError):
                    NativeDeployment.validate_archive(archive)

    def test_rollback_uses_actual_database_and_application_owner(self):
        with tempfile.TemporaryDirectory() as directory, patch('native_update.run'):
            root = Path(directory)
            d = NativeDeployment(root, root / 'state', root / 'maintenance', root / 'validation')
            d.backup = root / 'backup'
            d.backup.mkdir()
            (d.backup / 'BACKUP_COMPLETE').touch()
            (d.backup / 'database.dump').write_bytes(b'database')
            (d.backup / 'sub2api.previous').write_bytes(b'old binary')
            (root / 'config.yaml').write_text('old config')
            with tarfile.open(d.backup / 'configuration-data.tar.gz', 'w:gz') as archive:
                archive.add(root / 'config.yaml', arcname='config.yaml')
            (root / 'config.yaml').write_text('new config')
            commands = []
            d.pg = lambda command, **kwargs: commands.append(command)
            d.switched = True
            d.service = lambda action: None
            d.restore()
            self.assertIn('-d sub2api_v2', commands[0])
            self.assertIn('--role sub2api_v2', commands[1])
            self.assertEqual((root / 'sub2api').read_bytes(), b'old binary')
            self.assertEqual((root / 'config.yaml').read_text(), 'old config')


if __name__ == '__main__':
    unittest.main()
