# Выполнение внешнего аудита — 29 сентября 2026

Рабочий реестр разработчика по [исходному аудиту](audit-2026-09-29.md). Аудит и этот реестр не передаются независимым Code QA / Functional QA: рецензенты получают нейтральные требования, scope и разрешённый доступ. Исходный аудит и исторические QA не изменяем.

Статусы: **текущее** — работа начата; **ожидает сверки** — находка ещё не проверена на итоговом составе; **решение** — нужен продуктовый или эксплуатационный выбор; **проверено** — есть актуальное доказательство закрытия. Сохранение исходников AUD-01 выполнено; продуктовые находки ещё не закрыты. Сохранённые `[CD ✓]` в аудите подтверждают наличие проблемы в прежнем кандидате, не её исправление.

## Текущий состав

Текущий composed commit: `b99c4822ba9110dde5009e0aac738797cbb5ae66`. Preservation commit: `1ed6f8fb07698f29314536d98ba44f04f026a7ab`. Он сохраняет handover; композиция C7/D + intake и отдельных дельт в `platform/` завершена: 1830 файлов, четыре delivery/intake пересечения и privacy test объединены, sqlc дополнен вручную. Удалены 45 устаревших исходников (сохранены в preservation commit), сохранены 11 вспомогательных файлов. Локальный отчёт: `qa.local/go-resume-20260929/composition.json`. Форматирование завершено. Первый full compile выявил test-only цикл `agent test → api → botdelivery → interaction → agent`; root удалил ненужную зависимость `model_settings_internal_test.go` от `api.JSON`, сохранив JSON-ответ. Повтор full compile пяти authored roots и compile обоих пакетов tools/migrate прошли (exit 0), без запуска тестов. Итоговые gates и продуктовые QA ожидаются. Весь текущий source `platform/` отслеживается Git; `git diff --check` и credential-pattern scan 1104 файлов прошли без находок. Проверки привязаны к composed commit; новых деревьев-снимков не создаём. До compile/tests/PG/lint общего состава новые независимые кандидаты не открываем.

## Реестр

| ID | Статус | Следующее действие / условие закрытия |
| --- | --- | --- |
| AUD-01 | проверено: сохранение | Composed commit `b99c4822ba9110dde5009e0aac738797cbb5ae66`, текущий platform source tracked; diff check и credential-pattern scan 1104 файлов без находок. Медиа сохранены по provenance. Переносимость evidence остаётся AUD-63, продуктовая приёмка отдельно. |
| AUD-02 | текущее | Один состав в Git; общие compile/tests/PG/lint и QA. |
| AUD-10 | текущее | После композиции подтвердить механизм inbox; классификация ошибок, bounded retries/backoff/dead-letter, 403 и retry_after proof. |
| AUD-11 | ожидает сверки | Измерить цикл reconcile, обмен токенов и рост messages; ограничить ненужную работу с доказательством актуальности UI. |
| AUD-12 | ожидает сверки | Проверить callback-only префиксы и свободный текст агента. |
| AUD-13 | ожидает сверки | Проверить старые unpriced attempts и месячные границы бюджета. |
| AUD-14 | ожидает сверки | Проверить сохранение причин ошибок reserveBound и состояние транзакции. |
| AUD-15 | ожидает сверки | Проверить восстановление зависших reserved после сбоя. |
| AUD-16 | ожидает сверки / решение | Измерить reconcile под event lock; отдельно согласовать снятие оплаченной допуслуги и возврат. |
| AUD-17 | ожидает сверки | Проверить ограниченную конкурентность scriptservice и поведение занятого слота. |
| AUD-20 | ожидает сверки | Проверить scrub RemoteSecret и provisioning ClientSecret. |
| AUD-21 | ожидает сверки | Проверить ответ callback при ошибке. |
| AUD-22 | ожидает сверки | Проверить повторы после частичной отправки prompt/report/settings. |
| AUD-23 | ожидает сверки / решение | Проверить эксплуатационный контракт fail-fast при временной ошибке БД. |
| AUD-24 | решение | Согласовать начало срока pass invitation для долго ожидавшего пользователя. |
| AUD-25 | ожидает сверки | Сопоставить политику unknown/retry двух рассылок с durable delivery контрактом. |
| AUD-26 | ожидает сверки | Проверить пропуск 042 и обнаружение исчезнувших применённых миграций. |
| AUD-27 | ожидает сверки | Проверить порядок авторизации pass queue и чтения. |
| AUD-28 | ожидает сверки | Подтвердить UNIQUE контракт legacyfood loadOrder в итоговой схеме. |
| AUD-29 | ожидает сверки | Проверить fixture/fake команды и production packaging. |
| AUD-30 | ожидает сверки | Проверить реальную изоляцию памяти скрипта и разбор оболочки. |
| AUD-31 | ожидает сверки | Проверить bounded admission WebM/WebP до полного декодирования. |
| AUD-40 | текущее, реализация внесена | CI/quality runner расширены; syntax, ESLint и Prettier прошли. Свежий Code QA №1 — static PASS; actual Go gates и runtime proof ещё не подтверждены. |
| AUD-41 | текущее, реализация внесена | CI/quality/dependabot для tools/migrate обновлены; syntax, ESLint и Prettier прошли. Свежий Code QA №1 — static PASS; actual Go gates и runtime proof ещё не подтверждены. |
| AUD-42 | ожидает сверки | Проверить pinned images/actions и ffmpeg. |
| AUD-43 | ожидает сверки | Проверить evaluator healthcheck и readiness dependency. |
| AUD-44 | ожидает сверки | Проверить YAML rc, CI timeout и повторные workflow runs. |
| AUD-50 | ожидает сверки | Сверить остаточные loopback HTTP пути с appservices и авторизацией. |
| AUD-51 | ожидает сверки | Завершить ownership/host-policy Bot по scope C. |
| AUD-52 | ожидает сверки | Сверить дубли outbox с итоговым владельцем delivery. |
| AUD-53 | ожидает сверки | Проверить обратные зависимости и правила слоёв. |
| AUD-54 | ожидает сверки | Сверить дубли agent/agenthost и обоснованность отдельных пакетов/образов; не переименовывать автоматически. |
| AUD-55 | ожидает сверки | Перед первым релизом согласовать итоговую baseline-схему и importer proof. |
| AUD-56 | решение | Сохранить явное решение по Zitadel runtime; проверить trusted in-process auth без лишнего обмена. |
| AUD-57 | ожидает сверки | Доказать rollback/cutover, полный импорт, реальные разрешённые интеграции и CPU limits. |
| AUD-60 | текущее | PROGRESS.html — текущий plain HTML, PROGRESS.md — только ссылка; прежнее содержимое сохранено дословно в handover-status. Проверить актуальность ссылок и задач. |
| AUD-61 | текущее | Согласовать принадлежность D-001–D-007 этапу C; существующие IDs пока сохранить, массовое переименование не выполнено. |
| AUD-62 | ожидает сверки | Сверить runtime docs с кодом; архивировать только доказанно заменённые документы. |
| AUD-63 | ожидает сверки | Проверить qa.local ссылки и сохранить необходимые доказательства с commit binding. |
| AUD-64 | ожидает сверки | Проверить junction inventory; только адресная безопасная очистка при необходимости. |
| AUD-65 | ожидает сверки | Проверить reviewer helper scope; не отправлять аудит или fix hints независимым QA. |
| AUD-66 | ожидает сверки | Не переносить qa.local целиком; проверить секреты и явно synthetic credentials в коммите. |
| AUD-67 | решение | Уточнить необходимость отдельных Python worktrees для production; сохранить их изменения. |

## QA routing

Считаем **запросы** свежих Code QA с возобновления 29 сентября, начиная с **1**; следующий запрос — **6**, завершено **4**. Перед отправкой root фиксирует номер, scope и модель/канал, затем результат и ограничения. Каждая третья (3, 6, 9, …) идёт через **claude-opus-5-5**, если SSH Claude tunnel и модель доступны. CLI/tunnel доступны; точное configured model — `claude-opus-5-5`, реальный вызов подтверждён `modelUsage.claude-opus-5-5.canonicalModel=claude-opus-5-5`. Недоступность и выбранный маршрут записывать явно, не объявлять иной маршрут эквивалентным. Functional QA — отдельный свежий агент. Аудит, история и подсказки исключены из обоих контекстов; reviewer получает нейтральный scope отдельно от этого реестра.

| Запрос | Scope | Маршрут | Результат |
| --- | --- | --- | --- |
| 1 | Только четыре infrastructure файла: CI workflow, dependabot, quality.mjs, compose.yaml; frozen hashes в developer report | `/root/code_qa_01`, gpt-6-astra / Codex | [PASS, static only](qa/code-qa-2026-09-29-01.md); runtime gates и FQA не выполнялись |
| 2 | App-owned botdelivery, appclient/API, domain helpers, migration 084 и restricted-role tests; composed commit b99c4822 | `/root/code_qa_02`, gpt-6-astra / Codex | [CHANGES REQUIRED, P1](qa/code-qa-2026-09-29-02.md), static read-only; runtime proof отсутствует |
| 3 | Terminal memory projection, immutable modern reads, async fixtures | `/root/code_qa_03_opus`, read-only Claude launcher, exact `claude-opus-5-5` | [CHANGES REQUESTED](qa/code-qa-2026-09-29-03.md); actual model подтверждён, static review без runtime proof |
| 4 | Delivery application getters и domain constants/refactors, profile transient admission, registration lock union, restricted-role regression и SQLC | `/root/code_qa_04`, fresh Codex | [FAIL, один P1](qa/code-qa-2026-09-29-04.md): opaque inherited registration event lock order; исправление с реальной регрессией в работе |

Запрос №5: `/root/code_qa_05`, свежий read-only Code QA memory retirement repair; шесть файлов заморожены, результат ожидается. Следующий №6 обязан идти через Opus по правилу.
## Текущее окружение и проверки

Synthetic PostgreSQL `synthetic-qa-zns-resume-postgres-1` healthy, локальный порт `55432`; только synthetic данные. Продуктового Telegram-like FQA стенда ещё нет. Full compile пяти authored roots и обоих importer packages прошёл после исходной test-only ошибки; это compile-only. Pinned formatter 2 завершён с exit 0. Пять focused PG-сценариев прошли без skips за 8.187s: delivery split-role/restart, delivery revocation, native retention FIFO, external memo deletion before next plan, memo-delete list retained. Это developer proof, не Functional QA.

Полные native/PG всех roots (session 53470) завершились exit 1 на composed commit `b99c4822ba9110dde5009e0aac738797cbb5ae66`: 2237 pass events, 104 failed test events, 26 skipped; integration 477.001s. SQLC diff также exit 1 из-за типа botdelivery NotBefore; correction ожидается. [Baseline и developer triage](qa/composition-baseline-2026-09-29.md), не для независимых reviewers. Строгий pinned `lint-all-1` завершился с exit 1: 47 issues — gocognit 3, goconst 40, govet 1, mnd 1, nestif 1, whitespace 1. Исключений не добавлено. Source разморожен для исправлений: `delivery_repair` — authenticated getter и restricted-role regression; `delivery_lint` — constants/helpers; `inbox_diagnosis` — три identity fixtures; `claude_access` — profile/registration defects; `memory_retirement_fix` — воспроизведение Opus findings и регрессии; root — SQLC config/test lint. Новые исправления ещё не приняты. Независимый `/root/functional_qa_cd` подготовил план только по исходным требованиям и ждёт стенд; выполнение и PASS отсутствуют.

## Product decisions

Открыты AUD-16 (снятие оплаченной допуслуги и возврат), AUD-23 (fail-fast при временной ошибке БД), AUD-24 (срок приглашения после долгого ожидания), AUD-56 (роль Zitadel в runtime), AUD-67 (нужны ли отдельные Python worktrees для production). Сначала собрать зависимые факты и конкретные варианты. Ни один вариант не считается одобренным молчанием; независимая работа продолжается.
## Проверка закрытия

Для каждой находки записывать commit, актуальное доказательство и ограничения. Тесты не заменяют независимую приёмку; исторический PASS не переносится автоматически на композицию. Подтверждённые дефекты исправлять, затем повторять затронутые проверки и свежие reviews. Оценки обновлены отдельными Codex/Opus assessments; см. сравнение ниже.

## Opus readiness hypotheses — unverified

Отдельная [оценка готовности Opus](qa/readiness-opus-2026-09-29.md) не является Code QA №4. Read-only выборка без runtime proof требует developer-проверки следующих подозрений: fencing активных writers после потери admission; восстановление после bot-wide paused/parked delivery; достаточность lock/reauthorization у CompleteRegistration; полнота повторных проходов bounded memory/history reconciliation и роли runtime. Не объявлять их подтверждёнными дефектами и не передавать независимым reviewers как hints. Рекомендации о выборе механизма — инженерные варианты, не автоматические продуктовые вопросы.

Текущие сравнительные оценки и причины расхождения — в [readiness](readiness-estimate.md); прежние handover проценты больше не являются текущим прогнозом. Решения о пропуске обязательных gates не приняты.
Текущая разработка после baseline: SQLC PASS; lint 2 — 16 issues вместо исходных 47, без suppressions. Profile/registration focused 5 PASS, но новый P1 Code QA №4 открыт. Food immutable admitted-request defect доказан, исправление ожидает PG; identity delivery receipt context в работе. Эти результаты не меняют исторический baseline и не означают приёмку.