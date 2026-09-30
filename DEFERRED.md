# Отложенные дефекты

Правило согласовано Даниилом 28 сентября 2026: откладывать можно и серьёзные дефекты — только до финализации рефакторинга архитектуры, и если понятно, какое изменение новой архитектуры их устранит. Тяжесть сама по себе не запрещает перенос. Решение о переносе фиксирует агент без повторного согласования обычных технических шагов.

Для каждого переноса нужны:

- Воспроизведение, влияние и ссылки на сохранённые доказательства.
- Причина дефекта и конкретное архитектурное изменение, которое устраняет эту причину. Одного намерения «исправить при рефакторинге» недостаточно.
- Целевой этап, владелец и проверка закрытия, включая повтор исходного сценария и затронутые независимые QA.
- Что можно продолжать сейчас, какие проверки остаются невыполненными и какие ограничения действуют до исправления.
- Влияние на итоговую приёмку и выпуск. Перенос не означает исправление или успешное прохождение проваленного теста.

Можно продолжать зависимый рефакторинг с явно отложенным дефектом; этап в таком случае не получает безусловную полную приёмку. Отложенные требования остаются в главной цели. Не создавать временную совместимость со старым внутренним Go-контрактом только ради промежуточного зелёного результата, если согласованная новая архитектура этот контракт заменяет.

Отдельно согласован перенос недостающих функций после C–E. Их список и критерии сохраняются в [инвентаризации паритета](docs/parity-current.md); до окончания рефакторинга реализуем только необходимое для новых архитектурных границ. Затем закрываем функции, полную продуктовую приёмку и реальные интеграции. Реализованное, но ещё не принятое поведение учитывается отдельно от отсутствующей функциональности.

## Принятые переносы

Найденные и уже исправленные дефекты кандидата10 остаются в текущих проверках.

### D-001 — таймаут d096 при параллельном race-прогоне

**Статус:** перенос явно разрешён Даниилом 28 сентября 2026 («d096 точно можно»). Не блокирует переход к следующему рефакторингу. Владелец: основной агент. Целевой этап: C4, владение агентским выполнением и чтениями.

Актуальное доказательство 30 сентября: [R79 native runtime diagnostic](docs/qa/r79-runtime-diagnostic-2026-09-30.md) завершил все 91 действие исходных сценариев с прежними проверками данных, прав, receipt и restart; все Run завершились. Максимум — 7,07 секунды, 51 действие превысило исходные пять секунд. Итог прогона сохранён как FAIL; других ошибок в этом диагностическом прогоне не было. Это не race-приёмка и не закрытие D-001. Продуктовые бюджеты и исходные таймауты не изменены; независимый QA и проверки узкой оптимизации пустых SQL-чтений не доказывают исправление задержки.

Дополнительный [R94 scoped runtime](docs/qa/r92-r94-validation-2026-09-30.md): первый целевой прогон с параллельным lint пропустил пятисекундный предел большого ASCII-каталога. Изолированное наблюдение завершилось за 3,56 с, повтор полного исходного целевого набора без другого тяжёлого задания дал 15 PASS. Причина первого превышения не установлена; эти результаты не заменяют исходный параллельный race-прогон и не закрывают D-001. Лимиты не изменены.

`TestModernChoiceFullRuntimeAcrossRestart/d096` воспроизводимо пересекает пятисекундный предел при параллельной обработке больших ответов. Работа затем завершается; нарушения данных или прав не установлены. Повторная загрузка и сериализация выбора на подготовке quote измерена в isolated baseline/candidate; первоначальный candidate прошёл исходную race-пару, но последующие проверки обнаружили изменение denied/omitted semantics при удалённой истории и изменённом catalog/order. Этот candidate не принят. Уточнённое исправление сохраняет текущие domain checks и передаёт компактные domain-owned fingerprints; прежние цифры HTTP/allocations не являются результатом окончательной версии. Требуются новые измерения, исходный полный тест и независимый QA.

Доказательства: `qa.local/architecture-stage-b-repair8/runtime-d096-diagnosis/DIAGNOSIS.md`, исходный полный прогон и сохранённые повторные запуски. Это локальные QA-артефакты, не файлы для публикации.

В C4 устранить повторную загрузку и сериализацию quote при подготовке и исполнении, сохранив актуальную авторизацию перед исполнением. Отдельно измерить вклад этого изменения; разрешение на перенос не доказывает, что одной этой оптимизации достаточно. Проверка закрытия: исходная пара сценариев с теми же assertions, race и параллельностью, затем общий прогон. Результат исходного полного прогона остаётся проваленным, но именно этот известный дефект исключён из блокеров перехода по явному решению Даниила. Остальные ошибки не покрываются этим решением. Финальная приёмка всей цели требует закрытия D-001; увеличение таймаута не является исправлением причины.

Обновление доказательств: полный Linux race кандидата10 прошёл без падений (2641 успешное test event; qa.local/architecture-stage-b-repair10/linux-final-gates/result.json). Это успешный повтор, но не подтверждение устранения ранее воспроизведённой нестабильности; D-001 остаётся открытым до проверки причины и закрытия в C4.

### D-002 — неразличимые ссылки на сохранённые операции пассов

**Статус:** перенос в C2 по согласованному правилу архитектурных переносов. Владелец: разработчик registration coordinator; основной агент отвечает за проверку закрытия. Это ограничение discovery, не доказательство нарушения idempotency или прав.

Независимый recovery QA кандидата11 получил через публичный `passes.operations` две ссылки на batch-операции и не смог определить нужную: ответ содержит только `operation_id` и `tool`. Для продолжения конкретной проверки потребовалось отдельно запрашивать операторское read-only сопоставление. Доказательства текущего запуска: `qa.local/architecture-stage-b-repair11/functional-recovery-resumed/approved-run/`; окончательный результат recovery фиксирует reviewer отдельно.

Причина подтверждена чтением `source/internal/bot/script_pass_store.go` кандидата11: `scriptPassOperation` содержит только ID/Name, запрос выбирает только эти два поля. Нет публичного описания, позволяющего безопасно различить несколько операций одного вида; сортировка не является контрактом выбора.

C2 передаёт bind/read/execute/resume registration coordinator. В его typed read добавить ограниченное описание собственных операций: время, тип, доступный текущему пользователю контекст и состояние продолжения. Перед раскрытием доменных деталей проверять текущие права и исходное происхождение; не возвращать секреты, полный payload или отозванные тексты. Возвращаемый reference не даёт новых прав. Не добавлять независимый каталог или второй store.

Проверка закрытия: минимум две операции одного вида в одном диалоге, агент выбирает нужную исключительно через публичные tools без SQL/operator mapping, затем продолжает её; проверить отзыв прав/источника, одинаковые виды операций, повтор и отсутствие эффекта у второй операции. Свежий Functional QA должен подтвердить удобство выбора и актуальную видимость. До исправления recovery с операторским сопоставлением можно учитывать только как проверку выполнения известной операции, не как успешный пользовательский discovery. Дефект не блокирует структурный перенос C; финальная приёмка всей цели требует его закрытия.

### D-003 — batch инвалидирует собственный versioned source

**Статус:** перенос в C2 по согласованному правилу архитектурных переносов. Владелец: registration coordinator developer; root отвечает за композицию и независимую проверку. B не получает безусловную приёмку. Разрешено продолжать C; окончательная приёмка требует закрытия.

Два независимых synthetic-сценария кандидата11 (permission UI19884 и version UI19894) воспроизвели: первый элемент batch committed, остальные отвергнуты `source_stale` до запланированного interruption. Отзыв прав и concurrent edit поэтому не проверены. Отчёт: `qa.local/architecture-stage-b-repair11/functional-recovery-resumed/reports/report.md`; причинный разбор: `permission-prerequisite-diagnosis/REPORT.md` рядом с ним в repair11.

Причина: сохранённое происхождение содержит privileged read первого получателя v3. Batch сам меняет его на v4, но перед следующим элементом проверяет исходный v3. Общий read-authority validator правильно обнаруживает несовпадение; координатор не учитывает доказанное собственное изменение. Domain receipt первого эффекта сохраняется, но отдельная проверка общей script authority скрывает весь результат как `pass_access_changed`. Это не rollback. Обычный ранее успешный batch без такого источника не покрывает дефект.

C2 должен владеть эволюцией источника операции: оставить исходную derivation неизменной, а допустимый successor доказывать только canonical before/after receipts предыдущих элементов того же actor/event/batch с точными keys/request hashes. Под прежними locks проверить исходную identity/version, полную цепочку и живую ожидаемую successor version. Текущие grants, causal/history fences и посторонние источники проверяются как раньше. Внешнее изменение, чужой receipt, удаление/пересоздание и неоднозначная цепочка не принимаются. Нельзя подменять expected version текущей или ослаблять общий validator. Данные canonical receipts уже существуют; `adminReplay` возвращает текущие записи и не является историческим after-proof.

Отдельный C2 result seam должен показывать актуально разрешённый статус реального эффекта из receipts, сохраняя redaction старых private payloads и ранее скрытой истории. Это не разрешение восстановить весь script transcript. Nested causal leaves и partner side effects требуют точного receipt evidence; неподдержанные случаи явно остаются незавершёнными, а не объявляются паритетом.

Закрытие: real-PG baseline с настоящим privileged read первого получателя, затем успешный multi-item и interrupted/new-instance resume без повторного эффекта и изменения исходного source. Негативные случаи: внешняя версия (даже same-value), новая creation identity, чужой batch receipt, отзыв actor/target grants, retirement постороннего источника, удаление истории, rollback без receipt. Проверить nested/partner effects в поддерживаемых командах. После domain proof — свежие независимые Code QA и Telegram-like EN/RU Functional QA исходных permission/version сценариев и безопасной видимости committed результата. До этого оба исходных residual QA остаются NOT ACCEPTED.

Domain checkpoint for D-003: `qa.local/architecture-stage-c2-successor/run-source` (1480 files, manifest `2cc6cb666e61e95917f95f83ba9aedb4dd807c31480802932200fb0df4a5f56b`) passed the final focused PostgreSQL matrix, native/vet/pinned lint and fresh independent read-only CLI Code QA. Canonical receipt-bound successors, restart, partner effects and negative source/permission/version cases are covered in that scope. This does not close D-003: public operation status/discovery, overlapping grounded-command versions and the original composed Functional QA scenarios remain open. Report: `qa.local/architecture-stage-c2-code-qa/REPORT.md`.

### D-004 — позднее чтение восстанавливает очищенную память в ledger

**Статус:** открытое замечание свежего Code QA C4c, исправляется в C4d при передаче полной ответственности за read ledger. Владелец: C4 developer; root отвечает за композицию и независимую приёмку. C4c не принят. Дефект подтверждён управляемым real-PG RED: после MemoDelete и reconciliation поздний completion восстановил оба private bodies, request text и старые epochs (1.32s, package1.563s). Evidence: qa.local/architecture-stage-c4d/red-evidence/pg.log и pg-result.json; source9836085d2ea12b61046585568ec5d50ef2e1b61e930d83c087646597b9caf634. GREEN и независимое закрытие ещё впереди.

В frozen C4c internal/agenthost/read_store.go completion читает массив без FOR UPDATE и затем заменяет slot/записывает весь массив. ReconcileMemory блокирует и очищает те же строки, но не использует knowledgeReadLock. Возможен порядок fetch private text -> committed scrub -> late completion, который вернёт прежний текст в durable target slot; старый snapshot массива может вернуть также очищенных siblings. Последующая reauthorization проекции не исправляет сохранённую запись. Доказательство: qa.local/architecture-stage-c4c-code-qa/REPORT.md, finding1; frozen manifest37cb7e78284d2a79e1970f86b0235c94bba45c0edb7f57d956171548cdf3cb4b.

C4d должен сериализовать reserve/complete/reconcile в одном согласованном порядке, проверять актуальное retirement до записи fetched result и сохранять уже очищенные siblings. Domain authorization остаётся обязательной; простого переноса callbacks недостаточно. Не менять frozen C4c и не маскировать проблему повторной очисткой только model output.

Закрытие: real-PG barrier RED/GREEN для позднего target completion и изменения sibling во время reservation/completion; подтвердить отсутствие восстановления текста в БД и пользовательском результате, отсутствие потери другого committed read, отсутствие deadlock. Затем coherent final-manifest PG с существующими privacy/replay/operation cases, свежий Code QA и affected Functional QA. До этого можно продолжать отдельные структурные срезы, но полная приёмка C и выпуск запрещены. Перенос не объявляет privacy defect исправленным.

### D-005 — происхождение summaries операций не переносится в derived writes

**Статус:** P1 воспроизведён через public operations → memo (RED1188); scoped real-PG GREEN30028 прошёл. Полный C2 и независимые affected QA ещё требуются. Исправляется в C2 coordinator/typed projection, владелец registration developer с согласованием host result-authority capture C4. Отчёт: qa.local/architecture-stage-c2-operations-code-qa/REPORT.md, finding1, frozen6cf4bb7e54f915a8dc5c71841e25928739f4560e558833c7b6623745adf78071.

passes.operations возвращает summary без result authorities. Последующая derived memo наследует admission source самого вызова, но не исходные action/target grants и источники раскрытых операций. Повторная проверка исходного script не инвалидирует такую downstream копию. Исправление: ledger/scenario projection передаёт bounded host-only provenance раскрытых summaries в обычную цепочку derivation; private provenance не показывается модели. Закрытие: operations -> derived memo -> отзыв grant/source -> свежее чтение memo скрывает производные данные, включая nested dependencies; текущие разрешённые opaque statuses остаются правдивыми. Нужны RED/GREEN real-PG и свежие affected Code/Functional QA. До этого C2 не принят.

### D-006 — status admitted batch проверяет получателей слишком поздно

**Статус:** P1 того же Code QA, finding2. Real-PG RED51343 и GREEN70469 сохранены; повтор в authority suite30028 прошёл. Независимые affected QA и общий C2 остаются открыты. Владелец C2 registration developer, исправление в общих domain receipt/status checks.

Если batch допущен в script ledger, но строка batch ещё не сохранена, operation_receipts возвращает not_committed до проверки текущих target permissions. Grounding разрешает identity/profile и не заменяет permission check. После отзыва can_book у получателя такой запрос может раскрыть контекст и continuation. Проверять resolved targets до любого возврата status независимо от наличия batch row, без создания batch или обновления исходной derivation. Закрытие: interruption после admission до batch persistence -> target revoke -> list скрывает и exact lookup не отличает от отсутствующего; никаких новых domain effects/receipts. Сохранить разрешённый pending status. Real-PG и свежий affected QA обязательны; C2 не принят.

### D-007 — последующая ошибка отменяет уже обнаруженное retirement

**Статус:** P1 того же Code QA, finding3, inherited loop в script_pass_privacy. Mixed-error RED и scoped PG GREEN выполнены, но fresh review33577 выявил late private output, неполную очистку carriers и failed admission bookkeeping; actual dispatcher RED57108 подтвердил утечку. Исправление и повтор на согласованной C4d ещё требуются. Владелец C4d privacy developer, согласование с C2 result revalidation.

Проверка нескольких сохранённых runs может установить changed=true для первого, затем получить infrastructure error во втором и выйти до durable redaction. Если права восстановлены до retry, ранее retired transcript снова проходит. C4d переносит общий privacy policy и должен сделать definitive retirement монотонным: последующий outage не отменяет уже наблюдённый отзыв. Outage сам по себе не превращается в разрешение или ложное definitive retirement. Закрытие: denial -> later outage -> restored grant -> retry, durable target/dependent transcripts остаются очищенными; no private restoration и корректная retryable ошибка. Нужны управляемый real-PG сценарий, coherent final gates и независимые affected QA. C4/C2 до закрытия не принимаются.

Дополнение D-002: fresh operations Code QA finding4 показал, что batch descriptions не закрывают весь discovery. Две single assignment/targeted command в одном script могут иметь одинаковые time/event/tool/status; opaque ID не позволяет выбрать нужного получателя. C2 должен возвращать минимальную текущеразрешённую target identity для этих семейств и удалять её при retirement. Закрытие D-002 включает такой single-target сценарий через public tools, а не только два batch.

D-004 focused GREEN checkpoint: дваreal-PG теста прошли1.38s/1.58s (package3.153s), green manifest5aaf375efef142db4fef39c757341ac4de97404b5633dd689b25eebb68055dc6. Late target/sibling restoration предотвращён; concurrentdelete/complete дожидаютсяmemorygate и завершаются безdeadlock. Fresh narrowCodeQA56295 запущен. Это не закрываетD-004 до coherentC4d иaffectedFunctionalQA.


D-004 narrow Code QA завершён: green5aaf375e не покрывает два дополнительных порядка выполнения — late fetch с новым epoch и старым request text после retirement reservation; delayed reconciliation со старым captured epoch после следующего удаления и нового completion. Оба замечания переданы C4d с отдельными ordering regression tests. Предыдущие два GREEN не закрывают D-004.

D-006: фактический RED51343 воспроизвёл отсутствие отказа после отзыва target permission. GREEN70469 прошёл regression и две receipt/family группы (15.508s), synthetic PG удалён. Scope evidence: qa.local/architecture-stage-c2-coordinator/{red-target2,green-target,target-regression-manifest.json}. Независимые affected QA и общая композиция ещё требуются.

D-007: native RED8307 через настоящий reauthorizePassScripts подтвердил потерю первого definitive retirement при последующем503. Evidence: qa.local/architecture-stage-c4d/p1-red-evidence. Исправление и durable PG proof ещё в работе.

D-007 expanded developer GREEN:31283, immutable p1-green2-source manifest8a0a47ab19a29a0af53b0c1f5ccf552f18851de19f718ef7fab2a30e76a7bb65. Шесть PG сценариев load/complete/change × outage/cancellation прошли7.057s; cleanup и before/after hashes подтверждены. Fresh narrow Code QA33577 запущен. Это не закрытие: общий C4d состав и affected Functional QA ещё требуются.

D-005 runtime RED подтверждён через public operations→memo после реального receipt и retirement исходного текста. При последующем отзыве booking-admin права новая статусная memo остаётсяactive; raw evidence qa.local/architecture-stage-c2-coordinator/red-memo3/pg.log. Snapshot69bdaafe74e124db22aa1e84f95369273095e6fe3e9d55897468f306846f6d60 реконструирован после запуска с документированным reverse-only HTTP helper delta; это не pre-run source binding. Исправление typed result provenance в работе.

D-001 corrected measurement: the 2→0 preparation-request figures in pg-20260928T173207729Z belong to a rejected candidate and are not final evidence. The corrected pg-20260928T190501173Z measurement retains two preparation requests and three execution requests; preparation response bodies decrease from 277518 bytes (escaped: 277510) to 96 bytes. Timing samples do not establish a speedup. The original uninstrumented full Linux race passed once on the isolated final D-001 candidate with unchanged assertions and parallelism (race-candidate-full-20260928T191625257Z). Final composed race and independent acceptance remain mandatory. Source: qa.local/architecture-stage-c4-d001/REPORT.md and MEASUREMENTS.md.

D-001 composition follow-up (2026-09-29): frozen C7/D5a2e failed all three original subcases at inbox completion. The separate fixture successor8F6F changes only two Bot constructors to preserve Delivery; removing those two lines restores the original E220 test hash. Its guarded Linux race run qa.local/architecture-stage-d-c7-race/run-20260929T112713120 also failed all three subcases, now with interrupted-result/assertion failures rather than the earlier missing-delivery wait. Cleanup0/sourceDrift0. The fixture correction is not closure of D-001; original time limits, parallelism and assertions remain. Completion-path diagnosis and final independent acceptance are pending.
