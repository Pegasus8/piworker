"""Validate a local release image using a disposable container and data volume.

Usage: python3 scripts/smoke_container.py IMAGE [--platform linux/arm64]
Requires Docker. Does not publish images or touch an existing PiWorker instance.
The image serves HTTPS on 8443 with a local CA kept in the data volume; the
test trusts only that CA (read from the container) and checks the plain
listener on 8080 too.
"""
import argparse
import http.cookiejar
import json
import re
import ssl
import subprocess
import time
import urllib.request
import uuid

CA_PATH = "/app/data/tls/ca.crt"


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True).strip()


def https_opener(ca_pem, cookies=None):
    """An opener whose HTTPS trusts only ca_pem (no system CAs), without proxies."""
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    if ca_pem:
        context.load_verify_locations(cadata=ca_pem)
    return urllib.request.build_opener(
        urllib.request.ProxyHandler({}),
        urllib.request.HTTPSHandler(context=context),
        urllib.request.HTTPCookieProcessor(cookies if cookies is not None else http.cookiejar.CookieJar()))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("image")
    parser.add_argument("--platform", default="linux/arm64")
    args = parser.parse_args()
    name = "piworker-smoke-" + uuid.uuid4().hex[:12]
    volume = name + "-data"
    # One cookie jar across container recreation: the session must survive.
    cookies = http.cookiejar.CookieJar()
    plain = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    client = None
    base = ""

    def request(path, body=None, method=None):
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(base + path, data=data, method=method,
                                     headers={"Content-Type": "application/json", "X-PiWorker-Request": "1"})
        with client.open(req, timeout=5) as response:
            return response.read()

    def api(path, body=None, method=None):
        return json.loads(request(path, body, method))

    def wait_healthy(fetch):
        for _ in range(60):
            try:
                if json.loads(fetch("/api/health")) == {"status": "healthy"}:
                    return
            except (OSError, ValueError):
                pass
            time.sleep(0.5)
        raise RuntimeError("Container did not become healthy")

    def start(bootstrap):
        nonlocal base, client
        env = ["-e", "PIWORKER_ADMIN_USER=release-test", "-e", "PIWORKER_ADMIN_PASS=local-smoke-password"] if bootstrap else []
        docker("run", "-d", "--platform", args.platform, "--name", name,
               "-p", "127.0.0.1::8443", "-p", "127.0.0.1::8080", "-v", volume + ":/app/data", *env, args.image)
        base = "https://" + docker("port", name, "8443/tcp").splitlines()[0]
        http_base = "http://" + docker("port", name, "8080/tcp").splitlines()[0]

        def plain_get(path):
            with plain.open(http_base + path, timeout=5) as response:
                return response.read()
        # The plain listener answers health without redirect (no CA needed).
        wait_healthy(plain_get)
        # Trust the CA read from the volume, never one downloaded over HTTP.
        ca = docker("exec", name, "cat", CA_PATH)
        assert plain_get("/piworker-ca.crt").decode().strip() == ca, "CA served over HTTP differs from the volume's"
        client = https_opener(ca, cookies)
        wait_healthy(request)

    docker("volume", "create", volume)
    try:
        version = docker("run", "--rm", "--platform", args.platform, args.image, "-version")
        assert version.startswith("PiWorker ") and version != "PiWorker dev", version
        start(True)
        health = json.loads(docker("inspect", "--format", "{{json .Config.Healthcheck}}", name))
        if health and health.get("Test"):
            check = health["Test"]
            if check[0] == "CMD":
                docker("exec", name, *check[1:])
            elif check[0] == "CMD-SHELL":
                docker("exec", name, "sh", "-c", check[1])
        html = request("/").decode()
        assert 'id="app"' in html
        assets = re.findall(r'(?:src|href)="(/assets/[^\"]+)"', html)
        assert assets, "Embedded frontend assets missing"
        for asset in assets:
            assert len(request(asset)) > 100
        api("/api/auth/login", {"username": "release-test", "password": "local-smoke-password"})
        flow = api("/api/flows", {"name": "Release persistence smoke"})["data"]["id"]
        api("/api/variables/threshold", {"value": 25}, "PUT")
        api("/api/secrets/SMOKE", {"value": "test-secret-value"}, "PUT")
        preview = api("/api/nodes/process-transform/test", {"payload": 5, "config": {"expression": 'payload + vars["threshold"]'}})
        assert preview["data"]["outputs"][0]["payload"] == 30
        docker("stop", "--time", "10", name)
        docker("rm", name)
        start(False)
        # The existing session, accounts and data must survive recreation.
        assert api("/api/flows/" + flow)["data"]["flow"]["name"] == "Release persistence smoke"
        assert api("/api/variables")["data"]["variables"]["threshold"] == 25
        secrets = request("/api/secrets").decode()
        assert "SMOKE" in secrets and "test-secret-value" not in secrets
        api("/api/auth/logout", {})
        api("/api/auth/login", {"username": "release-test", "password": "local-smoke-password"})
        print(f"PASS {args.platform}: {version}; HTTPS with the local CA, plain-HTTP health/CA, embedded UI/assets, "
              "login, preview, volume/session/account persistence")
    finally:
        subprocess.run(["docker", "rm", "-f", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run(["docker", "volume", "rm", volume], stdout=subprocess.DEVNULL, check=True)


if __name__ == "__main__":
    main()
