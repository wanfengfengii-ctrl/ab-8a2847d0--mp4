# syntax=docker/dockerfile:1

# ---- build stage: compile the static API binary ----
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/fmp4audit ./cmd/server

# ---- runtime stage: minimal API image ----
FROM alpine:3.20 AS runtime
RUN adduser -D -u 10001 appuser
COPY --from=build /out/fmp4audit /usr/local/bin/fmp4audit
USER appuser
EXPOSE 8080
ENV PORT=8080
HEALTHCHECK --interval=5s --timeout=3s --retries=12 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/fmp4audit"]

# ---- verify stage: one-shot build check + unit tests + HTTP smoke ----
FROM golang:1.22-alpine AS verify
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY scripts ./scripts
RUN chmod +x scripts/verify.sh
CMD ["sh", "scripts/verify.sh"]
