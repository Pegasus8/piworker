"""The validation scripts talk HTTPS to PiWorker and trust only its local CA."""
import base64
import hashlib
import http.server
import json
from pathlib import Path
import shutil
import ssl
import subprocess
import tempfile
import threading
import unittest
import urllib.error

import smoke_container
import validate_installation
import validate_pi


def openssl(*args, cwd):
    subprocess.run(["openssl", *args], cwd=cwd, check=True, capture_output=True)


@unittest.skipUnless(shutil.which("openssl"), "openssl is required to build a test CA")
class LocalCATests(unittest.TestCase):
    """A server certificate for 127.0.0.1 signed by a throwaway CA."""

    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        d = Path(cls.temp.name)
        # Like the CA PiWorker generates (Python 3.13 verifies strictly: key
        # usage on the CA, authority key identifier on the leaf).
        cls.make_ca(d, "ca")
        openssl("req", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes",
                "-keyout", "server.key", "-out", "server.csr", "-subj", "/CN=localhost", cwd=d)
        (d / "san.ext").write_text("subjectAltName=IP:127.0.0.1,DNS:localhost\n"
                                   "keyUsage=critical,digitalSignature\nextendedKeyUsage=serverAuth\n"
                                   "authorityKeyIdentifier=keyid\nsubjectKeyIdentifier=hash\n")
        openssl("x509", "-req", "-in", "server.csr", "-CA", "ca.crt", "-CAkey", "ca.key", "-CAcreateserial",
                "-out", "server.crt", "-days", "2", "-extfile", "san.ext", cwd=d)
        cls.ca = d / "ca.crt"
        cls.server_crt = d / "server.crt"

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                body = json.dumps({"status": "healthy"}).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *args):
                pass

        class Server(http.server.ThreadingHTTPServer):
            def handle_error(self, request, client_address):
                pass  # clients that reject the certificate abort the handshake

        cls.httpd = Server(("127.0.0.1", 0), Handler)
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        ctx.load_cert_chain(d / "server.crt", d / "server.key")
        cls.httpd.socket = ctx.wrap_socket(cls.httpd.socket, server_side=True)
        cls.port = cls.httpd.server_address[1]
        cls.thread = threading.Thread(target=cls.httpd.serve_forever, daemon=True)
        cls.thread.start()

    @staticmethod
    def make_ca(directory, name):
        openssl("req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes",
                "-keyout", name + ".key", "-out", name + ".crt", "-days", "2", "-subj", "/CN=Test " + name,
                "-addext", "basicConstraints=critical,CA:TRUE,pathlen:0",
                "-addext", "keyUsage=critical,keyCertSign,cRLSign,digitalSignature", cwd=directory)

    @classmethod
    def tearDownClass(cls):
        cls.httpd.shutdown()
        cls.httpd.server_close()
        cls.temp.cleanup()

    def base(self):
        return f"https://127.0.0.1:{self.port}"

    def test_client_trusts_only_the_given_ca(self):
        client = validate_pi.Client(self.base(), cafile=self.ca)
        self.assertEqual(client.api("/api/health"), {"status": "healthy"})
        with self.assertRaises(urllib.error.URLError):
            validate_pi.Client(self.base()).api("/api/health")

    def test_smoke_opener_uses_the_container_ca(self):
        opener = smoke_container.https_opener(self.ca.read_text())
        with opener.open(self.base() + "/api/health", timeout=5) as response:
            self.assertEqual(json.loads(response.read()), {"status": "healthy"})
        with self.assertRaises(urllib.error.URLError):
            smoke_container.https_opener("").open(self.base() + "/api/health", timeout=5)

    def test_spki_pin_matches_openssl(self):
        pubkey = subprocess.run(["openssl", "x509", "-in", str(self.server_crt), "-noout", "-pubkey"],
                                check=True, capture_output=True).stdout
        der = subprocess.run(["openssl", "pkey", "-pubin", "-outform", "der"], input=pubkey,
                             check=True, capture_output=True).stdout
        expected = base64.b64encode(hashlib.sha256(der).digest()).decode()
        cert_der = ssl.PEM_cert_to_DER_cert(self.server_crt.read_text())
        self.assertEqual(validate_installation.spki_pin(cert_der), expected)

    def test_served_pin_is_verified_against_the_ca(self):
        cert_der = ssl.PEM_cert_to_DER_cert(self.server_crt.read_text())
        self.assertEqual(validate_installation.served_spki_pin("127.0.0.1", self.port, self.ca),
                         validate_installation.spki_pin(cert_der))
        self.make_ca(self.temp.name, "other-ca")
        other = Path(self.temp.name) / "other-ca.crt"
        with self.assertRaises(ssl.SSLError):
            validate_installation.served_spki_pin("127.0.0.1", self.port, other)


class InstallationEndpointsTest(unittest.TestCase):
    def test_installation_uses_https_with_the_local_ca(self):
        self.assertEqual(validate_installation.HTTPS_BASE, "https://127.0.0.1:8443")
        self.assertEqual(validate_installation.HTTP_BASE, "http://127.0.0.1:8080")
        self.assertEqual(validate_installation.CA_FILE, validate_installation.DATA / "tls" / "ca.crt")
        self.assertEqual(validate_installation.PORTS, (8443, 8080))


if __name__ == "__main__":
    unittest.main()
