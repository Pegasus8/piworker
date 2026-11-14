import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock
from types import SimpleNamespace

import validate_pi


class PackageTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.archive = self.root / "piworker-linux-arm64.tar.gz"
        self.destination = self.root / "work"
        self.destination.mkdir()

    def package(self, extra=None):
        with tarfile.open(self.archive, "w:gz") as archive:
            member = tarfile.TarInfo("piworker-linux-arm64")
            member.size = 4
            archive.addfile(member, io.BytesIO(b"test"))
            if extra:
                archive.addfile(extra)
        digest = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        (self.root / "SHA256SUMS").write_text(f"{digest}  ./{self.archive.name}\n")

    def test_verified_binary_is_extracted_executable(self):
        self.package()
        result = validate_pi.extract_package(self.archive, self.destination)
        self.assertEqual(result.read_bytes(), b"test")
        self.assertTrue(result.stat().st_mode & 0o111)

    def test_corrupt_package_is_rejected_before_extraction(self):
        self.package()
        with self.archive.open("ab") as stream:
            stream.write(b"corrupt")
        with self.assertRaises(ValueError):
            validate_pi.extract_package(self.archive, self.destination)
        self.assertEqual(list(self.destination.iterdir()), [])

    def test_missing_checksum_is_rejected(self):
        self.package()
        (self.root / "SHA256SUMS").write_text("")
        with self.assertRaises(ValueError):
            validate_pi.extract_package(self.archive, self.destination)

    def test_archive_cannot_write_outside_work_directory(self):
        self.package(tarfile.TarInfo("../escape"))
        with self.assertRaises(ValueError):
            validate_pi.extract_package(self.archive, self.destination)
        self.assertFalse((self.root / "escape").exists())

    def test_links_are_rejected(self):
        link = tarfile.TarInfo("piworkerctl")
        link.type = tarfile.SYMTYPE
        link.linkname = "/etc/passwd"
        self.package(link)
        with self.assertRaises(ValueError):
            validate_pi.extract_package(self.archive, self.destination)

    def test_detects_os_architecture_not_only_kernel(self):
        self.assertEqual(validate_pi.arch_name("aarch64", 32), "arm")
        self.assertEqual(validate_pi.arch_name("aarch64", 64), "arm64")
        self.assertEqual(validate_pi.arch_name("x86_64", 64), "amd64")
        with self.assertRaises(ValueError):
            validate_pi.arch_name("armv6l", 32)


class NativeIntegrationTests(unittest.TestCase):
    @unittest.skipUnless(os.environ.get("PIWORKER_TEST_BINARY"), "Set PIWORKER_TEST_BINARY to exercise a real binary")
    def test_real_binary_reports_success_and_cleans_private_data(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            report = root / "report.json"
            env = dict(os.environ, TMPDIR=temp, PIWORKER_AUTH="false", PIWORKER_ADMIN_USER="inherited")
            result = subprocess.run([sys.executable, str(Path(validate_pi.__file__)),
                                     "--binary", os.environ["PIWORKER_TEST_BINARY"],
                                     "--report", str(report)], env=env, text=True, capture_output=True, timeout=90)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            data = json.loads(report.read_text())
            self.assertEqual(data["status"], "passed")
            self.assertIn("session_and_data_survive_restart", [check["name"] for check in data["checks"]])
            self.assertEqual(list(root.iterdir()), [report])
            self.assertNotIn("password", report.read_text().lower())

    def test_failed_binary_still_writes_report(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            binary = root / "bad-binary"
            binary.write_text("#!/bin/sh\nexit 1\n")
            binary.chmod(0o755)
            report = root / "report.json"
            result = subprocess.run([sys.executable, str(Path(validate_pi.__file__)), "--binary", str(binary),
                                     "--report", str(report)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(json.loads(report.read_text())["status"], "failed")


class SystemdIsolationTests(unittest.TestCase):
    def runner(self, root):
        binary = root / "piworker"
        binary.write_text("fixture")
        with mock.patch("pwd.getpwnam", return_value=SimpleNamespace(pw_uid=65534, pw_gid=65534)), \
                mock.patch("validate_pi.os.chown"), mock.patch("validate_pi.socket.socket") as sock:
            sock.return_value.__enter__.return_value.getsockname.return_value = ("127.0.0.1", 18080)
            return validate_pi.Runner(binary, root, True)

    def test_service_is_temporary_and_uses_nonroot_identity(self):
        with tempfile.TemporaryDirectory() as temp:
            runner = self.runner(Path(temp))
            with mock.patch("validate_pi.subprocess.run", return_value=SimpleNamespace(returncode=0)) as run:
                runner.start()
                args = run.call_args.args[0]
                self.assertIn("--collect", args)
                self.assertEqual(args[args.index("--uid") + 1], "65534")
                self.assertTrue(runner.unit.startswith("piworker-validation-"))
                self.assertNotIn("piworker.service", args)
                self.assertNotIn("enable", args)
            with mock.patch("validate_pi.subprocess.run", side_effect=[SimpleNamespace(returncode=0), SimpleNamespace(returncode=3)]) as run:
                runner.stop()
                self.assertEqual(run.call_args_list[0].args[0], ["systemctl", "stop", runner.unit])
                self.assertFalse(runner.started)

    def test_cleanup_refuses_to_continue_if_service_still_runs(self):
        with tempfile.TemporaryDirectory() as temp:
            runner = self.runner(Path(temp))
            runner.started = True
            with mock.patch("validate_pi.subprocess.run", return_value=SimpleNamespace(returncode=0)):
                with self.assertRaises(RuntimeError):
                    runner.stop()
            self.assertTrue(runner.started)


if __name__ == "__main__":
    unittest.main()
