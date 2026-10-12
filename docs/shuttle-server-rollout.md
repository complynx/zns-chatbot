# Развёртывание Shuttle на complynx.net

Это инструкция для оператора, а не запись выполненного deployment. Проверка
сервера 2026-10-12 была только на чтение. Текущий этап Shuttle не заменяет рабочий
бот: регистрации, пары, платежи, аватарки и миграция ещё не реализованы.
Начинать следует с отдельного тестового Telegram-бота.

## 1. Зафиксировать исходное состояние и релиз

Подтверждено на сервере: x86_64, Docker 29.4.0, Compose 5.1.3;
`/home/complynx/server_configs`, HEAD `20e9be3`; Traefik v3.6; PostgreSQL 17
в `postgres-postgres-1`; Zitadel v4.16.3 в `zitadel`; issuer
`https://id.complynx.net`. PostgreSQL имеет alias `postgres` в `traefik-net`.
Рабочий бот — `zns-chatbot-zns_chatbot-1`, его каталог — `configs/zns-chatbot`.
Zitadeltg подключён к отдельной `zitadeltg-edge`: Shuttle не нужно подключать туда.
Loom и Shuttle не запущены; `configs/loom`, `configs/shuttle` и сеть
`clx-observability` на момент проверки отсутствовали. На диске было около 90 GiB
свободно. Перед deployment перепроверить эти факты.

Из PowerShell:

```powershell
ssh -i C:/Users/ddriz/.ssh/id_ed25519 complynx@complynx.net
```

На сервере:

```sh
cd /home/complynx/server_configs
git status --short
git rev-parse HEAD
docker ps --format '{{.Names}} {{.Image}}'
docker network ls
docker compose version
df -h data/postgres
ss -ltn
```

Не перезаписывать грязный checkout. Порты ниже предлагаются для пилота:
`127.0.0.1:58380` и `127.0.0.1:58390`; убедиться, что они свободны.

Сначала выбрать прошедший QA source commit. Локальный первый этап — `b130aa09`,
но последующие исправления review/QA должны войти в выбранный релиз. Сборка:

```sh
docker build --platform linux/amd64 -f deploy/shuttle/Dockerfile \
  -t complynx/shuttle:<release-tag> .
```

Публикация образа и server_configs — отдельные разрешённые оператором действия.
После публикации записать source SHA и digest образа. В deployment использовать
`complynx/shuttle@sha256:<approved-digest>`, а не изменяемый tag.

## 2. Сначала подготовить Loom и проверить IAM

У Loom уже есть отдельная инструкция `clx-scripter/docs/server-rollout.md` и
шаблон `clx-scripter/deploy/server/docker-compose.prod.yml`. Для текущего сервера
нужны его профиль `configs/loom`, миграции отдельной БД, sandbox/proxy/API,
receiver обновлений и согласованные Alloy changes. Не запускать глобальный
`init.sh` ради этих двух приложений. Использовать целевые Compose команды или
`scripts/rollout.sh --config-name loom --profile prod --service-name api` после
подготовки migration hook. Стратегия — `recreate`.

Для Shuttle в существующей организации Zitadel подготовить:

- service account с client credentials для actor token;
- confidential OIDC client с Token Exchange grant;
- API/introspection application именно в проекте Shuttle, а не Loom;
  её client ID/secret соответствуют настроенному identity.api_audience;
- аудитории проектов Loom и Shuttle; отдельную роль `shuttle.admin`;
- права service account на создание/поиск пользователей в целевой организации
  и разрешённую impersonation. Точные назначения проверить на Zitadel v4.16.3.

Фактический grant: `client_credentials` для actor, затем Token Exchange с
`subject_token_type=urn:zitadel:params:oauth:token-type:user_id`, actor token и
`requested_token_type=urn:ietf:params:oauth:token-type:jwt`. После истечения токена
бот снова выполняет exchange; refresh token в этом этапе не используется.
Этот вариант user-ID impersonation описан Zitadel как экспериментальный:
[официальный контракт](https://zitadel.com/docs/guides/integrate/token-exchange).

Проверить выданный пользовательский JWT и introspection: правильные `iss`, `sub`,
обе нужные аудитории, resource-owner organization, `act.sub` service account и
проектные роли. В запросах actor/exchange используется scope
`urn:zitadel:iam:org:projects:roles`. API берёт права только из claim
`urn:zitadel:iam:org:project:<Shuttle-project-id>:roles` в introspection;
плоский role claim не даёт административного доступа. Проверить назначенную
Shuttle admin role и отказ для одноимённой роли только в Loom, включая случай
ошибочно выбранного introspection client. Включить role assertion в настройках проектов/приложений и
проверить реальный результат; mock JWT не подтверждает правильность этих настроек.
См. [scopes Zitadel](https://zitadel.com/docs/apis/openidoauth/scopes).

Отдельный обязательный gate: одна и та же связка Telegram bot ID + user ID должна
разрешаться в тот же Zitadel subject при входе сначала через Shuttle и сначала
через zitadeltg. Email — `tg+<bot-id>+<user-id>@telegram.invalid`. Совпадение строки
email само по себе не доказывает двустороннее связывание аккаунтов. Не включать
непроверенное auto-linking реальных адресов.

В проекте Loom назначить service account отдельную роль caller, например
`shuttle-caller`. В конфигурации Loom добавить соответствие этой роли только
правам, объявленным текущим manifest Shuttle:

```yaml
identity:
  service_subjects: ["<Shuttle-actor-user-id>"]
  role_permissions:
    shuttle-caller: [runs.create, runs.control, chat.read, chat.events.append, artifacts.read, artifacts.write]
```

Это фрагмент для слияния с существующим identity block, а не замена остальных
настроек Loom. Actor должен принадлежать той же организации, что delegated user;
Loom introspection client должен принадлежать проекту Loom. Проверить trusted
Docker discovery binding Shuttle. В текущем Loom harness-права берутся из actor
и manifest, а права бизнес-модулей — из пользователя; роль caller не назначается
каждому новому Telegram-пользователю автоматически.

Отдельно выбрать бюджет и модель пилота. В deploy/config.example.yaml Loom
`bootstrap_settings.initial_credits` равен нулю: наличие token/run ID не доказывает,
что модель сможет исполнить задачу. До пилота настроить согласованную credit policy
и проверить баланс через operator API. Платные вызовы и выдача credits здесь
не выполнялись. Изменение bootstrap YAML не перезаписывает уже сохранённые settings.
Для IAM downtime проверить и Loom: его online introspection пока не даёт обещать
непрерывность только благодаря кешу Shuttle. Эти реальные контракты ещё не приняты.
До их проверки запуск означает инфраструктурный пилот, а не пользовательский релиз.

## 3. Отдельная база и роли

Использовать существующий PostgreSQL, не перезапускать его и не менять роли других
приложений. Войти локально, без пароля администратора в командной строке:

```sh
docker exec -it postgres-postgres-1 psql -v ON_ERROR_STOP=1 -U complynx -d postgres
```

Создать один раз; если имена уже существуют, проверить владельцев, а не удалять:

```sql
CREATE ROLE shuttle_migrator LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
CREATE ROLE shuttle_runtime LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
\password shuttle_migrator
\password shuttle_runtime
CREATE DATABASE shuttle OWNER shuttle_migrator;
REVOKE ALL ON DATABASE shuttle FROM PUBLIC;
GRANT CONNECT ON DATABASE shuttle TO shuttle_runtime;
\connect shuttle
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
ALTER DEFAULT PRIVILEGES FOR ROLE shuttle_migrator IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO shuttle_runtime;
ALTER DEFAULT PRIVILEGES FOR ROLE shuttle_migrator IN SCHEMA public
  GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO shuttle_runtime;
\quit
```

Runtime role не владеет схемой и не запускает DDL. Миграция Goose и River выполняется
в одном отдельном release step. Не применять SQL из старой Go-ветки поверх этой БД.
В DSN использовать URL-encoded пароль; для генерации можно выбрать hex пароль.

## 4. Каталоги и конфигурация

Подготовить профиль в репозитории server_configs и доставить его утверждённую
версию на сервер. Секреты и YAML с секретами не коммитить. На сервере:

```sh
cd /home/complynx/server_configs
install -d -m 0755 configs/shuttle
install -d -m 0700 data/shuttle
```

Файлы:

- `configs/shuttle/docker-compose.prod.yml`: шаблон следующего этапа;
- `configs/shuttle/.env`: `SHUTTLE_IMAGE` с digest, без application secrets;
- `data/shuttle/config.yaml`: startup YAML;
- `data/shuttle/runtime.env`: runtime DSN, Telegram/IAM secrets;
- `data/shuttle/migration.env`: migrator DSN. Остальные обязательные init-поля
  берутся из общего YAML; настоящие IAM/Telegram secrets миграции не нужны.

`config.yaml` начать с `config/shuttle.example.yaml` из выбранного релиза. Указать
`http_address: 0.0.0.0:8080`, `internal_address: 0.0.0.0:9090`,
`shutdown_timeout: 1s`, `identity.issuer: https://id.complynx.net`, реальные org,
actor/client IDs, аудитории; `loom.base_url` — подтверждённый URL Loom.
Для Docker выбрать уникальный alias Loom из его профиля, например
`http://loom:8080`, только после проверки этого alias в `traefik-net`.
Не использовать неоднозначный общий alias `api` нескольких Compose проектов.
Публичный `https://agent.complynx.net` допустим после deployment Loom и проверки TLS.

Оставить валидные fake secret placeholders в YAML и заменить их через env:

```dotenv
SHUTTLE_DATABASE_URL=postgres://shuttle_runtime:<encoded-password>@postgres:5432/shuttle?sslmode=disable
SHUTTLE_TELEGRAM_TOKEN=<separate-pilot-bot-token>
SHUTTLE_TELEGRAM_WEBHOOK_SECRET=<random-url-safe-secret-at-least-16-chars>
SHUTTLE_IDENTITY_ACTOR_CLIENT_SECRET=<service-account-secret>
SHUTTLE_IDENTITY_CLIENT_SECRET=<token-exchange-client-secret>
SHUTTLE_IDENTITY_INTROSPECTION_CLIENT_SECRET=<api-introspection-secret>
```

В `migration.env`:

```dotenv
SHUTTLE_DATABASE_URL=postgres://shuttle_migrator:<encoded-password>@postgres:5432/shuttle?sslmode=disable
```

`sslmode=disable` относится только к текущей доверенной Docker сети этого сервера.
При выносе PostgreSQL использовать TLS и проверку CA. Env files читает host Compose,
runtime YAML читает UID 10001 в контейнере. Например:

```sh
chmod 0600 data/shuttle/runtime.env data/shuttle/migration.env configs/shuttle/.env
chmod 0644 data/shuttle/config.yaml
```

0644 допустим только для YAML без настоящих секретов. Каталог data/shuttle остаётся
0700. Не печатать раскрытый `docker compose config`, env или `docker inspect` с
Environment в журнал CI. Docker administrators имеют доступ к container env.

## 5. Compose для пилота

Сохранить в `configs/shuttle/docker-compose.prod.yml`:

```yaml
name: shuttle
x-runtime: &runtime
  image: ${SHUTTLE_IMAGE:?Set approved image digest}
  platform: linux/amd64
  read_only: true
  cap_drop: [ALL]
  security_opt: [no-new-privileges:true]
  logging:
    driver: json-file
    options: {max-size: "10m", max-file: "3"}
services:
  migrate:
    <<: *runtime
    profiles: [migration]
    command: [-config, /etc/shuttle/config.yaml, -migrate]
    env_file: [../../data/shuttle/migration.env]
    volumes: [../../data/shuttle/config.yaml:/etc/shuttle/config.yaml:ro]
    networks: [traefik-net]
  shuttle:
    <<: *runtime
    command: [-config, /etc/shuttle/config.yaml]
    env_file: [../../data/shuttle/runtime.env]
    volumes: [../../data/shuttle/config.yaml:/etc/shuttle/config.yaml:ro]
    ports: ["127.0.0.1:58380:8080", "127.0.0.1:58390:9090"]
    networks:
      traefik-net: {aliases: [shuttle]}
      clx-observability: {aliases: [shuttle]}
    stop_grace_period: 5s
    restart: unless-stopped
    labels:
      - "traefik.enable=true"
      - "traefik.docker.network=traefik-net"
      - "traefik.http.routers.shuttle-pilot.rule=Host(`zouknonstop.com`) && PathPrefix(`/shuttle-pilot/`)"
      - "traefik.http.routers.shuttle-pilot.entrypoints=web"
      - "traefik.http.routers.shuttle-pilot.tls.certresolver=letsencrypt"
      - "traefik.http.routers.shuttle-pilot.middlewares=strip-shuttle-pilot"
      - "traefik.http.middlewares.strip-shuttle-pilot.stripprefix.prefixes=/shuttle-pilot"
      - "traefik.http.services.shuttle-pilot.loadbalancer.server.port=8080"
      - "complynx.docker.rollout-strategy=recreate"
      - "clx.harness.service=http://shuttle:9090/internal/agent/manifest"
networks:
  traefik-net: {external: true}
  clx-observability: {external: true}
```

Образ scratch не содержит shell/curl. Не добавлять CMD-SHELL healthcheck.
Private listener содержит API и неаутентифицированный `/metrics`; Traefik направляет
только на 8080. Контейнеры в общих сетях могут обращаться к 9090: membership сети —
граница доверия оператора. При необходимости более строгой изоляции нужен отдельный
сетевой профиль. У API есть дополнительная bearer/role проверка.

Сеть создать после согласованных Loom/Alloy changes:

```sh
docker network inspect clx-observability >/dev/null 2>&1 || docker network create clx-observability
cd /home/complynx/server_configs/configs/shuttle
docker compose -f docker-compose.prod.yml config --quiet
docker compose -f docker-compose.prod.yml --profile migration pull
```

Не масштабировать Shuttle: текущий singleton lock действует на всю схему БД.
Каждому отдельному боту пока нужна отдельная БД/схема и процесс.

## 6. Миграция и запуск

Для первого запуска:

```sh
docker compose -f docker-compose.prod.yml --profile migration run --rm --no-deps migrate
```

При ошибке остановиться. Затем выдать доступ к существующим объектам:

```sh
docker exec -i postgres-postgres-1 psql -v ON_ERROR_STOP=1 -U complynx -d shuttle <<'SQL'
GRANT USAGE ON SCHEMA public TO shuttle_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO shuttle_runtime;
GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO shuttle_runtime;
SQL

docker compose -f docker-compose.prod.yml up -d --no-deps shuttle
curl --fail --silent http://127.0.0.1:58380/healthz
curl --fail --silent http://127.0.0.1:58380/readyz
curl --fail --silent http://127.0.0.1:58390/internal/agent/manifest
curl --fail --silent https://zouknonstop.com/shuttle-pilot/healthz
```

Readiness проверяет PostgreSQL и admission, но не IAM/Loom и не наличие прав на все
записи: принятые задачи могут ждать upstream. Проверить отдельным действием запись
в БД, identity provisioning, run и Telegram delivery через реальный пилот.
Loom discovery должен найти label, принять manifest и показать разрешённый
`account.me`. Сейчас admin endpoints существуют как HTTP API, но не входят в
agent manifest. Проверьте вызов account.me именно агентом, не только curl.

## 7. Webhook отдельного бота и наблюдаемость

Новый пилот не меняет webhook рабочего ботa. Зарегистрировать его после проверки
публичного TLS. Bot token не включать в Git, вывод или command history. Например
коротким Python скриптом, который читает значения через getpass и печатает только ok:

```sh
python3 - <<'PY'
import getpass, json, urllib.request
token = getpass.getpass('Pilot bot token: ')
secret = getpass.getpass('Webhook secret from runtime.env: ')
payload = json.dumps({
    'url': 'https://zouknonstop.com/shuttle-pilot/telegram/webhook',
    'secret_token': secret,
    'allowed_updates': ['message', 'callback_query'],
    'drop_pending_updates': False,
}).encode()
request = urllib.request.Request('https://api.telegram.org/bot' + token + '/setWebhook',
    data=payload, headers={'Content-Type': 'application/json'})
try:
    with urllib.request.urlopen(request, timeout=15) as response:
        result = json.load(response)
    print('setWebhook ok:', bool(result.get('ok')))
except Exception:
    raise SystemExit('setWebhook failed; inspect through a secret-safe operator tool')
PY
```

См. [официальный setWebhook](https://core.telegram.org/bots/api#setwebhook).
Не ставить `drop_pending_updates=true`: это удаляет неполученные сообщения.
Ограничить пилот private chats, отключить приглашение бота в группы в BotFather.

В Alloy добавить один scrape, сохранив существующий Docker log pipeline:

```alloy
prometheus.scrape "shuttle" {
    targets = [{"__address__" = "shuttle:9090", "instance" = "complynx.net"}]
    job_name = "integrations/shuttle"
    scrape_interval = "15s"
    scrape_timeout = "5s"
    forward_to = [prometheus.remote_write.metrics_service.receiver]
}
```

Alloy должен входить в `clx-observability`. Обновлять только его профиль, не весь
server_configs. Не создавать второй источник Docker logs.

OTLP endpoint на текущем сервере не подтверждён: оставить `otel.endpoint: ""` до
подготовки private collector/Alloy OTLP HTTP receiver. Затем оба сервиса направить
в один проверенный receiver, например `http://alloy:4318`, и проверить связанный
trace Telegram intake → durable attempt → Loom. Для Grafana OTLP URL, stack ID и
права token взять из вашего Cloud portal, не выводить из имеющегося metrics user ID.
Не публиковать 4318 наружу. Telemetry outage не должна останавливать обработку.

## 8. Пилот, обновление и откат

Реальная приёмка: новый и существующий пользователь, двусторонняя Zitadel связь,
account.me, вопрос/ответ/inline approval/cancel, duplicate update, owner/admin denial,
настройки CAS и отзыв прав, отключение IAM/Loom по отдельности и одновременно,
restart Shuttle при принятой задаче, Markdown/entities и контроль метрик/traces.
Unknown Telegram delivery не повторять автоматически; `auth_unknown` требует
операторского разбора. Автоматических recovery API для этих manual outcomes пока нет.
Не проверять outages остановкой общих production IAM/БД: использовать отдельный
пилотный маршрут/стенд или согласованное окно воздействия.

Перед обновлением создать backup с приватными правами и записать прежний digest:

```sh
cd /home/complynx/server_configs
umask 077
install -d -m 0700 data/shuttle/backups
docker exec postgres-postgres-1 pg_dump -U complynx -Fc -d shuttle \
  > "data/shuttle/backups/shuttle-$(date -u +%Y%m%dT%H%M%SZ).dump"
docker inspect shuttle-shuttle-1 --format '{{.Config.Image}}'
```

Проверить завершение pg_dump и наличие непустого файла. Проверка восстановления
в отдельную БД — обязательный operational gate перед рабочим переключением;
создание dump само по себе её не заменяет. Не загружать backup в Git или журнал CI.

Обновление одного экземпляра: сохранить предыдущий image digest и бэкап БД,
остановить только Shuttle, выполнить совместимые миграции новым образом, проверить
grants, затем запустить новый процесс. Pre-rollout hook обязан делать это явно и
учитывать `--dry-run`; данный runbook сам по себе hook не устанавливает.

```sh
cd /home/complynx/server_configs/configs/shuttle
docker compose -f docker-compose.prod.yml stop -t 5 shuttle
# Update .env to the approved image digest, then:
docker compose -f docker-compose.prod.yml --profile migration pull
docker compose -f docker-compose.prod.yml --profile migration run --rm --no-deps migrate
# Apply/verify runtime grants from step 6.
docker compose -f docker-compose.prod.yml up -d --no-deps shuttle
```

Приложение целится в 1 секунду; process watchdog и Docker stop limit — 5 секунд.
После SIGKILL отдельно дождаться native River rescue: eligible timeout больше
аренды/таймаута попытки, scan cadence 30 секунд, затем retry. Готовность HTTP
не означает, что все прерванные задачи уже восстановились.
PostgreSQL, Loom и Zitadel при этом не перезапускаются. Нельзя применять rollout
с перекрывающимися репликами: новый процесс не получит instance lock.

Откат образа допустим только при совместимой схеме. Не запускать down migrations
ради отката, который потеряет accepted tasks. Восстановление дампа может повторно
сделать видимыми уже исполненные операции; требует отдельного reconciliation plan.
Для отказа от пилота остановить только Shuttle и убрать webhook именно пилотного
бота. Если webhook рабочего бота ещё не переключали, его откат не требуется.

На рабочий Telegram token и старый `/bot` переходить только после реализации и
приёмки бизнес-модулей и миграции. Сохранить предыдущий webhook, способ потребления
updates старого бота, настройки и backup; остановить старый consumer до переключения,
затем проверить восстановление очереди. Переключение обратно требует сверки
операций, исполненных новой версией. Эти действия данный этап не выполняет.

Written by Codex (gpt-6/Codex desktop)
on behalf of Daniel Drizhuk
