#!/usr/bin/env python3
"""Opt-in real systemd installation test on a clean Raspberry Pi. Python 3.9+."""
import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import platform
import pwd
import re
import secrets
import shutil
import socket
import subprocess
import sys
import tarfile
import tempfile

from validate_pi import Client, arch_name, extract_package, require, wait_for

ROOT = Path('/var/lib/piworker-release-test')
CONFIG = Path('/etc/piworker')
DATA = Path('/var/lib/piworker')
BINARY = Path('/usr/local/bin/piworker')
MANAGER = Path('/usr/local/bin/piworkerctl')
UNIT = Path('/etc/systemd/system/piworker.service')
REPORT = {'checks': [], 'not_tested': ['GitHub release download', 'GPIO/I2C physical operation',
                                      'interactive browser workflow', 'power-loss recovery']}
OWN_STATE = False
ENV = {k: v for k, v in os.environ.items() if not k.startswith('PIWORKER_')}
ENV['PATH'] = '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin'


def require_absent(paths):
    for path in paths:
        require(not path.exists() and not path.is_symlink(), f'Ruta existente: {path}. Usá una Pi sin instalación previa.')


def private_file(path, owner=0):
    for entry, mode in ((path.parent, 0o700), (path, 0o600)):
        info = entry.lstat()
        require(not entry.is_symlink() and info.st_uid == owner and info.st_mode & 0o777 == mode,
                f'Estado inseguro: {entry}')
    require(path.is_file(), 'El estado no es un archivo regular')


def require_reboot(state, boot, machine):
    require(machine == state['machine'], 'El estado pertenece a otra máquina')
    require(boot != state['boot'], 'Todavía no reiniciaste la Pi; ejecutá sudo reboot antes de resume')


def identity():
    return (Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
            Path('/etc/machine-id').read_text().strip())


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(args, ok=True, env=None, **kwargs):
    result = subprocess.run([str(x) for x in args], capture_output=True, text=True,
                            timeout=180, env=env or ENV, **kwargs)
    require(not ok or result.returncode == 0, f'Falló {Path(str(args[0])).name} (código {result.returncode})')
    return result


def as_service(args, **kwargs):
    account = pwd.getpwnam('piworker')
    return run(args, user=account.pw_uid, group=account.pw_gid,
               extra_groups=os.getgrouplist('piworker', account.pw_gid), **kwargs)


def check(name, fn):
    try:
        result = fn()
    except Exception:
        REPORT['checks'].append({'name': name, 'status': 'failed'})
        raise
    REPORT['checks'].append({'name': name, 'status': 'passed'})
    print('[OK]', name, flush=True)
    return result


def save(state):
    path = ROOT / 'state.json'
    temp = ROOT / 'state.tmp'
    temp.write_text(json.dumps(state))
    temp.chmod(0o600)
    temp.replace(path)


def load_state():
    private_file(ROOT / 'state.json')
    return json.loads((ROOT / 'state.json').read_text())


def ctl(action, release=None, health=None, ok=True):
    env = dict(ENV)
    args = ['bash', ROOT / 'piworkerctl', action]
    if release:
        env['PIWORKER_RELEASE_DIR'] = str(ROOT / release['directory'])
        args.append(release['version'])
    if health:
        env['PIWORKER_HEALTH_URL'] = health
    result = run(args, ok=False, env=env)
    with (ROOT / 'installer.log').open('a') as log:
        log.write(f'\n{action}: exit {result.returncode}\n' + result.stdout + result.stderr)
    require(not ok or result.returncode == 0, f'Falló piworkerctl {action}; revisá {ROOT}/installer.log')
    return result


def healthy(client):
    wait_for(lambda: client.api('/api/health') == {'status': 'healthy'})


def snapshot():
    return {str(p): digest(p) for p in (BINARY, MANAGER, UNIT, CONFIG / 'piworker.toml', CONFIG / 'installed-version')}


def verify_snapshot(state):
    require(snapshot() == state['files'], 'La instalación cambió fuera de la prueba; se dejó intacta')


def persistence(client, state):
    healthy(client)
    require(client.api('/api/variables')['data']['variables']['release_test'] == state['token'], 'Se perdió la sesión o la variable')
    flow = state['flow']
    require(client.api(f'/api/flows/{flow}')['data']['running'], 'El flujo no arrancó automáticamente')
    value = secrets.randbelow(1000000000)
    client.api(f'/api/flows/{flow}/inject/manual', {'payload': value})
    wait_for(lambda: client.api(f'/api/flows/{flow}/variables')['data']['variables'].get('result') == value)


def package(source, name):
    destination = ROOT / name
    destination.mkdir()
    arch = arch_name(platform.machine(), 64 if sys.maxsize > 2**32 else 32)
    archive = source / f'piworker-linux-{arch}.tar.gz'
    for path in (archive, source / 'SHA256SUMS'):
        shutil.copyfile(path, destination / path.name)
    binary = extract_package(destination / archive.name, destination)
    version = run([binary, '-version']).stdout.strip()
    # The CLI prints the version alone or prefixed with the product name.
    match = re.search(r'\bv[0-9][a-zA-Z0-9.+-]*$', version)
    require(match is not None, 'El binario debe tener una versión vVERSION')
    with tarfile.open(destination / archive.name) as tar:
        member = tar.getmember('piworkerctl')
        require(member.isfile(), 'Falta el instalador regular en el paquete')
        manager = tar.extractfile(member).read()
    require(b'PIWORKER_RELEASE_DIR' in manager, 'Paquete con instalador viejo; usá el nuevo kit')
    return {'directory': name, 'version': match[0], 'hash': digest(binary)}, manager


def sensor_path(value):
    if not re.fullmatch(r'/sys/bus/w1/devices/28-[A-Za-z0-9]+/w1_slave', value):
        raise ValueError('Usá /sys/bus/w1/devices/28-.../w1_slave')
    return Path(value)


def optional_checks(args):
    for device in args.device:
        require(re.fullmatch(r'/dev/(gpiochip[0-9]+|i2c-[0-9]+|spidev[0-9]+\.[0-9]+)', device), 'Dispositivo no admitido')
        check('service_permissions:' + device, lambda d=device: as_service(
            ['/bin/sh', '-c', 'test -r "$1" && test -w "$1"', 'check', d]))
    for sensor in args.sensor:
        def read_sensor(s=sensor):
            text = as_service(['/bin/cat', str(s)]).stdout
            require(re.search(r'YES\s*\n', text) and re.search(r't=-?\d+', text), 'Sensor sin CRC válido o temperatura')
        check('sensor_read:' + str(sensor), read_sensor)
    if not args.device and not args.sensor:
        REPORT['not_tested'].append('hardware: no devices/sensors selected')
    if args.browser:
        executable = shutil.which('chromium') or shutil.which('chromium-browser')
        require(executable, 'Instalá chromium para usar --browser')
        def browser():
            account = pwd.getpwnam('piworker')
            with tempfile.TemporaryDirectory(prefix='piworker-browser-') as temp:
                os.chown(temp, account.pw_uid, account.pw_gid)
                shot = Path(temp) / 'login.png'
                result = as_service([executable, '--headless', '--disable-gpu', '--no-first-run',
                                     '--user-data-dir=' + temp, '--virtual-time-budget=10000',
                                     '--screenshot=' + str(shot), '--dump-dom', 'http://127.0.0.1:8080/'], cwd=temp)
                require('Sign in to continue' in result.stdout and shot.is_file(), 'No se renderizó el login')
                shutil.copyfile(shot, ROOT / 'login.png')
        check('chromium_login_render', browser)
    else:
        REPORT['not_tested'].append('Chromium rendering: --browser not selected')


def prepare(args):
    global OWN_STATE
    for dependency in ('systemctl', 'curl', 'tar', 'bash', 'useradd'):
        require(shutil.which(dependency, path=ENV['PATH']), f'Falta instalar {dependency}')
    if args.browser:
        require(shutil.which('chromium', path=ENV['PATH']) or shutil.which('chromium-browser', path=ENV['PATH']),
                'Instalá chromium antes de usar --browser')
    require_absent([ROOT, CONFIG, DATA, BINARY, MANAGER, UNIT,
                    Path(str(UNIT) + '.d'), Path('/var/backups/piworker')])
    require(run(['systemctl', 'show', 'piworker.service', '-p', 'LoadState', '--value'], ok=False).stdout.strip() == 'not-found',
            'Existe una unidad piworker.service')
    with socket.socket() as probe:
        probe.bind(('0.0.0.0', 8080))
    ROOT.mkdir(mode=0o700)
    OWN_STATE = True
    boot, machine = identity()
    state = {'boot': boot, 'machine': machine, 'phase': 'preparing', 'token': secrets.token_hex(16)}
    save(state)
    old, manager = check('initial_package', lambda: package(args.initial, 'initial'))
    new, _ = check('update_package', lambda: package(args.update, 'update'))
    require(old['hash'] != new['hash'], 'La prueba requiere dos binarios distintos')
    (ROOT / 'piworkerctl').write_bytes(manager)
    check('real_install', lambda: ctl('install', old))
    client = Client('http://127.0.0.1:8080')
    check('service_health', lambda: healthy(client))
    password = secrets.token_urlsafe(24)
    output = as_service([BINARY, '-config', CONFIG / 'piworker.toml', '-db', DATA / 'piworker.db', '-setup-code']).stdout
    match = re.search(r'setup code .*: ([A-Za-z0-9_-]{43})', output)
    require(match, 'No se generó el código de setup')
    client.api('/api/auth/setup', {'username': 'validation', 'password': password, 'code': match[1]})
    client.api('/api/auth/login', {'username': 'validation', 'password': password})
    jar = http.cookiejar.LWPCookieJar(str(ROOT / 'cookies'))
    for cookie in client.cookies:
        jar.set_cookie(cookie)
    jar.save(ignore_discard=True, ignore_expires=True)
    client.api('/api/variables/release_test', {'value': state['token']}, 'PUT')
    flow = {'name': 'Release lifecycle validation', 'nodes': [
        {'id': 'manual', 'type': 'trigger-manual', 'category': 'input', 'enabled': True,
         'config': {'payload': '11'}, 'position': {'x': 0, 'y': 0}},
        {'id': 'set', 'type': 'set-var', 'category': 'processing', 'enabled': True,
         'config': {'scope': 'flow', 'key': 'result', 'value': 'payload.data'}, 'position': {'x': 250, 'y': 0}}],
        'connections': [{'id': 'edge', 'sourceNode': 'manual', 'sourcePort': 'output', 'targetNode': 'set', 'targetPort': 'input'}]}
    state['flow'] = client.api('/api/flows', flow)['data']['id']
    client.api('/api/flows/' + state['flow'] + '/deploy', {})
    check('seed_data_and_flow', lambda: persistence(client, state))
    before = snapshot()
    def rollback():
        with socket.socket() as unavailable:
            unavailable.bind(('127.0.0.1', 0))
            result = ctl('update', new, health=f'http://127.0.0.1:{unavailable.getsockname()[1]}/api/health', ok=False)
        require(result.returncode != 0, 'La actualización fallida no fue rechazada')
        require(snapshot() == before, 'Rollback no restauró los archivos originales')
        persistence(client, state)
    check('failed_update_rollback', rollback)
    check('real_update', lambda: ctl('update', new))
    require(digest(BINARY) == new['hash'], 'No se instaló el binario nuevo')
    require(digest(CONFIG / 'piworker.toml') == before[str(CONFIG / 'piworker.toml')], 'Cambió la configuración')
    check('update_preserves_session_and_data', lambda: persistence(client, state))
    check('uninstall', uninstall)
    require(DATA.exists() and (CONFIG / 'piworker.toml').exists(), 'Uninstall eliminó los datos')
    check('reinstall', lambda: ctl('install', new))
    check('reinstall_preserves_session_and_data', lambda: persistence(client, state))
    check('service_enabled', lambda: run(['systemctl', 'is-enabled', '--quiet', 'piworker.service']))
    optional_checks(args)
    state.update(phase='awaiting_reboot', files=snapshot(), checks=REPORT['checks'], not_tested=REPORT['not_tested'])
    save(state)
    print('Preparación lista. Ejecutá sudo reboot y luego sudo python3 validate_installation.py resume', flush=True)


def uninstall():
    ctl('uninstall')
    require_absent([BINARY, MANAGER, UNIT])
    require(run(['systemctl', 'is-active', '--quiet', 'piworker.service'], ok=False).returncode != 0,
            'El servicio sigue activo')


def resume():
    global OWN_STATE
    state = load_state()
    OWN_STATE = True
    require_reboot(state, *identity())
    require(state['phase'] == 'awaiting_reboot', 'No hay una prueba pendiente de reinicio')
    REPORT['checks'] = state['checks']
    REPORT['not_tested'] = state['not_tested']
    check('installation_unchanged', lambda: verify_snapshot(state))
    check('boot_started_service', lambda: run(['systemctl', 'is-active', '--quiet', 'piworker.service']))
    check('boot_service_enabled', lambda: run(['systemctl', 'is-enabled', '--quiet', 'piworker.service']))
    client = Client('http://127.0.0.1:8080')
    private_file(ROOT / 'cookies')
    jar = http.cookiejar.LWPCookieJar(str(ROOT / 'cookies'))
    jar.load(ignore_discard=True, ignore_expires=True)
    for cookie in jar:
        client.cookies.set_cookie(cookie)
    check('boot_session_data_and_flow', lambda: persistence(client, state))
    check('final_uninstall', uninstall)
    require(DATA.exists() and CONFIG.exists(), 'Se perdieron datos al desinstalar')
    state['phase'] = 'complete'
    save(state)
    print('Prueba completa; servicio desinstalado. Datos, configuración y backups conservados para inspección.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    start = commands.add_parser('prepare', help='Instala realmente; requiere una Pi sin instalación previa')
    start.add_argument('--initial', type=Path, default=Path(__file__).resolve().parent / 'initial')
    start.add_argument('--update', type=Path, default=Path(__file__).resolve().parent / 'update')
    start.add_argument('--browser', action='store_true', help='Prueba Chromium instalado, sin instalar dependencias')
    start.add_argument('--device', action='append', default=[], help='Verifica permisos del servicio; no acciona hardware')
    start.add_argument('--sensor', action='append', type=sensor_path, default=[], help='Lee un sensor 1-Wire explícito')
    commands.add_parser('resume', help='Verifica arranque tras reboot y desinstala conservando datos')
    args = parser.parse_args()
    require(platform.system() == 'Linux' and os.geteuid() == 0, 'Ejecutá con sudo en Linux/systemd')
    os.umask(0o077)
    try:
        prepare(args) if args.command == 'prepare' else resume()
        REPORT['status'] = 'awaiting_reboot' if args.command == 'prepare' else 'passed'
    except Exception as exc:
        REPORT.update(status='failed', error=str(exc))
        print('[FAIL]', exc, file=sys.stderr)
    # Do not overwrite anything in an existing/untrusted state directory on preflight failure.
    if OWN_STATE and ROOT.is_dir() and not ROOT.is_symlink() and ROOT.stat().st_uid == 0 and ROOT.stat().st_mode & 0o777 == 0o700:
        report = ROOT / (args.command + '-report.json')
        if not report.is_symlink():
            report.write_text(json.dumps(REPORT, indent=2) + '\n')
            print('Informe (sin credenciales):', report)
    return 1 if REPORT['status'] == 'failed' else 0


if __name__ == '__main__':
    sys.exit(main())
