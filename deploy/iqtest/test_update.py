import tempfile
import tarfile
from pathlib import Path
import unittest
from unittest.mock import patch

from update import Deployment, validate_image

IMAGE = 'ghcr.io/yongdenghuang/sub2api-iqtest@sha256:' + 'a' * 64


class DeploymentTest(unittest.TestCase):
    def test_only_immutable_custom_image_is_accepted(self):
        self.assertEqual(validate_image(IMAGE), IMAGE)
        for value in ('bash', IMAGE + '; id', IMAGE + '\n', 'ghcr.io/wei-shaw/sub2api:latest', IMAGE.replace('@sha256:', ':')):
            with self.assertRaises(ValueError):
                validate_image(value)

    def exercise(self, failure=None, restore_failure=False):
        with tempfile.TemporaryDirectory() as directory, patch('update.time.sleep'):
            root = Path(directory)
            d = Deployment(root, root / 'maintenance')
            d.backup = root / 'backup'
            d.backup.mkdir()
            (d.backup / 'release.json').write_text('{}')
            actions = []

            def action(name):
                def call(*args, **kwargs):
                    actions.append(name)
                    if name == failure or name == 'restore' and restore_failure:
                        raise RuntimeError(name)
                return call

            for name in ('prepare', 'compose', 'backup_state', 'switch', 'verify', 'restore', 'cleanup'):
                setattr(d, name, action(name))
            if failure:
                with self.assertRaises(RuntimeError):
                    d.deploy(IMAGE, b'test-token')
            else:
                d.deploy(IMAGE, b'test-token')
            return actions, d.maintenance.exists(), d.blocked.exists()

    def test_success_backs_up_before_switching(self):
        actions, maintenance, blocked = self.exercise()
        self.assertEqual(actions, ['prepare', 'compose', 'backup_state', 'switch', 'verify', 'cleanup'])
        self.assertFalse(maintenance)
        self.assertFalse(blocked)

    def test_pull_failure_does_not_stop_production(self):
        actions, maintenance, blocked = self.exercise('prepare')
        self.assertEqual(actions, ['prepare'])
        self.assertFalse(maintenance)
        self.assertFalse(blocked)

    def test_backup_failure_cannot_start_new_image(self):
        actions, maintenance, blocked = self.exercise('backup_state')
        self.assertNotIn('switch', actions)
        self.assertEqual(actions[-1], 'restore')
        self.assertFalse(maintenance)
        self.assertTrue(blocked)

    def test_failed_health_check_restores_previous_release_and_blocks_updates(self):
        actions, maintenance, blocked = self.exercise('verify')
        self.assertEqual(actions[-1], 'restore')
        self.assertFalse(maintenance)
        self.assertTrue(blocked)

    def test_failed_rollback_keeps_maintenance_and_backup(self):
        _, maintenance, blocked = self.exercise('verify', restore_failure=True)
        self.assertTrue(maintenance)
        self.assertTrue(blocked)

    def test_restore_recovers_data_and_original_compose_override(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            d = Deployment(root, root / 'maintenance')
            d.backup = root / 'backup'
            d.backup.mkdir()
            (root / 'data').mkdir()
            (root / 'data' / 'config').write_text('old')
            with tarfile.open(d.backup / 'data.tar.gz', 'w:gz') as archive:
                archive.add(root / 'data', arcname='data')
            (root / 'data' / 'config').write_text('new')
            (d.backup / 'database.dump').write_bytes(b'database')
            (d.backup / 'BACKUP_COMPLETE').touch()
            d.previous_override = b'{"old":true}'
            d.new_started = d.stopped = True
            with patch.object(d, 'compose') as compose, patch.object(d, 'start') as start, patch.object(d, 'verify') as verify:
                d.restore()
            self.assertEqual((root / 'data' / 'config').read_text(), 'old')
            self.assertEqual(d.override.read_bytes(), d.previous_override)
            commands = [str(call.args) for call in compose.call_args_list]
            self.assertIn('stop', commands[0])
            self.assertIn('DROP SCHEMA public', commands[1])
            self.assertIn('pg_restore --exit-on-error', commands[2])
            start.assert_called_once()
            verify.assert_called_once()

    def test_backup_failure_does_not_restore_database(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            d = Deployment(root, root / 'maintenance')
            d.stopped = True
            with patch.object(d, 'compose') as compose, patch.object(d, 'start'), patch.object(d, 'verify'):
                d.restore()
            compose.assert_not_called()


if __name__ == '__main__':
    unittest.main()
