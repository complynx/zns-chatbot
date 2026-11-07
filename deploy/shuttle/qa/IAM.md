# Отдельный reset выбранного IAM human

`iam_human.py` не меняет app DB, service org/projects/clients/IDP/provider,
не выдаёт роль ORG_OWNER и не фабрикует Telegram links. Source-only helper;
runtime config и actual human selection проверяет root перед dispatch.

Контракт закреплён на Zitadel v4.20.0 `management.proto`, blob
`fb5ec911a08bf0c74fbbbd29040a76abfbacac82`:

- GET `/management/v1/users/{id}` (`user.read`) → `{user}`.
- DELETE тот же path (`user.delete`), без body → `{details}`. Это logical deleted
  state, с удалением grants/memberships, а не физическая очистка БД.
- POST `/management/v1/users/human` (`user.write`) → `{userId,details}`.
  AddHuman не принимает ID: recreate всегда получает **новый subject**.
- Всегда явный `x-zitadel-orgid`; current exact user ID/resourceOwner/username,
  human present / machine absent проверяются перед delete. Service actor и
  IAM operator запрещены как targets.

Первичный источник: [Management API](https://github.com/zitadel/zitadel/blob/v4.20.0/proto/zitadel/management.proto#L8641),
[RemoveUser](https://github.com/zitadel/zitadel/blob/v4.20.0/internal/api/grpc/management/user.go#L211),
[user command](https://github.com/zitadel/zitadel/blob/v4.20.0/internal/command/user.go#L175).
Обычная reactivate работает только для INACTIVE, не отменяет delete.

Root расширяет защищённый config `operator_cli.py` полями `iam_id`, `iam_image`,
`iam_bearer:{path,sha256}`, `iam_operator_subject`, `forbidden_subjects`,
`iam_backup_receipt:{path,sha256}`, receiver/provider `fences` как у
`scenario_db.py`. Backup receipt: project/owner, scope `local_iam`,
`fully_decoded:true`, UTC `created_at` (<30мин), archive hashes. Existing app
config actor_id и IAM operator subject обязаны входить в forbidden_subjects.
Нужен реальный authorized IAM manager bearer; app-admin роль сама по себе не
даёт user.delete/user.write.

Idle guard использует root-owned local PostgreSQL **только для чтения**: config
содержит `postgres_id`, `postgres_image`, `postgres_admin`, `app_database`,
`loom_database`; проверяются owner/CID/image и exact app deployment binding.
Это позволяет назначить первого app-admin и вернуть роль после её удаления,
не требуя уже действующего app-admin bearer и не выполняя business SQL writes.

Private delete plan: `{action:"delete", operation:UUID, subject:EXACT_ID,
username:EXACT_CURRENT_USERNAME}`. Recreate — **отдельный** plan с теми же
subject/username, `delete_receipt:{path,sha256}` и `body`:
`{userName,profile:{firstName,lastName},email:{email,isEmailVerified},initialPassword?}`.
Это явно выбранные тестовые данные, не импорт provider proof. Grants/links не
восстанавливаются автоматически; дальнейший сценарий проходит actual OIDC/prelink.

```powershell
python -E deploy/shuttle/qa/iam_human.py --config PRIVATE_CONFIG --plan PRIVATE_DELETE_PLAN
# После source/config/target review; каждый action в новом защищённом пустом dir.
python -E deploy/shuttle/qa/iam_human.py --config PRIVATE_CONFIG --plan PRIVATE_DELETE_PLAN --apply-plan-sha256 EXACT_PLAN_SHA --apply-config-sha256 EXACT_CONFIG_SHA --private-dir PRIVATE_ACTION_DIR
```

Потеря ответа — unknown: не пересоздавать журнал/operation, не посылать повторный
POST. Root сначала читает exact selected user/search/canonical state. Обычный
recreate не является restore старого subject, memberships или app-account link.
Если нужен точный возврат прежнего subject, использовать изолированный IAM
snapshot/restore; он требует отдельного reviewed сценария с drain и согласованным
app snapshot. Реальный Telegram account и переписка в Telegram не удаляются.

## Main app-admin: добавить/снять

`iam_roles.py` использует тот же IAM manager и local guards. Private plan:
`{operation:UUID,subject,username,api_project_id,desired_admin:true|false,
expected_grant:null|{id,roleKeys:[sorted exact keys],sequence:EXACT_SEQUENCE}}`.
API project сверяется с текущим `identity.api_audience`; меняется только
`zinusya.admin`. Прочие role keys сохраняются. Для последней удаляемой роли
helper удаляет конкретный grant; это выбранная понятная семантика, **не**
утверждение, что IAM запрещает пустой roleKeys update. No-op не делает PUT.

Pinned v4.20 grant контракт: search POST `/management/v1/users/grants/_search`
с exact user/project queries; GET/PUT/DELETE `/management/v1/users/{user}/grants/{grant}`;
POST `/management/v1/users/{user}/grants` создаёт grant с projectId/roleKeys.
Каждый grant проверяется по id, userId, projectId, details.resourceOwner,
пустому projectGrantId и ACTIVE state. **Path userId не является guard:**
Zitadel GET/DELETE фактически выбирает grant ID и resource owner.

Update заменяет весь набор ролей; IAM API не имеет CAS для этого метода.
Поэтому root — единственный writer, plan фиксирует sequence и точные роли,
helper проверяет их непосредственно перед dispatch. Не заявлять atomic CAS
против постороннего IAM writer. ORG_OWNER и Loom permissions не выдаются.

Source: [grant methods](https://github.com/zitadel/zitadel/blob/v4.20.0/proto/zitadel/management.proto#L4703),
[grant handler](https://github.com/zitadel/zitadel/blob/v4.20.0/internal/api/grpc/management/user_grant.go#L13).
Команды `iam_roles.py` имеют те же config/plan/hash/private-dir flags, что
`iam_human.py`. После ответа root заново читает grant и получает реальный
app-admin JWT; успешный IAM write не выдаётся за проверку app authorization.

Written by Codex (unverified/Codex desktop)
on behalf of Daniel Drizhuk
