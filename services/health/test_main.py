import json
import threading
import unittest
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, HTTPServer

from main import check_all_services, create_server, parse_services


class _StubHandler(BaseHTTPRequestHandler):
    def do_GET(self):  # noqa: N802
        if self.path == "/ok":
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b'{"status":"healthy"}')
            return
        self.send_response(503)
        self.end_headers()

    def log_message(self, format, *args):  # noqa: A002
        pass


class HealthAggregatorTests(unittest.TestCase):
    def test_parse_services_formats(self):
        defaults = parse_services(None)
        self.assertEqual(defaults["core"], "http://core-service:3000/v1/core/health")
        self.assertEqual(defaults["media"], "http://media-service:8080/v1/media/health")
        self.assertEqual(defaults["hasura"], "http://hasura:8080/healthz")

        explicit = parse_services("a=http://a:3000/ok, b=http://b:8080/healthz")
        self.assertEqual(explicit, {"a": "http://a:3000/ok", "b": "http://b:8080/healthz"})

        legacy = parse_services("svc:9000")
        self.assertEqual(legacy, {"svc": "http://svc:9000/health"})

    def test_live_aggregation_healthy_and_degraded(self):
        upstream = HTTPServer(("127.0.0.1", 0), _StubHandler)
        up_port = upstream.server_address[1]
        up_thread = threading.Thread(target=upstream.serve_forever, daemon=True)
        up_thread.start()

        try:
            code, body = check_all_services({"svc1": f"http://127.0.0.1:{up_port}/ok"})
            self.assertEqual(code, 200)
            self.assertEqual(body["status"], "healthy")
            self.assertEqual(body["services"][0]["status"], "healthy")

            code, body = check_all_services(
                {
                    "ok_svc": f"http://127.0.0.1:{up_port}/ok",
                    "bad_svc": f"http://127.0.0.1:{up_port}/fail",
                }
            )
            self.assertEqual(code, 503)
            self.assertEqual(body["status"], "degraded")

            agg = create_server("127.0.0.1", 0, {"ok_svc": f"http://127.0.0.1:{up_port}/ok"})
            agg_port = agg.server_address[1]
            agg_thread = threading.Thread(target=agg.serve_forever, daemon=True)
            agg_thread.start()
            try:
                with urllib.request.urlopen(f"http://127.0.0.1:{agg_port}/health") as resp:
                    self.assertEqual(resp.status, 200)
                    data = json.loads(resp.read().decode("utf-8"))
                    self.assertEqual(data["status"], "healthy")

                with urllib.request.urlopen(f"http://127.0.0.1:{agg_port}/healthz") as resp:
                    self.assertEqual(resp.status, 200)
            finally:
                agg.shutdown()
                agg.server_close()
        finally:
            upstream.shutdown()
            upstream.server_close()


if __name__ == "__main__":
    unittest.main()
