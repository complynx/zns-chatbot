# Матрица прав Shuttle / Зинуси

Матрица описывает прикладные права и повторные проверки. Конкретные назначения людей и конфигурация окружения ведутся отдельно.

## 1. Где определяется доступ

| Уровень | Фактический контракт | Что он не заменяет | Источник |
| --- | --- | --- | --- |
| Deployment | Одна БД/схема привязана к Telegram bot ID, IAM organization и issuer. Чужой deployment не должен получать доступ к её объектам. | Нельзя сменить bot ID/organization удалением binding или перезапуском с чужими credentials. | [Deployment scope](shuttle-deployment-scope.md), `internal/db/deployment.go` |
| IAM identity | JWT: подпись, issuer, API audience, expiry, organization; introspection: active и совпадение subject/actor. | Email, Telegram username, текст модели и переданный `user_id` не являются правом доступа. | `internal/identity/client.go`: `signed`, `Verify` |
| Локальный account | Пользователь должен существовать в текущих bot/organization, быть активен и не иметь `revoked`. | Валидный IAM token не обходит локальный отзыв доступа. | `internal/api/server.go`: `authorizeIdentity`, `authorize` |
| IAM роль приложения | Главный администратор проверяется по `zinusya.admin` **в API project**, с ключом текущей organization. | Одноимённая роль только в Loom, плоский claim или `ORG_OWNER` сами по себе не дают Shuttle admin. | `internal/identity/client.go`: project-specific introspection claim; `internal/api/agent_admin.go` |
| Локальные бизнес-роли | Роль события, модератор знаний и привязка специалиста хранятся в PostgreSQL и перечитываются при операции. | Это не IAM роли; назначенный пасс, видимая кнопка и публичный specialist ID не создают их. | `internal/db/authorities.go`, `knowledge_write.go`, `massage_staff_reads.go` |
| Объект и событие | Backend проверяет владельца, event, включённый модуль, entitlement и версии согласно конкретному методу. | Выбранное в интерфейсе событие и discovery не дают права на произвольный объект. | `internal/db/event_context.go`, `payments.go`, `pair_assignments.go` |
| Подтверждение | Для опасных операций — исходные operation/proposal, actor, аргументы, версия, deadline и явное согласие. | `prepare` не исполняет действие; ответ модели «подтверждено» не является согласием человека. | [Спецификация](shuttle-spec.md#api-права-и-подтверждения), manifests и canonical store handlers |

`zinusya` — service ID в Loom. Например, `zinusya_payments` — module ID, а
`review` — метод. Эти идентификаторы не следует превращать в вымышленные IAM
permission keys. Включение `registrations`, `orders`, `meals` или `massage` у
события также не назначает пользователю роль.

## 2. Люди и роли приложения

| Роль | Где и каким ключом задаётся | Разрешено | Границы и запреты |
| --- | --- | --- | --- |
| Участник | Активная локальная identity. Специальная глобальная IAM роль участника не требуется для обычных owner-методов. | Свой профиль/имя, собственные legal details и историю; регистрация/пара/пасс; собственные drafts, заказы, питание, аватарки, бронирования и файлы через соответствующие API. Можно отправить своё подтверждение оплаты. | Чужой UUID не даёт доступа. Для событийных услуг действует требование модуля/назначенного пасса по контракту; назначенный **неоплаченный** пасс может быть достаточным entitlement. Отправка чека не кредитует баланс. |
| Главный администратор приложения | IAM project role `zinusya.admin` в Shuttle API project. Для агентского admin discovery нужна соответствующая роль/permission также в Loom project. | Настройки приложения; событийные конфигурации и каталоги; solo assignment; event authority grants; specialist bindings; moderator assignments; scoped diagnostics, delivery resolution, migration/archive administration; платежный review в своём deployment. | Это широкая роль всего bot/organization, не только одного события. Она не является IAM-администратором, не меняет init secrets/trust roots через settings и не открывает автоматически чужие обычные файлы, passport plaintext или specialist client contacts. Общая/историческая публикация знаний требует отдельного moderator assignment. |
| Администратор события | `shuttle_event_authorities.role = administrator` для точных `(event_id,user_id)`. | Платёжный review и scoped evidence/balance reads своего события; pair assignment по отдельному canonical контракту; редактирование/модерация знаний активного события. | Не управляет IAM, глобальными настройками, чужими событиями, event authority grants или specialist bindings. Не следует переносить право pair assignment на solo admin `assign`: тот требует глобального `zinusya.admin`. Исторические/общие знания требуют moderator assignment. |
| Амбассадор | Та же локальная таблица, `role = ambassador` для конкретного события. | Платёжные workspaces, review/confirm/reject и чтение **поданных** подтверждений в событии; balance/evidence согласно платёжному контракту. | Не глобальный администратор и не администратор события. Не назначает пассы, роли, специалистов или модераторов. Нет общего доступа к файлам владельца. В реализации нет отдельного IAM ключа «ambassador». |
| Специалист массажа | Приватная event-bound привязка active local account к specialist alias, назначенная главным администратором. | Staff event/schedule/windows/timetable; свои client details/contact; self time reservation `staff_self` и собственные notification preferences. | Участнический пасс не нужен для staff reads. Другие специалисты видят в timetable ограниченные данные, без чужих contacts; `client` требует собственный alias. Нельзя произвольно править чужие бронирования или подтверждать платежи. Global/event admin сами по себе не заменяют specialist binding. |
| Модератор общей/исторической базы знаний | Именное локальное назначение, `enabled`, bot/organization; не IAM role. | Общие и исторические материалы, соответствующие предложения/редактирование через knowledge API. | Назначает/отзывает только главный администратор. Moderator сам по себе не получает event authority, платежные или глобальные admin-права. Для активного event-контента применяется его отдельная event-authority проверка. |
| Автор предложения в базе знаний | Обычный активный пользователь, разрешённый knowledge-контрактом. | Подать предложение; читать доступный опубликованный контент. | Публикация предложения требует уполномоченного отдельного reviewer. Предложение автора не становится опубликованным от имени модели или из-за наличия global admin role. |
| Отозванный/чужой пользователь | `revoked`, отсутствующий local account, другая organization/bot. | Только специально допустимые публичные страницы/вход; не authenticated business API. | Ранее полученный receipt, callback, сохранённый draft или старый bearer не отменяет текущую проверку доступа. |

В `shuttle_event_authorities` хранится **одно** значение роли на пользователя и
событие: `none`, `ambassador` или `administrator`, а не набор независимых флагов.
`none` — явный отзыв с версией и историей. Права главного администратора,
модератора и специалиста задаются отдельно; один человек может иметь несколько
таких назначений, но они не выдаются автоматически друг из друга.

Источники: [event authorities](shuttle-event-authorities.md),
`internal/db/payments.go:lockPaymentModuleEvent`,
`internal/db/pair_assignments.go:lockPairAssignmentAuthority`,
`internal/db/knowledge_write.go:kbAuthorize`,
`internal/db/knowledge_publish.go:KnowledgeCapabilities`,
`internal/api/massage_staff_manifest.go`, `internal/api/massage_binding_manifest.go`.

## 3. Платежи и финансовые действия

| Действие | Участник | Амбассадор своего события | Event administrator своего события | Главный администратор | Что реально меняется |
| --- | --- | --- | --- | --- | --- |
| Прочитать свой пасс/остаток/ожидание | Да | Как владелец либо по authority-контракту | Аналогично | По предусмотренному admin/authority-контракту | Чтение; pending evidence не увеличивает confirmed total. |
| Подать evidence для своего пасса/заказа | Да, с owner/event/module/суммой и supported source | Роль не даёт права выдать чужой файл за свой | Аналогично | Admin не отменяет owner-контракт подачи | Создаётся ожидающее человеческого review подтверждение; не факт получения денег. |
| Читать поданный чек другого участника | Нет | Да, только своего события | Да, только своего события | Да, в текущем deployment по evidence API | Доступ связан с evidence ID, а не произвольным `attachment_id`. Права повторно проверяются после Telegram IO и при content download. |
| Confirm/reject поданный чек | Нет, если нет отдельной authority | Да | Да | Да | Exact proposal + человеческое согласие; проверяются версия, сумма/валюта, remaining balance, current authority и reason для специальных случаев. Confirm создаёт учётную запись оплаты; reject сам по себе денег не списывает. |
| Исправить результат новым решением | Только доступные owner submissions | По предусмотренному review-контракту | Аналогично | Аналогично | Аудируемые версии/причины и исходные receipts сохраняются; не прямой UPDATE баланса. |
| Создать заказ/бронь/meal proposal | Для своего объекта при eligibility и согласии | Платёжная роль не заменяет owner/consent | Аналогично | Аналогично | Цена/обязательство и платёжный факт различаются. `prepare` не создаёт оплаченный заказ. |
| Банковский перевод, списание, refund | Не является следствием этих permission keys | Не возникает от review роли | Аналогично | Аналогично | Текущие review APIs учитывают подтверждённый человеком факт, а не выполняют банковскую операцию. |

В source нет отдельной роли «платёжный администратор» сверх указанной комбинации
global admin / local ambassador / local administrator. Фактического получателя
денег определяет рабочий процесс оператора. Наличие роли не доказывает поступление
денег на счёт. Backend не заменяет человеческую сверку OCR или словами плательщика.
Не следует приписывать ему несуществующее правило «только этот закреплённый кассир»
или универсальное правило двух разных людей на всякое финансовое решение.

Для pass payments `lockPaymentEvent` проверяет модуль `registrations`;
order/meal evidence использует свою модульную границу. Это не разрешение читать
все платежи других событий. Источники: `internal/db/payments.go`,
`internal/api/payment_files.go`, `internal/api/order_evidence_manifest.go`,
[платёжный контракт](shuttle-payments.md), [ожидания](shuttle-payment-expectations.md).

## 4. PII, история, файлы и диагностика

| Данные | Кто получает через приложение | Ограничение |
| --- | --- | --- |
| Текущее имя/обычный профиль | Владелец; scoped summaries только там, где они нужны домену | User ID/subject берутся из проверенной identity. Name-only IAM sync не должен менять email, provider links и grants. |
| Legal name / passport | Owner-specific registration-details API | Отделены от обычного профиля; хранятся с protector key. Status не возвращает приватные legal values. `zinusya.admin` не объявлен общим plaintext passport-reader. |
| Произвольное вложение, оригинал аватарки, исторический Telegram locator | Владелец соответствующего source/reference | `zinusya_files` и owner history/avatar paths проверяют активный account и bot/organization. Global admin или знание UUID не дают общего file read. |
| Поданный платёжный proof | Владелец либо текущая уполномоченная authority его события | Через evidence ID и отдельный API. Это не доступ к остальной галерее пользователя. |
| Derived artifact/transcript в Loom | Текущий run/owner и его актуальные disclosure/access policies | Временный handle не является публичным URL или долговременным хранилищем. Revocation и source policies перепроверяются при соответствующем использовании. |
| Staff timetable | Пользователь с текущей приватной specialist binding | Event-wide ограниченное расписание; client contact только для собственных aliases. Global admin retained massage history — отдельный scoped read, не staff/contact grant. |
| Task diagnostics | Главный администратор текущего deployment | Метаданные task/queue/delivery/operator decisions; обычный diagnostic read не выдаёт raw message bodies/checkpoints/credentials. |
| Raw legacy archive, DB backups, служебные конфиги | Отдельный оператор хранения/импорта на уровне файлов и PostgreSQL | Это чувствительные административные assets. Application admin archive operations имеют отдельные prepare/hash/validation/apply ограничения; owner history API не является каталогом чужой переписки. Права на сервер/backup нельзя считать обычной ролью приложения. |
| Metrics / traces | Только разрешённые collectors/оператор private listener | Не публиковать private API/metrics в public router. Telemetry ACL и export credentials отдельны от пользовательских ролей. |

Источники: `internal/db/files.go:File`, `internal/api/payment_files.go`,
`internal/db/registration_details.go:RegistrationDetailsStatus`,
`internal/api/registration_details_manifest.go`, `internal/identity/profile.go`,
`internal/api/massage_administration.go`, [agent administration](shuttle-agent-administration.md),
[history apply](shuttle-history-apply.md).

## 5. Выдача, отзыв и первый главный администратор

| Задача | Кто и каким путём | Повторная проверка / важное ограничение |
| --- | --- | --- |
| Первый `zinusya.admin` | IAM-оператор создаёт/проверяет project role и user grant нужному **человеческому subject** в API project; для агентского admin доступа — также role/mapping в Loom project. | В Shuttle нет self-bootstrap endpoint, который превращает первого Telegram-пользователя в главного администратора. Telegram username и email не использовать как неявный admin allowlist. |
| Следующий главный администратор / отзыв глобального admin | Управление IAM project grants уполномоченным IAM-оператором | `zinusya_administration.authority` меняет только event role. Главный администратор приложения не получает IAM user/grant-management автоматически. После IAM-отзыва учитывать реальный cache TTL/invalidation и проверить новый отказ; старый UI не доказательство доступа. |
| Event ambassador / administrator | Главный администратор: `zinusya_administration.authority`, prepare → execute/status; `authorities` для scoped списка, `zinusya_authorities.me` для своей роли | Event/user/version связаны с proposal; повторный вызов возвращает исходный receipt и не восстанавливает позже отозванную роль. Роль `none` отзывает назначение. |
| Moderator знаний | Главный администратор: `moderators` / `set_moderator` с target, enabled, expected version и reason | Это отдельное назначение общей/исторической модерации, не автоматическое следствие global admin. |
| Specialist binding | Главный администратор: admin massage specialist target/binding методы и соответствующая native форма | Exact event/alias/local account, версия/generation и явное согласие. Публичный alias нельзя трактовать как право или безусловно передавать другому человеку вместе с историей. |
| Локально отозвать пользователя | Главный администратор: `user` → `revoke_user` prepare/execute/status | Отзывает local account и инвалидирует cache в процессе; remote IAM roles не меняет. Self-revocation возможна: другой активный admin может читать `user_revocation_receipt`, но не подтверждать чужую proposal. |
| Восстановить локально revoked account | Не считать IAM grant восстановлением local account | В текущем `user_revocations_manifest.go` нет симметричного `restore_user`. Если восстановление потребуется, нужна отдельная согласованная процедура; не предлагать прямой SQL как обычную админскую кнопку. |
| Повторить неизвестную операцию | Исходный actor и canonical status/receipt lookup | `not_found`/503/таймаут не дают права создать новое финансовое действие или другой operation ID. Отзыв прав проверяется и на replay, где этого требует контракт. |

Минимальный bootstrap для локального реального тестового бота: выбрать bot ID и
точные разрешённые Telegram accounts; связать их с каноническими IAM subjects;
одному выбранному subject выдать app admin grants; проверить собственный вход и
отказ обычному пользователю; только затем назначать event authorities, moderators
и specialist bindings через предусмотренные API/формы. Не выдавать всем новым
пользователям actor/caller-role или `zinusya.admin`.

Проверяющие источники: `internal/api/user_revocations_manifest.go`,
`internal/api/authorities.go`, `internal/db/authorities.go`,
`internal/api/knowledge_manifest.go`, `internal/db/knowledge_targets.go`,
`internal/api/massage_binding_manifest.go`, `internal/api/miniapp_massage_bindings.go`.
