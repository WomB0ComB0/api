import gc
import http.client
import json
import logging
import os
import signal
import socket
import ssl
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from types import FrameType
from typing import ClassVar, Final, Literal, NotRequired, TypedDict
from urllib.parse import urlsplit

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
logger: Final[logging.Logger] = logging.getLogger(__name__)

DEFAULT_SERVICES: Final[str] = (
    "core=http://core-service:3000/v1/core/health,"
    "media=http://media-service:8080/v1/media/health,"
    "hasura=http://hasura:8080/healthz"
)

_PROBE_HEADERS: Final[dict[str, str]] = {
    "User-Agent": "api-health-aggregator/2.0",
    "Connection": "keep-alive",
}
_SSL_CONTEXT: Final[ssl.SSLContext] = ssl.create_default_context()
_JSON_SEPARATORS: Final[tuple[str, str]] = (",", ":")
_SHARED_POOL: Final[ThreadPoolExecutor] = ThreadPoolExecutor(
    max_workers=10, thread_name_prefix="health-probe"
)

_MAX_PROBE_BODY_BYTES: Final[int] = 65_536

# Pre-encoded static wire frames for zero-allocation error / HEAD responses
_NOT_FOUND_BODY: Final[bytes] = b'{"error":"Not found"}'
_NOT_FOUND_HEAD: Final[bytes] = (
    b"HTTP/1.1 404 Not Found\r\n"
    b"Content-Type: application/json\r\n"
    b"Content-Length: 21\r\n"
    b"Cache-Control: no-store\r\n"
    b"X-Content-Type-Options: nosniff\r\n"
    b"Connection: keep-alive\r\n\r\n"
)
_NOT_FOUND_HEAD_CLOSE: Final[bytes] = (
    b"HTTP/1.1 404 Not Found\r\n"
    b"Content-Type: application/json\r\n"
    b"Content-Length: 21\r\n"
    b"Cache-Control: no-store\r\n"
    b"X-Content-Type-Options: nosniff\r\n"
    b"Connection: close\r\n\r\n"
)
_NOT_FOUND_GET: Final[bytes] = _NOT_FOUND_HEAD + _NOT_FOUND_BODY
_NOT_FOUND_GET_CLOSE: Final[bytes] = _NOT_FOUND_HEAD_CLOSE + _NOT_FOUND_BODY


class ServiceHealthResult(TypedDict):
    service: str
    status: Literal["healthy", "unhealthy"]
    url: str
    latency_ms: float
    error: NotRequired[str]


class AggregatedHealthPayload(TypedDict):
    status: Literal["healthy", "degraded"]
    timestamp: str
    services: list[ServiceHealthResult]


def _build_wire_frames(status: int, body: bytes, keep_alive: bool = True) -> tuple[bytes, bytes]:
    """Build contiguous HTTP/1.1 (head_frame, get_frame) buffers for single-syscall writes."""
    reason = "OK" if status == 200 else ("Service Unavailable" if status == 503 else "Error")
    conn = "keep-alive" if keep_alive else "close"
    head = (
        f"HTTP/1.1 {status} {reason}\r\n"
        f"Content-Type: application/json\r\n"
        f"Content-Length: {len(body)}\r\n"
        f"Cache-Control: no-store\r\n"
        f"X-Content-Type-Options: nosniff\r\n"
        f"Connection: {conn}\r\n\r\n"
    ).encode("ascii")
    return head, head + body


def _sanitize_probe_error(exc: Exception) -> str:
    """Return a non-sensitive error category for public /health responses (log details server-side)."""
    if isinstance(exc, (TimeoutError, socket.timeout)):
        return "probe timed out"
    if isinstance(exc, ConnectionRefusedError):
        return "connection refused"
    if isinstance(exc, ssl.SSLError):
        return "tls handshake failed"
    return "upstream unreachable"


class _UpstreamTarget:
    """Slotted, pre-parsed upstream endpoint avoiding per-probe URL parsing & dict overhead."""

    __slots__ = ("name", "url", "scheme", "host", "port", "path", "_lock", "_conn")

    name: str
    url: str
    scheme: str
    host: str
    port: int
    path: str
    _lock: threading.Lock
    _conn: http.client.HTTPConnection | None

    def __init__(self, name: str, url: str) -> None:
        parsed = urlsplit(url)
        self.name = name
        self.url = url
        self.scheme = parsed.scheme or "http"
        self.host = parsed.hostname or "127.0.0.1"
        self.port = parsed.port or (443 if self.scheme == "https" else 80)
        self.path = parsed.path or "/"
        if parsed.query:
            self.path = f"{self.path}?{parsed.query}"
        self._lock = threading.Lock()
        self._conn = None

    def _connect(self, timeout: float) -> http.client.HTTPConnection:
        conn: http.client.HTTPConnection
        if self.scheme == "https":
            conn = http.client.HTTPSConnection(
                self.host, self.port, timeout=timeout, context=_SSL_CONTEXT
            )
        else:
            conn = http.client.HTTPConnection(self.host, self.port, timeout=timeout)
        conn.connect()
        if conn.sock is not None:
            # Eliminate Nagle 40ms delayed-ACK stalls on upstream probe sockets
            conn.sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        return conn

    def _close_conn(self) -> None:
        if self._conn is not None:
            try:
                self._conn.close()
            except Exception:
                pass
            self._conn = None

    def probe(self, timeout: float = 3.0) -> ServiceHealthResult:
        start = time.monotonic()
        with self._lock:
            for attempt in range(2):
                reused_conn = self._conn is not None
                try:
                    if not reused_conn:
                        self._conn = self._connect(timeout)
                    else:
                        assert self._conn is not None
                        self._conn.timeout = timeout
                    self._conn.request("GET", self.path, headers=_PROBE_HEADERS)
                    resp = self._conn.getresponse()
                    status_code = resp.status
                    # Bounded drain so a misbehaving upstream cannot OOM the health aggregator
                    drained = resp.read(_MAX_PROBE_BODY_BYTES + 1)
                    if resp.will_close or len(drained) > _MAX_PROBE_BODY_BYTES:
                        self._close_conn()
                    latency_ms = round((time.monotonic() - start) * 1000.0, 1)
                    if status_code == 200:
                        return {
                            "service": self.name,
                            "status": "healthy",
                            "url": self.url,
                            "latency_ms": latency_ms,
                        }
                    return {
                        "service": self.name,
                        "status": "unhealthy",
                        "url": self.url,
                        "latency_ms": latency_ms,
                        "error": f"HTTP {status_code}",
                    }
                except (http.client.HTTPException, OSError) as exc:
                    # Only retry once when an already-open keep-alive socket went stale
                    self._close_conn()
                    if attempt == 0 and reused_conn:
                        continue
                    logger.warning("Health probe failed for %s: %s", self.name, exc)
                    latency_ms = round((time.monotonic() - start) * 1000.0, 1)
                    return {
                        "service": self.name,
                        "status": "unhealthy",
                        "url": self.url,
                        "latency_ms": latency_ms,
                        "error": _sanitize_probe_error(exc),
                    }
        latency_ms = round((time.monotonic() - start) * 1000.0, 1)
        return {
            "service": self.name,
            "status": "unhealthy",
            "url": self.url,
            "latency_ms": latency_ms,
            "error": "probe failed",
        }

    def close(self) -> None:
        with self._lock:
            self._close_conn()


class _CachedSnapshot:
    """Immutable pre-serialized wire snapshot; atomic reference swap eliminates reader locks."""

    __slots__ = (
        "status",
        "payload",
        "head_frame",
        "get_frame",
        "head_frame_close",
        "get_frame_close",
        "expires_at",
    )

    status: int
    payload: AggregatedHealthPayload
    head_frame: bytes
    get_frame: bytes
    head_frame_close: bytes
    get_frame_close: bytes
    expires_at: float

    def __init__(
        self, status: int, payload: AggregatedHealthPayload, expires_at: float
    ) -> None:
        body = json.dumps(payload, separators=_JSON_SEPARATORS).encode("utf-8")
        head_frame, get_frame = _build_wire_frames(status, body, keep_alive=True)
        head_frame_close, get_frame_close = _build_wire_frames(status, body, keep_alive=False)
        self.status = status
        self.payload = payload
        self.head_frame = head_frame
        self.get_frame = get_frame
        self.head_frame_close = head_frame_close
        self.get_frame_close = get_frame_close
        self.expires_at = expires_at


def parse_services(raw: str | None) -> dict[str, str]:
    """Parse SERVICES env var supporting 'name=http://host:port/path' or legacy 'host:port'."""
    services: dict[str, str] = {}
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


def check_service_health(name: str, url: str, timeout: float = 3.0) -> ServiceHealthResult:
    """Check health of a single upstream service."""
    target = _UpstreamTarget(name, url)
    try:
        return target.probe(timeout=timeout)
    finally:
        target.close()


def _probe_targets(
    targets: tuple[_UpstreamTarget, ...], timeout: float = 3.0
) -> tuple[int, AggregatedHealthPayload]:
    if not targets:
        return 503, {
            "status": "degraded",
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "services": [],
        }

    if len(targets) == 1:
        results: list[ServiceHealthResult] = [targets[0].probe(timeout)]
    else:
        futures = [_SHARED_POOL.submit(t.probe, timeout) for t in targets]
        results = [f.result() for f in futures]

    all_healthy = all(r["status"] == "healthy" for r in results)
    status_code = 200 if all_healthy else 503
    return status_code, {
        "status": "healthy" if all_healthy else "degraded",
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "services": results,
    }


def check_all_services(
    services: dict[str, str], timeout: float = 3.0
) -> tuple[int, AggregatedHealthPayload]:
    """Check all configured services concurrently and return (http_status, body)."""
    targets = tuple(_UpstreamTarget(name, url) for name, url in services.items())
    try:
        return _probe_targets(targets, timeout=timeout)
    finally:
        for t in targets:
            t.close()


class _CoalescedHealthCache:
    """Lock-free read / single-flight refresh cache serving pre-encoded HTTP wire frames."""

    __slots__ = ("_targets", "_ttl", "_lock", "_snapshot")

    _targets: tuple[_UpstreamTarget, ...]
    _ttl: float
    _lock: threading.Lock
    _snapshot: _CachedSnapshot | None

    def __init__(self, targets: tuple[_UpstreamTarget, ...], ttl_seconds: float = 2.0) -> None:
        self._targets = targets
        self._ttl = ttl_seconds
        self._lock = threading.Lock()
        self._snapshot = None

    def get_snapshot(self) -> _CachedSnapshot:
        # Lock-free fast path: reading a reference is atomic in CPython
        snap = self._snapshot
        now = time.monotonic()
        if snap is not None and now < snap.expires_at:
            return snap

        with self._lock:
            snap = self._snapshot
            if snap is not None and time.monotonic() < snap.expires_at:
                return snap
            status, payload = _probe_targets(self._targets)
            fresh = _CachedSnapshot(status, payload, time.monotonic() + self._ttl)
            self._snapshot = fresh
            return fresh

    def close(self) -> None:
        for t in self._targets:
            t.close()


class _HealthHandlerBase(BaseHTTPRequestHandler):
    cache: ClassVar[_CoalescedHealthCache]


def make_handler(
    services: dict[str, str], cache_ttl: float = 2.0, access_log: bool = True
) -> type[_HealthHandlerBase]:
    targets = tuple(_UpstreamTarget(name, url) for name, url in services.items())
    health_cache = _CoalescedHealthCache(targets, ttl_seconds=cache_ttl)

    class HealthHandler(_HealthHandlerBase):
        protocol_version = "HTTP/1.1"
        disable_nagle_algorithm = True  # Sets TCP_NODELAY on every accepted client socket
        timeout = 10.0  # Bound idle keep-alive sockets against Slowloris thread exhaustion
        cache: ClassVar[_CoalescedHealthCache] = health_cache

        def do_HEAD(self) -> None:  # noqa: N802
            self._handle(head_only=True)

        def do_GET(self) -> None:  # noqa: N802
            self._handle(head_only=False)

        def _handle(self, head_only: bool) -> None:
            path = self.path.split("?", 1)[0]
            keep_alive = not self.close_connection
            if path == "/health":
                snap = self.cache.get_snapshot()
                if keep_alive:
                    self.wfile.write(snap.head_frame if head_only else snap.get_frame)
                else:
                    self.wfile.write(snap.head_frame_close if head_only else snap.get_frame_close)
                if access_log:
                    self.log_request(snap.status)
                return

            if path == "/healthz":
                body = json.dumps(
                    {
                        "status": "healthy",
                        "service": "health-aggregator",
                        "timestamp": datetime.now(timezone.utc).isoformat(),
                    },
                    separators=_JSON_SEPARATORS,
                ).encode("utf-8")
                head_frame, get_frame = _build_wire_frames(200, body, keep_alive=keep_alive)
                self.wfile.write(head_frame if head_only else get_frame)
                if access_log:
                    self.log_request(200)
                return

            if keep_alive:
                self.wfile.write(_NOT_FOUND_HEAD if head_only else _NOT_FOUND_GET)
            else:
                self.wfile.write(_NOT_FOUND_HEAD_CLOSE if head_only else _NOT_FOUND_GET_CLOSE)
            if access_log:
                self.log_request(404)

        def log_message(self, format: str, *args: object) -> None:  # noqa: A002
            if access_log:
                logger.info("%s - %s", self.address_string(), format % args)

    return HealthHandler


class _DualStackHTTPServer(ThreadingHTTPServer):
    address_family = socket.AF_INET6
    allow_reuse_address = True
    daemon_threads = True
    request_queue_size = 128

    def server_bind(self) -> None:
        self.socket.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 0)
        super().server_bind()


class _IPv4HTTPServer(ThreadingHTTPServer):
    allow_reuse_address = True
    daemon_threads = True
    request_queue_size = 128


def create_server(
    host: str = "::",
    port: int = 8000,
    services: dict[str, str] | None = None,
    cache_ttl: float | None = None,
) -> ThreadingHTTPServer:
    resolved = services if services is not None else parse_services(os.getenv("SERVICES"))
    ttl = cache_ttl if cache_ttl is not None else float(os.getenv("HEALTH_CACHE_TTL_SECONDS", "2.0"))
    access_log = os.getenv("HEALTH_ACCESS_LOG", "true").lower() not in ("0", "false", "no")
    handler = make_handler(resolved, cache_ttl=ttl, access_log=access_log)
    if ":" in host:
        return _DualStackHTTPServer((host, port), handler)
    return _IPv4HTTPServer((host, port), handler)


def _tune_runtime_allocator_and_gc() -> None:
    """Freeze long-lived stdlib/module objects out of cyclic GC and raise gen-0 threshold."""
    gc.collect()
    gc.freeze()
    gc.set_threshold(50_000, 10, 10)


if __name__ == "__main__":
    port = int(os.getenv("PORT", "8000"))
    host = os.getenv("HOST", "::")
    services = parse_services(os.getenv("SERVICES"))
    server = create_server(host, port, services)
    _tune_runtime_allocator_and_gc()

    def _shutdown(signum: int, _frame: FrameType | None) -> None:
        logger.info("Received signal %s, shutting down health aggregator...", signum)
        threading.Thread(target=server.shutdown, daemon=True).start()

    signal.signal(signal.SIGTERM, _shutdown)
    signal.signal(signal.SIGINT, _shutdown)

    logger.info("Health aggregator listening on [%s]:%s checking %d services", host, port, len(services))
    try:
        server.serve_forever()
    finally:
        handler_cls = server.RequestHandlerClass
        if isinstance(handler_cls, type) and issubclass(handler_cls, _HealthHandlerBase):
            handler_cls.cache.close()
        server.server_close()
        _SHARED_POOL.shutdown(wait=False)
