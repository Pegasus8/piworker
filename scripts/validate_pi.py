#!/usr/bin/env python3
"""Prueba PiWorker con datos temporales. Requiere Python 3.9+, sin dependencias.

python3 validate_pi.py --package piworker-linux-arm64.tar.gz
sudo python3 validate_pi.py --package piworker-linux-arm64.tar.gz --systemd
"""
import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import signal
import socket
import struct
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
import uuid


def arch_name(machine, bits):
    if machine in ("aarch64", "arm64"):
        return "arm64" if bits == 64 else "arm"
    if machine in ("armv7l", "armv8l"):
        return "arm"
    if machine in ("x86_64", "amd64") and bits == 64:
        return "amd64"
    raise ValueError("Arquitectura no soportada: " + machine)


def extract_package(path, destination):
    match = re.fullmatch(r"piworker-linux-(amd64|arm64|arm)\.tar\.gz", path.name)
    if not match:
        raise ValueError("Nombre de paquete no reconocido")
    entries = []
    for line in (path.parent / "SHA256SUMS").read_text().splitlines():
        fields = line.split()
        if len(fields) == 2 and fields[1] in (path.name, "./" + path.name):
            entries.append(fields[0])
    if len(entries) != 1 or not re.fullmatch(r"[a-fA-F0-9]{64}", entries[0]):
        raise ValueError("Falta un checksum único y válido en SHA256SUMS")
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    if digest.hexdigest() != entries[0].lower():
        raise ValueError("Checksum incorrecto; no se ejecutó el paquete")
    name = "piworker-linux-" + match[1]
    with tarfile.open(path) as archive:
        members = archive.getmembers()
        names = [member.name for member in members]
        if (len(set(names)) != len(names) or name not in names or
                any(not member.isfile() or member.name not in (name, "piworkerctl") or
                    member.size > 256 * 1024 * 1024 for member in members)):
            raise ValueError("Contenido inesperado o enlaces en el paquete")
        binary = destination / "piworker"
        with archive.extractfile(name) as source, binary.open("wb") as target:
            shutil.copyfileobj(source, target)
    binary.chmod(0o755)
    return binary


class Runner:
    def __init__(self, binary, work, systemd):
        self.binary, self.work, self.systemd = binary, work, systemd
        self.env = {key: value for key, value in os.environ.items() if not key.startswith("PIWORKER_")}
        self.process = self.log = None
        self.unit = "piworker-validation-" + uuid.uuid4().hex
        self.started = False
        self.identity = {}
        with socket.socket() as probe:
            probe.bind(("127.0.0.1", 0))
            self.port = probe.getsockname()[1]
        self.base = f"http://127.0.0.1:{self.port}"
        config = work / "piworker.toml"
        config.write_text("auth = true\n")
        self.args = [str(binary), "-config", str(config), "-db", str(work / "test.db"),
                     "-addr", f"127.0.0.1:{self.port}", "-auth=true", "-session-secure=false",
                     "-admin-user", "", "-admin-pass", ""]
        if systemd:
            import pwd
            account = pwd.getpwnam("nobody")
            self.identity = {"user": account.pw_uid, "group": account.pw_gid, "extra_groups": []}
            for path in [work, *work.iterdir()]:
                os.chown(path, account.pw_uid, account.pw_gid)

    def command(self, args):
        result = subprocess.run(args, cwd=self.work, env=self.env, capture_output=True,
                                text=True, timeout=30, **self.identity)
        if result.returncode:
            raise RuntimeError(f"El comando de PiWorker terminó con código {result.returncode}")
        return result.stdout

    def code(self, purpose):
        flag = ["-setup-code"] if purpose == "setup" else ["-recover-account", "validation"]
        output = self.command([*self.args, *flag])
        match = re.search(rf"{purpose} code .*: ([A-Za-z0-9_-]{{43}})", output)
        if not match:
            raise RuntimeError("No se pudo generar el código de acceso temporal")
        return match[1]

    def start(self):
        if self.systemd:
            self.started = True
            result = subprocess.run(["systemd-run", "--quiet", "--collect", "--unit", self.unit,
                                     "--uid", str(self.identity["user"]), "--gid", str(self.identity["group"]),
                                     "--property=Type=exec", "--property=UMask=0077",
                                     "--working-directory", str(self.work), *self.args],
                                    capture_output=True, text=True, timeout=30)
            if result.returncode:
                raise RuntimeError("No se pudo iniciar la unidad temporal de systemd")
        else:
            self.log = (self.work / "server.log").open("ab")
            self.process = subprocess.Popen(self.args, cwd=self.work, env=self.env,
                                            stdout=self.log, stderr=subprocess.STDOUT)
            self.started = True

    def alive(self):
        if self.systemd:
            return subprocess.run(["systemctl", "is-active", "--quiet", self.unit],
                                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10).returncode == 0
        return self.process is not None and self.process.poll() is None

    def stop(self):
        if not self.started:
            return
        if self.systemd:
            subprocess.run(["systemctl", "stop", self.unit], capture_output=True, timeout=30)
            if self.alive():
                raise RuntimeError("La unidad temporal sigue activa; se conservaron sus datos")
        elif self.process:
            if self.process.poll() is None:
                self.process.terminate()
                try:
                    self.process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    self.process.kill()
                    self.process.wait(timeout=10)
            self.log.close()
        self.started = False


class Client:
    def __init__(self, base):
        self.base = base
        self.cookies = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                                                  urllib.request.HTTPCookieProcessor(self.cookies))

    def request(self, path, body=None, method=None):
        req = urllib.request.Request(self.base + path, method=method,
                                     data=None if body is None else json.dumps(body).encode(),
                                     headers={"Content-Type": "application/json", "X-PiWorker-Request": "1"})
        with self.opener.open(req, timeout=5) as response:
            return response.read()

    def api(self, path, body=None, method=None):
        return json.loads(self.request(path, body, method))


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def wait_for(predicate, timeout=30):
    until = time.monotonic() + timeout
    while time.monotonic() < until:
        try:
            if predicate():
                return
        except (OSError, ValueError):
            pass
        time.sleep(0.25)
    raise RuntimeError("Se agotó el tiempo de espera")


def exercise(runner, check):
    client = Client(runner.base)
    credential = secrets.token_urlsafe(24)

    def start():
        runner.start()
        wait_for(lambda: runner.alive() and client.api("/api/health") == {"status": "healthy"})
    check("startup_and_health", start)

    def setup():
        require(client.api("/api/auth/status")["data"]["setupRequired"], "La instalación de prueba no está vacía")
        client.api("/api/auth/setup", {"username": "validation", "password": credential, "code": runner.code("setup")})
        require(not client.api("/api/auth/status")["data"]["setupRequired"], "El setup no quedó cerrado")
    check("first_account_setup", setup)

    def ui():
        html = client.request("/").decode()
        assets = re.findall(r'(?:src|href)="(/assets/[^\"]+)"', html)
        require('id="app"' in html and assets, "Falta la interfaz embebida")
        for asset in assets:
            require(len(client.request(asset)) > 100, "Asset vacío")
    check("embedded_ui_assets", ui)

    def login():
        client.api("/api/auth/login", {"username": "validation", "password": credential})
        require(bool(list(client.cookies)), "No se recibió una sesión")
    check("login_session", login)

    def create():
        data = {"name": "Pi validation", "nodes": [
            {"id": "manual", "type": "trigger-manual", "category": "input", "enabled": True,
             "config": {"payload": "11"}, "position": {"x": 0, "y": 0}},
            {"id": "set", "type": "set-var", "category": "processing", "enabled": True,
             "config": {"scope": "flow", "key": "result", "value": "payload.data"}, "position": {"x": 250, "y": 0}}],
            "connections": [{"id": "edge", "sourceNode": "manual", "sourcePort": "output",
                             "targetNode": "set", "targetPort": "input"}]}
        flow = client.api("/api/flows", data)["data"]["id"]
        client.api("/api/variables/threshold", {"value": 25}, "PUT")
        client.api("/api/secrets/VALIDATION", {"value": credential}, "PUT")
        client.api(f"/api/flows/{flow}/deploy", {})
        client.api(f"/api/flows/{flow}/inject/manual", {"payload": 11})
        wait_for(lambda: client.api(f"/api/flows/{flow}/variables")["data"]["variables"].get("result") == 11)
        return flow
    flow = check("flow_deploy_and_execution", create)

    def persist():
        runner.stop()
        start()
        require(client.api(f"/api/flows/{flow}")["data"]["running"], "El flujo desplegado no se restauró")
        require(client.api("/api/variables")["data"]["variables"]["threshold"] == 25, "Se perdió la variable global")
        require(client.api(f"/api/flows/{flow}/variables")["data"]["variables"]["result"] == 11, "Se perdió la variable del flujo")
        names = client.request("/api/secrets").decode()
        require("VALIDATION" in names and credential not in names, "Listado de secretos incorrecto")
        client.api(f"/api/flows/{flow}/stop", {})
    check("session_and_data_survive_restart", persist)

    def recover():
        client.api("/api/auth/logout", {})
        replacement = secrets.token_urlsafe(24)
        client.api("/api/auth/recover", {"username": "validation", "password": replacement,
                                         "code": runner.code("recovery")})
        client.api("/api/auth/login", {"username": "validation", "password": replacement})
        require(client.api(f"/api/flows/{flow}")["data"]["flow"]["name"] == "Pi validation", "Cuenta no recuperada")
    check("account_recovery", recover)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    source = parser.add_mutually_exclusive_group()
    source.add_argument("--package", type=Path, help="Archivo de release; SHA256SUMS debe estar a su lado")
    source.add_argument("--binary", type=Path, help="Binario local de confianza, sin verificar checksum")
    parser.add_argument("--systemd", action="store_true", help="Usar una unidad temporal como nobody; requiere sudo")
    parser.add_argument("--report", type=Path, default=Path("piworker-validation-" + uuid.uuid4().hex[:8] + ".json"))
    args = parser.parse_args()
    if args.systemd and (platform.system() != "Linux" or os.geteuid() != 0 or not Path("/run/systemd/system").is_dir()):
        parser.error("--systemd requiere Linux con systemd activo y sudo")
    report = {"status": "running", "platform": platform.platform(), "mode": "systemd" if args.systemd else "process",
              "checks": [], "not_tested": ["physical_reboot", "GPIO_I2C_1Wire", "real_installer_update_rollback_uninstall", "browser_rendering"]}
    # Refuse to overwrite an existing report before starting any process.
    with args.report.open("x") as stream:
        json.dump(report, stream)
    work = Path(tempfile.mkdtemp(prefix="piworker-validation-"))
    runner = None
    code = 1

    def check(name, function):
        try:
            result = function()
        except Exception as error:
            report["checks"].append({"name": name, "status": "failed", "error": str(error)})
            raise
        report["checks"].append({"name": name, "status": "passed"})
        print("[OK] " + name, flush=True)
        return result

    def interrupt(_signum, _frame):
        raise KeyboardInterrupt()
    signal.signal(signal.SIGTERM, interrupt)
    try:
        def prepare():
            if args.binary:
                binary = work / "piworker"
                shutil.copyfile(args.binary.resolve(), binary)
                binary.chmod(0o755)
                return binary
            package = args.package or Path(f"piworker-linux-{arch_name(platform.machine(), struct.calcsize('P') * 8)}.tar.gz")
            return extract_package(package.resolve(), work)
        binary = check("binary_preparation" if args.binary else "package_checksum_and_extraction", prepare)
        runner = Runner(binary, work, args.systemd)
        def version():
            value = runner.command([str(binary), "-version"]).strip()
            require(value.startswith("PiWorker "), "No se reconoce la versión del binario")
            report["version"] = value
        check("version", version)
        exercise(runner, check)
        report["status"] = "passed"
        code = 0
    except KeyboardInterrupt:
        report["status"] = "interrupted"
        code = 130
    except Exception as error:
        report["status"] = "failed"
        print("[FALLO] " + str(error), file=sys.stderr)
    finally:
        try:
            if runner:
                runner.stop()
            shutil.rmtree(work)
        except Exception:
            report["status"] = "failed"
            report["cleanup_required"] = str(work)
            code = 1
            print("No se pudo limpiar la prueba: " + str(work), file=sys.stderr)
        args.report.write_text(json.dumps(report, indent=2) + "\n")
    print(f"Resultado: {report['status']}. Informe: {args.report.resolve()}")
    print("Pendiente aparte: reinicio físico, hardware, navegador e instalador real.")
    return code


if __name__ == "__main__":
    sys.exit(main())
