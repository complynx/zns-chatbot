FROM golang:1.27.2-alpine3.24@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build
WORKDIR /src
ENV GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH=amd64
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
RUN go build -trimpath -buildvcs=false -ldflags='-s -w' -o /out/shuttle ./cmd/shuttle
RUN go build -trimpath -buildvcs=false -ldflags='-s -w' -o /out/zinusya-avatar ./cmd/zinusya-avatar
RUN go build -trimpath -buildvcs=false -ldflags='-s -w' -o /out/shuttle-import ./cmd/shuttle-import
RUN go build -trimpath -buildvcs=false -ldflags='-s -w' -o /out/shuttle-symbols ./cmd/shuttle-symbols
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/shuttle /usr/local/bin/shuttle
COPY --from=build /out/zinusya-avatar /usr/local/bin/zinusya-avatar
COPY --from=build /out/shuttle-import /usr/local/bin/shuttle-import
COPY --from=build /out/shuttle-symbols /usr/local/bin/shuttle-symbols
COPY deploy/shuttle/licenses/dependencies /licenses/dependencies/
COPY deploy/shuttle/licenses/avatars /licenses/avatars/
COPY LICENSE /licenses/shuttle/LICENSE
COPY --from=build /usr/local/go/LICENSE /usr/local/go/PATENTS /licenses/go/
USER 10001:10001
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/shuttle"]
CMD ["-config", "/etc/shuttle/config.yaml"]
