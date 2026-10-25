FROM postgres:18-alpine
RUN apk add --no-cache ca-certificates tzdata && addgroup -g 1000 sub2api && adduser -D -u 1000 -G sub2api sub2api
WORKDIR /app
COPY --chown=sub2api:sub2api sub2api /app/sub2api
COPY --chown=sub2api:sub2api resources /app/resources
USER sub2api
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=6 CMD wget -q -T 4 -O /dev/null http://127.0.0.1:8080/health || exit 1
ENTRYPOINT ["/app/sub2api"]
CMD []
