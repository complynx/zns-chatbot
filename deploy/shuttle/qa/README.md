# Локальный ручной QA: операторские планы

`operator_cli.py` по умолчанию печатает **offline** карту команд. Он не получает
credentials, не вызывает модели, не провизионирует IAM и не исполняет SQL.
`plan` также работает offline. Только `observe` обращается к локальному API;
только отдельный `apply` отправляет **один** выбранный mutation request.
Подготовка команд не означает, что стенд уже изменён или сценарий принят.

Один entry point: `python -E deploy/shuttle/qa/qa.py inspect`.
Статические команды `app`, `scenario`, `iam-human`, `iam-role`, `history`,
`fixture`, `archive-upload`, `loom-history` пересылают аргументы соответствующему
маленькому helper. Цепочек prepare→confirm, автоматических retries и model calls нет.
Подробности selected IAM lifecycle/roles: [IAM.md](IAM.md); clean/seed history:
[HISTORY.md](HISTORY.md).

Применяется lean-build: штатные API и отдельные конечные сценарии вместо
универсального fixture engine. Root владеет секретами, runtime и запуском.
Данный каталог не создаёт глобального IAM admin и не подменяет human consent.

## Поддержанная карта API

| Задача | Штатный endpoint / principal | CLI / граница |
|---|---|---|
| Event seed/read | GET/PUT `/internal/admin/events/{id}`, GET `/internal/admin/events`; app admin | `event`, `events`, `event-set`; exact version0 для нового event. Document по `internal/events`. |
| Регистрация | GET/POST `/internal/events/{id}/registration`; owner | `registration`, `registration-join`. Это создание запроса, не удаление существующей регистрации. |
| Назначение pass | GET admin registrations; POST `/internal/admin/events/{id}/registrations/{registration_id}/assign`; app admin | `registrations`, `registration-assign`; exact registration/event versions. |
| Event roles | `/internal/agent/administration/authorities`, `authority/prepare`, `authority`, `authority/status`; app admin | `authorities`, `authority-*`; none/ambassador/administrator. Prepare и confirm — отдельные планы/запуски. |
| App account revoke | `administration/user`, `revoke_user/prepare`, `revoke_user`; app admin | `app-user`, `app-revoke-*`; expected subject/revoked. Это soft revoke, не delete/unrevoke/reset регистрации. |
| Orders/meal fixtures | `administration/{orders,meal}_catalog`, `set_{orders,meal}_catalog/prepare`, `set_{orders,meal}_catalog`; app admin | Read/prepare/confirm; actual document и event/catalog versions. |
| Операции/задачи | `administration/tasks`, `administration/task`; app admin | `tasks`, `task`; diagnostic read, без закрытия/удаления unknown financial effects. |
| Review платежа | `/app/payment-reviews/{workspaces,confirmations}` и `/app/order-reviews/...`; current payment authority | Штатная Mini App, actual actor/session + frozen terms. CLI не исполняет финансовое approve SQL и не фабрикует подтверждение. Точные routes: `internal/api/miniapp_payments.go`, `miniapp_order_review_reads.go`. |
| IAM lifecycle/grants | Отдельный IAM Management API, authorized IAM manager | Не app-admin token, не сервисный actor/ORG_OWNER impersonation. Выбранный human и scoped grants; exact schemas/operator binding подготовить по runtime configuration. |
| Clear app registration/history/actions | Fresh application scenario database | Смена **всего** QA scenario, не per-user purge. Старую DB сохранять; последовательность в `scenarios.json`. Не DELETE по FK. |
| Seed history | Validated synthetic legacy archive/import; Loom passive chat-event API | Archive store/validate: `internal/api/archive_*.go`; account adoption — отдельный `cmd/shuttle-import apply-accounts`, не HTTP endpoint. Контракт `docs/shuttle-history-account-import.md`. Не fake Telegram webhook и не SQL-approve. |

Источники: `internal/api/events.go`, `registrations.go`, `authorities.go`,
`user_revocations.go`, `orders_catalog.go`, `meal_catalog.go`, `agent_admin.go`;
`docs/shuttle-permissions-matrix.md`, `internal/db/deployment.go`.
FK ограничения проверять по текущему schema baseline в `migrations/`
(users/tasks/files/registrations/payments/revocation proposals).

## Конфигурация root

CLI ограничен явно выбранным локальным deployment и его reviewed source guard.
Root создаёт **приватный**
JSON config вне tracked source с полями:

```json
{
  "project":"LOCAL_QA_PROJECT",
  "owner":"LOCAL_QA_OWNER",
  "bot_id":"LOCAL_TEST_BOT_ID_INTEGER",
  "issuer":"https://LOCAL_IAM_HOST:LOCAL_IAM_PORT",
  "organization_id":"EXACT_LOCAL_ORG",
  "base_url":"https://LOCAL_APP_HOST:LOCAL_INTERNAL_API_PORT",
  "app_id":"EXACT_FULL_CONTAINER_ID",
  "app_image":"sha256:EXACT_IMAGE",
  "bearer_file":"PRIVATE_ACTUAL_PRINCIPAL_BEARER",
  "bearer_sha256":"EXACT_SELECTED_PRINCIPAL_CREDENTIAL_HASH",
  "ca_file":"TRUSTED_LOCAL_CA_PEM",
  "configuration_custody":{"path":"PRIVATE_APP_CONFIG","sha256":"EXACT_SHA256"},
  "deployment_receipt":{"path":"PROTECTED_SCOPE_RECEIPT","sha256":"EXACT_SHA256"},
  "backup_receipt":{"path":"PROTECTED_BACKUP_RECEIPT","sha256":"EXACT_SHA256"}
}
```

Scope receipt повторяет project/owner/bot_id/issuer/organization_id/app_id/app_image
после проверки actual startup binding. Backup receipt содержит project, owner,
`fully_decoded:true`, UTC `created_at` и архивные hashes; apply требует свежесть
менее30минут. CLI проверяет hashes и live exact CID/image/owner/project; root
проверяет содержательную достоверность receipts до dispatch. Ни config, ни
bearer не печатать. TLS обязателен, redirects/proxies отключены.

Selection JSON содержит `action`, `arguments`, `targets` (только route UUIDs),
`operation` (новый UUID), optional `proposal` (original backend proposal UUID).
Mutation draft содержит `selection`, read-only `before` selection и
`expected_fields`: JSON pointers → exact known values текущего объекта. Для роли
проверять target/event/current version; для revoke subject/revoked; для каталогов
event/catalog versions. Read snapshot сохраняется перед dispatch. Не выбирать
нестабильные `observed_at` как precondition.

```powershell
python -E deploy/shuttle/qa/operator_cli.py
python -E deploy/shuttle/qa/operator_cli.py plan --config PRIVATE_CONFIG --selection PRIVATE_DRAFT
# Root сохраняет stdout plan в защищённый файл, читает его и вычисляет hash.
python -E deploy/shuttle/qa/operator_cli.py apply --config PRIVATE_CONFIG --plan PRIVATE_PLAN --sha256 REVIEWED_SHA --audit-dir EXISTING_PRIVATE_ACL_DIR --ack LOCAL_SELECTED_OPERATION_ONLY
```

Audit directory root создаёт заранее с current-owner-only DACL. План, pre-read,
dispatch marker и response имеют exclusive имена по operation UUID + action: повторный
apply блокируется. После потери ответа — **unknown**, никакого повторного send.
Root запрашивает status/reconcile исходной operation и сохраняет фактический
результат. HTTP200 не объявляется acceptance PASS.

## Readiness и оставшаяся привязка

`scenarios.json` фиксирует requested scope и restore шаги. IAM-only reset не
удаляет app DB; новый IAM subject после delete/create нельзя выдать за возврат
старого. Обратное создание должно пройти настоящий bot/prelink/OIDC путь.
Telegram account означает app link/account/chat visibility; **реальный аккаунт
Telegram не удаляется**. Полная очистка app-state достигается fresh whole DB.

### Whole-app database switch и restore

`scenario_db.py` — отдельный исполняемый source для app-only switch, без IAM
изменений. Он использует существующие runtime/migrator roles, сохраняет старую
DB и config, snapshot-ит остановленный app, полностью декодирует архив, запускает
maintained `shuttle -migrate`, создаёт app-only compose overlay и проверяет
`/readyz`, exact deployment binding и пустые counts. Unknown/pending app/Loom
work блокирует switch; ingress и provider должны быть held. Ошибка после эффекта
сохраняет intent; автоматического повтора/rollback нет.

Root scenario config: project/owner/bot_id/issuer/organization_id как выше;
`metadata`, `app_config`, `migrator_config`, `compose_custody` — каждый `{path,sha256}`;
`app_id`, `postgres_id`, `postgres_admin`, `original_database`, `loom_database`,
`backend_network`, `ca_file`, `ready_url`. Metadata — selected-runtime receipt с
пятью original services. `fences` содержит ровно receiver/provider, каждый
`{name,path,sha256,expected_fields}`; JSON pointers фиксируют фактические held
поля controls текущего manual runtime. Не подставлять пустые/выдуманные controls.
App/migrator configs — JSON, различаются DSN роли; никакого копирования секретов
в tracked config. Новый DB name начинается `qa_`, его ещё не должно существовать.

```powershell
python -E deploy/shuttle/qa/scenario_db.py --config PRIVATE_SCENARIO_CONFIG --name qa_clean_01
# Source+config review прежде dispatch; output dir заранее owner-only и пустой.
python -E deploy/shuttle/qa/scenario_db.py --config PRIVATE_SCENARIO_CONFIG --name qa_clean_01 --private-dir PRIVATE_FRESH_DIR --apply-config-sha256 REVIEWED_CONFIG_SHA
# После ручного сценария: новые held fences, тот же original config/metadata.
python -E deploy/shuttle/qa/scenario_db.py --config PRIVATE_RESTORE_CONFIG --restore-receipt PRIVATE_FRESH_DIR/receipt.json --restore-receipt-sha256 EXACT_RECEIPT_SHA --private-dir PRIVATE_RESTORE_DIR --apply-config-sha256 REVIEWED_RESTORE_CONFIG_SHA
```

Restore отдельно snapshot-ит scenario, останавливает только текущий scenario app
и возвращает original compose/config. Обе DB остаются; provider/IAM/gateway не
recreate-ятся. Runtime receipt не заменяет независимую проверку фактического
ручного сценария. Полный IAM reset и history seed требуют ещё отдельной точной
API/fixture binding; это не причина подменять их SQL purge или fake PASS.

Focused pure proof: `python -E deploy/shuttle/qa/test_operator.py`.



Written by Codex (unverified/Codex desktop)
on behalf of Daniel Drizhuk
