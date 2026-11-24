FROM caddy:2.11-alpine

COPY Caddyfile /etc/caddy/Caddyfile
COPY static /srv/static

ENV PORT=8080
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:${PORT}/gateway/health >/dev/null || exit 1
