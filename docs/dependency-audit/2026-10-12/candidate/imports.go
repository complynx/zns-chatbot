// Package selection enumerates planned imports for dependency auditing only.
package selection

import (
    _ "github.com/danielgtaylor/huma/v2/adapters/humago"
    _ "github.com/go-jose/go-jose/v4"
    _ "github.com/google/uuid"
    _ "github.com/jackc/pgx/v5/pgxpool"
    _ "github.com/nicksnyder/go-i18n/v2/i18n"
    _ "github.com/pressly/goose/v3"
    _ "github.com/prometheus/client_golang/prometheus/promhttp"
    _ "github.com/riverqueue/river"
    _ "github.com/riverqueue/river/riverdriver/riverpgxv5"
    _ "github.com/yuin/goldmark/v2"
    _ "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
    _ "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
    _ "go.opentelemetry.io/otel/exporters/prometheus"
    _ "go.opentelemetry.io/otel/sdk"
    _ "go.opentelemetry.io/otel/sdk/metric"
    _ "go.yaml.in/yaml/v3"
    _ "golang.org/x/sync/singleflight"
    _ "golang.org/x/text/language"
)
