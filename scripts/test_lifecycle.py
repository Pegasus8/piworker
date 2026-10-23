"""Exercise the real lifecycle script with isolated paths and fake OS services."""
import hashlib
import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("piworkerctl")


class LifecycleTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.fake = self.root / "fake"
        self.fake.mkdir()
        self.release = self.root / "release"
        self.release.mkdir()
        self.script = self.root / "piworkerctl"
        source = SCRIPT.read_text()
        for path in ("/usr/local/bin", "/etc/piworker", "/var/lib/piworker",
                     "/var/backups/piworker", "/etc/systemd/system", "/run/lock"):
            source = source.replace(path, str(self.root) + path)
        self.script.write_text(source)
        self.env = dict(os.environ, PATH=f"{self.fake}:{os.environ['PATH']}",
                        FIXTURE=str(self.root), RELEASE=str(self.release))
        self.mock("id", 'echo 0')
        self.mock("uname", 'if [ "$1" = -s ]; then echo Linux; else echo "${ARCH:-aarch64}"; fi')
        for command in ("chown", "useradd"):
            self.mock(command, 'exit 0')
        self.mock("sleep", 'exit 0')
        self.mock("systemctl", '''
echo "$*" >> "$FIXTURE/services"
case "$1" in
  restart|start) if [ -f "$FIXTURE/fail-start" ]; then
    rm "$FIXTURE/fail-start"
    echo damaged > "$FIXTURE/var/lib/piworker/piworker.db"
    exit 1
  fi ;;
esac
''')
        self.mock("curl", '''
out= url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o|--output) out=$2; shift ;;
    https://*|http://*) url=$1 ;;
  esac
  shift
done
case "$url" in
  */releases/latest) printf 'https://github.com/Pegasus8/piworker/releases/tag/v2.0.0' ;;
  */api/health) [ ! -f "$FIXTURE/fail-health" ] ;;
  *) cp "$RELEASE/${url##*/}" "$out" ;;
esac
''')
        self.bundle("old")

    def mock(self, name, body):
        path = self.fake / name
        path.write_text("#!/bin/sh\nset -eu\n" + body + "\n")
        path.chmod(0o755)

    def bundle(self, label, arch="arm64"):
        name = f"piworker-linux-{arch}"
        archive = self.release / f"{name}.tar.gz"
        with tarfile.open(archive, "w:gz") as tar:
            for filename, content in [(name, f"#!/bin/sh\n# {label}\nexit 0\n"),
                                      ("piworkerctl", self.script.read_text())]:
                data = content.encode()
                info = tarfile.TarInfo(filename)
                info.size = len(data)
                info.mode = 0o755
                tar.addfile(info, io.BytesIO(data))
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        (self.release / "SHA256SUMS").write_text(f"{digest}  ./{archive.name}\n")

    def run_ctl(self, *args, ok=True):
        result = subprocess.run(["bash", str(self.script), *args], env=self.env,
                                capture_output=True, text=True)
        if ok:
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        return result

    def path(self, name):
        return self.root / name.lstrip("/")

    def install(self):
        self.run_ctl("install")
        self.db = self.path("/var/lib/piworker/piworker.db")
        self.db.write_text("flows and accounts")
        self.config = self.path("/etc/piworker/piworker.toml")
        self.config.write_text('addr = ":8080"\n# custom configuration\n')

    def test_install_creates_service_and_manager(self):
        self.run_ctl("install")
        self.assertTrue(self.path("/usr/local/bin/piworker").stat().st_mode & 0o111)
        self.assertTrue(self.path("/usr/local/bin/piworkerctl").exists())
        self.assertEqual(self.path("/usr/local/bin").stat().st_mode & 0o777, 0o755)
        unit = self.path("/etc/systemd/system/piworker.service").read_text()
        self.assertIn("User=piworker", unit)
        self.assertIn("enable --now piworker.service", (self.root / "services").read_text())

    def test_local_release_never_downloads_and_requires_version(self):
        self.env["PIWORKER_RELEASE_DIR"] = str(self.release)
        self.mock("curl", 'case "$*" in *https:*) exit 99 ;; *) exit 0 ;; esac')
        self.run_ctl("install", ok=False)
        self.assertFalse(self.path("/usr/local/bin/piworker").exists())
        self.run_ctl("install", "v2.0.0")

    def test_local_release_checksum_is_required(self):
        self.env["PIWORKER_RELEASE_DIR"] = str(self.release)
        (self.release / "piworker-linux-arm64.tar.gz").write_bytes(b"corrupt")
        self.run_ctl("install", "v2.0.0", ok=False)
        self.assertFalse(self.path("/usr/local/bin/piworker").exists())

    def test_update_preserves_data_config_and_makes_backup(self):
        self.install()
        self.bundle("new")
        self.run_ctl("update", "v2.1.0")
        self.assertIn("# new", self.path("/usr/local/bin/piworker").read_text())
        self.assertEqual(self.db.read_text(), "flows and accounts")
        self.assertIn("custom configuration", self.config.read_text())
        backups = list(self.path("/var/backups/piworker").glob("*/data/piworker.db"))
        self.assertEqual(len(backups), 1)
        self.assertEqual(backups[0].read_text(), "flows and accounts")

    def test_bad_checksum_does_not_stop_or_replace_existing_service(self):
        self.install()
        before = (self.root / "services").read_text()
        self.bundle("new")
        with (self.release / "piworker-linux-arm64.tar.gz").open("ab") as f:
            f.write(b"corrupt")
        self.run_ctl("update", ok=False)
        self.assertEqual((self.root / "services").read_text(), before)
        self.assertIn("# old", self.path("/usr/local/bin/piworker").read_text())

    def test_failed_update_restores_binary_and_database(self):
        self.install()
        self.bundle("new")
        (self.root / "fail-start").touch()
        self.run_ctl("update", ok=False)
        self.assertIn("# old", self.path("/usr/local/bin/piworker").read_text())
        self.assertEqual(self.db.read_text(), "flows and accounts")
        self.assertIn("custom configuration", self.config.read_text())

    def test_unhealthy_update_rolls_back_and_keeps_previous_version(self):
        self.install()
        self.bundle("new")
        (self.root / "fail-health").touch()
        self.run_ctl("update", "v2.1.0", ok=False)
        self.assertIn("# old", self.path("/usr/local/bin/piworker").read_text())
        self.assertEqual(self.db.read_text(), "flows and accounts")
        self.assertEqual(self.path("/etc/piworker/installed-version").read_text(), "v2.0.0\n")

    def test_missing_release_keeps_running_installation(self):
        self.install()
        before = (self.root / "services").read_text()
        (self.release / "piworker-linux-arm64.tar.gz").unlink()
        self.run_ctl("update", ok=False)
        self.assertEqual((self.root / "services").read_text(), before)

    def test_supported_architectures(self):
        for machine, arch in (("x86_64", "amd64"), ("aarch64", "arm64"), ("armv7l", "arm")):
            self.env["ARCH"] = machine
            self.bundle(machine, arch)
            self.run_ctl("install")
            self.assertIn(f"# {machine}", self.path("/usr/local/bin/piworker").read_text())

    def test_concurrent_operation_is_rejected(self):
        self.install()
        lock = self.path("/run/lock/piworker-install.lock")
        lock.mkdir()
        self.run_ctl("uninstall", ok=False)
        self.assertTrue(self.path("/usr/local/bin/piworker").exists())

    def test_non_root_cannot_install(self):
        self.mock("id", 'echo 1000')
        self.run_ctl("install", ok=False)
        self.assertFalse(self.path("/usr/local/bin/piworker").exists())

    def test_uninstall_retains_data_and_reinstall_reuses_it(self):
        self.install()
        self.run_ctl("uninstall")
        self.assertFalse(self.path("/usr/local/bin/piworker").exists())
        self.assertFalse(self.path("/usr/local/bin/piworkerctl").exists())
        self.assertFalse(self.path("/etc/systemd/system/piworker.service").exists())
        self.assertEqual(self.db.read_text(), "flows and accounts")
        self.run_ctl("install")
        self.assertEqual(self.db.read_text(), "flows and accounts")
        self.assertIn("custom configuration", self.config.read_text())

    def test_install_twice_is_safe(self):
        self.install()
        self.run_ctl("install")
        self.assertEqual(self.db.read_text(), "flows and accounts")

    def test_update_without_install_fails(self):
        self.run_ctl("update", ok=False)
        self.assertFalse(self.path("/usr/local/bin/piworker").exists())

    def test_invalid_options_and_unsupported_platform_fail_before_changes(self):
        for args in (("uninstall", "--purge"), ("install", "../../oops"), ("nonsense",)):
            self.run_ctl(*args, ok=False)
        self.env["ARCH"] = "armv6l"
        self.run_ctl("install", ok=False)
        self.assertFalse(self.path("/usr/local/bin/piworker").exists())

    def test_unmanaged_install_is_not_overwritten(self):
        binary = self.path("/usr/local/bin/piworker")
        binary.parent.mkdir(parents=True)
        binary.write_text("existing manual installation")
        self.run_ctl("install", ok=False)
        self.assertEqual(binary.read_text(), "existing manual installation")


if __name__ == "__main__":
    unittest.main()
