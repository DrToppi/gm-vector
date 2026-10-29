# Artifacts are built by scripts/build.py from pinned go.mod/go.sum/package-lock.
FROM scratch AS artifacts
COPY bin/planner /planner
COPY web/dist /web
COPY calendars /calendars
COPY notices /notices
FROM gcr.io/distroless/static-debian13:nonroot@sha256:d62067879de890a1def9da0e7766b7d20810a5e5555cef8506b752437d7a3e88
COPY --from=artifacts --chown=65532:65532 /planner /app/planner
COPY --from=artifacts --chown=65532:65532 /web /app/web
COPY --from=artifacts --chown=65532:65532 /calendars /app/calendars
COPY --from=artifacts --chown=65532:65532 /notices /app/notices
USER 65532:65532
WORKDIR /app
ENTRYPOINT ["/app/planner"]
CMD ["api"]
