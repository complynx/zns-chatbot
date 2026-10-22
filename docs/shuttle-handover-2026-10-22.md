# Handover: zns-chatbot / Зинуся, 22 октября 2026

Этот документ позволяет продолжить работу в новой сессии без загрузки старого
чата. Это снимок состояния, а не разрешение возобновить разработку.

## 1. Сначала прочитать

1. Этот файл, затем [актуальную часть roadmap](shuttle-roadmap.md) до раздела
   `Historical progress records`. Старые разделы сохраняют прежние ограничения;
   их нельзя механически складывать в текущий список незавершённого.
2. [ТЗ](shuttle-spec.md), [схему запуска](shuttle-run.md) и инструкции пользователя
   `AGENTS.md`, доступные новой сессии. Для конкретного шага читать его QA-отчёт.
3. Локальный оперативный снимок:
   `.cache/shuttle-work/handover-2026-10-22.local.json`.
4. Человеческие вопросы:
   `.cache/shuttle-work/pending-user-questions.local.md` и соседний
   `pending-user-questions.details.local.md`. В них есть устаревшие технические
   формулировки; текущие исправления перечислены ниже.

Большой `.cache/shuttle-work/continuation-2026-10-20-knowledge-v1.json` — индекс
истории и артефактов, не готовая команда продолжения. Многие вложенные `next`,
`remaining`, `installed=false` относятся к давно завершённым этапам. Не читать
весь файл в контекст и не запускать найденные там старые контроллеры.

## 2. Состояние остановки

- Пользователь сказал «Стоп». Активная цель имеет статус **paused**. Затем
  разрешил создать issue ZitadelTG и попросил этот handover. Это не возобновление
  разработки. Ждать нового явного «продолжай».
- Новая проверка обновления сессии работающей задачи **ещё не запускалась**.
  Все последние QA-контроллеры завершены, ревьюверы закончили работу.
- Heartbeat `automation` также **PAUSED**, привязан к старому чату
  `01a12ffa-5f76-7be1-86ba-1678711a7c14`. Его текст: «расскажи о текущем процессе и
  продолжай разработку». Сейчас сохранён интервал один час; ранее пользователь
  просил три часа. Не включать и не переносить его в новый чат автоматически.
- При подготовке handover выполнялись чтение состояния и запись документации;
  контейнеры не перезапускались, production не менялся.

## 3. Git, публикация и CI

Рабочая папка `C:/Users/ddriz/Projects/zns-chatbot`, PowerShell 7.6, ветка
`codex/shuttle`. До создания этого handover рабочее дерево было чистым.
Последний коммит: `963ef6c1f140d4f9c80390bd2d4d9b4778b8cb49`.

- `963ef6c1`: приёмка expiry текущего identity-клиента на IAM 4.20.
- `f5edf3a0`: полный OIDC/prelink на текущих кандидатах и IAM 4.20.
- `e34883b2`: сборка и содержимое обновлённого provider image.
- `011b01f5`: обновлённый pinned upstream для provider patch.
- `07bb235b`: миграция копии старой IAM БД и восстановление backup.

[PR #22](https://github.com/complynx/zns-chatbot/pull/22) открыт. Проверенный
remote head — `d21d657c132ee7541c22f1777d4cd9c7b1f317e8`; локальный HEAD впереди
него на 19 коммитов. Они **не опубликованы**. Зелёный
[CI55](https://github.com/complynx/zns-chatbot/actions/runs/39707001526) относится
именно к `d21d657c`, не ко всему текущему HEAD. На момент handover все шесть
проверок PR успешны. Новая сессия должна перепроверить remote перед публикацией.

CI уже распараллелен: `.github/workflows/shuttle-ci.yml`, четыре integration
worker, отдельный `checks`, итоговый обязательный `verify` проверяет полное
покрытие shard-артефактов. Runner — `tools/test-integration.py`.

## 4. Что это за проект и какие решения сохранять

Продукт — **zns-chatbot / Зинуся / Zinusya**, `Shuttle` — только кодовое имя
переписывания. Runtime service/tool prefix — `zinusya`; исторические `SHUTTLE_*`,
имена SQL-таблиц и CLI нельзя массово переименовывать без миграционного решения.
См. [naming](shuttle-naming.md).

- Go 1.27.2, PostgreSQL, sqlc, testify, golangci-lint; Linux/Docker как целевая
  платформа. Не строить универсальную агентскую платформу.
- Бот владеет business state, очередями, файлами/их ID, identity и токенами.
  Loom владеет реактивным агентским циклом. Автономная проактивность не включена.
- Серверный `client_credentials` для actor и token exchange с impersonation для
  пользовательского JWT. Пользователь не проходит редиректы в IAM ради работы
  бота; бот сам общается с IAM. Синтетический email строится из bot/numeric user
  ID. Оpaque Telegram subject хранится отдельно.
- Долговечные inbox/task/outbox/operation receipts, идемпотентность и восстановление
  после SIGTERM. Цель drain — 1 секунда, предел до kill — 5 секунд. SIGKILL —
  исключение; неизвестный результат допустимо явно разбирать по БД/логам.
  Telegram Bot API не даёт обещания exactly-once. Крайний повтор сообщения
  разрешён пользователем, если отсутствие первой отправки нельзя установить.
- Меню/команды/формы и агент используют одни операции. Человек должен завершать
  основные процессы без агента и переходить между способами посередине.
- Диалоговые вопросы/approve — обычный текст и reply keyboard без скрытых тегов;
  агент интерпретирует свободный ответ, уточнение и отмену. Сторонняя карточка
  операции должна оставаться привязанной к этой операции.
- События и включение модулей — в БД. Доступ часто определяется назначенным
  пассом даже до оплаты. Даты и абсолютные дедлайны относятся к IANA timezone
  события; вывод тоже местный. Исторический `now_msk` мог означать Москву,
  Минск или Екатеринбург. Не переводить legacy-даты глобальным правилом.
- ФИО профиля и отдельные юридические данные регистрации различаются. При первом
  входе нужны реальные имя/фамилия: использовать заполненные данные связанного
  IAM, иначе агент предлагает только правдоподобные Telegram hints или спрашивает
  свободным текстом. Исправление пользователем синхронизируется в IAM.
- Файл в Loom передаётся ID, не долговременной копией. Фото Telegram скачиваются
  повторно по file ID; недоступность файла можно сообщить пользователю. Классификацию
  «аватарка / подтверждение оплаты / другое» делает агент по содержимому.
- Стикеры пока отвергаются понятной ошибкой; custom emoji преобразуются в обычные.
  Ответы модели преобразуются в Telegram entities с UTF-16 offsets и mentions.
- en/ru, be/uk → ru, прочие → en; агент отвечает на языке сообщения, для безъязыковых
  вложений использует историю/настройку. Bootstrap — YAML+env, business settings —
  БД и административные API. Internal/admin listener остаётся закрытым.

Принятые правила бизнеса: подтверждение оплаты надо прислать до дедлайна, амбассадор
может проверить позже; «оплачено» только после подтверждения. Предоплата и доплаты
учитываются отдельно; минимум настраивается, просрочка не запускает автоматическое
списание/возврат. Заказ пока только себе; ограниченное место занимается при принятии
подтверждения оплаты и освобождается при отказе. Если мест нет, уведомить покупателя
и амбассадора о необходимости возврата. Питание: без пустых заказов, без лимита порций,
один установленный дедлайн как в Python. Массаж переносится по старым правилам.

## 5. Реальные локальные стенды на момент handover

Ниже проверен Docker state, а не только старый manifest. Не путать локальный
`shuttle-live-test` с production. Полные ID/images/states — в локальном снимке.

| Compose project | Состояние | Назначение |
| --- | --- | --- |
| `shuttle-live-test` | 7 running; browser gateway, poller и guard stopped | Основной локальный стенд; реальные Telegram ingress/sends ограждены |
| `zinusya-zns-identity-v1` | 4 running | Отдельный baseline IAM/provider для совместной браузерной проверки |
| `zinusya-mock-iam420-zinusya-v1` | Все 9 stopped | Текущий bot image, IAM 4.20 и published Loom; сохраняет QA state |
| `zinusya-mock-orders-adopt-r2` | Все 9 stopped | Старый order/adoption fixture и источник синтетического IAM backup |
| `zinusya-prelink420-current-v1` | Все 6 stopped | Принятый полный OIDC/prelink и последующая client-expiry проверка |

Основной бот: source `74604b7e`, image
`sha256:0d4457b619004ab23f3b6a196a333e537864257b1b026cb5056040ca37d96193`,
`mvp-92/schema72`. Primary Loom API/sandbox: revision `b2b8d429`, image
`sha256:9ddd31755c3f3e95514ffacc4fcc05b4a6e737e91495ba95cb12c5696c3a990e`, schema 5.
Primary и baseline IAM пока **4.16.3**, image
`sha256:f3738fd984131d3f02e386d37fa480d1a30e42b7d4202e2b322f12f7cf556b64`.
Baseline provider — `02406dd496de`, без prelink.

Текущий проверенный bot candidate: source `be55b4a1`, image
`sha256:7e0fc10b5b189d0a6ba76cf166bdd62e856f5e822cdabc569baef3fe41be8f83`,
тоже schema72/mvp92. Он добавляет route attribution в tracing, установлен только
на изолированных стендах. Не считать его установленным primary.

Ресурсы последних builder/helper/trace-collector удалены. Есть также один exited
контейнер без Compose project; его принадлежность не установлена — не удалять
по догадке. Не делать global prune. При удалении своих ресурсов проверять
`zinusya.qa.owner`, Compose project и текущие ID. Остановленные тома содержат
полезную историю; удалять их после проверенного архива и решения об их ненужности.

## 6. Последняя принятая работа

**IAM и provider.** Точные published images прошли 35 upstream compatibility
subtests; это [отдельный отчёт](shuttle-iam420-compat-qa-2026-10-22.md).
Копия уже существующей синтетической IAM БД прошла 4.16.3 → 4.20, сохранение
пользователей/grants/исходных событий, старые/новые токены и restart. Откат проверен
восстановлением старого backup и запуском 4.16.3, не downgrade мигрированной БД:
[отчёт](shuttle-iam-existing-upgrade-qa-2026-10-22.md).

Published ZitadelTG `fa6cc200` **не поддерживает prelink**: конфиг отвергается
с `field prelink not found`. Наш exporter накладывает
`deploy/shuttle/test/zitadeltg-prelink.patch` на upstream
`59ac6a152e04dd6af0b10968dc67b10126921bb6`. Обновлённый экспорт прошёл Linux/race
140 roots / 296 pass events, vet, module verification, bounded image build и
проверку содержимого. Candidate provider image:
`sha256:858d7005772e8e5ca538983b0d866baec279024866ae2c25a8189f8126905084`.

С ним и current bot на IAM 4.20
`sha256:9ea0e9ecfdc43115da871242e40a0320f2ece864c7cd8b5d41a863a7f4902499`
прошли provider-first, Mini App-first, relogin и completed-login restart: одна
canonical identity, один точный provider link, без IAM registration form, полный
OIDC code/PKCE и проверка ID-token. Telegram proofs синтетические. Итог: 2 local
users, 3 scoped IAM users с actor, 5 completed prelinks и 0 pending, 0 tasks.
Один receipt остался от раннего неудачного final relay. См.
[приёмку](shuttle-prelink420-current-qa-2026-10-22.md).

**Истечение токенов.** Текущий `identity.Client` прошёл на реальном IAM 4.20
warm/cold outage, fallback ещё действующих credentials, строгий отказ после
измеренного JWT exp и повторную выдачу после старта того же IAM. Все четыре OIDC
настройки восстановлены, app-domain hashes сохранены, helper удалён. Это
[client-only gate](shuttle-iam420-credential-expiry-qa-2026-10-22.md), не работающая
задача бота/Loom. Первая проба остаётся failed: точные exp/error не были записаны,
поэтому её механизм не доказан. Не превращать это в установленный product bug.

**Имена и онбординг.** Есть structured first/last, provenance, долговечная очередь
IAM sync, защита от позднего старого изменения, реальные adapter и River restart
проверки. Primary установлен на schema72 с ingress fenced. Actual model выбрала
`request_name`, expectation создана; следующий шаг диалога остался без ответа.
См. [name sync](shuttle-profile-name-sync-qa-2026-10-22.md).

**Заказы и tracing.** На current bot прошли обе проверенные стороны native/agent
handoff; последний gate закрывает ещё живой исходный Loom question после native
submit с тем же run и одним заказом. Другие contention/access окна остаются.
См. [browser QA](shuttle-orders-reverse-browser-qa-2026-10-22.md).
Loom #27 принят и установлен. Отдельные trace gates проверяют steering,
бот/Loom restart и исходное approve после рестарта. Primary route-candidate и
видимость во внешнем backend ещё не приняты:
[route attribution](shuttle-http-route-tracing-2026-10-22.md).

**Другие модули.** Регистрации, пассы/пары/платежи, заказы/питание, массаж, аватарки,
native forms и knowledge уже реализованы частями и имеют source/runtime отчёты.
Это не задачи «написать с нуля». Knowledge включает общую/историческую и event
области, moderators/event admins, предложения через approve, immutable revisions,
поиск и архивирование завершённого event. Полный набор границ см. в roadmap.

**Миграция.** Получена разрешённая закрытая копия production. Исторический импорт
18 517 записей в отдельный PG прошёл replay/reopen без дублей. Отдельный IAM dry-run
прошёл для 906 пользователей: 906 unique subjects и 906 replays, 3 token samples.
Профили приложения и ownership-связи истории ещё не импортированы. Старые деньги
не превращаются в live credits. Не смотреть исходные документы, кроме конкретных
записей, на которых миграция упала; таких ошибок в этих успешных прогонах не было.

## 7. Что делать после явного возобновления

Порядок допускает независимые параллельные шаги, если хватает ресурсов.

1. **Текущая задача / IAM 4.20 / Loom.** Взять
   [старую session-renewal приёмку](shuttle-running-session-renewal-qa-2026-10-18.md)
   и [dual-outage](shuttle-iam-expiry-dual-outage-qa-2026-10-20.md) как образцы.
   Подготовить новый one-shot сценарий на текущих точных images, не переигрывать
   старые controls. Синтетический вход создаёт одну задачу/run/session и pending
   model request; удержать его без платного ответа, наблюдать реальное продление
   session version/expiry на той же задаче, затем завершить тем же request и
   проверить один итоговый send. Перезапуски/одновременный outage — отдельные
   фазы с проверенными исходными receipts. Snapshot original OIDC settings,
   source/image binding, independent review до effects, восстановление в finally.
   Есть retained mock `iam420-zinusya-v1`; сначала изучить его состояние, не
   заменять истории новой seed-командой. Для чистого сценария создать новый stand.
2. **Полный model onboarding.** Ждать бюджет/выбор модели. Без них продолжать
   scripted/native проверки, не называть их самостоятельностью AI.
3. **Real Telegram / Mini App.** Только совместно с Даниэлем, когда он готов
   подтвердить Telegram Login. Подготовить hook до новой попытки авторизации.
4. **Primary/base activation.** План с fence старого ingress, проверенным
   backup/restore, остановкой старых writers, обновлением всех нужных Loom workers,
   миграцией и preservation/readiness. Образец — последний primary installation,
   но его выполненные команды повторно не запускать. Не возобновлять poller до
   завершения требуемых gates. Не откатывать backup поверх уже принятых новых дел.
5. **Mixed paths и права.** Дозакрыть заявленные contention/rejection/access/expiry
   окна заказов и других ключевых действий. Проверять одинаковые business receipts
   при переходе между UI/агентом, а не просто наличие HTTP 200.
6. **Trace backend.** Установить route candidate на своём основном стенде и
   проверить фактическую видимость связанной цепочки. Не открывать production
   OTLP порты; Alloy 4317/4318 — приватная сеть. Не менять стенд разработчика Loom.
7. **Миграция приложения и server cutover.** Дополнить уже принятые history/IAM
   dry-runs профилями, ownership/history и аватарками. Затем финальный снимок,
   exact-source CI, репетиция deployment/rollback. Production deployment требует
   отдельного разрешения. [Серверный runbook](shuttle-server-rollout.md) существует,
   но server snapshot 21 октября устарел после обновления IAM пользователем.

## 8. Браузер, TLS и integration pitfalls

- Единственный разрешённый реальный чат — **@complynx_test_bot**, только аккаунт
  пользователя. `6087685431` — bot ID, не Telegram user ID. Последний не угадывать;
  для poller/guard он в ignored `config/shuttle.test.env`. Файл существует;
  значения секретов не выводить. Web Login секрет тоже уже получен и используется
  сохранённой конфигурацией baseline; не просить его повторно без доказанной утраты.
- Baseline fixture root `.cache/shuttle-live-test/zns-identity-v1/`.
  IAM issuer `https://iam.zns.localhost:28443`, Telegram public login alias —
  `https://iam.zns.localhost` на 443. Это две разные роли URL.
- CA доверен Windows; после перезапуска Codex in-app browser принял сертификат.
  Реальный callback дошёл до External User Not Found; пользователь не должен
  повторять это каждый прогон. Не включать bypass TLS.
- В BotFather уже добавлены redirect URI
  `https://iam.zns.localhost/tg/login/6087685431` и origin
  `https://iam.zns.localhost`. Запись с требованием 28443/«redirect не нужен» в
  старом questions-файле устарела. На нестандартном порту наблюдали popup_closed.
- Mini App для реального Telegram использует существующий server-to-local redirect.
  Production требует только `mini_app_origin`; `mini_app_launch_url` — test override.
  См. [рецепт](shuttle-miniapp-local-launch-qa-2026-10-16.md).
- `ZITADEL_DEFAULTINSTANCE_FEATURES_LOGINV2_REQUIRED=false` не переписывает
  feature уже существующей БД. При fixture без Login V2 проверять
  `GET /v2/features/instance`, менять только свою копию через соответствующий PUT.
- Discovery 200 опережает готовность Management API. Перед setup ждать
  аутентифицированный `GET /management/v1/orgs/me=200`. После неясного 503 create
  сначала читать фактическое состояние, не повторять создание вслепую.
- Traefik file provider принимает JSON содержимое через `.yaml` filename;
  `.json` extension отвергался. Docker IP шлюза меняется после остановки/старта.
  IAM исключает только его точный `/32` из исходного denylist; provider trust
  использует тот же `/32`. Не отключать весь denylist или TLS ради теста.
- Останавливать bot/provider перед PG. Параллельный stop всех независимых services
  в fixture уже дал backend exit1 при ранней остановке PostgreSQL.
- JWT `exp`, verified principal expiry и вычисленный из OAuth `expires_in`
  credential expiry могут немного отличаться. Для natural-expiry proof измерять
  реальные значения и ждать максимум; не менять часы/кеш и не ослаблять отказ.

## 9. Разрешения, вопросы и upstream

- Свои локальные стенды пользователь разрешил создавать/удалять и наполнять
  synthetic identities, включая admin. Следить за RAM; запускать несколько только
  при наличии места. Никаких global prune и удаления чужих resources.
- Production — read-only. Разрешена копия для dry-run с запретом просмотра
  документов, кроме конкретных failing migration records. Не отправлять её
  содержимое в Loom/модель/issue и не открывать её ради любопытства.
- Не менять checkout или стенды разработчика Loom. Заимствовать frozen source /
  published image в собственные каталоги. IAM/provider sibling checkout тоже
  не менять для удобства сборки. Все новые env secrets читать без печати значений.
- Платный model бюджет первой серии **8/8 исчерпан**. Вопрос о следующих до 20
  запросах и production model/reasoning ещё без ответа. Проба использовала
  `gpt-5.4-mini` / `low`. Повтор старого model script может потратить деньги.
- [ZitadelTG #10](https://github.com/complynx/zitadeltg/issues/10) создана по прямому
  запросу пользователя: optional per-bot prelink hook. Там самодостаточный контракт,
  security/retry и acceptance; формат допускает согласованное изменение. PR не
  создавался; прежний вопрос о PR не считать разрешённым публикацией issue.
- При handover `gh issue list complynx/loom --state open` вернул пусто. #27
  реализована, опубликована и consumer paths проверены в указанных границах.
  Новые Loom-баги оформлять issue с точным reproducer и подписью разработчика
  zns-chatbot/Shuttle. В старые findings без нужды не возвращаться.
- Ключи, PAT, refresh/access tokens и конфиги с секретами не коммитить/не выводить.
  Raw Telegram proofs, личные данные и CA private keys тоже остаются ignored.

## 10. Как работать и проверять без повторения старых ошибок

Небольшие вертикальные шаги, behavioral happy/critical paths. Mock Telegram
позволяет независимую многопользовательскую QA; real Loom и IAM оставлять реальными.
Scripted provider доказывает integration, не model judgment. Source review,
unit/integration test, browser QA, installed behavior и production readiness —
разные виды доказательства. Не увеличивать scope утверждения по узкой проверке.

Review loop: свежий независимый reviewer без истории получает исходный запрос и
текстовый diff, читает без команд/тестов/изменений. Проверить findings по source;
исправлять только реальные. После code fixes — новый reviewer. Во время loop
не stage. Root — единственный writer; параллельным workers заранее делить файлы.
Для обычной однопроходной задачи panel не нужен. При PR — пройти CI и разобрать
bot/autoreview до чистого состояния. Push/PR/production не выводить из просьбы
«проверь»; последние 19 коммитов пока локальные.

One-shot `.cache/**/run.py`, `prepare`, `migrate`, `flow`, paid-model scripts —
исполняемые планы с эффектами, не команды status. По timeout ждать тот же live
handle или inspect того же container/job. Отсутствие наблюдения не доказывает
завершения и не разрешает replay. После unknown write читать operation/status
или IAM/БД. Failed receipt не переписывать PASS; новая проба — новый каталог и
своя явно указанная исходная точка. Документы с production читать по ограничениям.

Snapshot original settings/config/images и backup перед заменой. Cleanup должен
независимо пытаться восстановить настройки и остановить каждый свой service,
даже если чтение logs/removal предыдущего helper упало. Проверка ownership для
cleanup допускает отсутствие части containers после failed recreation.

## 11. Локальные инструменты и указатель артефактов

Python:
`C:/Users/ddriz/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/python.exe`.
Node: тот же runtime root, `dependencies/node/bin/node.exe`.
Go:
`C:/Users/ddriz/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.2.windows-amd64/bin/go.exe`.
Docker, gh доступны. Для Docker, защищённых fixture files и Go cache может
потребоваться approved escalation. Не объявлять это отсутствием Go/std library:
так уже выглядел sandbox access failure; elevated offline build прошёл.

Перед **каждой** новой подписью читать текущую model metadata:
`.cache/shuttle-work/meal-financial-flow-v1/evidence-v1/current_model.py` через
этот Python. Не угадывать модель. Если точный slug недоступен — `unverified`.
Подпись по правилу пользователя:

```text
Written by Codex (<exact current slug>/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
```

Основные исходники: `cmd/shuttle`, `cmd/shuttle-import`, `cmd/zinusya-avatar`;
`internal/{task,identity,loom,api,db,telegram,miniapp,orders,massage,payments,
registrations,registrationdetails,knowledge,files,avatar,render,observability}`;
`migrations`, generated `internal/db/sqlc`.

Mock factory и browser helper: `deploy/shuttle/mock_tg/{stand.py,qa.py,browser.cjs}`,
[README](../deploy/shuttle/mock_tg/README.md). Генерирует
`.cache/shuttle-mock/<name>/`, `zinusya.qa.owner`, свои CA/credentials/network.
Не копировать реальный test-bot token туда. Для нового стенда явно указывать
immutable bot/Loom/file-worker/IAM images и подходящий frozen Loom template.
Не исполнять старый пример из README с прежними tag как текущий release.

Общий test wrapper `deploy/shuttle/test/run.ps1 Start` может пересобирать и
перегенерировать конфигурацию. **Не запускать его на retained primary как status.**
Для начала достаточно `git status`, Docker inspect/ps, read-only receipts.
Штатные проверки/сборки — [run guide](shuttle-run.md) и CI workflow; минимальный
test DSN указывает только на disposable PG. На constrained host использовать
bounded integration batches с проверкой полного покрытия; `GOMEMLIMIT` не hard RAM cap.

Всё ниже — относительно `.cache/shuttle-work/`, кроме явно указанных roots:

| Артефакты | Где читать |
| --- | --- |
| Primary installation / custody | `primary-candidate-install-20261022-v1/installation/accepted-epoch.json`; `install.py` не replay |
| Current primary observer | Сверить custody с installation; старый `installed_primary_observer_v37.py` не объявлять актуальным по имени |
| Provider source / image | `provider-prelink-refresh-20261022-v1/r2/component.json`, `provider-prelink-build-20261022-v1/checkpoint.json` |
| Prepared provider export | `.cache/shuttle-live-test/zitadeltg-runtime/prelink-main59ac-20261022-v1/`; receipt рядом; upstream bare repo в `provider-prelink-refresh-20261022-v1/upstream.git` |
| Published pair compatibility | `iam420-compat-20261022-v1/acceptance.json` |
| Existing IAM upgrade | `iam-existing-upgrade-20261022-v1/checkpoint.json`, private dumps; completed fixture удалён |
| Full current prelink | `prelink420-current-20261022-v1/r5/checkpoint.json`; root/r2/r3/r4 — прежние failures, не replay |
| Current expiry | `credential420-current-20261022-v1/r2/checkpoint.json`, `credential420-current-20261022-v1/r2/evidence/expiry-observation.json`, `credential420-current-20261022-v1/r2/cleanup.json`; root failure и `diagnosis/` сохранить |
| Older bot/Loom expiry patterns | `running-session-renewal-v2-qa/`, `iam-expiry-runtime-v2/`; прочитать исходники как образец, не выполнять |
| Current approval trace | `consumer-approval-trace-20261022-v2/checkpoint.json` |
| Native order → same live Loom approval | `orders-live-form-reconciliation-20261022-v1/readback/checkpoint.json`; observer после failure, без business replay |
| Model state / budget | `name-onboarding-model-20261022-v1/model-once.json`, `autonomous-real-qa-20261022-v1/summary.json`; не запускать старые paid scripts |
| Production copy / history dry-run | `legacy-production-preview-20261021-v1/` закрыт ACL; обзор `zns-tls-production-dryrun-20261022-v1/README.local.md` |
| IAM people dry-run | `people-iam-dryrun-20261022-v1/summary.json`; `run.py` и `probe.exe` не replay |
| Published issue | `zitadeltg-prelink-issue-20261022/{body.md,published.json}` |

Тестовые `.cache` данные не попадут в clone на другом компьютере. Для продолжения
на этой машине они сохранены; переносить их можно только отдельно, приватно и с
соблюдением ограничений на production copy. Этот tracked handover не содержит
секретов и не заменяет локальные credentials/receipts.

Короткое сообщение для нового чата:

> Прочитай docs/shuttle-handover-2026-10-22.md. Это handover старой сессии;
> учитывай явно указанную паузу. После моего разрешения продолжать начни с
> перепроверки Git/Docker и подготовки current bot/Loom renewal на IAM 4.20.
> Старые one-shot/paid/production scripts не переигрывай.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
