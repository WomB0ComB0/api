import json
import logging
import os
import socket
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Dict, List

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
logger = logging.getLogger(__name__)

DEFAULT_SERVICES = (
    "core=http://core-service:3000/v1/core/health,"
    "media=http://media-service:8080/v1/media/health,"
    "hasura=http://hasura:8080/healthz"
)


def parse_services(raw: str | None) -> Dict[str, str]:
    """Parse SERVICES env var supporting 'name=http://host:port/path' or legacy 'host:port'."""
    services: Dict[str, str] = {}
    value = (raw or "").strip() or DEFAULT_SERVICES

    for entry in value.split(","):
        item = entry.strip()
        if not item:
            continue
        if "=" in item:
            name, url = item.split("=", 1)
            services[name.strip()] = url.strip()
        elif ":" in item:
            name = item.split(":", 1)[0].strip()
            services[name] = f"http://{item}/health"
    return services


def check_service_health(name: str, url: str, timeout: float = 5.0) -> dict:
    """Check health of a single upstream service."""
    start = time.monotonic()
    req = urllib.request.Request(url, method="GET", headers={"User-Agent": "api-health-aggregator/1.0"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as response:
            status_code = response.getcode()
            latency_ms = round((time.monotonic() - start) * 1000, 1)
            if status_code == 200:
                return {
                    "service": name,
                    "status": "healthy",
                    "url": url,
                    "latency_ms": latency_ms,
                }
            return {
                "service": name,
                "status": "unhealthy",
                "url": url,
                "latency_ms": latency_ms,
                "error": f"HTTP {status_code}",
            }
    except urllib.error.HTTPError as exc:
        code = exc.code
        exc.close()
        latency_ms = round((time.monotonic() - start) * 1000, 1)
        return {
            "service": name,
            "status": "unhealthy",
            "url": url,
            "latency_ms": latency_ms,
            "error": f"HTTP {code}",
        }
    except Exception as exc:
        latency_ms = round((time.monotonic() - start) * 1000, 1)
        return {
            "service": name,
            "status": "unhealthy",
            "url": url,
            "latency_ms": latency_ms,
            "error": str(exc),
        }


def check_all_services(services: Dict[str, str], timeout: float = 5.0) -> tuple[int, dict]:
    """Check all configured services concurrently and return (http_status, body)."""
    if not services:
        return 503, {
            "status": "degraded",
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "services": [],
        }

    with ThreadPoolExecutor(max_workers=min(len(services), 10)) as pool:
        futures = [pool.submit(check_service_health, name, url, timeout) for name, url in services.items()]
        results: List[dict] = [f.result() for f in futures]

    all_healthy = all(r["status"] == "healthy" for r in results)
    status_code = 200 if all_healthy else 503
    return status_code, {
        "status": "healthy" if all_healthy else "degraded",
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "services": results,
    }


def make_handler(services: Dict[str, str]):
    class HealthHandler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:  # noqa: N802
            if self.path == "/healthz":
                self._send_json(
                    200,
                    {
                        "status": "healthy",
                        "service": "health-aggregator",
                        "timestamp": datetime.now(timezone.utc).isoformat(),
                    },
                )
                return

            if self.path == "/health":
                status_code, payload = check_all_services(services)
                self._send_json(status_code, payload)
                return

            self._send_json(404, {"error": "Not found"})

        def _send_json(self, status: int, payload: dict) -> None:
            body = json.dumps(payload).encode("utf-8")
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, format: str, *args) -> None:  # noqa: A002
            logger.info("%s - %s", self.address_string(), format % args)

    return HealthHandler


class _DualStackHTTPServer(ThreadingHTTPServer):
    address_family = socket.AF_INET6

    def server_bind(self) -> None:
        self.socket.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 0)
        super().server_bind()


def create_server(host: str = "::", port: int = 8000, services: Dict[str, str] | None = None) -> ThreadingHTTPServer:
    resolved = services if services is not None else parse_services(os.getenv("SERVICES"))
    if ":" in host:
        return _DualStackHTTPServer((host, port), make_handler(resolved))
    return ThreadingHTTPServer((host, port), make_handler(resolved))


if __name__ == "__main__":
    port = int(os.getenv("PORT", "8000"))
    host = os.getenv("HOST", "::")
    services = parse_services(os.getenv("SERVICES"))
    logger.info("Health aggregator starting on [%s]:%d with services: %s", host, port, list(services.keys()))
    try:
        server = create_server(host=host, port=port, services=services)
    except OSError:
        server = create_server(host="0.0.0.0", port=port, services=services)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
