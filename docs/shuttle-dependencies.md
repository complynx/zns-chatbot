# Shuttle библиотеки и инструменты

Выбор и аудит зависимостей — снимок на 12 октября 2026 года. Описание
реализованных частей сверено с кодом 15 октября 2026 года. Основной принцип — небольшие предметные модули,
stdlib для транспорта и инфраструктурных операций, общий проверенный стек с Loom.
Зависимость добавляется в приложение только вместе с использующим её кодом.
Целевая платформа — сервер Daniel, Linux/Docker, linux/amd64, как у Loom.
Windows — среда разработки; обязательные release gates выполняются в Linux.

В таблицах закреплён выбор кандидатов для полного проекта, а не перечень
уже импортируемых пакетов. Go-i18n, Goleak и otelhttp пока не используются
реализацией. Фактический runtime graph определяется go.mod и
tools/dependencies.lock.json; planned imports проверены отдельно в candidate.

## Основной стек

| Назначение | Выбор | Версия | Лицензия | GitHub stars |
| --- | --- | --- | --- | ---: |
| PostgreSQL driver и pool | github.com/jackc/pgx/v5 | 5.11.0 | MIT | 14 321 |
| Типизированный SQL | github.com/sqlc-dev/sqlc, dev tool | 1.31.1 | MIT | 18 371 |
| SQL migrations | github.com/pressly/goose/v3 | 3.28.0 | MIT | 11 563 |
| HTTP API и OpenAPI | github.com/danielgtaylor/huma/v2, humago | 2.39.1 | MIT | 4 450 |
| PostgreSQL jobs | River core и riverpgxv5 | 0.49.0 | MPL-2.0 | 5 768 |
| JWT/JWKS криптография | github.com/go-jose/go-jose/v4 | 4.1.4 | Apache-2.0 | 537 |
| UUID | github.com/google/uuid | 1.6.0 | BSD-3-Clause | 6 148 |
| Markdown AST | github.com/yuin/goldmark/v2 | 2.1.6 | MIT | 5 055 |
| i18n и plurals | github.com/nicksnyder/go-i18n/v2 | 2.6.1 | MIT | 3 548 |
| Языковые теги | golang.org/x/text | 0.43.0 | BSD-3-Clause | 809 |
| Конфигурация | go.yaml.in/yaml/v3 | 3.0.5 | MIT и Apache-2.0 | 535 |
| Общая попытка обновления токена | golang.org/x/sync/singleflight | 0.24.0 | BSD-3-Clause | 933 |
| Assertions в тестах | github.com/stretchr/testify | 1.12.1 | MIT | 26 224 |
| Проверка завершения goroutines | go.uber.org/goleak, test only | 1.3.0 | MIT | 5 290 |

Звёзды — снимок публичного GitHub API, а не оценка безопасности. Все перечисленные
репозитории не архивированы и не отключены. Даты создания и последнего push
сохранены в [github-evidence.json](dependency-audit/2026-10-12/github-evidence.json).
Возраст yaml/go-yaml отражает maintained repository, а не возраст всей реализации YAML.
GitHub помечает Goose как NOASSERTION; закреплённый LICENSE проверен непосредственно
и содержит MIT. Для YAML сохраняются оба лицензионных уведомления.

pgx, sqlc, Goose, Huma, River, go-jose, YAML и Testify совпадают с выбранными версиями
Loom. Переиспользуются проверенные версии, а не весь граф и код Loom.

Huma использует стандартный net/http ServeMux. Дополнительный router, ORM, sqlx
и отдельный JSON Schema engine для первой версии не требуются. Совместимость
manifest с Loom проверяется контрактом, без загрузки remote schema refs.

River обеспечивает очередь, аренду исполнения и scheduling повторов. Предметная
идемпотентность, status lookup, права и дедупликация внешних изменений остаются
ответственностью Shuttle. River Pro не используется. Вставка job и изменение
бизнес-состояния должны выполняться одной PostgreSQL-транзакцией.

Goldmark используется как AST parser. Собственный небольшой renderer преобразует
AST в текст и Telegram entities, включая mentions, UTF-16 offsets и разбиение.
HTML renderer не применяется как промежуточный протокол Telegram. Версия v2
имеет стабильный release; специфическое поведение renderer проверяется отдельно.
Вход не обязан быть валидным Markdown. Parser/renderer должны сохранять содержимое
оборванных fences, незакрытых маркеров и кривых ссылок. Неоднозначное форматирование
переходит в plain text, без потери ответа или изменения user reference.
Перед отправкой проверяются Telegram nesting и UTF-16 offsets. Для renderer
и разбиения используются повреждённые fixtures и встроенный Go fuzzing.
Опциональный formatting repair через Loom пока не реализован. В будущем для
диагностированного дефекта можно один раз запросить исправление в Loom
с конкретной ошибкой. Это ограниченный запрос без бизнес-инструментов, после него
entities проверяются заново. Нет необходимости писать сложный repair engine:
при отказе Loom или повторном дефекте сохраняется локальный текстовый fallback.

Go-i18n и JSON каталоги en/ru относятся к будущему i18n этапу. Сейчас системные
строки en/ru находятся в небольшой таблице internal/task/delivery.go.
Fallback be/uk → ru
задаётся приложением. CLI для extraction/merge добавляется при необходимости;
runtime не обязан импортировать TOML/YAML loaders этой CLI.

go.yaml.in/yaml/v3 читает config.yaml с init-параметрами. Env overrides и передача
секретов используют os.LookupEnv и небольшое typed mapping; Viper или универсальный
config framework не требуются. Изменяемые runtime-параметры читаются из PostgreSQL
через sqlc и меняются административным API с валидацией, проверкой прав и аудитом.

go-jose выполняет криптографические проверки. Discovery, introspection, выдача
и обмен токенов Zitadel выполняются через net/http за небольшим типизированным
adapter. Zitadel SDK не добавляется без потребности в его конкретных API.

## Telegram transport

Основной вариант — собственный узкий client на net/http с типизированными DTO
только для используемых updates и методов. Это позволяет явно контролировать
durable inbox, продвижение polling offset, webhook ACK, cancellation и лимиты файлов.
Не требуется Telegram dispatcher или хранение workflow внутри библиотеки.

Перед реализацией фиксируются необходимые методы: getUpdates/webhook setup,
getMe, sendMessage/editMessageText, answerCallbackQuery, getFile и фактически
используемые методы отправки файлов и настройки меню. Общий HTTP transport,
response decoding и типизированные ошибки переиспользуются. URL с bot token
не должен попадать в логи или OTel span attributes. HTTP instrumentation должна
санитизировать такой path до записи; redaction после экспорта недостаточна.

Проверенный резерв — github.com/go-telegram/bot 1.27.0, MIT, 1 854 stars,
репозиторий активен и его go.mod не содержит внешних require. Его версия и полный
SHA сохранены в selection.json, но библиотека исключена из основного audit-графа.
Встроенные polling/webhook handlers нельзя принимать за durable очередь:
их использование допускается только после проверки порядка commit и ACK.

Альтернатива gotgbot имеет MIT и 723 stars; текущие v2 tags — release candidates,
например v2.0.0-rc.36. Для начального стека выбранный stdlib transport проще,
а стабильный go-telegram/bot остаётся резервом без зависимости от RC API.

## OpenTelemetry metrics и логи

| Назначение | Модуль | Версия | Лицензия |
| --- | --- | --- | --- |
| Traces, propagation и resources | go.opentelemetry.io/otel и sdk | 1.47.0 | Apache-2.0 |
| Metrics API и SDK | go.opentelemetry.io/otel/metric и sdk/metric | 1.47.0 | Apache-2.0 |
| OTLP HTTP traces | go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp | 1.47.0 | Apache-2.0 |
| Prometheus metrics exporter | go.opentelemetry.io/otel/exporters/prometheus | 0.69.0 | Apache-2.0 |
| HTTP instrumentation | go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp | 0.72.0 | Apache-2.0 |
| Registry, promhttp и Go runtime collectors | github.com/prometheus/client_golang | 1.25.0 | Apache-2.0 |

У OTel API/SDK и exporters отдельные версии. Prometheus exporter и otelhttp
ещё имеют v0 API: фиксируются полные SHA, обновления требуют контрактной проверки.
OTel Go repository имеет 6 574 stars, contrib — 1 678, Prometheus client — 6 040.

Прикладные metrics создаются через OTel Metrics API и экспортируются в одну
Prometheus registry. client_golang обслуживает /metrics и стандартные collectors;
второй независимый набор тех же прикладных counters не создаётся. Traces отправляются
через OTLP HTTP в Alloy. Логи остаются slog JSON stdout. Контекстные записи теперь
получают валидные trace/span ID через stdlib slog.Handler и существующий OTel API;
записи без span остаются без ID. См. [контракт и ограничения](shuttle-log-correlation.md).
Отдельный OTel Logs SDK и push логов не требуются.

На текущем server_configs Alloy уже собирает Prometheus и Docker logs. OTLP receiver
и маршрут traces требуют отдельного конфигурационного изменения; их наличие
на production не предполагается. Loom уже имеет OTel providers в internal/telemetry/otel.go. Сквозная propagation,
redaction и связь очередей проверяются отдельным контрактом с Shuttle.

Для очереди tracing выполняется короткими spans попыток с сохранёнными links.
Личные данные, auth credentials и содержимое файлов не входят в telemetry.
Metrics endpoint доступен на внутреннем listener. Pprof пока не подключён;
если потребуется, он также остаётся внутренним.

## Инструменты разработки и требования к CI

Версии инструментов и локальные проверки закреплены. Shuttle CI в
[shuttle-ci.yml](../.github/workflows/shuttle-ci.yml) проверяет reviewed module graph
и checksums, PostgreSQL behavioral/race tests, generated SQL, lint и Linux/Docker
build. Actions и developer tools закреплены полными commit SHA; образ БД — digest.
Workflow не публикует образ и не запускает deployment. Existing build.yml
собирает прежний Python image. Dependabot и автоматические vulnerability/license
gates пока остаются следующими улучшениями.

- Go 1.27.2, как в Loom. Официальный стабильный release и SHA архивов проверяются;
  локальная системная установка не изменяется автоматически.
- golangci-lint 2.14.0 с Golden-конфигурацией Loom: contextcheck, запрет deprecated
  openpgp, goimports/golines и длина строки 120. В [.golangci.yml](../.golangci.yml)
  текущий local import prefix — github.com/complynx/zns-chatbot.
- sqlc 1.31.1 с PostgreSQL и pgx/v5. Generated queries коммитятся вместе с SQL;
  CI проверяет отсутствие изменений после generate. [sqlc.yaml](../sqlc.yaml)
  читает migrations и internal/db/queries.sql, результат — internal/db/sqlc.
- Goose SQL migrations. Сборка мигратора включает только нужный PostgreSQL driver.
- go test, go vet, race detector, httptest и testing/synctest; Testify assert/require.
  Mock framework по умолчанию не используется. Goleak по необходимости применяется к lifecycle,
  cancellation и shutdown, а не механически ко всем тестам.
- Реальный PostgreSQL через изолированный DSN/schema и Docker Compose, как в Loom.
  Testcontainers не нужен для первого integration harness.
- govulncheck 1.8.0 и go-licenses/v2 2.0.1, с полными SHA tools в selection.json.
- Linux/amd64 Docker build, CGO=0 для приложения; race gate требует CGO.
  Образы фиксируются digest, CI actions — полным commit.
- Dependabot для gomod, images и actions без auto-merge; изменение версии требует
  обновить SHA lock, аудит и релевантные проверки.

Первый этап использует net/http, context, log/slog, os/signal, encoding/json и
crypto. Loom API вызывается типизированным stdlib HTTP client с polling run status.
SSE cursor/reconnect пока не реализован. Если потребуется поток событий, parser
должен ограничивать размер события и поддерживать восстановление cursor.

os/exec уже запускает отдельный avatar executable, а стандартные image codecs
используются его renderer. См. [avatar contract](shuttle-avatars.md).
internal/events импортирует time/tzdata: IANA zone data включены в бинарник
для минимального Linux image. html/template пока не используется.
Деньги хранятся в целых minor units с currency scale; подтверждённые платежи,
частичные оплаты и ledger уже реализованы. См. [payment contract](shuttle-payments.md).
Decimal library, Cobra/Viper, второй logger и собственный model SDK не нужны.

## Дополнительные кандидаты по потребности

| Компонент | Лицензия и stars | Условие добавления |
| --- | --- | --- |
| golang.org/x/time/rate | BSD-3-Clause, 424 | Admission limits HTTP и Telegram delivery, если простых ограничений недостаточно |
| golang.org/x/sys/unix | BSD-3-Clause, 1 359 | Linux process groups/resource limits isolated workers |
| Excelize v2 | BSD-3-Clause, 20 983 | Реализация необходимого XLSX export из административного API |
| Gitleaks | MIT, 29 874 | CI/pre-commit проверка случайно добавленных credentials |
| Trivy | Apache-2.0, 38 369 | Image/OS scan и SBOM перед первой контейнерной поставкой |

Эти репозитории проверены по metadata, но конкретные версии и полный граф ещё
не выбраны. Они не объявляются проверенными runtime-зависимостями. Перед добавлением
нужны version/SHA pin, license scan и применимые security checks. Для XLSX также
проверяются лицензии фактически импортируемых пакетов и embedded assets.

Retry и ожидание зависимостей первоначально реализуются через River и сохранённое
состояние задачи. Backoff уже встречается транзитивно в OTLP exporter; это не
разрешает автоматический replay бизнес-записей. Circuit breaker добавляется только
при измеренной необходимости. Redis, Kafka, отдельный workflow engine, Goja,
model SDKs, PDF/media converters и face-recognition dependencies в бот не входят.
Общие file tools принадлежат Loom и читают исходники через API Shuttle по
`attachment_id`. В задачи передаются ID и метаданные; Loom использует временные
рабочие копии. Специфические avatar workers подключаются отдельно.

## Лицензии фиксация и аудит

Политика соответствует Loom: MIT, Apache-2.0, BSD-2/3-Clause, ISC и MPL-2.0
допустимы с выполнением notices и соответствующих условий. MPL-2.0 River имеет
обязательства для покрытых файлов; это не коммерческая лицензия River Pro.
Новые неизвестные или custom licenses требуют отдельного решения.

Исключение GPL-3.0 применяется только к golangci-lint как dev/CI tool.
Оно не разрешает GPL/AGPL/LGPL runtime libraries. Линтер не включается в image
приложения. При распространении самих инструментов проверяются их собственные
обязательства и транзитивный граф.

go.mod содержит canonical versions, go.sum — content hashes, отдельный lock —
полные VCS SHA всего разрешённого графа. В production запрещены floating latest,
branches и непроверенные replace. Изменения проходят go mod verify, проверку lock,
лицензий, govulncheck, lint, build и предметные тесты.

Аудит выбора хранится в [dependency-audit/2026-10-12](dependency-audit/2026-10-12/README.md).
Это исторический самостоятельный candidate module этапа выбора. Его граф включает
planned imports и test libraries. Размер полного module graph не равен числу
пакетов или лицензий binary: например Goose приводит дополнительные driver modules,
которые не импортируются PostgreSQL-only приложением.

Первоначальный audit выявил GO-2026-6603, GO-2026-6610, GO-2026-6611,
GO-2026-6612 и GO-2026-6617 в golang.org/x/net 0.59.0. Candidate обновлён до 0.60.0.
Не следует считать JSON exit code govulncheck доказательством отсутствия findings;
проверяется содержимое отчёта. Финальный результат и ограничения указаны в README аудита.

Сканер лицензий предупреждает о не-Go assembly в xxhash: это ограничение проверки,
а не обнаруженная запрещённая лицензия. Toolchain/native assets проверяются отдельно. Фактический runtime audit приложения
уже сохранён в runtime-checks.json рядом с candidate; go.mod и
tools/dependencies.lock.json определяют реализованный граф. Выбор не заменяет production QA.

Written by Codex (gpt-6/Codex desktop)
on behalf of Daniel Drizhuk


## Parallel CI workers

Shuttle CI runs four integration jobs with independent PostgreSQL 17 services.
Each job discovers the complete sorted test list and runs every fourth root,
starting at its zero-based shard index. Batching, race detection, process-tree
timeouts and rejection of skipped tests remain enabled. Unit, UI, dependency,
SQL, lint, operational and build checks run concurrently in the `checks` job;
its PostgreSQL service also remains available to database unit tests.

The final required check remains `verify`. It requires every upstream job to
succeed and validates all four summaries: identical discovery, distinct worker
indices, the expected partition and exactly one successful execution of every
root. Missing artifacts or incomplete batches fail this check. Shard summaries
and logs are retained for seven days, including failed runs.

Local use remains `python3 tools/test-integration.py NEW_OUTPUT_DIRECTORY` with
`SHUTTLE_TEST_DATABASE_URL` set. Optional `--shard-index 0 --shard-count 4`
selects one partition; concurrent local workers need separate databases.
`python3 tools/test-integration.py DOWNLOADED_ARTIFACT_DIRECTORY --verify-shards 4`
validates downloaded evidence and does not require PostgreSQL or Go.

Artifact transfer uses the official MIT-licensed
[upload-artifact](https://github.com/actions/upload-artifact) v7.0.2 and
[download-artifact](https://github.com/actions/download-artifact) v8.0.2 actions,
pinned to full commit hashes. Both use Node 24 on the hosted Ubuntu runner and
add no application runtime dependency or production configuration.
