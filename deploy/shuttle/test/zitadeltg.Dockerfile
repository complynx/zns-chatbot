# Build only from an isolated, recorded zitadeltg source snapshot.
FROM golang:1.27.2-alpine3.24@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build
WORKDIR /src
ENV GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH=amd64
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY cmd ./cmd
COPY internal ./internal
COPY LICENSE ./LICENSE
RUN go build -trimpath -buildvcs=false -ldflags='-s -w' -o /out/zitadeltg ./cmd/zitadeltg
RUN mkdir -p /runtime-data
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/zitadeltg /usr/local/bin/zitadeltg
COPY --from=build /src/LICENSE /licenses/zitadeltg/LICENSE
COPY --from=build /usr/local/go/LICENSE /usr/local/go/PATENTS /licenses/go/
COPY --from=build --chown=1000:1000 /runtime-data /data
USER 1000:1000
WORKDIR /data
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/zitadeltg"]
CMD ["-config", "/config/zitadeltg.yaml"]
