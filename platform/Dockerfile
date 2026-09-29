FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /zns ./cmd/zns
FROM build AS test
RUN apk add --no-cache gcc musl-dev ffmpeg
ENV MEDIA_TEST_REQUIRE_FFMPEG=1
CMD ["go", "test", "-race", "-count=1", "-p", "2", "-parallel", "4", "./cmd/...", "./internal/...", "./integration/..."]
FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 zns
USER zns
COPY --from=build /zns /usr/local/bin/zns
ENTRYPOINT ["zns"]
