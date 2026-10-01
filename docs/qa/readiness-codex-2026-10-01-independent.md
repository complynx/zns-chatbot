# Независимая оценка готовности — 1 октября 2026

**35–75 инженерных человеко-дней по 8 часов (280–600 часов).
Уверенность низкая. Production NO-GO.**

Оценка независимо получена из требований, разрешённых отчётов и исходников
на интегрированном продукте 7ff6659b; root HEAD документации на момент чтения —
41d78519. Прошлые числовые оценки и PROGRESS не предоставлялись и не читались.
Оценщик не изменял файлы, не запускал проверки, службы и не читал credentials.

Это суммарная работа разработчиков и независимого QA, не календарный срок
и не процент готовности. Ожидание доступов, человеческих решений и разрешения
production сюда не входит.

## Состояние и основания

Значительная реализация уже интегрирована, полной текущей приёмки нет.

- Заказы, passes, массаж, knowledge/history, broadcasts, credits, runtime
  и многодоменный importer существуют. Повторная реализация в остаток не включена.
- C/D требуют окончательной композиции, архитектурного подтверждения
  и независимой проверки recovery/privacy. Например, bot/turn_host.go ещё
  делегирует binding, authorization и script-вызовы обратно Bot; наличие
  agenthost само по себе не доказывает завершённую границу ответственности.
- Полный baseline09 — FAIL с незавершённым integration-охватом. Focused10
  подтверждает исправленные предусловия служебных ролей, но сохраняет payment
  failure; отдельный успешный meal-runtime прогон не объясняет прежний сбой.
- Payment/clock candidates имеют блокирующие Code QA findings. Observer получил
  статическое scoped PASS, но живое исполнение F08 не принято. Работа
  в авторских деревьях не считается интегрированной или функционально принятой.
- Полные 40 исходных FQA строк сохраняют незакрытые требования. Глобальная
  матрица требует раздельные исходы, роли, EN/RU, mouse/emulated-touch,
  manual/agent, replay/restart/interruption. Восьмишардный baseline — план
  полного объединения, не выполненный PASS.
- Настоящий Zitadel доступен, но adapter test завершился FAIL на ожидании
  классификации ошибки. SDK PASS использовал administrator PAT и не доказывает
  least privilege. E имеет подготовку, а не исполненную all-domain репетицию.

## Неперекрывающиеся корзины

| Работа | Дни |
| --- | ---: |
| Финальная C/D-композиция, privacy/recovery, operator/job/clock/payment исправления, архитектурные и affected QA gates | 8–16 |
| E: окончательная схема, seed-free import, apply/replay/reconcile, removal, постоянные ссылки и runtime restart | 5–10 |
| Остальной parity: /start, passes callbacks, legacy web routes и детальные действующие контракты | 3–7 |
| Полный baseline, Linux/race/JS/sqlc/ограниченные роли и полная синтетическая FQA с исправлением дефектов | 8–16 |
| Реальные identity/cache/revocation/first-contact/Login, Telegram, model/ASR/source/payment интеграции | 8–18 |
| Финальный remote/base reconciliation, Python exclusions, backup/rollback, production reconciliation и отдельно разрешённый cutover | 3–8 |
| **Всего** | **35–75** |

Исправления каждого класса и повторные affected reviews учтены внутри его
корзины; общей дополнительной QA-надбавки нет. Синтетическая E-проверка
не повторно посчитана как production import.

Нижняя граница предполагает ограниченные исправления текущих кандидатов,
стабилизацию схемы и отсутствие нового системного дефекта в полной матрице.
Верхняя предполагает несколько содержательных циклов ремонта и приёмки,
дополнительные identity/browser/media стенды и import reconciliation.
Новый архитектурный или production-data дефект способен вывести работу
за этот диапазон.

## Критический путь и контрольные точки

C/D-композиция и финальная схема → current whole-proof и recovery controls →
E apply/replay/remove/restart → remaining parity и полная синтетическая приёмка
→ реальные интеграции → отдельно разрешённый cutover.

Три gates, которые сильнее всего сузят оценку:

1. Заморозить окончательный source/schema состав, завершить весь baseline union
   с Linux/special prerequisites и свежую affected C/D FQA.
2. Исполнить all-domain E apply/replay/reconcile/removal/restart
   и независимые EN/RU проверки сохранённых данных.
3. Получить genuine least-privilege identity/runtime/cache/Login и реальные
   model/media/Telegram результаты; пересчитать остаток по незакрытым cells.

Проверены требования архитектуры/миграции, parity-current, DEFERRED,
fqa-scenarios, combined FQA accounting и next-batch plan с полной
восьмишардной областью, baseline09/focused10, QA270/272/273/275,
реальная identity readiness и E execution preparation.
Это оценка готовности, не Code QA verdict и не Functional acceptance.

Written by readiness_independent_oct01 (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
