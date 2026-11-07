# История: пустой сценарий и synthetic seed

Свежая app DB очищает app registration/history/actions **всего сценария**, но
не историю Loom. `internal/task/profile.go:prepareCreate` не передаёт chat_id,
поэтому тот же IAM owner продолжает default Loom chat. Это разные слои очистки.

`loom_history.py` предлагает отдельный owner-authorized clear существующего
**явного** chat UUID или один synthetic passive event. Root предоставляет
actual delegated owner token с `chat.delete` / `chat.events.append`, не подменяет
его сервисным admin. Нужны idle app/Loom, held receiver/provider и свежий полностью
decoded backup Loom DB. Root читает exact chat/owner/org/subject из локальной
QA DB; перед DELETE не вызывает GET `/v1/chats/default`: этот GET делает upsert
chat/user и потому не является чистым read-only inspect.

Native контракт: DELETE `/v1/chats/{id}` →204; POST `/v1/chats/{id}/events` →201.
Clear — logical chat deletion с обычной политикой отзыва доступа к файлам/тексту,
не физическая очистка recovery/billing. Следующий обычный bot run создаст новый
default chat. Обычного undelete API нет; точный restore требует отдельного whole
Loom DB snapshot/restore, а не SQL-переключения deleted_at. Не очищать active или
unknown work и не трогать другие QA/production проекты.

Config расширяет operator config: `postgres_id`, `postgres_image`,
`postgres_admin`, `app_database`, `loom_database`, `loom_api_id`, `loom_api_image`,
`loom_base_url` (verified local TLS), `loom_bearer:{path,sha256}`,
`loom_backup_receipt:{path,sha256}` и оба held fences. Backup receipt имеет
project/owner, scope `local_loom`, `fully_decoded:true`, UTC created_at (<30мин).
Plan: `{action:"clear"|"append",operation:UUID,chat_id:UUID,owner_id:UUID,subject}`;
append дополнительно содержит `text` с явным префиксом `SYNTHETIC QA history: `
и RFC3339 `occurred_at`. Это passive action `qa.history.seed`, не Telegram message,
не результат модели, не подтверждённый платёж. Ни один run/model endpoint не
вызывается. Source: Loom `internal/api/api.go`, `internal/store/history_auth.go`,
`internal/store/lifecycle.go`.

## Исторический архив Shuttle

`history_fixture.py` создаёт только небольшой синтетический source snapshot:
один выбранный numeric owner и две явно synthetic исторические записи, без
passes/payments/live actions. По умолчанию — plan-only; запись только по
`--write-private-dir` в заранее защищённый пустой каталог. Реальные истории и
production copy здесь не читаются.

```powershell
python -E deploy/shuttle/qa/history_fixture.py --bot-id LOCAL_TEST_BOT_ID --telegram-user-id SELECTED_TEST_OWNER --instance NEW_UUID
# Явная локальная запись fixture, затем maintained import/validation.
python -E deploy/shuttle/qa/history_fixture.py --bot-id LOCAL_TEST_BOT_ID --telegram-user-id SELECTED_TEST_OWNER --instance NEW_UUID --write-private-dir PRIVATE_FIXTURE_DIR
shuttle-import -manifest PRIVATE_FIXTURE_DIR/snapshot.json -output PRIVATE_NEW_ARCHIVE
shuttle-import -verify PRIVATE_NEW_ARCHIVE -sha256 EXACT_SHA
```

`history_operator.py` использует те же `inspect/plan/observe/apply` flags и guards,
что operator CLI: `archive-prepare`, `archive-status`, `archive-validate`,
`history-prepare`, `history-confirm`, `history-status`, `history-page`, `history-text`.
Prepare/confirm — отдельные действия с тем же backend operation и exact proposal.
Для history apply arguments: archive_id, sha256 и projection_version из текущего
контракта `legacyimport.ProjectionVersion`, не произвольная строка.

После archive preparation без source_file_id root может использовать
`archive_upload.py`. Private upload plan: `{operation,proposal,archive:{path,sha256},
preparation:{path,sha256}}`. Helper сверяет original prepared receipt/expiry,
создаёт exclusive audit и один raw gzip request с documented operation/proposal/SHA
headers. Размер этого synthetic QA upload ограничен8MiB. Flags как у IAM helper:
`--config`, `--plan`, optional reviewed `--apply-plan-sha256`,
`--apply-config-sha256`, `--private-dir`. Никаких секретов в аргументах процесса.
После неизвестного ответа root запрашивает status исходной operation; новый
operation не является восстановлением старого.

Отдельный maintained `shuttle-import apply-accounts` сопоставляет уже applied
users records с проверенными IAM accounts под реальным app-admin JWT. Он не
создаёт IAM пользователя, деньги, pass, live task или confirmed name. Использовать
только если сценарию действительно нужны отсутствующие app accounts; обычный
bot/prelink остаётся отдельным проверяемым входом. Никакой SQL-seed approvals.

Written by Codex (unverified/Codex desktop)
on behalf of Daniel Drizhuk
