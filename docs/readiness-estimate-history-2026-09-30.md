# Оценка готовности — 30 сентября 2026

**Production: NO-GO.** Основная функциональность написана; общий runtime, полный паритет, импорт и выпуск ещё не приняты. Текущие задачи — [PROGRESS.html](../management.local/PROGRESS.html).

## Текущая оценка

[Codex, 30 сентября](qa/readiness-codex-2026-09-30.md): функциональная реализация **85–95%**, архитектурная реализация **75–85%**. Это экспертные диапазоны наличия реализации, не доля успешно принятых требований. Формально приняты A и B1; достоверного процента подтверждённого паритета текущего состава нет.

Плановый остаток Codex — **25–45 инженерных дней по 8 часов**, уверенность низкая. Диапазон сохранён: исправления и scoped PASS подтверждены, но нового полного зелёного baseline и Functional QA ещё нет. Это трудоёмкость, а не календарный срок AI-команды; число агентов не делит критический путь линейно.

[Последняя независимая оценка Opus, 30 сентября](qa/readiness-opus-2026-09-30.md): **60–72% общего объёма реализации**, **85–190 инженерных дней** остатка. Нового пересчёта после QA №56 нет. Он использует 20 равновесных пунктов архитектуры, паритета, аудита и выпуска; это другой знаменатель, чем функциональность и архитектура отдельно у Codex. Модель claude-opus-5-5 подтверждена, medium задан явно. Предыдущие оценки ему не передавались.

**Расхождение большое; единый надёжный прогноз пока отсутствует.** Оценки не усредняем. Codex предполагает значительный объём уже реализованных доменов и общие причины integration failures. Opus закладывает гораздо больше работ по паритету, импорту и приёмке. При этом он читал выборку исходников и логов, не проверял соответствие всех hashes и не получил поздние PG №3/4. Его предположение о pending implementation AUD-16/24/56 не учитывает их существующие реализации и scoped proof; таблица паритета также различает реализованное и ещё не принятое. Поэтому 85–190 сохраняем как независимую консервативную оценку с этими ограничениями, а не как подтверждённый новый срок.

Следующий шаг для сближения оценок — по завершённому baseline №4 разложить остаток по одной матрице требований: отсутствует реализация / реализовано, требует исправления / требуется только приёмка. Не превращаем отсутствие найденного оценщиком evidence в отсутствие кода. Предложения Opus заново решать уже согласованные AUD-67, полный паритет или отказываться от fencing не меняют требований и не создают новых блокирующих вопросов.

## Проверенное изменение состояния

Текущий срез: R37/R38 scoped gates и независимые QA №96–97 завершены. Baseline №5 и полный pinned lint выполняются на замороженном составе из 2071 файла; нового общего результата ещё нет. Supervisor PostgreSQL здоров, runtime не запущен. Оценка 25–45 инженерных дней с низкой уверенностью пока сохранена; следующий пересмотр использует результат этого общего прогона, а не сумму локальных PASS.


Текущий срез после QA №95: R35/R36 и root media client fix прошли полный bot/appclient unit с PostgreSQL, affected повтор и финальный scoped lint0issues. Независимые QA №91/92/93/95 чистые в своих границах; QA90 подтвердил history/source SQL corrections. R37 Opus и R38 Codex закрывают оставшиеся AV/source origins. Supervisor stand прошёл static QA94; machine-id probe и state/config volume initialization выполнены, но managed runtime ещё не запускался. Полный baseline и Functional QA не повторялись; оценка **25–45 инженерных дней, низкая уверенность** сохраняется. Нового независимого пересчёта Opus нет.


Текущий checkpoint: повтор R33/R34 завершён — полный bot unit с PostgreSQL PASS (26.873s), bot/integration lint 0 issues. QA №87 addendum подтвердил финальное исправление тестовой конфигурации; QA №88/89 также чистые в своих границах. Начаты R35 Opus (command/order/food/pass) и R36 Codex (media/saved/script). Новая read-only трассировка выявила raw SQL leaves ValidateHistoryInteractions и sourceReferenceCurrent; root воспроизвёл и исправил оба, unit пакетов и scoped lint прошли, независимый QA №90 ожидается. Prerequisite supervisor Compose подготовлен и разобран, но runtime не запущен. Общий baseline и Functional QA по-прежнему отсутствуют; оценка 25–45 инженерных дней с низкой уверенностью сохранена.


Актуальный срез после QA №89: R33 Opus реализовал callback/pass-menu propagation, R34 Codex — остановку notification Deliver/Recover. Code QA №87–88 чистые по проверенному составу; №88 также проверил финальный refactor batch-теста. Первый полный bot unit выявил два failing onboarding subtests, первый lint — два замечания к тестам. Исправления тестов завершены; повтор unit/lint и review финального R33 test delta ещё нужны. R33/R34 остаются в «Сейчас».

Acknowledgement recovery PG №1 — **1 PASS / 0 FAIL / 0 SKIP**, 1.272s; независимый QA №89 без замечаний. Проверены сохранение persisted inbox и повтор без дублирования language receipt. SQL-признак внедрён в ControlPolicy: реальные pacing SQL, полный Telegram intake и supervisor restart этим не доказаны.

Остаточная инвентаризация AUD-23 выделила bot mappers в manual/order/food/pass, media и saved-turn/script. Достижимость каждого кандидата ещё требует проверки. Конфигурация supervisor-стенда подготовлена и разобрана Docker Compose, но реальные образы, prerequisites, coordinator и fault fixture ещё не запущены. AUD-10, новый полный baseline, архитектурные условия, паритет, импорт и Functional QA остаются впереди.

**Оценка пересмотрена и сохранена:** 85–95% наличия функциональной реализации, 75–85% архитектурной; **25–45 инженерных дней по 8 часов**, низкая уверенность. Локальные доказательства не дают основания сокращать общий остаток. Это условная трудоёмкость, не календарное обещание или верхний предел. Независимая оценка Opus в этом обновлении не пересчитывалась. Следующая опорная переоценка — после AUD-23/AUD-10, нового полного baseline и первой Functional QA текущего состава.

Ниже — исторические срезы; статусы заданий и процессов относятся к их моменту.


Последний срез R31/R32: unit legacyfood/massage/derivedmutation/bot прошёл, admission recovery PG №1 — 9 PASS / 0 FAIL / 0 SKIP событий, 3.402s. Scoped lint №3 — 0 issues. Code QA №82 massage, №83 admission и №84 food чистые в своих границах (мелкие test/lint corrections отражены addenda). Bot QA №81/85 потребовали доработки SQL followup и marked-domain fallbacks; исправления и focused tests прошли, fresh QA №86 ещё выполняется. Callback acknowledgement и delivery immediate-stop остаются в реализации. Оценка 25–45 инженерных дней с низкой уверенностью сохранена: общего baseline, supervisor и Functional QA всё ещё нет.

Последний срез после QA №79–80: scanProposal исправлен после воспроизведения raw EOF; пять lint findings устранены. Knowledge unit с PostgreSQL — 0.940s, derivedmutation — 0.134s, scoped lint №2 — 0 issues. Recovery PG №1 — 8 PASS / 0 FAIL / 0 SKIP событий (2.559s): knowledge rollback/replay и derived registration/profile/assignment authority. Свежие независимые Codex reviews №79–80 без actionable findings. COMMIT fault проверяет разрыв до отправки COMMIT, не потерю подтверждения уже выполненной транзакции. R31 Opus и R32 Codex начали legacy food и massage, по 15 файлов. Bot mapper fixes и runtime/Functional QA ещё открыты. Оценка остатка 25–45 инженерных дней сохранена, низкая уверенность; это scoped progress без нового полного baseline.

Далее — предыдущие срезы.

Актуальный срез после QA №78: два Begin в derived registration исправлены, unit пакета (0.088s) и scoped lint прошли; независимый Opus review PASS с Low-пробелом покрытия второй ветви. R29/R30 завершили 19 knowledge source files; unit пакета с PostgreSQL PASS (0.998s), но lint №1 завершился с пятью замечаниями к тестам. Подтверждён незакрытый SQL-origin в scanProposal; новый knowledge rollback/replay integration proof подготовлен, но не запускался. Knowledge Code QA ещё не проведён, следующий номер — 79.

Остаток AUD-23 уточнён: по 15 файлов massage и legacy food, остаточные registration/admission и callback пути, а также потеря положительного SQL-признака в refund/notification/acknowledgement mappers. Инвентаризация — план исправлений, не QA. AUD-10, supervisor proof, полный baseline, архитектурные условия, паритет, импорт и выпуск остаются открытыми. **Оценка пересмотрена и сохранена: 85–95% функциональной реализации, 75–85% архитектурной, 25–45 инженерных дней с низкой уверенностью.** Нового общего доказательства, позволяющего сократить диапазон, нет; свежая независимая переоценка Opus в этом обновлении не выполнялась.

Ниже — исторические срезы; назначения и статусы процессов в них относятся к указанному моменту.

Актуальный срез после R27/R28: account и общий derived Begin/Commit исправлены; unit этих пакетов прошли. Media fixture пришлось исправить по реальному CHECK и QA №74; затем целевой PG прошёл. Account/render/native PG №2 — 11 PASS / 0 FAIL / 0 SKIP событий, lint №3 — 0 issues, QA №75–77 чистые. Начаты две непересекающиеся группы из 19 knowledge SQL-файлов. Это закрывает конкретные origins, но не даёт новой общей приёмки или основания сократить 25–45 инженерных дней: knowledge, massage, food, оставшиеся registration/callback пути, AUD-10 и системные gates ещё впереди.

Ниже — предыдущие срезы со статусами на их момент.

Обновление после R25/R26: полный bot unit с PostgreSQL PASS (26.311s), scoped lint PASS; QA №71–73 без замечаний в назначенных границах. Уточнён следующий остаток: реальные SQL transport failures account воспроизведены в трёх точках (RED, исправление R27), а три media loaders требуют разделить JSON decode и SQL (R28). Для полного supervisor proof подготовлен план отдельного Linux coordinator-стенда; Docker-supervised и systemd доказательства нельзя смешивать. Все эти результаты сохраняют оценку 25–45 инженерных дней с низкой уверенностью: полного baseline и Functional QA всё ещё нет.

Предыдущий срез (исторические статусы): R23/R24 affected unit с PostgreSQL прошли в четырёх пакетах, render PG №6 — 8 PASS / 0 FAIL / 0 SKIP событий (4.510s), lint №8 — 0 issues. Свежий Codex QA №69 подтвердил JSON/cancellation scope R23. QA №70 нашёл один JSON classification defect в knowledge view; root исправление и новые negative controls ещё ожидают запуска, QA №71 завершён без замечаний в source-only scope. R25 Opus и R26 Codex закрывают две оставшиеся группы локального render inventory. Отдельная read-only инвентаризация уточнила 53 файла оставшихся SQL-доменов, включая shared commit и записи при чтении. Это уточнение обязательного остатка, а не доказанная приёмка. Диапазон 25–45 инженерных дней с низкой уверенностью сохранён; нового полного baseline/Functional QA нет.

Предыдущий срез (статусы заданий ниже исторические):

Актуализация после QA №68 и render PG №5: workers unit №4 прошёл во всём назначенном наборе, pinned lint №5 — 0 issues. PG №4 — 8 PASS / 0 FAIL / 0 SKIP (5.068s): native rollback/replay, startup ownership recovery, SQL-then-cancel, render SQL failure после здоровых domain reads и provider control. QA №65 maintenance и №67 native/source — чистые в своём scope.

QA №66 Opus завершён с запросом исправлений: смешение SQL и JSON decode failures, а также лишнее откладывание receipt continuation при обычной отмене. R23 actual Opus 5.5 medium начал исправления; их проверки и свежий Codex review ещё впереди. Ошибка knowledge read из QA №68 воспроизведена реальным PG и исправлена вместе с SQL-записями profile buttons. Render PG №5 — пять сценариев и общий тест PASS (6 событий, 0 FAIL / 0 SKIP, 2.546s), bot/integration lint №6 — 0 issues; независимый review этих последних правок ещё не получен.

Инвентаризация локальных render-цепочек завершена. Остаток разделён на три группы: shared interaction/privacy и orders; media/resumed receipts; massage/pass menus. Найдено также подавление положительного SQL-сбоя в меню настроек модели. Это конкретизированный остаток реализации, не новое требование. Другие SQL-домены, AUD-10, общий supervisor recovery, архитектурные условия, паритет, импорт и выпуск остаются открытыми.

**Оценка пересмотрена по этому срезу и оставлена без численного повышения:** 85–95% функциональной реализации, 75–85% архитектурной, 25–45 инженерных дней остатка при низкой уверенности. Новые scoped PASS уменьшают неопределённость по отдельным механизмам, но свежие дефекты и непроверенная общая композиция не позволяют обоснованно уменьшить диапазон. Нижняя граница предполагает, что оставшиеся integration failures имеют общие устранимые причины; новые системные дефекты паритета или импорта могут вывести работу за верхнюю границу. Диапазон не является обязательством или предельным бюджетом. Следующий содержательный пересчёт — после закрытия AUD-23/AUD-10, полного baseline и первой независимой Functional QA текущего состава.

Предыдущий checkpoint:

Актуальный срез read/authority: замечания №48–51 воспроизведены и исправлены; свежие QA №49/52 чистые. R17/R18 завершили 20 файлов, unit/lint и QA №53–54 прошли. Расширенный PG после fixture fixes дал 86 PASS / 1 FAIL; последний failure экспорта исправлен и целевой export PG дал 12 PASS / 0 FAIL / 0 SKIP, lint чистый, свежий QA №56 без замечаний. R20 Codex сообщил о завершении реализации передачи ошибки фоновой доставки в Bot.Run: код и focused tests готовы, но Go tests/lint/независимый Code QA ещё не запускались. R19 Opus продолжает работу над maintenance/assistant sources; последний статус — живой процесс, правки в восьми разрешённых product files; стабильный checkpoint ещё не получен. Подготовлен, но не запущен PG-сценарий фонового сбоя и восстановления очереди. Дальше нужны другие SQL-домены, AUD-10, полный supervisor recovery и Functional QA. Нового полного baseline нет; оценку не повышаем.

**Пересмотр оценки:** диапазоны Codex 85–95% / 75–85% и 25–45 инженерных дней пока сохранены с низкой уверенностью. Новые доказательства закрывают отдельные участки, но обнаруженный широкий остаток SQL-классификации не даёт оснований сокращать срок или повышать готовность. Оценка Opus выше остаётся датированной независимой оценкой, а не новым пересчётом после QA №56. Надёжный общий срок появится после единой матрицы остатка и нового полного baseline.

Ниже — история проверок от новых к старым. Упомянутые в старых записях активные задания не описывают текущие процессы.

- R15/R16: passbooking unit — **62 PASS / 0 FAIL / 0 SKIP** (0.966s, с реальным PG secondary PaymentProof); notification/serialization PG — **14 PASS / 0 FAIL / 0 SKIP** (8.054s); payment/admin PG — **20 PASS / 0 FAIL / 0 SKIP** (8.412s). Scoped lint — 0 issues, QA №46–47 без замечаний. Доказаны атомарный rollback уведомления, восстановление безопасного bounded serialization retry и сохранение SQL/cancellation/deadline вместо ошибочного forbidden в PaymentProof. QA №48 ещё ожидается. Наборы не суммируются в общий процент готовности и не заменяют Functional QA.
- R13/R14: readsource/passbooking/passes unit — PASS, scoped lint — 0 issues; QA №42 APPROVE, №43–44 PASS. PG №1 дал 31 PASS / 1 FAIL / 0 SKIP: единственный failure — устаревшее ожидание raw SQL text в тесте пары. После замены на безопасный ErrDatabase и проверки отсутствия raw text целевой PG №2 — **4 PASS / 0 FAIL / 0 SKIP**, 1.734s. Сохранены rollback/retry/split-tier assertions; profile local/HTTP recovery и replay прошли. Свежий независимый Opus QA №45 — **ACCEPT**, без блокирующих замечаний. Это не новый полный baseline и не Functional QA.
- Следующий AUD-23 участок bot diagnostics и conversation подтверждён: `aud23-bot-history-pg-1` — **16 PASS / 0 FAIL / 0 SKIP** за 9.167s; bot unit PASS1.174s, все conversation unit PASS0.110s, scoped lint и свежие QA №40–41 — PASS. Проверен выход до модели при диагностическом SQL-сбое и повторная обработка сохранённого inbox после снятия fault; non-SQL recorder failure остаётся optional. Это не OS/supervisor restart. Начаты readsource и booking/profile origins; общие baseline и Functional QA остаются открытыми, оценка не повышена.
- AUD-23 script propagation: реальный SQL-сбой внутри перехватываемого скриптом вызова теперь завершает Run; один SQL attempt, один model call, inbox сохранён, заказ не создан. Расширенный PG `aud23-script-pg-3` — **25 PASS / 0 FAIL / 0 SKIP** за 23.862s; agenthost/bot unit, финальный lint и Code QA №39 — PASS. Delivery/fence получили дополнительные lifecycle fault/success tests и свежий QA №38 PASS. Очистка диагностики отменённого SQL-соединения подтверждена core unit/lint и QA №37. Это scoped proof: остаются SQL origins bot/booking/profile/conversation/knowledge/readsource, полный supervisor restart и Functional QA. Общий baseline не повторялся; оценка трудоёмкости не сужена.
- Последующий identity retry закрыл единственный failure целевого AUD-23 PG №2: `aud23-identity-pg-3` — **12 PASS / 0 FAIL / 0 SKIP**, 5.657s, exit 0; integration lint №4 — 0 issues; свежий Code QA №35 — static PASS. Проверка теперь требует безопасный ErrDatabase и отсутствие исходной ошибки драйвера, сохраняя reservation/recovery и единственное создание provider identity. R9 Opus разбирает script propagation; R10 Codex реализует SQL origins в delivery/fence. Это дальнейший scoped proof, без изменения процентов или прогноза и без нового полного baseline.
- Последний checkpoint AUD-23: R7 Opus (SQL origins orders) и R8 Codex (Core HTTP) завершены. Свежие Code QA №32–34 — static PASS в своих границах; unit девяти пакетов и scoped lint №3 — PASS. Целевой PostgreSQL №2 завершён за 20.457s, exit 1: **31 PASS / 1 FAIL / 0 SKIP тестовых событий**, включая подтесты; пакетный FAIL считается отдельно. Direct orders, registration intake и onboarding SQL → HTTP → Run прошли. Единственный failing test — `TestIdentityProvisioningSQLFailureKeepsReservation`: проверка требует исходный `pgconn.PgError`, тогда как новый контракт возвращает безопасный `ErrDatabase`. Обновление этой проверки должно сохранить доказательство reservation/recovery; до повтора весь участок не считается зелёным. Evidence: `qa.local/go-resume-20260929/aud23-ab-{unit-2,pg-2,lint-3}.*`, отчёты QA №32–34 в `docs/qa/`.
- Остаток AUD-23 уточнён: script propagation, SQL origins в delivery/fence и других доменах, доказательство supervisor restart и восстановления на полном стенде. AUD-10 (ограниченные retries и изоляция сбойного события) ещё не реализован. Эти работы остаются на критическом пути. Проценты и диапазон трудоёмкости Codex **сохранены**, поскольку новые scoped PASS не доказывают общий runtime. Оценка Opus выше — последний независимый отчёт, а не повторная оценка после QA №34.
- Последующий broadcast EN/RU исправлен через реальный worker: PG №3 PASS 4.521s; после выделения неизменного pacing helper PG №4 PASS 4.683s, integration lint №4 — 0 issues, Code QA №30 — static PASS с подтверждением конечного hash. Все сценарии назначенной группы получили целевое подтверждение. Полный baseline №4 всё ещё последний общий результат; его 222 FAIL нельзя уменьшать арифметически по целевым повторам.
- После baseline №4 выполнены две целевые проверки: №1 — 14 PASS / 8 FAIL за 31.100s, №2 — 9 PASS / 4 FAIL за 23.754s (события с подтестами; разные наборы, не суммируются). Во втором остался один сценарий broadcast preparation/results в EN/RU. Order export, proof journey и усиленный pass upload прошли; scoped lint №2 и Code QA №28/29 — PASS. Это продвижение внутри группы исправлений, не новый полный baseline или Functional QA. Остаток архитектуры и выпуска не изменён; диапазоны оценки пока не сужаем.
- Последний полный native/PG №4 завершён: **3355 PASS / 222 FAIL / 28 SKIP** событий, включая подтесты; exit 1, ошибки только в пакете integration, 1408.613s. Panic и незавершённых тестов нет. Состав из 1984 исходных файлов не менялся: ноль изменений hashes и inventory. Полный lint №9 — PASS, независимый Code QA №24 — static PASS. Это новый диагностический baseline, а не общая приёмка. Разность 294 и 222 не означает исправление 72 независимых дефектов: менялись состав тестов и количество подтестов.
- Полный native/PG №3 завершён: **3250 PASS / 294 FAIL / 28 SKIP** событий, включая подтесты, без panic и незавершённых started events. Это исходный baseline до последующих исправлений, не текущий остаток. Старый неполный прогон с 104 FAIL больше не описывает текущую исходную диагностику.
- Общая compile №5, полный lint №7 и SQLC diff №5 — PASS. Refund PG №3 и payment/export PG №4 — PASS. После платежных правок agenthost unit и scoped lint также PASS.
- Code QA №21 (refund) и №23 (восстановление платежного результата) — static PASS. Оплаченная допуслуга снимается, создаётся задача возврата через амбассадора; Functional QA этого сценария ещё нужен.
- Focused PG №1: **58 PASS / 9 FAIL** событий, 70.158s. Новые payment two-run accept/reject, lost-reply и оба reminder cases прошли.
- Повтор №2: **8 PASS / 4 FAIL** событий, 18.012s. Обновление языка платёжной карточки и восстановление удалённой карточки прошли; на тот момент два pass-plan cases оставались красными. Родительские и пакетный FAIL учтены. Последующий focused PG №3: **35 PASS / 3 FAIL** событий за 43.217s; payment ACL и revoked pass-plan прошли, единственный failing leaf — missing_authority. Codex установил, что тест проверял language receipt вместо workflow receipt, и привязал проверки к обеим точным доставкам. Последний focused retry №4 — PASS за 7.324s (вся группа TestPassPlanDeliveryRetryAndRender); последующий Code QA №24 и полный lint №9 — PASS. Functional QA ещё открыт.
- Отдельные Linux race/media/sticker проверки прошли в своих границах. Это не полный Linux runtime, live Zitadel или системная приёмка. Предварительные образы собраны; текущий полный app/fake-стенд и Functional QA ещё не запускались.

## Что определяет срок

1. Устранить оставшиеся причины integration failures и получить свежий полный baseline.
2. Завершить C–E, AUD-10 (изоляция сбойного inbox-события), AUD-23 (выход/restart/recovery), writer fencing и два независимых QA каждого этапа.
3. Подтвердить полный активный паритет и импорт всех доменов с reconciliation и сохранением данных.
4. Пройти Telegram-like EN/RU функциональную приёмку и разрешённые реальные интеграции.
5. Выполнить final rebase, fresh gates, backup/rollback и репетицию cutover; подготовить выпуск для разрешения Daniel.

Продуктовые решения AUD-16/23/24/56/67 получены; сейчас работа не ждёт пользовательского выбора. Неопределённость связана с причинами оставшихся integration failures и объёмом дефектов при системных проверках. Само завершение baseline №4 не позволяет сузить срок: сначала нужны разбор причин и единая матрица остатка. Диапазоны оценки сохранены без повышения процентов готовности.

## История

[Оценка Codex от 29 сентября](qa/readiness-codex-2026-09-29.md) и предыдущая оценка Opus сохраняются как датированные отчёты. Handover-числа 92% реализовано / 78% подтверждено и прежние архитектурные проценты не переносятся на текущий runtime: исторический статус в Git history.


---

## Архив перед обновлением 30 сентября 2026, 08:46 CEST

# Оценка готовности — 30 сентября 2026, 07:54 CEST, после QA105

## Текущий срез — 30 сентября, 08:38 CEST

Свежая независимая Functional QA manual Docker-supervisor slice: PASS. Доказаны SQL-origin app exit1, полная замена группы, owner restart и восстановление той же регистрации без дублей; EN/RU актуальные карточки, persistence и отказ гостю. [Отчёт](qa/functional-qa-supervisor-2026-09-30-01.md). Тестовый fault снят после окончания QA, данные/образы/работающий стенд сохранены. Freeze снят.

Это закрывает конкретный ручной Docker recovery proof, не AUD-23 целиком и не архитектуру B–E. Systemd/reboot, исторический ноль старых DB sessions в момент retirement, реальные провайдеры, broad parity и общий зелёный baseline остаются. R47 Opus готовит AUD-10. Диапазоны 25–45 Codex / прежние 70–155 Opus пока не пересчитаны по полному backlog; production NO-GO.

Предыдущие срезы ниже — история.

## Текущий срез — 30 сентября, 08:27 CEST

R42/R45/R46 выбранные проверки: 24 PASS / 0 FAIL / 0 SKIP, lint 0 issues. Fresh QA109 чистый; QA108 conditional pass без блокирующих дефектов. Его предел доказательства ремонта существующей БД дополнен фактическим scoped proof: /start update1 обработан без повторной отправки, inbox0, одна sent delivery; zns_meter usage/select false. App образ db857c913ea1cb53e60691df53cf4c7a6af573d53f40aefdef16a4439fc5302f запущен, coordinator on-failure/count0.

Одноразовый SQL fault установлен в изолированном стенде. Независимая manual EN/RU supervisor Functional QA начата; пересборок нет, результат pending. Сквозной Suggest/model/live integrations и systemd/host reboot этим не покрыты. Общие оценки пока не сокращены; NO-GO.

Предыдущие срезы ниже — история, текущий статус выше имеет приоритет.

## Текущий срез — 30 сентября, 08:16 CEST

Первый healthy оказался кратковременным. UI /start сохранился в inbox, затем LatestNotice завершился SQL permission denied for schema interaction; app exit1 вызвал повторные замены runtime. QA owner остановлен с restart=no. R46 Codex исследует разрешённую границу доступа; SQL-fault fixture ещё не установлен. Stable runtime/FQA не подтверждены.

R45 memo/curate recovery: 22 PASS / 0 FAIL / 0 SKIP, scoped lint 0 issues. QA107 требует исправить ещё completed Suggest replay; Opus продолжает тот же scope. Общие оценки не сокращены, NO-GO сохраняется.

Предыдущие срезы ниже сохраняются как история; текущие факты выше имеют приоритет.

## Текущий срез — 30 сентября, 08:05 CEST

R41/R43/R44 прошли выбранные unit/PG cases; affected lint 0 issues. Canonical app/coordinator пересобраны и запущены: app healthy, все шесть managed-компонентов работают, owner on-failure/restart count0. Это снимает startup blocker, но не доказывает recovery/FQA.

Общий целевой PG exit1: 40 PASS / 6 FAIL / 0 SKIP событий. Три положительных leaf-сценария knowledge attachment recovery выявили повторный API вызов после завершения (3→4); остальные fail events — родители. R45 Opus исправляет механизм, Codex добавляет quota assertion по QA106. Полный baseline не повторён. 25–45 условных дней Codex и прежние независимые 70–155 Opus не сокращены. Ни общий паритет, ни production acceptance не добавились.

Ниже — оценочная декомпозиция и предыдущий срез 07:54; текущие результаты выше имеют приоритет.

**Production: NO-GO.** Основная функциональность написана, но общий runtime, паритет, импорт и выпуск ещё не приняты. Текущие задачи: [PROGRESS.html](../management.local/PROGRESS.html).

## Обновлённые оценки

| Оценщик | Остаток, инженерные дни по 8 часов | Основание и границы |
| --- | ---: | --- |
| [Codex — текущий пересмотр](qa/readiness-codex-2026-09-30.md) | **25–45** | Условный план при значительном уже реализованном паритете и общих устранимых причинах failures; низкая уверенность |
| [Opus 5.5 medium — свежая независимая оценка](qa/readiness-opus-2026-09-30-refresh.md) | **70–155** | Более широкий объём доработок C–E, полной Functional QA и отдельный резерв на её находки; ограниченная выборка исходников/доказательств |

**Единого надёжного срока пока нет.** Диапазоны не усредняются. 25–45 нельзя использовать как гарантированный срок: он зависит от ещё не доказанных допущений. 70–155 — независимый консервативный прогноз, а не измеренный backlog. Оба диапазона — трудоёмкость, не календарное обещание AI-команды.

Codex включил обычные исправления и повторные QA внутрь рабочих пакетов; Opus отдельно заложил 10–30 дней резерва и больше работ по паритету, C–E и выпуску. Пересмотр после R41–R44 и QA105 не сократил диапазоны: причины нескольких сбоев установлены, но финальные проверки и общая приёмка не добавились. Оценка Opus остаётся ранее полученным независимым срезом; нового прогноза в этом обновлении не запрашивали.

Следующий пересчёт должен опираться на общий список требований со статусами «нет реализации / требуется исправление / требуется приёмка», полный разбор baseline №5 и результаты первой сквозной Functional QA.

Прежние проценты Codex 85–95% функциональной и 75–85% архитектурной реализации не пересчитаны и остаются историческими. Opus оценивает наличие заметной реализации в 75–90% выбранных групп требований. Эти знаменатели различаются; ни одна цифра не означает процент готовности к проду. **Процент принятого текущего состава не установлен.**

## Фактическое состояние

- Полный native Go/PostgreSQL baseline №5 завершён: **4624 PASS / 214 FAIL / 29 SKIP** событий, включая подтесты. Main exit 1; ошибки в readsource и integration. Importer module exit 0.
- Все 2071 исходный файл сохранили хеши и состав. Panic и незавершённых тестов нет. Разность с baseline №4 не равна числу исправленных дефектов: состав тестов изменился.
- Полный pinned Go lint №10: **0 issues**. R37/R38 и независимые scoped QA №96–97 завершены.
- Сборка app/coordinator/helpers завершена. На отдельном стенде PostgreSQL/fake healthy, миграции/fixtures exit 0, 85 migration checksums проверены, UI доступен.
- После этого среза R39/R40 исправили шесть тестовых файлов: целевой unit/PG 12 PASS / 0 FAIL / 0 SKIP, scoped lint 0 issues, независимые QA №99–100 чистые. Общий baseline после правок не повторялся; диапазоны оценки не сокращены.
- R41 (Opus): orders fixtures исправлены, Code QA101 чистый. Целевой PG выявил продуктовый legacy-export сбой; связку с R44 ещё нужно проверить.
- R42 (Codex): исходные knowledge-группы прошли PG; v2 добавила проверку доставки и attachments. Opus QA105 требует усилить доказательство отсутствия дополнительного сообщения при completed replay; также отмечены пробелы privacy/отказов. Финальный прогон и свежий review впереди.
- R43 (Opus): подтверждён startup-дефект — strict config отвергал служебный ZNS_INSTALLATION_ID до lifecycle admission. Исправлено отделение двух runtime-переменных с сохранением строгих проверок. Code QA103 чистый; unit/lint, пересборка и запуск ещё впереди.
- R44 (Codex): воспроизведены и исправлены пустой pending-export reply и устаревший успех после отзыва прав. Code QA104 чистый; финальные unit/PG/lint и функциональное доказательство ещё впереди.
- Owner exact resume: staged probe подтвердил повышение history generation при скрытии устаревшего assistant reply после отмены бронирования; последующая reconciliation закрывает исходную операцию. Исправление ещё не реализовано; настоящие privacy/history revocation fences необходимо сохранить.
- Supervisor: Docker start всех шести компонентов успешен, затем app exit1 до рабочего старта; пять helpers работали. Канонические образы ещё не включают R43/R44 и token 999:sandbox. QA owner остановлен с restart=no. Нужны пересборка, обновление fake, возврат on-failure, SQL fault/restart/replay и независимая Functional QA.
- FoodCSV и остальные группы baseline failures остаются открытыми. Общего нового baseline и Functional QA нет.

Сырые доказательства: qa.local/go-resume-20260929/native-pg-all-5-summary.json, native-pg-all-5-failures-skips.json, full-lint-10.log; qa.local/aud23-supervisor-20260930/images.env и журналы сборки/миграций. Детали этапов: [реестр аудита](audit-followup.md).

## Как читать независимую оценку

Opus запускался отдельным read-only процессом claude-opus-5-5 с явно заданным medium; предыдущие прогнозы и QA findings ему не передавались. Он увидел финальный FAIL integration, но полный итог runner появился после его запуска; итоговый summary добавлен к отчёту отдельно, без выдачи дополнения за новую оценку Opus.

Отсутствие доступных оценщику QA-отчётов не означает нулевую историческую приёмку. A и B1 сохраняют прежнюю scoped acceptance. Упомянутые им старый preliminary image и нулевые parse-only digest не описывают новые images.env; migrate/fixtures фактически завершились exit 0. Эти уточнения не закрывают его основной вывод: общего runtime/recovery/FQA proof ещё нет.

Предполагаемые новые вопросы из отчёта не становятся автоматически требованиями к Daniel. Coordinator — отдельный deployment owner с Docker socket; managed runtime его не получает. Полный паритет и сохранность данных уже обязательны: сначала проверяем существующие данные/реализацию, не предлагаем отказаться от импорта или принять потери. Конкретные продуктовые развилки, если появятся, попадут в «Вопросы для решения». Сейчас открытых продуктовых вопросов нет.

## Критический остаток

1. Разобрать baseline №5, завершить AUD-23 и получить зелёный общий состав.
2. Реализовать AUD-10, доказать supervisor recovery и оставшиеся условия архитектуры C–E, включая fencing.
3. Подтвердить активный паритет независимой Telegram-like EN/RU Functional QA, затем реальные интеграции.
4. Финальный импорт и reconciliation всех доменов, работа без импортёра, сохранность данных.
5. Final rebase, fresh gates, backup/rollback и репетиция cutover; затем разрешённый выпуск.

[История предыдущих оценок и проверок](readiness-estimate-history-2026-09-30.md). Built, tested и accepted остаются разными статусами.

Written by Codex (gpt-6-astra/Codex)
on behalf of Daniel Drizhuk
