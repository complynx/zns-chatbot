# Прямой переход Shuttle в production

План от 2026-10-23. Это runbook для подготовки и последующего исполнения оператором,
а не запись deployment. **Серверного пилота нет.** Маршрут Daniel:
локальный тестовый бот → настоящий Telegram login и один paid ASR → после их
приёмки реальный `gpt-6-luna/low` для walkthrough Daniel → окончательный production
cutover единственного consumer. Merge, публикация image, серверная установка и
production cutover сейчас не разрешены. Статус продукта — [roadmap](shuttle-roadmap.md).

## 1. Зафиксированный релиз

| Артефакт | Проверенное значение / граница |
| --- | --- |
| Проверенный checkpoint PR22 | `7b54cb29e254fdaa2f7b4f5f1a827ea800dc71f1`; remote HEAD подтверждён перед этим дополнением. CI39968014604 содержит6 success jobs именно для этого SHA. Перед release повторно сверить новый HEAD/CI. |
| App build source | `035f2a6b0356effff75f3dfce0c776d7551c706a`; tracked context SHA256 `6bc1bf6d9b1df21bae56ea394c1b4190fa9a8b928b5c47de3f259c34337ea2c6`. |
| Local image ID | `sha256:bf1383e85fafd2d328437d1787420cb778c2b4f90ddd01806f62f4c8361a5fbe`; Linux/amd64, UID/GID10001. **Не registry digest**, не опубликован. Нельзя превращать его в вымышленный `complynx/shuttle@sha256:…`. |
| Build-input delta | `git diff 035f2a6b… HEAD -- cmd internal migrations deploy/shuttle/Dockerfile go.mod go.sum` пуст. При новом build-input delta привязать новый image и проверить только затронутые gates. |
| Loom | Source `ba1f4a11237a07edb242735b09edb210e2a7209e`; API/sandbox/proxy `complynx/loom@sha256:cbba225f8491449ab1110f7321d25aa1afb105cb039023daf22f4624550b56f4`. |
| Loom worker | `complynx/loom@sha256:856d5968d9d03f5234ac6a778f93138676b1e67680fd396d6439175d39a6150e`; не смешивать protocol/cache generations. |
| IAM |4.20.0, `ghcr.io/zitadel/zitadel@sha256:9ea0e9ecfdc43115da871242e40a0320f2ece864c7cd8b5d41a863a7f4902499`. Уже приняты узкие критические IAM4.20/prelink изменения; повтор всей матрицы не нужен. |
| ZitadelTG | `complynx/zitadeltg@sha256:8864dd802d83c92a7cb63e044a6e79ea8de5a7c1f12f4d609f5c3f8e5765d431`; published prelink принят, но exact source revision image не доказана. |

Evidence: `.cache/shuttle-work/parallel-acceptance-20261022/file-source-qa/build-intent.json`,
`image.iid`, `release/file-audio-final-head-ci.json`;
[ASR contract QA](shuttle-audio-transcription-qa-2026-10-23.md),
[local pilot QA](shuttle-local-pilot-qa-2026-10-23.md),
[model QA](shuttle-persona-model-qa-2026-10-23.md).
`ff8d4234/87103b82` — предыдущая app baseline для указанного там reuse,
а не artifact текущего релиза. Синтетический ASR не доказывает speech quality;
локальный OTLP не доказывает Cloud ingestion. Старый model campaign HELD49/51
не выделяет автоматически budget следующему walkthrough. Этот план подготовлен
по source/tracked files/сохранённым receipts; новых live/SSH/API/Docker/DB проверок нет.

## 2. Владение фазами и разрешения

| Фаза | Владелец | Действие / разрешение Daniel |
| --- | --- | --- |
| L0 | Локальный агент | Read-only inventory старых стендов, owner/journal/version guards, reviewed reuse. Не replay старого setup. |
| L1/L2 | Локальный агент + Daniel | Подготовить настоящий test-bot login; Daniel входит. Daniel выбрал `C:/Users/ddriz/Downloads/Untitled.opus`; после прямого согласия на exact audio/endpoint original ASR завершился HTTP200, использован1/1 без retry. Native Loom adapter принял text и provider usage duration10s. Human Telegram login/voice ingress ещё pending. Второй paid call через Telegram не требуется. |
| L3 | Локальный агент + Daniel | Только после L1/L2 PASS включить реальный API для walkthrough. Нужны новый bounded budget/расходы; дополнительная автономная серия не подразумевается. |
| P0 | Release agent | Конкретный release change-set/review можно готовить; merge и publication требуют явного разрешения. |
| P1/P2 | Серверный оператор | Production provisioning/config/backup/restore только после разрешения конкретного reviewed набора, включая private data scope и paths. |
| P3 | Migration operator + Daniel | Утверждённое окно freeze/export/final delta и reconciliation, без consumers. |
| P4 | Cutover operator | Отдельное явное production activation/cutover разрешение после всех GO. |
| P5 | Оператор + Daniel | Installed acceptance/monitoring; реальные финансовые и административные действия сохраняют обычный exact consent. |

Технические решения внутри разрешённого scope агент принимает сам. Дополнительного
неопределённого «security owner approval» нет: нужны нормальный TLS и разрешение
на production change. Локальное разрешение не разрешает серверный consumer.
Deployment journal и effect lock имеют одного владельца; параллельные агенты
не исполняют эффекты над тем же ботом/проектом.

## 3. L0–L3: локальная человеческая приёмка

Текущий L0: локальный testbot поднят на app035f/imagebf138, IAM4.20 и published
provider8864; пять сервисов, отдельная организация и ограниченная runtime role.
Сохранённая локальная IAM DB действительно восстановлена в отдельный clone.
Canonical app18382, IAM28443 и Telegram443 проверены; прежние app/gateway
штатно остановлены с сохранением storage/config/cursor. Персональная кнопка
Daniel «Открыть Зинусю» настроена, exact URL readback прошёл. Runtime-role
read-only baseline до входа: users/sessions/tasks/inbox/outbox0. L1 ещё не принят:
реальный signed bootstrap Daniel не наблюдался. Loom и polling не запущены,
платная модель не подключена. Один разрешённый ASR уже использован.

Первый app-controller остановился на лишнем chmod после успешного копирования
ключа. Read-only проверка установила исходные bytes hash, UID100/GID101 и0600;
отдельное проверенное продолжение приняло это состояние без recopy/нового IAM
provisioning. Original FAIL сохранён. Известный internal-only host-bind риск
Docker Desktop устранён до continuation: app остаётся во внутренней сети,
публичный app-порт обслуживает gateway frontend. Финальный handoff прошёл
проверку очереди до/после graceful stop и сохранил два terminal outbox receipts.


Root inventory указал старый `shuttle-live-test` с остановленными Telegram guard/poller
и отдельный `zinusya-zns-identity-v1` (gateway/IAM/provider/PG, local login28443,
доверенный `*.zns.localhost`). Это направление reuse, не выполненная этим документом
live проверка. Сверить exact image/bot/org/issuer, private owner/journals и прежние
unknown outcomes. Не запускать старый bootstrap или poller по имени контейнера.
Привязка к bf138 и совместимой схеме делается reviewed продолжением.

1. **L0, единственный receiver.** Test bot6087685431, уже сохранённый token — не
   спрашивать его заново. Secret-safe tooling фиксирует `getMe`, `getWebhookInfo`,
   pending count и прежний receiver. Выбран один webhook либо один long poller,
   не оба. Рабочий production Python-бот не меняется; test DB не станет production.
2. **L1, настоящий Telegram.** Подготовить exact HTTPS launch origin для выбранного
   клиента Daniel и обычную проверку сертификата. Desktop-local trust не доказывает
   доступность с телефона. Переиспользовать принятый local launch contract без
   выдуманного public DNS. Daniel открывает Mini App из настоящего test bot:
   настоящий initData проходит server-side, пользователь не видит IAM, `/app/me`
   возвращает его owner; повторное открытие сохраняет canonical identity.
   Если этот путь использует provider OIDC — проверить real callback и отсутствие
   Link/Register, не подменять mock issuer. JWT/initData/cookies остаются private.
3. **L2, один actual-provider ASR и отдельный Telegram ingress.** Daniel выбрал
   `C:/Users/ddriz/Downloads/Untitled.opus`. Подготовлен offline Opus→mono16kHz
   WAV, затем exact native transcription adapter→actual provider; cap — ровно один
   paid request после прямого согласия на этот payload/destination. Сохранить source/extracted byte hashes, adapter/model,
   provider receipt, duration/usage и результат проверки смысла транскрипта.
   **Actual-provider adapter proof завершён:** Daniel дал direct consent на выбранный
   audio и `https://api.openai.com/v1/audio/transcriptions`, модель `gpt-transcribe`.
   Original single call вернул HTTP200; exact copied native Loom adapter принял
   text и provider usage duration10s при длительности original audio9.1265s.
   Использован1/1 ASR, retry не было. Transcript Daniel показан, но здесь не
   публикуется; raw receipts/transcript/byte hashes остаются в private custody.
   Денежная стоимость этим результатом не заявляется. Первоначальный отказ tool
   до process execution сохранён как отдельная история; он не заменяет final HTTP200.
   Прежний полный Shuttle→Loom→worker→local TLS ASR peer→transcript/delivery путь
   уже принят и переиспользуется в его synthetic contract scope. Actual-provider
   adapter proof не превращается в real Telegram proof; второй paid call через
   Telegram не нужен. Human Telegram voice ingress/file binding/delivery остаются
   отдельной проверкой реального транспорта с заранее fenced платным dispatch.
   Unknown HTTP не запускает automatic paid retry; сверять тот же request/receipt.
4. **L3, затем реальный LLM.** Только после login/audio PASS подключить test bot к
   `gpt-6-luna/low` с согласованным новым budget. Daniel проходит представление/имя,
   owner read, событие/каталог и подготовку точного действия с понятной native
   кнопкой. Проверяются actual backend results и стиль Зинуси. Не выполнять реальные
   финансы ради smoke. ASR, диалог и reply repair имеют отдельный учёт/лимиты.
   Закрыть admission до включения model-starting Telegram/Mini App/task ingress;
   оставшиеся2 предыдущего campaign не считать новым разрешением.

**GO L:** receipts привязаны к bf138/неизменным inputs, Daniel принимает login,
транскрипт и основной walkthrough; нет неизвестных paid/business исходов.
Иначе production NO-GO. Повтор готовой полной IAM/бизнес матрицы не нужен.

## 4. Несекретная конфигурационная ведомость

«Проверить/выбрать» — обязательное поле execution manifest, не созданный ресурс.

| Параметр | Значение / источник |
| --- | --- |
| Production bot | ID/username рабочего Python-бота получить selected getMe в разрешённом preflight. Не6087685431. Token из существующего private custody. |
| App artifact | Выбранный registry repository/tag→опубликованный immutable repository digest; сверить pulled config ID с bf138, source035f/context hash/PR head. Repository ещё не опубликованный факт. |
| IAM issuer | Checkpoint `https://id.complynx.net`; проверить freshness. |
| Canonical org | Checkpoint candidate329725211047952388; доказать связь с реальными пользователями/906 mapping/provider. Не принимать по имени. |
| Loom project/audience | Checkpoint329873099606458372; reuse после owner/assertion проверки. Второй Loom project не нужен. |
| Shuttle API project | Найти/создать production project с assertions; exact ID после lookup, не из fixture. |
| Actor | Dedicated production machine ID/client ID в canonical org; только `ORG_USER_MANAGER`, `ORG_END_USER_IMPERSONATOR` и caller role. Не `ORG_OWNER`. |
| Exchange/introspection | Confidential Token Exchange client; introspection client именно Shuttle API project. Проверить обе audience, `iss/sub/act.sub/org` и project-scoped roles. |
| Provider | Exact org JWT provider для issuer `https://id.complynx.net/tg`; checkpoint JWKS `https://id.complynx.net/tg/.well-known/jwks.json`. Provider ID ещё не утверждён. |
| Prelink | Выбранный private TLS host/leaf/trust, dedicated audience отдельно от IAM/Loom, random32-byte unpadded-base64url bearer. Backend все5 полей вместе. |
| Public origin | Выбрать production HTTPS origin с `/app/`. Старые `zinusya-pilot.*` не одобренные production names. Existing Python route checkpoint `zouknonstop.com/bot`: разобрать conflicts до switch. |
| PostgreSQL | Existing `postgres:5432`/`traefik-net`; proposed `shuttle`, `shuttle_migrator`, `shuttle_runtime` после проверки свободных имён/owners. |
| Listeners | Public8080; internal9090 private network/при необходимости operator loopback. Нет публичных metrics/admin/pprof/prelink9090. |
| Loom service binding | `loom-api:8080`; exact actor service_subject, `zinusya.service`→runs.create/runs.control/chat.read/chat.events.append/artifacts.read/artifacts.write. |
| Real admission | Per-subject/per-billing policy, actual credits/model/limits; `gpt-6-luna/low`. Не останавливать shared paid provider глобально. |
| Telemetry | Existing Alloy `http://alloy:4318`; private scrape `zinusya:9090`, job `integrations/zinusya`, instance `complynx.net`; final alias закрепить в Compose. |
| Private gateway | Candidate `traefik@sha256:31267173a15b4944e797a76ffd9c419707c8d8b32fe5b610f80cd0cfa05f372d` (local3.6.25 accepted). Pin проверяется при release preparation; не mutabletag. |
| Persistent keys | DB/bot/webhook/prelink/IAM secrets и registration-details key в private custody/backup; не регенерировать durable key на каждом deploy. |
| Admin/ambassador | Exact subject/event grants по [permissions matrix](shuttle-permissions-matrix.md). Bot ID не Telegram user ID Daniel. |

## 5. P0: release и неизменяемый execution manifest

Владелец: release agent. Завершить review/CI для конечного HEAD. После явного
merge/publish разрешения опубликовать штатный image и получить registry manifest
digest. Сверить pulled config ID/platform/UID и наличие `shuttle`,
`zinusya-avatar`, `shuttle-import`. Local image ID/дата публикации не заменяют
source→registry binding. Не пересобирать непроверенный dirty checkout и не
использовать mutablelatest как единственный locator.

Private deployment journal фиксирует owner/host/project, source/context/config
hashes, image digests, bot/org/issuer/provider/client IDs, consumer/webhook baseline,
backups, planned effects и rollback. Перед эффектом fsync pending intent, после —
result/receipt. Один exclusive execution lock. Timeout/потерянный ответ означает
наблюдение прежнего outcome, а не новый create/operation UUID/switch.

GO P0: опубликованный artifact проверен, current CI/review clean, change-set
reviewable, execution manifest без placeholders. Старые QA/FAIL сохраняют версии.

## 6. P1: окончательный production профиль без server pilot

Владелец: deployment operator после разрешения конкретного change-set. Selected
preflight проверяет серверный HEAD/целевые dirty paths, exact images/aliases,
mounts/certs, запас диска и объекты из ведомости. Снимки22/23 октября исторические.
Этот документ не выполнял новых server/live проверок.

Reuse ignored `.cache/shuttle-work/parallel-acceptance-20261022/release/`:
`pilot.compose.yaml`, `pilot-database.sql`, `prelink-route.yaml`,
`provider-hook.fragment.yaml`, `provider-hook.compose.yaml`,
`alloy-zinusya.fragment.alloy` — **только additive фрагменты**. Конечные пути:
`/home/complynx/server_configs/configs/shuttle/docker-compose.prod.yml`,
`data/shuttle/{config.yaml,runtime.env,migration.env}`. Заменить pilot names,
aliases/secrets/бот/DB/org/audiences. Не устанавливать `configs/shuttle-pilot`,
не копировать fixture secrets/данные и не переписывать shared configs целиком.

1. **IAM.** Искать exact owner/project/issuer перед create; совпадение имени
   недостаточно. Scoped actor/exchange/introspection/provider из ведомости,
   реальные tokens/introspection без вывода JWT. Test `prepare.py` не production
   provisioner: он шире разрешённых прав. Не давать actor ORG_OWNER и не включать
   administrator impersonation ради обычного пользователя.
2. **Loom.** Добавить только exact actor service_subject и caller-role mapping,
   сохранив чужие subjects/settings. При startup change нужен reviewed targeted
   API reload; не переустанавливать всех workers без причины. Credits/settings
   хранятся в БД: bootstrap YAML не обновляет сохранённые settings. Проверить
   trusted Docker discovery на actual network и per-deployment admission до
   любого model-starting ingress. Если нужный scoped fence отсутствует, держать
   Shuttle ingress/worker закрытым, а не выключать весь общий provider.
3. **DB.** Создать/reuse роли по [server runbook](shuttle-server-rollout.md#3-отдельная-база-и-роли).
   Runtime NOSUPERUSER/NOCREATEDB/NOCREATEROLE, без permanent schema CREATE и
   membership migrator; archive staging требует TEMPORARY. Миграции — только
   migrator, затем grants существующих tables/sequences и default privileges.
   Existing names не удалять для retry. Test и production bot не делят app DB;
   singleton deployment binding bot/org/issuer не сбрасывается.
4. **TLS/routing.** Public route ведёт webhook/`/app/` на8080. Private prelink
   gateway доступен provider по `zitadeltg-edge`, backend — по `traefik-net`;
   боту не нужен provider edge. Exact host+POST+path
   `/internal/identity/telegram-prelink`, другое404; неверный service bearer401,
   public internal route404. Проверить цепочку/SAN обычным TLS. Нет skip-verify,
   копии чужого private key и публичного9090. В локальном pilot Windows mounted
   bytes иногда оставались неприменёнными watcher: проверять actual statuses,
   а не только file hash/watchtrue; explicit apply/reload имеет отдельный effect.
5. **Provider.** Добавить hook именно production bot к существующему entry,
   сохранив другие bots/issuer/relay/key policy. Baseline hash/image и обратный
   diff сохранены заранее. Targeted provider recreation — явный эффект с
   оценённым влиянием на остальных пользователей. Код8864 не доказывает active
   hook. Dedicated service key присутствует только у этого hook и backend.
6. **Admin.** Выполнить bootstrap из [permissions matrix](shuttle-permissions-matrix.md):
   first application account, роль `zinusya.admin` в Shuttle API и Loom projects,
   Loom `role_permissions.zinusya.admin: [zinusya.admin]`;
   затем event-scoped ambassador через supported API/точный consent. Импортируемые
   пользователи не получают admin автоматически; service actor не человек-admin.

Будущие команды после разрешения; reviewed files/env уже привязаны к immutable
artifact. Эти команды не выполнены:

```sh
cd /home/complynx/server_configs/configs/shuttle
docker compose -f docker-compose.prod.yml config --quiet
docker compose -f docker-compose.prod.yml --profile migration run --rm --no-deps migrate
# Теперь post-migration grants; ingress и consumer ещё закрыты.
```

Обычный `shuttle` запускает runtime и очередь; универсального dry-run server или
production polling-disable CLI не предполагается. Первый boot с закрытым ingress
требует проверенно пустой/разобранной очереди и закрытого model admission.
Не запускать runtime поверх unknown outbox под видом health-check.

GO P1: exact artifact/config/IDs совпали, миграция завершена, scoped permissions,
TLS/private hook/discovery и budget подготовлены. Это ещё не разрешение переключения.

## 7. P2/P3: backup, migration, freeze, final delta

Владелец: migration operator. Доступ и пути private export/backup согласовать
отдельно; parent0700/files0600, Windows ACL dispatcher+SYSTEM. Не передавать
production profiles/history/ключи/снимки модели или в чат. В журнале hashes/counts,
а не строки переписки. Старые one-shot export/import scripts не исполнять заново.

Предыдущая защищённая copy-проверка приняла **906 exact IAM subjects**,
**907 app users с одним synthetic operator**, **18517 history records**,
replay/reopen и неизменность остальных таблиц. Это не нынешние production totals,
не906 HTTP logins. Final target — `N canonical subjects + отдельно учтённые реальные
bootstrap/operator accounts`, а не автоматически907.

1. **Scope.** Согласовать collections/source-instance/owner mapping и archive
   format. Оригинальные даты сохранить без timezone guesses. По [spec](shuttle-spec.md)
   ongoing events для переноса нет; старые регистрации/пары/платежи — history,
   не active passes/новый ledger. Повторно подтвердить это на final source.
   Появившиеся active processes — NO-GO до отдельного плана. Avatar Telegram
   references мигрируют как owner-checked on-demand sources, не обязательная bulk
   копия bytes. `paid_claim` не подтверждает деньги/пасс в новом backend.
2. **Preliminary export.** Новый scoped export по фактическим коллекциям с
   immutable manifest/SHA. Packaged CLI `shuttle-import -manifest ...`,
   `-output ...`, `-verify ... -sha256 ...` только inspect/bundle/verify;
   это не import history. Exact source provenance хранится отдельно от projected
   records. Пользовательские assistant texts остаются недоверенными данными.
3. **Backups.** Legacy Mongo source, target Shuttle PG, соответствующий IAM
   state/objects, изменяемые shared Loom data/configs, persistent keys,
   provider/ingress configs и webhook metadata. Раздельные DB dumps не являются
   cross-system atomic snapshot: записать freeze interval/границы каждого.
   Для PG custom dump full `pg_restore --file=-`, затем restore в изолированную
   копию и count/hash compare; decode не заменяет restore. Mongo dump отдельно
   проверить restore/readability. Shared IAM restore затронет другие приложения
   и не является обычным deployment rollback.
4. **Schema/history.** Проверить новый `-migrate` на копии конечной target schema,
   затем migration/grants по журналу. Supported
   [store_archive prepare/upload/status](shuttle-legacy-archive-storage.md), затем
   exact approved [apply_history](shuttle-history-apply.md). Каждый write имеет
   original operation UUID/proposal/idempotency tuple/SHA и durable receipt.
   Не подменять operator archive≤257MiB Telegram route с лимитом20MiB.
5. **Accounts.** После отдельного IAM reconcile построить exact users source
   record→active canonical subject plan. Применить packaged command в reviewed
   одноразовом container того же release image, без Telegram consumer:

```sh
# entrypoint /usr/local/bin/shuttle-import; config/plan/JWT mounted privately.
shuttle-import apply-accounts -config /private/app.yaml \
  -plan /private/accounts.json -authorization-file /private/operator.jwt
```

   [Контракт](shuttle-history-account-import.md): ≤1000 mappings/≤1MiB plan,
   operator application UUID соответствует current API admin JWT, deployment
   bot/org/issuer и IAM проверяются до/после transaction. CLI не создаёт IAM
   subjects/платежи/подтверждённые имена. Replay тем же plan; stdout `accounts`
   включает replay и не равен числу newly created users.
6. **Freeze.** Закрыть прежний webhook/poller ingress, остановить старый Python
   writer/jobs и прочих writers выбранных коллекций. Записать pending/offset и
   unresolved deliveries; `drop_pending_updates=true` запрещён. Shared IAM/Loom
   глобально не останавливать: обеспечить scoped отсутствие конкурентной записи
   мигрируемых identities. Если это невозможно, не обещать атомарность.
7. **Final export/delta.** Frozen export сравнить с preliminary по source keys и
   canonical hashes. Новый snapshot повторно использует matching records и
   добавляет memberships. Изменение прежнего source key конфликтует по текущему
   [history contract](shuttle-history-apply.md); произвольный upsert отсутствует.
   Если старые records изменились, подготовить чистый final target/reviewed
   mapping до activation. Не обходить конфликт новым source_instance.
   При только добавлениях применить согласованный additive final delta тем же
   supported путём. Account/archive receipts должны ссылаться на final snapshot.
8. **Final reconcile.** Count-only source owners→IAM subjects→app accounts,
   roles/records/memberships/hashes; foreign bot/org, conflicts/duplicates=0;
   bootstrap accounts отдельно; zero новых payments/passes/tasks/outbox от import.
   Любой delta к906/18517 объяснён. Один installed owner-history/avatar-reference
   read и stranger denial достаточны для slice; не906 logins/полная IAM матрица.

GO P3: final frozen manifest, backup/restore/reconcile receipts проверены;
старые writers остановлены, исходники сохранены, unknown write outcomes
разобраны по original status. Иначе новый production consumer не запускается.

## 8. P4: staged sole-consumer cutover

Перед этим Daniel явно разрешает окно/target artifact/change-set. Старый Python
image/config/consumer mode сохраняются в rollback manifest. Test-bot walkthrough
не означает переключение production token.

1. Зафиксировать health/schema/settings/manifest/images, DB deployment binding
   `production bot + canonical org + issuer`, frozen backup и webhook/offset.
   Проверить locks/admission/очереди. Два consumer одного token запрещены даже
   на разных DB: advisory instance lock защищает одну DB, не Telegram globally.
2. Старый Python consumer/writers **сначала** остановлены/fenced. Для unknown
   update/send сохранить original task/operation/update ID. Pending updates
   Telegram имеют конечную retention: окно не бесконечно; queue не сбрасывать.
3. Запустить только release `shuttle`; проверить health/ready, private manifest,
   actor/audience/deployment binding, TLS и owner read. Выполнение ранее queued
   work — новая write boundary даже при закрытом public ingress. Model/ASR/billing
   policy к этому моменту утверждены. Подготовить доступный по нормальному HTTPS
   exact webhook route до setWebhook, сохранив прочие user/model entrypoints fenced.
   До этого runtime не должен неожиданно
   исполнять неразобранные задачи/доставки.
4. Secret-safe Telegram operator tool устанавливает exact production webhook
   `/telegram/webhook` (или один согласованный poller), secret_token из privateenv,
   `drop_pending_updates=false`. Token URL не попадает в shell history/log.
   Сохранить metadata и независимо наблюдать `getWebhookInfo`. Timeout означает
   observe same outcome, не второй switch с новым URL. Уже после успешного
   setWebhook могут поступить новые durable writes: snapshot rollback больше
   не считать безопасным без сверки. После setWebhook
   не включать long polling; после выбора polling не оставлять webhook.
5. Открыть exact public origin/router и per-deployment model admission в порядке,
   указанном manifest. Ровно один production consumer. Daniel делает один настоящий
   вход/обычный запрос и old owner-history read. Сверить task/run/outbox receipts
   и отсутствие duplicate business action. После принятия новой работы старый
   snapshot нельзя автоматически восстанавливать поверх новой БД.
6. После сохранения локальных receipts оставить test consumer неактивным.
   Test token не заменяет production token, DB binding не переписывается.
   Прежний Python профиль оставить остановленным; данные/volumes не удалять.

## 9. P5: installed telemetry и sign-off

Владелец: оператор. Existing Alloy уже имеет `metrics_service` remote write,
private HTTP4318 и Docker logs pipeline. Checkpoint показывает traefik-net и
отсутствие host OTLP bindings; это не Cloud ingestion/alerts acceptance.
Новый collector/Prometheus/Grafana не нужен. Additive target `zinusya:9090`
должен быть единственным для production deployment, без старого pilot scrape.

```promql
count(up{job="integrations/zinusya",instance="complynx.net"})
up{job="integrations/zinusya",instance="complynx.net"}
zinusya_tasks_collection_success{job="integrations/zinusya",instance="complynx.net"}
zinusya_outbox_collection_success{job="integrations/zinusya",instance="complynx.net"}
zinusya_queue_collection_success{job="integrations/zinusya",instance="complynx.net"}
```

Требовать target count1/up1. Каждая семья: collection_success1, last_success>0
и newer120s, snapshot_age≤120s. Missing metrics не означает backlog0.
Проверить linked trace Telegram→Shuttle→Loom→API в existing Tempo, service.name
zinusya и correlation JSON logs без payload/URL/identity. Cached IAM read не
обязан иметь IAM span. Shuttle OTLP HTTP идёт на4318, не gRPC4317; sampling/queue/
timeouts ограничены. Export best effort, бизнес-receipt остаётся durable.

[Семь готовых alerts](shuttle-alerts.md) установить в обнаруженный existing
Grafana Cloud Prometheus/Mimir namespace, сохранив чужие groups.
`configs/alloy/alerts.grafana.json` server-health не wrapper для этого YAML.
Проверить installed group hash/evaluation, query и selected notification route.
Если нужен synthetic firing/resolution с внешним сообщением, Daniel заранее
разрешает recipient/test delivery. Не провоцировать production outage и не
объявлять alerts принятыми из-за одного local promtool PASS.

GO final: L1/L2/L3, provenance, final migration/restore, sole consumer, installed
model budget и Cloud evidence закрыты; Daniel принимает основной walkthrough;
on-call/rollback owner записаны. Непринятый пункт остаётся явным, `/readyz` не
закрывает задачу автоматически.

## 10. Rollback и точка необратимости

| Когда отказ | Допустимое действие | Не делать автоматически |
| --- | --- | --- |
| До freeze/source изменений | Убрать только новый config fragment; Python сохраняет прежний режим | Глобальный restart IAM/Loom/Traefik, удаление чужих объектов |
| После freeze, до новых writes/import | Проверить original webhook/offset; отменить новый ingress, затем снять freeze старого consumer | Одновременные consumers |
| Import выполнен, новый consumer не принял работу | Остановить runtime; после подтверждения отсутствия новых эффектов восстановить только target snapshot/config, старый Mongo authoritative | Shared IAM restore без отдельного impact plan; Down populated receipt migrations |
| Runtime/consumer принял task, отправил reply, создал operation/identity | Fence/drain, snapshot текущего состояния, original receipt/status reconcile; forward fix либо проверенный совместимый binary rollback с той же DB/keys/bot/org/issuer | Старый DB restore: потеряет updates/idempotency/receipts и может повторить действия/уведомления |
| Timeout webhook/provider/IAM write, auth_unknown/delivery_unknown | Сохранить pending/locks/ресурс и выяснить тот же outcome | Новый UUID/subject, повтор send, очистка pending как «лечение» |

Binary rollback проверяется против фактической схемы/cache generation. Прежняя
локальная same-fresh-session current-old-current приёмка относится к
ff8/871↔dd06/a2fa. Она не доказывает bf138→старый Python или произвольный Go image.
До P4 выбрать rollback artifact и проверить его schema/worker compatibility,
либо явно согласовать forward-recovery-only boundary. Goose Down не обычный
откат; guard populated migrations не обходить. Loom cache envelope требует
согласованной worker generation; disposable cache не путать с durable receipts.

**После первой новой записи возврат к Python требует отдельной сверки:** какие
изменения authoritative только в PG и какие уже видны Telegram/IAM. Эквивалентная
обратная миграция PG→Mongo этим планом не обещается. Backup этого ограничения
не снимает. Daniel принимает этот boundary до activation.

## 11. Незакрытые решения и следующий шаг

1. Human Telegram login/voice ingress и основной real-API walkthrough ещё pending.
   Actual-provider ASR выбранного audio завершён HTTP200: native adapter принял
   результат/usage, использован1/1 без retry; второй paid Telegram call не gate.
   [Шесть языковых probes](shuttle-language-model-qa-2026-10-23.md) завершены:
   четыре португальских, белорусский и украинский ответы соответствуют ожиданиям,
   независимый review чист. Они не заменяют human login/walkthrough.
2. Registry repository/immutable release digest; явные merge/publish permissions.
3. Production bot ID, exact public origin/private prelink cert/trust, verified
   canonical org/provider/API project/client/actor IDs. Checkpoint IDs — lookup
   hints; новый canonical org требует identity decision.
4. Final export scope/окно freeze/private destinations, объяснённые count deltas
   против906/18517, подтверждение отсутствия active event migration.
5. Per-deployment shared Loom credits/model admission, real ASR endpoint/model,
   runtime/repair cost caps; не только bootstrap defaults.
6. Выбранный rollback artifact/schema либо forward-only boundary после writes;
   production operator/on-call.
7. Cloud ruler namespace/notification route/test delivery; scoped admin bootstrap
   и ambassador grants по permissions matrix.

Следующее разрешённое действие — локальная человеческая приёмка. После её PASS
агент приносит Daniel exact production change-set, заполненную ведомость,
backup/restore/reconcile receipts и окончательный cutover/rollback план для
явного разрешения. Этот документ ничего не публикует и не развёртывает.

Written by Codex (gpt-6.1-sol/Codex desktop)
on behalf of Daniel Drizhuk
