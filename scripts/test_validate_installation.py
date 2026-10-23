"""Safety and reboot gates for the real installation validation kit."""
import tempfile
from pathlib import Path
import unittest
from unittest.mock import patch
import validate_installation as validation


class InstallationValidationTest(unittest.TestCase):
    def test_existing_or_dangling_paths_are_never_adopted(self):
        with tempfile.TemporaryDirectory() as tmp:
            target = Path(tmp) / 'existing'
            validation.require_absent([target])
            target.symlink_to(Path(tmp) / 'missing')
            with self.assertRaisesRegex(RuntimeError, 'existente'):
                validation.require_absent([target])

    def test_resume_requires_new_boot_on_same_machine(self):
        state = {'boot': 'before', 'machine': 'pi'}
        with self.assertRaises(RuntimeError):
            validation.require_reboot(state, 'before', 'pi')
        with self.assertRaises(RuntimeError):
            validation.require_reboot(state, 'after', 'other')
        validation.require_reboot(state, 'after', 'pi')

    def test_state_requires_private_root_owned_regular_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            root.chmod(0o700)
            state = root / 'state.json'
            state.write_text('{}')
            state.chmod(0o600)
            with patch.object(validation.os, 'getuid', return_value=state.stat().st_uid):
                validation.private_file(state, owner=state.stat().st_uid)
                state.chmod(0o644)
                with self.assertRaises(RuntimeError):
                    validation.private_file(state, owner=state.stat().st_uid)

    def test_invalid_sensor_path_is_rejected_before_read(self):
        with self.assertRaises(ValueError):
            validation.sensor_path('/etc/shadow')
        self.assertEqual(str(validation.sensor_path('/sys/bus/w1/devices/28-123/w1_slave')),
                         '/sys/bus/w1/devices/28-123/w1_slave')

    def test_resume_does_not_start_service_before_boot_check(self):
        with patch.object(validation, 'load_state', return_value={'boot': 'same', 'machine': 'pi'}), \
             patch.object(validation, 'identity', return_value=('same', 'pi')), \
             patch.object(validation, 'run') as run:
            with self.assertRaises(RuntimeError):
                validation.resume()
            run.assert_not_called()

    def test_uninstall_must_stop_service(self):
        from subprocess import CompletedProcess
        with patch.object(validation, 'ctl'), patch.object(validation, 'require_absent'), \
             patch.object(validation, 'run', return_value=CompletedProcess([], 0)):
            with self.assertRaisesRegex(RuntimeError, 'sigue activo'):
                validation.uninstall()

    def test_changed_installation_blocks_resume_before_uninstall(self):
        state = {'boot': 'old', 'machine': 'pi', 'phase': 'awaiting_reboot',
                 'checks': [], 'not_tested': [], 'files': {'binary': 'old'}}
        with patch.object(validation, 'load_state', return_value=state), \
             patch.object(validation, 'identity', return_value=('new', 'pi')), \
             patch.object(validation, 'snapshot', return_value={'binary': 'changed'}), \
             patch.object(validation, 'ctl') as ctl, patch.object(validation, 'run') as run:
            with self.assertRaisesRegex(RuntimeError, 'cambió'):
                validation.resume()
            ctl.assert_not_called()
            run.assert_not_called()
