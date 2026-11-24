FROM python:3.12-alpine

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PYTHONOPTIMIZE=2 \
    MALLOC_ARENA_MAX=2

WORKDIR /app
COPY main.py .

RUN adduser -D -H appuser
USER appuser

EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD python -c "import sys, http.client; c = http.client.HTTPConnection('127.0.0.1', 8000, timeout=2); c.request('HEAD', '/healthz'); sys.exit(0 if c.getresponse().status == 200 else 1)"

CMD ["python", "-OO", "main.py"]
