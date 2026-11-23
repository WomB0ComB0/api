import json
import logging
import os
import signal
import socket
import threading
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

_SHARED_POOL = ThreadPoolExecutor(max_workers=10, thread_name_prefix="health-probe")


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


def check_service_health(name: str, url: str, timeout: float = 3.0) -> dict:
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


def check_all_services(services: Dict[str, str], timeout: float = 3.0) -> tuple[int, dict]:
    """Check all configured services concurrently using the shared pool and return (http_status, body)."""
    if not services:
        return 503, {
            "status": "degraded",
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "services": [],
        }

    futures = [_SHARED_POOL.submit(check_service_health, name, url, timeout) for name, url in services.items()]
    results: List[dict] = [f.result() for f in futures]

    all_healthy = all(r["status"] == "healthy" for r in results)
    status_code = 200 if all_healthy else 503
    return status_code, {
        "status": "healthy" if all_healthy else "degraded",
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "services": results,
    }


class _CoalescedHealthCache:
    """Short-TTL coalescing cache to prevent thundering-herd fan-out under probe storms."""

    def __init__(self, ttl_seconds: float = 2.0) -> None:
        self._ttl = ttl_seconds
        self._lock = threading.Lock()
        self._expires_at = 0.0
        self._cached: tuple[int, dict] | None = None

    def get_or_refresh(self, services: Dict[str, str]) -> tuple[int, dict]:
        now = time.monotonic()
        with self._lock:
            if self._cached is not None and now < self._expires_at:
                return self._cached
            result = check_all_services(services)
            self._cached = result
            self._expires_at = time.monotonic() + self._ttl
            return result


def make_handler(services: Dict[str, str], cache_ttl: float = 2.0):
    cache = _CoalescedHealthCache(ttl_seconds=cache_ttl)

    class HealthHandler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def do_HEAD(self) -> None:  # noqa: N802
            self._handle(head_only=True)

        def do_GET(self) -> None:  # noqa: N802
            self._handle(head_only=False)

        def _handle(self, head_only: bool) -> None:
            if self.path == "/healthz":
                self._send_json(
                    200,
                    {
                        "status": "healthy",
                        "service": "health-aggregator",
                        "timestamp": datetime.now(timezone.utc).isoformat(),
                    },
                    head_only=head_only,
                )
                return

            if self.path == "/health":
                status_code, payload = cache.get_or_refresh(services)
                self._send_json(status_code, payload, head_only=head_only)
                return

            self._send_json(404, {"error": "Not found"}, head_only=head_only)

        def _send_json(self, status: int, payload: dict, head_only: bool = False) -> None:
            body = json.dumps(payload).encode("utf-8")
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            if not head_only:
                self.wfile.write(body)

        def log_message(self, format: str, *args) -> None:  # noqa: A002
            logger.info("%s - %s", self.address_string(), format % args)

    return HealthHandler


class _DualStackHTTPServer(ThreadingHTTPServer):
    address_family = socket.AF_INET6
    allow_reuse_address = True
    daemon_threads = True

    def server_bind(self) -> None:
        self.socket.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 0)
        super().server_bind()


class _IPv4HTTPServer(ThreadingHTTPServer):
    allow_reuse_address = True
    daemon_threads = True


def create_server(
    host: str = "::",
    port: int = 8000,
    services: Dict[str, str] | None = None,
    cache_ttl: float | None = None,
) -> ThreadingHTTPServer:
    resolved = services if services is not None else parse_services(os.getenv("SERVICES"))
    ttl = cache_ttl if cache_ttl is not None else float(os.getenv("HEALTH_CACHE_TTL_SECONDS", "2.0"))
    handler = make_handler(resolved, cache_ttl=ttl)
    if ":" in host:
        return _DualStackHTTPServer((host, port), handler)
    return _IPv4HTTPServer((host, port), handler)


if __name__ == "__main__":
    port = int(os.getenv("PORT", "8000"))
    host = os.getenv("HOST", "::")
    services = parse_services(os.getenv("SERVICES"))
    server = create_server(host, port, services)

    def _shutdown(signum, _frame):
        logger.info("Received signal %s, shutting down health aggregator...", signum)
        threading.Thread(target=server.shutdown, daemon=True).start()

    signal.signal(signal.SIGTERM, _shutdown)
    signal.signal(signal.SIGINT, _shutdown)

    logger.info("Health aggregator listening on [%s]:%s checking %d services", host, port, len(services))
    try:
        server.serve_forever()
    finally:
        server.server_close()
        _SHARED_POOL.shutdown(wait=False)
