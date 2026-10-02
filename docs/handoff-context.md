# Контекст передачи — 2026-09-29

> **Работа возобновлена 2 октября 2026 по указанию Daniel.** Кандидат093 квалифицирован полными исходными775/775 PASS, strict11/2/1287,20exit0; PostgreSQL319.870с/360с. Независимые source QA331 и affected QA332 PASS. Exactea0bfc3e включён в интеграцию, raw67 совпали, пользовательский docs/code-quality.md сохранён. Общая схема091–094 полна; после-мёрж/общий quality, финальные стенды, Functional40 и E execution ещё открыты. Актуальное состояние: [PROGRESS](../management.local/PROGRESS.html), [канбан](../management.local/KANBAN.html), [resume](../management.local/resume-2026-10-02.md), [отчёт093](qa/notification-093-resume-2026-10-02.md). Ниже историческая передача, не нынешнее окружение. QA332/next333; только Codex, без push/production.

> **Текущее состояние — остановка 2 октября 2026.** Разработка остановлена в08:00 Amsterdam. Все рабочие агенты и их сессии завершены; удалены297 контейнеров проекта и168 повторно проверенных устаревших tmpfs IPC-томов, сохранены64 нужных/сомнительных тома, исходники, ветки, образы, сети и доказательства. Контейнеров0. Авторитетные текущие записи: [PROGRESS](../management.local/PROGRESS.html), [канбан](../management.local/KANBAN.html), [handover](../management.local/handover-2026-10-02.md). Последний принятый продукт4b0fcfe4/QA323; схема093 не влита. Замороженныйe5775d22/base39417f09 прошёл независимый source-only QA331, но полный gate FAIL:749 завершённых PASS/775 ожидаемых,26 прежних PostgreSQL-ключей не завершены до360-секундного лимита. Functional40/E NOT RUN; production NO-GO. После явного возобновления исправить timing новых изолированных сценариев без ослабления старых тестов/лимитов, повторить полные проверки и требуемое свежее ревью, затем сведение093 и общая приёмка. Счётчик331, следующий332. ALL Codex, Claude не запускать. Production/push не разрешены. Защитить пользовательское docs/code-quality.md SHA256306E99E1CBAD613CA8F54784F724BE4E6F0397A91660C58735DCCDEE614D6437. Ниже — исторические записи29 сентября, не актуальное окружение.


> **Работа возобновлена 29 сентября.** Текущий порядок — [PROGRESS](../management.local/PROGRESS.html), аудит отслеживается в [реестре](audit-followup.md). Создан preservation commit `1ed6f8fb07698f29314536d98ba44f04f026a7ab`; текущий composed commit — `b99c4822ba9110dde5009e0aac738797cbb5ae66`, source `platform/` отслеживается Git; композиция 1830 файлов в `platform/` завершена; форматирование завершено; full compile и importer compile прошли без запуска тестов; pinned format и пять focused PG прошли, полные native/PG, lint и SQLC gates завершились FAIL; исправления и продуктовые QA остаются. Git — единственный источник правды, новых деревьев-снимков не создаём. Ниже сохранено историческое состояние handover; указания об остановке работ, старом `platform/`, авторитетных snapshot-деревьях и отсутствующих коммитах относятся к моменту передачи. Старая приёмка не повышена; стенды требуют создания заново. По текущему указанию Daniel все разработчики и QA временно работают на Codex. Ротация Opus, вызовы Claude и проверки шлюза приостановлены до явного восстановления; прежнее правило каждой третьей Code QA сейчас не действует. Исторические результаты Opus сохранены, актуальный счётчик — в реестре. Аудит и developer hints независимым QA не передавать.

> **Актуальный статус:** см. [PROGRESS.html](../management.local/PROGRESS.html) и [последний полный baseline](qa/composition-baseline-2026-09-30-07.md). Ниже сохранена история передачи; её номера прогонов и состояние стендов не описывают текущую работу.

## Последующее удаление окружения

После подготовки передачи Даниил явно запросил удаление всех стендов и кешей, затем отдельно подтвердил глобальный Docker prune без именованных томов Touchzouk. Все 72 локальных контейнера zns/synthetic/imported-runtime остановлены и удалены, 64 именованных zns-тома удалены. Локальные synthetic БД и состояние Zitadel больше не существуют. Production не затрагивался.

`qa.local/final-environment-cleanup-20260929/` содержит инвентаризацию и результаты удаления/prune; `host/` — очистку файловых кешей. Изначально исключённые из удаления два тома другого проекта `testing_touchzouk-demo-data` и `touchzouk-test_touchzouk-demo-data` пользователь затем удалил сам, что явно подтвердил. Финальная проверка Docker: **0 контейнеров, 0 образов, 0 томов, 0 build cache**; остались только встроенные сети bridge/host/none. Предыдущие сведения ниже об активных кешах, работающих контейнерах и доступе к стенду описывают состояние **до этой очистки**.

При возобновлении:

1. Проверить сохранённые manifests и выбрать итоговые исходники. Не восстанавливать старые Go-кеши или все исторические стенды.
2. Скачать/pull зависимости и базовые образы, собрать выбранный immutable candidate заново. Старые image IDs из QA не доступны после prune; новые сборки требуют нового binding.
3. Создать свежие synthetic PostgreSQL и Zitadel, применить role bootstrap и миграции выбранного состава, выполнить seed. Compose/adapters/bootstrap scripts сохранены; runtime data и старые контейнеры не являются prerequisite.
4. Сгенерировать новые synthetic credentials/tokens и environment bindings. Сохранённые токены от удалённого issuer и старые DB identifiers не считать действительными. Реальные локальные конфиги/ключи не удалялись и не публикуются.
5. Создать только нужные QA-варианты, проверить Telegram-like UI readiness, затем начать независимую приёмку. Предыдущие отчёты сохраняют историческую доказательную силу, но не означают наличие живого стенда.

## Цель и договорённости

Полный перенос Python → Go/PostgreSQL, disposable forward migrator, identity/runtime, обязательный рефакторинг A–E, функциональный паритет и независимые Code/Functional QA. Linux/Docker без GPU. Приоритет: закончить C–E, затем оставшиеся функции и полный паритет, затем реальная модель/Telegram. Breaking changes внутренних Go-контрактов допустимы; мигратор должен сохранить исходные Python-данные и согласованное поведение.

Все локальные synthetic проверки, контейнеры, Zitadel, тестовые права и данные разрешены. Несовместимые состояния — разные стенды. Это не разрешение разрушать Windows/Codex или менять production. Реальные проверки — только разрешённые тестовый аккаунт и CLX Test Bot после synthetic. Секреты остаются в локальных конфигах; не копировать их в handoff или логи.

Регистрационная очередь: первый запрос, уже определяющий конкретный эвент у проверки открытия продаж; общее меню не резервирует место. Удержание настраиваемое, по умолчанию 10 минут. Истечение переносит незаконченный черновик в конец, сохраняя данные. Повторные действия не продлевают приоритет. Порядок применим к выдаче мест и объявлениям в чат хайпа.

Доставка: общий и адресный cooldown, durable порядок, продолжение после ошибки отдельного получателя; неоднозначную доставку нельзя повторять вслепую. Общая/частная память, доступность инструментов и history/source retirement должны сохранять текущие границы прав.

## Авторитетные деревья и объединение

`platform/` — более старый общий состав, не последние исходники разработки. Не начинать исправления там, полагая, что все новые изменения уже перенесены.

`qa.local/architecture-stage-d-final-composition/source` — ранний D-состав. `architecture-stage-d-c7-composition/source` добавил необходимые C4/C7 зависимости; fixture successor `architecture-stage-d-c7-fixture-repair/source` добавил две передачи Delivery в исходном D001-тесте. Это baseline для отдельных repair-кандидатов.

Root создал `qa.local/architecture-stage-cd-composed-acceptance/source` из точного fixture baseline и запечатанного intake export. `intake-preflight.json`: 55 путей применились чисто, один конфликт `cmd/zns/app.go` решён вручную. Сохранён `runAppServers` с ожиданием завершения Bot; добавлены только retention и свежая авторизация native ingress. Итог после gofmt: 1805 файлов, manifest SHA256 **D64D51CC89FE6BF29B7BE391AEA76B247B00A4AD91BF033C5F51E9A2358E7E4A**. Проверять hash файла `handover-source-manifest.json`, а не повторять значения из сообщений агентов.

Root compile `go test -run '^$'` прошёл для bot/api/appclient/appservices/passbooking/cmd/integration; это compile-only. Первый vet runner ошибочно назвал отсутствующий `internal/registrationintake`; сохранён как runner failure. Исправленный `go vet ./cmd/zns ./internal/passbooking` прошёл, продукт между ними не менялся. Доказательства: `intake-native-terminal.json`, `intake-vet-2-terminal.json`. Ни composed PG, ни lint, ни independent QA пока нет.

Intake handoff содержит только 56 изменённых путей в `handoff/source`, baseline/final manifests и `changed-files.json` с before/after hashes. Не копировать целиком старый intake source. Его 119 baseline lint findings не означают, что их можно игнорировать в итоговом составе; единственный изменённый путь с finding — старая версия runApp. Новый состав должен пройти собственный lint.

App-owned delivery candidate отдельно переносит admission/Begin/receipt continuation из Bot в приложение. Не выдавать Bot доступ к приватным core-таблицам. Не делать HTTP/identity вызовы под SQL-транзакцией. Проверять family ACL даже у ручных карточек без Source, историю, конкретный target/reference/revision и idempotent continuation. Таблица family → owner predicates → locks записана в его `SCOPE.md`. Миграции 082/083 принадлежат intake, 084 — delivery; не переиспользовать номера.

## Проверки и незакрытые дефекты

- C7: `qa.local/architecture-stage-c-repair7-independent-code/report.md` — scoped Code PASS; `architecture-stage-c-repair7-independent-functional/report.md` — **INCOMPLETE**. CLI reviewer завершился, не пытаться возобновлять старый process handle 52135. UI/private memo/doc/history invalidation/tool visibility/bounds/replay/restart дали scoped PASS. Все grant revocations восстановлены; history tombstones и unknown-cost запись сохранены как evidence.
- Нейтральный scope следующего независимого FQA: `qa.local/architecture-stage-cd-composed-acceptance/QA-SCOPE.md`, пока **NOT READY**. Сначала заморозить итоговый состав и стенд. Reviewer получает требования и доступ, без исходников, прошлых находок и подсказок; один input coordinator на стенд.
- Retirement: внешнее удаление памяти прерывало план до второго model call. Исправление проецирует только уже admitted, отредактированный terminal outcome, не перезапускает VM и не открывает ledger. Joined unrelated errors, history/permission retirement и cancellation не должны превращаться в успех. Итоговый HANDOFF содержит final manifest и gates.
- D001: `qa.local/architecture-stage-d-modern-read-repair/HANDOFF.md`. Prepare фиксирует authorized snapshot/cursor; execute перечитывает и сверяет, не меняя admitted request. Ledger identity не ослаблен. Focused RED→GREEN и семь дополнительных случаев прошли. Полный Linux race `qa.local/architecture-stage-d-c7-race/run-20260929T122115513`: ASCII/escaped PASS, d096 FAIL на исходном 5-секундном ожидании quote/update11; cleanup0/drift0. Сэмпл goroutine показывает decode полного orders response при RenderOrders после завершения модели. Это **не полное доказательство причины задержки**. Нужен дальнейший целевой анализ, не увеличение timeout.
- Async fixture repair: четыре прежних ожидания синхронной доставки/успешного retired run приведены к durable контракту с проверками эффекта/replay/privacy. Все четыре PG-сценария прошли, включая EN/RU; source drift0 и cleanup0. Есть конфликт на уровне функций с retirement privacy test.
- E: `qa.local/architecture-stage-e-linux-runtime/REPORT.md` и `architecture-stage-e-linux-importer/REPORT.md`. Штатный Linux runtime под ограниченной ролью, restart того же контейнера, отправка строго после retry_at, удаление реального Linux CLI/QA-образа/бинарника с сохранением runtime и логического состояния прошли в своих составах. CLI image — QA packaging, не доказательство shipped packaging. После окончательного объединения нужны актуальные доказательства и независимые QA.

## Операционная среда

PowerShell 7.6, Windows. Максимум два тяжёлых Go/PG/build процесса одновременно; один pinned lint. `GOMAXPROCS=2`, `GOFLAGS=-p=2`, `GOWORK=off`. Активный Go cache `platform/.go-cache`, pinned lint `platform/tools.local/golangci-lint.exe`; отдельные runners могут использовать `platform/tools.local/go-cache`/`lint-cache`. Проверять env самого runner, не очищать кеш работающего процесса.

Docker read/write иногда недоступны из sandbox из-за npipe/config permissions. Сохранить infrastructure failure и повторить разрешённый конкретный runner с escalation; не ослаблять product tests. Не считать observation timeout завершением job и не запускать дубликат. Исторические session IDs не являются живыми leases после передачи.

Снимок реально работающих контейнеров: `qa.local/architecture-stage-cd-composed-acceptance/handover-running-containers.jsonl`, компактный `handover-containers-summary.json`. Снимок read-only, не обещание текущего состояния следующей сессии. Стенды не были глобально остановлены/удалены при handover. Перед изменениями проверить actual containers и владельцев. Не делать global prune.

C7 stand: проект `synthetic-qa-zns-c-repair7`, БД `synthetic_qa_zns_c_repair7`, UI `http://localhost:19988`, app `http://localhost:19987`, bot80. Идентичности 101/202/303; доступ и управление описаны в `qa.local/architecture-stage-c-repair7-functional/stand/QA-ACCESS.md`. Старый UI не доказывает новый объединённый код. Остальные стенды/общие зависимости перечислены в runtime snapshot; сохранять необходимые source/mounts.

## Очистка и сохранение

Историческая QA-сверка: 1082 документа, 1754 классифицированных Markdown-пути. `docs/qa-evidence-registry.md` и `docs/fqa-scenarios.md`; FAIL/INCOMPLETE/NOT RUN сохранены. Новые evidence после snapshot не становятся автоматически сверенными.

Дополнительная очистка проекта: `qa.local/project-size-cleanup-20260929/REPORT.md`. Удалено 94,99 ГБ / 437 targets, все пути проверены отсутствующими. Размер 148,65 → 54,19 ГБ; concurrent builds объясняют разницу между удалёнными bytes и net change. До этого отдельно освобождены 1,246 ГБ, не включённые в новый результат. Активные кеши, snapshots, proofs, конфиги, модели и окружения сохранены. Не пытаться запускать старый runner, не проверив удалённый путь кеша: создать новый cache path допустимо, но не восстанавливать весь мусор автоматически.

## После передачи

1. Проверить manifests всех нужных деревьев и отсутствие текущих владельцев.
2. Довести delivery boundary до compile/реальных role+ACL+receipt gates; учитывать явные недоделки его HANDOFF.
3. Объединить только именованные deltas с root intake-композицией, разрешить overlap app/services/types и две функции privacy test.
4. Закрыть D001 timing и остальные DEFERRED obligations; пройти конечные Go/JS/sqlc/PG/race/lint проверки без suppressions.
5. Свежие независимые Code QA + Functional QA, end-D architecture audit, затем E и полный паритет. Разрешение production/cutover не получено.
