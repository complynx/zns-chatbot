# Архив прогресса — 26 сентября 2026

Исторические записи; статусы могут быть устаревшими. Текущие задачи — в [PROGRESS.md](PROGRESS.md).

# Прогресс Go-бота

Обновлено: **26 сентября 2026, текущий review-loop после восстановления связи**.
Ветка: `feature/go-platform-sandbox`. Изменения локальные, без commit/push и
переключения production. Цель — полный перенос действующих функций Python-бота.
Текущий Go-стенд ещё не является полной заменой.

## Принято

Каждый перечисленный этап прошёл локальные проверки, независимые Code QA и
Functional QA. Ограничения конкретных прогонов сохранены в checkpoint-документах.

| Сделано                            | Результат / доказательства                                                                                                                                                                                                                                                                                         |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Базовая Go-платформа и PostgreSQL  | Отдельные bot/API/model, типизированные команды и права; sandbox identity пока вместо production Zitadel.                                                                                                                                                                                                          |
| Рабочий Telegram-подобный стенд    | Кнопки, редактирование сообщений, загрузка/скачивание файлов, Mini App, мышь и эмуляция touch; отдельные synthetic users.                                                                                                                                                                                          |
| Базовый ручной/агентский сценарий  | Общие права, история действий и обновление карточек; адаптер OpenAI `gpt-6-luna`, в стенде детерминированная модель.                                                                                                                                                                                               |
| Заказы и дополнительные услуги     | Создание/изменение, наличные, ограничения, история и продолжение агентом. [Checkpoint](docs/orders-checkpoint.md).                                                                                                                                                                                                 |
| Mini App питания и ФИО             | Подписанный запуск, серверный расчёт, сохранение, устаревший draft и точный retry. [Checkpoint](docs/meal-editor-checkpoint.md).                                                                                                                                                                                   |
| Чеки                               | Неизменяемые owner-bound файлы, просмотр, маршрутизация и проверка оплаты; права/version/attempt.                                                                                                                                                                                                                  |
| Уведомления и напоминания          | Очередь, повторные доставки, блокировка получателя, сроки и уведомление об освобождении мест. [Checkpoint](docs/order-notifications-checkpoint.md).                                                                                                                                                                |
| Экспорт XLSX                       | Пять листов, исторические цены, literal text, права, единый снимок и явный отказ при превышении лимитов. [Checkpoint](docs/order-export-checkpoint.md).                                                                                                                                                            |
| Основной quality toolset           | Golden golangci + Nebius, testify, PostgreSQL/race, ESLint/Prettier/Playwright, Dependabot, CI и govulncheck.                                                                                                                                                                                                      |
| Общий FQA toolkit                  | API/auth/pagination, журнал fixtures/recovery/cleanup, browser driver/trace, Docker wrapper и независимый XLSX inspector. Полный gate + свежий Code QA + [независимая Functional QA](qa.local/functional-fqa-toolkit-pass1/report.md) пройдены. [Инструкция](platform/scripts/fqa/README.md).                      |
| Архитектурные материалы            | Презентация и HTML в `architecture.local/`: [report](architecture.local/output/report.html).                                                                                                                                                                                                                       |
| Способы оплаты и основа i18n       | Owner-bound API, обновляемая карточка оплаты, en/ru, выбор языка с защитой от повторов, заданные fallback и CLDR. Полный локальный gate, два Code QA прохода и независимая Functional QA пройдены. [Checkpoint](docs/order-payment-instructions-checkpoint.md). Полная локализация остальных экранов ещё впереди.  |
| Агентское ФИО и профили            | Вопросы не поглощаются ожиданием ФИО; имя можно прислать позже или без запроса, заменить существующее. Общие API-права, история ручных изменений и обновление карточки. Полный quality gate, Code QA pass 5 и независимый FQA pass 2 с реальной моделью пройдены. [Checkpoint](docs/profile-intake-checkpoint.md). |
| Локальный стенд с реальной моделью | Изолированный native-стенд `gpt-6-luna` через Codex, отдельный PostgreSQL, запуск/остановка/перезапуск и mouse/touch-проверки. Code QA и FQA пройдены; данные только synthetic. [Запуск](docs/codex-model-provider.md).                                                                                            |

## Сейчас в работе

Product FQA pass 2 завершён: оплата reject/resubmit/accept, профиль, история,
EN↔RU, административное назначение, чужие/повторные callback и сохранность после
restart проверены. **Этап не принят:** оплаченный пасс показывал недоступную
кнопку отмены. Условие исправлено; PG-проверка четырёх состояний проходит,
свежий Code QA чистый. Положительная парная регистрация и обязательный паспорт
требуют отдельного чистого события в стенде. Завершённый product-стенд остановлен.

Экспорт пассов XLSX реализован и проходит PG-тесты; после замечания Code QA
сохранены source-маркер `free_pass` и история отклонения заменённого чека;
отменённые заявки исключены из workbook. Четвёртый Code QA чистый, PG export
suite проходит. Поля Telegram identity переносятся отдельно. Контекст базы знаний
и регистрации перепроверяет права перед каждым запросом провайдера, включая
отдельные selection/planning и retry; ограниченный discovery-список событий
не используется как источник разрешений. PG-тесты прошли, свежий Code QA чистый.
Удаляемый мигратор дополнен `plan users`; это private candidate artifact,
не разрешение на apply. После замечания Code QA исправлена привязка повторно
прочитанного manifest к проверенному digest; второй review чистый.
Независимая CLI Functional QA идёт.

Свежий общий Windows Go gate: **PASS**, PostgreSQL integration **76.125s**;
полный pinned golangci **0 issues**, JS ESLint/Prettier **PASS**. Полный Linux
race/FFmpeg gate **PASS**, PostgreSQL integration **65.184s**. Исправлены сравнение
публичного JSON при knowledge replay и общий env-срез параллельных config-тестов;
20 дополнительных config race-прогонов прошли. Отдельно устранена нестабильность
теста дедлайна: диагностированы скачки часов Docker, ожидание теперь требует и
монотонно прошедшего интервала, и истёкшего SQL-дедлайна; 12 повторов прошли.

Штатные свежие QA-агенты снова доступны. Внешний экспорт исходников для них не
нужен. Code QA уведомлений нашёл и помог исправить сохранение review парного чека,
атомарность истории доставки, поколения приглашений и справедливость retry-очереди.
Четвёртый независимый проход уведомлений завершён без замечаний. Functional QA
первого product-прогона проверил GUI через нейтральный root browser driver
(браузер недоступен в дочернем агенте): JS, личные заметки, оплата заказа и restart.
Полная приёмка не дана. Устойчивая подсказка при неполном профиле регистрации
добавлена с PG-тестами. Новый FQA на пересобранном замороженном образе проверил
свободный вопрос при ожидании ФИО, отложенное ФИО/паспорт, роль, соло-регистрацию,
обновление прежних карточек, RU/EN и историю. Оплаты/администрирование/повторы ещё
проверяются. Filechooser завис на несколько часов и не выбрал файл; FQA продолжает
через synthetic upload API с проверкой следующих действий в GUI. Code QA
programmable model fixture завершён без блокирующих находок.

Code QA административного контекста обнаружил повторную выдачу сохранённых
привилегированных чтений после отзыва прав. Перезагрузка кэша исправлена и проверена
PG-тестом; свежий review расширил проверку на каждую итерацию модели после других
инструментов. Исправление прошло свежий Code QA. Общий sandbox workflow локализован;
системный fallback сохраняет ключ перевода, реальные ответы модели не переводятся.
Два прохода Code QA чистые после исправлений, FQA подтвердил смену языка карточек.
Датированные тарифы теперь пересчитывают waitlist без пользовательского действия;
PG-тесты открытия тарифа, повторного сканирования и 101 заблокированного события
прошли. Ошибки отдельного события изолированы, startup не блокируется; второй
Code QA чистый. Полная Functional QA lifecycle ещё обязательна.

Удаляемый `tools/migrate` теперь умеет строго проверять manifest/checksums/raw JSONL
и создавать неизменяемый staging с безопасным повтором. Его тесты, vet и pinned
lint проходят. После первого Code QA исправлены блокирующее открытие FIFO и
неограниченное чтение каталога; Linux FIFO/race тесты прошли.
Второй Code QA чистый; независимый FQA прошёл 35 CLI-проверок verify/stage,
сохранности исходников, tamper/replay, приватности, чеков и конкурентного запуска.
Принят только этот ограниченный этап. Это **ещё не импорт**: преобразования доменов,
PostgreSQL/Zitadel apply и итоговая функциональная приёмка остаются обязательными.
Следующий этап в работе: приватный офлайн-план пользователей/профилей с явными
неразрешёнными identity-связями и блокерами полей; без DB/Zitadel apply.

Новый checkpoint: `compose.product.yaml` запускает один Go-процесс приложения,
PostgreSQL с отдельной непривилегированной ролью, Fake Telegram и изолированные
JS/media helpers. На замороженном образе проверены EN → RU, изменение заказа,
stop → start и работа прежней кнопки после перезапуска; история и язык сохранены.
Это проверка разработчика, не независимая приёмка.

Оплата пропусков полностью подключена к ручному GUI и агенту: сумма пары,
выбор между чеком заказа и пропуска, неоднозначность, review и скачивание файла.
Целевые PostgreSQL, unit, lint и vet проходят. Административное назначение
реализовано в домене/API (включая free-pass и сохранение финансового аудита),
его GUI и уведомления/напоминания сейчас добавляются. Независимые QA ожидаются.
Полный gate повторяется после стабилизации параллельно добавляемой fixture-модели;
промежуточная ошибка сборки незаконченного helper не считается дефектом продукта.

Свежий внешний Code QA пока заблокирован автоматической проверкой передачи
исходников в отдельный Codex-процесс. Запрошено конкретное разрешение; конфиги,
секреты и production-данные исключены. Обход блокировки не применяется.

Актуальный checkpoint: знания/личные заметки, JS-инструмент и массаж подключены
к runtime. На замороженном Telegram-стенде проверены предложение знания → ручное
одобрение, запись на массаж мышью, смена RU → EN, отмена и сохранение состояния
после перезапуска. Полная Functional QA этого состава пока **не принята**:
живой прогон нашёл неверный язык memo-ответа и неудачный поиск нескольких фактов.
Исправления прошли отдельные live Luna-тесты memo, EN/RU/PL/DE и сочетания знаний
прошлого события с общей базой; требуется повторить полный GUI-сценарий.
Текущий одноразовый стенд остановлен, его БД удалена. Touch-сценарий массажа
пока не засчитан. Windows Go gate ранее обнаружил пустое сообщение при
навигации — исправлено, три затронутых теста прошли; общий gate повторяется.

Параллельные направления: история/summary и чтение архива; агентская регистрация
и приглашения на пропуска; домен оплаты пропусков. Для последнего добавляются
атомарная оплата пары, отдельные receiving/review provenance, immutable attempts
и защита от устаревшего решения. Это ещё не завершённая Telegram-функция.
Реальная локальная настройка Zitadel остаётся на отдельном ожидающем разрешении
после отказа автоматической проверки доступа; production не изменён.

Последний review-loop: аудио/видео — исходный голосовой вопрос и кадры
разных уточняющих диапазонов сохраняются; целевые PG-тесты и lint проходят.
Code QA pass 6 выявил восстановление сохранённого voice-действия после истечения
исходного файла; дефект воспроизведён и исправлен, AV/media PG-тесты и lint
проходят. Свежие независимые Code QA через изолированные текстовые Codex-процессы
не нашли новых подтверждённых дефектов. FQA с Luna пока не принят: выбор
старого чека голосом и использование изображения требуют повторной проверки.
Два финальных вызова OpenAI Transcribe ранее точно распознали synthetic EN/RU,
ключ убран; routine ASR использует fixtures. Sticker decoder Code QA pass 3 и
transport Code QA pass 3 без замечаний; пересобраны изолированные decoder/broker.
В Telegram-стенд добавлены реальные sticker/custom emoji upload и интерфейс,
устойчивые ID, UTF-16 entities и сохранение metadata. Целевые PG/Go-тесты,
ESLint/Prettier проходят. Повторный browser-прогон совпал с общим сбоем OpenAI;
его ответы «модель недоступна» не считаются доказательством дефекта продукта
или успешной приёмкой. Повторить после готовности новой модели со skills.
Первый доменный слой пропусков прошёл целевые PG-тесты и Code QA pass 2; API,
GUI и Functional QA ещё впереди. Общий quality gate текущего состава не завершён.

| Работа                                | Состояние                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| ------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Durable Telegram inbox                | Пачка и receive-offset сохраняются атомарно до обработчиков. Целевые PG-тесты, включая kill реального процесса и восстановление без upstream-копии, проходят на Windows. Свежий Code QA №2 без замечаний. Functional QA и полный Linux race gate ещё не приняты. [Контракт](docs/telegram-inbox.md).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| YAML/env                              | Loader и runtime подключены; unit/lint и startup health через YAML проходят. Code QA интеграции без замечаний. FQA полного запуска впереди. [Контракт](docs/configuration.md).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| Markdown Telegram                     | Конвертер, доставка, сохранение entities и безопасный GUI подключены. Mouse/touch browser-тесты и пакетные проверки проходят. Исправлен адресат ссылки у вложенного изображения; свежий Code QA №2 без замечаний. Независимая Functional QA ещё впереди. [Контракт](docs/telegram-markdown.md).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| Язык и тон                            | GUI locale исключён из контекста реальной модели, текущий вопрос явно завершает ограниченный контекст. Строгие live EN/RU/PL/DE повторно прошли после разделения на skills; ответы совпадают с языком текущего вопроса при другой истории/UI locale.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| Длинные списки                        | Owner-bound пагинация заказов/оплат по 10 карточек реализована; целевые PG-тесты, усиленный сценарий 320 заказов, lint и Code QA проходят. Browser FQA и race впереди.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| Единый app/runtime                    | Один процесс bot/API/model/Mini App; mixed flow и graceful restart проверены на одноразовой БД. HTTP drain отменяет запросы по сроку и ждёт завершения handlers. Server-тесты/lint и Code QA №2 проходят. [Контракт](docs/single-process-runtime.md).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| JS helper                             | Инструмент подключён к агенту; helper и supervisor изолированы в networkless/read-only контейнере. PG, unit/process и статическое ревью интеграции прошли. Контейнерная проверка разных nonroot UID прошла: цикл/Proxy-преобразование прерываются, следующий запрос работает. После уточнения IPC group permissions нужно свежее ревью и полная Functional QA. Стенд остановлен. [Контракт](docs/agent-scripting.md).                                                                                                                                                                                                                                                                                                                                                                                                      |
| Skills                                | Реальные OpenAI/Codex выбирают по каталогу `use when` только нужные embedded skills. Исправлен приём неоднозначного JSON от модели; unit/lint и свежий Code QA №2 проходят. Live multilingual проверка проходит; domain Functional QA впереди.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| Observability                         | Contextual slog с редакцией секретов, Prometheus, optional OTLP, HTTP/model/update/PG spans подключены в runtime. Collector-тесты подтверждают доставку и корреляцию; tests/vet/lint и Code QA №1 проходят. Транзитивный gRPC обновлён до исправленного 1.83.2, govulncheck чистый. Functional QA впереди. [Контракт](docs/observability.md).                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| Knowledge и memo                      | PG/API, tools и Telegram GUI подключены; фильтр → human review и страницы с полным охватом фактов проверены. После живой FQA исправлены поиск по нескольким темам, выбор внутреннего memo-ключа и язык ответа. Live Luna memo/EN-RU-PL-DE/scoped-facts проходят. Карточки очищены от технических ключей; повторная полная FQA и свежее Code QA ожидаются. [Контракт](docs/knowledge.md).                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| Смысловая обработка фото и документов | Подключены хранение/API, PNG/JPEG-модель, подписи, выбор назначения/заказа и owner-bound кнопки. Изображение не поглощается ожиданием чека или ФИО. Целевые PG-тесты и браузерные RU mouse / EN touch сценарии проходят. Code QA выявил рассинхронизацию видимых вариантов с контекстом модели и потерю загрузки при временном сбое — исправлено, добавлены проверки повторов и смены языка/заказа. Проверки Go/race и браузерные наборы прошли; независимый FQA с реальной Luna нашёл сбой естественного выбора заказа. Естественный выбор, неизвестное назначение файла и история чеков исправлены; свежий FQA с реальной Luna проверяет сценарии. Дополнительно унифицировано восстановление сохранённой операции после потери ответа и истечения файла; Code QA повторяется. [Scope](docs/media-intake-checkpoint.md). |
| Аудио и видео                         | Изолированный Go worker реализован: OpenAI Transcribe, проверка фактических 240 секунд, редкие кадры и дополнительная раскадровка диапазона. Реальные WSL-фикстуры проходят, включая Opus/AAC и поддельные заголовки. Исправлены совместимость ffprobe 6/8, ранний отказ при потоковом сканировании и ограничения потоков; FFmpeg-тесты обязательны в тестовом образе. MP3 и повёрнутые видео с телефона проходят граничные тесты. Декодер отделён от сетевого ASR-процесса: без сети и ключа, с ограничениями контейнера; независимый Code QA воркера пройден. Первичные кадры зависят от длительности; плотный диапазон — по запросу модели. Интеграция с ботом, реальный ASR и FQA ещё не завершены. [Контракт](docs/audio-video-intake.md).                                                                            |
| Custom emoji и стикеры                | Добавлено в scope: реальные WEBP/TGS/WEBM вместо placeholder, повторное использование описаний. Реализован PostgreSQL-кэш 5000 описаний, ленивый exponential decay с настраиваемым half-life. Новый элемент гарантированно попадает в первую половину по весу, исторический больший вес сохраняется. PG-тесты, lint и независимый Code QA кэша пройдены. Telegram-интеграция и Functional QA впереди. [Контракт](docs/sticker-cache.md).                                                                                                                                                                                                                                                                                                                                                                                   |
| Пропуска                              | Домен регистрации/пар/очередей/tier, API, ручные Telegram-карточки и агентские команды реализованы. ФИО/паспорт/роль и доверенные contact/forward metadata подключены; PG, unit, lint и отдельные mouse/touch-проверки проходят. Полная Functional QA ещё не принята. Оплата: домен/API/очередь/сумма пары проходят PG-тесты; GUI и маршрутизация медиа подключаются. [Контракт оплаты](docs/pass-payments.md).                                                                                                                                                                                                                                                                                                                                                                                                            |
| Массаж                                | Домен/API/Telegram, расписание, лимиты, staff-настройки и уведомления реализованы. PG и lint проходят; статические замечания проверены. Живой GUI: RU запись → EN отмена и сохранение настроек после restart проверены. Touch и независимая финальная FQA ещё не засчитаны.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| История и summary                     | Последние K, приватный архив, реальные summary и owner-bound чтение реализованы. PG проверяет late commits, CAS, privacy и недоступность summary. Общий gate выявил потерю двух видов контекста — исправлено, исходные тесты проходят. Свежая встроенная Code QA и полная Functional QA ещё нужны; внешний экспорт исходников не требуется. [Контракт](docs/conversation-history.md).                                                                                                                                                                                                                                                                                                                                                                                                                                      |

## Что остаётся до полной приёмки

Ниже — условия итоговой приёмки, не перечень отсутствующего кода. Реализованные,
но ещё не принятые части отмечены в таблице выше. Зелёные отдельные тесты не
закрывают эти условия без требуемых QA.

- Агент: окончательная Functional QA подключённой JS-среды и skills;
  последние K сообщений/действий, summary ранней истории и инструменты
  чтения доступной истории. Все инструменты работают с правами пользователя.
- Ответы на языке текущего вопроса, независимо от `input.language` и языка истории;
  live-model тесты переключения языков. Тон девушки-помощницы: коротко, дружелюбно,
  эмоции/смайлики; персона влияет только на текст, не на решения и права.
- Markdown → Telegram Markdown: форматирование и ссылки на людей, чаты, темы,
  сообщения; безопасное экранирование и тесты сохранения адресатов ссылок.
- Knowledge: будущие и прошедшие события (переход по времени окончания; сведения
  будущего события приоритетнее исторических), общие знания, небольшая личная
  память пользователя. Администраторы добавляют знания; предложения пользователей
  проходят фильтр агента и review администратора/амбассадора с нужным правом.
- Подбирать подходящие библиотеки с лицензиями MIT/BSD/Apache 2.0/MPL;
  проверять точную лицензию выбранной версии и обязательства распространения.
  Перед добавлением: известные уязвимости и security-практики, сопровождение,
  релизы и реальные downstream-пользователи. Stars/downloads не считать
  доказательством использования; подозрительные показатели исключать, пробелы
  доказательств указывать. Решение и источники фиксировать в dependency review.

- Проверить единый runtime на полном продукте. Bot/API/agent —
  модульные границы, отдельное развёртывание пока не требуется. Перезапуск:
  остановить старый экземпляр, дождаться его завершения, затем запустить новый;
  без rolling overlap и распределённой координации. Сохранить восстановление
  после сбоя, транзакции и идемпотентность; изолированные медиадекодеры —
  вспомогательные процессы, без независимой бизнес-обработки событий.
- Конфигурация Go: типизированный YAML с переопределениями из env; приоритет
  `defaults → YAML → env`, совместимые `ZNS_` и вложенный разделитель `__`.
  Проверка типов/обязательных значений при запуске, тесты приоритетов и ошибок,
  документированный пример без секретов. Реализовано; остаётся полная Functional QA.
- Observability: контекстный `slog`, уровни trace/debug/info/warning/error,
  согласованная с Python политика событий и ПД; централизованная редакция секретов.
  Метрики и distributed traces для bot/API/agent/workers/PostgreSQL, корреляция
  с логами. Изучить действующие настройки Alloy и обеспечить совместимость с
  Grafana Labs; проверить доставку на локальном collector-стенде, без ПД в labels.
  OpenTelemetry tracing — обязательная возможность платформы независимо от
  готовности Alloy: YAML/env для enabled, OTLP endpoint и sampling; отключённый
  exporter не мешает работе. Локально проверить включение и доставку spans.
- Graceful shutdown: по сигналу прекратить приём новых событий, закончить текущую
  пачку Telegram либо надёжно сохранить незавершённое для продолжения после
  рестарта. Ограниченный drain, корректные offset/ack, защита от повторных эффектов,
  освобождение задач workers и финальный flush телеметрии. Проверить сигнал и
  рестарт во время обработки, таймаут drain и восстановление сохранённой пачки.
  Сохранять все принятые события в PostgreSQL до ack/сдвига Telegram offset при
  обычной работе, не только при shutdown: восстановление обязательно и после
  аварийного завершения без сигнала. Дедупликация по update ID, порядок в чате,
  сохранённый payload и состояние обработки.
- Полная i18n: Telegram, Mini App, агент, ошибки, уведомления и админские сценарии;
  выбор языка, fallback, форматирование и обновление старых сообщений.
- Полная функциональность пропусков: пары, очереди, уровни/промо, ФИО/паспорт/роль,
  оплаты, экспорт и администрирование.
- Полная функциональность массажа: специалисты, расписание, длительности,
  дневные лимиты, настройки и напоминания. Базовая booking-fixture не заменяет её.
- Остальные действующие команды и операции Python-бота; knowledge/document
  refresh, lineup, квоты и проверка реальной модели.
- Production Zitadel: общее представление пользователя с авторизатором,
  отдельное приложение бота и проверка каждого запроса; отдельный test Zitadel.
- Одноразовый forward-мигратор Mongo → PostgreSQL/Zitadel, resumable import,
  сверка данных/чеков и удаление мигратора после переноса.
- Итоговая parity-матрица по всем действующим функциям и приёмка полного продукта.
- Точные fault-injection по методу/получателю и crash barriers, когда соответствующие
  сценарии потребуются FQA. Текущий 429 относится к следующему запросу.

## Как работаем дальше

Root: интеграция, инструменты и единственный владелец стенда вне FQA-прогона.
Разработчики получают непересекающиеся файлы; Code QA проверяет независимо.
Functional QA получает замороженный стенд и требования без деталей реализации.
После двух чистых QA и обязательных проверок следующий этап начинается автоматически.

Подробный план: [go-migration.md](docs/go-migration.md).
Этот файл обновляется после приёмки этапа, существенного изменения плана или блокера.
Непроверенная реализация не переносится в «Принято».

## 2026-09-26 — Order GUI i18n built, not accepted

Order cards, actions, receipt/export notices and payment notifications now use
English/Russian typed catalogs, CLDR meal-day plurals and localized prices.
Existing deterministic notices do not preserve the old locale after a language
change; agent conversational replies retain their current-question language.
Focused real PostgreSQL order/proof/payment/notification/history suite passed
with bounded parallelism (22.118s). Owner-local Go tests, ESLint and Prettier pass. Final actual /language callback
and native-answer preservation test passed (1.024s); owned lint findings are zero.
Browser acceptance remains pending on a frozen stand; scope is documented in
[orders-localization.md](docs/orders-localization.md). Independent external Code QA
can now use fresh built-in reviewers without source export; not accepted.

History regression checks preserve committed manual-action metadata and delivered
system notices in the new private archive; unchanged failing tests now pass.
Migration 031 remains draft and includes a system-event kind. No production data
or existing stand was changed for these checks.

### Programmable model fixture — built, not accepted

- Added synthetic-only scoped fixture provider and bounded plan queues. Owner/update/turn and partial typed input must match; no fallback or permission grants. Atomic fixture plus text injection avoids a live polling race.
- Unit tests pass for malformed/oversized input, cross-owner isolation, sequence enforcement, parallel consumption, cancellation/redirect refusal, and atomic enqueue. Parent-run real PostgreSQL `TestSandboxModelFixture` passed, including JS tool result, private memo persistence, replay, and denied visitor action.
- Optional `compose.product.fixture.yaml` and `docs/model-fixtures.md` describe use. No stand rebuild performed by this task. The fixture controls Plan only, not knowledge assessment or semantic summary interfaces.
- Pinned formatting applied; combined lint found one unused helper parameter, removed and focused tests rerun. Final lint confirmation, Linux race check, fresh Code QA, and Functional QA remain acceptance gates. Native race check cannot run with the current CGO-disabled toolchain. Use fresh built-in reviewers; external source export is unnecessary.

## 2026-09-26 — продолжение после handoff
Runtime focused bot/API/config/cmd tests, PG identity/reminder/notifications (2.839s), vet: PASS. Свежий CodeQA выявил starvation массажных уведомлений; durable recipient rotation migration044 и regression на101 недоступного получателя + healthy recipient проходят PG (2.295s); повтор runtime PG 2.628s. Финальный общий lint и свежий CodeQA ожидаются. Lineup tests/vet/lint PASS, fresh CodeQA clean; FQA обнаружил отсутствие tzdata в Docker image, Dockerfile исправлен, новый образ пока не принят. Users apply tests/PG/vet/build/mod verify/lint/govulncheck PASS по scoped evidence; оба independent QA идут. User подтвердил цель полного переноса, внедрение sqlc и библиотечный подход к скриптам агента. Стенды QA раздельные, production/commit/push не выполнялись.

Users apply: fresh CodeQA clean qa.local/users-apply-codeqa-resume/report.md; independent CLI/PG FQA qa.local/users-apply-fqa-resume/report.md — no defects, apply/replay, negative inputs, drift, partial resume, concurrent apply, kill before commit; restricted role has no importer access and can read imported runtime aggregate. Actual DROP schema skipped after auto-review rejection; non-destructive independence evidence used, not claiming drop test. YAML v4.0.0-rc.6 explicitly approved; two import replacements, baseline/post config/cmd tests+vet and tidy/verify PASS. Order notification starvation reproduced (recipient26 stays blocked) and fixed ORDER BY available_at,id; focused runtime PG6.168s PASS, fresh QA pending.

2026-09-26: sqlc scoped conversation/history Code QA + Functional QA PASS (qa.local/sqlc-codeqa-resume/report.md; qa.local/sqlc-fqa-resume/report.md), including restart persistence; semantic summary generation remains outside acceptance. User added mandatory hidden unauthorized tools across model schemas/skills/script discovery. Dedicated tooling_research owns ergonomics comparison with Codex/OpenClaw, reusable search/filter/composition, and observability of recent agent struggles with trace drilldown. Research is not implementation acceptance.

2026-09-26: current cmd/internal Go tests PASS before sandbox delivery upgrade. Sobek fresh CodeQA found unhandled detached rejection and test goroutine assertion; owner fixed, focused tests/lint pass; fresh combined worker+host CodeQA started. Host current-state grounding fixed with two real PG regressions. Event importer CodeQA found exact-key/date validation gaps; fixed and new binary SHA256 931D60BAC1F414967DB3B05D960F497285385E5A32EFA2F17D964233571FFE14 awaits fresh gates. Batch FQA core flows passed but misleading paid-total wording corrected en/ru; admin plain format label corrected. FQA formatted/channel delivery needs upgraded fake, currently in progress. Tooling report qa.local/tooling-research/report.md; native JS first, gojq not implemented. User authorized code logging after secret/private literal redaction; memo content/read results excluded. Structured log/export implementation underway; no dashboard. Deployment config uses pgvector/pgvector:pg17, live extension/index state unverified.

2026-09-26: events foundation accepted after qa.local/events-codeqa-current/report.md and qa.local/migration-events-fqa-current/report.md (57 in-contract checks); current CLI SHA256 931D60BAC1F414967DB3B05D960F497285385E5A32EFA2F17D964233571FFE14. Receipt DROP denied by auto-review, no deletion proof claimed; disabled Zitadel login does not invalidate stable offline Core owner linkage. Root later clarified source field shapes in operator docs only. sqlc diff passes current migrations. Full PG with default parallelism exhausted shared locks; bounded repeat found one reminder failure. Existing TestFailedReminderIsNotAttemptedAgain red, terminal Telegram400/403 classification fixed, reminder/notification/Zitadel suite PASS9.938s. Worker source-map ambient read reproduced by fresh QA; both runtimes disable source maps, focused regressions green. Host read-refresh fixed, focused PG+vet/lint green. FQA copied coherent source to qa.local/script-fqa/frozen; isolated build started. User requested shared/private memory hierarchy; foundation implementation assigned, full progressive model integration remains required.

2026-09-26: Docker lifecycle cleanup requested by user. Removed 61 obsolete containers, 39 unused zns image tags, 17 disposable volumes from six completed QA projects, and 11 project networks. Active zns-script-fqa2, shared zns-sandbox PostgreSQL, zns-identity and unrelated Touchzouk resources preserved. Build cache prune reported 76.26 MB + 18.65 GB reclaimed; system df cache decreased from 33.87 GB to 15.14 GB (retention target 5 GB is not an achieved total-size claim). Images 53 -> 14; containers 75 -> 14. Additional unattached legacy-project volumes were excluded after automatic review rejected their removal without disposal evidence; no data deletion workaround used. Audit: qa.local/docker-cleanup-20260926. Remaining running services verified; docs/code-quality.md now requires ownership, evidence preservation and cleanup after QA. Migration remains in progress.

2026-09-26: Model settings Code QA clean (qa.local/model-settings-codeqa/report.md), independent actual EN/RU mouse/emulated-touch FQA + synthetic OpenAI wire clean in exercised scope (qa.local/model-settings-fqa/REPORT.md). Remaining transport/malicious-input limitations explicit; not full migration acceptance. Capacity fresh Code QA clean after independently reproduced bare-claim deletion leak fixed; 25 real-PG tests passed, separate Functional QA running. Active-event runtime/config migration050 built, 5 PG tests pass after Core API owner-bound global-order lookup corrected zns_bot P1; remaining preexisting split-role metadata/history paths surfaced and tracked.

2026-09-26: Script candidate2 FQA discovered ordinary planner schemas and selected skills exposing create/edit/select/export without capability; root added fresh Core capability API + provider projection, fail-closed absent grants, guessed-action rejection, hidden skill blocks and same callback discovery API. Root business PG test passed1.323s before later API extraction; affected rerun pending. Sandbox blockquote/pre continuity defect reproduced in browser and corrected with focused Edge DOM regression + ESLint pass. Memory CodeQA found valid escaped search pages >32KiB and missing ordinary shared/legacy source attachment; fixes assigned, independent FQA uses frozen build. G101 on public AdminUtilityPassSummary/AdminUtilityPasses translation identifiers corrected by precise RegistrationSummary/RegistrationCount naming, no suppression; i18n lint0.

2026-09-26: User authorized all interactions with CLX Test Bot (6087685431) from designated real test account, strictly final checks after main synthetic QA. Telegram Web opened for user login; authorized account shows existing named bot; local config/config.yaml token prefix matches expected bot ID without exposing token. No real bot messages sent by agent yet; no production/deploy/commit/push. This authorization does not extend to other bots/accounts.

### 26 сентября — ограничение payment instructions

- Независимый Code QA выявил, что payment_instructions оставался в схеме и skill после отзыва can_book, хотя API запрещает действие.
- Проекция теперь скрывает весь order_action через null при отсутствии разрешений; skill также скрывает инструкции, guessed action отклоняется. API ACL сохранён.
- go test -p 1 ./internal/agent: PASS, включая missing/restricted/revoked и разрешённые роли. Начат свежий независимый Code QA; Functional QA ещё требуется.
- Capacity FQA проверил сценарии, но независимость частично нарушена случайным чтением прежнего summary; требуется свежий reviewer release/replay перед приёмкой.

### 26 сентября — очередь пассов через Core API

- Имена участников включены в авторизованную страницу очереди, из того же SQL snapshot и только для возвращённых записей. Прямое чтение core.users процессом бота удалено.
- PostgreSQL TestPassBookingPagesSurviveBoundaryDeletion PASS: пагинация, удаление границы, соответствие names текущей странице и отказ обычному пользователю. Bot unit suite PASS.
- Свежий Code QA запрошен; функциональная приёмка split-role образа остаётся обязательной.
- Назначен свежий независимый capacity Functional QA. Telegram metadata и history summary переводятся за Core API отдельными владельцами файлов.
- Свежий независимый Code QA business visibility, renderer и queue: PASS без actionable findings; отчёт platform/qa.local/business-visibility-final-codeqa/report.md. Реальные DB-роли подтверждены: zns_bot не имеет Core USAGE, capability query работает под zns_api. Полный lint и split-role GUI ещё не подтверждены.

### 26 сентября — split-role массаж и достоверность QA-образа

- Имена массажистов переведены из прямого Core SQL бота в авторизованный API, возвращающий только owner/name. PostgreSQL TestMassageAuthenticatedAPI PASS, включая отказ без токена и отсутствие данных другого события.
- Уведомления массажа переводятся за сервисный API отдельным владельцем; migration052 зарезервирована, migration051 остаётся у orders importer.
- Свежий capacity FQA обнаружил несовпадение сохранённого образа с ожидаемым. Существующий sha256:9f237f4e385f7a29dc48ba6e196447f2ebf3095104456aa286da77fc69edbb72 не содержит требуемых reservation claims. Приёмка остановлена; необходим новый frozen image. Это не доказательство дефекта текущих исходников.
- Telegram metadata: фактический zns_bot не может UPDATE core.users, но полный обработчик именованного сообщения/callback/replay работает через Core API. Focused PG + units PASS по отчёту implementation; независимый QA ещё нужен.

### 26 сентября — общий gate и удаление памяти

- Общий go test -p 1 ./cmd/... ./internal/... PASS на текущем составе.
- Прямых Core SQL и Service{DB:b.DB} в bot исходниках поиском больше не найдено; это статическая проверка, не замена полному split-role FQA.
- Полный pinned lint нашёл 15 замечаний к константам, форматированию, shadow и SELECT * в новых тестах. Исправляются без suppressions/ослабления правил.
- Свежий Code QA обнаружил сохранение тела удалённой памяти в knowledge_operations и выдачу при replay. Назначено воспроизведение и исправление для document/legacy memo/shared fact; новый frozen image ожидает устранения.
- Полный PostgreSQL integration gate запущен отдельным процессом; результат ещё не получен.

### 26 сентября — результаты фоновых gate

- Полный integration gate завершился `Test killed: ran too long (11m0s)`, wall time3536s на фоне длительных задержек tool calls. PASS не заявляется; повтор нужен с подробным журналом после согласования deletion API.
- Последний lint завершился ошибками typecheck временно отсутствующего MemoryDeletionState/связанных методов во время работы двух владельцев. Строка `0 issues` не означает успешный gate (exit1).
- Независимый diagnostics Code QA запущен: приватность логов, sanitized code, права экспорта и bounded reads.
- Реальный Edge DOM тест непрерывности blockquote/pre PASS на текущем renderer.
- Полный ESLint `eslint . --max-warnings 0` PASS. Исправлено только форматирование tests/entities.mjs; targeted Prettier check renderer/test/package PASS.

### 26 сентября — диагностика и проверка генерации

- Независимый diagnostics Code QA: P2 тайм-ауты/invalid/oversize скриптов сводятся к unavailable; исправление поручено владельцу script integration после privacy-правок. Unit observability и CLI PASS; общий acceptance ещё нет.
- go mod verify PASS: all modules verified.
- Пинованный sqlc diff -f ../../sqlc.yaml PASS на текущих migrations/queries; сгенерированный код синхронен.
- MemoryDeletionState контракт и legacy-read invalidation добавлены; восстановление общей сборки и regression gate проверяются владельцами.
- Текущий PostgreSQL focused gate Test(Massage|ActiveEvent|BusinessCapabilities) PASS9.973s. Общая компиляция integration восстановлена. Независимый массаж Code QA возобновлён для собственных недостающих тестов.

### 26 сентября — независимый massage Code QA завершён

- Fresh scoped Code QA PASS без подтверждённых дефектов. Независимый TestMassage на PostgreSQL PASS16.226s.
- Миграция052 проверена с историческими scheduling rows: записи и точные timestamps сохранены, старая таблица оставлена. QA temporary DB удалена,12 source hashes неизменны.
- Fresh deletion Code QA запущен на исправления receipt/script/legacy read; Functional QA ещё требуется.
- Новый полный PG gate запущен с подробным JSON журналом platform/qa.local/current-gates/integration.jsonl для локализации возможного тайм-аута.
- Memory domain окончательно frozen: PostgreSQL Memory|Knowledge PASS58.625s, vet PASS. Script privacy+diagnostic classification focused PG PASS22.265s по отчёту владельца.
- Последний полный lint оставил одно shadow замечание knowledge_context.go; исправлено заменой локального объявления на присваивание. Повтор общего lint ещё нужен.
- Создан runtime-candidate3 snapshot (572 файла) и source-sha256.json. Docker build zns-runtime-candidate3:frozen запущен; образ ещё не подтверждён как готовый. Functional acceptance не заявляется.
- Fresh active-event Code QA PASS в проверенном объёме15 focused PG/config; подробности и ограничения в qa.local/active-event-final-codeqa/report.md.

### 26 сентября — candidate3 и новые приёмочные стенды

- Docker runtime build PASS; image inspect подтвердил sha256:7414bc94e357d8b9de102a9e63ff20b29a1049fd7854a6c2cb9d15e18235d8f8. Снимок и SHA256 сохранены qa.local/runtime-candidate3.
- Полный закреплённый golangci-lint cmd/internal/integration PASS0 issues после shadow fix.
- Fresh Code QA удаления памяти без blocking findings: PostgreSQL Memory/Knowledge, независимые1001 ledger backlog и late-finish tests PASS. Сохранённые ledgers очищаются лениво ограниченными пачками; текущая проекция очищается независимо. Это не полное историческое стирание.
- Независимые FQA capacity/memory/visibility получили frozenruntime, отдельные stand/report ownership. Matching script-evaluator строится из того же снимка; старые evaluator images для приёмки не разрешены.
- Новый diagnostics Code QA запущен после исправления классификации ошибок.

### 26 сентября — результат общего PG gate и уточнение capacity scope

- Полный PG gate завершён exit1:522 pass events, один реальный failed scenario TestRegistrationAuthorizationBeforeModelAfterHistoryRead/database_failure (родитель и package дают ещё2 fail events). Получен internal_error вместо ожидаемого обработанного отказа; требуется диагностика, не ослабление теста.
- Capacity FQA scope исправлен: root ошибочно требовал резерв при обычном unpaid/cash выборе. Python и docs/order-capacity.md резервируют после proof/validation; исторические bareclaims отдельно должны переживать время/restart и освобождаться при удалении. Исходники не менялись для подгонки проверки.
- Diagnostics свежий QA нашёл2 P2: memory.* операции превращаются в unknown; wire ошибки scriptclient теряют finite classification до host. Первое исправлено конечным allowlist8 операций + exportedlog regression; observability tests PASS. Второе ещё исправить.
- Candidate3 образы остаются неизменны во время FQA; новые diagnostics изменения пока только в рабочих исходниках.

### 26 сентября — типизированные wire ошибки JS

- scriptclient теперь возвращает безопасные ErrInvalidResult/ErrResultLimit при malformed/oversized worker reply; Evaluate и Execute используют одну decoder классификацию. Bot diagnostics сохраняет тип вместо unavailable.
- Новый реальный Unix-socket HTTP worker тест проверяет malformed JSON, payload limit, wire limit и приватный worker error до структурированного журнала. Секретный текст не попадает в лог.
- Полные scriptclient и bot unit suites PASS после изменения; scoped pinned lint запущен.
- Точная причина failing PG: optional /model-settings/permissions500 блокировал отправку AgentUnavailable, хотя повторный вызов модели был правильно запрещён. Владелец исправляет failclosed скрытие optional menu; проверки авторизации не ослабляются.
- Wire regression оформлен как internal test; путь сокета через t.TempDir, TestScriptWireLogs PASS на Windows. Исправлены оба замечания scoped lint без suppressions; полный lint запущен снова.
- Optional model menu fix: focused Registration+ModelSettings PostgreSQL PASS8.702s, safe reply/no controls/history assertions сохранены. Свежий независимый Code QA этих изменений и wire diagnostics запущен.
- Model menu regression assertions extracted to helper to satisfy cognitive complexity; no checks removed. Focused recheck launched.
- Fresh FQA isolated P2 private-notes empty-state regression: no legacy memos -> fresh callback returns stale; with memo list works. Documents-only owner also affected. Candidate3 source unchanged; root must fix preserved manual empty state next.
- Fresh wire Code QA found remaining finite worker error gap: actual worker timeout/invalid_result envelopes discarded by decode. Typed malformed/oversize result fix is insufficient until finite worker codes preserved. No broad diagnostic acceptance claimed.
- Visibility final FQA PASS tested scope на точных app/evaluator hashes: схемы/skills/discovery/guessed calls/retained function/mid-model revoke/stale callback/positive restore, RU+EN UI. ACL и язык восстановлены, собственный стенд остановлен. Report qa.local/visibility-final-fqa/report.md; interrupted transport replay и live provider не проверены.

### 26 сентября — finite worker codes

- Decoder сохраняет только timeout/canceled/invalid_result/invalid_request как типизированные безопасные ошибки; неизвестный worker text отбрасывается. Bot logs различают timeout/canceled/invalid и недоступность.
- Реальные Unix wire tests дополнены конечными worker envelopes; scriptclient/bot/observability unit suites PASS. Общий lint запущен.
- Orders importer P2 воспроизведён RED на реальной PG: tokened reserved service отсутствует в choice. Builder добавил strict reject без изменения легитимных historical bareclaims; ждём gates/freeze.
- Memory empty-state дефект локализован в renderer, callback auth не меняется; builder добавляет en/ru empty/documents-only regression.
- Orders importer fix frozen: realPG red/green отказ unselected-service tokenclaim без частичных записей, full importer/CLI tests+race+vet+lint PASS по отчёту владельца. Назначен свежий black-box CLI/PG Functional QA поддержанного среза; общий мигратор остаётся неполным.

## 2026-09-26 — candidate4 preparation

- Full PostgreSQL integration run PASS (522.68 s), no failing events: platform/qa.local/current-gates/integration-after-menu-fix.jsonl. Optional model menu ACL lookup failure no longer prevents the localized unavailable response.
- Capacity accepted in scoped modern runtime: fresh Code QA plus candidate3 GUI/PG FQA and follow-up. Real upload/cancel, competing buyer claims, exact concurrent Core receipt replay all PASS. Reports: qa.local/capacity-independent-fqa/candidate3-report.md and candidate3-followup-report.md. Stand stopped, DB retained.
- Empty private notes renderer fixed; fresh independent Code QA PASS with four EN/RU PostgreSQL cases and existing memo lifecycle regression. Report qa.local/private-notes-codeqa/report.md. Fresh UI FQA assigned for candidate4.
- Independent diagnostics QA found ServeRPC transport closing before its timeout envelope. Transport bound now uses existing six-second process bound, execution remains five seconds. Real RPC blocked callback regression PASS, scoped pinned lint zero issues, scriptworker/scriptclient/bot/observability suites PASS with bounded test concurrency. An initial unconstrained worker suite timed out on 16k Unicode evaluation; isolated and bounded reruns passed without product deadline changes.
- QA toolkit accepts explicit synthetic stand signing key without recording it. Four toolkit tests and ESLint PASS. This unblocked exact capacity command replay.
- Supported modern orders importer fresh Code QA PASS, full importer PG suite/vet verified. Public exporter shapes expanded; independent source-blind CLI/PG Functional QA continues. Full legacy migration remains incomplete.
- candidate4 frozen source snapshot: qa.local/runtime-candidate4, 577 files. New runtime and script image builds underway; candidate3 remains unchanged.
- candidate4 images built and inspected: runtime sha256:87a312364e3de8d374aa6c0c3b9a1532d7812b7142a064b0d6679c7dfd0c2c39; script sha256:3c4efc7d9dcec5970497548ccc3aec5e388482e4d7794f124b0ad1b339c0759c. Fresh source-blind private-notes FQA launched on immutable images.
- CI product lint command `golangci-lint run ./cmd/... ./internal/... ./integration/...`: PASS, zero issues. An exploratory broader `./...` also included two old local probe programs and failed their formatting/print rules; no exclusions added, product gate scope unchanged from CI.
- Supported modern orders importer accepted: independent qa.local/orders-import-final-codeqa/report.md (full importer PG suite + vet) and qa.local/orders-import-fqa/report.md (source-blind CLI/PG, historical prices, proof bytes/owners, exact slots, rollback, resume/concurrency/reconcile, refusals). No full legacy/production claim. Builder resumes the next minimal legacy parity slice; independent gates will repeat for new scope.
- Fresh rpc-final-codeqa report now PASS for end-to-end bot diagnostics. QA reproduced direct-client cancellation identity loss but separately confirmed the bot recovers canceled/timeout from its contexts; retained as nonblocking library API limitation, not a demonstrated bot failure. Source-blind candidate4 diagnostics FQA running in its own stand.
- Full current Windows `go test -p 1 -parallel 2 ./cmd/... ./internal/... -count=1` PASS after RPC fix (root process34921). Optional real isolated-worker test remains opt-in; candidate4 functional stands supply separate live evidence. No production/commit/push performed.

## 2026-09-26 — accepted private-notes fix and next parity slices

- Fresh source-blind candidate4 private-notes FQA PASS: qa.local/private-notes-final-fqa/REPORT.md. Four rendered EN/RU mouse/emulated-touch zero-memo/document-only scenarios, repeated navigation, owner isolation and retained stale/foreign callbacks. Combined with private-notes-codeqa PASS closes the affected empty-card defect. Hierarchical document UI navigation is outside this legacy memo gate and remains follow-up.
- Effective RU administrator importer routing implementation completed with PG/race/lint/vet evidence; fresh independent Code/FQA started. Full legacy token/time fallback remains outstanding.
- docs/script-api-parity-plan.md now defines current14 tools and complete domain gap inventory. Shared authorized registry + own profile/preferences reads implementation started; all-domain exposure remains objective, not limited to16.
- Cleanup complete: finished capacity stand removed by its owner; root removed memory-final-fqa and visibility-final-fqa containers/networks/6project volumes after labels/stopped-state checks. Root dropped three exact idle orders importer QA DBs, preserved artifacts. SharedPG/active candidate4 stands unchanged.
- candidate4 diagnostics FQA: live timeout/cancellation/retry/all8 memory operations/privacy/rendered UI verified; found [] outcome mismatch and exception vs unavailable conflation. Root fixes pending fresh QA: result array count/no_results, finite worker ErrExecution → script_error/error+execution_failed, arbitrary text remains discarded. New HTTP wire regression cases cover empty/nonempty list and finite execution failure. Existing tests passed; latest small whitespace/operation-name addition check running.
- Removed superseded candidate3 app/script images after docker ps -a ancestor checks returned no consumers. Immutable source snapshot/hash manifests and acceptance evidence preserved; candidate4 active stands unchanged.
- Modern orders effective RU administrator slice accepted: qa.local/orders-ru-codeqa/report.md and qa.local/orders-ru-fqa/report.md both PASS; FQA cleaned owned DBs. Legacy proof fallback builder resumed; coordinated ownership includes orders/service.go and new legacy helpers/tests if needed, preserving source provenance.
- Shared script registry + two owner reads built/frozen. Builder unit/PG/vet/scoped lint PASS. Root diagnostic unit packages and scoped observability/scriptclient lint PASS. Fresh independent registry_diagnostics_codeqa and source-blind registry_diagnostics_fqa assigned.
- candidate5 app build PASS; snapshot579files at qa.local/runtime-candidate5 with SHA256 manifest. Runtime image sha256:da40b650fce807f065f9366522669f14950e04ec44e93738d592d2a452f52671. Reuse candidate4 script image: all19 worker/service/protocol/cmd/module source inputs compared identical, avoiding redundant build/image.
- Parity inventory refreshed from current source (docs/parity-current.md): former missing batch/admin/lineup features now implemented, acceptance separate. Browser massage timetable builder assigned MiniApp initData gateway/page, EN/RU and authorized staff/admin entry. No production change.

## 2026-09-26 — continued parity work and approved cleanup

- Registry/diagnostics fresh Code QA passed (qa.local/registry-diagnostics-codeqa/report.md), with actual focused PG + memory deletion replay evidence; source-blind FQA still running on frozen candidate5.
- Started fresh source-blind full pass lifecycle QA on separate candidate5 stand; profile/pair/payment/takeover/cancel/export/current-event/replay scope remains unaccepted until evidence.
- Tier report parity builder owns allocator report helpers, passbooking status and bot/i18n presentation; timetable builder owns miniapp/gateway/page and its entrypoint. Shared catalog ownership transferred after timetable key addition. Temporary shared-tree missing helper/embed compile barriers reported to owners and resolved before dependent checks.
- Removed stopped private-notes-final-fqa project after acceptance (6containers,2networks,2volumes). Auto-review initially rejected live old memory-fqa removal; user explicitly authorized exact stand+DB removal. Retry succeeded:7containers,2networks,4volumes removed; project resource lists empty. Artifacts retained, current stands/shared services unchanged.

## 26 сентября — legacy proof, pass lifecycle и candidate6

- Legacy actual-proof importer: свежие Code QA и source-blind CLI/PostgreSQL Functional QA PASS. Отчёты: `qa.local/legacy-proof-codeqa/report.md`, `qa.local/legacy-proof-fqa/report.md`. Runtime API continuation — дополнительное свидетельство, не Telegram UI/старые Python callbacks. Следующий этап: tokenless cash/validation-only.
- Candidate5 pass lifecycle: реальные EN/RU UI пары/отмена/очередь/оплата/takeover/экспорт/отзыв прав, восстановление allocation notice после outage/restart прошли. Ограничения и desktop upload P2: `qa.local/pass-lifecycle-final-fqa/report.md`. Общая приёмка не заявлена.
- Desktop upload form исправлена; реальные Edge mouse/touch проверки layout 1440/800/390 PASS. Исправлено сохранение прокрутки расписания при обновлении с немедленным удалением старых приватных данных. Реальный PG+Playwright TestMassageTimetable PASS (6.208 s), ESLint/Prettier PASS; свежий независимый Code QA чистый (`qa.local/timetable-final-codeqa/report.md`).
- Candidate6 заморожен: `sha256:09b4431595887c2dacc5417ae4a98cbc94d385370d65f208fbfffebcb00eae19`, unchanged candidate4-script. Snapshot/hash manifest в `qa.local/runtime-candidate6`; содержит timetable/tier/layout overlay поверх принятого candidate5, без незавершённых 19-tool изменений. Выдан отдельному Functional QA; rebuild запрещён до окончания проверки.
- По явному подтверждению пользователя удалён старый `zns-memory-fqa`: 7 контейнеров, 2 сети, 4 тестовых volume. Отчёты/fixtures сохранены, общие PostgreSQL/identity не затронуты.
- Старый script-fqa2 остановлен; удалены его 12 контейнеров и 2 сети. Report/provider evidence и volumes пока сохранены. Candidate6/новый QA и общий PostgreSQL не затронуты.
- После проверки отсутствия потребителей также удалены 7 старых script-fqa2 образов и 4 неиспользуемых project volume. Никакого global prune; отчёты/fixtures сохранены.

## 26 сентября — candidate7: скриптовые страницы

- Реализованы history.page/orders.page/orders.read, name-only bindings до128 с прежними byte/call/time budgets. Исходный Code QA выявил потерю причины stale: вместо неё оставалось interrupted. Исправлено конечным безопасным `{error:"stale",restart:true}`, durable Outcome.Error=stale и диагностикой error/conflict. PG test проверяет восстановление чтения, сохранность причины в следующем model prompt и отказ чужому владельцу.
- Новые имена включены в конечный observability allowlist; observability tests PASS. Builder focused units/PG/vet PASS, повторный независимый Code QA идёт.
- Candidate7 final app `sha256:8533aa75ba8a0c87bff2961334f14acff934e8be548f33ff9d5d87e29854cb8f`; script `sha256:376ce3acb71f27d9c6eb3cf38ee268f279d4dc157e43589631533eb434431d53`. Отдельный snapshot/hash manifest; source-blind FQA выдана эта неизменяемая пара. Concurrent legacy-cash builder changes не входят в snapshot.

## 26 сентября — независимая приёмка candidate6 и общий gate

- Свежие Code QA (`qa.local/timetable-final-codeqa/report.md`, `qa.local/tier-report-codeqa/report.md`) и Functional QA (`qa.local/timetable-tiers-fqa/REPORT.md`) прошли проверенный scope расписания и подробных тарифов. Actual upload mouse1440/touch390 HTTP200, ACL revoke, cancellation/error/pending content removal, position preservation and real minute refresh. Headless hidden→visible переход не проверен; не считать PASS этой ветки.
- Уточнены исходные Python-контракты в public docs: is_open=true исключается из массажного timetable; assigned-unpaid/waitlist используют balance exclusions; couple candidate появляется лишь при обеих ролях в очереди. Это исторические правила, не новые исключения ради QA.
- Candidate7 script reads/catalog: свежий independent Code QA PASS (`qa.local/script-reads-final-codeqa/report.md`); FQA идёт на неизменяемом образе.
- Общий product pinned lint `./cmd/... ./internal/... ./integration/...`: PASS, 0 issues. Полные cmd/internal и PostgreSQL integration на текущем составе запущены; завершение пока не заявлено.
- Tokenless cash/validation-only builder завершил PG/race/lint/vet/build; два fresh QA начаты. Browser-consent parity передан отдельному builder с согласованием общих файлов до правок.
- Завершённые timetable-tiers и pass-lifecycle стенды удалены после остановки: 12 containers,3 networks,6 synthetic volumes. Удалены неиспользуемые candidate5/candidate6 app и candidate4-script images после проверки отсутствия потребителей; reports/fixtures/snapshots сохранены.

## 26 сентября — candidate8 общий gate

- Полные cmd/internal tests PASS; общий product pinned lint PASS (0 issues).
- Полный реальный PostgreSQL integration PASS,235.021s; 0 fail,548 pass test events. JSONL: `platform/qa.local/current-gates/integration-candidate7-cash.jsonl`. Opt-in skips: LiveSandbox, RegistrationContactBrowser, MassageTimetableBrowser, MarkdownBrowser. Эти skips не считаются PASS; расписание проверено отдельным PG+Playwright и независимым FQA.
- Scope gate: текущий candidate8/cash/script/timetable состав до новых browser-auth изменений; новый builder потребует свои последующие gates.
- Tokenless cash/validation-only independent Code QA PASS (`qa.local/legacy-cash-codeqa/report.md`), Functional QA ещё идёт.
- Candidate8 frozen app `sha256:e020f422b3934be92a07fd3550bf67501702f7a083e4d5f4966f0657bb8786c0`, unchanged candidate7-script. Runtime продолжение передано cash FQA на отдельной импортированной synthetic БД.

## 26 сентября — candidate7 script reads accepted

Оба fresh QA PASS: `qa.local/script-reads-final-codeqa/report.md` и `qa.local/script-reads-fqa/REPORT.md`. Actual UI-backed19/17 discovery,27order/31history pagination, Unicode JSON chunks, foreign refs/cursors, stale same-version hash и version change, retained model error,29,510B history page and omission in next prompt, call/result budgets, EN/RU manualconfirmation.52 provider inputs,21 omitted successful read payloads, max observed input15,284B. No live semantic model or full all-domain claim. Mid-run ACL race/processrestart/historic-event specifics remain separate. Help docs fixed: result_schema/example optional, matching wire descriptors. Stand containers/network removed by owner; evidence retained. Next slice passes/massage reads started under existing reusable registry.

## 26 сентября — tokenless cash/validation-only accepted

Свежий Code QA `qa.local/legacy-cash-codeqa/report.md` и source-blind Functional QA `qa.local/legacy-cash-fqa/report.md` PASS в CLI/PG + supplemental API объёме.9 positive records,8 refusal cases, raw absence/null/false preservation, exact dated/undated helper identities, zero invented proofs, replay/reconcile, actual imported cash accept/reject/current rights/version/attempt and retry. New cash retry rejects stale empty attempt; exact old rejection replay leaves newer state unchanged. TelegramUI/limited-capacity approval/fault injection не заявлены этим прогоном; atomic importer/runtime scopes имеют отдельные предыдущие доказательства. Public runnable example corrected: explicit sandbox runtime env and synthetic identity1101 avoiding seeded101. Далее обязательные старые Python orders| callbacks; builder начинает isolated helpers, shared dispatch координируется с browserauth.

## 26 сентября — следующие parity slices и очистка

- В работе browser consent (включая opt-in exact-origin legacy /auth), old orders callbacks и27-tool pass/massage navigation/details. Root добавил8 конечных observability names; package tests PASS.
- Pass importer mapping выявил реальные отсутствующие исторические actor поля; разрешено explicit unknown с legacy provenance guard, без выдуманных uploader/reviewer.055/runtime/read compatibility и importer в разработке; user embedded event data/preference/reminder dispositions будут перенесены явно, не выброшены.
- Root headed-Edge попытка visibility-return не создала hidden state (30s timeout); limitation остаётся непроверенной, не PASS. Отчёт `qa.local/timetable-visibility/REPORT.md`; процесс/server завершены.
- Удалены завершённый cash API+ownedDB (0 active sessions), script-reads2volumes и model-settings6containers/2networks/1volume. Удалены неиспользуемые candidate4/candidate7/model-settings app images; latestcandidate8/worker и sharedPG/Zitadel сохранены. Reports/fixtures/source snapshots сохранены.

## 26 сентября — новые совместимые переходы

- Source inspection confirmed required pass importer seams: embedded user[event] data, current contact preferences, exact field-presence passport reminder suppression, unknown historical payment actors and unavailable receipts.055/nullable legacy compatibility and full importer in progress; current normal actor/ACL constraints must remain strict. Permanent pass provenance must not duplicate unrelated user memo bodies.
- Browser consent focused PG cases and actual Edge UI smoke passed: EN login, session reuse, RU decline/retry/cancel, old /auth longpoll/check, specialist timetable; race/lint/freshQA remain. Sandbox optional username fixture fixes repeated login testability while real metadata semantics stay unchanged.
-27-tool navigation/detail focused PG regression passed before final lint cleanup; domain data is paginated and full details chunked with explicit caps/stale recovery. Final image/freshQA still required.
- Legacy orders callback adapter focused namespace/ACL/version/replay tests and EN/RU close/delete flows progressing; preserves original cash/proof distinction and current-principal export authorization. Shared edits coordinated; temporary compile windows were corrected by owners.

- Повторная проверка Docker: контейнеры, networks и volumes с именем zns-memory-fqa отсутствуют; разрешённая очистка завершена, повторное удаление не требуется.
- 27-tool slice заморожен после целевых PG/vet проверок; candidate9 собирается отдельно от browser/legacy callback/pass-import изменений. Начат независимый Code QA. Свежий source-blind Functional QA назначен и готовит свой стенд; запуск ожидает frozen image digest.

- Candidate9: свежий Code QA PASS (qa.local/domain-reads-codeqa/report.md); source/hash scope проверен, PG domain/script regressions прошли также из snapshot. Frozen app ec7ffc527fcd269d7d7167f7efcaf287e89aaf3044081351c05a925d4b811d52 передан source-blind FQA, отдельный стенд :18217/:18218.
- Candidate10 legacy orders callbacks: isolated product overlay из accepted8 собран в Docker; source Code QA начат. Integration test manifest дополняется зависимостями fixtures, пропущенными в baseline snapshot. Product build успешен; это ещё не acceptance.

- Candidate10: fresh Code QA PASS, focused actualPG tests/vet PASS; isolated Golden/Nebius lint PASS0issues после форматирования двух startup строк. Новый frozen image zns-runtime-candidate10-format:frozen (8360c113199d40ac791b7b8ea12073d7193e31d4f59c750cc5607babbaf2f334) передан FQA до начала сценариев. Browser candidate11 собирается отдельно.

- Source inventory следующего food этапа: docs/legacy-food-parity.md фиксирует 11 активных callback suffixes, GET/POST /menu, отдельные оплаты питания/активностей в одной записи, legacy export/reminder semantics. /food_get_orders отключён в регистрации Python и не объявляется активным endpoint только из-за наличия handler class.
- После переключения FQA на candidate10-format и проверки отсутствия consumers удалён старый image zns-runtime-candidate10:frozen; активный стенд и snapshot/report сохранены.

- Candidate9 принят в documented public/owner domain-read scope: Code QA qa.local/domain-reads-codeqa/report.md и source-blind Functional QA qa.local/domain-reads-fqa/report.md PASS, isolated tests/vet/build/Golden-Nebius lint PASS. FQA отдельно проверил global-admin без paymentgrant; текущие privileges не добавляют отсутствующие adminbindings. Ограничения: deterministic provider, emulatedtouch, нет timed intra-run ACLrace/exacttransportreplay. Владелец восстановил права и удалил свой стенд; frozenimages/evidence сохранены.
- Builder migration_orders начинает legacyfood полноценный slice; ownership newfood files и migration056, sharedhooks согласуются с pass055builder. Callback11/menu/two independentpayments не подменяются modernordersalias.

- Legacyfood mapping подтвердил отдельные meal/activity states, delete meals без удаленияactivities, cacao capacity38 поselected, обегруппыreminder markers и дваCSV. Одобрена056 domain schema с reuse immutable owner-bound proofbytes и собственным foodACL; canonicalorderspayment не переиспользуется длядвух оплат. Builder согласуетCLI/user deferral сpasses owner.

- Candidate11 frozen: browserauth only поверхaccepted8, 611files/37changes, image687c16ccb6417c618a2b574147926821a632f16ddfd99c087fc799c2cc1f9b5f. Compile-all, focusedPG, vet, catalogs иGolden/Nebiuslint PASS. Ненужныеdupl suppressions вlocale maps удалены, одинаковаяRUdecline label выделена вconstant; свежий Code QA назначен.
- Общий текущий workspace cmd/internal unit gate PASS (root23677): browserauth, script27, legacycallbacks, pass055compatibility совместно компилируются. Это checkpoint, не замена stageFQA и не fullmigrationacceptance.

- Совместный current-worktree PG checkpoint PASS10.924s: TestBrowser/LegacyBrowser/LegacyPass/ScriptDomain/DomainDetail/DomainPage/LegacyOrder. Проверяет сборку и соседство новых browser/script/pass/callback изменений; независимыеFQA продолжаются.
- Pass importer source captured вcandidate12 отдельноотfood; CLI ownership переданfoodbuilder. Скрытое завершениеfooddeferrals запрещено: notified_food_first/last остаютсяявнойнеперенесённойработой доfoodstage.

- Послеaccepted9 и проверкиотсутствияconsumers удалён superseded app image zns-runtime-candidate8:frozen (e020f422…); исходныйsnapshot8 ивсеотчётысохранены. Активныеcandidate10-format/candidate11 иобщийworker7неизменены.

- Legacyorderscallbacks accepted scoped: qa.local/legacy-callbacks-codeqa/report.md +qa.local/legacy-callbacks-fqa/REPORT.md PASS; candidate10-format lint/PG/vet/build clean. FQA проверил10routefamilies, sourcetokens/namespace/event/currentACL includingrevoked savedretry, concurrentaccept одинeffect, sameupdate/restart/default-eventbinding. XLSXdelivery/access only, notcontentrerun; realTelegramcrashwindow notclaimed. Owner savedDBdump/logs иудалилstand.
- Candidate12 frozen e12ccefbf1e5fc94b718264e70e550e9cb86612b84bc2ee9c70d7fa7307e845f: separate runtime/tool snapshots; full isolated migratorPG/vet/lint/build andruntime focusedPG/vet/lint/build PASS. FreshsourceCodeQA иsource-blindCLI/runtimeFQA назначены; ещёнепринят.

- Passcandidate12 CodeQA changesrequired: realPG probes доказали привязкуhistoricalmetadata/backfills кновомуassignment иподменуreceiver второгоучастникаsharedreceipt первым. qa.local/passes-import-codeqa/report.md; полныесуществующиетестыэтинюансыпропускали. Builderвозобновлён, candidate12frozenнеизменяется.
- Source-blindpassFQA отдельнонашёлvalidsecond-only notified_deadline_close2 blocked pass_first_reminder_unresolved. Contract требуетнезависимыемаркеры, безinventedfirstdate. Findingпереданbuilder, первоначальныйfixtureсохраняется; другиеcasesпродолжаютсянаотдельныхfixtures. Passstageнепринят, послеfixfreshобаQA.

- Passfix design согласован: одинsharedattempt сper-participantreceiving/reviewingprovenance; unanimousactor наattempt, иначеlegacy-guardedNULL; runtimeexport/queue/takeover читаютфактыучастника. Historicalreceipt внеcurrentassignment metadata/backfill; raw/archiveсохранены. Independentdeadlinefirst_at nullable, firstnoticeupsertнеперезаписываетsecondmarker. Fixещёвработе, acceptanceнеподнят.
- PassFQA обнаружилstandlimitation arbitraryTelegramIDs: fixedsandboxadapter101/202/303. GUI будетпроверятьсячерезотдельныйend-to-endimport этихIDsвновуюdisposableDB сalice/bob/visitor ownerresolutions; исходный12-userCLIproofостаётсяотдельным, importedIDsнепереписываютсяпослеapply.

- Повторно проверена разрешённая очистка zns-memory-fqa: контейнеров, томов (включая тестовую БД) и сетей с этим именем нет. Другие стенды не затронуты.
- Pass candidate12 Functional QA завершён FAIL: независимый second-only marker блокер; CLI/PG и EN/RU mouse/touch подтверждают остальные проверенные сценарии. Отчёт qa.local/passes-import-fqa/report.md. Исправления прошли целевые PG-тесты и vet, линтеры и candidate13 ещё в работе.
- Browser candidate11 FQA подтвердил HTTPS origin isolation, Secure/HttpOnly/SameSite=None cookie, EN/RU editor save. Отзыв identity через изменение core.users.telegram_id неприменим к fixed sandbox adapter; production identity revoke этим тестом не доказан.


- Privileged tool research завершён: qa.local/privileged-tool-plan.md. В реализацию переданы payment queue/history, practitioner own schedule/preferences/bookings и scoped events discovery. Текущие права проверяются для discovery/help/call и каждой страницы; общие роли не заменяют event permissions. Это новый этап, не принятый all-domain parity.
- Финальный FQA12 отчёт прочитан: кроме подтверждённого second-only дефекта остаётся широкий непроверенный importer scope. Новая Functional QA13 должна проверять полный контракт, а не только три исправления.


- Browser FQA11 FAIL: signed Mini App iframe не загружал browser-auth.js (405). Root добавил regression path к существующему proxy test, воспроизвёл405, исправил одну route entry. Полный sandbox package tests+vet и pinned lint0 PASS. Candidate15 отдельно заморожен из11 с двумя изменёнными Go-файлами и четырьмя отсутствовавшими syntheticASR fixture WAV; image b673a8c1a98d427518a0aa5e821039f9094435cba554708adaba27be689b2b2a. Свежий CodeQA назначен; свежий FunctionalQA ждёт свободный agent slot.11 unchanged.
- Pass candidate13 frozen f061b674855bc04e8d7987fe0b79dc36626874038fdc90ecb1b55d30b31b671f; isolated importer+runtime PG/vet/build/lint0/Linux race PASS. Fresh passes13_codeqa и source-blind passes13_fqa назначены на полный контракт. Не принято до результатов.
- Legacy food parallel ownership: migration_orders — domain/API/importer/menu/schema056; food_bot — новые bot callbacks/media/export/locales и минимальные hooks. Общие seams согласуются напрямую.


- Старые завершённые zns-browser-consent-fqa и zns-passes-import-fqa удалены compose down -v после сохранения synthetic DB dumps; browser-final.sql189036bytes и триpassdump176778/235346/251454bytes. Candidate13 новыйstand/общийPG/Zitadel не затронуты. Images/snapshots пока сохранены.
- Fresh CodeQA15 FAIL: foreign browser binding cancel falsely200 приunchangedDB; trailing body bypass1KiB. Root новые PG regression tests подтвердили обеошибки (и несколькоJSONvalues); decoderтеперьтребуетEOF, cancelпроверяетRowsAffected. Candidate16 isolated Browser/Legacy PG suite2.475s+vetPASS; build/lint ongoing. Globalworkingtree tests временноне компилировались из-за ongoingfoodhooks, snapshotисключаетих.
- Fresh CodeQA13 FAIL: food deferral prematurelycompleted; sourceassignment_tier_number blocks; staleacceptancetimestamp splits sharedproof. Все3 доказаныrealPGoverlay, builderвозобновлён для17. FQA13продолжаетсяsourceblind безподсказок.


- Candidate16 final full Golden/Nebius lint0 и Dockerbuild PASS, image d57c8467744debf9f9bbfeef8b51779c302e9b08811d51b6dd4a372f782c0ed5. Fresh browser16_fqa запущен. Fresh browser16_codeqa spawn отклонён по concurrency limit, остаётся следующим обязательным gate; старый reviewer не переиспользован как fresh.


- Источник Python assistant.py204–217/317 подтвердил дополнительный knowledge gap: static rag_data.yaml загружается и включается вкаждыйконтекст наряду сDriveabout. ВGo loader/converter не найден; archive resource не равен runtime use. Новый docs/assistant-source-parity.md фиксирует обаисточника, last-good refresh, provenance, boundedread/privacy/CPU-only иобаQA. docs/parity-current.md уточнён; этотscopeещёне реализован.


- После проверки отсутствия контейнеров-потребителей удалены superseded QA images11/12/15; их source snapshots, hash manifests, reports и DB dumps сохранены. Активные13/16 и готовящиеся14/17 остаются.


- Candidate14 privileged reads frozen c852bae02041d5955482af77b9cd601c7416c388f98798b7da45c6abdd530bfa, isolated PG16.944s/package/vet/build/lintPASS. Fresh privileged14_codeqa и source-blind privileged14_fqa назначены. Baseline13 остаётся непринятым; inheritedpassfixes17 должны учитываться дообщейприёмки.
- Fresh browser16_codeqa запущен после освобождения stale pending слотов; обаQA16 теперьидут. Старые pendingmodelsettings/usersreviews закрытыбезновойприёмки.
- Assistant source implementation назначена domain_reads_codeqa (теперьbuilder, неQA): новые source-ownedtables/readintegration/config, oauth2dependency,057. Root startuphooks только CoreAPI/app; restrictedbot не получаетCoreSQL. Другиевладельцы уведомлены.


- Event parity builder events_codeqa возобновлён как разработчик (неQA), владеет058/eventconfig/announcementoutbox и согласованными importer/runtimehooks. Требуется source sent_to_hype_thread preservation: импорт не должен заново объявлять исторические регистрации. Никакие прежниеQAвердикты не переносятся на новыйscope.
- Удалены только неиспользуемые go-cache/lint-cache candidate11: 508873765bytes, 16233files. Перед recursive delete проверены абсолютные пути внутри candidate11; snapshot и всеQAартефактысохранены.


- Browser16 fresh CodeQA PASS: qa.local/browser16-codeqa/report.md,616hashes/affectedpackage tests/independentPGquota+sender+logout probes. FunctionalQA ещёидёт, stageнепринят.
- Privileged14 fresh CodeQA FAIL: accepted200byte event/partyIDs сescaping порождают2338bytecursor, continuation отвергает его по2048limit. qa.local/privileged14-codeqa/report.md; builderготовит18, FQA14independentcontinues.
- Root подключил startAssistantSources кrunApp/runAPI lifecycle сerrorreturn иdeferstop; cmd/zns compilePASS. RestrictedrunBot не получаетCoreSQL.
- sent_to_hype_thread source mapping явно остаётся eventstage058 после17freeze. Исправленныйpass17 не доказывает поддержку этого поля или полныйsourceparity. Eventbuilder владеет согласованным announcement-onlyimportseam; historicalannouncement suppression обязательна.


- Root исправил run discovery consistency: текущий реестр пересекается с initialcallablebindings VM; revoke immediatelydenies/listhelp, grant newnames nextscript. НастоящийSobek/PGtest сначала воспроизвёл listedtrue/boundfalse, затем PASS сrevokedcachedcall безсозданиязаказа. Новые script_scope.go/script_tools.go/script_registry.go +test, безworker/protocolchanges.
- Candidate19 объединяет18cursorfix иrun scope поверх14; combinedScript/Privileged PG28.244s, vet, fullpinnedlint0, Dockerbuild PASS. Frozen image8e5d03cc0ba9166c06069a61aaf45d637510cd7efecce9de54cdfbb33563c577; hashmanifest+acceptance+verification saved. FreshQA19 ещёpendingagentcapacity, inheritedpass13 не принят.
- Pass17 frozen a4244460287062fea9e4968e5d7f47c7c3d3c371b992840541766eb95080c767; обаfreshQA назначены. CodeQA исследует allowlisted notified_no_more_passes безexplicitdisposition; finalverdictещёожидается.
- Browser16 FQA подтвердилJSON503 наограниченномDBpermissionfault/recovery, но нашёл default405text/plain безno-store вопрекипубличномуHTTPcontract. Frozen16неизменён, remainingFQAпродолжается; routeerrorfix планируется20.


### 2026-09-26 — Global architecture review and fresh QA dispatch

- Global architecture report completed: qa.local/architecture-review/REPORT.md. Recommends modular monolith/application scenarios, ownership and runtime boundaries; comparison of three alternatives. No product refactoring or acceptance implied.
- Candidate13 Functional QA PASS in its documented broad scope; earlier independent Code QA findings mean candidate13 is not accepted overall.
- Candidate19 independent Code QA PASS (qa.local/privileged19-codeqa/report.md); fresh source-blind Functional QA dispatched to isolated own stand. Full privileged-read and per-run discovery stage remains pending FQA.
- Candidate20 browser routing JSON/no-store correction: regression first reproduced, focused unit/real-PG tests and vet PASS, full pinned lint 0 issues, Docker build PASS. Image sha256:f200500b62a3a478e28c74910c7743080051138ad27074d1d904dead1ea24c29; frozen source manifest and verification under qa.local/runtime-candidate20. Fresh independent Code QA and Functional QA dispatched.
- Source21 final cache/rendering checks still underway before independent QA; food22 and event23 builders continue. No production, real Telegram, commit or push.

### 2026-09-26 — Architecture placement and additional estimate

Added user-requested architecture schedule/budget to docs/readiness-estimate.md and PROGRESS.md. Recommend ownership planning now, one complete application-boundary extraction after affected functional acceptance and before final combined QA, then broader domain/runtime work after stabilization. Additional 20–35 engineering days; 40–80 until migration plus initial structural acceptance, 50–95 for entire proposed program. Estimates are low-confidence planning equivalents, not AI calendar commitments. No product refactor started or production action authorized.

Completed authorized scoped cleanup of zns-browser16-fqa and zns-privileged14-fqa containers/networks/volumes. Full SQL dumps (271482 and 571848 bytes), reports and frozen source evidence preserved. Active new QA stands and shared PostgreSQL/Zitadel untouched.

### 2026-09-26 — Architecture made mandatory

User explicitly added the full architectural refactoring to the main goal. Added durable docs/architecture-review.md and staged docs/architecture-refactor-plan.md with completion evidence. Updated PROGRESS, readiness estimate, migration plan and both handoffs. Remaining main-goal budget now50–95 engineering days (planning estimate); no product structural changes or production actions performed. Goal remains active.


### 2026-09-26 — Broadcast template language decision

User permits Go-native templates. Selected standard text/template and html/template rather than a Tornado compatibility layer. This is an explicitly accepted syntax migration; field-preserving import, recipient selection and personalized content still require complete implementation and QA.


### 2026-09-26 — Focused fixes and obsolete image cleanup

Browser24 adds noncanonical auth-path rejection before Service/Gateway/app mux redirects; new three-layer regression and vet pass. Source25 combines frozen21 with token cancellation transport binding; focused source/PG tests and vet pass. Both Docker builds pass; lint/race and fresh affected reviews pending. Frozen20/21 active stands unchanged.

Removed obsolete candidate14/16/18 Docker images after verifying no containers reference them. Preserved reports, SQL dumps and frozen source manifests; active19/20/21 and script helper remain.


### 2026-09-26 — Privileged reads accepted

Candidate19 both independent QA passed scoped requirements. Code report qa.local/privileged19-codeqa/report.md; functional report qa.local/privileged19-fqa/report.md. Real Sobek in-VM grant/revoke, own-only six reads, complete pagination/current/historical metadata, max escaped IDs, restart/429, provider omission and EN mouse/RU emulated-touch/manual confirmation proven. Full importer, all-domain mutations and real model remain separate. FQA owner removed own stand after evidence retention.

Architecture stage A ownership mapping delegated read-only, output docs/architecture-ownership.md. No structural product extraction yet.


### 2026-09-26 — Final correction gates dispatched

Candidate24 full lint0 and independent CodeQA PASS (619 hashes; independent384-case routing matrix). Fresh FunctionalQA dispatched. Candidate25 full lint0 and Linuxrace PASS; both fresh reviews dispatched on separate synthetic stand18501–18503/55501. Its initial PostgreSQL init-health race resolved by TCP healthcheck; no product change. Candidate23 frozen event/pass stage both independent reviews dispatched. Candidate21 stand removed after complete593622-byte DBdump and owner release; all evidence retained,25 stand untouched.


### 2026-09-26 — Remaining domain and workspace hygiene

Started complete massage importer contract/implementation including external party config and persisted draft continuity; schema060 reserved, shared user deferral hooks coordinated. Current source pricing verified against checked-out Python; no runtime price change made. Added cache/worktree ignore paths after verifying >20k untracked entries were generated caches; files not deleted. Updated restart handoff to current active scope/authorization/candidates instead of historical STOP snapshot.


### 2026-09-26 — Architectural review snapshot and parity inventory

Created immutable planning-only source snapshot qa.local/architecture-stagea with ownership map/hash manifest for independent stage-A review (not a runnable product acceptance candidate). Reviewer dispatch awaits concurrency slot. Updated parity-current inventory to distinguish implemented food/event/source/broadcast work from still-pending independent acceptance; recorded approved Go-native template syntax change. Massage importer/runtime adapter owners assigned separate paths and authoritative-execution constraints.


## 2026-09-26 — browser24 acceptance and current contracts

- Candidate24 accepted for the exercised synthetic browser-auth scope: Code QA and rendered Functional QA PASS; reports qa.local/browser24-codeqa/report.md and qa.local/browser24-fqa/REPORT.md. Stand owner cleaned up its stand; evidence retained.
- Architecture stage A review found an incorrect shared sticker-cache ownership/privacy boundary. Corrected docs/architecture-ownership.md and froze v2 separately; fresh affected Code QA pending.
- Source25 broad Functional QA passed; actual elapsed-hour refresh remains under observation on its unchanged stand.
- User accepted basic broadcast filters plus agent-side filtering over authorized data; unrestricted Mongo selectors are not required. Go standard templates accepted. Tool visibility, scoped reads, live permissions and human send confirmation remain mandatory.
- Production runtime sandbox-only guard remains a real implementation task, separate from permission to deploy. Read-only audit started.


## 2026-09-26 — architecture A accepted; obsolete pass stands cleaned

- Stage A ownership/contracts accepted: fresh independent v3 Code QA PASS against770 frozen source hashes. Product architecture stages B–E remain unimplemented/unaccepted; doc review makes no runtime claim.
- Retired zns-passes13-fqa and zns-passes17-fqa after final complete cluster dumps (1985142 and1040059 bytes respectively). Reports, fixtures and snapshots retained; active event23/source25 and shared PG/Zitadel untouched.


## 2026-09-26 — production identity continuation

- User requires automatic creation/linking on first trusted Telegram contact; mandatory browser login is not the chosen onboarding flow. Shared authorizer identity must prevent duplicate accounts; synthetic email alone does not authorize linking.
- Added Telegram getMe namespace verification with sanitized failure errors; focused telegram tests/vet PASS. Production configuration/startup wiring is being built separately and is not yet independently accepted.
- Candidate26 frozen: complete native tests/vet/build, affected Linux race, pinned lint0, Docker build PASS. Imported free payment source timestamps stay nullable. Fresh affected independent QA pending.


## 2026-09-26 — progress cleanup

- At the user's request, PROGRESS.md now contains only the compact current status, remaining acceptance and accepted scope; duplicated/stale runs removed.
- Deleted docs/restart-handoff.md and docs/handoff-context.md. Active onboarding/audience decisions are retained in docs/go-migration.md; architecture references point to the maintained identity contract. QA reports and evidence remain intact.
- Candidate26 fresh Code QA PASS:630 source hashes, focused real-PG checks and independent nullable JSON/export/ACL probes. Functional QA remains required.


## 2026-09-26 — acceptance and stand retirement

- Candidate26 affected free-payment scope passed fresh Code QA and Functional QA; full importer acceptance remains separate.
- Candidate22 Code QA found event dependency drift, unresolved agent food receipt selection, and missing RU editor locale. Fixes assigned by file ownership; Functional QA continues independently.
- Candidate29 bounded production policy passed fresh Code QA. Functional QA dispatched; complete race rerun remains pending after a timing failure and successful unchanged focused recheck.
- Retired event23-fqa after completed review. Verified final database backup (1,256,599 bytes); removed only its containers/network/volume. Reports and backup remain in qa.local/event23-fqa.


- Source25 natural approximately-hour refresh observed under unchanged controls/image/process: counters1→2, same revision/body/digests. Exact wall delta3592.807s is not an exact monotonic timing claim. Extra token-delay/raw-byte fixture coverage continues separately.
- Massage28 frozen after all builder gates; fresh Code QA dispatched. Functional QA awaits a free slot.
- Automatic onboarding startup now wires the trusted-ingress callbacks and signed Core API wrapper in app and split modes. Root go build ./cmd/zns passed; focused security/functional tests and shared authorizer integration remain required.


## 2026-09-26 — source25 synthetic acceptance

- Fresh Code QA and Functional QA source25 scope complete. Additional isolated helper-only checks passed: invalid Drive UTF-8 retains last-good data, token timeout30.402s, cancellation2.218s/startup2.383s, credential/content redaction. Natural hourly observation remains separately documented with wall-clock caveat.
- Both source25-owned stands backed up and removed; original frozen images/evidence/shared services retained. Evidence: qa.local/source25-fqa/REPORT.md, gap-results.json, cleanup-results.json and backup/manifest.json.


### Integration follow-up — 2026-09-26

- Startup route assembly extracted into appMux without changing routes. cmd/zns and deploy tests passed; pinned golangci-lint 2.14.0 reported zero issues for cmd/zns.
- Broadcast27 Functional QA exercised scenarios passed, but the full gate remains unaccepted because coverage is incomplete; independent QA continues stand expansion.
- Completed event26 synthetic stand removed after dump hash/completion verification. Backup and evidence retained under qa.local/event26-extended-fqa; shared services unchanged.


- Deployment bootstrap: exact init-roles.sh, isolated PostgreSQL17.11, candidate29 migrations and replay passed as nonsuperuser migrator. Runtime DML/default sequence access passed; DDL denied by privileges, no seeded users. Stand removed; qa.local/deploy-bootstrap/report.md retains scope and limitations.
- Diagnostics32 fresh Code QA passed; builder fullgates and independent Functional QA remain separate. Integrated33 snapshot frozen before authorizer-link development.


- Candidate31 all builder gates and fresh Code QA passed; independent Functional QA started against immutable image47f3dbf029ab.
- Candidate33 Code QA failed on permanent onboarding denial poisoning global inbox and loopback issuer mismatch; identity owner implementing scoped fixes, frozen33 unchanged.
- Broadcast27 FQA discovered frozen informal-name race. Root reproduced on separate27 copy, fixed live two-file scope, PG suite and focused regressions/vet passed; qa.local/broadcast-snapshot-fix/report.md. No corrected acceptance yet.
- Removed unused Docker source21 production/synthetic/test and retired memory-fqa app/evaluator images; current stands/toolchains and evidence retained.


- Candidate30 builder gates complete and fresh independent Code/Functional QA started. CodeQA independently reproduced event receipt resolution/admin hash drift accepted before first food apply; correction assigned without changing frozen30.
- Final identity CLI CodeQA verified184source hashes,11ownedpaths,binary and singular lint exception. Functional operatorQA uses own TLS provider and database; fullidentity acceptance remains separate.
- Candidate34 independent CodeQA clean,747hashes/two-path delta verified; race/image/Functional QA pending.
- Old massage28 synthetic stand backed up (1,210,606bytes, no role password hashes) and removed; evidence retained. Shared services and31QA unchanged.


## 2026-09-26 — completed independent scope gates

- Massage31 Functional QA passed the exercised import/continuation/reminder scope, joining builder gates and independent Code QA. Synthetic Telegram and scripted model remain limits; real identity supplement used the hash-verified native artifact. Evidence: qa.local/massage31-fqa/report.md. Owned stand backed up and removed.
- Diagnostics32 Functional QA passed startup diagnostics and EN/RU mouse/touch UI preservation, joining builder gates and independent Code QA. Full production operation and arbitrary wrapped errors are outside this black-box proof. Evidence: qa.local/diagnostics32-fqa/report.md. Owned stand removed.
- Identity importer operator CLI Functional QA passed32 recorded CLI cases and abrupt-interruption recovery, joining builder gates and independent Code QA. Synthetic TLS provider and Linux file permissions are the tested boundary; integrated onboarding/authorizer acceptance remains separate. Evidence: qa.local/identity-import-fqa/report.md.
- Food30 Functional QA remains incomplete. Both independently confirmed Code QA defects have seven-file corrections with focused verification; candidate36 and fresh acceptance remain pending. Evidence: qa.local/food30-fqa/report.md and qa.local/food36-import-fix/report.md.
- PROGRESS now keeps pending actions in Current and moves completed scoped gates to Accepted. Restart handoff documents are removed; build/integration ownership stays with the main agent.

- Broadcast34 image frozen at sha256:4443da5a276bb8ac9fbecfb2d6ae235f04256bbebf763f9e97f751d8de151548. Domain race1.025s and actual-PG broadcast race22.459s passed; all747 source hashes unchanged after build. Fresh Code QA passed; fresh source-blind Functional QA assigned its own stand. Evidence: qa.local/runtime-candidate34/FROZEN.json and build-report.md.

- Candidate35 fresh Code QA rejected transient SQL binding errors classified as terminal identity conflicts (persisted input consumed), plus paired-authorizer example endpoint mismatch. Frozen35 preserved; fixes assigned to identity owner for37 and authorizer successor artifact. Evidence: qa.local/identity35-codeqa/report.md.
- Candidate36 fresh seven-file Code QA clean; builder gates and full Functional QA remain required. Evidence: qa.local/food36-codeqa/report.md.
- User expanded quota requirement: replace question-count limits with cost/credit accounting covering agent and other paid work. Support unlimited users including superadmin while retaining usage tracking. Unit, prices and allocation policy under design; no arbitrary historical-cost conversion or acceptance claimed.

- Broadcast34 affected snapshot gate passed fresh Functional QA across EN/RU mouse/touch, both profile-concurrency timings, actual delivery, replay/restart, ACL and failure/presence semantics. Separate confirmed P2: results endpoint omits content while review/delivery retain it; remains open. Evidence: qa.local/broadcast34-fqa/report.md and results-contract-gap.json.
- Root corrected Fake Telegram menu launcher/proxy so actual legacy /menu buttons can reach runtime. Owner/signature/query/proxy regressions pass, pinned scoped lint0; successor37 carries the adapter for independent acceptance. Evidence: qa.local/fake-food-launch/report.md. Frozen34/35/36 unchanged.
- Routing candidate2 isolated build and affected actual-PG suite passed34.070s; fresh independent CodeQA clean. Full provider/apply/reconcile/removal and Functional QA remain pending. Evidence: qa.local/orders-routing-codeqa/REPORT.md.
- Credit unit clarified as monetary OpenAI API balance; ordinary initial policy1credit/month without carryover, configurable. Codex onlytest estimates explicitly allowed. Separate isolated implementation starts with capture/accounting; no credit enforcement accepted yet.

## 2026-09-27 — disposable synthetic cleanup

User requested removing reproducible synthetic backup data. Removed34 SQL/custom database dumps and2 archived stand copies; kept reports, screenshots, fixture sources and metadata. Historical backup references above no longer imply retained payloads. Exact paths: qa.local/cleanup-20260927/deleted-synthetic-backups.json.

Removed retired broadcast27-fqa, food30-fqa, zns-p29-fqa and food22-fqa projects with their volumes/networks,8 exited p29 probes and10 obsolete app images. Current34 triage stand/shared services/toolchains remain; no global Docker cache pruning. Cleanup verification recorded in qa.local/cleanup-20260927/README.md.

- Results39 isolated correction passed actual-PG adminmessage/broadcast tests16.600s, scoped pinned lint0, vet, immutableimage764e6a81ffc47dd5c9d08962c193070e2b01986424d3661921e80b675361f249 and freshCodeQA. Fresh sourceblindFunctionalQA started. Source748files, exact2filedelta vs34. Evidence: qa.local/runtime-candidate39/build-report.md and qa.local/broadcast39-codeqa/report.md.
- Food36 allbuildergates+CodeQA passed; fullfreshFunctionalQA active. Separate hashverified native37 Fake transport supplies menu launcher while product36 remainsunchanged. Publiclegacy callback table and docs/food-agent-contract.md supplied to sourceblindreviewer.
- Native37 actual-PG fullsuite/vet/build/lint passed; dependency verification/scans stillrunning. Independent37CodeQA identifies pairedclient config permitting unsupported synthetic verifiedemail assertion; successor40 preserves37 and addressesguard+exactformatting. Approvalreview initially mistook37validation forunrelatedcleanup; samecommand permitted aftercurrentexplicitgoalcontext, no bypass.

## 2026-09-27 — results acceptance and accounting continuation

- Results39 fresh Functional QA passed affected saved-content contract: EN/RU mouse/touch, exact text/HTML/personalized/forward results and actual synthetic delivery, pending/failed/completed states, replay/restart, legacy NULL fallback and current authorization. Report: qa.local/broadcast39-fqa/report.md. Limits include cached naming, service-injected terminal failure and API forward attachment; full agent flow remains pending. Owned stand and volume removed without dumps.
- Native37 verification runner completed exit0: full native tests/vet/build/lint, runtime/tools/SQLC module verification, Linux runtime and native SQLC vulnerability scans; 1015 runtime and 1016 deployment hashes unchanged. Exact40 has fresh clean independent Code QA; full Linux race/client/image and source-blind real-provider browser acceptance remain pending.
- Isolated credit shadow source frozen at1026files/25deltas with focused PG/tests/vet/lint passing. Fresh independent Code QA started. Monthly policy, finite reserve/enforcement and reports proceed in a separate copy; frozen accounting source and live runtime are unchanged. No full credit acceptance claimed.
- Root integrated only the ten formatter files from frozen40 after proving every live preimage matched frozen37. All1016 runtime manifest paths now match frozen40 byte-for-byte; frozen candidates unchanged and git diff --check passed. This is source integration, not Functional QA acceptance. Paired sibling authorizer remains unchanged.
- Tool coverage research completed: docs/agent-tool-coverage.md maps public business operations to current typed intents/Sobek/manual boundaries. Food semantic bindings start in an isolated copy. Fresh credit CodeQA found runtime role/schema and broker/model database composition gaps; successor implementation is correcting these before integration.

## 2026-09-27 — model tools and current acceptance work

- Root implemented isolated model-tools (base40): effective read; separate own/others/global get/set; superadmin-only model-setting grant/revoke. Discovery/help filter current permissions; domain transactions recheck ACL and versions; host stores command and replay key. Exact12 deltas/1021 source files frozen in qa.local/model-tools. Final actual-PG Script/Model suite26.677s, bot unit0.182s, pinned scoped lint0, vet and native build passed. Fresh independent CodeQA started; Linux image/race and Functional QA not yet run. Live runtime unchanged by this feature.
- Food owner tools isolated builder gates passed; fresh separate CodeQA started. Reviewer/export operations remain required follow-up; no full tool parity accepted.
- Credits fresh CodeQA found three startup/least-privilege blockers and provider request-ID loss. Successor copy addresses those plus monthly finite policy. Report: qa.local/credits-codeqa/report.md. No credit acceptance or live integration.
- Runtime40 full Linux race failed a test-only comparison of equal instants with different time.Location. Successor41 verifies Time.Equal before retaining full Order equality. Original UTC test reproduced RED; corrected repeated UTC and Amsterdam tests passed. Root integrated only the verified test delta; all1016 live runtime manifest paths match41. Full41 race continues. Product bytes equal40, whose immutable image/client are under independent Functional QA with own real Zitadel.
- Daniel established a decision queue in PROGRESS: defer only work requiring his explicit decision/participation and continue all independent development/tests. Blanket authorization covers exclusively local synthetic Zitadel/debug/trace/test-DB copies/test builds. Small real-model API checks and the designated Telegram pair follow main tests. If only product decisions remain, continue required refactoring, then security/architecture/modularity audits. No commit/push/production permission inferred.
- Fresh model-tools Code QA clean within12-file delta;1021 hashes verified and focused argument test passed. Linux/runtime Functional QA remains pending. Food-tools CodeQA found same-update payment pending ordering and unbound observed menu/price revisions; successor fixes assigned. Current-event wording is resolved by retaining established host-bound event semantics, not treated as a new user decision.
- Daniel explicitly reaffirmed local commits, branches from the current branch and merges back into it. Updated current planning authorization; push/publication/production still require separate permission. No commit was made by this documentation update.
- Independent real-Zitadel browser QA confirmed bot-first EN and browser-first RU both stop at email verification for the unverified synthetic address after successful external linking. Q1 is recorded in PROGRESS; only complete browser-login acceptance is deferred. Research of supported Telegram-only login continues without changing the frozen QA stand; other identity checks continue.
- Full archive/removal builder rehearsal completed and owned resources were cleaned. Fresh source-blind fullimport_fqa started from public acceptance/startup packet, with independently owned real-provider stand and required rendered UI/removal checks.
- Runtime41 full Linux race passed (integration455.369s), image73d485f02532e7c11427364ff200d839ca220178944c7ea6a6549a155f8cc1f4 built; runtime executable byte-identical40, all1016/1017 source hashes preserved. Identity40 independent FunctionalQA NOT ACCEPTED due actual browser email gate; backend exercised checks/limits and cleanup recorded in qa.local/identity40-fqa/report.md. Separate supported LoginV2 compatibility proof now in progress, not a user-policy change or accepted fix.
- Model tools focused Linux race/images passed; frozen runtime and networkless Sobek helper delivered to fresh FunctionalQA. Full source1021 hashes unchanged. Food tools successor review found additional same-update event/order switch pending-hint edge; next isolated correction assigned. No broader tool parity claimed.
- Frozen36 independent Functional QA completed without confirmed defects in executed local cases: all11 callbacks, import atomicity/concurrency/drift/provenance, EN/RU mouse/emulated-touch, independent payment histories and receipt guards, reminders, capacity race, CSV partial retry/revocation and separate modern XLSX. Code QA and builder gates previously passed. Scope accepted; unavailable external photo delivery and real Telegram/model remain unverified. Evidence qa.local/food36-fqa/REPORT.md; owned resources cleaned without dumps.
- Full archive independent Functional QA did not pass: available imported pass receipt cannot be retrieved. Import/replay/reconcile with real local provider, stable identities, preserved proof bytes, importer-free byte-identical runtime, rendered EN/RU mouse and original food/massage continuation passed within report limits. Touch and remaining matrix are not accepted. Evidence qa.local/fullimport-fqa/REPORT.md; owned resources cleaned.
- Isolated delegated-downloads successor based41 corrects four file clients to use trusted user token exchange and refuses redirects for order proof/export. RED reproduced all four real-auth failures and two redirect cases. Focused native bot/PG/vet/lint passed; Linux and independent reviews pending. No live integration yet.
- Delegated-downloads CodeQA found metadata redirect forwarding beyond the first binary-file patch. Successor delegated-downloads-next centralizes copied Core API HTTP-client redirect refusal for JSON/service/metadata and all binary routes; adds metadata302/307/308 and order-export regressions. Native focused PG14.946s, vet/lint0, Linux race bot1.063s/integration25.085s, frozen runtime imageb44e4b6c595493ffa1e573c2a33eb73318924337029c05ef2b76008250f0d221 passed. Fresh CodeQA clean,1017 hashes/eight-file successor delta verified. First corrected binary image FunctionalQA continues; fresh successor FunctionalQA remains required.
- Food owner tools final successor passed fresh CodeQA, Linux focused race bot2.959s/integration46.103s and both runtime/Sobek builds, all1026 source hashes unchanged. Fresh source-blind foodtools_fqa started from exact immutable images. Reviewer/decision/proof/export tools are being implemented in a separate successor, not counted accepted.
- Model tools no-argument successor passed fresh CodeQA, actual Sobek/host PG tests, Linux worker race1.343s and targeted PG race2.727s. Scriptprotocol filter matched no tests in that Linux invocation; full native protocol tests previously passed. Exact runtime219b4b72b5429d380835ee192526f332999cbd005da8fef53429d4306cbb9e1e and helper60a4d48d6210f4b6cbfa8f503e92e426b2005ce428734e30bbbe2b7129548874 built,1023 hashes unchanged. Fresh FunctionalQA including touch remains required; old modeltools FQA is not full acceptance.
- Cleanup: after current container-reference check, removed13 retired app images (runtime23/24/26/27-frozen/30/31/32/34/36/39/40 and source25 variants), no force/prune. Current QA/provider images, runtime41 and retained runtime27/events23 test toolchains preserved. Evidence/source fixtures remain; no backup made.
- Credits enforcement fresh CodeQA failed with confirmed bounded-pool deadlock in settlement (transaction plus extra pool checkout), leaving dispatched receipt; normal-pool control succeeds. Manual usage also omits configured policy. Both belong to new credits-complete successor together with remaining ASR/remote/operator/cutover work; frozen1048-file source unchanged. Report qa.local/credits-enforcement-codeqa/report.md. No full credit acceptance.
- Fresh modeltools-next_fqa launched from rebuilt exact runtime/helper after clean independent CodeQA; required EN/RU mouse/touch and actual Sobek/provider-selection/replay scope remain open until its evidence is complete.

## 27 сентября — текущие независимые проверки

- Локальные commits, ветки от текущей ветки и merges обратно повторно явно разрешены. Push и production не разрешены.
- Login V2: builder proof на отдельном Zitadel 4.16.3 подтвердил browser-first и bot-first PKCE login с тем же subject, повторный вход и сохранение email.isVerified=false. Q1 снят; независимая приёмка новой конфигурации обязательна. Доказательства: `qa.local/identity-login-v2-proof/report.md`.
- Model settings: Functional QA обнаружил потерю конкурентного изменения. Новый кандидат защищает UPDATE версией и создание отсутствующей строки; три управляемые гонки прошли. Свежий Code QA начат. Кандидат не принят: `qa.local/model-tools-final/`.
- Food: независимые проверки нашли проблемы привязки после durable admission, курсора чтения review и свежести proof. Исправления объединяются в `qa.local/food-tools-complete/`; прежние успешные проверки не означают принятия нового состава.
- Massage agent tools: book/cancel, practitioner instant/configure реализованы в отдельном кандидате; host связывает owner/version/key, время проверяется под блокировкой, отзыв прав проверяется при исполнении. Собек/PG соседние сценарии прошли (integration 38.752s), pinned lint — 0 issues. Независимые QA ещё не выполнены: `qa.local/massage-tools/`.

- Model settings final: свежий Code QA PASS (1025 хешей, PG 9.959s), Linux race PASS (PG 19.558s), runtime/helper собраны без изменения исходников. Свежий source-blind Functional QA начат на отдельном стенде. Отчёт: `qa.local/model-final-codeqa/report.md`; Linux evidence: `qa.local/model-tools-final/evidence/`.
- Massage tools: runtime и Sobek helper собраны. Свежий Code QA выполняется; reviewer PG проверка прошла 3.180s. Linux race запущен после освобождения общего PG.
- Credits-complete: builder заморозил 1062 файла/43 изменения; PG lifecycle и operator paging, vet/lint прошли. Независимые gates и интеграция обязательны; `qa.local/credits-complete/STATUS.md`.

- Massage tools: независимый Code QA PASS (`qa.local/massage-tools-codeqa/report.md`); Linux race integration36.037s, обе сборки и проверка неизменности1020 исходников PASS. Functional QA ещё не начат. Окончательные IDs после сборочного gate записаны в acceptance.md.
- Downloads final FQA: обнаружена ошибка только конфигурации дополнительного split-стенда — bot наследовал scripted provider; передано требуемое remote + отдельный scripted model. Проверки отказа редиректов продолжаются, не считаются выполненными до доказательства.
- Credits-complete передан свежему Code QA. Следующий pass-tools кандидат принадлежит отдельному builder, базируется на исправленном delegated-downloads-next; live исходники пока не меняются.

- Food complete: обе Linux image-сборки прошли; запущен race, свежий независимый Code QA начат.
- Credits complete Code QA FAIL: pre-cutover legacy update применял денежный enforcement; персональный unlimited ошибочно отображался как default. Root воспроизвёл оба дефекта (RED), создал отдельный credits-final, исправил привязку LegacyBudget и источник политики. Полный TestCredits PG PASS12.420s; vet/lint ещё выполняются. Прежний кандидат не изменён.
- Подготовлена проверяемая read-only карта интеграции пяти кандидатов: `qa.local/integration-plan/inspect.ps1`. Все manifest-хеши подтверждены;12 пересекающихся путей и отдельное расхождение credits deploy/compose.yaml с новым runtime требуют осмысленного объединения. Никакой автоматической замены live source не выполнено.

- Delegated downloads next принят в полном заявленном download scope: Code QA PASS и независимый real-Zitadel Functional QA PASS (`qa.local/downloads-final-fqa/report.md`), включая девять redirect маршрутов без sink requests, imported exact bytes, current revoke, EN/RU mouse/touch, обе stale replacement проверки и durable replay. Восемь файлов интегрированы в platform; все1017 source-хешей совпали с принятым кандидатом. Повторный live bot focused test PASS0.170s. Это не полная приёмка импортера.
- Model recovery: неверное denied воспроизведено через реальный Sobek (RED); только stale_model_settings HTTP409 преобразуется в существующий stale/restart outcome, отзыв прав остаётся denied. Соседние model/settings PG tests PASS9.861s; vet/lint выполняются. Кандидат `qa.local/model-tools-recovery/`, live не изменён.
- Food complete Code QA FAIL: begin_payment после архивирования исходного события возвращает успех и назначает получателя. Доказательство `qa.local/food-complete-codeqa/report.md`; требуется узкая доменная проверка активности без изменения исторического review/replay.

- Food guard заморожен:1038 файлов,2 изменения. Проверка активности в assignReceiver покрывает begin_payment обоих видов и prepare_activities. RED подтвердил все три обхода, GREEN всей соседней Food/ScriptFood/LegacyFood интеграции60.236s; vet/lint0. Completed replay проверен отдельно; historical review не менялся. Свежие QA обязательны.
- Credits final: свежий Code QA PASS (`qa.local/credits-final-codeqa/report.md`), реальный PG17.451s, full agent/mediaproc/credits unit suites. Runtime/helper сборки прошли; Linux race запущен.
- Model recovery: vet/lint0, заморожено1025 файлов/2 изменения; свежий Code QA идёт.
- Identity Login V2 Functional QA подтвердил реальные PKCE/nonce/signature, bot/browser-first convergence, email false, EN/RU booking и concurrent linking. Обнаружен новый liveness defect: disabled provider identity удерживает общий inbox, active users не получают ответа. Исправление разрабатывается отдельно в `qa.local/identity-inbox/`, временные provider сбои должны сохранить retry. Общая identity-приёмка не пройдена.

## 27 сентября: принятые загрузки и массаж, следующие gates

- Делегированные загрузки: свежие Code QA и Functional QA прошли; точные байты, текущие права, stale/replay и девять redirect-маршрутов проверены. В основной platform перенесены 8 файлов с проверкой исходных хешей.
- Massage tools: оба независимых QA прошли; booking/cancel, practitioner instant/configure, отзыв прав, абсолютное время, повторы, EN/RU mouse/touch. Интегрированы 13 файлов; focused bot/domain tests прошли. Общий состав зафиксирован в qa.local/integration-plan/live-downloads-massage-sha256.json (1021 файл); общие gates ещё нужны.
- Credits-final и model-tools-recovery: свежий Code QA, Linux PostgreSQL race и неизменяемые сборки прошли. Независимые Functional QA работают на отдельных стендах; интеграция ожидает приёмки.
- Food-tools-guard: свежий Code QA прошёл, включая дополнительные независимые проверки исторического settlement и отзыва прав. Linux race/сборки запущены; Functional QA ещё не выполнен.
- Pass tools: отдельный кандидат прошёл builder PostgreSQL/vet/lint; независимый Code QA начат. Самостоятельной приёмкой это не считается.
- Identity Login V2: независимый QA подтвердил вход без подтверждения email, но обнаружил блокировку durable inbox отключённым пользователем. Отдельный кандидат воспроизвёл реальный ответ Zitadel и исправляет терминальный отказ без потери retry при временных сбоях.
- Удалён завершённый локальный synthetic стенд identity-login-v2-proof: 8 контейнеров, сеть и воспроизводимый database volume. Отчёты и setup harness сохранены; активные QA и общий PostgreSQL не затронуты.

- Linux food guard выявил несовместимое сравнение метаданных time.Location в новом replay-тесте. Оба момента нормализованы к UTC перед полным сравнением заказа; продуктовый код не менялся. Исходный manifest/RED сохранён; повтор Linux gate и независимая проверка тестового изменения ожидаются.
- Удалены 9 устаревших synthetic Docker-образов downloads/model/food после проверки отсутствия ссылок из всех контейнеров. Текущие frozen кандидаты и warm toolchain сохранены.

- Независимый Pass Code QA воспроизвёл отказ приглашения получателя, увиденного только в разрешённой очереди. Тот же ID из текста пользователя принимается. Кандидат не принят; отдельный successor исправляет grounding без расширения прав.

- Локальный коммит 9491c79: .gitignore для локальных кешей/стендов/артефактов и удаление .vscode/launch.json из индекса с сохранением файла на диске. git check-ignore и diff --check прошли. Push не выполнялся.

- Food guard: финальное pointer-aware сравнение UTC прошло независимую компиляцию и Code QA тестового изменения; Linux race PostgreSQL PASS (43.897s). Runtime/helper сборки ещё выполняются.
- Identity inbox: builder подтвердил исправление на локальном Zitadel 4.16.3 и заморозил пятифайловый кандидат; свежий независимый Code QA начат. Acceptance ещё не получен.

- Food guard Linux gate завершён exit0: race + runtime f3b9f1248c0fc63532502256597ddb528608b076f6dd54d14968d3db29466b20 и script helper 1a1235a163d697b17a75918732dfb5750175390f5f6ecc1aa55218a3fafcbe01. Frozen source повторно проверен; Functional QA ожидает свободного независимого reviewer slot.

## 27 сентября: следующие независимые проверки

- Identity inbox: свежий Code QA PASS; 1019 frozen файлов проверены до/после, unit и 10 focused PostgreSQL tests прошли. Проверен официальный Zitadel4.16.3: actor-token и inactive-subject ошибки различаются. Linux race и сборка прошли; независимый реальный local-provider Functional QA начат.
- Food guard: свежий независимый Functional QA получил immutable runtime/helper и полный owner/reviewer/export контракт; отдельный stand, EN/RU mouse/touch.
- Model recovery: независимые semantic/race/replay/revocation и EN/RU mouse проверки прошли без продуктовых findings. Touch не был выполнен тем инструментом; отдельный свежий reviewer получил neutral setup и освобождённый stand. Общая приёмка ожидает touch.
- Pass-tools-next: builder заморозил исправление queue grounding (3-файловый delta); native PG/vet/lint прошли. Новый независимый Code QA и Linux race/сборки начаты, приёмка ещё не получена.
- Документ credits-accounting обновлён: актуальный кандидат отделён от исторических inventory, указаны точные операторские ASR bounds. Это документация контракта, не приёмка.

- Pass-tools-next Linux gate завершён exit0: PostgreSQL race37.980s, runtime ec746605657c651002a30edbc9c2696de75ea32efca9c9dcde0aed9c181391d2, helper417e3a541c4ff6386c00677e2811e0c8f8b149f61e36e61c473a5e71093a0417. Source hashes неизменны. Свежий Code QA продолжается, Functional QA не начат.

## 27 сентября: model settings интегрированы

- Independent model-recovery-codeqa PASS + model-recovery-fqa semantic/mouse PASS + model-touch-fqa supplemental EN/RU emulated-touch PASS относятся к одним immutable runtime/helper. Вместе закрывают заявленный synthetic scope; реальные модель/Telegram и общая архитектура отдельно.
- В основной platform перенесён 18-файловый model delta. 11 файлов перенесены после проверки baseline hash; 7 пересечений вручную объединены с принятыми massage hooks и точными ожиданиями discovery. Downloads сохранены. Manifest общего состава: qa.local/integration-plan/live-downloads-massage-models-sha256.json, 1030 файлов.
- Совместные focused Script/ModelSettings проверки с реальным PostgreSQL прошли (integration26.077s), соответствующие bot/worker tests зелёные. Дополнительные regex для Delegated/imported-download не нашли отдельных test names и не дают нового download evidence; неизменность download-файлов проверена хешами. Vet/lint выполняются; общие замороженные продуктовые gates ещё нужны.
- Освобождённый zns-model-recovery-fqa удалён вместе с контейнерами, сетью и временным IPC volume. Отчёты, screenshots и immutable образы сохранены.
- Pass-tools-next не принят: новый независимый reviewer доказал выполнение queue-only приглашения после отзыва права до POST и при resume. Отдельный pass-tools-authority сохраняет происхождение evidence и проверяет право в транзакции; старые manifests/images не изменяются.

- После model integration совместные vet/pinned lint завершились exit0, 0 issues; исключения не менялись и ничего не скрыли. Полные общие runtime/Functional gates не заменены focused-проверками.
- Credits Functional QA нашёл P2: после cutover неdefault assistant_daily_limit блокирует paid dispatch, но сообщение теряется в редакции ошибок, inbox бесконечно повторяется. Отдельный credits-diagnostics builder сохраняет fail-closed и pending inbox, добавляет статическую диагностику/остановку конфигурационного retry. Frozen credits-final не меняется.
- Начат отдельный broadcast-status candidate на текущем принятом составе1030: review/results и native show, без send/enqueue tool. Root владеет liveplatform/PROGRESS, builder — только изолированным source/report.

- Pass-authority frozen image builds PASS: runtime666db242c37d24e25d679acc33ab1592ee24df0a0fa1bccd81be90a4e3f7ddd0, helper557d7be9c1ae8d86ff858d99af265c70a159fb78df10b19cc05bbcb4b4766dcf; source hashes проверены. Race ожидает свободного PostgreSQL lane; Code QA выполняется.
- Profile-tools builder стартовал отдельный scope Sobek profile history/writes + preferences language на принятом baseline1030, без liveизменений.

## 27 сентября: identity inbox принят и интегрирован

- Независимый identity-inbox-fqa PASS: реальный Zitadel/Login4.16.3, четыре EN/RU mouse/touch сочетания, callback ACK, no disabled effects/history, очередь другого пользователя, повтор terminal update после реактивации, outage/client fault + restart recovery. Fresh onboarding namespace проверил transient provisioning retry и permanent unlinked denial. Bot/browser-first PKCE convergence, opaque-subject nonmerge и emailVerified=false подтверждены.
- Runtime985e384843059aca75c0fb8f79e2949467dee6135864a3f76571c03fc59e38f0 соответствует reviewed candidate; Code QA и Linux gates ранее PASS. Owned stand/volumes/secrets удалены reviewer, sanitized evidence сохранён.
- Пять файлов identity delta перенесены в platform только после проверки всех1030 live hashes, baseline каждого затронутого файла и source hashes. Новый общий manifest qa.local/integration-plan/live-identity-models-massage-downloads-sha256.json содержит1032файла. Общие unit/PG проверки следуют; scoped acceptance не означает full migration completion.

- Identity live integration: полные unit packages identity/bot/i18n прошли (2.233/0.296/0.681s). Совместный PostgreSQL gate ожидает освобождения lane.
- Pass-authority: свежий Code QA PASS, 15top-level integration включая3независимыхprobes, все1027hashesunchanged. Linux racePASS47.958s, ранее зафиксированныеimages unchanged. Независимый Functional QA полногоpassконтракта начат на отдельном immutableстенде.

- Общий live identity/inbox/model/massage PostgreSQL gate PASS30.726s после интеграции identity. Полные unit identity/bot/i18n ранее прошли. Состав1032 без новых исходных изменений.
- Broadcast-status builder передал frozen1032candidate/5delta; nativePG/unit/vet/lintPASS. Runtime/helper images собраны с hashverification; свежий независимый Code QA выполняется. Linux race/FunctionalQA впереди.

## 27 сентября: свежие gates кредитов и рассылок

- Credits-diagnostics: независимый Code QA PASS, 1066 source hashes unchanged, static diagnostic privacy и PostgreSQL startup/intake/cursor/replay/custom legacy limit checks. Root Linux race exit0: selected integration19.790s, config1.043s; credits/agent/bot packages в этом regex не содержали matching tests, их runtime race coverage не заявляется. Frozen source unchanged.
- Credits runtime6123bee0b00a07246697c09973dfe90688278e5b0a4e18bc94ffd7c56470e381 и Sobek0d1e3f2164a56f434adeea8ed12ec77603807639f276520c4ee43fae5d032cce переданы свежему source-blind Functional QA с публичным контрактом. Operator artifact hash повторно проверен. Product не интегрирован.
- Broadcast-status: независимый Code QA PASS, 1032 hashes unchanged, четыре Sobek PG tests и самостоятельная transport-loss/replay проверка; pinned lint0. Linux race запущен отдельно от неизменных product images. Functional gate ещё не пройден.
- Broadcast-status Linux race завершён exit0: matching integration11.397s, source hashes unchanged. Bot/adminmessage packages были no-tests в выбранном regex, отдельная unit-race приёмка не заявляется. Свежий source-blind Functional QA получил immutable runtime/helper и публичный контракт; интеграция ожидает gate.

- Начат изолированный knowledge-tools builder для оставшихся shared fact/proposal Sobek операций. Владение только qa.local/knowledge-tools; live platform не меняется. Modern orders полный Sobek scope остаётся следующим отдельным направлением.
- Для source-blind QA опубликован synthetic Core auth wire contract в docs/product-sandbox.md: public claims/signature/config, authenticated artifact endpoint и безопасное сравнение exact bytes. Реальные токены/ключи в документацию не внесены. Pass QA получил публичный контракт без implementation hints.
- Parity inventory согласован с текущими stage statuses: сняты устаревшие утверждения о незавершённых food36/massage31/identity-local проверках. Полный importer/removal/composition gate остаётся открытым; обновление инвентаря не создаёт новой приёмки.
- Profile-tools frozen1036files/20delta: builder units/vet/pinnedlint0 и actualSobek/PG12.326s PASS. Свежий Code QA запущен; baseline-delta восстановлен из файлов с точным совпадением captured hashes. Root immutable image build выполняется. Acceptance не заявляется; live platform unchanged.
- Profile-tools immutable builds завершились exit0 с повторной проверкой1036hashes: runtime14d6241b19df094d45f294918099b82de2a1b29ebb097d87d887b003120e9600, Sobekc8325e0529ff1c40130221da7d819c1c29ecb3e63bde93d26eeaacb10a11098f. Linux race ожидает shared PG lane; независимый Code QA выполняется.
- Profile Code QA FAIL/P1: private_profile marker терялся при zero admitted calls из-за inner expansion; независимый neutral-canary Sobek/PG probe доказал утечку в conversation_events. Frozen original не изменён. Root создал profile-tools-private из1036проверенныхhashes, воспроизвёл RED и отделил run marker от calls в SQL. Полный focused ScriptProfile PG GREEN5.621s; unit/vet/lint запущены. Не интегрировано.

## 27 сентября: food Sobek принят и интегрирован

- Food guard FQA PASS:76 независимых assertions,24 deadline EN/RU mouse/touch cells, actualSobek, currentrights/admission/proofgeneration/chunkcompletion/exportreplay+overlap. Sourceblind report qa.local/food-guard-fqa/report.md. Frozen runtimef3b9f124.../helper1a1235a1... соответствуют reviewed source. Stand удалён владельцем.
- 39-file delta интегрирована после hashguard всего live1032 и candidate1038. Четыре shared merges сохраняют Model/Massage command fields, их registry/key assignment и общий no-redirect HTTPclient. Новый manifest live-food-identity-models-massage-downloads-sha256.json содержит1054files. Unit bot/legacyfood/api PASS; combinedPG/quality ещё не завершены. Общее product acceptance не заявляется.
- Credits diagnostics FQA PASS affected startup/live configuration faults, durableinbox/replay/defaultpolicy, exact spend, Sobek, EN/RU mouse/touch и logprivacy. Inherited broad accounting paths этим повторно не заявляются. Ownedstand cleaned. Полный credits delta ещё не integrated.
- Profile private successor unit/vet/pinnedlint0 и Linuxrace integration11.674s PASS. Fresh independent CodeQA выполняется; новый imagebuild активен. Originalfailedcandidate untouched.
- Food combined live gate PASS: focused Food/ScriptFood/Model/Massage/discovery/reads/profile integration55.423s, unit/vet/pinnedlint0. Исходные права и новые инструменты проверены совместно; полная финальная composition QA остаётся.
- Broadcast status оба QA PASS; frozen5filedelta интегрирована после проверки1054live/1032candidate hashes. Два sharedmerge добавляют BroadcastReview record и privateprojection, сохраняя food/model/massage. Новый manifest live-broadcast-food-identity-models-massage-downloads-sha256.json1056files. Unit запущены; combinedPG/quality дальше.
- Pass-authority Functional QA PASS, полный scopedreport qa.local/pass-authority-fqa/report.md: права/queueprovenance, owner/admin/payment/batch/export/restart, actualupload и redirectdenial, EN/RU mouse/touch. Ownedstandcleaned; delta23files пока не integrated.
- Profile-private corrective CodeQA PASS: независимая8case marker/archive matrix missing/empty/null/admitted/unmarked, assistant echo и весь ScriptProfilePG12.011s; frozen1036hashes unchanged. Runtime9e88fe669.../helpere2d91d844... собраны, свежий sourceblind fullprofile FunctionalQA запущен.
- Broadcast live bot/adminmessage unit PASS0.174/0.643s. Публичный review/show контракт и tool inventory обновлены после интеграции; общий PG/quality gate ещё требуется. Root shared PG освобождён и передан modern-orders builder (knowledge использует другой lane).

## 27 сентября: pass и credits интеграция

- Pass23delta интегрирована с8явнымиsharedmerges: registry/privateprojections/keybinding/record, счётчикиdiscovery +4, observability сохраняет foodnames. Общийmanifest1066. Bot/passbooking/observabilityunitsPASS; combinedpass/broadcast/food/model/massagePG55.966s, vet/pinnedlint0PASS.
- Credits91delta интегрирована после проверки1066live и1066candidatehashes.12sharedfiles с9разрешённымиconflicts: сохранены identityinactive deferred terminalhandling, food Inputfields, все registry/record domains, +2ordinary credittools, обеlocaleentries, combinedobservability; mediabroker DBnetwork/dependency перенесены. Manifest live-credits-passes-broadcast-food-identity-models-massage-downloads-sha256.json1119files.
- Полные go test ./internal/... ./cmd/... PASS. Combined credit/SobekPG71.338s PASS. Первоначальный regex identity не совпадал с нужными названиями; отдельный actualnamed inactive/provider/onboarding/firstmessage gate PASS6.395s. Full vet/lint выполняется. Production cutover не выполнялся.
- Knowledge candidate frozen1048files/11delta; fresh independent CodeQA выполняется, exactbaseline-delta восстановлен поhashes. Immutable runtime/helper build выполняется; FQA впереди.
- Full vet текущего1119composition PASS. Full lint сначала обнаружил6замечаний только в прежних ignored local probes platform/qa.local/mdprobe иplatform/tools.local/media-probe (fmtstdout, integer range, magic fixturecounts). Исправлены сами probes безисключений/suppressions; реальный providerprobe не запускался. Повторныйfull lint активен, originalfail log сохранён.
- Knowledge independent CodeQA FAIL/P1: script memo list копировал Memos в next-model context без deletionstate; delete очищал ledger, но сохранял text в Knowledge.Memos. Собственный reviewer realPG/Sobek canaryFAIL.1048frozenhashes unchanged; остальныеunit/vet/lint/существующиеPGtestsPASS. Successor knowledge-tools-private/platform исправляется владельцем отдельно; originalнеintegrated.
- Повторный полный pinned lint текущегоcomposition завершён exit0/0issues. Исключения не менялись, существующие warnings skip0. QA probes остаютсяignored (git check-ignore подтверждён). Все1119 manifested productfiles повторно сверены; локальные probe-only изменения за пределами runtime manifest.

## 27 сентября: профиль, границы импорта и очистка

- Profile-private Functional QA PASS в назначенном объёме: реальные Sobek и rendered EN/RU mouse/touch, приватность отклонённых записей с omitted/empty/null calls, current rights, история, stale/lost-response/restart. qa.local/profile-private-fqa/REPORT.md раскрывает случайное чтение generic ValidateScript и начала Remote.Plan при поиске публичных DTO; профильная реализация и тесты не читались. Не заявляем абсолютную source-blind изоляцию. Интеграция ещё впереди; владельцу разрешена очистка только своего стенда.
- Read-only importer audit qa.local/importer-completion-audit/report.md отделил реальное историческое six-domain import/replay/removal evidence от отсутствующей приёмки текущего состава. Четыре домена отсутствовали в fixture; начат inventory активных readers и target mappings. Будущий removal helper: точный DB guard, шесть явных таблиц, RESTRICT, catalog dependency check, content/byte digests, повторный restart после изменений. Старый CASCADE helper не переносим без исправления.
- Удалены 10 неиспользуемых superseded Docker tags (pass-tools, pass-tools-next, food-tools-complete, credits-final, profile-tools runtime/helper) после проверки immutable image IDs всех контейнеров, включая остановленные. Все удаления exit0; перечень qa.local/integration-plan/retired-images-20260927.json. Актуальные frozen images, media helpers, toolchain и чужие проекты сохранены. Root не удалял БД или volumes этим действием.

## 27 сентября: интеграция профиля и новые QA

- Profile private20-file delta интегрирована после проверки1125 итоговых файлов (до интеграции1119). Восемь shared merges сохраняют credits/identity/food/pass/model/massage/broadcast; discovery +3 bookable/+2 read-only. Совместный PG PASS65.091s, unitPASS. Lint выявил превышение сложности в createAllowedPlan/reserveScriptTool после объединения; выделены addProfileContext/bindScriptToolKey без suppressions. Сохранена исходная fallback profileversion и текущая observed version. Повтор PG PASS60.043s, unit/vet/pinnedlint0. Общий независимый composition QA ещё не выполнен. Profile FQA владелец подтвердил очистку своего project/volumes/credentials, frozen images и sanitized evidence сохранены.
- Knowledge private1050files прошёл свежий CodeQA: исходники неизменны, affected PG31.939s, unit/vet/pinnedlint0. Linuxrace PASS47.643s integration. Immutable runtimea62f4907.../helper18c294ff... собраны; свежий Functional QA владеет zns-knowledge-private-fqa/18970–18989. В live не интегрирован.
- Modernorders1061files/18delta заморожен после actualSobek/PG, unit/vet/pinned2.14lint0. История включает старые audit rows безsnapshot с явным details_available=false. Fresh CodeQA начат, imagebuild запущен; rendered FQA не заявляется.
- Дополнительный importer inventory: active messages history/adminstats требуют mapping владельца, исходных времён и сохранения privacy, это ещё не реализовано. StaticQA/about/lineup runtime уже есть; полный rehearsal должен доказать snapshot/config/runtimeartifact binding. Active bot_storage.menu_version запускает Telegram menu setup, отсутствовавший в Go; отдельный builder владеет qa.local/telegram-menu/source, live не меняет.

- Modernorders runtime/helper imagebuild завершён exit0; source manifest проверен сборщиком. Root live1125hashes повторно сверены. Свежий profile-composition CodeQA проверяет sharedmerge и выделение функций; root заморозил live до его отчёта.

## 27 сентября: большой заказ, история и диагностика

- Profile-composition independent CodeQA PASS: все20 integration paths, shared domains и две extraction проверены;1125hashes безdrift. Это source-review gate, не замена PG/FunctionalQA.
- Modernorders independent CodeQA P2: валидный заказ80KB/200extras требует больше16 sequential chunks; после двухVM чтение не завершено, agent mutation недоступна. Собственный reviewer Sobek/PG reproducer FAIL; остальные gatesPASS. Отдельный modern-orders-complete successor добавляет durable continuation с актуальной авторизацией/versionbinding, исходный1061candidate immutable.
- Добавлен proposed messages-migration-contract.md: ownership/bot атрибуция, исходные времена/порядок, privacy provenance, long-message completeness без увеличения requestcount, replay/removal/restart. Q2 о периоде history retention вынесен в PROGRESS; применение реальной истории отложено, другие направления продолжаются.
- Tool diagnostics RED: models.effective терял имя из-за неполного словаря; после добавления обнаружен второй дефект — models.own.get попадал под JWT regex. Добавлены17точных публичных имён, сохранены100прежних; exactknownnames проходят, configuredsecrets проверяются раньше, неизвестные имена/токены не допускаются. Финальный словарь — static switch без mutableglobals, limits/suppressions неизменны. Unit/vet/pinnedlint0 и LinuxracePASS; native Windows race не запустился из-за disabledCGO и не считаетсяPASS. Свежий независимый CodeQA выполняется. Manifest live-diagnostics-profile-credits-passes-broadcast-food-identity-models-massage-downloads-sha256.json1127files. Host-flow FunctionalQA отдельно в общем составе.

## 27 сентября: menu QA и безопасное удаление импортера

- Tool-diagnostics independent CodeQA PASS: сохранены все100старых имён и17новых,117roundtrips logger/export и351payload-bearing variants проверены независимо; focusedtests+CLI+pinnedlintPASS,4filehashes match. Actualhostflow FunctionalQA остаётся общей.
- Telegram menu1133files/13delta frozen. Builder fullinternalunits/focusedPG18.302s/vet/pinnedlint0/LinuxracePG2.801s PASS. Root imagebuild exit0: runtime693b360e.../helper748d22eb... . Fresh CodeQA и sourceblind FQA запущены, отдельный zns-telegram-menu-fqa19000–19019. Existing5baselinefiles восстановлены и проверены exactSHA, включая gofmt-normalized retained pre-extraction copies. Productlive не менялся.
- Новый importer-removal-next helper строится только для локальной synthetic БД: exactname/hostguard, stoppedwriters, explicit6receipt tables RESTRICT, schemaRESTRICT, transactionrollback, unexpectedobject/dependency denial, permanentrow/byte/sequence digests. Исторический CASCADE helper оставлен как неизменённое evidence, не используется. Это самостоятельная проверка helper; полный runtime import/removal/restartE остаётся обязательным.

## 27 сентября: подготовка knowledge merge и дополнительные границы QA

- Knowledgeprivate candidate1050hashes повторно сверены. Подготовлено шесть merge-only файлов в qa.local/integration-plan/knowledge-merged: сохранить все domain registry/projections, добавить knowledge bindings, перенести11новых diagnosticnames в текущий finite switch (knowledge.read ужеесть), обновить ordinarydiscoverycounts+8. Guarded integrate-knowledge.ps1 подготовлен, но НЕ запускался: FunctionalQA дополняет >20scopes/authorityoutsidefirstpage/general-onlyscope и EN/RU×mouse/touch cells. Live1127hashes unchanged.
- Telegrammenu CodeQA PASS после independentunits/vet/fullpinnedlint0 и трёх realPG cases ownership/restart, partialfailure/retry, cancellation/release. Frozen1133hashesunchanged. FunctionalQA проверяет отдельные locale/inputmode menu actions и существующие messagefaults. Default-language fallback adapter сверён с официальными Telegram getMyCommands/Determining-list-of-commands; отсутствие expliciten записи само по себе не баг.
- Importer-removal-next готов: actualsyntheticPG preservation/fullrow+sequence digests, alreadyabsent explicitresult, wrongDB/activewriter/extraobject/externaldependency/latefailrollback; всеtemporaryDBcleaned. FreshCodeQA выполняется, finalruntime/removalE не заявляется.
- Начат read-only history-import-design: конкретный способ полной bounded выдачи длинной истории без подмены исходных времён и увеличения requestcounts; Q2 retentionperiod не решается агентом. Это independent engineering work while actualhistory application awaits decision.

- Independent removalCodeQA подтвердил два actualPG дефекта: NOT LIKE 'pg_%' исключал legalapplication schema pgapp; enabled DDLtrigger+nextval менял permanentsequence несмотряна rollback, делая preservationclaim ложным. Originalhelper не принят, frozenuntouched; отдельный importer-removal-safe successor поручен владельцу. Finalreview продолжается, новые проверки не считаются выполненными.

## 27 сентября — интеграция knowledge/menu и проверка discovery

Knowledge privacy successor и Telegram menu прошли независимые Code QA и Functional QA, включая EN/RU mouse/touch; интегрированы в live manifest1143. Evidence: qa.local/knowledge-private-fqa/report.md, qa.local/telegram-menu-fqa/report.md, qa.local/telegram-menu-codeqa/report.md. Собственные временные стенды QA удалены после завершения.

Полные unit/vet/pinned lint общего состава прошли. Совместный PG прогон82.249s обнаружил одно ложное отрицание: проверка слова approve во всём JSON discovery захватывала описание suggestion, запрещающее approval. Исправлена проверка структурированного имени; проверки help/authority сохранены. Focused PG1.176s PASS. Повтор общего PG и независимый review интеграционных изменений остаются.

Modern orders successor1064files заморожен, новый Code QA выполняется. Removal-safe helper: девять PG scenarios/vet PASS, свежий Code QA выполняется; runtime removal acceptance отдельно. План истории qa.local/history-import-design/design.md готов; Q2 retention остаётся открытым, техническая работа с synthetic policy возможна.

## 27 сентября — removal helper Code QA и следующие кандидаты

Свежий qa.local/removal-safe-review/REPORT.md: scoped PASS, девять supplied + семь независимых PG scenarios, target guards и vet; frozen hashes неизменны. Дополнительно проверены quoted identifiers/partition, external FK, extra/missing objects, replica/disabled triggers. Ограничения: admin-only rehearsal; prepared-xact/transport/cancellation fault injection и реальный runtime/UI removal gate не приняты.

Modern orders successor собран без изменения frozen исходников: runtime sha256:31f3b11ceb4ea9ac470daef1330e2b5867fea27e4c6f7b1d52fa86c8feef8183, helper sha256:9c33a5b0ca15866d5d201effbea0ac137b83d1e5477bb3fba7762f8448382de7. Code QA выполняется; Functional QA впереди.

Runtime истории реализуется отдельно в qa.local/history-runtime от точного live1143 manifest. Владение isolated conversation/API/schema и bot/skill разделено внутри команды; основной platform не меняется. Retention Q2 не выбран. Общие integration Code QA и повтор PG выполняются независимо.

## 27 сентября — повтор общего PG и visibility finding

Общий live1143 composition PG90.585s PASS, discovery-fix pinned lint0issues; sessions35145/23147 завершились exit0. Это общий regression gate, не замена полной Functional QA. Независимый composition Code QA ещё выполняется.

Modern successor Code QA обнаружил раскрытие имени orders.review.read вне capability-блока в provider skill обычного пользователя. Выполнение защищено, но discovery/privacy контракт включает и prompt. Независимый actual provider prompt RED: qa.local/modern-complete-review/provider-visibility.log. Новый isolated successor qa.local/modern-orders-visible сохраняет предыдущий frozen source/images неизменными; до исправления этот кандидат не принят и его Functional QA не запускается. Прежние положительные PG/unit/static evidence не перекрывают finding.

## 27 сентября — Code QA композиции принят

qa.local/composition-review/report.md: PASS без actionable findings. Проверены все1143 frozen hashes до/после,30 changed paths knowledge/menu/assertion integration; существующие bindings/projections сохранены, finite diagnostics vocabulary не расширен произвольно, discovery assertions проверяют имена. Focused bot/observability/Telegram/sandbox tests прошли. Этот reviewer не запускал независимый PG/browser: общий root PG90.585s и отдельные Functional QA кандидатов остаются отдельными доказательствами, не финальной приёмкой полного продукта.

## 27 сентября — retained resources и проверка Git hygiene

Начат isolated qa.local/resource-binding harness: непустые bot_storage/static QA/about/lineup bindings, точные digests и runtime readback после переноса артефактов за пределы staging. Это воспроизводимая synthetic политика, не решение за production keys/resources; acceptance ещё нет. Исправлен устаревший текст forward-migration: CLI уже имеет шесть доменных apply paths.

Повторный Git inventory: среди tracked paths нет .go-cache/.cache*/node_modules/qa.local/*.local или exe/log/pem/key/sqlite по проверенному шаблону. git check-ignore подтвердил platform/.go-cache, platform/.cache-diagnostics, qa.local, platform/cache.local. Среди1355 видимых untracked файлов filename scan дал только шесть исходников/документов sticker cache; это проверка путей, не полный secret scan и не готовность всех файлов к commit.

## 27 сентября — frozen visibility successor и независимый FQA

Modern-orders-visible:1065 frozen files, manifest300b9f57b11ad97c3538e08b436902669fbf4ddaf58cddfe0b719f1566404e2c. Двухфайловый prompt/test delta; builder actualprovider ordinary/admin/grant/revoke RED/GREEN, units/vet/pinnedlint PASS. Свежий affected CodeQA выполняется.

Образы собраны с проверкой source hashes: runtime sha256:6f92e5c20b4e501e93db414110233709edf609b5e18115c2c5c71830a6d11de2, script sha256:35a80f73875c807b454ddfda2479b184009966bda0a7dc3e3f85f217c65a74ca. Отдельный source-blind FunctionalQA владеет qa.local/modern-orders-fqa и своим Dockerproject; получает neutralcontract/publicsetup, не исходники/предыдущие findings. Root Linuxrace session68031 использует finalsource readonly и sharedPG lane1; итог ещё не подтверждён.

Final modern-orders-visible Linux race завершён exit0, frozen1065 hashes перепроверены без изменений. Подробный лог qa.local/modern-orders-visible/linux-race.log. Shared PG lane1 освобождена. Public deterministic fixture DTO документирован в docs/product-sandbox.md для независимых QA без чтения реализации.

## 27 сентября — предварительная композиция modern и новые boundary observations

Подготовлены modern-changes.json (22unionpaths), prepare-modern.ps1 (восемь shared merge files) и modern-overlay.json. Live и frozen кандидаты не изменены. Обычные tool counts увеличены47→58/44→53; добавлено16точных operation names в текущий vocabulary, сохранены knowledge/pass/profile/credit projection/receipt поля. Overlay unit packages PASS и focused combinedPG35.341s PASS. Это предварительная проверка объединения, не разрешение интегрировать непринятый FQA scope.

Независимый FQA видит ограничение large-choice publicAPI и failure/retry до provider на boundary order+catalog. Отдельное воспроизведение/изоляция продолжается; Go actualSobek tests обходили полный runtime planning stage, поэтому их PASS не закрывает наблюдение. Evidence сохраняется в qa.local/modern-orders-fqa; read-only diagnosis в qa.local/modern-order-write-limits.

## 27 сентября — lineup Code QA и gates ресурсов

Lineup six-file candidate1146files: qa.local/lineup-codeqa/report.md PASS, actualSobek/focused tests/fullscriptworker/vet/pinnedlint0issues, hashes неизменны. Runtime/helper образы собраны, source-blind qa.local/lineup-sobek-fqa начал отдельный Dockerproject. Functional acceptance ещё нет.

Resource-binding независимое replay PG прошло, но adversarial review доказал: неизвестный included domain с согласованным receipt manifest hash не отвергается. Это дефект coverage harness, не доказанный дефект runtime. Harness не принят, итоговый отчёт/исправление впереди.

Modern largecatalog FQA isolation: тот же queued update не выполнялся после уменьшения заказа до{}, но выполнился после восстановления обычного event.extras, без ручного измененияcursor. Дополнительно проверяются реальные API/Sobek write limits. Код кандидата/стенда не менялся.

## 27 сентября — история frozen и modern FQA not accepted

History runtime source1150files (baseline1143),29delta/no deletions заморожен. Builder actualPG conversation/API/bot, bounded Sobek continuation, sqlc/vet/pinnedlint0issues и compile allcmd/internal/integration прошли; независимый history-runtime-codeqa начал review всегоdelta. messages-importer получил frozencontract для isolatedapply; runtime пока не принят, Q2productionretention не выбрана.

Modern FQA report qa.local/modern-orders-fqa/report.md NOTACCEPTED: даже обычный API-created owner order с расширенным catalog приводит к retries до script; восстановлениеcatalog выпускает fallback/model.cache, а не успешное Sobek выполнение. Отдельная diagnosis подтверждает APIcreate/edit/quote invalid_json для допустимого262144byte canonicalchoice, а actualSobek edit отвергается workerargs или API для меньшего~70KiB. Root разрешил cleanup releasedFQAstand после сохранения повторяемыхfixtures/screenshots; никакой integration acceptance нет.

## 27 сентября — стратегия исправления modern и история Linux

Read-only qa.local/modern-order-write-limits/report.md подтверждает самостоятельные catalog-only и order-summary-only сбои actualremote planner, API64KiB vs domain256KiB и script128KiB. Одно увеличение transport не позволяет передавать arbitrary256KiB choice между VM/turns при4KiB output. Выбран узкий вариант: один orders.choice(begin/patch/read), choice_ref в существующих quote/update, host-owned canonical drafts в existing durable receipts, compact catalog item references. Current owner/event/catalog/version/rights и bounded calls/traffic сохраняются; новый generic framework/table не нужен. Smallchoice contract остаётся, отдельный orderAPIdecoder покрываетdomainmax. Builder работает только в новом qa.local/modern-orders-bounded, freshgates/QA обязательны.

History runtime/helper собраны (a6bbeaa94c38525e3513726567077707b3fd221f4b76ded28b1e20430f0ca95b / 5fd145b611b601ba8758813ae6b98098761385fac4ee604a0c535d43f0efbb93). Linuxrace session6761: conversation/bot PASS, интеграционная часть ещё выполняется; API selector не выбрал tests, не считать отдельнымAPIpass. FreshCodeQA продолжается.

## 27 сентября — history race / importer pipeline / strict resource review

History Linuxrace finishedexit0: conversation2.707s,bot5.441s,integration9.865s; API selector matchednotests. Frozen1150 hashes unchanged. Built stand-only zns-history-runtime-qa-host CLI calls actual DeleteContent, restricts hosthistory-db/databasehistory_fqa/fixtureactors and explicitconfirm; no public productendpoint added. Networkless missingconfirm test returnsinvalid_fixture/exit1. Helperartifact files outsidefrozen source.

History independent adversarial finalplan overlay PASS: deletion during modelcompletion after ordercreate leaves terminalplan;retry stale,2modelcalls/1script/1order. Originalbroadintegration stillactive. A redundant affectedrun was explicitlyaborted and notcountedpassed; releasedlane assignedmessages importer.

Messages builder realApplyUsers(profile included)→Messages focusedPG3.74s PASS: userscreate no conversationrows, longbody/order/replay/tombstone/permanentrefs/preexistingguard. Offline/vet passed; acceptance notclaimed. Resource-complete QA FAIL selectedbot mismatch/ambiguousrecord/duplicateJSON/trailingvalue; strictsuccessor nowreuses actualmigrate.Verify plus semanticbindings.

## 27 сентября — lineup принят и интегрирован; история test successor

Lineup source-blind Functional QA PASS в qa.local/lineup-sobek-fqa/report.md: actual isolated Sobek, EN/RU mouse и wide emulated touch, current/day/full, 600 entries за 8 страниц/два update, общий typed/script budget, forged/filter/restart cursor denial и bounded output. Узкий 480px stand layout и физический touch не приняты; реальная модель остаётся финальным gate. Стенд удалён, evidence и frozen images сохранены.

Root интегрировал шесть принятых файлов и обновил четыре discovery count assertions. Manifest qa.local/integration-plan/live-lineup-sha256.json содержит 1146 файлов. Полный internal/command unit gate PASS; совместные vet/lint/PG ещё не завершены. Ранее подготовленный modern overlay устарел и не применяется.

History original Code QA не зелёный из-за discovery counts и onboarding fake. Builder воспроизвёл onboarding failure на live baseline и frozen successor; fixture не отвечал на startup menu methods. Новый qa.local/history-runtime-counts меняет только три integration test files, сохраняет все SQL retry/ACL assertions. Focused PG/vet/pinned lint PASS; свежий независимый review запущен. Runtime source и исходный frozen1150 не изменены.

Lineup после интеграции: полный internal/command unit gate, vet и pinned golangci-lint2.14.0 ./... PASS (0 issues). Четыре затронутых discovery/owner/replay integration tests на реальном PostgreSQL PASS5.431s; shared lane2 освобождена. git diff --check PASS. Это scoped composition check, не полный Functional QA состава.

## 27 сентября — history composition и новые независимые находки

Fresh qa.local/history-counts-codeqa/REPORT.md PASS:1150 baseline/frozen hashes, ровно три test files, четыре affected realPG tests5.260s, pinnedlint0. History runtime Functional QA запущен source-blind на отдельном Docker-стенде; приёмки ещё нет.

Root prepare-history.ps1 создаёт32path Go overlay поверх принятого lineup, не меняя live/frozen source. Первичный unit RED поймал потерянную lineup private-result projection в объединённом registry; объединение исправлено с сохранением history.read и lineup.query. Повтор bot/agent/api/conversation/observability unit PASS, affected owner/discovery/replay/history/onboarding realPG8.668s PASS. Применение в live ждёт функциональной приёмки истории.

Resource strict QA обнаружил две P2 неоднозначности receipt/config: duplicate/alias/extra keys и нестрогая config→resource привязка; ambiguous full runtime положительный тест подтвердил обход. Новая qa.local/resource-binding-unambiguous изолирована, предыдущие1137 frozen files сохранены.

Messages importer197files/9delta заморожен, offline/vet/pinnedlint и actualCLI users→messages PG прошли. Независимый Code QA обнаружил silent replacement lone-surrogate escapes в U+FFFD; concurrency replay, atomic rollback и altered-target reconcile прошли. Новый qa.local/messages-importer-unicode исправляет lossless validation; приёмка исходного кандидата не заявляется.

## 27 сентября — полные runtime проверки нашли два блокера

History source-blind FQA прочитал два Unicode/control-escaped сообщения94525/94530codepoints (24chunks каждое), сверил полный checksum, продолжил через два app restart, проверил foreign/tampered cursors и host deletion/count preservation. Контролируемое удаление во время final-model response доказало P1 operational blocker: redacted terminal plan update22 повторяется бесконечно, сохраняет эффект ровно один раз, но не выпускает следующий update23; перезапуск не помогает. Gate FAIL. Новый qa.local/history-runtime-terminal сохраняет исходный1150candidate и устраняет именно durable terminal handling; общая queue continuation обязательна в RED/GREEN и freshQA.

Deployment freshCodeQA qa.local/deployment-composition-codeqa/report.md: собственный networkless Linux PostgreSQL17.11, два production migrate (64migrations безfixtures), runtime/meter grants и restart persistence PASS. P1: existing /bot stripPrefix несовместим с root-absolute assets/fetches и cookie Path=/miniapp; P2: docs не описывают accounting.password и media zns_meter DB access. Новый qa.local/public-prefix исправляет paths/cookies и документацию в изоляции. Production/server_configs не менялись, QA PG удалён.

Удалены только два неиспользуемых superseded knowledge image tags после сверки всех container image IDs. Ledger qa.local/integration-plan/retired-knowledge-images-20260927.json; исходники/отчёты сохранены. Live1146 hashes повторно совпали, git diff --check PASS.

Resource-unambiguous freshCodeQA PASS: qa.local/resource-unambiguous-codeqa/report.md, 11focused runtime tests, vet, independent positive+11adversarial controls,1147hashes unchanged. Root повторно подтвердил только postgres DB и0activeclients, удалил released project zns-resource-binding-complete (15484). Общая функциональная приёмка импорта/удаления импортера отдельно.

Messages Unicode successor199files/4delta: source hashes preserved, full offline/vet, pinnedlint0 и focused realPG7.131s PASS. Fresh independent messages-unicode-codeqa запущен. Root отдельно исправил только копию deployment docs для public-prefix candidate: accounting password/media zns_meter и enforcement alignment; live docs не менялись.

## 27 сентября — terminal history и modern bounded frozen

History terminal candidate1151/3delta (две product files + actual runtime test) прошёл оба воспроизведённых RED, nativePG, broad Linuxrace bot/conversation/integration1.968/2.138/12.107s, final affectedrace3.676s, vet/pinnedlint0. Root собрал immutable runtime b8f53a04f2b2c3d9f50150b48dfa2fbc810a95f75d10c1737e1f9fc426ee8705 и script9fdb5a274e6bf16dcbcd45b53969b4b14b970520ab5f988332964920a57b1351; hashes verified. Fresh CodeQA проверил дополнительный late-script completion и отказ terminal markerUPDATE/restart без повторов; отчёт ещё финализируется. Fresh source-blind history-terminal-fqa начал отдельный стенд.

Modern bounded root inspection выявил оставшийся64KiB decoder в MiniApp quote/save. Builder signed requests воспроизвели RED2.251s, shared order-specific decoder дал GREEN2.901s, включая права/stale/replay/oversize. Frozen1081/32delta противvisible1065, manifest0d5efcda675b51cbef29278026953be7bc5920abbba9e2a1b40c24ee3ce72bf5; full affectedPG85.117s/unit/vet/pinnedlint0. Root images runtime0f37f1786884f7a4960225834535dc653c8530271017491adb685ef9f94da652 / scriptd47e0efa766dcc1f6d194fe73775c52d6aead9385dc90106f1c90413ab958850. Fresh CodeQA идёт, FQA впереди; интеграции нет.

Messages Unicode freshCodeQA PASS,199candidate/197baseline/65dependency-migrations unchanged, full actualPG suite+independent CLI/source-byte/Unicode/tombstone controls, pinnedlint0. Builder готовит qa.local/full-import-rehearsal/messages-complete без изменения старых репетиций:18files,12domains included/nonempty,9messages и exact5resource cross-binding; offline планы семи доменов безблокеров. PostgreSQL orchestration ещё не проверен.

Root qa.local/importer-removal-messages расширяет exact receipts whitelist с6до7 и использует len списка для inventory. Новый тест с реальной066schema + nonserial message_receiptsFK сохраняет полное Unicodebody, provenance/tombstone/events/generation; original helper RED inventory_rejected, final9oldscenarios+history GREEN2.585s/vet. Frozen five-file manifest, свежий CodeQA назначен. Полный import/removal/runtime acceptance отдельно.

History terminal independent CodeQA FINAL PASS:1151candidate/1150baseline hashes unchanged, actual Run late-script deletion и failed markerUPDATE/restart, credits/onboarding guards, pinnedlint0. Новый FunctionalQA работает отдельно, принятия runtime пока нет. Modern bounded свежий FQA назначен на immutable образы, CodeQA готовит независимые actualworker/race cases.

Removal-messages freshCodeQA FINAL PASS: independent six-table/extra-eighth/external message-view refusal, полные row/sequence/catalog неизменны и reconnect; реальная066schema body/provenance/tombstones/generation сохранены. Preparedtransaction guard проверен исходником, не динамически. Полный runtime после CLI-import/removal отдельно.

Полный syntheticCLI первый запуск: users→messages(9references/7events)→events PASS; orders отказал до writes с order_configuration_records_required на сохранённый configuration/resource config.json. Fixture/артефакт не удаляется и не переклассифицируется для зелёного результата. Builder исправляет domain disposition в отдельной копии модуля с freshQA после RED/GREEN. Связанные дальнейшие domain applies и removal ещё не выполнены.

Public-prefix builder frozen1153/23delta: candidate manifest c6565e00d3cfaa2020d39644efc100b10b499678eb9a0d236490c432411d7deb. Реальный stripped /bot и root EN/RU mouse/touch: consent/reload, editor quote/save, timetable aliases ACL, legacyfood quote/save/photo, logout; Go/JS/vet/pinnedlint0. Root immutable runtime dd389d1da903f5ac4e33ac24381ed53f6341e16e17a8fb488b3ff7bd1b461df8 / script607643312711e5df397c8bdc5024df993615a25e85c9a15f8bafb6063ed7ce2a, hashes сохранены. Fresh independent public-prefix-codeqa назначен; FunctionalQA впереди. Это candidate, не изменение production/live.

Public sandbox docs уточняют GLOBAL fixture capacity32scopes/32steps/256KiB, отсутствие reset endpoint и перезапускfake с сохранённым chat/update state. Для summary есть отдельный remote wirecontract; полное чтение истории внутриSobek не означает пересылку большого chunk через4KiB finalreturn. Эти разъяснения не меняют продуктовые бюджеты.

## 2026-09-27 — independent restart and public-prefix findings

- Modern bounded1081: fresh independent Code QA PASS (`qa.local/modern-bounded-codeqa/report.md`), including real queued HTTP/external Sobek and PostgreSQL boundary checks. Source-blind Functional QA continues; root Linux race is a separate pending gate.
- Public-prefix1153: fresh independent Code QA PASS (`qa.local/public-prefix-codeqa/report.md`), all source hashes unchanged. Source-blind Functional QA reproduced a redirect outside `/bot` when requesting `/bot/miniapp` without a trailing slash through the actual stripping proxy. Separate immutable successor requested; existing valid-slash browser results do not close this defect.
- History terminal1151: source-blind Functional QA reproduced model regeneration after privacy deletion followed by a crash before final plan persistence. On two restarts the old inbox item reached the model again with redacted history; later items waited. No source-content leak observed. Separate successor requested; previous Code QA and race results do not establish acceptance of this restart boundary.
- Full synthetic archive: builder ran all seven actual CLI applies, zero-write replays and reconciliation with 31 SQL invariants. The reviewed seven-table removal helper returned `removed_preserved`; permanent runtime fingerprints match (`qa.local/full-import-rehearsal/messages-complete/run-retained/removal-evidence.json`). Populated runtime database retained for independent acceptance. Converter delta review and source-blind imported-state runtime acceptance remain required.

- Modern bounded source-blind Functional QA isolated a second P1: eleven valid public order changes produced a 1,788,676-byte history response. The inbox retried before reaching the model. Reducing only four saved synthetic audit snapshots reduced the response to 787,769 bytes and immediately allowed the same queue item to finish; event and order-list responses were unchanged. Original snapshots preserved/restored. Repair is owned by `qa.local/modern-orders-history-transport`; no live/frozen source changed. Code QA PASS does not override this functional failure.
- Root independently compared importer-removal evidence: all156 permanent table/sequence fingerprints match, zero missing/changed entries. This is preservation evidence, not runtime functional acceptance.

## 2026-09-27 — restart1152 and redirect1154 frozen successors

- History restart1152, delta3: builder actual Bot.Run RED/GREEN, PostgreSQL77.410s, final Linux race12.458s, vet/pinnedlint pass. Fresh `qa.local/history-restart-codeqa/REPORT.md` PASS: focused PG6.840s and independent ledger/ACL isolation matrix2.233s; all1152 hashes unchanged. Frozen runtime a268b34edc80d6e28fe5a05550bdda21cd2c0bd3e928fed597d70992d40eee90, script d46fb2a75878a294491e87b0d1e23f456e13a9a7673cde6f606ef91ea35fabd1. New independent source-blind Functional QA active; not accepted/integrated yet.
- Public redirect1154, delta4: composed app/proxy RED, root+/bot GREEN preserving307/raw query, unit/vet/pinnedlint pass; focused PG2.487s. Frozen runtime d198b7cfdb496c210e9e95c931235fc50ce57b7247f3aebfa9ad2259a1241025, script0e868a6c5683a1c4c79befcea807671fea8fb673bb9c643c1d3e5f5d1f28e12e. Fresh affected Code/Functional QA required, live source unchanged.
- Root bounded1081 Linux race finished with one failed eventual assertion in TestModernChoiceFullRuntimeAcrossRestart/d096, integration228.719s. No race detector report; this is still a failed check, not a pass or proven harmless timing issue. Evidence `qa.local/modern-orders-bounded/linux-race.log`; successor owner must diagnose/recheck affected scenario.
- Final retained-configuration converter201 frozen; final offline tests/vet/pinnedlint pass. Earlier full CLI run/removal used a slightly earlier executable. Fresh independent CodeQA owns exact-final binary full PostgreSQL repetition; original post-removal runtime dataset remains untouched.

- `qa.local/retained-import-codeqa/report.md`: final converter201 PASS. Independently rebuilt final source, repeated all seven actual CLI apply/replay/reconcile stages and full PostgreSQL suite,31 invariants,16 adversarial claim/JSON/receipt probes, exact retained body/provenance comparison; hashes unchanged. Runtime and removal gates remain separate.
- Imported runtime Functional QA owns a private database clone and normal frozen restart1152 image without importer/archive mounts. Root verified all65 migration-file digests against the imported schema receipt. Retained resource binding pinned independently to SHA256 fe00a6caa5ae09337d569ae390347f1cc15d8aa74dc84db2f218032b0d35a2a9. Placeholder synthetic.invalid provider subjects require explicit local stand provisioning; only owned-clone provider identity fields may be adapted, all owner/business IDs stay intact. This is runtime adapter acceptance, not an exact CLI-to-real-provider mapping rehearsal; that boundary stays explicit for final composition.
- Fresh `qa.local/prefix-redirect-codeqa/report.md` PASS:1154 hashes,24 independent adversarial route cases, pinnedlint0. Source-blind browser acceptance continues before integration.
- Retired old `history-terminal-fqa` containers/network/private volumes and old public-prefix Functional QA stands after evidence preservation. New successor stands and all frozen source/report evidence remain intact; no global Docker prune.

- Public-prefix redirect Functional QA additionally observed malformed empty `order_id` opening editor HTML successfully but surfacing a JSON parse error from the missing-order HTTP404. This is a separate editor error-handling/i18n follow-up, not the routing fix and not a user decision. Canonical-routing successor intentionally does not alter frontend behavior; verify the error contract before a narrow follow-up.

- Linux importer filesystem/Unicode gate PASS on exact final201 source: `qa.local/retained-import-linux/check.log`, including actual symlink rejection (not skipped), malformed-surrogate rejection, valid replacement/supplementary preservation and reviewed retained-config cases. No database used; source read-only and all201 hashes reverified.
- Modern history transport1084 initial Linux race failed on the existing five-second runtime eventual assertions (no race-detector report). Source preserved. Successor adds a complete one-response fast path for histories within the existing limit and bounded paging only after proven response-limit exhaustion; generic auth/network/JSON failures must not trigger fallback. Fresh reviews target the eventual final successor, not1084. Initial independent1084 reviewer stopped before tests and reported source-only, unaccepted scope.

## 2026-09-27 — imported free assignment and canonical routing boundaries

- Source-blind imported runtime FQA found own archived free-payment read403 despite real provider authentication. Original immutable source u404 contains paid/zero/free_pass and assignment date but no payment timestamps. Root read-only verification on original post-removal database confirms paid zero booking, no attempt and absent current legacy payment metadata. Repair belongs to importer, not authorization relaxation or fabricated payment times. New owned `qa.local/importer-free-history` successor required; old source and Functional QA stand remain unchanged.
- Fresh canonical1155 Code QA rejected the path classifier: mixed repeated slashes plus parent segments (including escaped leading slash) reach mux redirects outside `/bot`. Evidence `qa.local/prefix-canonical-codeqa/report.md`; source hashes unchanged. No1155 images built and no live integration. New routing successor must reject structural noncanonical routes coherently while preserving all valid routes, root and encoded ID data. Malformed private-route cleanup redirects are not a required compatibility behavior.
- Retired old modern-bounded Functional QA stand/privateDB/proxy and prefix-redirect mounted stand after independent reproduction/evidence preservation. Reports, snapshots and source remain; new QA stands untouched.

## 2026-09-27 — live history retention acceptance

- Independent history restart Functional QA completed: qa.local/history-restart-fqa/REPORT.md. Restart/deletion barriers, actual IPC late replies, saved ledgers, transient persistence/ack failures, Unicode imported bodies, role revocation, source counts and EN/RU mouse/touch passed. Whole scope remains NOT ACCEPTED: valid live Telegram input of 5508 bytes/3008 codepoints becomes 5000 bytes/2726 codepoints with omitted=false and no retained body; actual Sobek read cannot recover the tail. New qa.local/history-live-retention successor owns durable original capture, bounded UTF-8 model projection and full-source privacy checks.
- Independent imported runtime report completed at qa.local/imported-runtime-fqa/REPORT.md: FAIL for historical free-payment read, with explicit gaps for new uploads/replacement, deterministic races/retries and some lifecycle/rendered cases. Completed checks are scoped evidence, not full imported-runtime acceptance. Both frozen stands preserved for successor comparisons.


- Root reverified live1146, frozen routing1155 and modern history1085 manifests; no source drift. Final modern fast-path gates passed: native PG6.843s, Linux race145.786s with unchanged five-second waits, full pinned lint/unit/vet. Fresh independent source review is active; no images or acceptance claims yet. Routing1155 structural successor also passed builder gates (PG2.703s), and fresh independent source review is active.


## 2026-09-27 — independent boundary reviews and focused successors

- Fresh routing Code QA confirmed 36 mounted automatic subtree redirects drop /bot across six registered roots, escaped equivalent segments and GET/HEAD/POST. qa.local/routing-final-codeqa/report.md and subtree-results.txt preserve actual HTTP evidence;1155 hashes unchanged. New public-prefix-subtrees successor required; no integration or images from rejected candidate.
- Modern final Code QA found known-invalid oversized JSON prefixes could be masked by history fallback. New modern-history-prefix successor reproduced six corrupt-response cases and preserves bounded reads with standard JSON decoding. Final quality gates and fresh independent acceptance still required.
- History live-retention1154 passed builder RED/GREEN, PostgreSQL, vet/lint and Linux race; six-file delta retains complete original text atomically, keeps bounded valid UTF-8 projection, checks sensitive suffix and prevents replay resurrection. Fresh independent Code QA active; live1146 unchanged.
- Importer free-history202 passed actual full CLI RED/GREEN, full PostgreSQL68.965s, replay/reconcile, reviewed seven-table removal with unchanged permanent fingerprints, and real runtime own/foreign/free-proof checks. Fresh Code QA and source-blind local-IdP Functional QA active; source dataset importer_removal_synthetic_free_green preserved read-only.
- Root editor error overlay passed actual component-browser RED/GREEN20 root/mounted mouse/touch scenarios with RU/EN switching/reload, ESLint and Prettier; raw upstream parser diagnostics replaced by localized messages and missing-ID request prevented. qa.local/editor-error-ux/REPORT.md records limits. Overlay transferred into mutable routing successor for fresh composed acceptance; not independently accepted yet.


- Fresh importer-free Code QA PASS: six independent PostgreSQL boundaries8.50s,14 existing pass tests12.93s, no skips,202 source hashes unchanged. Runtime/GUI acceptance remains separate. Fresh live-history Code QA PASS: independent atomic rollback/Unicode/digest/ACL/privacy/concurrent replay+delete probes2.304s, focused live transport2.152s;1154 hashes unchanged. Frozen live-history images built: runtime352fc10a4e3a9be571b80849ce753dffb42147a319a0f625b42ac909f76bc414, worker c85abb34e9c833ea416725fcd16887999ab401edfb2655376746053757abde15. Source-blind successor FQA now running.
- Automatic cleanup review initially rejected retirement of history-restart-fqa/imported-runtime-fqa as possibly active. Both original owners then independently confirmed completed reports, no pending work/active workers/transactions and no external volume consumers. Same exact cleanup retried with this evidence and approved: old containers, networks, named volumes and standalone imported-runtime-fqa-pg anonymous volume removed. Active history-live-fqa/imported-free-fqa and shared PG/identity remain; all evidence directories retained.
- Subtrees routing candidate1158 passed builder gates, but fresh independent Code QA reproduced invalid successful editor JSON publishing editable partial state and exposing TypeError. New editor-validation successor required; routing unit checks continue. No candidate images/integration from this rejected snapshot.


- Removed eight unused obsolete QA images without force/prune after docker ps -a confirmed no consumers: public-prefix/redirect runtime+script, modern-orders-bounded runtime+script, history-runtime-terminal runtime+script. Current history-live-retention/history-runtime-restart images (active QA) and toolchain/base images retained. Frozen sources, manifests and reports remain.


- Modern prefix independent Code QA PASS under standard encoding/json classification: complete decoder errors and definitive syntax failures remain fatal; incomplete prefixes can defer semantic type errors. Independent full-client HTTP proof showed fallback returns only freshly authorized complete data; denied detail returns error+nil, never partial data. Strict-prefix probe failure is retained as an explicit limitation, not erased or solved with a custom streaming schema validator. Actual PG latest30/concurrent append/deleted/expanded history checks passed;1085 hashes unchanged. Candidate images building for fresh full Functional QA.
- Imported free FQA confirmed corrected own archived free-payment API200 with proper unknown provenance, then found actual Sobek registration.read payment rejects explicit archived event as unknown_event. New archived-pass-runtime successor will preserve own historical reads without reopening mutations. Existing frozen FQA stand remains unchanged; broader acceptance not claimed.


- Imported-free Functional QA final report: qa.local/imported-free-fqa/REPORT.md, NOT ACCEPTED for archived payment agent reachability. Thirteen API observations identical before/after two restarts; own free unknown provenance, paired paid proof bytes, foreign denials and stale review409 replay passed. Source-import variants, live revoke/concurrency gaps are explicit. Separate archived-pass runtime fix is active; no workaround data/role changes on frozen QA stand.
- Modern history frozen images ready: runtime747bf43b7c1ff69d0fb12baf8d1617c224278a4df60ad3bce7af1bc7bf12ba0c, worker4bda5c0ee4c3fc215b39edfec5da70cc10bf8f9c01133684472d1478b5a80a6d. Fresh source-blind modern-history-fqa owns full business/history scope on separate local stand; acceptance not yet established.


- History live-retention independent Functional QA PASS (qa.local/history-live-fqa/REPORT.md): exact Unicode bodies, source-bound pagination, retries/restart, deletion barriers, current rights, EN/RU rendered mouse/emulated touch, profile and photo privacy. Synthetic providers only; no audio/video transcription or real Telegram. Joint live composition verification remains separate.
- Modern history Functional QA FAIL (qa.local/modern-history-fqa/report.md): valid 1,056,014-byte event catalog (SHA256 41482d9cb174f06541c62bdec6c9d3dded8ddc3ac6b7ba963b95ae080a835487) blocks MiniApp GET and queued Bot.Run across restart. A separate catalog transport candidate reproduces the issue and is under verification; prior history/business passing cases do not waive the full-scope failure.
- Editor-state candidate frozen at 1158 files, manifest d5166046459385c45609381ccaebc9311b80a924ed321ed1a73ca52e7e2abfb1. Builder reports 248 browser cases, unit/vet/lint/PG passing. Fresh independent Code QA is active; Functional QA remains required.
- Archived pass runtime candidate frozen at 1156 files, manifest c10895da766192d8a11ce40c6fb5d5a7ea42294c2a73f725e421601b36643e23. Builder unit/PG/vet/lint/race pass; fresh independent Code QA active. No source archive or imported retained DB was modified.
- Prepared history+modern integration in qa.local/integration-history-modern-draft/source, resolving nine three-way conflicts while retaining live credit/profile/language/lineup/history privacy behavior. Updated the profile-redaction callback to the combined transactional tool-store signature. This draft is not accepted, frozen or applied to platform.
- Joint history composition first run exhausted PostgreSQL shared lock memory when test packages initialized databases concurrently; repeated with package/test scheduling serialized and unchanged assertions/timeouts. No product change or green claim from the failed run.

- Joint history composition1157 passed all internal/cmd unit tests and 150 selected PostgreSQL tests (250.224s) with serialized scheduling; all precheck source hashes remain unchanged. Fresh composition Code QA, vet/lint/race and final composed Functional QA remain. Candidate is not integrated into live platform.

- Fresh archived pass Code QA independently reproduced two defects (qa.local/archived-pass-fresh-codeqa/probe-final.log): historical assigned registration without payment attempt falls to unavailable/current picker; after interruption and removal of the owned registration, normal retry denies Registration.Reads but still exposes stale booking content via Script.Runs.Result. Frozen candidate not accepted. Successor archived-pass-runtime-privacy assigned; no source/archive/stand workaround.

- Fresh history composition Code QA rejected candidate with independently reproduced P1: deletion after final generation check but before assistant archive allows stale model text to reappear as a new conversation event (qa.local/history-composition-codeqa/report.md). New history-append-generation successor assigned; atomic append fencing and durable terminal handling required. Earlier scoped FQA remains evidence only for its executed cases.
- Editor-state images built and frozen manifest reverified: runtime sha256:3e4178cc63481dfb226d6eebe995d8a215a89e26aac1e1eef9654938f88ae8ff, script sha256:cbde37827d5f5898858de1fb2a284573a06ff560456bb763f9959348b9cd5ef4. Independent editor-state-functional owns root/mounted synthetic stands.
- Retired completed history-live-fqa project with compose down --volumes after owner release and direct verification: inbox0, active database queries0, QA triggers0; both pg/ipc volumes had only project-local consumers. Seven containers, both volumes and project network removed; evidence, frozen source and images retained. No global prune or other stand touched.

- Removed abandoned partial integration-history-modern copy after verifying all 56 remaining files exactly matched integration-history-candidate/source. Kept active integration-history-modern-draft and its resolved merge state; no unique changes removed.

- Refreshed three stale parity inventory qualifications against current platform/PROGRESS: integrated food Sobek, accepted lineup Sobek, and credit lifecycle/cutover with credit-mode bypass of question counts. Final composition/real-provider scope remains open; this documentation refresh adds no QA acceptance claim.

- Catalog successor frozen (1091 files, manifest fdf64ef56b87cec32c1cabbc91dcc842c068797d003602cc12bf5ec832148255); fresh independent Code QA and new full-scope Functional QA preparation started. New fixture explicitly retains native5s responsiveness and uses race20s correctness observation; original race5s failures are preserved for independent review. Nine verified catalog delta paths overlaid into the unaccepted history-modern integration draft only; live platform unchanged.

- Editor-state Functional QA found composed-runtime routing failure on exact runtime3e4178cc…: raw /bot//miniapp/../v1 ->307 Location:/v1, /bot/miniapp/../outside ->307 /outside, /bot/miniapp//api ->307 /miniapp/api. Direct internal malformed path also redirects; original stand untouched. Evidence qa.local/editor-state-functional/routing.json. Separate public-prefix-runtime-routing successor assigned; editor 368 passing assertions do not close routing gate.

- Catalog runtime fresh Code QA PASS (qa.local/catalog-runtime-fresh-codeqa/report.md): six adversarial host probes; 2,520,088-byte Unicode/escaping reconstruction over33 chunks; four PostgreSQL catalog cases7.286s; queued actual Sobek1.231s within native5s. All1091 hashes unchanged. Test-specific race observer reviewed as correctness-only, production deadlines unchanged. Repeated whole-catalog serialization per chunk remains documented scaling limit. Immutable Docker builds started for independent full catalog-orders-functional scope.

- Catalog runtime Docker build91702 completed successfully; all1091 frozen hashes reverified. Runtime sha256:6682c0fa28c8fc525cde657ac47c54dbf94dc4014cf651462336cdd2f562579a, worker sha256:e8652fb9cb5a4789b2bf3e6d1342b9002b7c7a6caa790ffcf104752fe2c60054. Fresh source-blind catalog-orders-functional received IDs and full business/history/catalog acceptance; no current functional acceptance claimed.
- Full-composition routing RED reproduced through actual serveListener/telemetry/public/app/Core and real stripping proxy. The outer telemetry ServeMux normalizes paths before the public guard. Separate successor replaces only that outer dispatch and preserves exact editor bytes; gates/independent QA pending.

- Combined history/modern/catalog draft compiled all packages with go test -p1 ./... -run '^$' (session66895 exit0); no test bodies ran, so this is compilation evidence only. Nine verified history append delta paths added to draft; live platform/frozen candidates unchanged.
- History append generation candidate1160 passed builder gates but fresh Code QA FAILED (qa.local/history-generation-fresh-codeqa/report.md): deletion completed after archive commit and before interaction insert; normal Handle and later explicit Render displayed stale canary. Append-before-delete linearization is not disputed. Separate history-reply-generation successor must enforce visibility of persisted derived replies, not only archival admission.
- Runtime routing candidate1159 frozen (manifest cc17784fa802f708444ba71d463a1097da6c3763bcdacb8fd415245d1c409f90); actual launcher/HTTP controls, units/vet/lint/PG passed. Fresh Code QA active; new source-blind runtime-routing-functional prepares independent root/mounted HTTPS checks. Prior editor Functional QA final:656 passing editor assertions, route gate failed; precise unexecuted cases remain in its report.
- Archived pass privacy successor1159 frozen (manifest7c6b5603f17576bf05c1d586e534316bbdb390bce20bdf405c19fe82eddea0e6). Units/vet/lint0/PG and Linux race54.216s passed; fresh independent Code QA started. Functional acceptance remains pending.
- Runtime routing fresh Code QA PASS (qa.local/runtime-routing-fresh-codeqa/report.md); all1159 hashes unchanged. Immutable images built successfully: runtime ea5ac8a2906ad1385a8b4d1a65c68db156efbb0154554ed82f0474cbc7d1c95e; worker970963fedd89cd8900dfe7258f4925735621db4752f39fac09c78ac2e5c93546. Independent Functional QA received images for own root/mounted HTTPS stands.
- Unaccepted history-modern draft received32 verified runtime-routing paths. The single overlapping handler composition preserves both large-order request decoders alongside public routing/assets. Exact ancestor/input hashes and merge assumptions checked by qa.local/integration-plan/overlay-runtime-routing.ps1; prefix-overlay.json records before/incoming/after. No live source edits or new combined acceptance.
- Archived privacy fresh Code QA FAILED P1 (qa.local/archived-privacy-fresh-codeqa/report.md): interrupted passes.events script retains private historical membership in derived run result after booking deletion/retry. Existing focused suite passed16.544s but independent overlay probe failed; all1159 hashes unchanged. New discovery-privacy successor assigned; no functional acceptance claimed.
- Runtime routing HTTPS Functional QA found exact-escaped-path contract violation for /%6diniapp GET/HEAD: Location canonicalizes to /miniapp/. Structural malformed requests remain denied404. Separate escaped-entry source copied1159 verified files; actual launcher/proxy test reproduced RED on root and /bot, then GREEN after preserving request RawPath through trusted mount. Four affected Go packages passed; PG/vet/lint pending. Frozen current stand and editor bytes unchanged.
- Escaped-entry successor1160 frozen (manifest89a10a812ae92f5dd80d2066088a868253673edf9efaed8ad23457b0cfe17f66). Actual launcher root/mount RED reproduced, all four affected packages GREEN, PG consent2.595s/vet/full pinned lint0. Only Gateway handler and one new runtime regression test differ; editor untouched. Fresh independent Code QA started. Two verified paths composed into unaccepted draft while preserving large-order decoding; no live edits.
- History reply-generation successor1163 completed builder gates: exact independent RED→GREEN, five reply surfaces/current/manual/system/restart/retry, selected integration156.037s, pinned lint0, Linux race conversation2.530s/integration17.929s. Fresh independent Code QA started; prior candidate1160 unchanged and no new acceptance asserted.
- Escaped-entry fresh Code QA PASS: independent overlay1536 entry requests plus launcher routing tests, all1160 source hashes unchanged (qa.local/escaped-entry-fresh-codeqa/REPORT.md). Fresh source-blind HTTPS Functional QA prepares own stands; immutable image build awaits lane.
- Runtime-routing Functional QA final FAIL only escaped-entry P2:516 raw requests/216 malformed denials and8 rendered root/mount×EN/RU×mouse/touch editor cases passed; external redirect boundary unexercised. Evidence preserved in qa.local/runtime-routing-functional/report.md.
- Retired superseded editor-state-functional and editor-state-functional-root projects after reports were saved and their routing defect was independently reproduced/fixed. Both inboxes0, active queries0; four volumes had only same-project consumers. Removed12 containers,4 disposable volumes,2 networks. Removed the two obsolete editor-state runtime/helper image tags after confirming zero container consumers; frozen sources/manifests/reports retained, current QA stands untouched.
- History reply-generation fresh Code QA PASS (qa.local/history-reply-fresh-codeqa/report.md): independent failed-delivery→delete→fresh Bot retry→ordered render→new reply for five views, cumulative integration28.585s/bot1.220s/conversation3.755s; all1163 hashes unchanged. Immutable image build started; fresh source-blind Functional QA prepares own stand from neutral requirements.
- Escaped-entry immutable build PASS: runtime43846f5081246180f99cbb2a909784508853eab4f626b0c37dab63950989e667, worker891ad6f45eeddcea218e15cbeaff1dfdf72b952e47815a25a89eaa8ea4c14ae2. Fresh source-blind Functional QA received IDs for own HTTPS root/mount projects.
- Combined unaccepted draft received10 verified history-reply delta paths. orders.go merged cleanly against exact append-generation ancestor; history-reply-overlay.json records hashes. Live platform unchanged; combined tests and independent gates remain pending.
- Catalog Functional QA partial checkpoint has no verified defect but explicit original-scope gaps. Work continues on same immutable stand rather than accepting a narrower scope. Root supplied synthetic loopback host.exe (SHA12f03cbe8eb71e6be005628017d931afc341622a972515b8003588b10e0b4146) invoking unchanged frozen APIClient for independently injected upstream fault/fallback cases; all1091 dependency hashes reverified. This library-transport proof is separate from queued whole-app proof. Owned synthetic agent quota reset authorized for long bounded continuation; generic budgets unchanged.
- History reply immutable images built successfully: runtime99f20932af2f0ae41ff4c56a3c8a996152907e774e986d842eef87f72f06cc35, worker59ede0f3ef28431396daa6d5fa42a540f5c81448b7a7c26fe1e7d456465a73ba. Fresh history-reply-functional received IDs and neutral acceptance/setup; independent source-blind checks pending.
- Combined history/modern/catalog/prefix draft compiled all packages after reply visibility and escaped-entry overlays (compile-reply-prefix.log, go test -p1 ./... -run '^$', exit0). This executes no test bodies. Joint serial history/modern/browser-mounted PostgreSQL cases started separately; main1146 composition remains unchanged.
- Accepted public-prefix/escaped-entry scope integrated into main platform:33 changed paths, all1160 source hashes exactly match reviewed candidate89a10a812ae92f5dd80d2066088a868253673edf9efaed8ad23457b0cfe17f66. Fresh CodeQA1536 adversarial requests and FunctionalQA120 HTTP observations plus8 root/mount×EN/RU×mouse/touch cases PASS; external redirect passthrough has CodeQA composition evidence but no published whole-runtime trigger. Earlier unchanged-editor656-case evidence retained; no full-product/production acceptance inferred. Integration plan checks all1146 predecessor hashes before copying and all1160 afterward.
- Updated deployment documentation's stale media-broker credential description to match already integrated credit accounting configuration: dedicated zns_meter and accounting.password, no general bot/history DML; production still unauthorized. Verified actual compose/init-roles/media.env files before updating.
- Escaped-entry-functional owner cleaned both completed disposable projects and retained report/evidence. Main1160 integration does not change any currently frozen catalog/history/importer QA candidate.
- Combined unaccepted history/modern/catalog/prefix draft serial PostgreSQL stage terminal PASS (session38111 exit0; joint-reply-modern-prefix-pg.log). Test selection matches47 top-level history/modern/script-modern/browser-mount tests. This is affected-composition evidence, not full-suite or independent acceptance. Draft1214 manifest9d01c04c6023149446044a05cce23f198ff73de406de0f5dc16a2163ddc839c4 recorded; archive discovery successor still outside this draft.
- Retired superseded runtime-routing-functional/root: owner verified empty inboxes/no active queries/no outside volume consumers, then removed12 containers/4 volumes/2 networks and stopped own HTTPS proxy PID11032. Six assigned ports released; cleanup.md retained. Root independently verified no container users and removed only obsolete zns-public-prefix-runtime-routing runtime/helper tags. Current accepted escaped-entry images retained.
- Retired obsolete modern-history-fqa after current catalog QA confirmed no dependency. Original F1 failing catalog SHA41482d9cb174f06541c62bdec6c9d3dded8ddc3ac6b7ba963b95ae080a835487/report preserved. One abandoned reproducible inbox item70 remained (260 JSON-text bytes, MD5 fingerprint51e00e3a4d187ffb7f50f232265eccb7); no active DB query, volumes had only project-local consumers. Removed6 containers/2 disposable volumes/private network. Verified no remaining image consumers and removed two obsolete modern-history-prefix tags. Current catalog-orders-functional untouched.
- Combined draft units and vet PASS. Full lint found duplicate create literals across merged orders code; replaced two order-operation literals with existing actionCreateOrder, no behavior or suppression change. Full lint rerun active; original failure retained in joint-lint.log.
- Combined draft full pinned lint rerun PASS0 (session28945 exit0), with units/vet and serial PostgreSQL277.658s already passed. quality-sha256.json captures two value-identical constant substitutions after the PG run. Main1160 unchanged; archive successor and independent composition acceptance remain pending.
- History reply-generation Functional QA PASS tested scope (qa.local/history-reply-functional/REPORT.md): actual queued remote/external Sobek, EN/RU rendered five reply surfaces, DeleteContent/held-provider/failed-delivery/restart privacy, full Unicode bounded retrieval and owner isolation, file/photo omission. Explicit limits remain: synthetic large-body fixture, no real provider/Telegram, exhaustive domain effects or broader media acceptance.
- Integrated52 accepted history paths into main with no overlap against accepted prefix. All1160 previous hashes checked before edit; resulting1177 source hashes verified and recorded in live-history-prefix-sha256.json. Standalone history1163 and prefix1160 candidates remain unchanged. Updated conversation-history/messages-migration contracts to actual full-body and generation semantics; Q2 real retention unchanged.
- Independent composed-history/orders CodeQA started on immutable draft1214 quality manifest95ace90a6bee889b2d47bfb55d37de1011288746253548c6c491ab7645713888. Separately, archived discovery1170 successor passed builder gates including sqlc/PG57.838s/Linux race75.581s; fresh independent CodeQA started. Neither modern orders nor archive successor accepted as integrated yet.
- Fresh archived-import-functional assigned setup of a private clone of preserved importer_removal_synthetic_free_green with local Zitadel and retained resources. Source DB stays immutable, no importer/archive mounted into runtime; current archived CodeQA/image freeze gate remains required.
- Completed history-reply-functional owner cleaned7 containers,2 volumes and own network/process listeners after release. Inbox empty, cursor73 fully consumed, faults cleared, active queries0 and no foreign consumers verified. Report and accepted images retained.
- Composed history/orders CodeQA rejected draft1214: independent Sobek/PG probe read and committed deleted private history via an uncommitted cross-turn choice under cleanup backlog. New composed-choice-history-privacy1218 candidate (manifestb389873237a838d129418877f11986afa46daf630a343c494d1e709aa1f5575b) passed exact RED→GREEN, per-use/claim/atomic Core generation controls, committed/manual preservation,53 integration cases229.120s, lint0 and Linuxrace39.056s. Fresh independent CodeQA started; main1177 unaffected.
- Archived discovery1170 fresh CodeQA rejected cached private passes.get after event reopening and booking removal. New access-provenance1171 candidate (manifestfcd4931dfb641689d9720c387b29d69e95bd8d83ab18a7c2f5f3b6271fd2e126) uses saved booking authority independently of current calendar; focused/expandedPG50.279s, units/vet/lint0/Linuxrace87.042s passed. Fresh independent CodeQA started. Imported-state FQA setup ready: immutable source DB copied, four real local OAuth identities verified, no runtime acceptance yet.
- Catalog FQA extended scope now covers732 history pages/92 turns across restart, actual queued OpenAI mock selection→planning grant/revoke, exact export/restart uncertainty, four rendered MiniApp modes, huge extra/dish references, depth128 and stale/queued-duplicate/reconciliation cases. Apparent secondary-event failure was resolved by public call shape: event belongs to begin; subsequent choice_ref reads work and commit to original event. Missing operation-specific help is retained as usability follow-up, not a data-flow failure. Shared64KiB non-order route contract supplied for final check.

## 2026-09-27 — full modern catalog Functional acceptance and archived context review

- Independent catalog/orders Functional QA completed PASS for all three supplied synthetic scopes. Final shared decoder proof: authenticated language PUT accepts 65536B, rejects 65537B without persisted change. Secondary-event draft read/quote/create passed with operation-correct arguments; remaining help clarification is non-blocking. Report: `qa.local/catalog-orders-functional/report.md`. Integrated composition acceptance remains separate.
- Fresh archived-access Code QA reproduced private derived script output surviving owner booking revocation after direct RegistrationAction context, despite the direct read becoming forbidden. Candidate1171 remains frozen and rejected; successor owned at `qa.local/archived-pass-derived-context`. Independent evidence: `qa.local/archived-origin-fresh-codeqa/independent-probes.log`. No production impact claimed or production changes made.

## 2026-09-27 — composed order draft generation Code QA

Fresh independent Code QA PASS for immutable1218, manifest `b389873237a838d129418877f11986afa46daf630a343c494d1e709aa1f5575b`. Focused PG/full external-worker restart cases310.795s; exact independent forged-authority/Core-validation probes8.053s. All1218 hashes unchanged. Report: `qa.local/choice-generation-fresh-codeqa/REPORT.md`. Runtime/worker build started for separate source-blind Functional QA, project `choice-deletion-functional`; no main integration or full acceptance claimed.

Catalog Functional stand retirement: owner verified and removed6 containers,2 private volumes,1 network and observer. Ports18417–18421 released, no active consumers or queued work. Immutable images and32MB filesystem evidence retained; ephemeral proxy private key removed. Evidence: `qa.local/catalog-orders-functional/cleanup.json`. Shared services and other projects unchanged.

Composed-choice1218 Docker builds and before/after source verification passed. Runtime `sha256:5c2d7de45ab5f238d1968ec47c9d30c4cac39550b684e983e0869903e5ee1fdc`; worker `sha256:28a82eddda64dbe732bced727202a03e5f58feeb4fdcf40b080923bebbaee5c3`. Supplied to fresh source-blind `choice-deletion-functional`; QA pending. Operation-specific event argument documented in `docs/agent-scripting.md`; runtime help clarification remains queued for next composition, frozen sources unchanged.

Stage B concrete implementation plan published as `docs/architecture-stage-b-plan.md`, linked from the required architecture plan. Existing order-extras scenario, composition/client/plan ownership boundaries, precise crash/restart proofs and archived-pass collision constraints are documented. Separate owned baseline-test preparation started; structural implementation has not started. Source snapshots remain immutable.

Independent memory/knowledge composition Functional QA started on a separate owned stand with the same frozen1218 images. Its scope includes progressive reads/search, private isolation, scoped moderation, event precedence, deletion/replay and EN/RU rendered flows. Initial observations are not final acceptance. Proposed orders integration dry-run verified1177 live paths,1218 candidate paths and64 changed/added paths, with zero removals; no live files copied.

## 2026-09-27 — architecture baseline and broader registration authority

Stage B isolated baseline adds one test file with six focused order cases: three durable fault windows and saved version/ACL/history fences. Focused native/PostgreSQL run passed8.477s; product files unchanged, package lint still pending. Existing generic inbox and successful duplicate tests did not prove these order-specific windows. This establishes baseline evidence, not structural acceptance.

Archived registration dependency investigation also reproduced derived privileged queue data surviving admin revocation (1.250s). The same narrow dependency binding was extended to current view/target authorization; exact GREEN1.282s and final direct-context matrix7.863s. Expanded PG/units/vet/lint/race and fresh independent QA remain required on the final successor; earlier owner-only gate results are insufficient for this extension.

## 2026-09-27 — independent memory composition Functional acceptance

`qa.local/memory-composition-functional/REPORT.md`: PASS representative synthetic scope on immutable1218.24 API assertions plus real queued provider/external Sobek and rendered EN/RU mouse/touch. Complete14500-character API and7280-character script reads,23-result navigation, owner/query cursor isolation, scoped classification/human review/no self-review, live revocation/hidden help and restart passed. Additional boundaries: shared generation0→1 invalidates retained secret output while private generation4 stays unchanged; deleted shared revisions/receipts scrubbed, replay cannot resurrect. Literal and regex searches over119 documents follow an empty/incomplete first100 candidates to sole later match, API and Sobek. Limitations: real models/Telegram/physical touch, exhaustive races/approval races/worst-case pages not claimed. Project-scoped cleanup requested; current images retained for other active QA.

Architecture baseline final: six focused cases PASS8.477s, pinned integration-package lint0 issues. All1218 product/base paths unchanged; one added test produces1219-path test-only candidate, manifest `6bd0f7e92bafef5fed1e47c28b5b6352097d20c9fc2c9ae3c6ac3768902a9c08`. In-process fresh Bot against durable PG is explicitly not an OS-kill proof. Evidence: `qa.local/architecture-stage-b-baseline/report.md`.

Memory composition stand retired after acceptance:7 owned containers,2 private volumes and network removed; no pending inbox or active DB work. Ports19517–19519 verified free. Current frozen runtime/worker images and all filesystem evidence retained. Report cleanup appendix and `cleanup-ports.json` record isolation checks; no shared-service changes.

## 2026-09-27 — accepted modern composition integrated and readiness reassessed

Fresh source-blind choice-deletion Functional QA PASS: backlog retained stale payload but denied all use, owner/permission isolation, deletion across held provider/restart, preserved committed/manual orders and rendered EN/RU. Limitations are explicit in `qa.local/choice-deletion-functional/REPORT.md`; exact entered-Core deletion lock/replay boundaries are supported by separate Code QA/PG evidence, not inferred from UI. After gates and both reviews, integration script applied64 changed/added paths, zero removals. All1218 main files match the immutable tested source; `live-modern-composition-sha256.json` saved. No redundant test rerun on identical bytes. Whole-product/architecture acceptance remains open.

Readiness reassessment replaced stale candidate19–23 assumptions with current accepted scopes and remaining gates. Same functional weights yield91.25% implementation/75.5% confirmed; publish rounded90%/75%, ranges85–95/70–80. Architecture B–E remains unimplemented, so these are not whole-goal or release percentages. Remaining whole scope estimate35–65 engineering days (low confidence), including architecture18–30 and separate defect reserve. No calibrated AI calendar or monetary estimate asserted. Q2 remains only the real-history policy decision. Detailed evidence/method: `docs/readiness-estimate.md`.

Archived direct-context successor1175 frozen, manifest `45556f9c649e54f8fae0e1e20edf690acc5851308f13372b74e694db518cc147`; local gates incl Linux race100.788s green. Fresh independent `registration-context-fresh-codeqa` assigned lane1 with original neutral contracts/source only. Not accepted or integrated yet.

## 2026-09-27 — systematic registration tool authority and parallel B1

Fresh registration-context Code QA independently reproduced a privileged script-tool queue result surviving administrator revocation on1175. Direct registration context was denied but saved Script.Runs still disclosed private queue content. Evidence: `qa.local/registration-context-fresh-codeqa/probe.log` and owned independent probe. Candidate1175 remains unaccepted/immutable. New successor `qa.local/archived-pass-tool-context` must inventory and cover registration read authority classes rather than only a queue method name; no production mutation.

Accepted orders/history1218 permits isolated architecture step B1 (ready service composition), owned at `qa.local/architecture-stage-b-composition`. The implementation sequence was refined to advance this independent boundary while archived privacy is fixed. No client/saved-plan/execution moves until the accepted shared baseline is composed. No source ownership overlap; pending pass-navigation adapter overlap must be reconciled explicitly. Baseline six restart/fence tests carry forward. This starts implementation, not Stage B acceptance; readiness percentages unchanged.

Choice deletion Functional stand cleanup completed:7 containers,2 private volumes,1 network removed; ports19417–19419 verified free. All83 filesystem evidence files and runtime/worker/helper images preserved. Root accepted the report before cleanup; no shared service or product source changed.

After accepted1218 integration, retired four superseded history-reply/public-prefix runtime/worker image tags. Each exact digest was checked with zero container consumers immediately before non-force removal. Current composition runtime/worker and DeleteContent helper were reverified retained. Source snapshots, reports and evidence preserved; no global prune. Inventory: `qa.local/integration-plan/retired-history-prefix-images-20260927.json`.

## 2026-09-27 — B1 Code QA and frozen functional candidate

Isolated B1 service composition passed fresh independent Code QA with no actionable findings. All1222 frozen hashes and44-path delta verified;12 focused real-PG integration tests passed15.698s, including six durable order restart/fence cases, alongside API/cmd tests. Assertions in adapted existing tests were preserved. This accepts the code-review scope only, not whole Stage B. Evidence: `qa.local/composition-b1-fresh-codeqa/report.md`.

Root built frozen runtime `sha256:b6e641c210cac96677dccdada5a36577b9127a7035fd6e72515b7946eb9eec78` and worker `sha256:8b5932e1ec2014b6a1a7a36ea01d3c6cfaea848c43d32ba0bca261c2a1eaf508`, checking source hashes before and after. Source-blind Functional QA received only acceptance/wire contracts, setup infrastructure and exact image IDs. Its owned stand will exercise combined and split modes. No functional result is claimed yet.

Prepared `qa.local/integration-plan/integrate-b1-composition.ps1`. Dry-run verified all1218 live and1222 candidate paths,44 changes and no removals. Apply requires a root-reviewed acceptance record for this exact manifest; no main-source files copied. Pending archived registration fix remains separate; client/plan/coordinator moves are still deferred.

## 2026-09-27 — retired superseded imported-runtime stand

Source-blind imported-runtime QA revalidated its own3-container Zitadel stand, four active linked humans, private clone, retained75,025-character body and five resource bindings. No importer schema exists in the clone. It confirmed no dependency on the old imported-free-fqa project; adapter/config copies are owned separately. Checkpoint: `qa.local/archived-import-functional/HEALTH-CHECKPOINT.md`. This is setup evidence, not runtime acceptance.

Root inspected every old project container label, volume consumer and network member, then stopped/removed9 exact containers,2 private volumes and the project network. New identity stand and healthy shared PostgreSQL remained running. Removed the now-unused exact runtime/worker images `zns-history-runtime-restart:frozen` and `zns-history-runtime-restart-script:frozen` after digest/zero-consumer recheck, without force or global prune. Current composition/B1/helper/toolchain and shared base images retained. Records: `qa.local/integration-plan/retired-imported-free-resources-20260927.json` and `retired-imported-free-images-20260927.json`. Filesystem reports, frozen source and imported shared-PG baseline/clone retained.

## 2026-09-27 — registration tool-context successor frozen

Successor1180 is frozen at `qa.local/archived-pass-tool-context/source`, manifest `643126b0470f32a0ed90749b6305c3bf3d29779b34dd5181bb97e23e798a2308`. Immediate12 paths and cumulative48 paths against explicit1154 baseline; predecessor1175 unchanged. Builder reports exact RED/GREEN, expanded current-authority/invitation/effect-receipt/outage checks, PG, units, vet, pinnedlint0 and Linux race111.062s. Evidence/limits: `REPORT.md`, `verification-summary.json`, `ACCEPTANCE.md` in that candidate directory. Local results are not independent acceptance.

Fresh `registration_tool_authority_codeqa` assigned source/deltas and original neutral contracts only, no builder logs or prior findings. Root builds frozen runtime/worker in separate lane2 while Code QA uses lane1. Imported-runtime Functional QA prepares owned adapters and clone independently; no source/reviewer reports provided. Main remains accepted1218 and B1 Functional QA remains active.

Registration1180 immutable images built successfully with before/after source hash verification: runtime `sha256:1da064ae46951f71a777fd2492ecdc2e7e496235f6ec7cc492ce595523ddc803`, worker `sha256:3fdb37ac8f2a93123b9aa820387673879e388f5aea71153a0eba1cf669650091`. Exact IDs supplied to the independent imported-runtime Functional reviewer. Root lane2 released; code and functional gates may proceed concurrently on the frozen candidate. No acceptance claim or main integration yet.

## 2026-09-27 — B1 accepted and integrated; invitation successor required

B1 Functional Senior QA PASS in `qa.local/composition-b1-functional/report.md`: combined and split runtime; explicit seeding; EN mouse/RU emulated touch manual→queued provider/external Sobek→Mini App→manual; service-only auth/provenance/summary/assessment, current domain ACL, stale controls, named legacy bot77 binding and remote name adapter. Explicit delivery503 followed by restart preserved one domain effect/current cards. Real providers/Telegram/Zitadel and whole StageB/product remain excluded; report documents exact limitations. With fresh CodeQA and local gates already passed, root accepted B1 only and integrated44 paths. Every1222 main hash matches the reviewed candidate; acceptance record, before copies and final manifest are retained under `qa.local/integration-plan`. No redundant execution on identical integrated bytes.

After report acknowledgement, B1 reviewer retired8 owned containers, private pg/ipc volumes and network; ports19617–19619 free. Immutable images and filesystem evidence retained for composition.

Fresh registration tool CodeQA rejected1180: bounded invitation authority omitted registration creation identity, so replacement at the same version could authorize a previous private snapshot through two invitation reads. Independent PG probe and final report: `qa.local/registration-tool-authority-codeqa/REPORT.md`. Existing focused checks passed; all frozen hashes stayed intact. A separate successor `qa.local/archived-pass-invitation-identity` is assigned for exact reproduction and correction; local results or broad current1180 Functional observations cannot close this finding.

Root prepared `qa.local/composed-registration-b1/source` as a draft only. Cumulative1180 overlay adds26 paths to B1, with four intersections: three automatic merges and one explicit union of modern-choice fields plus PassRead in scriptToolRecord. A new integration test needed only runtime.NewServices construction adaptation; assertions preserved. All packages compiled except that caller on first attempt; corrected integration compile-only run passed. Clarified begin-only fields in orders.choice help in this draft. The draft depends on rejected1180 and cannot freeze/be accepted/integrated until the reviewed successor replaces it and combined gates pass. Main and all frozen candidates remained unchanged except the accepted B1 integration.

## 2026-09-27 — independent final-response finding and bounded B2 draft

Imported-runtime Functional QA independently held the final provider request after an authorized passes.get on a synthetic historical booking, removed only that private booking, then released the old-text answer. The workflow card rendered the revoked comment (verified after Markdown escaping normalization). Local Zitadel, actual queued provider and external Sobek were used; original imported rows/rights/event flags remained untouched. Evidence: `qa.local/archived-import-functional/LATE-REVOCATION-REPRO.md`, `late-revoke404.evidence.json` and screenshot. Candidate1180 remains rejected. Builder's invitation identity exact GREEN and expanded PG/unit/vet are separate evidence; they do not fix this final-response boundary. Freeze is held while typed authority provenance and late/cached output fencing are designed in the same unfrozen successor.

B2 preflight verified accepted main1222 and inventoried124 client receivers/30 constructor matches, including elided auth-test literals. Bounded client extraction has no product-file collision with current registration work if lower-case forwarding signatures and concrete domain DTOs remain; one history-cache test constructor needs an explicit merge. Root allowed isolated draft work at `qa.local/architecture-stage-b-client/source` with baseline native checks and lane2. Registration owns lane1/sharedPG. No saved-plan/coordinator move or B2 acceptance before final privacy composition. Evidence/map: `qa.local/architecture-stage-b-client-preflight/REPORT.md` and inventories. This refines scheduling from observed dependencies without dropping any gate.

## 2026-09-27 — imported-runtime evidence retained, fresh successor QA prepared

Independent `qa.local/archived-import-functional/REPORT-1180.md` completed NOT ACCEPTED due the late-completion privacy finding. Passing scope includes actual local Zitadel exchange, queued provider/externalSobek, representative EN/RU rendered flows, complete75,025-character history reconstructed across19pages with exactSHA, allfour original proof blobs, free/paid truthfulmetadata, current24062-byte receipt upload/callback/owner-onlydownload and two app+worker restarts. Retained imported rows matched except expected reminder_claimed_at bookkeeping. Food history/draft UI and wider authority matrix remain expressly untested. The report does not accept a successor.

Root released the obsolete1180stand/ownedclone for scoped cleanup after builder reproduced the issue; baseline importer_removal_synthetic_free_green and filesystem evidence remain protected. Fresh source-blind `imported_runtime_fresh_functional` prepares a distinct clean clone/localIdP using only the neutral public-setup subtree. No prior reports/findings/source provided; final images still pending.

B2's21 focused shared-PG cases passed28.146s, including MiniApp/food/timetable/delegated identity/large transport and six saved-order restart fences. PG was explicitly returned to registration builder; B2 continues native-only lane2. These are builder results, not architectural acceptance.

After acceptedB1 replacement, root retired two unused modern1218 image tags, each exactdigest/zero-containerconsumer checked immediately before non-force removal. CurrentB1 images, DeleteContent helper, toolchain and all sources/reports retained. Record: `qa.local/integration-plan/retired-modern1218-images-20260927.json`. No globalprune.

## 2026-09-27 — B2 frozen; fresh Code QA started

Root verified all1228 B2 source hashes against source-sha256.json (21994c621c0d2a43be5844ef20fd1eb9ddcb075253cefc49d61e21318a00de60); delta9added/10changed/3removed, fc9d819d235321ce8919656e9f297c1b7241d3686ca8f4c4c30fa5f0f29f7e52. Native focused compile/tests, vet/build and pinned lint completed successfully; earlier PG21 cases/six restart fences passed. Constructor-only tests restored to B1 with Go1.27 promoted literals; the previous history-cache constructor collision is absent. Fresh independent b2_client_fresh_codeqa owns qa.local/b2-client-fresh-codeqa, native lane2 only while privacy builder retains sharedPG. No B2 main integration or acceptance claimed.

Archived-import-functional cleanup completed: eight containers, two private volumes, one network and its private DB clone removed; checked ports free. Preserved baseline importer_removal_synthetic_free_green, images, neutral setup and report evidence. Record: qa.local/archived-import-functional/CLEANUP.json.

## 2026-09-27 — composition preflight and broader privacy regression gate

Root added read-only qa.local/integration-plan/check-b2-overlay.ps1 and executed it against the existing registration+B1 draft. All22 frozen B2 delta paths are clean overlays, with full1228 source hash verification. Report b2-overlay-preflight.json is text/hash compatibility only, not final-successor compatibility or acceptance. Neither draft source nor main changed.

The privacy successor's expanded PostgreSQL gate failed in124.920s (pg-late-expanded.log): existing revoked-script/direct-context continuations and system fallback/profile error notices regressed. New late-response/replay/render probes passing does not close these failures. Builder is repairing semantics without weakening assertions; successor remains unfrozen. SharedPG released from terminal gate67194 to fresh B2 Code QA for focused integration. Root also requested checking missing saved-plan provenance for old model replies while preserving authoritative/manual receipts.

## 2026-09-27 — B2 Code QA PASS and draft composition compile

Fresh qa.local/b2-client-fresh-codeqa/REPORT.md passes scoped B2. Reviewer verified both full source manifests before/after; candidate and B1 passed native checks and the same eight real-PG cases without skipped integration cases, including a1,497,923-byte catalog. Focused pinned lint/vet passed. Real Zitadel, rendered Functional QA, race and final privacy composition remain outside this code review.

Root applied22 hash-checked B2 delta paths only to qa.local/composed-registration-b1/source (including three explicit old-client removals); main remains accepted1222. All draft packages compile via go test -p1 -run ^$ ./... (session93479 exit0); no tests executed by that compile gate. Old1180 registration parent remains rejected: this is not final privacy composition or acceptance. Root lane2 released; sharedPG returned to privacy builder after reviewer release. Neutral client-functional-acceptance.md defines eventual rendered EN/RU manual-agent-MiniApp, auth, food/timetable, large transport and restart proof without earlier findings.

## 2026-09-27 — composed native gate and saved-plan preparation

Root B1+B2+1180 draft passed go test -p1 ./cmd/... ./internal/... with no TEST_DATABASE_URL (session93126 exit0, b2-composed-native.log). This checks native composition only; no final privacy or PostgreSQL acceptance. Read-only provisional comparison of the changing privacy successor against1180 found seven both-sided source differences in bot.go/history_capture.go/knowledge_cards.go/markdown.go/orders.go/pass_agent_execute.go/profile.go. Recompute at freeze; no source overlay from the changing successor was applied.

The prior B2 builder now owns only qa.local/architecture-stage-b-plan-preflight for a read-only concrete B3 saved-plan ownership/callsite map. Product movement remains deferred until accepted privacy composition. Privacy builder retains exclusive sharedPG for repaired gates and is checking missing saved-plan provenance without suppressing manual/system/committed-action receipts.

## 2026-09-27 — repaired privacy regression gate and composed acceptance sequence

Builder collected session37129 exit0: repaired pg-late-repaired.log integration70.382s. Script/pass-plan, authorization-after-history-read and unavailable/profile fallback checks passed with existing assertions preserved. Missing saved-plan and archive-boundary probes remain before freeze; this is not independent acceptance.

Root refined acceptance scheduling: freeze the privacy successor, merge its delta into the existing B1+B2 draft, run affected combined gates, then fresh composed Code QA and source-blind Functional QA against exact combined images. Avoid building/reviewing an obsolete standalone privacy runtime only to repeat on B2. The independent functional reviewer received only the neutral client-functional-acceptance contract in addition to original imported-runtime requirements; setup uses its existing private stand and preserves imported evidence. Both reviews and all required scopes remain mandatory before main integration.

B3 plan now explicitly includes nested authority identity data and scalar JSON readers (latestNotice, resumeConsumedVoice), distinct terminal policies and nil/empty evidence semantics identified by read-only preflight. Extraction remains deferred until accepted composition.

## 2026-09-27 — expanded privacy gate PASS; final legacy classification

Builder collected session16944 exit0: expanded2 PostgreSQL gate128.395s passed, including archive-boundary membership removal and missing saved workflow-plan rendering. Final legacy reply classification remains in progress; no freeze or acceptance yet. Missing provenance must not be inferred safe solely from native_markdown=false. Proven manual/system/structured action results stay authoritative; unclassified agent-origin legacy output is conservatively hidden.

Functional setup author disclosed reading docs/browser-auth-files.md, an implementation file map, before any acceptance execution. Root retained the useful isolated setup but reassigned that agent to neutral setup only. A fresh source-blind reviewer will perform final composed acceptance after explicit stand-writer transfer; no duplicate Zitadel/DB needed and no prior findings supplied. Original acceptance scope is unchanged.

Root prepared hash-checked compose-privacy-successor.ps1 for an explicit frozen manifest and1180 common ancestor. It is parsed but not executed; main and frozen candidates unchanged.

## 2026-09-27 — Q2 resolved: full personal assistant history

Daniel chose the entire available old history, without a time cutoff, and confirmed it contains only personal correspondence with the assistant, not other bot actions. He states it predates agent mode and has no secrets/private profile fields. Updated messages-migration-contract, forward-migration, PROGRESS and readiness estimate: retention is no longer a pending product decision. Preserve stored owner/role/source time, complete bounded bodies and snapshot-bound identity/clock resolutions. Do not synthesize buttons/payments/actions as missing assistant history. General secret/media safeguards remain; this decision itself neither executes real import nor authorizes production cutover. Readiness percentages unchanged because acceptance work remains.

## 2026-09-27 — explicit full-period importer policy implementation

Root inspected frozen202 message policy: retain_from/retain_until are mandatory and temporal exclusions are supported. User's resolved Q2 warrants an explicit all-history mode rather than invented broad date bounds. Builder owns only qa.local/importer-full-history-policy/source, copied from verified202 manifest5e4b461601177bfb7228525d786e9ba37ca87c9faa2051395c9644f1ee30a691. Existing window inputs remain compatible; all mode must reject contradictory bounds and temporal exclusions while preserving owner/clock/privacy/count/replay contracts. Source work only until a heavy lane is granted. Frozen importer, original synthetic source, resolutions and preserved baseline database remain unchanged. Fresh CLI/PG/reconcile and independent QA are required; later full-period rehearsal must use fresh reviewed inputs rather than mutate a runtime stand under QA.

## 2026-09-27 — composed privacy marker precedence and full-period Code QA

Privacy successor1187 and B2 were composed in qa.local/composed-registration-b1/source, not main. Four textual conflicts preserve archive-before-persistence plus both history and current pass authority checks. Initial affected PostgreSQL run failed because validatePassReply converted a history-terminal plan without pass provenance into a pass-terminal plan on retry. It now runs the existing history-before-pass validator; visibility treats either terminal result as hidden. The existing three focused history retry/restart/manual-notice tests pass (7.329s), bot/appclient/miniapp native tests pass, pinned lint reports zero issues. Broad affected rerun and fresh composed Code/Functional QA remain required; draft is not accepted.

Full-period importer204 fresh independent Code QA passed with no actionable findings. All 16 message tests ran against real PostgreSQL with no skips (9.700s); candidate/base hashes unchanged. Source-blind CLI Functional QA now owns the exclusive shared PG lane for its isolated database. User's clarification remains recorded: source is only personal assistant correspondence, not other bot actions.

## 2026-09-27 — readiness refresh and architecture breakdown

Updated PROGRESS, readiness-estimate and architecture-refactor-plan at Daniel's request. Functional estimate is approximately92% implemented/76% confirmed (weighted92/76.25, uncertainty85–95/70–80). Full-period importer204 passed independent Code QA and CLI Functional QA; runtime/restart/model-input isolation remains separate. Architecture is approximately20% implemented/15% accepted using explicit planned weights A5/B30/C30/D20/E15; A and B1 accepted, B2 implemented with scoped Code QA, B3/B4 and C/D remain. E has partial evidence but is not accepted. PROGRESS now explains A, B1–B4, C, D and E in plain language.

Remaining workload32–60 equivalent8h engineer-days, including16–27 architecture days; previous35–65 reduced for completed B2 implementation and full-period CLI slice. Not a calendar/API-cost estimate. New composed Code QA concerns are under reproduction; expected migration-ledger table was absent in the private runtime clone, so no seed/migrate or app launch authorized yet. Overall production readiness remains unproven.

## 2026-09-27 — independent composed rejection and verified stand schema

Fresh Code QA independently reproduced both privacy defects in frozen1261 through QA-owned overlays: revoke just before archive commits private assistant text despite terminal return; subsequent model context still sees old derived text after render redaction; temporary BeforeProvider authority503 returns success and saves a fallback. Candidate rejected, immutable. A separate successor qa.local/composed-privacy-repair/source is assigned to one builder, with explicit PG lane, preserving originals/manual/authoritative results and approved legacy personal correspondence.

Read-only stand investigation resolved the missing ledger: importer rehearsal applies migration SQL directly. Original65 migration digests and exact filenames match candidate; numbering ends066 with a gap. Schema-only core/bot/credits dumps of preserved baseline and privateclone are identical. No migration/seed or DB mutation performed; proof stand-schema-verification.json. Fresh source-blind reviewer informed to await images without migration.

Full-period whole-import rehearsal preparation assigned separately in qa.local/importer-full-period-rehearsal. Original source/resolutions/baseline and active stand stay immutable; a fresh all-history resolution and isolated database will be used after an explicit PG grant.

## 2026-09-27 — obsolete Docker images removed

Read current Docker consumers, then removed31 explicitly named obsolete runtime/script tags from superseded accepted domain stages. Each tag had zero container consumers, removal used no force, all31 succeeded. Retained active stand images, B1 runtime/script, rejected1180 reproduction images, history QA host, media helpers and test toolchain. Other-project images and running containers were untouched; no global prune. Exact image IDs and deletion results: qa.local/obsolete-domain-images-removal.json.

Full-period preparation independently inspected by root: original snapshot identity unchanged, seven plans and six non-message resolutions byte-identical; only retention policy and the previously date-excluded ordinary user message change. Offline CLI validation reports all7domains/12coverage domains and zero blockers. All five removal-helper files match the accepted frozen snapshot; intended DB importer_removal_synthetic_full_period respects its existing target guard. Apply awaits sharedPG release from privacy builder.

## 2026-09-27 — full-period import/removal passed; privacy repair ownership split

Fresh importer_removal_synthetic_full_period ran all seven applies/replays and explicit reconciliations successfully. All32 domain/history/credit readbacks passed, unchanged reviewed removal helper returned removed_preserved with156 identical permanent table/sequence fingerprints, and post-removal32 readbacks passed. Connections restored; database preserved without importer receipts. Runtime acceptance must use its private clone and the new all-period resource/resolution package. Neutral source-only handoff added at qa.local/integration-plan/full-period-runtime-inputs.md; final app/worker and any required schema upgrade remain pending.

Privacy successor ownership split with explicit contracts: importer_free_pass_builder owns bot provenance propagation/outage and conversation_authority_builder owns domain authority/conversation/API/schema boundaries. Preserve authorized history and original imported correspondence, validate bounded source authority under the same transaction at authoritative history boundaries, invalidate stale derived bodies/summaries and fence downstream plans. No blanket omission replacement. Both work only in qa.local/composed-privacy-repair/source; main and frozen1261 remain unchanged. PostgreSQL lane transferred from completed rehearsal to conversation builder.

## 2026-09-27 — canonical restricted-role bootstrap proof

Root ran unchanged deployment init-roles.sh (hash equal frozenB1) on a fresh isolated PostgreSQL17 container with tmpfs/no publishedports and generated local credentials. FrozenB1 production-mode migrate completed as nonsuperuser zns_migrator; all65migrations and core/bot/credits ownership correct. zns_runtime authenticated and read conversation/inbox/credits; no schema/table CREATE. zns_meter cannot read private history/orders or update unlimited but may record credit attempts. No seed users. Privilege denials inspected through PostgreSQL privilege functions, not full adversarial app QA; new067 and complete restricted runtime acceptance still pending.

Container label verified then stopped/removed; all4generated password files deleted, no persistent volume. Evidence qa.local/deploy-role-rehearsal/REPORT.md and proof.json. Native lane returned to lead privacy builder; conversation builder retains sharedPG lane. No product code changed or production connected.

## 2026-09-27 — bounded provenance implementation review

Root inspected mutable privacy successor contract across conversation/passbooking/bot. The initial authority query scanned every non-omitted derived event on each read/append, making work and locks grow with total history. Requested bounded selection without truncating authorized correspondence. Conversation builder changed queries to selected event IDs plus independently persisted bounded summary authority; new067 adds event provenance and summary authority, originals remain unchanged. Root verified the new selected-ID query and summary column exist. Bot exposure callback now wraps authority infrastructure errors with the retryable sentinel. These are in-progress source observations, not passing gates or acceptance; both builders continue scoped tests and integration.

## 2026-09-27 — first provenance transaction gates

Conversation builder collected terminal PG evidence: three new authority cases PASS3.098s (authorized long-body retention/revocation with original correspondence intact; summary/inherited reply invalidation; concurrent booking deletion before archive commit). First attempt failed synthetic fixture FK cleanup, not product behavior; fixed fixture and retained distinction. Existing internal/conversation PG suite PASS4.372s including history-derived order deletion-lock and concurrent body/deletion barriers. That gate first exposed a cancellation error overridden by tx.Commit; commit-on-history_stale only now preserves infrastructure/cancel errors.

Source move needed to break conversation→passbooking→orders→conversation dependency: unchanged LockGeneration moved to conversation/fence with compatibility wrapper and one orders call/import change. Root verified exact function equality against frozen1261 (CRLF-normalized), qa.local/generation-fence-extraction-check.json. Both builders still own successor; bounded/grant proof, original independent reproductions, full affected gates and fresh QA remain pending. No freeze or acceptance claimed.

## 2026-09-27 — original privacy boundaries pass; clean-turn generation defect found

Lead collected focused PG72376 terminal evidence: original BeforeProvider outage and exact revoke-before-archive reproductions pass; outage recovery, history-derived late revoke and privileged target incarnation cases pass. Two cases still reject safely: pruning stale/unproven runtime history during a new turn advances the owner's history generation, but planForUpdate later assigns the earlier pre-read generation. This makes a clean next request terminal. Lead is fixing the pre-first-provider capture boundary; cached plans/receipts and already exposed model context must not be rebound. Evidence privacy-first-green.log; no test assertion weakened and no final acceptance.

Conversation-specific final gate independently collected: integration18.554s + conversation3.970s pass including bounded unrelated-source locks, grants/target eligibility, long bodies, summary inheritance, archive race, empty history and order/deletion fences. New schema candidate remains mutable; root parametrized the existing local role bootstrap proof for an immutable future image and expected migration count, parsed successfully, not rerun before final image exists.

## 2026-09-27 — original reproductions and recovery green

Focused rerun60252 terminal PASS13.072s. All three independent original failures now pass, along with direct/Sobek/derived-context outage recovery preserving effects and budget, history-derived late revocation, recognized old runtime archive, and both privileged target replacement cases. New-turn generation now comes from one bounded history Window reused for model context; cached plans and prior interaction/script receipts retain their original generation checks after pruning. Expanded affected PG gate88740 is active, sequential packages with parallel2; no restart or completion inferred from elapsed time. Final freeze/quality/Linux gates and fresh independent QA remain outstanding.

## 2026-09-27 — Full-period QA clone schema upgrade

Created isolated imported_runtime_full_period_fqa from preserved importer_removal_synthetic_full_period. Applied only067 transactionally; all156 prior table/sequence fingerprints and all32 imported-data readbacks passed. Baseline has no067 objects and remains unchanged. Clone has13 total events (8 imported correspondence +5 domain events),9 message references; no seed, migration ledger, identity rebinding or runtime start. Exact migration hash and evidence: qa.local/integration-plan/full-period-schema-upgrade.json. First harness attempt rolled back before migration because sequence rows need an anonymous composite projection; corrected and rerun successfully. This is schema/data preservation evidence, not runtime acceptance.

Expanded privacy integration gate88740 failed in387.162s; five top-level scenarios also reproduce on rejected1261, with one additional stale-takeover subcase under stricter authority binding. Builder diagnoses current contracts and actual inbox refusal delivery before adjusting tests. Vet passed; lint cleanup and fresh independent acceptance remain required.


## 2026-09-27 — Restricted-role schema067 proof

Canonical bootstrap plus frozen B1 schema and exact067 passed under non-superuser migrator in isolated tmpfs PostgreSQL17. Actual password-authenticated SQL proved runtime new-table/view reads and denied schema/table creation; meter accounting permissions succeeded while history/orders/authority/view reads and unlimited policy updates raised insufficient_privilege. Mutations used zero rows and rollback: no billed-operation claim. Exact ACL export supplied as neutral stand contract. Temporary container and four passwords removed. Evidence: qa.local/roles067-rehearsal/REPORT.md. Final frozen-image migration and full restricted runtime QA remain required.

Terminal refusal delivery now passes focused EN/RU delivery-failure/restart/inbox-consumption and unchanged-model/quota/dedup assertions. First recovery probe caught cached-plan rejection before the initial notice hook; corrected at authenticated outer Handle boundary. Final native/lint/affected-PG checks and both fresh independent QA are pending; not accepted.


## 2026-09-27 — Immutable build input validation

Frozen-image builder now compares the complete case-sensitive source file inventory before and after Docker builds, in addition to hashes. Verify-only mode passed all1222 B1 files; an isolated extra-file probe was rejected before Docker execution. Evidence: qa.local/frozen-image-inventory-check/proof.json. No product source or accepted frozen manifest changed. Restricted QA role/config preparation is ready but sharedPG mutation waits for active test gates.


## 2026-09-27 — Privacy successor PostgreSQL gate

Final affected PostgreSQL run passed: bot1.152s, conversation2.530s, integration141.414s (qa.local/composed-privacy-repair/pg-final.log). Existing exact privacy probes, authority/late-plan/history and repaired contracts are covered; full product acceptance is not implied. Pinned lint has one test-only cognitive-complexity finding, pending equivalent test structure cleanup and final native/race gates. Source remains unaccepted until both independent QA gates.


Role-proof clarification: root's container-local TCP probes supplied passwords but did not test wrong credentials, so those runs prove effective roles/SQL privileges, not password enforcement. The Functional stand owner detected local trust and verified correct-login plus wrong-password rejection through the actual host.docker.internal:55432 app connection path. Evidence: qa.local/composed-runtime-functional/restricted-login-results.json. Root role reports corrected; no acceptance percentage changed.


## 2026-09-27 — Parallel review and held1280 candidate

Built runtime/script images from exact1280 inventory with before/after hashes; image IDs and manifest binding are in qa.local/composed-privacy-repair/images/build-proof.json. Images are NOT released for FQA. Native units/vet and pinned lint passed. Linux race run ended with two time.Local versus time.UTC structural-equality test failures at equal instants; no race-detector finding was reported. Builder independently reproduced refusal loss on normal Render/reconciliation. Exact old1280 source remains unchanged for fresh Code QA. Root created verified1280 successor qa.local/composed-privacy-repair2/source for bounded fixes; bot/i18n/notice-test ownership assigned to lead builder. Code QA has separate source/report ownership and sharedPG probe slot. Runtime Stage D preflight has its own read-only scope/report, no product edits.

Global FQA team coordination during architectural refactoring is now documented in docs/code-quality.md and docs/architecture-refactor-plan.md: one lead, disjoint scopes/fixtures, exclusive shared-fault/restart barriers, combined evidence. Current scoped FQA stays single-lead; one briefly created child is held with no live actions.


## 2026-09-27 — Independent queue identity finding

Fresh Code QA reproduced P1 on held1280 with its own real-PG overlay: deleting a queue row while administrator privilege remains permits stale private content in late completion and cached script retry. Evidence: qa.local/privacy-composition-final-codeqa/queue_identity_test.go and queue-probe.log. Source remains unchanged. Successor ownership split: lead owns bot capture/notice/tests; domain builder examines exact queue/payment-queue authority identity and may reuse existing domain contract. No blanket omission of authorized data is allowed. Source review continues; both fresh final QA gates remain required.


## 2026-09-27 — Rejected image cleanup

Removed exact unused runtime/script images for rejected1280 and superseded1180 after checking zero container consumers. No force or global prune. Accepted B1, active infrastructure, toolchain image, frozen sources and reports retained. Evidence: qa.local/rejected-privacy-images-cleanup.json. Build proof1280 records successful build but removed/unreleased images, not available acceptance binaries.


## 2026-09-27 — Successor queue and notice verification

New domain queue-authority tests passed (5.833s): exact admin row identity while preserving can_book=false visibility; payment booking incarnation/current pending attempt/state changes and archive-decision lock race. Initial successor queue/reconciliation/EN-RU notice checks passed (5.087s). These are builder checks only. Root flagged a provenance risk from persisting terminal notice as SystemNotice; the new precise legacy-archive test reproduced a leak into subsequent model history (notice-legacy-red.log). Lead is replacing that draft approach with fixed notice rendering from terminal metadata, preserving provenance. Domain builder separately owns precise old queue archive classification; any required schema extension uses new068, not editing already-applied067. Candidate remains mutable/unaccepted.


## 2026-09-27 — Combined B repair and parallel FQA pilot

The accepted main platform remains the 1222-file B1 composition. Privacy and application/state/order changes are combined in isolated candidates; none of these later candidates has final Functional acceptance.

Repair3 contains 1321 frozen source files (manifest BCC45EFBBA83F90158F1415B2E47A8600FC926B59F489637A4D6442F70F4B379). Appclient preserves cancellation and sanitizes other failures; native tests and independent static Code QA passed. Timestamp equality now compares the identical instant across transport locations. Strengthened knowledge outage/revocation fixtures passed focused PostgreSQL checks. Unified lint found two test issues; successor4 owns their correction. Independent fixture review identified knowledge-only provenance loss; separate PostgreSQL reproduction confirmed it. This remains a product blocker, not an accepted fixture correction.

The earlier full Linux race gate passed 42 packages and 2283 test/subtest events but failed five leaf tests and exceeded the integration package's 10-minute deadline; 110 started events remained unfinished. No race report occurred. Source hashes stayed unchanged and owned containers/networks were removed. Evidence: qa.local/architecture-stage-b-repair/linux-gates/REPORT.md. Subsequent targeted checks do not replace the incomplete full gate.

Sobek phase measurements isolate argument serialization cost: roughly 40–48 ms without race and 131–173 ms with race for the oversized-choice case. An isolated uninstrumented race run timed out once in five attempts. A primitive-conversion overlay preserved behavior but did not improve performance and was not promoted. The production 200 ms execution limit and assertions remain unchanged.

The parallel offline FQA completeness pilot has 103 passing tests and clean targeted ESLint/Prettier; fresh source-blind Functional QA passed 67 cases. It does not establish semantic product acceptance. Final Code review assesses the explicitly documented trusted-local-storage prerequisite. All previous reports/candidate snapshots are retained, including the mapped-drive finding; physical filesystem locality is not inferred from path syntax. No active stand was changed and Go/PG lanes remained dedicated to migration.

## 2026-09-27 — Fresh combined B review and successor5

Two independent read-only Code QA sessions reviewed frozen successor4: 1326 files, manifest 823990D0041737CFD4E6A4FE39A1ED4400FACCD8EB400C4CDF6A9689D72B9C04. Both returned substantive findings. Reports remain in qa.local/architecture-stage-b-repair4/code-qa. Source4 is unchanged; successor5 is the development copy.

Successor5's initial seven-file lint repair passed focused real-PG tests (17.408s), native knowledge tests (0.146s), and unified pinned lint with zero issues. These results precede the subsequent review repairs and are not final candidate acceptance.

Four privacy findings were reproduced before product edits: a script still wrote memory after source revocation; a previously written derivative remained searchable after revocation; deleting original history left an archived derivative readable; and revocation after reading a summary batch did not prevent provider exposure. Evidence: qa.local/architecture-stage-b-repair5/causal-boundaries-red.log, terminal exit1, 3.199s. Developers are repairing these boundaries plus checking script-order generation fences, host/user capability separation, and provider authorization for queued admin delivery. A static claim of unbuildable promoted-field literals was not reproduced: Go1.27 compiled the actual candidate packages with GOWORK disabled and candidate GOMOD/package directories verified.

The full Linux race launcher now checks unexpected files as well as every manifest hash before and after execution. The exact frozen source4 inventory passed; a synthetic added Go file was rejected. Evidence: successor5/gate-integrity-check. The final full race run remains pending, as do fresh acceptance reviews and Functional QA. FQA has independently prepared 33 scenarios and 78 required cells, all pending. Developers may be reused; acceptance reviewers remain fresh and independent.

Successor5 migration069 was applied to a separate disposable clone of the imported schema068 baseline. All35 imported-state readbacks passed before and after; exact conversation-event, full-body and permanent-reference fingerprints were unchanged. The baseline was untouched and the probe clone was removed. The first launcher used an incorrect filename and its cleanup saw a transient closing connection; a follow-up verified no remaining session and completed the actual migration/check/cleanup. Evidence: qa.local/architecture-stage-b-repair5/schema069-probe. This storage check does not establish final-schema importer or runtime acceptance; further memory-provenance schema work remains pending.

## 2026-09-27 — Successor5 domain fences and runtime wiring

Focused PostgreSQL checks passed for language, model settings/grants and credit policies (session76958, integration5.743s; API0.126s), and simple registration, assignment and profiles (session59728, integration4.048s). These cover revoked-source denial, committed receipt replay, current target authorization and domain-specific concurrency. Settings also check that a denied request does not create a credit account and that replay cannot restore a manually revoked model grant. Registration checks opposing source/target event lock order. Reports: qa.local/architecture-stage-b-repair5/DERIVED-SETTINGS.md and derived-mutation/registration-profile.md.

Food/massage focused checks passed (session2265, integration3.723s and legacyfood0.870s). A later cancellation change rechecks owner/event on the locked row; its added regression still needs a rerun. Report: successor5/FOOD-MASSAGE-DERIVATION.md. These are developer checks, not fresh independent acceptance.

Host/API composition now connects eleven additional typed mutation methods and routes manual pass batches through the shared batch coordinator. CLI session68018 completed with static inspection and formatting only; no compile or runtime acceptance is claimed. Script callers and knowledge/broadcast lifetime work remain in progress. Report: successor5/wiring/REPORT.md. Intermediate batch/settings build failures caused by concurrently edited signatures are preserved separately from executed test results.

The intermediate schema070 probe passed35 imported-state readbacks on a disposable clone, but070 changed afterward to record memory origin. That probe is superseded for the current DDL. Final schema, runtime-role grants and importer reconciliation must be checked again after freeze; accepted main remains unchanged.

Pass batch checks passed (session5220, integration6.187s), including durable source binding, partial effects, manual continuation and existing batch regressions. Root's Host boundary follow-up passed all appclient native tests (chunk b3a9ac,0.154s): order/workflow command limits are enforced for both transports, local results are bounded and a mismatched derived owner is denied before execution. Reports: successor5/BATCH-DERIVATION-RESULT.md and wiring/ROOT-FOLLOWUP.md.

The causal matrix passed on repeat (session5980,4.476s), including the original four reproduced privacy failures and added publication/copy checks. An earlier authorized memory write produced no effect in session54193; isolated and full-matrix reruns passed without a product fix. Its cause remains unproven, so it is retained as unresolved instability rather than discarded. Food delivery checks exposed a read-only transaction attempting a locking permission query; after separating read and mutation authorization, manual export and domain cases passed. The broader food/script delivery matrix still has diagnostics under investigation.

`sqlc diff` against the current migration directory passed (chunk6bc7d0,exit0). Live inspection confirms both store.Migrate and sqlc use internal/store/migrations; schema.sql is an old initial snapshot and must not receive an append of later migrations. The synthetic final-stand permission supplement is prepared in qa.local/integration-plan/schema073-runtime-permissions.md; effective rights still require SQL and runtime checks after freeze. Migration squashing has not been performed.

Final focused food/massage delivery gate passed (session81698, integration26.979s): domain fences, final cancellation owner recheck, source-aware export/proof delivery, EN/RU script paths and manual CSV/retry behavior. The export restoration diagnostic showed saved generation0 versus current1 after terminal invalidation; the test now requires the old continuation to remain blocked and a fresh authorized request to succeed. The existing massage fault hook follows its new endpoint with its original assertions preserved. Evidence: successor5/food-delivery-green.log and FOOD-MASSAGE-DERIVATION.md.

Broadcast gate21790 passed41.874s, including provider-return revocation before cache/persistence and revocation of an already claimed retry, alongside the complete selected admin/broadcast matrix. Evidence: successor5/client-boundary/BROADCAST-LINEAGE.md. These remain developer checks. Follow-up audits found source rebinding on pass resume and missing commit-time source fences on ordinary non-Sobek model dispatch; both repairs are in progress. The stageB plan now explicitly enumerates these shared-source consumers without claiming the later C/D refactors complete.

The typed mutation coordinators now share their existing replay/source-fence/apply/commit sequence without a callback dispatcher. Current target authorization remains in explicit domain preparation, registration keeps its stronger event-lock prelude, and manual batch source absence remains distinct from malformed evidence. Scoped pinned lint passed with0issues (chunka8232a); focused realPG regression passed9.821s (session70123). Evidence: successor5/derived-mutation/REFACTOR.md and refactor-pg.log. Full freeze and independent acceptance remain pending.

Current migrations069–074 passed imported-state preservation on a new disposable clone: all140original-table row counts and original-column fingerprints unchanged, all35readbacks passed, no invented source authorities/model-grant receipts. Clone cleanup succeeded, baseline untouched. Evidence: successor5/schema074-probe2/REPORT.md, migration-digests.json and before/after.json (terminal1e448d). Baseline memory/proposal/media collections are empty, so this does not prove populated derived runtime behavior. The first harness attempt expected a migration ledger absent from the raw-DDL baseline; it failed before applying migrations and cleaned its clone. The corrected actual-schema preflight and successful run remain separately recorded.

Knowledge regression37500 completed with five failing top-level cases (integration78.701s), while its new causal and focused unit cases passed. Diagnosis separates two memo deletion/restart error-contract regressions from three foreign-private MemorySources lookups that previously returned an empty page. Fixes preserve the original assertions, scope definitive404 absence separately from provider/database errors, and retain privacy invalidation. These failures remain evidence until the corrected gate passes.

Saved-plan/media33488 passed its new four-domain revoke-at-dispatch cases, selection-turn media source case and selected order/replay flows, but the aggregate gate failed28.816s on a Russian registration payment-rejection scenario reporting source_stale. Target effect versus post-command menu provenance is under investigation; no full acceptance claim. A separate read-only audit also found inconsistent payment-instruction action naming and missing late source checks on order read deliveries. Those exact fixes are assigned without changing the original scope.

Five stable API wiring/test files were formatted with the pinned formatter; API package tests passed0.144s (chunk91bc44). This narrow result does not replace current candidate-wide lint or integration gates.

Exact MemorySources retry74605 passed all three foreign-private empty-page contract cases and source/projection units without changing their assertions. Two memo-script continuation regressions remain: stale preparation reports a generic failure, and delete-then-list does not yet refresh the remaining memo into context. The gate is still failed (6.639s), not partially accepted.

The registration publication failure is now reproduced precisely: TestSavedPlanRegistrationReviewPublication observed payment decision=rejected and a durable committed registration outcome before Handle returned source_stale (chunkbb473d). The repair is scoped to rebuilding a fresh authorized post-command menu from the committed outcome; menu-only and failed-command paths retain their causal checks. Independent reviews remain pending.

Pass delivery gate52674 passed8.807s after correcting two new test harness expectations: deleted history must refuse Handle, with no saved menu or export send/receipt. Gate4216 had already passed committed-payment publication, exact resumed receipts and other selected pass flows. Evidence: successor5/derived-mutation/pass-delivery.md and pass-delivery-gate3/4.log. These are developer checks; independent acceptance remains pending.

Causal worker gate56441 fixed both original memo continuation regressions without weakening their assertions. Child cancellation keeps parent persistence usable and distinguishes definitive stale sources from retryable outages; units and before/after-write cases passed. A new same-key replay assertion failed: an already terminal derived memory revision became visible through its saved operation result after restoring the source grant. Evidence: successor5/causal-worker-stop-replay-red.log (integration5.517s overall FAIL). The narrow replay repair is in progress; this is not an accepted gate.

Root updated three obsolete order fault-injection fixtures to recognize both public manual and Host-derived routes, preserving their original transaction/lost-reply assertions. The commit-boundary fixture decodes the Host envelope with the existing strict restartOrderCommand helper. Pinned formatting passed; targeted PostgreSQL rerun remains queued. An attempted unknown-commit probe failed at compilation during another owner's unfinished read-delivery patch, so it produced no behavioral evidence.

Root order fault-hook regression24944 passed all five selected top-level PostgreSQL cases (9.871s, terminalb76fe4): current admin/booking recheck, catalog race, history deletion at commit boundary, lost-reply admission/idempotence, and authorized read refresh after a lost write response. The new Host route hooks retain all original assertions. Evidence: successor5/derived-mutation/root-order-fixtures.log. No full candidate or independent QA acceptance is implied.

Root replaced the ambiguous missing-batch nil/nil return with pgx.ErrNoRows and updated both coordinator callers; RuntimeBatchState now consistently uses pointer receivers. Pinned formatting passed. PostgreSQL batch gate7177 passed5.749s (terminalaa226a): derived/manual source binding, receipt-before-source replay, grounded/interrupted/concurrent batches, rollback and EN/RU Telegram flows. Evidence: successor5/derived-mutation/root-batch-lookup.log. Scoped lint confirmation remains pending after the earlier lint snapshot overlapped these edits.

Unknown-commit reproducer now reaches actual committed effects before retry. All three cases fail recovery: own booking version changed by the successful command, source grant revoked, or source history deleted. Each retry stops at saved-plan validation without reaching Host receipt lookup. Evidence: developer terminalaa2a14, integration2.089s. A current-target-authorized, exact-command receipt-only lookup is being implemented; it must never create a new effect and must not restore deleted model prose. Missing receipts continue through the normal validation/archive path. This is a confirmed implementation defect, not a user decision.

Knowledge replay gate40718 passed3.684s. The original terminal-revision replay defect and added operation/input-evidence cases are now green, with committed receipt counts preserved. Both original request authority and returned content authority are checked; losing only a later review request's authority does not revoke unrelated proposal content globally. Existing deletion replay and manual-publication/private-draft controls also passed. Evidence: successor5/causal-replay-green.log. These focused developer checks do not replace the final combined gates or independent QA.

Current schema074 sqlc diff passed (terminal7fa874,2.124s) using the pinned tool module against successor5/sqlc.yaml and its migration-directory schema input. No generated files changed. The final frozen candidate still needs its full required gates.

Pinned passbooking lint82461 passed with0issues (terminal5ff4a8). This confirms the current missing-batch sentinel/receiver repair and pass export package cleanup. Derivedmutation lint is deferred until the concurrently developed receipt-only recovery is coherent; no candidate-wide lint claim. Evidence: successor5/derived-mutation/root-batch-lint.log.

Order read-delivery gate29514 failed14.853s. Canonical payment-instructions EN/RU and all four selected export source/grant barriers passed; proof replacement and retained delivery uncertainty controls also passed. Failures remain in proof/history setup or receipt persistence, fresh manual payment-card reopening, and the existing proof/country/cancellation script sequence. These are under diagnosis with original scenario intent retained; the aggregate gate is not accepted. Evidence: successor5/client-boundary/order-delivery-pg.log.

Root extended the existing forged/malformed request matrix to all six receipt-only routes and added actual-route credential denial checks (ordinary user, host alone, mismatched owner, inactive identity, provider outage). Focused API tests passed0.140s (terminalbdf162). The first attempt used an incorrect knowledge URL in the new test and returned404; it was corrected to the registered /internal/knowledge/derived/receipt, with no product change. This proves route wiring and envelope rejection, not full identity-provider or receipt lifecycle acceptance.

Read-only investigation of memory-write run54193 remains inconclusive. Its minimal row-count output did not capture worker, receipt or terminal-state evidence; later passes occurred before the cancellation fixes, so those fixes cannot be credited as its cause. Evidence: successor5/MEMORY-INSTABILITY-ANALYSIS.md. A bounded repeat with failure-only state diagnostics is reserved for a fixed source snapshot; the original timing/assertions remain unchanged.

Knowledge receipt gate53119 passed4.597s for the no-effect missing-receipt lookup, privacy redaction, stable operation count and causal/replay regressions. Evidence: successor5/causal-receipt-gate.log. This is domain-level proof; the complete saved-turn recovery and pending-filter continuation remain under integration testing.

Receipt route test formatting completed with the pinned formatter; the focused API rerun passed0.135s (terminal5f40fd). Runtime diagnosis also isolated the existing country-command regression: script binding populated HistoryGeneration on a command whose domain contract permits it only on create/edit. The fix must retain the separate Derivation fence on every effect; no validator relaxation is planned.

Root removed two shared literal lint findings without changing values: workflow result metadata reuses the existing workflow constant, and workflow/model-settings locale parameters share revisionParameter. Only bot.go and model_settings.go changed; exact pinned formatting passed (terminal3e9719). Behavioral checks remain in the current combined test runs; no standalone feature or acceptance claim.

Order delivery retry82235 passed all10 selected top-level scenarios in17.469s with zero skips. Proof history/version barriers, four export source/grant barriers, payment source retention through refresh/fallback and fresh manual reset, actual EN/RU canonical instructions, uncertainty/restart and the original proof-country-cancel sequence all passed. Source binding now leaves HistoryGeneration off non-content order commands while retaining Derivation for their effects. Evidence: successor5/client-boundary/order-delivery-pg2.log and ORDER-DELIVERY-REPAIR.md. A focused transient pre-send outage retry test is still pending; full frozen gates and independent acceptance remain required.

Saved-plan recovery gate38335 passed8.118s: five top-level families and14 subcases cover unknown commit, absent receipt with revoked source, command-hash mismatch, current target revocation, retryable receipt outage, saved/committed/delivery restart boundaries, resume fences and committed registration publication. Exact domain mutation count remains1 during recovery. Evidence: successor5/derived-mutation/saved-plan-recovery-controls.log. The postcommit response is reconstructed from current-authorized domain receipts; stale model prose is not reused. Knowledge pending-filter continuation still needs explicit end-to-end proof, and full frozen gates/independent QA remain pending.

Pass helper regression70952 passed its five selected tests/eight leaf scenarios in9.266s, while the overall command exited1 because vet encountered history_client.go during concurrent removal. This is test evidence, not a green combined gate. Source ownership now requires a stable file-set checkpoint before compilation; the complete candidate still needs current-source vet. Evidence: successor5/derived-mutation/pass-delivery-gate5.log.

Pre-transport outage gate36326 passed2.226s: an admitted but never-sent export is retried after a transient503, retaining original source and delivering exactly one file. Evidence: successor5/client-boundary/order-delivery-outage.log. This is separate from the already-passing ambiguous acknowledgment path after a transport attempt.

Root bounded stability probe38542 passed22.047s: the unchanged before_write/after_write causal test ran10 times (20 leaf executions), preserving original assertions, parallelism2 and VM budget. All1191 Go source/test file hashes were identical before and after the run. Evidence: successor5/causal-stability-count10.log and causal-stability-before/after.json. Current repetition is green; historical54193 remains unexplained and is not attributed to a speculative fix. Do not repeat this probe without a new failure or changed relevant code.

Root/source-owner refactoring preserved the source-lock pipeline: event/actor/domain checks still precede sorted history-owner fences; history invalidation remains after domain authority acquisition and summary locking. Root extracted only these phases, replaced byte-to-string comparison with bytes.Compare and used strconv for integer test labels. Native readsource/conversation tests passed (5d4f15); pinned lint36142 passed0issues (2a54cb). Complete selected Causal+ConversationAuthority PostgreSQL regression13993 passed8.374s (cef84d), covering origin/reader distinction, history deletion, summary/archive locks, publication, terminal replay and receipt privacy. Evidence: successor5/derived-mutation/source-lock-lint.log and source-lock-refactor-pg2.log. First attempt stopped before tests on an owned script result type mismatch; source owner corrected it and proved compilation before retry.

Receipt API/appclient/derivedmutation pinned lint77358 passed0issues (d5864a), including the current batch caller and new receipt-only routes. Current candidate-wide tests/lint/vet and independent acceptance remain pending; package-scoped results are not promoted to stage acceptance.

Knowledge lost-response filtering passed all three end-to-end cases (5cb9dd,2.658s): continue pending filtering, reuse saved verdict without another classifier call, and refuse revoked-source continuation without resurrecting its body after grant restoration. Initial failures were new fixture assumptions (authorized UI legitimately shows the proposal; a history query used the wrong column), corrected with the intended disclosure and persistence assertions retained. Evidence: successor5/causal-knowledge-recovery2.log.

JavaScript ESLint passed; initial Prettier gate43892 flagged three files. Pinned Prettier formatting changed only Compose layout, Markdown spacing/line endings in history/orders skills. The formatted disposable copy passed ESLint, full Prettier check and all4 offline FQA client tests (28069,terminal80432f); the three exact formatted files were copied into source5. Local Python FQA helpers passed6 tests (e48292). Evidence: successor5/js-quality.log, js-quality2.log and fqa-python-tests.log. Disposable Node containers removed themselves; no browser/runtime acceptance is inferred.

Full native go vet passed for cmd, internal and integration packages (session11567,terminalc125a9). Final scoped knowledge/bot/integration pinned lint35121 passed0issues. Native all-cmd/internal tests with real PostgreSQL are running as72879, and candidate-wide pinned lint as58842; neither has a final verdict yet. The test run exposed one obsolete history-cache fixture before its deletion scenario: it supplies nil instead of the required explicit source carrier. A narrowly scoped fixture adaptation is prepared, preserving all deletion and completion-race assertions; it is not yet applied while the current source is under gates.

Native cmd/internal gate72879 completed with41 passing packages,2 failed packages and19 packages without tests;1299 test/subtest passes,1 assertion failure and18 skipped tests were recorded. The bot failure was an obsolete nil source carrier/mock; the conversation package was stopped only after proving its old test barrier deadlocked against the new serialized read snapshot. The exact test child identity was verified before termination; remaining packages continued. Both fixtures were adapted without changing product behavior or deletion assertions. Focused62549 passed, then full bot+conversation with realPG passed (4cf84b; bot2.601s, conversation3.497s). Evidence: native-unit.jsonl, history-fixture-repair.log, history-full-packages.log. Skips include explicitly gated real model/Zitadel, Unix worker and FFmpeg/sticker media cases; Linux and final real integration gates remain required.

Candidate-wide pinned lint58842 found five remaining issues outside earlier scoped checks. Root corrected only formatting/constant reuse/receiver naming and extracted the listener creation from the oversized composition function; affected native package tests passed8a46fb. Full lint rerun34818 and full integration59962 are now running on held source. No full candidate pass or stage acceptance is claimed before their results and remaining gates.

Full lint34818 completed with only two findings in the new history-cache test: cognitive complexity and errors.AsType usage. Repairs wait for the integration source freeze to end. Integration59962 remains live and has exposed at least18 failed tests; failure evidence is collected in successor5/INTEGRATION-FAILURES.md. Developer diagnosis is split between script source/cursor handling, pass delivery and fixtures, and lost-response fault hooks. Original freshness, privacy and retry assertions remain required; fixture changes must prove the intended fault was injected. Module verification17456 is also still live, with two completed module checks and the third pending. These are incomplete gates, not acceptance.

Module verification17456 completed successfully (terminal64be9d): runtime, tools and sqlc module checks each reported all modules verified. Full integration59962 is still running; later failures include missed profile/model/food fault hooks and a genuine missing structural check for malformed saved pass discovery results. The latter must be repaired before the modern authority shortcut, preserving the existing malformed-result assertions. Current-source gate coverage was audited in successor5/FINAL-GATE-COVERAGE.md: the Linux race launcher does not enable isolated script-worker socket or native sticker acceptance, which require separate contained runs. SQLC vulnerability scan98855 has started; no result is claimed yet.

Integration59962 completed (69b9c4, exit1,1019.304s):1203 passing test events,53 failed events across31 top-level tests,5 explicit stand/browser skips, no unfinished tests. Full evidence and failure list: successor5/native-integration-result.json and INTEGRATION-FAILURES.md. Source thawed for three non-overlapping repair owners; a fourth developer investigates the modern-choice runtime timeout with original deadlines and bounded synthetic diagnostics. Further product findings include takeover owner-ID/Telegram-ID confusion and retaining obsolete source versions after a successful manual pass mutation. Existing original assertions remain required.

Vulnerability scan98855 failed only because sandbox networking could not reach vuln.go.dev; authorized network retry21011 passed (1f5d5e). Current runtime scan passed7499eb. Both report zero reachable vulnerable symbols; sqlc imports one affected package and runtime requires one affected module without a reported vulnerable call. Evidence: successor5/vulnerability-sqlc2.log and vulnerability-runtime.log. This is the pinned tool's reachability result, not a claim that every transitive dependency is vulnerability-free.

Required identity fuzz gate36291 passed (0b7f22,11.163s), running FuzzVerify for10 seconds with two workers and98,914 executions. Identity source was unchanged during the run; pass/menu fixture owners were editing independent packages. Evidence: successor5/identity-fuzz.log. This does not cover the pending final frozen runtime or browser acceptance.

Modern-choice diagnostic4418 failed all three leaves at unchanged deadlines (4ecb4f,24.742s). Each completed eight inspect calls and both model steps; captured stacks were in repeated source-authority validation during finalization, with no PostgreSQL blockers. A boundary-local deduplicated authority check is approved; rights must still be checked afresh between boundaries, with original per-carrier fallback for oversized unions. Report: successor5/client-boundary/MODERN-CHOICE-RUNTIME-DIAGNOSIS.md.

Combined repair regression54544 completed (7dc9a0,86.363s):37 of40 top-level tests passed,80 passing test events,8 failed events in3 families, no skips. It covered30 original failed scenarios plus10 privacy/delivery controls; the separately reproduced modern-choice runtime test remains pending. Remaining families are restored-rights pass tombstones, history-deleted pass replies, and pass card refresh/fallback cleanup. Source thawed for narrow repairs. Full lint93257 found11 issues: seven formatting locations plus duplicate source-stale literal and callback nesting; root applied pinned formatting to the seven named files (088d69). Remaining pass issues and authority batching are owner-assigned; no final lint or stage pass claimed. Evidence: successor5/repair-regression1-result.json, repair-regression1.jsonl and full-native-lint3.log.

After narrow card identity/source-fixture corrections and durable reply invalidation, plus boundary-local authority deduplication, full bot package passed400b2e (2.421s). Combined regression44858 passed (74a3d8,186.076s), all41 top-level scenarios including all31 original integration failures and10 privacy/delivery controls, no skips. All three complete modern-choice runtime variants retained their original per-update deadlines. The batching unit tests preserve unique revoked dependencies, fresh checks at each boundary and individually bounded fallback for oversized unions. Evidence: successor5/bot-repair2.log, repair-regression2.jsonl and repair-regression2-result.json. Full lint90192 now reports only formatting and complexity in the new batching test/helper; narrow cleanup remains before final frozen gates.

Native sticker gate passedabe2b6: all five tests, including the three formerly skipped native asset/WebM cases. Current Dockerfile.sticker input hashes were unchanged across build/run (10 files). Container had no network, read-only root, dropped capabilities, no privilege escalation,512MiB memory,2CPUs and128PIDs; it removed itself on exit. Evidence: successor5/sticker-build.log, sticker-native.log, sticker-source-before.json and sticker-result.json. This scoped native gate does not replace broader media or functional acceptance.

Final batching test cleanup preserved its assertions and passed native focused checks (4322b1,0.241s); exact two-file pinned formatting completed. Successor5 is frozen at1450 files: final-manifest.json SHA2563BDD5C8583BCE80A60DA85857D04748BF729E62A642B24B3DCF74051523E8755, recorded in final-freeze.json. Full pinned lint24231 is running. An independent developer is building/running the contained Unix-worker gate in its own report/resource namespace; this remains developer verification, not fresh Functional QA. No source edits are allowed during frozen gates; any necessary repair requires a new manifest and corresponding affected validation.

Lint24231 found only one leading blank line in the new test helper. Root removed that line; comparison of both manifests confirms this is the only changed file. Current freeze is final-manifest2.json SHA256 D13EDEE252FB2BFF88333AC2AF58AE8F01CA3B36CE396C6D509E3BFD9D413478 (1450 files). Complete pinned lint55433 passed0issues (f2ff11). Full Linux race54468 started with this exact manifest, read-only source and its own PostgreSQL. Final native vet/build/sqlc36358 is running; no result claimed yet. Fresh independent Code QA has begun, with separate runtime/orders, provenance/delivery and durable/history/import ownership. Functional QA has not begun; its synthetic stand refresh is being prepared.

Contained Unix-worker gate passed (e4ef81,0.47s, no skips), verifying actual isolated service/client behavior, interrupt and serialization-loop recovery, absent host globals and inspected limits. All28 worker/client inputs remained unchanged; owned containers and tmpfs IPC volume were removed (033767). Full evidence: successor5/script-worker-gate/REPORT.md. This closes the explicit opt-in Evaluate test omitted by ordinary race, not the wider Execute/Telegram functional scope.

Final native vet, command build and sqlc diff all passed36358 (1166f5). Full Linux race54468 remains live. Stand builder6310 is refreshing synthetic app/worker images and preparing a candidate-specific local DB copy while preserving retained baseline evidence. Fresh source-blind Functional QA lead received only neutral requirements/catalog and candidate identity; planning has started, execution must wait for verified stand readiness. Code QA independently verified all1450 manifest entries without mismatch and is reviewing its assigned scopes. Neither independent gate has delivered final acceptance.

### B: свежий Code QA и отдельный кандидат6

Свежий Code QA кандидата5 (1450 файлов, manifest2 D13EDEE252FB2BFF88333AC2AF58AE8F01CA3B36CE396C6D509E3BFD9D413478) завершён: один P1 в происхождении результатов привилегированных чтений. Отзыв доступа к событию A при сохранении доступа к B должен закрывать преобразованный результат и производную память. Отчёт: qa.local/architecture-stage-b-repair5/code-qa-final/REPORT.md. Исправление ведётся отдельно в repair6; исходный кандидат5 и его текущие проверки неизменны. Добавлена интеграционная проверка преобразованного результата payment history/queue с сохранённым вторым правом; запуск ещё впереди. FQA сохраняет независимость и использует отдельные synthetic fixtures; приёмка не завершена.

Полный Linux race кандидата5 завершён exit1: 2577 успешных test events, один упавший TestModernChoiceRevisionExpiryAndPatchLimits (timeout), незавершённых тестов нет, race report отсутствует. Отдельные live/browser/native сценарии пропущены в этом запуске и не считаются пройденными; native sticker и Unix worker имеют отдельные ранее записанные доказательства. Контейнеры запуска очищены, manifest до/после проверен. Доказательства: repair5/linux-final-gates/result.json, failures.txt, integrity-after.json.

Новая PG-регрессия выполнена против неизменного кандидата5 через Go overlay: 88043 / 4d93c5, exit1 за4.627s. Оба authorized controls прошли; history и пустая queue после отзыва A при сохранении B раскрыли преобразованный digest. Это воспроизводит P1, а не только статическое замечание. Доказательство: repair6/baseline-regression.log. Исправление ещё не принято.

Дополнительная PG-регрессия против неизменного кандидата5 воспроизвела сохранение утечки в памяти:59398/c53610 exit1 за2.096s. После отзыва конкретного payment-права сохранённый производный документ остаётся читаемым и новая производная запись ошибочно принимается. Тест использует фактические ReadAuthorities реального script callback, а не вручную придуманное право. Доказательство: repair6/baseline-memory-regression.log. Исправленный кандидат6 ожидает целевого запуска.

Кандидат6: целевые readsource/bot unit прошли; привилегированные, practitioner и causal PG-проверки завершились PASS22.072s (16312/418b90), включая новые платежные регрессии. Аналогичные food/massage/event-list сценарии добавлены отдельно и ещё проверяются. Исследование race-timeout подтвердило исчерпание200ms до host callback при race-инструментировании:1024 элемента ~201ms timeout; normal1024/1025 callback82/73ms. Лимиты не увеличены, выделяется точная decoder-проверка с сохранением небольшого end-to-end потока.

Подготовлен полный повтор импорта schema074 в qa.local/importer-schema074. Offline CLI verify/stage/plan/validate прошёл для7доменов, conversion blockers0; applied=false. Новый bootstrap требует настоящий zns migrate и точное совпадение73записей schema ledger, вместо ручного выполненияDDL. Добавлены10проверок отсутствия выдуманного происхождения поверх исходных35. PostgreSQL apply/removal и runtime этого повтора ещё не выполнены. FQA-стенд получил helpers, voice и actual-local-Zitadel onboarding; оператор снял общий setup barrier для независимых сценариев.

Кандидат6: расширенные domain-regressions завершились PASS13.297s (71664/aa832e). Проверены food/practitioner controls и revocation, event-list, сохранённая производная память, latewrite и same-event practitioner isolation. Первый запуск остановился на FK в synthetic fixture удаления specialist; fixture исправлена удалением зависимых тестовых bookings/work перед отзывом. Первое падение сохранено отдельно; продуктовые assertions не ослаблялись. Общие gates и свежие QA ещё впереди.

Независимый FQA сообщил о повторяемом result_limit для tools.$list() в полном runtime-стенде. Замечание передано разработчику для read-only установления механизма и минимального исправления; пока не объявлено устранённым. Прототип контроля DB commit acknowledgement готовится отдельно от активного стенда на pgproto3; подключение возможно только после собственных проверок и независимого review.

Discovery проверен по независимым входам:71имя занимает1325UTF8bytes и возвращается полностью. Неудачная обёртка возвращала6000символов при лимите конечного результата4096bytes; host callback limit64KiB не исчерпан. Изменение продукта не требуется, исходные наблюдения сохранены. Кандидат6 frozen1459files, manifest7f7e8b8790ff19ee89629806e8af7cb01bc8a5768a116a21b428f657511fef3e,19изменённых/добавленных путей относительно5. Свежий независимый CodeQA stage_b6_fresh_code_qa получил только требования/исходники/дельту, без прежних findings. Native lint/vet/build handle56467 выполняется; source6 не меняется.

Полный schema074 импорт/removal завершён PASS: новая importer_removal_synthetic_schema074, canonical zns migrate+replay под отдельной non-superuser ролью,73реальных schema receipts,7apply/7zero-write replay/5explicit reconcile,45readbacks до/после. Remover сохранил161fingerprint включая schema ledger; его временная схема отсутствует. Учётная запись migrator отключена и password очищен. Original inputs byte-identical. Отчёт qa.local/importer-schema074/REPORT.md. Это storage proof, не runtimeFunctional acceptance.

Независимый CodeQA6 завершил19path review и5целевых unit tests без findings. Linux race decoder1024/1025 и изменённый PG сценарий выбора заказа прошли, инфраструктура удалена. Общий lint выявил15замечаний форматирования/test conventions; исправляются после остановки всехчитателей. Промежуточная ошибка type-elision в тесте сохранена и исправлена; никаких runtime limits/suppressions не изменено. Текущий repeatlint/vet handle27369, завершение ещё не подтверждено.

Свежий CodeQA6 подтвердил manifest2 и lint-only delta без findings; затем оставшееся golines условие разбито только переносами строк. Manifest3:247f01d4152d7089b4e438e7a42149ffb33daed8e6c9ce58dcdd7ad677c8237f,1459files; отmanifest2 отличается только internal/readsource/authority.go. Повтор lint/vet12983 выполняется. Подготовлен runtime handoff новой импортированной БД в qa.local/importer-schema074/RUNTIME-SETUP.md; исходная БД остаётся reference, runtime должен использовать отдельный clone. Независимый schema074_import_review проверяет полный import/removal evidence read-only. FQA5 продолжает независимые источники/историю/эффекты и ещё не завершён.

Уточнение независимого import-review: applied=0 на7replay означает отсутствие новых импортированных записей, не отсутствие любых SQL writes. Events replay обновляет временный receipt snapshot теми же значениями. Прежние формулировки zero-write в истории нужно читать с этим ограничением; текущие schema074 отчёт/summary исправлены без изменения evidence. Сохранность постоянных данных и161fingerprint отдельно проверена.

Итоговый lint6 PASS0issues и vet PASS,12983/b078c5. Полный Linux race43534 запущен на manifest3 (1459files) с собственной disposablePG; источник read-only и проверяется до/после. FQA5 продолжается отдельно.

Независимый schema074_import_review завершён: blocking findings нет в scoped storage rehearsal. Подтверждены73schema hashes/ledger,206importer source hashes,7plan+resolution pairs,5retained resources,161permanent fingerprints и45before/after readbacks. Границы: evidence/source review без DB rerun; initialbinary binding не reproduciblebuild; пустые provenanceтаблицы не подтверждают populatedruntimeflows; deployment/runtime/removal-of-CI acceptance остаются отдельно. Отчёт qa.local/importer-schema074/code-qa/REPORT.md. Полный Linux race43534 подтверждён живым, зависимости загружаются; не перезапускался.

Параллельно gates/FQA B начата read-only подготовка C: registration_batch_derivation владеет новым docs/architecture-stage-c-plan.md, инвентаризирует реальные потребители/границы регистраций, знаний и agent host, транзакции/ACL/provenance и проверочные сценарии. Product/source6 не меняется; реализация C и его приёмка не объявлены. Проверка отсутствующего ручного history-delete не выявила соответствующей команды в Python assistant; текущая операция Go явно host-only, FQA использует canonical synthetic operator control, не придуманную публичную команду.

Подготовка свежего affected FQA6 вынесена отдельно: fixturebuilder владеет repair6/stand-affected (пока только конфигурация/план, без DBwrites/builds), root подготовил нейтральный functional-qa-affected/SCOPE.md. Будущий reviewer получает требования/доступ, не исходники и прежние findings. Новый стенд должен иметь отдельные app/provider/TG/state/cloneDB; активный FQA5 и импортированный reference неизменны. Реализация/приёмка нового стенда не заявлены.

Подготовка C завершена source-only: docs/architecture-stage-c-plan.md фиксирует C1–C5, владельцев, реальные consumers и доказательства. Отдельные zns bot/api остаются HTTPпотребителями; MiniApp сейчасorders/food/timetable, невыдуманныеregistration/knowledge маршруты. Реализация C ждёт Bacceptance. Для affectedFQA6 утверждён отдельныйproject zns-b6-affected/cloneDB zns_b6_affected_functional, собственные app/provider/TG/evaluator/state; общиеZitadel lifecycleбарьеры согласуются с5lead. Slot2 передан builder послеproxytests. Proxyhelper прошёл11nativecases+release/drop, raceне запускался(cgo); свежий db_boundary_proxy_review идёт read-only до любого подключения кFQA.

Свежий Code QA тестового DB-boundary proxy выявил 1P1 и 2P2: observer I/O под общим mutex без внутреннего срока; hold deadline начинается после ReadyForQuery; overflow/non-idle завершение закрывает соединение вместо fail-open. Отчёт: qa.local/architecture-stage-b-repair5/stand-refresh/db-boundary-proxy/code-qa/REPORT.md. Прототип не подключён к активному FQA; исправления назначены отдельному db-boundary-proxy-repair1, исходный пятифайловый snapshot сохраняется. После исправлений нужны целевые воспроизведения и новый независимый Code QA. Кандидат6 остаётся неизменным. Linux43534 подтверждён живым; affected-стенд6 имеет отдельный clone, запуск auth ждёт согласованного окончания synthetic Zitadel barrier FQA5. Общий B не принят.

В продолжающемся Linux43534 зафиксированы два failing top-level tests: TestFoodReviewProofChecksVersionAtByteRead (revoked: expected displayed=false, actual omitted=true/pass_access_changed) и TestModernChoiceFullRuntimeAcrossRestart (d096: update11 completion condition). Полный запуск не останавливается; результат ещё не терминальный. registration_batch_derivation назначен read-only diagnosis с отчётом linux-failure-diagnosis.md, без изменения frozen6 и без автоматического увеличения timeout. Candidate6 app/evaluator builds завершены exit0, slot2 передан proxy repair; запуск6auth ждёт окончания согласованного IdP barrier. Образы и доказательства записаны в repair6/stand-affected.

Полный Linux6 gate43534 завершён exit1; cleanup=True. result.json:2608passing test events,2top-level failures с их subtests,13skips,0unfinished,no race reports. Все1459файлов совпадают сmanifest3 после выполнения. Отдельные race/PGcontainers иnetwork удалены; отчёт qa.local/architecture-stage-b-repair6/linux-final-gates/REPORT.md фиксирует failures и ограничения skipped suites. Диагностика получила свободныйslot1; proxy repair остаётсяslot2. Frozen6 не меняется, общий B не принят.

Отдельный unchanged Linux race TestModernChoiceFullRuntimeAcrossRestart/d096 завершился PASS49.013s,28370,cleanup=True; manifest3 unchanged. Evidence runtime-failure-repro/baseline2. Один проход не закрывает исходный timeout и не доказывает нагрузочную причину; developer исследует завершение доставки и выполняет отдельный food-test overlay. Shared local Zitadel barrier завершён и released к builder6; свежий Functional QA будет назначен после observed standhealth и освобождения agent slot. Proxy repair1 native proof завершён, независимая приёмка пока впереди; frozen application6 не меняется.

Проверка результата food overlay21240 выявила ложный зелёный статус launcher: exit0, но0testevents и no tests to run. PASS не засчитывается. Причина в CRLF временного shellrunner и trailingCR в anchored -run; developer исправляет LF и добавляет явную проверку выбранных двух subtests. Исходный evidence сохранён, frozen6 неизменён. Отдельный unchanged d096 действительно выполнил2testevents и прошёл; причина full-suite timeout ещё не установлена.

Food LF-repeat52414 доказан: PASS2.552s, ровно parent+replacement+revoked (3pass events), inventory-guard accepted,0skips/race/unfinished,cleanupTrue; внешний test-onlypatch не интегрирован. Proxyrepair1 native13cases и Linuxrace49539 PASS2.767s; source manifest da2674fe49546bec734dd777166f9c33b072c2910326f99f2c0ca0edcfa50b01. Свежий независимый Code QA запущен отдельной read-only CLI28888: original requirements+frozen files, без предыдущих findings. Первый CLI запуск не нашёл home в ограниченной оболочке; стандартный разрешённый запуск с сохранением дочернего read-onlysandbox работает, session2.log.

Свежий source-blind reviewer stage_b6_fresh_functional подготовил matrix, runtime ожидает operator release. Root93033 выполняет три исходных ModernChoiceFullRuntimeAcrossRestart variants вместе на isolatedPG: unchanged5s, только диагностический overlay с полной выборкой релевантных goroutine stacks при падении. Все исходные1459файлов остаются frozen. Evidence runtime-failure-repro/concurrent-diagnostic; результат пока не заявлен.

Fresh independent CLI CodeQA28888 завершён: proxyrepair1 не clean. Report code-qa-fresh/REPORT.md содержит1High+3Medium: stale decision может примениться к новому arm безID; expiry/Drop selectrace; valid>8MiBframes закрывают unrelatedconnection; отсутствует aggregateconnection admissionlimit. Findings source-derived, не runtime reproduction. Все6frozenhashes проверены; прежние findings/history ревьюеру не передавались. Назначен отдельный repair2 с точными regressionproofs; activeproxyroute по-прежнему отсутствует. Широкий FQA продолжает executable cells независимо от этих контролей.

Совместный race-повтор трёх ModernChoiceFullRuntimeAcrossRestart вариантов завершён PASS140.232s (93033),4testevents,0skip/failure/race/unfinished,cleanupTrue; прежний5s unchanged. Диагностический overlay менял только вывод при failure. Это не устанавливает причину прежнего full-suite timeout. Создан отдельный frozen7:1459файлов,manifest f9601991bbbeea0516f85bd85a20ec63dc6478fba2b1b84fb6b564dd625498b7. Ровно два изменённых integration testfiles (food expectation+durable terminal marker; runtime diagnostics); все non-testfiles побайтно равны6. Source6/FQAstand не меняются. Fresh Code QA49167 read-onlyCLI и native lint/vet/build/sqlc96683 выполняются; полный повтор7 после nativegate. Fresh FQA6 released на runningstand по observedhealth; независимаяmatrix сохранена.

Proxyrepair2 разрешён узкий streamingtype/length relay передpgproto3: библиотека не предоставляет streaming дляoversizedframes. Без SQL/bodyparser, fork илилогированиясодержимого; boundedforwarding иoverread/fragmentation proof требуются. До свежегоreview маршрутизация наFQA запрещена. Отказ автоматическогоapproval для synthetic303 history717/736 оставилданные неизменными; operator собирает read-only provenance/guard evidence для узкого повтора в рамках пользовательского разрешения всех local synthetic manipulations. Обходотказа не разрешён; другиеFQA продолжаются.

Auto-review повторно отклонил точечный canonicalDelete717 после read-onlyguard/provenance: считает blanketpermission непроверенным и требует exactauthorization.717/736 не удалены. Q3 записан вPROGRESS и задан черезasyncinput; зависимые source-retirement cells отложены, остальныеFQA продолжаются. Synthetic101/core-B reviewergrant одобрен для независимого manualpublication review с existingorders_ui reviewer. Native7lint96683 завершился1: единственноеmodernize(strings.SplitSeq) в новой диагностике; пока не исправляется во время активногоread-onlyCLI49167. Source7frozen неизменён, после завершенияreview потребуется минимальнаяlintправка и новыйmanifest.

Независимый CLI CodeQA7(49167) завершён clean scoped, исходныйmanifest и все1459hashes проверены, ровно2testdelta, продуктпобайтно6. После окончанияreview единственныйmodernize исправлен Split→SplitSeq только в failurediagnostic loop; старыйфайл сохранён, manifest2 3cedf4c501d842e9bfc88abd59c19137d5afd162726c508b820fa87a62ef3cd4. Freshreviewreport+rootlintdelta отдельно. Repeatnative77982 PASS:0lintissues,vet,cmd-build,sqlcdiff. Полный Linux7race запущен наmanifest2 с собственнойPG; дополнен expected-pass inventory дляfood обоихвариантов и3runtimevariants, чтобы no-tests не считалсяproof. Q3остаётсяpending, никакойповторудаления доответа.

Publication2 source-only diagnosis: reviewgrant присутствует, но causal private_history=true делает предложение недоступным другомуactor до явной публикации. Поздний SQLsnapshot20:49 также показывает revoked=true/gen12→14 после последующих source-retirements; это не состояние при первом наблюдении20:42. Текущая privacydenial намеренная, подтверждена существующимтестом; удалятьcheck нельзя. UXgap — pending_review без ownerconsent пути — вынесен вQ4 с конкретным docs/knowledge-publication-decision.md. Пятьответственныхsourcefiles побайтноидентичны5/6/7; productpatch не сделан. Свежий CLI CodeQA proxyrepair2 запущен50992 по frozenmanifest8a644acd58774d38b1333348eb5aaf3001bbf98f452b405748a26cb46f06520b; независимостьотpriorfindings сохранена. Broadcast confirmation ранееотклонёнauto-review; собранread-onlylocalfakeTelegram routingproof до возможного узкогоsameactionretry, никакойsend не выполнялся.

Fresh proxyrepair2 CodeQA50992 завершён с1High+2Medium source-derived findings: idle timeout раньшеGate.Timeout может оборватьheldack безDrop; saturation отклоняетCancelRequest; счётчикExecute/ReadyForQuery не учитываетнесколькоExecuteнаодинSync и теряетfuturematching. Все7manifesthashes intactbeforeafter. Назначен отдельныйrepair3; helperнеподключён. Дополнительноreviewnoted allowedlogbinding native2.376 vsfinal2.405 — уточнитьartifactsвследующемreview. FullLinux7 11075 подтверждёнживым и выполняетintegration.

Broadcast1 sameUIretry с read-onlylocalfake routingproof повторноотклонёнauto-review; никакихsend/alternateAPI. Q5 exactsynthetic303/broadcast1/source009/recipients930301..3 отправленДаниилу и внесёнPROGRESS. Q3history717/736 и Q4private-context publicationpolicy такжеpending. ДругиеFQA продолжаются; goalнеblocked.

### 2026-09-27 — candidate7 full race and isolated QA controls

Candidate7 full Linux race terminal4ac695/process11075 exit0:2612 passed test events,13 explicit skips, no failures/unfinished/race. Manifest2 all1459 files unchanged; owned resources cleaned. Evidence: qa.local/architecture-stage-b-repair7/linux-final-gates/REPORT.md. Prior runtime timeout did not reproduce; cause remains unproven. Overall B remains unaccepted.

DB-proxy repair3 frozen after21 native/race tests and vet; independent CLI Code QA process99618 started. Isolated response-boundary adapter provides actor/marker selection and live request evidence;5 Node tests passed, fresh independent Code QA started. Active stand unchanged; deployment requires sole operator and FQA idle barrier. Pending questions remain deferred.


DB-proxy repair3 independent review terminal1a5352/process99618: changes required, two source-derived findings (expired downstream write deadline after slow unmatched observation; workers not joined before admission release). Developer assigned successor repair4 with focused reproductions, no routing. Response adapter independent review process48732 remains active.


Response-boundary initial Code QA terminal934f26/process48732 found Unicode multipart marker mismatch. Root reproduced both text/caption misses (0907a6), repaired only non-file UTF8 decoding in isolated response-boundary-repair1, added exact binary download comparison. Final7 tests PASS475646; interim incorrect test assertion retained separately. Successor frozen and fresh independent review started. Active adapter unchanged.


Fresh response-boundary-repair1 Code QA exact process46881 active. Affected FQA6 reports same-update20000068 recovery after guarded restart at live provider request118; final independent report pending. No overall acceptance claimed.


### 2026-09-27 — affected FQA6 complete and exact-release correction

Fresh independent Functional QA6 report PASS within source-authority SCOPE, no reproduced actionable product defect. Evidence: qa.local/architecture-stage-b-repair6/functional-qa-affected/review/REPORT.md. Explicit limits include completed-update replay unavailable, physical touch absent, synthetic provider only. Same-update68 recovery verified; original imported history2 preserved; all captured roles/selection restored, no holds. This is not broader B acceptance.

Response-boundary-repair1 fresh review terminale5cba9/process46881 reproduced invalid explicit fault_id releasing all holds. Root successor repair2 reproduced RED27a58a; validates explicit selector and distinguishes omission, full8 tests PASSddb378. Frozen and fresh Code QA launched. Active stand adapter unchanged. DB-proxy repair4 fresh Code QA process10817 active after23 native/race tests+vet.


DB-proxy repair4 fresh independent review terminal44c1db/process10817: changes required, source findings on pipeline observation eligibility, duplicate concurrent decisions and GSS auth response relay. Developer owns successor repair5; no active routing. Fresh response adapter review process59947 still live.


Response-boundary-repair2 fresh CLI Code QA terminal9ea86c/process59947 scoped clean: hashes and independent syntax checks passed, no actionable defect. Eight test results retained with explicit coverage limits. Root handed deployment-only ownership to soleoperator5, conditional on lead all-lane barrier and baseline/hash/state preservation. No deployment claim yet.

202 QA browser filechooser hung2495s and aborted. Root interrupt/checkpoint elicited explicit IDLE: no upload click, no attachment control, no proof effect. Lease released to101; native proof remains unaccepted, no false success. Exact original Updates192/194 unavailable after ingress pruning; no reconstruction performed.


### 2026-09-27 — reviewed response control deployed

Soleoperator5 deployed reviewed response-boundary-repair2 adapter under explicit all-lane barrier, restored orders-a activeevent. Active adapter SHA25604eef5d0189d63df2af0e9c8f9683f4228405c920204813ecdf64d47cb8e244a; baseline matched, state retained, health200, no active_requests. Initial2-second health probe failure preserved; subsequent10-second probe passed without another restart. Appimage unchanged. Evidence stand-refresh/response-boundary-deployed.json and deployment log. Functional exercise remains open; this deployment alone does not prove saved-response durability.

Separate developer research assigned to stage_b_fixture_builder, owned debugger-control-research/ only: existing Delve or targeted ordinary PG row lock for missing saved-decision checkpoint, no active stand mutation or product hooks. Read-only source7 suggests target-order row lock as hypothesis; no acceptance proof yet.


### 2026-09-27 — controller runtime proof and saved-decision control

Root owns isolated stand-refresh/db-boundary-control/. Native4tests+vet PASS1e53ae; Linux Go1.27.1 race4tests+vet PASSd6c8da/process34610,1.137s. Actual authenticated HTTP→Gate→PG transport proved held committedreceipt visible, explicitRelease successful and explicitDrop uncertain while receipt retained; runtime workers joined. OwnedPG/runner/networkcleaned. Frozen9files, independent Code QA process91098 active. Not deployed; proxydependency still unaccepted.

Proxyrepair5 independent Code QA terminaldeb31e/process67695 changesrequired: post-Matched pipeline forwarding may lose unrelated acknowledgements; smallintervening notice/error can be swallowed. Developer assigned repair6, predecessors preserved, no routing.

Developer row-lock proof PASS1.321s: exactorderlock, savedturnvisible, orderv1/receipt0/inbox1 and exact blocker; hardkill +lockrelease rolledback unchanged, sameexecutable recoveryupdate1→v2/receipt1 without model call. Frozen7 manifest unchanged, isolatedresourcescleaned. Fullruntime integration harness only, not shipped-image FQA. See repair7/debugger-control-research/ROW-LOCK-PROOF.md; operator may expose neutral control to independentQA.

303B response attempt213 operatorreports exactpersistedresponse+receipt/live beforeeffect bracket and immediate sameimage apprestart; independent recoveryverification inprogress. Prior212 missinglive-atstop remains explicitly no-restart/notpassed.


Controller independent review91098 terminal235f1a changesrequired: activeHTTP handlers not joined by Server.Close/Serve return. Root preserved initialsnapshot, successor db-boundary-control-repair1 reproduces shutdownRED1274f5; switched to shared cancelled BaseContext + standard Server.Shutdown wait. Native full5tests+vet PASScded25, Linuxrace successor started. No routing; currentdependencyrepair5 still unaccepted.


Controller successorrepair1 finalnative5tests+vet PASSd6e9e5, Linuxrace5+vet PASS045324/process61686, pinnedlint0issues c3cf8f. No gate configuration/suppression changes. Structured helpers and Go workspace replace localgo.mod replacement; dependency frozenproxy6. All19 source/evidence files bound in review-manifest; fresh Code QA started. Earlier lifecycle/lint attempts preserved. No routing.

Libraryproxy6 fresh Code QA terminal7f45ec/process65566 oneP2 finalDrop deadline recheck absent aftermutexdelay; successorrepair7 assigned. Neutral rowlockoperator script rootreviewed: exactsyntheticguards/lifecycleorder; boundedSQLobserver and robustfinally requested/applied, no executionyet.

FQA303B recovery independently confirmed: original modelreceipt count1, xhigh/v4, same savedreply, inboxgone, no postrestart replan. QAsimulator missing actualanswerCallbackQuery text/show_alert leaves227 outcomeunknown; developer owns isolatedcallback-notice successor, activeadapterunchanged pendingtests/review/barrier. No guessedproductdefect.


### 2026-09-28 — local-only controller and remaining Telegram UI scope

Controllerrepair1 freshCodeQA50350 terminalaf917c identified unenforcedlocalnetwork contract. Successorrepair2 now validates literal-loopback listeners beforeconnection, strictlocalPG names and checked/pinnedDNSaddresses. Native7tests+vet PASSde9d53; Linuxrace7+vet PASSbaa160/process87791; pinnedlint0issues758b0d. Frozen21source/evidence files; freshindependentCodeQA started. No proxy routing. Currentworkspace dependency remains immutableproxy6 and separately unaccepted.

Proxy7 freshreview79670 terminal4f4ed2 oneHighfrontend admission/accounting race; successorrepair8 assigned with coherentperconnectionstate requirement. Sourcesandfailedproofspreserved.

Unified callback-notice candidate17testsPASS, actualcallback notice/voice/caption UI prepared; freshCLI review8210 active. Realbrowser smoke remainsunverified; candidate notdeployed. EN/RU spokenextraeditfixtures preparedoffline byoperator. Required voice-extra mutation and actualcaptioncapacity proof remainopen; priorHTTPvoice evidence notsubstituted.


### 2026-09-28 — shipped-image recovery boundary and helper reviews

The S0 operator sequence for exact update242 completed on the unchanged application image: saved turn and inbox, order version31 and no receipt; app stopped before releasing the exact-row locker; blocked backend disappeared and version31/receipt0 were verified before restart. Independent Functional QA observed automatic recovery to version32, total242 and selected extra025 in the rendered card and owned Mini App, with only the original provider request383. Final receipt/audit observer is pending; this is not full stage acceptance.

Controller repair2 independent Code QA39632 completed: no actionable controller defect, but dependency repair6 shutdown does not join all listener/cancellation helpers. Source-analysis finding is assigned for verification against current proxy8 and correction in a separate successor if applicable. Combined acceptance and active routing remain open.

Proxy8 frozen after 28 native tests/vet and 28 Linux race tests/vet, plus the focused admission harness. Review manifest F2E7C1FCA78A041AF5C8367A41E74C35A45D9AE1E8F350B8FEA05C111967F3A2. These are developer gates, not independent acceptance.

Callback/voice/caption candidate fresh review8210 found actor capture across async file reading, reconstruction of evicted callback bindings, and insufficient byte/control bounds. A separate repair1 is assigned; the active adapter remains unchanged. No product defect is inferred from unavailable callback notice evidence.

S0 final readback accepted independently by Functional QA: exact tg-order-10000242 receipt count1, result version32, single edit audit163, current version32/extra025/total242, inbox drained. Together with rendered card/Mini App and original provider383-only evidence this passes the bounded ordinary-order saved-decision recovery scenario. Operator recovery-observer.log inspected by root; overall B remains unaccepted.

Callback successor repair1 frozen: manifest846200b187f3a86f93f2094772fb2b523f36e5d476eb8f02c196213ee105aa15, script c7d965f4cd1b9c15ef6a06920d4d4f9752f76791353ec67d203efc4a6801cac3. All22tests pass with zero skips and syntax checks pass; fresh independent read-only CLI Code QA35435 launched. No active deployment.

### 2026-09-28 — callback review and idle-stand cleanup

Fresh callback repair1 Code QA35435 terminal418104 reproduced two P2 defects without sockets: multipart splitting truncates an interior boundary-token sequence, and browser seenAlerts retains unbounded IDs. Repair2 is assigned to the existing builder with focused reproductions, preserved attachment bytes and bounded acknowledgement state. The frozen predecessor is retained; no helper deployment occurred. Baseline metadata must be included in the next review manifest.

Completed affectedFQA6 operator and independent reviewer both confirmed no current process/readback uses its stand. Root verified project labels and stopped only six exact zns-b6-affected containers: qa-drive, qa-telegram, qa-provider, app, gateway, evaluator. Stop63589 terminal3c9ca2 confirms all running=false; app/evaluator exited0, Node adapters exited137 after timeout. No active QA was interrupted. DB, source/evidence, images and shared PostgreSQL/Zitadel services remain intact. Broad stand5 is unchanged by this cleanup.

Proxy9 full native30 tests/vet and Linux30 race tests/vet passed. Root Linux runner76427 terminal144bc1: Go1.27.1, test duration5.634s, exit0, zero skipped tests; disposable runner/PG/network cleanup recorded. The source is read-only mounted and the exact runner recipe is retained. Final freeze and fresh independent library review remain pending; no active proxy routing.

S2 order saved-response experiment update243 reached persisted marker/ready turn, exact script receipt and order version33 with the same live response request before app restart. Operator readiness observer timed out; later health200 and no live requests were observed separately. The timeout is retained, and independent native recovery/receipt verification is still pending. This is not yet an accepted recovery result.

### 2026-09-28 — order response recovery accepted in bounded scope

Independent FQA accepted S2 update243 after native rendered card/Mini App recovery and final observer readback: exact script receipt count1/version33, total266, extra024 selected, saved reply unchanged, inbox consumed. Only provider384/385 before restart were observed for the exact original text; no resubmission/replan. Readiness timeout is retained and later health200 is a separate observation. This does not establish real Telegram exactly-once delivery or the after-domain-commit-before-response boundary.

Proxy9 frozen and fresh Code QA36810 running. Source manifest641A8F6BD756CB1FC8B015D968EE51BC8964BC88BAEF3988AB1FDB58FF10444B; verification13CC14EF046FFF720F86FB1F54BF52DCA9FF696A91A5EC91993179F1692ED8A0.

Callback repair2 frozen after24 passing tests and syntax checks; fresh independent CodeQA37571 launched. Manifest16781d7a6b076bd9fb8334a672706c0fe0bf34c3394cc3684e49cf0827421f26 includes exact baseline copies. Active adapter remains unchanged until review and idle-barrier deployment. Authentic signed-token negative fixture preparation is separately owned by the operator; no shared issuer mutation yet.

Q6 resolved by Daniel: explicit approval for dedicated shared local Zitadel negative-token setup and, more broadly, any changes in test Zitadel. The earlier auto-review rejection occurred before execution. Operator may retry the authorized setup after an idle barrier and record created resource IDs; production remains outside scope.

Proxy9 fresh Code QA36810 identified a ReadyForQuery cleanup/next-COMMIT protection race. Root copied immutable9 to isolated10 and reproduced the exact interleaving in a generated harness: RED87591d unexpected EOF; moving activity release inside the existing response-state lock passes the same scenario49cf78. No pause hooks in final library. Full native and Linux gates remain required.

Callback repair2 fresh Code QA37571 terminalb80d29 reproduced out-of-order polling that reopens dismissed alerts and replaces current cards. Builder will fix response sequencing and owner generation in the separate replay successor, preserving frozen2. No active helper deployment.

Q3 explicitly approved by Daniel: "Q3 разрешаю, как и другие синтетические тесты." Fresh Functional QA may delete canonical synthetic history717/736 through DeleteContent, recording then-current state and original receipt preservation. Earlier precondition snapshots are not current expectations. Permission is resolved; execution/acceptance remain pending.

Q6 dedicated local Zitadel fixtures created successfully after explicit approval. Resource inventory and cleanup instructions are under stand-refresh/identity-negative-fixtures. Wrong-actor token has alternate actor with expected issuer/user/audience/client; wrong-audience also differs in client ID, a documented limitation for independent QA. Existing identities/configuration remain unchanged; no product restart.

Q4 approved: create a complete self-contained proposal text, preview it to its author, then a manual Send for review action. The approved exact-text/destination consent contract is updated in docs/knowledge-publication-decision.md. Implementation assigned to isolated candidate8 from immutable7; current FQA5 unchanged.

Q5 explicitly approved: "Q5 всё разрешаю! Синтетические тесты ВСЕ разрешены". Exact local stale-broadcast confirmation may proceed with independent evidence. All pending user questions are now resolved; implementation/test completion remains separate.

Daniel requires separate stands when synthetic checks need incompatible system states. FQA lead classified remaining work: current5 for compatible manual/media/source cases; separate exact-image/DB clone for remaining commit/partial-batch failures; independent identity-lifecycle stand with its own mutable issuer. Existing completed S0/S2 evidence remains valid and is not rerun merely because of this split. New product Q4 has separate candidate acceptance.

Daniel clarified blanket authorization: all synthetic checks and operations on local test containers/inside them are permitted; do not damage the Windows host, Codex itself or unrelated host data. Incompatible system states require distinct stands. This supersedes any inferred need to ask again for ordinary local synthetic mutations.

Callback capture/replay candidate frozen after30 passing tests, syntax checks and demonstrated stale-response reproduction on its comparison baseline. Manifestb08ea7a6ca278c70503b11a88faacc1e713de8a2a0b4c8aaa53579bbb56023ad; fresh independent CodeQA97064 started. No deployment yet.

Proxy10 frozen: source825171F9C1C54623A3336C3D4A32DBBF3E9F5E70C984C9499D83CBE32B37F245, verificationF4EEAA6636476C16E8E47F90441EA5836B7D41F40CE08D716508B4F03BA518B8. Native30/vet, Linux30race/vet and separate interleaving harness race all passed; fresh independent CodeQA46534 started. No combined controller acceptance yet.

### 2026-09-28 — independent proxy review and approved scenario results

Proxy10 independent CodeQA46534 terminale6c4f1 scoped clean:43source/16verification hashes match, no actionable library defect. Composition successor controller3 uses exact proxy10: native7/vet, Linux7race/vet and pinnedlint0issues passed. Fresh combined CodeQA19375 launched. Initial launch1682 was stopped because manifest generation failed on a PowerShell parameter typo; no valid review claimed, its log retained. Corrected manifest generated before the new fresh reviewer.

Q5 independent UI outcome: exact old Send reviewed draft clicked as update246 after local routing check; a new Russian source_revoked refusal appeared. Postobserver reports draft1 cancelled/source_revoked, zero deliveries and drained inbox. Earlier approval rejections remain historical; authorized test now executed and passed in this bounded scope.

Q3 canonical deletions717/736 advanced generations22→23→24. Before/after observer retained original receipts and then-current values. Fresh models.own.get247 preserved xhigh/v4; provider390/391 inputs omit original138/147 texts and CANARY304/305 sentinels. Final paginated-history coverage remains with independent QA; no new conflicting-state restart on baseline5.

Developer ownership: runtime_stage_d_preflight handles candidate8 domain/SQL/API/client and operator5; proposal_submission_ui handles agreed candidate8 bot cards/callbacks/locales/help; isolated_fqa_stands owns separate recovery/identity containers and recipes; stage_b_fixture_builder owns replay-repair1. Root owns progress, controller composition and review coordination. No product acceptance inferred from helper gates.

Q3 final public-history readback completed independently:11pages to more=false. Canonical717/736 omitted as deleted; derived718/737 omitted as authority_revoked; no retired text or CANARY304/305 anywhere. This complements original/current receipt preservation and fresh model-input exclusion. No new restart is claimed. Evidence: functional-qa-final/shared-saved-data/q3-history-pages.json and q3-history-omissions.json.

Controller3 Linux binary built with CGO disabled, network disabled and source read-only; SHA256013E4A6815EA0A2656DB91D570F45B968D815CC9B72A6200C7227DE461B7DA7C. Active routing remains pending independent composition review. Stand builder has the artifact and neutral configuration constraints.

Controller3 composition fresh CodeQA19375 terminal9bc1c0 PASS, independently checked exactproxy10/source hashes. No actionable defect; activation is limited to new isolated recovery stand and does not replace Functional QA. Binary and neutral control contract delivered to stand builder.

Replay-repair1 frozen after34 passing tests/no skips and syntax checks. Aggregate admission128files/64MiB retains all accepted bytes; no unsafe GC/release API. Explicit voice duration0 preserved. Manifest42bd4cf58865455a8411998c040b0c04d220c707a0c0dd57a6408e065d5daa67; fresh independent CodeQA28871 launched. No active deployment yet.

Separate recovery and identity PostgreSQL clones restored from recorded candidate5 snapshot; fresh independent Zitadel issuers are healthy and bootstrap continues. They do not inherit later baseline5 tests unless explicitly reconciled. Active5/shared issuer unchanged. Q4 core/API/client compile-only passed; focused UI/domain tests are running in separate scopes/DBs. Built is not accepted.

## 28 сентября 2026 — промежуточные gates B

Кандидат8 author-confirmed proposal submission заморожен:1467 файлов,35 изменённых путей относительно7; manifest398c1c91bd06c6c10137569e0c185cb3344f7b21ee121c8795e761dd48e8b546. Целевые PG54.367s, unit и affected pinned lint прошли; полный Linux race ещё выполняется. Это не итоговая приёмка.

Независимый identity Functional QA на отдельном candidate5 стенде подтвердил отказ в выдаче proof после завершённого inspect и отказ в export после разрешённых event/payment-queue reads при отключённой реальной synthetic identity. Восстановление+restart не возродили отклонённые действия. Это внешне наблюдаемые границы, не произвольный внутренний binding hook; export queue был пустым. Evidence: qa.local/architecture-stage-b-repair5/functional-qa-final/identity-delivery/isolated/REPORT.md.

Частичный batch recovery253 не принят: первый эффект/receipt сохранён один раз, оставшиеся отклонены. Точная причина перехода поколения23→24 в этом старом запуске не доказана. Отдельный реальный PG-repro на неизменном source8 доказал механизм: lazy read уже устаревшей производной истории создаёт лишнее поколение и блокирует новый допустимый batch; clean-history и явное удаление источника служат контрольными сценариями. Evidence: qa.local/architecture-stage-b-repair8/recovery253-diagnosis/repro/RESULT.md. Исправление и свежая affected-приёмка впереди, source8 не изменяется.

### 28 сентября: frozen candidate9 and Telegram helper verification

Candidate9 frozen:1468 source files, manifest `f318d9888f0e9f5e60493584e9d503080b7c4840131982fc16292cd9b3da30f3`; exactly2 changed paths relative candidate8. Focused native PG8.107s, Linux race conversation5.113s/integration7.435s (14passed/0skip/no race), vet and pinned lint passed. Fresh composed Code QA and final static gates started; no independent functional acceptance or deployment is claimed.

Candidate8 full Linux race terminal exit1:2614 passed test events,13 explicit skips, no unfinished tests/data race; only `TestModernChoiceFullRuntimeAcrossRestart/d096` failed. Disposable resources cleaned. The same frozen source is under focused diagnosis; diagnostic state had reached the expected completion predicate by observation, but that alone does not establish the cause or a pass.

Telegram helper `callback-notice-replay-repair5`: exact predecessor2 cases fail as expected; candidate50/50 pass, syntax pass. Process-interrupted state writes and uncaptioned voice display covered; no power-loss/filesystem recovery claim. Frozen review manifest `7a14acfefa3a61bd1fe3a52548309d516c3d4a9a26b09d641ff69bae9e6a9022`; fresh Code QA started. Active stand adapters remain unchanged pending review.

### 28 сентября: independent candidate9 review and successor10

Candidate9 final static composition: vet/build/sqlc/pinned lint passed,1468 frozen files unchanged. Fresh independent Code QA reviewed the composed7→9 change and found two P1 privacy-boundary issues (downstream proposal/fact provenance projection; history generation consistency during invalidation) and a P2 aggregate-authority limit regression. These are source-derived findings pending runtime reproductions. Successor10 starts from exact frozen9; knowledge, conversation and readsource changes have separate owners. Candidate9 remains immutable and unaccepted.

Helper6: predecessor2+4 RED and54 candidate tests passed; fresh review manifest `ce5d1ea88db1c099bb3362c215545d689cf804b52547c5a2d441b90d9e2f329a`. New independent Code QA started; no active stand upgraded.

The d096 deadline miss reproduced with the unchanged unprofiled concurrent pair, while a different scheduling of all three leaves passed. No business/privacy failure was observed. Duplicate quote hydration is a candidate performance cost, not separately measured proof. Existing assertions were not relaxed; the full gate remains failed. Deferral policy has been discussed with Daniel but not yet adopted.

### 28 сентября: helper6 independent review clean

Fresh CLI Code QA terminal0, scoped clean. All19 manifest files verified unchanged; independent4 status tests passed and same4 failed on exact predecessor. Full54 tests remain supplied evidence. Operator assigned adapter-only deployment to idle active5/recovery/identity with live no-work checks and preserved transport/business data. Product images remain unchanged. Functional continuation is not yet a pass.

### 28 сентября: candidate10 regression evidence

All three candidate9 review findings reproduced before fixes: private closure metadata escaped through downstream proposal/fact results and receipts in real PostgreSQL; five history read modes exposed derived text stamped with a newly advanced generation; repeated/distinct aggregate authorities hit the persisted-record budget. History correction passed the five new modes and prior lazy-retirement/batch controls (native13.505s). Scoped Linux race conversation4.726s/integration12.848s and vet subprocesses passed; wrapper exit2 from CRLF is preserved, and overlapping readsource edit limits this to focused evidence. Final frozen-composition gates still required.

### 28 сентября: helper deployment and aggregate PostgreSQL checks

Helper6 installed on active5, recovery5 and identity5 with exact SHA256 `6d5077b2bab4a959de3b718313c5b8a16a0f059ffb204452977c503c2c2be4df`. All protected messages/files/updates/calls/counters retained. Active5 three historical unused fault counters were disarmed after lead confirmation and exact before/after preservation; all live request/inbox/armed counts zero at handoff. Product images/DB/issuers unchanged. Lead resumes actual rendered callback/capture/replay flows; ASR030/031 mapping is separately prepared for active5 voice scenarios.

Candidate10 aggregate-authority PostgreSQL tests passed6.214s for repeated/distinct valid history windows and positional origin/reader semantics; opaque-memory hook unit checks also passed. Knowledge projection combined verification remains in progress. Fresh dedicated author-stand PG/proxy/Zitadel bootstrap started without business restore/schema/app deployment.

### 28 сентября: candidate10 knowledge checks and author baseline

Opaque knowledge closure regression and invalid-ancestry controls passed4.234s, including ordinary fact readers, child/legacy receipts, downstream memo and live ancestor retirement. Affected existing PG cases passed except disposable setup lacking zns_bot; after canonical NOLOGIN prerequisite initialization, only affected split-role cases were repeated and passed9.689s. Scoped lint remains in progress; no schema/migration change.

Author-stand genuine issuer and five delegated tokens verified. Historical business-only dump restored into a new isolated business DB as a preserved reference, but contains no public.zns_schema_migrations ledger or submission table. No ledger entries fabricated and no075 migration applied. Canonical schema/data restore order is being resolved before application deployment; this is an implementation task, not a user decision.

Active5 ASR exact5 mappings retained and2 verified030/031 mappings added; only ASRhelper restarted. Token renewal uses supported syntheticissuer exchange. Actual rendered FQA continues independently.

### 28 сентября: архив уточнений из PROGRESS
## Требует решения Даниила

Открытый вопрос: применять ли предложенный порядок отложения незначительных дефектов в `DEFERRED.md` с влиянием, воспроизведением и целевым этапом? Утечки, ошибки прав, потеря данных и повторные эффекты не откладываются; проваленные проверки не становятся успешными. Ответ ожидается; исправления приватности и остальные работы продолжаются.

Q4 согласован: самостоятельный законченный текст предложения и ручная кнопка «Отправить на проверку»; реализация проходит проверки. Q3 и Q5 проверены в указанном выше объёме. Любые необходимые операции с локальными тестовыми контейнерами и их содержимым явно разрешены, включая удаление, пересоздание, изменение прав и имитацию сбоев. Не затрагиваем Windows, Codex и посторонние данные хоста; несовместимые состояния проверяем на отдельных стендах.



Q3 решён и проверен 28 сентября: synthetic история717/736 удалена штатно; независимый QA подтвердил скрытие исходных и зависимых ответов и сохранность выполненных операций.

Q2 решён 27 сентября: переносим всю доступную старую личную переписку с ассистентом, без ограничения периода. Других действий бота в этой коллекции нет. Даниил подтвердил отсутствие секретов и приватных полей в этой истории до агентского режима. Решение закреплено в [контракте](docs/messages-migration-contract.md); запуск реального импорта и production cutover остаются отдельными действиями.

Q1 снят: Login V2 и независимый Functional QA подтвердили Telegram-only вход и `email_verified=false`. [Конфигурация](docs/identity-login-v2.md).

Ошибки реализации, конфигурации и локальных стендов исправляются без отдельного согласования.

Здесь фиксируются продуктовые вопросы и проверки, которым действительно нужны участие или согласие Даниила: конкретное решение, варианты и последствия, зависимая работа и доступные независимые проверки. Зависимую работу откладываем до ответа; остальные направления продолжаем. Ответы переносим в историю и соответствующий контракт.

Локальные синтетические тесты разрешены, включая отдельные Zitadel, полный локальный debug/trace, копирование между локальными тестовыми БД и изменение тестовых сборок. Q6 решён 28 сентября: Даниил явно разрешил любые изменения в тестовом Zitadel, включая отдельные проекты/OAuth-клиенты/machine identity с impersonation для негативных проверок; общие QA-стенды меняем в согласованную паузу. Секреты не публикуем в Git или отчётах. Ограниченные проверки реального API и связки разрешённого тестового аккаунта с CLX Test Bot выполняем после основных тестов; расходы учитываем. Локальные коммиты/ветки/слияния отдельно разрешены; push/production deployment — нет.

Если из продуктовых задач останутся только ожидающие решения, явно отмечаем это и продолжаем обязательный рефакторинг. После его завершения, если ответы ещё не получены, выполняем security audit, затем дополнительный архитектурный анализ модульности, сопровождения и расширяемости. Эта последовательность не заменяет незавершённые продуктовые требования или QA.



### 28 сентября: candidate10 frozen and independent gates started

Frozen1474 files, manifest `90516aea35ada8791971b73bcfb70cd5ae74e8fab84acb405e4de490ac90478a`,13 changed paths relative9, no deletions. No schema migration changes. Targeted history/privacy/authority-window PG evidence and final scoped pinned lint passed. Full Linux race17314 runs immutable source with required new regressions explicitly checked; fresh source-only independent Code QA47819 reviews composed7→10 changes without prior findings. Final native composition gates wait for the canonical074 restore slot. Built remains unaccepted.

### 28 сентября: разрешён архитектурный перенос серьёзных дефектов

Даниил уточнил: тяжесть дефекта не запрещает перенос, если есть чёткое понимание, что его устраняет новая архитектура. Прежнее предложенное абсолютное ограничение по классам дефектов отменено. Правило и обязательные доказательства/этапы/ограничения закреплены в DEFERRED.md; C может продолжаться при конкретном обоснованном переносе без ложной полной приёмки B. D-001/d096 пока кандидат на оценку, не принятый перенос: вклад повторной загрузки в таймаут ещё не доказан отдельно. Текущие исправления кандидата10 не отменяются.

### 28 сентября: D-001/d096 перенесён по явному решению

Даниил подтвердил: «d096 точно можно». D-001 принят в DEFERRED.md, целевой этап C4. Известный race-таймаут больше не блокирует переход к дальнейшему рефакторингу. Полный текущий прогон и тест не изменяются; его возможный failed-result сохраняется, остальные ошибки не покрываются переносом. Проверка после изменения архитектуры и закрытие D-001 остаются обязательными для всей цели.

### 28 сентября: candidate10 fresh Code QA clean

Fresh independent CLI Code QA47819 terminal0, no substantiated findings. Verified1474 candidate10 and1459 candidate7 manifest entries before/after, reviewed45 composed changedfiles plus supporting source and approved requirements. Static review only; runtime/Functional QA not claimed. Final native vet/build/sqlc passed, full lint and immutable image builds remain in progress. Canonical074 scratch restore separately sent to fresh read-only review83121.

### 28 сентября: missing features после архитектуры

Даниил подтвердил предложенную последовательность: рефакторинг C–E → недостающие функции → полная проверка паритета → финальные реальные интеграции. До завершения архитектуры реализуем только необходимое для проектирования/проверки её границ. Инвентаризация функций и критерии приёмки сохраняются; построенное и непроверенное не смешивается с отсутствующим. Фокусные проверки и независимые QA архитектурных этапов остаются обязательными; полный продуктовый набор не требуется перед каждым шагом. Решение внесено в PROGRESS, архитектурный план, parity-current и DEFERRED.

## 28 сентября — итоговые gates кандидата 10

Замороженный состав: 1474 файла, manifest 90516aea35ada8791971b73bcfb70cd5ae74e8fab84acb405e4de490ac90478a. Свежий независимый Code QA всего состава, vet, build, точный sqlc diff и полный pinned lint прошли. Оба образа собраны штатными неизменёнными Dockerfiles; привязки сохранены в qa.local/architecture-stage-b-repair10/images/run-20260928T091348312Z/IMAGE-BINDING.json.

Полный Linux race завершился с exit0: 2641 успешное test event, 13 явно пропущенных внешних проверок, ни одного failed/unfinished test или race report. PostgreSQL integration прошла за 1151.235s. Все 1474 исходника неизменны; контейнеры и сеть прогона удалены. Доказательства: qa.local/architecture-stage-b-repair10/linux-final-gates/result.json, integrity-after.json, cleanup.txt. Один успешный общий прогон не закрывает ранее воспроизведённый нестабильный D-001; задача C4 сохраняется. Функциональная приёмка итогового состава остаётся отдельной.

Synthetic provider получил отдельный точный AV-transcript selector в замороженном successor; fresh Code QA без замечаний, hashes10/10, независимые VM7/7 и дополнительные проверки. Deployment ожидает подтверждённого простоя UI QA, без прерывания текущих260/261. Отчёт: qa.local/architecture-stage-b-repair5/stand-refresh/provider-av-selector/code-qa-fresh/REPORT.md.

## 28 сентября — восстановление author stand и диагностика260

Canonical074 restore repair2 прошёл свежий независимый разбор восстановления/ownership и полной case-sensitive проверки trigger semantics. Единственное оставшееся P2 касалось очистки временного fault-list после ошибки удаления synthetic scratch DB; узкий successor repair3 использует независимый finally и сохраняет обе ошибки. Root проверил diff и повторил mocked cleanup proof: exit0, без DB/Docker операций. Полный повтор восстановления не требовался. Строгое сравнение сохраняет различия CRLF/LF; независимый reviewer проверил полные тела двух конкретных функций и признал только эти различия семантически эквивалентными. Доказательства: qa.local/architecture-stage-b-repair8/stand/canonical074-repair2/code-qa-fresh/REPORT.md и canonical074-repair3/.

Для зависшего update260 минимальный isolated remove_fact → render → receipt replay прошёл на frozen5 и frozen10 под полной и ограниченной bot ролью. Это не воспроизведение текущего сбоя и не доказательство исправления кандидатом10. Оператор исправил ошибку интерпретации origin: reply260 authoritative, input origin agent. Текущее сохранённое состояние исследуется отдельно, общий FQA-стенд не меняется.

## 28 сентября — ограничение диагностики260

Automatic approval review отклонил экспорт pg_dump текущей synthetic БД active5, сославшись на возможное приватное содержимое и недостаточно конкретное разрешение на этот payload. Экспорт не выполнялся; обход другим способом не используется. Несмотря на ранее данное разрешение копировать локальные тестовые БД, диагностика продолжена через узкую обезличенную fixture. Это ограничение дополнительного способа диагностики, не доказательство дефекта и не остановка остальных задач. Если точная копия окажется необходимой, отдельное решение будет вынесено в PROGRESS.

## 28 сентября — итоговый author stand10 передан независимому FQA

Развёртывание41821 завершилось exit0. Существующая проверенная canonical074 БД обновлена штатным образом10 до075 под ограниченным migrator; журнал содержит74 точных checksums. Consent пуст, преобразовано только ожидаемое состояние старых предложений; другие business digests сохранены. Runtime/meter права, собственный issuer и пять настоящих delegated user capability calls проверены; UI/API200. Добавлен единственный synthetic review grant202/core-b;303 явно отмечен superadmin,404/606 без knowledge grants. Нейтральный ACCESS и RELEASE сохранены в qa.local/architecture-stage-b-repair8/stand/.

Запущен свежий source-blind candidate10_author_fqa: original requirements + neutral access, без прежних findings. Отдельный developer готовит два recovery variant стенда final10 в уже выделенных проектах; это не приёмка восстановления и не общее принятие B. Stand ownership разделён; авторский стенд заморожен на время QA.

## 2026-09-28 — keyboard protocol verification and remaining Functional QA

Candidate10 author Functional QA did not pass: manual consent commits and the card updates, but later ingress remains pending. Recovery QA stopped before its batch mutation after automatic approval rejection; the operator removed its lock, confirmed unchanged registrations/no batch or receipt, and released both stand leases. Read-only UI observations remain scoped evidence, not recovery acceptance.

Primary Telegram Bot API and TDLib source verification establishes that a present inline_keyboard:null is invalid: Client::get_reply_markup requests Array and JsonObject::extract_optional_field rejects present non-array values. The prior helper-envelope candidate accepting null is withdrawn, with its original experiment retained. Application nil-row serialization must emit a valid keyboard; the independent helper error-envelope correction must retain strict null rejection. A narrow candidate11 transport patch is in development, without changing frozen10 or active stands. Protocol evidence: qa.local/architecture-stage-b-repair5/retirement260-diagnosis/PROTOCOL-CORRECTION.md.

The user asked about reducing intermediate FQA. The proposed changed-area FQA during C–E and full regression afterward is recorded as a pending decision, not silently adopted. Existing accepted deferrals and architecture-first scope remain unchanged.

## 2026-09-28 — candidate11 transport correction, narrow gates

Candidate11 composes the unchanged1474-file candidate10 with two new Telegram files. Final binding is final-manifest2.json SHA256232b22b5962d21daa8e7ad610b084d9018c309943032601eec124f3548fcb804 (1476files). Send.MarshalJSON encodes a nil outer keyboard as [], preserving input values, inbound Message/callback encoding and malformed inner rows. The first lint failure was import grouping only; original file/manifest and failing log are retained. Final physical telegram package tests, vet and pinned lint all passed. Fresh independent Code QA is clean by source inspection; its inability to run tests is explicit, and root's successful runs are separate evidence.

Canonical unchanged Dockerfiles built app754e15f7669ec67345e2f37606a4f2a1e216c56b6dfe4886a53e10ea5db08009 and evaluator432dff19ee8a06ee4b23c2995c31e328b8800d8e1c85c9ad3d9f86fdc5e924e6. Source verified before/after. Sole author-stand operator authorized app-only upgrade preserving DB, issuer, strict helper6, provider and old evaluator, with explicit mixed binding. A fresh independent author Functional QA agent has requirements/public contracts only and waits for operator release. No overall acceptance or readiness increase yet. Envelope-only helper correction is reviewed but not deployed.

Candidate11 author-stand update completed with unchanged dependencies/configuration and no operator DB writes or ingress replay. The upgrade wrapper timed out (exit1 retained); an independent read-only check verified exact image, health200 and current state. Pending update20000012 drained, queued20000013 produced a saved reply, and proposal3 remained pending_review/v3 with one consent. Evidence: repair8/stand/candidate11-upgrade/run-20260928T104200999Z. Fresh author FQA lease released. A separate fresh source/privacy reviewer is assigned source variant19854; rejected batch and price mutations explicitly excluded, preserving the unresolved authorization boundary. Percentages unchanged.

Two obsolete diagnostic-only images for retirement260 were removed after verifying zero references across running and stopped containers and preserving all1452 source hashes, recipes and evidence. No containers, volumes, database, shared dependencies or active QA images were removed. Active5 remained originalbac8… before/after. Exact IDs and checks: repair5/retirement260-diagnosis/cleanup/RESULT.json and PRESERVATION.json.

Source-only QA observed unexpected app exits without OOM. Investigation found a strong source/timing match to the QA proxy's one-minute authenticated idle eviction closing the bot's advisory-lock session; exact live Ping error was redacted, so attribution remains an inference. Report: repair11/source-exit-diagnosis/REPORT.md. Existing proxy tests prove idle eviction, not the full composed incident. Direct connection to the same isolated PG is authorized for source-only QA, which excludes wire fault/partial-batch injection; permission variant stays unchanged. Operator must preserve original queued inputs and use fresh running/health checks at release. Earlier restoration release used an older health check; that limitation is explicitly retained. Stage D now explicitly includes loss of its admission/lock session during owned work, safe replacement and durable recovery; no production lifecycle fix is claimed here.

Source-only direct-PG binding released11:07:16Z with same app754e15…, same isolated PG container/volume, DB/role/credentials/TLS and all non-route configuration. Only DSN host/port changed from127.0.0.1:15433 to postgres:5432. Original20000006 admitted after startup and finishedready/inbox0; current health200 and State.Running=true checked at release. Permission variant unchanged. Evidence: repair5/stand-isolated/variants/source/direct-pg-binding/. This removes the unnecessary proxy from the current scope; neither incident-cause proof nor overall Functional QA acceptance is inferred.

Candidate11 author publication scope PASSED fresh independent Functional QA, including explicit eligible foreign-author consent check G10 (still awaiting_submission/v2/consent0 before and after product update20000061), EN/RU mouse/emulated touch, separate approve/reject, restart/exact replay, permission withdrawal/restoration, parent/descendant source retirement and fresh-card fallback. Earlier foreign54 tested only stale state and is correctly limited. No actionable defect. Binding app11/evaluator10 explicitly retained; no real Telegram/physical touch or whole-B acceptance. Own34 provider and7 assessment controls cleaned. Source/privacy and interrupted recovery remain open. Report repair11/functional-author-fresh/report.md.

For the deferred batch, new read-only evidence proves exact seed-generated recipients and dedicated local PG/provider/Telegram routing; no actual charge/transfer path. Original reviewer reactivated to seek renewed automatic review for the same UI action, with prior rejection disclosed and no API/SQL substitute. Another rejection will stop it. No mutation is inferred from evidence gathering. Target evidence: variants/permission/authorization-evidence/ (source-blind reviewer receives only sanitized binding/target files, not developer analysis).

Deferred permission batch: the original reviewer prepared a guarded one-click rendered Playwright script because CUA had no available browser surface. Root inspected SHA2171E5D4602A2E8C7CE529640CB81BFFDF6FA2E3885111FD16C11A45F37173CA. One explicit escalated automatic review disclosed the earlier denial and new seed/route evidence. It rejected before process creation, requiring exact mutation approval and calling CUA→Playwright an unauthorized computer-use bypass. No further attempt or alternate route is permitted pending the requested user decision. Operator removed lock1414 and stopped watcher98528; app start unchanged, recipients2/1/2 at price1000, matchingreceipts0. Exact permission request sent asynchronously; source QA and other work continue. Original and renewed denials remain preserved.

Daniel explicitly approved the exact permission-variant UI batch through Playwright, including eventqa-b5-core-a, recipients930301/302/303, solo2718, comment RECOVERY10 ORDINARY 20260928, and stop/recovery after the first item: «Да, разрешаю этот тест через Playwright». This resolves the two conditions stated in the latest automatic rejection. Original independent reviewer and sole operator reactivated for the same reviewed action and new evidence run; earlier denials/disarms remain intact. The permission item is removed from pending user questions. No acceptance follows merely from authorization.

### 28 сентября — разрешены оба независимых QA-потока кандидата11

Даниил явно разрешил exact synthetic batch через Playwright на localhost:19844, затем независимо source/privacy QA на localhost:19854: synthetic source11fresh index/documents, просмотр и скриншоты тестового чата, удаление созданных QA источников, held-plan/replay. Прежние автоматические отказы сохранены; после нового разрешения оба reviewer продолжают прежний scope на отдельных стендах.

Batch: оригинальный UI update20000008 выполнен один раз; первый получатель сохранён с ценой2718 и одним receipt, запись второго заблокирована управляемым locker. Приложение остановлено до снятия блокировки и восстановлено на том же образе. Первый результат сохранился, остальные ещё not_attempted; публичное продолжение и отсутствие дублей проверяет независимый QA. Это промежуточное доказательство, не закрытие B.

### 28 сентября — проверена подготовка интеграции кандидата11

Read-only preflight подтвердил все1476хешей кандидата11 и все1222хеша принятого B1 в основном platform: drift отсутствует. Git-visible инвентарь содержит1228путей; overlay кандидата добавит272 и заменит301. Из28путей, отсутствующих в кандидате,20 — прежние bot client-файлы с преемниками в appclient,8 — отдельные QA/config-файлы, которые сохраняем. Source product не изменён; копирование и удаление до завершения приёмки не выполнены. Точные before/after hashes и карта путей: qa.local/architecture-stage-b-repair11/integration-preflight/20260928T113357Z/.

В recovery QA после успешной фиксации partial commit и первого restart приложение повторно вышло с exit1/OOMfalse до приёма navigation input10. Разрешено same-image восстановление через прямую ту же PG после сохранения failed-state evidence и подтверждения отсутствия активных fault/lock/watchers. Первоначальное доказательство остановки получено через proxy; последующее продолжение на direct-PG будет отмечено отдельно. Причина выхода не объявлена исправленной.

### 28 сентября — механизм proxy idle eviction и discovery D-002

Изолированный developer reproduction с реальной PostgreSQL17 подтвердил: прямое соединение сохраняет advisory-lock после650ms простоя, frozen proxy10 с IdleTimeout250ms закрывает исходную сессию; её Ping завершается ошибкой, lock освобождён и доступен второй сессии. Оба subcase PASS, процесс49698 exit0, собственные container/volume удалены. Действующие QA-стенды не затронуты. Это доказательство механизма прокси, не точная атрибуция прежних incidents и не проверка полного Bot.Run. Доказательства: qa.local/architecture-stage-b-repair11/lock-session-reproduction/REPORT.md. Композиционная проверка потери lock-session и владения незавершённой работой остаётся в D.

Публичный passes.operations вернул независимому reviewer две неразличимые операции одного вида. Source inspection подтвердил ID/tool-only формат. Ограничение D-002 перенесено в C2: bounded authorized summaries из того же coordinator/store и выбор нужной операции публичными tools без operator/SQL mapping. Recovery при внешнем сопоставлении не засчитывается как успешный discovery. Также исправлена устаревшая оценка в architecture-refactor-plan.md:12–21день архитектуры,27–53всей цели,35%выполнено/15%принято, без повышения готовности.

### 28 сентября — source/privacy/profile Functional QA завершён

Прочитан итоговый независимый qa.local/architecture-stage-b-repair11/functional-source-fresh/REPORT.md: scoped PASS на app11/evaluator10/direct-PG. Подтверждены retirement приватного документа, canonical event retirement при held provider plan, отсутствие profile mutation после удаления, exact captured replay без нового model call/эффекта, неизменность первого committed profile receipt, новый разрешённый запрос, фактические EN/RU mouse/emulated-touch и rendered завершение. Исходный blocked report сохранён отдельно, два выхода приложения до direct-PG не превращены в PASS.

Отдельный исходный requirement обычной регистрации из shared source ещё не проверен. Ему выделен fresh no-history reviewer ordinary_registration_fqa и отдельный synthetic stand под sole operator; scope записан в repair11/ordinary-registration-scope.md. Это не расширение цели и не повтор закрытого profile slice. Batch reviewer сообщил о завершении трёх элементов через публичный resume с неизменным первым результатом; repeat/permission/version ещё выполняются, общий B остаётся открытым.

### 28 сентября — подготовлен guarded integration dry-run B

repair11/integrate.ps1 проверяет принятую базу1222, frozen manifest и каждый исходный hash, отказывается перезаписывать неизвестные локальные файлы, сохраняет before-копии и удаляет только20явно перечисленных прежних bot client-файлов. Dry-run завершился exit0:571запись,20удалений, без изменений продукта. Apply не выполнен и закрыт до root acceptance record с обоими QA.

Выявлены2Python bytecode-файла в физическом freeze1476; они ignored/untracked в основном Git. Frozen QA inventory сохранён, source projection для интеграции содержит1474пути и исключает эти2кэша. Первоначальные preflight301replacement включали2различающихся кэша; реальные product/source replacements299 и additions272. Восемь независимых main-only QA/config-файлов сохраняются. Это уточнение инвентаризации, не новый runtime candidate и не рост готовности.

### 28 сентября — параллельный C1 draft и разрешение QA clone

Чтобы остаточная приёмка B не останавливала всю разработку, runtime_stage_d_preflight выделен отдельный qa.local/architecture-stage-c-draft/source с exclusive ownership первого C1 checkpoint: локальные PassBooking read/ExecutePassBooking command, live authorization, combined-app wiring, focused tests. Frozen B/main/QA-стенды не меняются. Это начало черновой реализации, не принятие B/C и не увеличение процентов; до freeze/integration нужна сверка с окончательным B.

Автоматическая проверка отклонила author app stop+DB clone до выполнения. Пользователь явно разрешил остановку и копирование zns_b8_author_canonical074→локальная zns_b11_registration_replay. Sole operator stage_b_fixture_builder повторно активирован; прежний оператор снят с stop ownership, свежий QA ждёт neutral release. Зависимая команда до ответа не выполнялась, обхода отказа не было.

### 28 сентября — ordinary partial-batch durable recovery подтверждён

Прочитаны approved-run/report.md и comparison.json независимого candidate10_recovery_fqa. Настоящая остановка после первого commit, same-image restart, публичный passes.resume и повтор завершились:3receipts→3receipts, first outcome и полные batch/bookings/receipts структурно неизменны, repeat inbox0/savedready. Current public target read подтвердил сохранённую цену/версию/assigned_at. Это limited durable PASS с proxy-boundary/direct-PG continuation, не full B.

Ограничения сохранены: availability exit1, operator-assisted operation mapping (D-002), фактический EN productlocale и native UI coverage. Карточка другого пользователя не считается обязательной в собственном menu, если это вне public show contract; timeout и callback17 наблюдаются отдельно, pending не объявляется дефектом. Permission/version сценарии ещё требуют отдельных состояний. C1 draft теперь содержит сверенную копию1474source paths и конкретное exclusive file ownership разработчика; новые проверки пока не завершены.

### 28 сентября — released registration stand и разрешённые recovery clones

Ordinary registration stand released11:58:18Z: UI19874/app19873/ownPG19875, same app11/eval10, canonical075, genuine actor303 capabilities200. Approved author app stop/snapshot/restore завершены exit0, freshreviewer получил neutral ACCESS. Первоначальный startup до readiness dependencies неуспешен и сохранён; тот же image перезапущен после готовности adapters. Реальные сервисы не затронуты.

Две несовместимые recovery проверки требуют отдельных DB. Автоматическая проверка отклонила stop/copy освобождённого permission stand до выполнения. Пользователь явно разрешил остановку zns-b9-recovery-permission-app-1 и копирование zns_boundary_b9_permission_canonical074 в zns_b11_recovery_permission и zns_b11_recovery_version, с local snapshot в repair11/recovery-variants/private. Sole operator возобновлён после ответа. Registration QA и C1 продолжались независимо; обхода отказа не было.

Ordinary recovery UI уточнён: после подтверждённого callback17 отображается актуальное собственное меню. Прежний chooser был pending, не дефект. Public target read отдельно доказывает данные получателя; show по контракту показывает собственное меню. Scripted-show timeout и непроверенный EN productlocale сохранены. Остаток — отдельные permission/version scenarios, не повтор ordinarybatch.

### 28 сентября — явное synthetic именование ресурсов

По указанию Даниила новые тестовые БД именуются synthetic_qa_zns_<scenario>, Compose/container — synthetic-qa-zns-<scenario>, где формат допускает. Название не заменяет доказательства искусственных данных и разрешение. Правило добавлено в docs/code-quality.md; активные стенды не переименовываются посреди QA. Operator подтвердил: pending recovery targets ещё не созданы и restore не начат, поэтому теперь это synthetic_qa_zns_b11_recovery_permission и synthetic_qa_zns_b11_recovery_version; связь с ранее явно одобренными targets сохраняется. C1 developer также получил правило для disposablePG.

Registration QA после явного разрешения всего Playwright сценария на19874 снова получил generic automatic denial «JavaScript execution did not receive approval» на actor/lang действие. Зависимые UI-действия остановлены, обхода нет; provider helper отдельно запущен после transparent escalation, но сценарий не вводился. Пользовательское разрешение уже есть, повторный такой же вопрос не требуется; проверка остаётся blocked/unexecuted, разработка C1 и отдельные recovery stands продолжаются.

### 28 сентября — первые исполняемые C1 проверки и recovery release

C1 developer выполнил focused unit gate appclient/applicationauth/passbooking/cmd (exit0) и отдельный реальный PG TestLocalRegistrationHTTPParity (1.48s, exit0). Проверены local command→HTTP exact replay, одинаковые reads/invalid-command outcomes, cancellation и current can_book denial; одна domain operation. Собственный synthetic-qa-zns-c1 контейнер удалён. Lint/vet ещё выполняются; свежий независимый c1_boundary_code_qa назначен, ждёт source freeze. Это checkpoint, не полный C1/C и не main integration.

Recovery variants released: permissionUI19884/app19883 и versionUI19894/app19893, отдельные synthetic_qa_zns_b11_recovery_permission/version, sameapp11/eval10/directPG. Здоровье/identity303 подтверждены, сценарных мутаций при provisioning не было. Original recoveryreviewer реактивирован для двух оставшихся сценариев, soleoperator доступен через followup_task. Ordinary registration19874 остаётся blocked на node_repl approval; причина точнее установлена: отказ возвращает mcp__node_repl__js, не Playwright и не cua_repl, без rationale/requestID. Пользователю указан официальный /approve для одного конкретного повторения; факт override пока не получен.

### 28 сентября — C1 freeze и сбой предпосылки permission QA

C1 checkpoint: 1477 source paths; manifest cb63377bd6f413f84e146a1168ffffefbe68c6fb8efd40d1fb3146d4aa95cc1f. Root проверил manifest; итоговые unit/vet и pinned lint exit0, focused real PG PASS. Свежий независимый Code QA получил подтверждение freeze; основной состав не изменён. C4a готовится отдельно без записи в frozen source.

Permission batch40000001: первый элемент v3→v4/3141 committed, два остальных source_stale до ожидаемой блокировки. Права не отзывались, приложение не останавливалось, locker снят. Это провал предпосылки проверки, не доказательство permission recovery. Независимый отчёт functional-recovery-resumed/reports/permission-report.md; разработчик отдельно устанавливает причину. Version QA продолжает свой изолированный сценарий. Регистрационный QA19874 по-прежнему ждёт изменения approval-контекста, повторное разрешение не предполагается.


### 28 сентября — C1 Code QA и начало C4a

Независимый c1_boundary_code_qa подтвердил scoped checkpoint без actionable defects: auth/owner, domain transactions/replay, HTTP fallback, восемь changed paths. Ограничения: настоящая issuer matrix и composed Telegram-like Functional QA ещё не выполнены. Root прочитал отчёт, сверил execution evidence и подготовил successor architecture-stage-c-working/source: все1477 hashes совпали с C1, baseline-manifest сохранён.

C4a developer isolated_fqa_stands назначен единственным владельцем agenthost и точно перечисленных bot context seams в successor. Реализация по architecture-stage-c4a-preparation/PROPOSAL.md: реальная сборка/refresh context, typed admitted input, transport-neutral ports, сохранение before-provider reauthorization и budgets. Frozen B/C1 и main не меняются.

Отдельный version batch50000000 также остановился на source_stale после первого commit, до external edit. Независимый итог residual QA — NOT ACCEPTED (functional-recovery-resumed/reports/report.md); locks сняты, app не останавливались, leases освобождены. Прежний ordinary recovery scoped PASS не расширяется.


### 28 сентября — D-003 и два параллельных C владельца

Разбор frozen11 подтвердил source self-invalidation: первый assignment меняет собственную versioned authority, следующий item повторно проверяет immutable pre-version. Canonical before/after receipts позволяют C2 доказать только собственный successor под прежними locks. D-003 фиксирует причину, ограничения, безопасное исправление и обязательные исходные/негативные проверки; это перенос по правилу Даниила, не PASS B.

На одном проверенном C-working successor два disjoint writer: isolated_fqa_stands — C4a agenthost/bot context; runtime_stage_d_preflight — C2 derivedmutation/pass_batch и новые receipt/test helpers. Root резервирует appclient/API/runtime/cmd composition. C2 сначала воспроизводит дефект на realPG, затем исправляет; публичная видимость committed result остаётся отдельным C2 seam. Frozen B/C1 неизменны.

### 28 сентября — исполняемое воспроизведение D-003 и C4a код

C2 TestDerivedPassBatchReceiptSuccessor воспроизвёл дефект на отдельном synthetic PostgreSQL: первый эффект succeeded, второй rejected вместо succeeded (1.65s; architecture-stage-c2-successor/red/pg-result.json exit1). Это ожидаемый RED до исправления; прежняя ошибка подготовки fixture сохранена отдельно и не подменяет доказательство. Disposable PG удалён.

C4a уже перенёс AddSupporting/Refresh и небольшие projection policies в internal/agenthost; root прочитал новые границы, typed admission и оба актуальных caller. Developer native/vet прошли; lint/PG ещё выполняются. C1reads добавил четыре typed local read маршрута; focused unit exit0 (architecture-stage-c1-reads/evidence). PG ещё не выполнен: первая попытка остановилась на sandbox Docker pipe до compilation. Для исключения конфликтов с незавершённым C2 каждый профильный gate получает snapshot frozenC1 плюс точные изменения владельца; общий состав проверяется отдельно после согласованного freeze.

Оба освобождённых recovery runtime остановлены: app/evaluator/provider/media/Drive/Telegram helpers exited, PG/данные/evidence сохранены. Shared issuer и ожидающий registration19874 не затронуты. Точный inventory: repair11/recovery-variants/cleanup-status.md. C1 knowledge boundary отдельно исследуется без source writes; не является завершённой работой.

### 28 сентября — C4a локальные gates завершены, C4b начат

C4a проверен на отдельной копии frozenC1 +12 owned deltas: 1479файлов, manifest и owned-binding-check в evidence-c4a. Native/vet/pinned lint0 и11real-PG tests PASS (71.406s); disposablePG удалён. Свежий c4a_context_code_qa читает только frozen check-c4a-source и baseline. Это не Functional QA всегоC. Разработчик продолжает C4b в изменяемом successor, не в проверяемой копии.

C1reads realPG TestLocalRegistrationReadsHTTPParity PASS1.60s: сравнение с HTTP, 26-row pagination, current permissions и owner isolation. Unit/vet также прошли; lint нашёл только test-package naming/shadowing, исправления и rerun отдельно. Первые sandbox/fixture failures сохранены, не засчитаны как тесты продукта.

C2 GREEN40293 выполняет новую negative/resume матрицу на отдельной 1480-file копии, первоначальный RED сохранён. Knowledge developer получил исключительное владение appclient knowledge/host/client и combined wiring, плюс один bot/memory_tools caller; C4b/C2 файлы не пересекаются. Общий source freeze и итоговые gates впереди.

### 28 сентября — C1 read gates завершены

Четыре локальных read операции прошли final unit/vet/pinned lint0; PG parity ранее прошёл на тех же product/integration bytes. Исправления lint затронули только имя unit test и shadow variables. Точная snapshot binding: architecture-stage-c1-reads/check-manifest.json SHA25638c6713fb0f5baf268869bfefa2b3cc963b8696947f0d79e7d88ddff2973aa44. ПолныйC и независимый Functional QA этим не приняты.

Тот же developer продолжает JSON registration admin/takeover/payment/capability boundary в отдельных appclient files; binary proof/export и profile остаются явным остатком. Knowledge boundary developer добавляет actual combined composition test, сохраняя user/Host разделение. C4b получает только три дополнительных точных seams: createPlan из agent_quota.go, performReadTool dispatcher из script.go и historyFencedPlan из history_deletions.go; script reservations не затронуты.

### 28 сентября — независимый C4a PASS и C5a account

Свежий независимый c4a_context_code_qa: scoped PASS, zero actionable findings. Все1479candidate/1477baseline hashes проверены, точный12path delta. Прямых bot/Telegram зависимостей host нет, но транзитивные через core/metadata и прежние домены сохраняются; полная архитектурная независимость ещё не доказана. Отчёт architecture-stage-c4a-code-qa/REPORT.md. Composed Functional QA не выполнен.

C5a параллельно получил собственную копию frozenC4a: stage_b_fixture_builder владеет только architecture-stage-c5-account/source. Утверждены реальные переносы preferences/language transaction/trusted metadata в account.Service, transport-neutral sender/projection values и обновление actual API/client/derived/runtime/bot consumers. Old aliases не сохраняются, workflow остаётся отдельным C5b остатком. Это изолированная разработка, не main integration; merge и совместные gates впереди.

Knowledge native snapshot gate46218 прошёл (appclient/api/cmd); bot URL helper memory_client.go стал неиспользуемым после typed callers и удаляется по проверенному rg. C4b first compile идёт отдельно. C2 strengthened PG пока ещё уточняет ожидаемый source_stale для nested causal history-deletion fixture: исходные неудачные assertion runs сохранены; до final receipt укреплённая матрица не объявляется PASS.

### 28 сентября — C2 независимый Code QA и следующие PG gates

Встроенный лимит agent threads не позволил создать нового reviewer. Запущен свежий ephemeral Codex CLI в read-only режиме по отдельно разрешённому пользователем способу; первоначальный sandbox запуск завершился до review («Could not find home directory»), обычный профиль через прозрачную escalation запустил сессию. CLI5727 terminal0, независимый scoped verdict clean: все1480candidate/1477baseline hashes и4path delta проверены. Отчёт architecture-stage-c2-code-qa/REPORT.md. Это не Functional QA и не полное закрытие D-003.

C1 registration operations:7JSON methods прошли unit/PG/vet/pinnedlint0; replay/current grants/payment-role isolation проверены,8ownedpath hashes совпали. Разработчик продолжает typed registration navigation/history; profile/binary/export остаются открытыми. Knowledge native и PG прошли (1.53s), final composition/error tests и lint corrections ещё выполняются.

C4b вынес TurnHost/model/AV/read loop в agenthost; focused native/vet и19named realPG tests прошли (64.911s); последний lint нашёл только err shadowing, исправление/повтор впереди. C5a account extraction в отдельной копии прошёл native/vet и11focused realPG tests плюс5 derivedsettings subcases (9.282s); account/broadcast rollback proof включён. Scopedlint, независимый QA и reconciliation ещё впереди. Disposable PG обоих этапов удалены.

### 28 сентября — локальные gates C4b/C5a/knowledge/navigation завершены

C4b: final host tests и pinned lint0; frozen check-source1483, manifest049884101c5cc8bd37a0284023b91f093c0f62a19d04858de3beedc90784b7e5. Knowledge: final affected appclient lint0 после constant-only правки; frozen1480, manifest799a8a08c8c993703a85ca4286bcfc727c1c501671054b08179f347e7e0f1751. C5a: scoped lint0,26deltas, frozen1480 manifest54023fa57367f044e9d7d6a8e31cf62cc8ef961fc0ff6f022e95e65cfef52ff2. Registration navigation: native/vet/PG/lint0; frozen manifest1D0BFDC3C5039D968F64C3C239486C5472E53D5CB36A99E1B70841BBD0FA5E7B. Подробности и ограничения сохранены в отдельных RESULT/REPORT, это не stage acceptance.

Запущены новые независимые read-only Codex CLI reviewers для C4b и knowledge (42319/79878); им переданы исходные требования и замороженные source/baseline без предыдущих reviews/диагнозов. C5a подготовлен к отдельному fresh review. Root сохраняет composition ownership. C4c начинает план переноса script/reservation ownership; C5b изолированно продолжает workflow extraction; registration developer получает profile boundary, бинарные операции отдельно.

C2 operations реализует bounded discovery и подтверждённые canonical receipt statuses без нового хранилища. После retirement источника допускается только текущим domain-authorized владельцам opaque ID/time/status без исходного контекста и без resume; отзыв action/event/target прав скрывает запись. Cached summaries должны повторно проверяться и монотонно редактироваться; прежний Run.Result не восстанавливается. D-002/D-003 остаются открыты до полного доказательства.

### 28 сентября — knowledge Code QA и следующие изолированные переносы

Fresh knowledge Code QA завершён terminal0: clean scoped verdict;1480candidate/1477baseline entries и19delta paths проверены независимо. Отчёт architecture-stage-c1-knowledge-fresh-code-qa/REPORT.md. Ограничения: supplied execution logs не доказывают криптографическую привязку самого исполнения, live IdP/полная concurrency/FQA не выполнялись reviewer. Совместные stage gates остаются обязательными.

Root добавил Profile: services.PassProfiles в combinedClient рабочего C; разработчик профилей включает точный initializer delta и проверку DB identity в изолированный snapshot, без остальных текущих правок. C5b переносит workflow в отдельном successor C5a. C4c получил65existing/13new exact paths только в изолированной копии frozenC4b, без aliases; C2 bot delta будет перенесён после coherent checkpoint, knowledge/account composition также сверяется явно. Общая C-working сборка пока не заморожена.

### 28 сентября — account интегрирован, C4b architectural finding сохранён

C5a fresh independent Code QA terminal0, clean scoped verdict;1480candidate/1479baseline и26delta hashes проверены. Все manifests native/vet/PG/lint имеют одинаковые path/hash entries. Reviewer отдельно отметил непроверенные исполнением TelegramMetadataHostBoundary и TelegramMetadataWithSplitBotRole — добавить в composed proof. Отчёт architecture-stage-c5-account-fresh-code-qa/REPORT.md.

Root повторно проверил26target baseline hashes, сохранил pre-integration-backup и применил4added/19modified/3deleted paths в C-working; все target hashes совпали с C5a. working-integration-result.json подтверждает копирование, не stage acceptance. Main platform и frozen QA copies неизменны.

C4b fresh Code QA terminal0: поведенческих регрессий не найдено, один P2 inherited architecture finding — agenthost→core→telegram через metadata. Account extraction удаляет именно этот import path; полная production dependency closure после композиции ещё проверяется. Reviewer сохранил limitation финального PG binding:4files после PG отличаются от final manifest. Developer post-pg-adjustments фиксирует только formatting/setup variable renames; это не заменяет final composed PG. Отчёт architecture-stage-c4b-fresh-code-qa/REPORT.md. C4b не объявлен clean/full accepted до закрытия архитектурной границы и общего proof.

### 28 сентября — проверка полного графа agenthost после account

Root выполнил production go list -deps ./internal/agenthost в C-working после account integration. Проверка НЕ прошла: остался путь agenthost→agent→api→identity→telegram. Точный граф сохранён в architecture-stage-c-working/agenthost-dependency-paths-after-account.txt. Account устранил прежний metadata edge, но один удалённый import недостаточен для полного закрытия P2.

Назначен отдельный narrow repair developer в architecture-stage-c4-transport/source (frozenC4b+точныйC5a delta): agent.go/openai.go используют application API только ради JSON HTTP helpers. Цель — сохранить ответы/лимиты scripted/model endpoints, убрать этот обратный production import без нового framework; подтвердить closure, focused gates и независимым review. C4c/C5b продолжаются независимо; final C closure будет проверен повторно после композиции.

### 28 сентября — C1 profiles/C5b готовы к review, оставшийся C scope

Profiles: unit/composition/PG/vet и final pinnedlint70171 прошли. Final manifestF424D41949980560F775C9E50CC453692B5C792FF18E4E17963DE5D9A897D1A8; после PG только testlocal shadow rename. Fresh cumulative C1 registration reviewer15115 запущен на profile snapshot относительно начального C1: читает все накопленные delta, без developer reports/истории. Binary/export остаются следующим slice, named batch/tier и фактические raw bot callers также не завершены.

C5b mainlint1794 и finalcore91763 завершены0; финальный snapshot1481/32paths a35db20b1d4a1ace872aae7b388da99b904c0b286f1fbb09fea002db11ad3774. Единственная поздняя правка — удаление неиспользуемого private core.fail; core tests/vet/lint повторены. Fresh independent workflow Code QA31530 запущен, источник заморожен.

C2 operations первый PG набор10.670sPASS: статус canonical commit при потерянном источнике без контекста; отзыв actor/target прав скрывает list и запрещает exact read; отсутствие receipt/чужой owner, выбор из двух операций, local/HTTP parity проверены. Расширение command/assignment/cancel/uncouple и public discovery продолжается. Один старый test discovery после отзыва grant требует изменения ожидаемого значения на пустой список по прямому пользовательскому требованию; durable receipt/effect assertions сохраняются.

Transport dependency repair изолирован: исходный HTTP contract test прошёл, после правки agent+agenthost tests/vet и go list closure прошли (нет api/bot/telegram/appclient/runtimeapp). Pinnedlint и fresh Code QA впереди. C4c завершил pure-type compile checkpoint, переносит настоящий controller/registry/store без aliases. C3 выделен разработчику только в отдельной копии: grounding/durable assessment/original-source attachment должны перейти в coordinator; этап не заменяется уже выполненной local boundary.

### 28 сентября — workflow/transport composition и findings C1

C5b independentCodeQA31530 terminal0, clean32path scope. Root применил30baseline-matching paths и сохранил текущие изменения в двух пересечениях (bot.go,appclient/derived_mutations.go); ещё3newC4b consumers переведены core.Workflow/Slot→workflow. Pre-integration backup, audit и working-integration-result.json сохранены. Исходный main/platform не меняется; текущая Cworking композиция остаётся промежуточной.

Transport independentCodeQA94468 terminal0, clean4path scope. Root перенёс точный Handler/helper delta, сохранив workflow types. Повторный go list production closure в C-working прошёл: нет api/bot/telegram/appclient/runtimeapp. Diff относительно C5b подтверждает только согласованные HTTP helper изменения. Scoped composed native gate запущен отдельно; полные final C gates/FQA впереди.

Cumulative registration CodeQA15115 terminal0,25changedpaths verified,2P2: unbounded catalog field/aggregate bytes и unbounded optional command identifiers до lock/hash. Отчёт architecture-stage-c1-registration-fresh-code-qa/REPORT.md. Назначен отдельный domain-bound repair в замороженной копии: UTF8/NUL/200byte identifiers до транзакции, existing1MiB catalog resource limit с предварительной SQL защитой materialization и final aggregate size check. RED/PG/effect absence proof +freshreview обязательны; review не объявляется clean.

C3 executable plan утверждён в собственном frozen1512 baseline, reusable developer реализует coordinator/durable assessment/existing Store; manual consent отдельный. C4c движется в своей копии, новые exact loader/test seams разрешены с сохранением assertions. Binary local/HTTP proof/export gates завершены; проверка named capability/tier readers и фактических callers остаётся.

Composed native27188 завершён0: agent0.799s,agenthost0.179s,appclient0.238s,bot0.373s; workflow компилируется, собственных unittests нет. Лог architecture-stage-c-working/workflow-transport-native.log. Это текущая рабочая композиция, не финальный frozen stage proof. Два слота переданы C1 toolreads и C2 native→PG; C4c/C3/bounds продолжают код отдельно.

### 28 сентября — расширенные C2 gates и новые coordinator checkpoints

C2 snapshot5 native/vet прошли; realPG42912 прошёл34.632s, включая фактический public passes.operations выбор двух однотипных операций, resume по выбранному reference, current permissions/history/receipt statuses. Scopedlint сначала остановился до анализа на cwd access denied; повтор того же runner с разрешённым доступом31009 выполняется, исходная ошибка сохраняется отдельно. Это не приёмка полного C2: после discovery остаётся настоящий RegistrationCoordinator для grounding/manual-agent-execute/resume и исходный fresh FQA.

C3 native/vet64063 прошли. PG21985:22существующих выбранных случая прошли, новый interruption test провалился из-за несовпадения fixture fact_key с reused assertion helper. PG удалён, failure сохранён. Product не менялся; focused retry после исправления test fixture. C4c concrete ownership checkpoint компилируется, live-registry/restart tests добавлены, финальные native/vet/lint/PG впереди.

C1 toolread PG обнаружил неверное ожидание нового теста: tariff access сохраняется у exact-event payment admin после удаления global role. Тест исправлен согласно исходному domain contract; проверяет разрешение пока event role есть и отказ после его отзыва, cross-event denial сохранён. Product не менялся, повтор ещё ожидается. Root заменил ручной sendPassTier raw URL на named PassTierStatus; регистрационный owner включит exactcaller delta в proof. RuntimeBatch POST пока остаётся отдельным обязательным typed seam через derived.RunManualPassBatch, без обхода original derivation.

C1 bounds repair получил свободный слот для исходного RED unit/PG. Общая приёмка и проценты не изменены на основании промежуточных результатов.

### 28 сентября — пересмотр процентов и сокращение B по просьбе Даниила

Архитектурная реализация35%→55%; принято15% без изменения. Функциональные92%/78% без изменения. Сохранены веса A5/B30/C30/D20/E15; C оценивается примерно70% реализации (внутренняя оценка73% округлена вниз из-за композиции), поэтому5+28,5+21=54,5%. Отдельные Code QA и локальные gates не поднимают полную архитектурную приёмку. Методика и остаток обновлены в docs/readiness-estimate.md.

Остаток25–50 инженерных8часовых дней вместо27–53: архитектура10–18 (B1–2,C4–7,D3–6,E2–3); функциональность/импорт/финальная приёмка/резерв не урезаны. Исходные recovery дефекты не закрыты и не удалены. В PROGRESS B1–B4 объединены в одну строку; убраны повторные подробности кандидатов/стендов, сохранены ссылки на планB, DEFERRED и исходные отчёты/историю.

### 28 сентября — промежуточный независимый архитектурный аудит C

По прямому запросу Даниила запущен отдельный read-only архитектор во время продолжающегося рефакторинга: qa.local/architecture-mid-c-audit, CLI44627. Scope шире проверки выполнения плана: оценить реальные границы/владение, layering, избыточные interfaces/typed unions, транзакции/receipts, приватность/авторизацию, расширяемость и стоимость QA; предложить дополнительные архитектурные изменения, которые выгодно включить сейчас. Нужны обоснование, стоимость/риск, проверка результата и разделение now/D–E/после. Это не обязательный Code/Functional gate; разработка не заморожена. Report ещё не получен.

Root composition6file lint fixes переданы владельцам точными текстовыми delta/hash (не whole-file overwrite). C1 toolreads завершил gates, independent binary/tools reviewer94548 запущен. Ручной batch теперь подключён через Client.RunPassBatch и injected derivedmutation.Service.RunManualPassBatch, preserving stored source; rootinitializer/caller проверены и переданы владельцу для isolatedproof. Не объявлено принятым до этих gates.

### 28 сентября — результаты C1 и финальные проверки срезов C

Свежий независимый binary/tools Code QA завершён: два P2 в typed tool reads — локальный TierStatus не ограничивает размер ответа, HTTP fallback может вернуть частично декодированный результат вместе с ошибкой. Binary proof/export, текущая авторизация, owner/attempt/version binding и event-bound export не получили замечаний в проверенном объёме. Отчёт: qa.local/architecture-stage-c1-files-tools-code-qa/REPORT.md. Исправления выделены отдельно; включают zero-on-error для нового ручного batch boundary. Полной C1-приёмки нет.

Manual batch frozen7DF5EC8B92535DC81BBC328606F15E0038FE5444AECBEDEDEB2B612C19E40EF2 прошёл native/cmd, PostgreSQL и vet (1684, terminal0): сохранение original source, отказ после source retirement, replay, EN/RU. Pinned lint и независимый review ещё впереди. Domain catalog/command bounds прошли финальный lint77971 с0issues после ранее выполненных RED/GREEN unit/PG и vet; требуется frozen handoff и свежий Code QA.

C3 и C4c scoped lint выявили два общих замечания в заимствованном C2 privacy helper; исправление handoff7 сохраняет политику и выделяет существующие проверки в функции. C2 snapshot7 проверяется отдельно, затем C3/C4c повторяют affected gates. C4c PostgreSQL прошёл14выбранных сценариев за50.002s, native/vet также прошли; это не общая C-приёмка.

### 28 сентября — решения по промежуточному архитектурному аудиту

Архитектор44627 завершился terminal0. Отчёт сохранён в docs/architecture-mid-c-audit.md. Приняты дополнения C: убрать остаточную generic host-policy из Bot; ledger-owner выдаёт typed admitted-operation projection вместо знания nested script JSON в derivedmutation; сохранить interaction без обратной зависимости на agenthost; явный deferred assessment вместо provider error -> nil,nil. Полный registration coordinator, bounds/local-HTTP parity и реальный IdP остаются обязательными. SQL затронутых stores переводится в sqlc по мере стабилизации контрактов C/E; нет нового framework, universal repository или безусловной нормализации JSON. D/E lifecycle/importer не перенесены в C.

C2 получил разрешение на isolated coordinator successor с согласованием shared function deltas; C3 — на explicit deferred outcome successor; C4 готовит exact residual-policy scope после freeze. Domain bounds передан свежему независимому read-only CodeQA46768. Остаток26–53 вместо25–50 инженерных дней (C5–10 вместо4–7); текущие55%реализации/15%приёмки не повышены.


C1 bounds fresh CodeQA46768 terminal0: findings отсутствуют, manifests и5pathdelta проверены независимо. Root сверил все workingbaseline hashes, сохранил backup и интегрировал ровно5paths в C-working; результат qa.local/architecture-stage-c1-registration-bounds/working-integration-result.json. Source limits/SQL snapshot подтверждены review; exact equality/100events/concurrentcatalog и память PostgreSQL не измерены. Composed gates/FQA ещё впереди.


После интеграции bounds root выполнил go test ./internal/passbooking ./internal/appclient в C-working: оба PASS (0.898s/0.407s, terminal0). Это focused composition proof, не PostgreSQL/FQA всего состава. C3 extraction lint43268 завершён0issues, frozen dd8568... передан свежему независимому CLI CodeQA70440. Deferred-assessment successor остаётся отдельным исходником.


### 28 сентября — независимые C2/C3/C4 reviews и следующие структурные срезы

C3 fresh CodeQA70440 завершён с одним P2: inherited provider error превращается в успешный coordinator outcome. Остальной19path coordinator move и6borrowed changes без найденных дефектов; final PG-suite не доказан из-за различий manifests. Отдельный deferred successor исправляет явный outcome и добавляет request-time authority guard в реальные OpenAI/Codex/Remote листья; cancellation, pending proposal и прежние durable keys сохраняются. Новый coherent PG прогон должен закрыть source-binding gap; полный C не принят.

C4c final lint62892 terminal0, frozen37cb7e78284d2a79e1970f86b0235c94bba45c0edb7f57d956171548cdf3cb4b/1502files,100delta (82owned+18dependencies). Fresh read-only CodeQA4219 запущен. C4d successor разрешён по exact RESIDUAL-POLICY-PROPOSAL.md отдельно: generic evidence/redaction/authority/key/exposure policy выходит из Bot; script ledger выдаёт interaction-owned typed operation projection. Derivedmutation получает только явные domain parameters, без обратной зависимости на interaction/agenthost.

C2 final lint8 terminal0; frozen6cf4bb7e54f915a8dc5c71841e25928739f4560e558833c7b6623745adf78071/1514files. Native7/vet7 иPG6 привязаны через exact helper/EOL change chain. Fresh read-only CodeQA95732 запущен для16owned paths; baseline — настоящий c2-successor, а не post-change первый snapshot. Полный RegistrationCoordinator развивается отдельно; старые D-002/D-003 ещё требуют исходного Functional QA.

C1 manualbatch/read-repair последовательный lint97284 выполняется; новыеграницы host/history инвентаризируются отдельным разработчиком без конфликтов с C4d. StageD developer preflight только анализирует admission/shutdown/helpers и риски lock-session loss, продукт не меняет. Главный приоритет C сохраняется; полный goal не сокращён до проверенных срезов.

### 28 сентября — host/history и ограниченный D helper slice

C1 host/history inventory завершён:4typed user reads и6Host operations ещё шли через HTTP. Разрешён isolated slice по qa.local/architecture-stage-c1-host-history/INVENTORY.md: LocalHistory, общий conversation archive entrypoint вместо API-only sensitive/media redaction, domain-owned aggregate read budget до материализации JSON, именованный full-text caller. Host-capability и fresh user/owner authorization сохраняются. Root оставляет за собой runtime wiring; C4 DTO move согласуется отдельными function deltas.

D developer preflight записан в qa.local/architecture-stage-d-runtime-preflight/PLAN.md. Выявлены startup writers до Bot lock и незавершённый lifecycle hijacked evaluator sessions/children. Разрешена только isolated helper реализация в architecture-stage-d-helper: scriptservice lifecycle, service и command shutdown. Admission, launcher и общая composition пока не меняются. Advisory lock heartbeat/cancellation не объявляются transaction fencing; stronger replacement contract требует отдельного разбора. Это не приёмка D и не замена приоритета C.

C1 repair lint выявил3musttag замечания: nested Tier сериализовался с implicit legacy field names. Разрешены exact same-name JSON tags в passallocation/allocation.go с before/after wire proof; gates повторяются по затронутому коду, без suppression. C3 successor initial native провалился на неправильной обработке двух return values i18n; исправление и повтор на всех4затронутых пакетах, исходный failure сохранён. PostgreSQL до успешной сборки не запускался.

C3 deferred successor coherent manifest0f414ba02ab8bf5b3994ba7d1f3b46cecb0f3d9392e7d22bbaba8d01e448935c/1522files: native+vet34321 прошли agent/agenthost/interaction/bot; PostgreSQL75632 прошёл24сценария за56.654s, включая все прежние23 и новый outage/cancellation/same-key recovery через actualhost adapter. Cleanup выполнен. Lint и свежий review ещё впереди; общий C не принят. Root прочитал actual adapter: bounded deferred reason, свежий authority guard перед provider boundary, один request budget; Remote ограничение явно указано в плане.

Fresh C4c CodeQA4219:2P2 — late knowledge-read completion может восстановить scrubbed target/siblings (D-004), и generic policy/ledger choice claims ещё принадлежат Bot. Оба включены в C4d; required barrier RED/GREEN и move script_order_choice_store ledgerclaim. Существующий PG manifest отличается от final11paths, поэтому следующий C4d прогон привязывается к единому составу с исходными14 плюс operations/successor/barrier. C4c не принят.


C2 operations freshCodeQA95732:3P1 и1P2, source-derived безruntime reproduction. D-005summary provenance, D-006unpersistedbatch targetauth, D-007monotonicretirement; D-002 расширенsingle-target discovery. Точные corrective scopes переданыC2/C4d; фиксируютсявновыхowners, frozenoperations неaccepted. C1 read/manual cumulativefreshreview запущениз8b737e... послеgreenlocalgates.


### 28 сентября — подтверждённый privacy RED, финальный C3 и Linux helper

D-004 real-PG RED81035 воспроизвёл восстановление target и sibling после настоящего MemoDelete, чтения старого массива и завершённой reconciliation. Root прочитал failing assertions/raw result; это дефект сохранения приватности, не инфраструктурная ошибка. Исправление использует transaction-aware существующие memory gates и row serialization; тесты не ослабляются, добавляется no-deadlock proof.

C3 final warm lint84639 terminal0; frozen50ce44db365f209cc0b4c077cf61a5b73c8c472ff2d219d193c3fe937683301b/1522files. Native4/vet/24PG привязаны кabe70944 с единственной доказанной whitespace-only правкой; fresh cumulative CodeQA24670 запущен для coordinator+provider guard. Не является приёмкой полногоC. Освободившийся developer исследует D-001 cause/measurement plan read-only; frozenC3 не меняется.

D helper run-002 Linux go1.27.0: race tests scriptservice71.422s/scriptclient1.087s, helper/worker CGO=0 builds прошли, cleanup remaining[] подтверждён. Initial run001 compile failure нового test Shutdown receiver сохранён, исправлен только fixture. Narrow lint и actual entrypoint shutdown proof ещё впереди; D не принят.

C1 host/history native conversation/appclient/api/bot прошёл. Initial PG: старые affected history cases прошли, новые cases остановились на31-byte synthetic signing key при минимуме32. Исправляется только fixture, dedicated rerun; это не product failure и не PASS новыхcases. C2 coordinator development baseline d5dce297... собран с C4c+C5/transport+operations без aliases, compile ещё впереди.

### 28 сентября — C1 cumulative findings и focused privacy GREEN

Fresh C1 binary/read/manual review17423 подтвердил23pathdelta и нашёл2P2: TierStatus всё ещё materializes всеbookings/comments черезreadBookings приfewtiers; старые ExportAuthority/Upload HTTP returns могут содержать partialtypedresult. Авторизация, proof owner/attempt/version, manualbatch originalsource/receipts без замечаний в проверенномscope. Новый isolated successor заменяет read на bounded stats stream с общим аккумулятором allocator, а HTTP errors обнуляютрезультат; arbitrary bookingcount rejection не вводится.

D-004 GREEN34900 прошёл два barrier/concurrentdelete теста наfrozen5aaf375e; PGcontainer removed. Fresh narrow CodeQA56295 запущен безразмораживанияgreen-source. C4d продолжает genericpolicy иD-007 monotonicretirement. Rootфиксируетузкое доказательство, не общуюприёмку.

Helper actualentrypoint SIGTERM probe прошёл натомжеLinuxbinary489477948f35984a49c1ddd3a143c7c5539be4f217046bd65fab1a471c8a63c1: activeCONNECT child завершён, EOF; incompleteHTTPbody такжеотменён иEOF, child дляэтогосценария не создавался. Контейнерудалён. Первый narrowlint остановлен доанализа иззаWindowscwd AccessDenied; одинаковый scopedretry послеC1, безослабленияgates.

### 28 сентября — review successors и подключение history

Root сверил baseline hashes всех22 файлов host/history и перенёс их в development C-working с резервными копиями и root-integration.json. Подключены LocalHistory в combinedClient и Host через общий conversation.Service и текущий Authorizer; добавлен composition test повторной авторизации user/host чтений. Проверка этого состава запущена; основная platform и frozen QA snapshots не менялись.

Независимый narrow retirement Code QA выявил ещё P1/P2: completion может заменить уже retired reservation результатом с новым epoch и старым request text; delayed reconciliation может записать устаревший epoch поверх нового чтения. Передано C4d для отдельных ordering RED/GREEN; D-004 остаётся открыт.

C2 D-006 подтверждён RED51343 и GREEN70469: unpersisted operation теперь проверяет grounded targets до ответа. Три focused PG families прошли15.508s, synthetic container удалён. Это developer proof конкретного механизма, не приёмка C2. D-007 RED8307 подтверждает потерю ранее обнаруженного retirement при последующей инфраструктурной ошибке; GREEN пока не доказан.

C3 cumulative review выявил guard до блокирующего accounting и утечку read_authorities через anonymous result wrapper. Исправляются именованная model projection и проверка перед фактическим provider dispatch с явным not_sent для неотправленного запроса. Новые проверки должны подтвердить отсутствие вызова и фиктивного списания.

D helper frozen140e3f59 прошёл Linux race/build, actual SIGTERM probes и scoped pinned lint. Запущен fresh read-only Code QA60692; полный D не принят. Оценка PROGRESS приведена к26–53 инженерным дням.

History composition test47507 завершился PASS (cmd/zns0.190s). Review snapshot composition-source:1543files, manifestDB5A60F05C03C007AC40202D2F7F60EC019D2E436734DC38E2AF355490CB96B7; отличается от frozen developer slice только двумя cmd/zns wiring/test файлами. Fresh independent CodeQA19930 запущен; полная приёмка ещё не заявляется.

Fresh helper CodeQA60692 завершён без actionable source defects. Reviewer подтвердил4pathdelta и lifecycle ownership; отметил отсутствие собственной проверки whitespace-restoration и final lint source binding. Это scoped source review, не runtime/FQA acceptance. Report: qa.local/architecture-stage-d-helper-code-qa/REPORT.md.

C3 actual-provider accounting RED96641 воспроизвёл реальный HTTP dispatch после отзыва authority, пока Reserve/Dispatch ждал policy lock. GREEN90072 прошёл оба случая с guard после blocking accounting и not_sent accounting; запускается расширенный coherent native/PG/vet, включая Codex marker и accounting transitions. C2 binding extraction native54216 прошёл; остальная coordinator/provenance работа продолжается.

### 28 сентября — helper integration и ограниченное чтение тарифов

Root независимо восстановил ровно одну удаляемую пустую строку lifecycle_test.go в памяти и получил точный Linux run002 hash1BF4CB11E3826127B88DA6A8D83EFD9BA212126E1F052CAC69BF263A6E7E4790. Источник не менялся; root-whitespace-verification.json фиксирует проверку. Четыре helper файла интегрированы только в development C-working после проверки baseline и frozen hashes, с backup и root-integration.json. Полный D остаётся непринятым.

C1 stream successor: full focused PG21359 прошёл; final resource-child PG и scoped lint87331 прошли (0 issues). 40000 bookings/80MB comments дали1010008 Go allocation bytes при24MiB ceiling. Final freeze8C6DE87DE9BEEBAA009AE26ED6819230D4055CE99274BD83228B74FE771B60B3; final bytes не выдаются за полный повтор более раннего unit/vet/PG. Fresh cumulative CodeQA подготовлен. C3 coherent tests и C4 private/shared ordering RED выполняются независимо.

Fresh C1 stream cumulative CodeQA31300 запущен на frozen8C6DE87... без предыдущих findings/history. History CodeQA19930 подтверждён live. C3 coherent89376 terminal0: native6packages,31PG top-level иvet6; последний adapter error-chain correction проверяется отдельно до freeze. D001 implementation разрешена владельцу после завершения C3 gates, только на неизменяемом post-choice-move checkpoint, с измерением baseline/candidate и исходной paired race без ослабления assertions/budgets.

### 28 сентября — ранний независимый срез D и сверка C1

Root сверил11 owned paths stream successor с C-working: все уже совпадают с frozen8C6DE87..., повторное копирование не выполнялось; root-composition-binding.json фиксирует факт. History19930 и stream31300 независимые reviews подтверждены live.

Для ускорения свободному developer передан изолированный neutral-services slice D: убрать runtimeapp.NewServices -> api.Dependencies через конкретный runtime-owned bundle, сохранив domain construction, standalone API и combined local wiring. Отдельная копия architecture-stage-d-services; root composition не меняется. Это перенос независимой части D вперёд, не старт admission/fencing и не приёмка D. Общий runtime wiring остаётся после стабилизации C.

### 28 сентября — независимые C1 findings и новый privacy GREEN

History CodeQA19930 завершён с P2: local original/outcome/derived archive допускает payload больше HTTP65536-byte envelope; developer делает новый successor с общим live admission и отдельным importer allowance. Stream cumulative CodeQA31300 нашёл оставшиеся registration HTTP methods с partial typed result при decode error; assigned полный registration adapter audit/fix, не очередной единичный binary метод. Обе frozen версии сохранены, приёмка не повышена.

D004 successor33740 прошёл4real-PG tests с private/shared subcases и исходными барьерами7.619s. До/после1504hashes совпали сb08cead39806ea6b64b2c3a604f676c32497a4222d0915fa4b2f4a5e95bb4dca; synthetic PG удалён. Подготовлен новый fresh narrow Code QA; D004 ещё открыт до независимой проверки и coherent C4d/Functional evidence.

Neutral services D уточнение: domain bundle+constructor переходят в internal/appservices; API зависит от этого нейтрального пакета, будущий runtimeapp сможет импортировать API без цикла. Никаких aliases/forwarding compatibility wrappers. Реализация только в isolated copy.

### 28 сентября — финальный C3 и монотонное privacy retirement

C3 successor64042 terminal0: final native projection/assessor, four-phase accounting PG3.018s и pinned lint0issues прошли; source71ee8f7d397efcd600682df8cbae77f81c09a4c01659b1fbf807cc9dc64a6709/1525files/13paths заморожен. Fresh cumulative independent CodeQA19157 запущен без предыдущих findings. D001 исследует coherent choice-only checkpoint; precompile8file handoff имеет посторонние privacy dependencies, поэтому mutable C4d не копируется и измерения ещё не начаты.

D007 expanded GREEN31283:6real-PG load/complete/change × outage/cancellation прошли7.057s. Failed bookkeeping не закоммичен, command keys сохранены, retry не восстанавливает приватные данные. Before/after1506files binding8a0a47ab19a29a0af53b0c1f5ccf552f18851de19f718ef7fab2a30e76a7bb65, PG удалён. Fresh narrow CodeQA33577 запущен. D007 не закрыт до coherent final gates/affected FQA.

History admission RED24530 подтвердил расхождение local/HTTP у всех3archive routes; exactHTTP65536 и importer70000 прошли. Новый live shared envelope bound реализуется отдельно, importer сохраняет allowance. Neutral appservices slice начал native/compile/vet13870; exact constructor body и48callers mechanical binding сохранены.

### 28 сентября — объединение C3 в development composition

Root сравнил cumulative C3 относительно C2 baseline9924... с текущим C-working:32 clean transfers,5 already-final,3 manual seams. После повторной hash проверки перенёс32paths сbackup; вручную добавил единственный provider guard/NotSent hunk в OpenAI, сохранив C5 workflow types и transport decoupling. Два borrowed formatter/dead-description hunk не переносились: текущий C2 test сохраняется, лишняя description уже отсутствует. root-composition-preflight.json/root-integration.json фиксируют решение. Это development integration, не приёмка C3/C.

Запущен совместный native gate interaction/agent/agenthost/bot/credits/cmd-zns. Frozen source71ee... и review19157 не меняются. C1 archive admission GREEN16367 прошёл native3, dedicatedPG9.181s иvet4; formatter/lint10756 ещё идёт.

D005 public operations→memo RED подтверждён: после отзыва booking-admin grant статусная memo всё ещёactive. Evidence coordinator/red-memo3/pg.log; reconstructed snapshot69bdaafe74e124db22aa1e84f95369273095e6fe3e9d55897468f306846f6d60 явно помечен как post-run reconstruction, а не pre-run binding. C2 исправляет typed provenance, включая текущую permission authority для opaque metadata и exact target identity.

D004 retirement2 review не нашёл production defect, но потребовал усилить concurrent test: проверять returnedpayload и immediate durable completion до возможной последующей очистки; одинаковый RED/GREEN должен падать на semantic privacy assertion, а не обязательном lock wait. Test-only successor готовится; прежние доказательства сохранены, D004 открыт.

Root combined native34114 terminal0: interaction/agent/agenthost/bot/credits/cmd-zns прошли; cmd2.476s. Первый14189 обнаружил старый knowledge_actions.go, удалённый в frozen C3, но пропущенный при копировании add/change manifest. Root сверил точный baseline hash, сохранил backup и применил удаление; cumulative delta теперь учитывает removal. root-integration-removal.json/root-native-result.json содержат доказательства. Main platform не менялась.

Fresh C3 CodeQA19157 выявил P2: assessmentVerdict возвращает только ctx.Err и теряет joined accounting cleanup failure из assessor. Исправление назначено developer с проверкой через composed coordinator execute/retry, no dispatch/no winner и обоими errors.Is. C3 не принят; финальный71ee остаётся frozen, successor отдельно.

### 28 сентября — coherent script retirement repair

Fresh scoped CodeQA33577 вернул3P1 и2P2: completion может вернуть заранее кодированный private payload после retirement; privacy-only scrub оставляет Calls.Memory.Text; late admission denial коммитит choice claim; внешняя авторизация выполняется под ledger locks; repair UPDATE теряет original error. Это активные условия C4, не принятые регрессии и не причина объявлять новый этап завершённым.

Согласовано coherent исправление в agenthost store/dispatch/redaction/choice: внешние проверки до транзакции, затем locked compare исходной ledger revision; при retirement сохранять только scrub исходных durable records, не failed bookkeeping; completion должен запретить live payload. Memory.Text удаляется в самом scrub; error chains сохраняются. Контракт обычного CAS conflict проверяется отдельно: он не должен превращать concurrent progress в ложный source retirement. Actual dispatcher/choice/retry PG proofs обязательны. Новый store/framework не вводится.

C1 response5529: full appclient native34methodmatrix,8existing liveauth PG иvet прошли наB3912A5...; lint ещё впереди. Dservices69397:5approvedPG cases прошли7.545s/cleanupremaining[]; native/compile/vet уже green, lint впереди. Archive72627: finalfmt/integrationcompile/vet/scopedlint0issues прошли; finalfreeze готовится дляfresh review.

C1 archive admission successor BE809CF9E2F3F4FEF9D5D1858EA7FD29AD17A6D9FEB4C9B54A3228590F9C62DB/1546files/6paths заморожен после gates. Fresh independent CodeQA16123 запущен. Root сверил все6before/afterhashes и перенёс только их в C-working сbackup/root-integration.json. Общие проверки нового состава ещё требуются; frozen исходники не менялись.

C4 conflict contract уточнён: StaleError вызываетsource_changed и остановку worker, поэтому обычный CAS conflict использует отдельную retryable ошибку. Допустим bounded reload+reauth только ledger операции, без повторного доменного/модельного эффекта, с теми же durable identity/keys; исчерпание не объявляет retirement или успешное завершение. Actual contention/dispatcher tests обязательны.

### 28 сентября — response contracts и composed cancellation proof

Registration99369 final pinned lint terminal0/0issues; final915C746C7ADF9E34FD658FD9048DAEDADA4650B6C96D9C9FADF5FFE2F9909F02 отличается от native/PG/vet B391 только unused test parameter names→underscores. Fresh review следующий, независимая приёмка не заявляется.

D004 test-only successor83323: all4PG tests PASS7.352s, exact same strengthened test file as semantic RED40032. ManifestF9E8A286197F65475CE93754CD8510EB5FB811AD186FDF209B081853D363966D/1504files before/afterverified; PG removed. Fresh independent CodeQA78054 запущен.

C3 deferred3 RED74764 подтвердил потерю accounting sentinel и при Execute, и при RetryAssessment, при этом no-dispatch/no-winner assertions прошли. Однострочное errors.Join исправление дало GREEN85340 на новом и2existing coordinator cases; cleanup подтверждён. Focused vet/lint ещё впереди.

Root read-only preflight appservices changes.patch прошёл git apply --check поверх текущего C-working (включаяC3/history admission); дополнительных api.Dependencies/runtimeapp callers вне51pathscope не найдено. root-composition-preflight.json сохранён. Сам перенос ещё не применён: finalfmt/lint snapshot developer ожидается.

### 28 сентября — общий development состав C1/C3 и appservices

Root перенёс response15paths в C-working после before/afterhash checks; единственный manual seam derived_receipts сохранил workflow types/import из C5 и добавил registration zero-result handling. Fresh CodeQA57099 запущен на frozen915C...; root-integration.json/backup сохраняют перенос.

Neutral appservices final E4C1304D96D74A5FB54FECD21E40D5C9BC437EF2C5663C53C407F9A50C9C4BCA прошёл final lint74862; fresh CodeQA62107 запущен. Root повторил git apply --check и применил51pathpatch сbackup поверх нового C3/history/response состава. Поиск Go callers не нашёл оставшихся api.Dependencies/runtimeapp imports; alias или wrapper не создавался.

C3 deferred3 final5680a9c828d06505ddc6c94e83bf6929402e2c11e832cd2f35c93ee91448d30d: exactly2paths, RED/PG3/vet/lint green. Root перенёс оба послеbaselinehashvalidation. Общий native/integration compile gate нужен на этом новомсоставе; никакая независимая приёмка этим переносом не повышается.

### 28 сентября — независимый privacy review и общий gate

Fresh retirement3 CodeQA78054 не нашёл actionable findings в4pathdelta. Проверены identical RED/GREEN test bytes, returned+immediate durable target/sibling redaction, epoch ordering и before/after source binding. D004 сохраняет ограничения полного coherent C4d/FQA и runtime isolation; narrow source review чистый.

Archive admission CodeQA16123 выявил оставшийся P2 в HTTP client: json.Marshal(input) выполняется до shared bounds, хотя localdomain защищён. Назначен новый successor с reuse live admission послеauthentication и до marshaling, focused hugeinput/no-send tests; frozen BE809 сохранён. Importer не меняется.

Общий development gate запущен на объединённых C1responses, C3deferred3 и appservices:10nativepackages и integration compile, с before/after source manifest. Во время gate C-working не меняется. Root fixtures source snapshots и CodeQA candidates неизменяемы; ни C, ни D пока не приняты.

Общий C-working gate60972 terminal0:9packages tests PASS плюс appservices compile(no tests), integration compile PASS(no tests to run). Before/after1567files unchanged; evidence architecture-stage-c-combined-check/{source-manifest.json,native.log,integration-compile.log,result.json}. Это доказательство совместимой сборки C1/C3/appservices, не PG execution или весьStageC/FQA. Rootне менялисточниквовремяgate.

C2 typed checkpoint41483 такжеterminal0: interaction/passbooking/derivedmutation/appclient/api/bot/integration compile иfocusedunits; D005 realPGGREEN ещёнет. Разрешён coordinated receipt witness: domain-owned canonical request digest captures exact existing receipt hashing at new admission into existing hostledger, retains owner/event/key/action/target identity, never grants authority or reexecutesrequest. C4 может terminally scrub private Comment/Create.LegalName/Batch.Options; new opaque status verifies witness. Old scrubbed intermediate Go records without witness explicitly unavailable; никаких invented witnesses/compatibility aliases/newstores. C2domaincontracts иC4hostcapture остаются раздельнымиowners.

### 28 сентября — три независимых scoped Code QA без замечаний

Response CodeQA57099: verified915C...1512files/15paths, no actionable source findings across registration HTTP inventory; newer PassOperations остаётся C2 scope. Supplied native/8PG/vet иfinal lint подтверждены слимитами testparameter-rename binding. Rootcombinedgate уже проверил реальный mergedworkflowseam.

Appservices CodeQA62107: exact51pathdelta и17servicefields/constructor/callers проверены, dependency closure безAPI/runtimeapp, independentreverseformat обоихфайлов совпал сtestedhashes. No actionable findings;5PG cases не означаютfullsuite.

C3 cancellation CodeQA9259: all1526candidate/1525baselinehashes match, exact2pathdelta; actualExecute/Retry evidence preserves both boundederrors, no dispatch/winner, private diagnostics filtered. No actionable findings; timingcoverage scoped.

Эти CodeQA не являются полнымStageC/D илиFunctionalacceptance. PROGRESS статусы обновлены без повышения acceptedpercent. C1HTTP pre-marshal admission RED43069 иD001 baseline/candidate native measurements81223 выполняются враздельныхsyntheticprobes; C4actualdispatcherRED ждётследующий освободившийсяslot.

### 28 сентября — измеренный D001 candidate и фактический dispatcher RED

Root прочитал raw baseline-benchmark.log/candidate-benchmark.log и result.json в architecture-stage-c4-d001/evidence/pg-20260928T173207729Z. На одинаковом measurement overlay подготовка quote сократила domain HTTP requests2→0 и~277510–277518responsebytes→0; в показанных non-ASCII/escaped samples allocations примерно2.4–3.7MB→2.1–3.2KB. Prepare+execute requests5→3, обычныйbody817181→539663bytes. Execute сохраняет3live domainrequests и прежнююпорядковуюстоимость; SQL/TOAST work иfull per-pageJSON не объявленыустранёнными. Bothnativebenchmarks иfocusedPGcorrectness PASS; candidate manifest47A16C3A5A2E0F955A4EB7CA555D4D420689BE9931F9B1611382977E5FC7F428/1506. D001остаётсяоткрыт до unchangedLinuxpairedrace/requiredgates/review, затемfullcomposedproof.

C4dispatcher2RED57108 воспроизвёл живую утечку: actualBot→Sobek→realregistrationHTTPread succeeded, grant revoked atcompletion, worker callback всёещёполучилprivatecomment. Test1.77s, frozen1507files/c05444b482c9c1be2026cf4c230f8b861f35fe11a91047dd8ebeb9d99a93e87d before/aftermatch, PGremoved. Это actualprivacy failure, не stub-only илиfixtureerror. Coherent CAS/retirement/dispatch repair вработе; никакойприёмкипоGREENдругихузкихтестов.

### 28 сентября — привязка общей композиции и согласование оценок

Общий native gate60972 прошёл на неизменном составе1567 файлов; integration package скомпилирован без запуска PG. component-bindings.json подтверждает точное совпадение всех оставшихся owned appservices файлов с принятым scoped снимком и выполнение final registration response test ED9584A00DB1C965C7AF784A2FF5D3F6C32D84FCB6C40A50336B10AA8D25B604 в полном native appclient suite. Это закрывает привязку финальных test bytes, но не заменяет PG или общую Functional QA. Доказательства: qa.local/architecture-stage-c-combined-check/.

Исправлены устаревшие дубли оценок в PROGRESS и общем архитектурном плане: C5–10, архитектура11–21, вся цель26–53 инженерных дня; реализация55%, принято15%. Нового повышения готовности нет.

### 28 сентября — HTTP archive admission в общей development composition

Интегрированы ровно4 пути из frozen1547 manifest80C2CAADA943488A8A7B0ABF3BB030A170BA314DECB9FC50F3BC06F4B17B65EC. Все прежние файлы совпали с baseline до копирования; backups и before/after hashes сохранены в qa.local/architecture-stage-c1-host-history-admission-http/root-integration.json. Shared DTO validation выполняется после authentication и до marshal; focused native, realPG24.502s, isolated allocation benchmark, vet и pinned lint прошли. Финальные allocation cases808–872B/op включая harness, guard<1MiB. Fresh independent read-only Code QA28011 запущен; общий gate после интеграции ещё не выполнен. Stage acceptance не изменена.

### 28 сентября — общий native после HTTP admission и разделение C2

Root gate77038 завершился0: общий native набор interaction/agent/agenthost/bot/credits/conversation/appclient/appservices/api/cmd и integration compile прошли на1568 неизменных файлах. Логи и manifest: qa.local/architecture-stage-c-http-combined-check/. Это не запуск integration PG и не Functional acceptance; независимый HTTP admission Code QA28011 ещё выполняется.

C2 scoped authority PG30028 прошёл6 top-level tests за21.315s, включая operations→memo при отзыве прав и canonical command/assignment/batch witnesses. Root проверил raw завершение и cleanup отдельного synthetic контейнера. Actual host witness admission, same-kind single-target discovery, long-history и полный coordinator остаются открыты. Для ускорения execution/routing/recovery coordinator поручен отдельному разработчику в architecture-stage-c2-registration-execution; основной C2 владеет binding/read/reservation/status/provenance/script. Файлы и function seams разделены; общий состав интегрирует root.

D001 final guard61574 завершился1: TestModernChoiceDeletedGenerationRejectsEveryUse/quote ожидал denied:true, получил false. Остальные7 generation uses и legacy/permission/version guards прошли; это не компенсирует регрессию. Полный3leaf race ещё не запускался, scoped vet/lint после этого gate тоже не подтверждены. Разработчик исправляет optimized quote path с сохранением live generation denial; исходные assertions не ослабляются. Предыдущая успешная race-пара не закрывает D001. Raw evidence: qa.local/architecture-stage-c4-d001/evidence/pg-20260928T175530011Z/.

### 28 сентября — HTTP admission: независимый Code QA завершён

Fresh Code QA28011 CLEAN: проверены все1547 candidate/1546 baseline hashes, ровно4 изменённых пути, shared bounded validation до marshal после authentication, canonical65536bytes и importer16MiB. Raw native/PG/allocation/vet/lint подтверждены с оговоркой scoped coverage и отдельного benchmark command. Отчёт: qa.local/architecture-stage-c1-http-admission-code-qa/REPORT.md. Rootcombined77038 уже подтвердил интеграцию на1568 файлах. Полная stage Functional QA остаётся обязательной; принятие C не повышено. Статусы D005–D007 синхронизированы с полученными RED/GREEN и незакрытыми замечаниями.

### 28 сентября — C4 фактическая утечка callback: парный RED/GREEN

Runner58193 завершился0 как оркестратор ожидаемого RED и GREEN. Root прочитал raw pg.log обоих запусков: одинаковый тест actual Bot/Sobek completion в RED выдаёт completion-private-comment после отзыва прав (1.83s), в GREEN этот сценарий и mixed retirement/outage/cancellation plus knowledge retirement проходят14.515s. Доказательства: qa.local/architecture-stage-c4d/dispatch3-red-evidence/ и dispatch3-green-evidence/. Это scoped runtime proof; CAS conflicts, exact witness, полная очистка carriers и независимые QA всего согласованного C4 ещё впереди.

### 28 сентября — дополнительная граница конкурентной очистки C4

Root source review выявил в persistRetirement blanket redaction последней строки ledger после внешней проверки прав. Developer подтвердил возможную гонку: новый разрешённый run может быть добавлен между наблюдением retirement и repair, после чего будет ошибочно очищен. Это source-confirmed concern, runtime RED ещё предстоит. Исправление связывает target set с исходными admission identities и сохраняет новые runs и независимые committed изменения; отдельный PG barrier test обязателен. Не заменять latest ledger старым snapshot.

C2 selection41559 завершился timeout до assertions; long-history оставался paused. Transport retry loop по source trace не установлен. Diagnostic copy с tracing/bounded client wait и анализом lock waits готовится отдельно, исходный timeout не увеличивается. Результат не считается public discovery proof.

### 28 сентября — контракт metadata при исполнении C2

Проверен actual runPassBatchItem: domain failure сохраняется в per-item outcome, aggregate может вернуть nil error при partial/refused items. Поэтому общий executor не создаёт registration_action для batch; существующие receipts/ledger сохраняют истину по каждому элементу. Command/assignment используют прежний writer, сохраняют committed result и joined errors при сбое metadata; caller не должен принять такой сбой за обычный отказ. Recovery читает исходный command/receipt без повторного model/binding. Контракт внесён в план C, developer добавляет соответствующие проверки.

### 28 сентября — подтверждение late-repair RED и подготовка D

Root проверил raw late-repair72899: оба варианта (исходный context активен/отменён) ошибочно очищают новый отдельно разрешённый run;3.024s, owned PG08fd75ec удалён. Одинаковый frozen test будет использован после identity-bound repair. C2 original-budget selection и отдельный long-history запущены33112; результатов пока нет.

Свободному developer поручен read-only контракт D admission: actual role/writer matrix app/api/bot, существующие keys, пределы DB-session lock и supported Linux/Docker replacement barrier. Ownership только qa.local/architecture-stage-d-admission-design/PLAN.md. Product implementation/wiring этим не разрешены; C остаётся приоритетом. Нейтральный appservices не переносится повторно.

### 28 сентября — C2 длинная история и граница resume

Root проверил raw green-long-history/pg.log: TestPassOperationLongHistoryTypedRead PASS1.48s (package1.698s),1000 admissions, recent20/exact-old-ID/foreign-owner denial; scoped result0. Single-target selection2 завершился8.755s без timeout, но failed на nil Context после исполнения в том же script. По source путь cached-summary revalidation замечает not_committed→committed и очищает зависимый run; фактические retirement flags/target effects/canonical receipt ещё проверяются. Этот тест не объявлен успешным. Полный public discovery/resume переносится на согласованный C4 admission/witness, не на временный старый SQL reader. Исходный timeout41559 остаётся необъяснённым и сохранён.

### 28 сентября — D001: сохранение quote refusal contract

Root прочитал identical public quote-denial candidate90061: catalog и order возвращают denied:false вместо baseline denied:true, оба сценария failed (package4.555s); baseline8286 passed. Это регрессия внешнего tool contract, успешная race-пара её не отменяет. Одобрено компактное domain-owned ChoiceSnapshot с текущими правами и тем же canonical fingerprint algorithm, одинаковое для local/HTTP; полная Execute revalidation сохраняется. Domain hydration/hash пока остаются, итоговые запросы/байты/allocations будут измерены заново. Старое2→0 не заявляется окончательным выигрышем.

### 28 сентября — targeted retirement GREEN и точный эффект resume

Root прочитал late-repair2 raw logs: одинаковый уточнённый тест RED3.007s ошибочно очищает fresh admission в обоих context variants; GREEN13.232s проходит вместе с actual dispatcher privacy, CAS admission/conflict и mixed-error repair. Проверены developer bindings RED8d98dee1…/GREENabbb7a2b…,1510files; coherent final QA ещё требуются. Это scoped исправление ошибочной blanket cleanup, не общая приёмка C4.

Root прочитал selection-retirement diagnostic14236: visible omitted/pass_access_changed, durable pass_redacted=true, alice assigned v1, bob v0, canonical receipt count1. Сохранённая assertion stale Context продолжает падать; фактический эффект правильный, приватная cachedsummary инвалидируется при смене статуса. Новое публичное чтение и receipt после retirement проверяются на окончательном host witness.

### 28 сентября — C2 execution real-Bot proof и isolated D admission

Root проверил pg-001/tests.log и terminal-cleanup.json: C2 execution35515 realdomain metadatafailure test и integration suite7.123s PASS, explicit/empty saved keys, actualBot retries без model/duplicateeffects, unknowncommit/history/source cases, manual menu иpublication. Cleanup exit0,remaining[]. Source binding F1873654C3421A0DDBC840E9D1CE78F11A7EF0CBB36AC1E5114048BCAB24CE02/1522. Native50111 scopedtests/compile/vet passed; final lint/freeze/review ещё впереди.

Root одобрил только unconnected D admission primitive в отдельном source:3 новых пути runtimeapp/admission.go, admission_internal_test.go, integration/runtime_admission_test.go. Static ordered API/BOT keys, dedicated session, bounded independent loss monitor, explicitclose после drain; не transactionfence. В обязательных PG проверках есть counterexample, где потеря admission не останавливает транзакцию другой connection. Cmd/bot/launcher/deployment wiring не выполняются этим срезом; whole-group replacement требует отдельной integrated proof.

### 28 сентября — C2 typed/CAS checkpoint и честная классификация lint

C2 compile22795 завершился0:8 packages includingintegration compile, typedHTTP zero-result/freshidentity и cachedsummary tests PASS. Это не PG или publicwitness closure. Execution63527 lint завершился1 с32 замечаниями: один в новом тесте (blankline), остальные в неизменённом baseline. Собственное замечание исправляется безsuppressions; exact sameflags baseline/final runs подготовлены для attribution. Fullfinal lint остаётся обязательным, gate не объявленgreen. Root подготовил scope будущего независимого executionQA; dispatch только после immutablefinalbindings.

Уточнённые D001 измерения45681: запросы Prepare2→2, responsebytes≈277510–277518→96, allocations≈2.35–3.73MB→0.80–1.28MB. Надёжного ускорения по timing нет, местами candidate медленнее; domainhydrate/hash сохраняются. Эти замеры не доказывают закрытие таймаута. Finalmetadata flags/sourcebinding и исходный full3leafrace ещё требуются.

### 28 сентября — execution coordinator интегрирован в development composition

Root перенёс frozen4BC073CF906F603F9A7BD3FE5F0F065A3EB6BCBDF94B88A67A2DD3EACD462B8A exactowned10paths:9 совпали с baseline (новые отсутствовали), pass_batch.go получил только interaction import и execution call. Старый HTTPhelper не возвращён; текущее C1/C3/appservices сохранено. Before backups и afterhashes: qa.local/architecture-stage-c2-registration-execution/root-integration.json. Fresh CodeQA57119 ещё выполняется, lint attribution и общий gate послеmerge pending; интеграция не равна приёмке.

Оценки синхронизированы с ответом пользователю: C75–80%, D≈5% за helper/appservices, взвешенно57–58.5% реализации с рабочим диапазоном55–60%. Принято15%, функциональные92/78 и остаток26–53 дней не изменены.

### 28 сентября — общий execution gate и полный D001 race

Root combined24497 завершился0 на1572 неизменных файлах: native набор и integration compile прошли. Доказательства qa.local/architecture-stage-c-execution-combined-check/. Independent executionCodeQA57119 выполняется; final4BC source не менялся. Baseline/final lint31 замечание, own0, это не full lintPASS.

Root разобрал raw D001 fullrace83768 JSONL: d09679.08s, escaped109.53s, ASCII93.48s, package173.640s — всеPASS с исходными проверками. Finalmanifest b0524c6600a7449dbe9b25072fdee2c25060a67387e8cca5d63cdb0388f6b989/1511 точно соответствуетrace;11owned+3borrowedformatting paths. Последующая frozen-byte HTTP appclient проверка7429+vet прошла без измененияhashes. Fresh independent D001 CodeQA50456 запущен. Восемь inheritedlint findings, общий состав иFunctionalQA ещё требуются; latency improvement не заявляется.

### 28 сентября — C2 actual host gate: частичные результаты

Root проверил raw PG3243 (green-host-admission/pg.log): public same-kind single-target selection PASS32.30s, longhistory PASS2.36s. OpaqueMemoGrantWithdrawal failed execution_failed (line25); CombinedSelectionRetiresResult failed ожидание2modelInputs при1 (helperline28). Весь gate FAIL53.455s; механизм двух ошибок расследуется, положительные тесты не превращают его вPASS. Source1524 былbound доrun, disposablePGe554653e завершён поrunner. Полная C2/FQA приёмка не заявляется.

### 28 сентября — execution Code QA: граница кандидата и общего состава

Fresh Code QA57119 NOT CLEAN: изолированный4BC combinedClient не заполнял LocalRegistration.Batches после частичного заимствования C1, а script consumer ещё обходил executor. Root проверил currentCworking cmd/zns/app.go133–136: Batches:services.DerivedMutations уже есть, с TestCombinedClientInjectsRegistrationBatches; общий app.go hashE781A907BCC1D862EDC58B3364857E898436035E12BC9B2B0AC2490C2944ADB9. P1 относится к неполному isolated dependency snapshot, не объявляется обнаруженным panic в текущем rootсоставе. Successor получает точное подключение и combinedclient PGproof. P2 остаётся обязательной задачей mainC2.

Reviewer верно ограничил прежние history/source revocation tests: они относятся к profile commands; их нельзя считать registration executor coverage. Завершённый lint-attribution baseline31/final31/own0 будет передан новому review отдельно; full lint остаётсяfailed untilcleancomposition. Rootlint9018 exit3 был Windows sandbox cwdAccessDenied доанализа; identical escalation retry26638 выполняется, не считать первое codefailure.

### C: общий lint и уточнения QA — 28 сентября, 20:04 UTC

Общий scoped pinned lint development composition завершился exit0 (65270): interaction/agent/agenthost/bot/credits/conversation/appclient/appservices/api/cmd/zns/integration. Единственное исправление pass_batch.go оказалось нормализацией переводов строк: format-binding.json доказывает равенство после CRLF/LF normalization; native proof состава1572 сохранён. Evidence: qa.local/architecture-stage-c-execution-combined-lint-003. Это не full PG или Functional QA.

Независимый D001 Code QA (50456) обнаружил отсутствующую повторную проверку memory/pass retirement и JSON null в execution loader после Prepare. Замечание передано владельцу; frozen candidate1511 b052 сохранён, новый successor получит Prepare→retirement→Execute проверки. D001 не закрыт. Неполное соответствие measurement overlay также исправляется через сохранённые contemporary bindings, без выдуманной реконструкции.

C2 host-admission2 PG (82768) завершился exit0, четыре focused сценария, 28.725s, cleanup подтверждён владельцем. Исправлены fixtures: явное указание получателя в запросе и корректная проверка одного уже совершённого эффекта при остановке продолжения после retirement. Общая приёмка C остаётся открытой.

### Автономные black-box FQA — новое исполнение поручения

По прямому поручению Даниила выделен FQA-архитектор, после краткого анализа он начинает тестовую реализацию. Первый срез переиспользует Node/Playwright/QARun/SandboxClient: изолированный Compose lifecycle, динамические loopback ports, внутренний PostgreSQL, synthetic seed, EN/RU mouse/touch order journey, restart и публичные invariants, сбор артефактов и очистка только собственного проекта. Владение: platform/scripts/fqa/stand.mjs, stand.test.mjs, platform/tests/fqa-owned.mjs; отчёт qa.local/fqa-blackbox-architecture/REPORT.md. Docker-run ждёт свободного heavy slot. Тесты не заменяют независимые QA и не означают приёмку C на старом B1 составе.

### Имена synthetic-ресурсов: обнаруженное несоблюдение

Live Docker inventory подтвердил старые проекты zns-b5/b8/b9/b11, imported-runtime-fresh-functional, zns-identity и zns-sandbox. Правило docs/code-quality.md требует synthetic-qa-zns-<scenario> и synthetic_qa_zns_<scenario>; оно не было последовательно применено. Root исправляет default project names трёх local Compose (не deploy). Новый автономный FQA harness обязан применять канонический префикс к реальным проектам, контейнерам, сетям, volumes и БД/DSN. Уже работающие проекты не переименованы и не очищены этим изменением; их lifecycle требует сверки зависимостей. Ошибочно предложенный сегодня альтернативный zns-synthetic префикс отменён; единственный уже запущенный D001 RED24618 завершится без перезапуска, GREEN обязан использовать каноническое имя.

### Compose defaults и новые independent reviews

Три local Compose default project names теперь synthetic-qa-zns-sandbox, synthetic-qa-zns-codex и synthetic-qa-zns-media. docker compose config --quiet для всех трёх завершился0; были предупреждения чтения Docker config в sandbox, но локальная проверка конфигурации успешна. Production deploy Compose не менялся. Активные старые проекты и внутренние имена их БД остаются отдельным незавершённым cleanup/recreation scope.

Запущены свежие read-only independent Code QA: admission primitive (61853, architecture-stage-d-admission-fresh-code-qa) и combinedClient batch dependency successor (52858, architecture-stage-c2-execution-successor-fresh-code-qa). Оба frozen candidates прошли свои focused gates; вердикты ещё ожидаются. D001 between-Prepare/Execute retirement/null получил RED→GREEN (24618→83361); финальная проверка successor впереди. Проценты принятия не повышены.

Уточнение FQA по поручению Даниила: общий suite-owned стек для совместимых сценариев; отдельная БД или контейнер только при нужной изоляции. Restart/fault сценарии эксклюзивны. Отдельный полный стек под каждый сценарий не требуется.

### C-working: constructor test integration

Successor actual combinedClient PG test добавлен в development composition с сохранением текущего appservices API и прежнего injection test. Первый compile выявил оставшийся старый runtimeapp import; он заменён реальным appservices.NewServices/Options. Повтор native cmd constructor test и vet прошёл0 (result-002.json). PG case в общем составе пока только скомпилирован; отдельный successor ранее исполнил его на PostgreSQL. Binding и исходный failing log сохранены в architecture-stage-c2-execution-successor-integration.

### C/D и автономный FQA: fresh reviews и первый pilot

C2 successor fresh Code QA52858 CLEAN в двухфайловой области; D admission fresh Code QA61853 CLEAN в трёхфайловой области. Admission reviewer отдельно отметил отсутствие race evidence и полной cryptographic binding logs, это не transaction fence/Stage D acceptance. Три неизменённых admission файла добавлены в C-working (root-integration.json); native и integration compile82413 прошли0. Runtime wiring не добавлен.

Autonomous pilot38647 на immutable B image b6e641c210ca завершился1: EN mouse ожидал Add: Трансфер, фактическая UI кнопка Add: Shuttle; trace подтверждает отсутствие клика. Остальные ячейки не пройдены. Cleanup errors=[]; root independently verified отсутствие containers/volumes/networks с exact project label synthetic-qa-zns-db489b64-5a28-4f14-b9fd-e4515380420e. Fresh CodeQA56357 ещё читает frozen три файла; исправление ждёт окончания review. Mini App URL сохраняет fixed8090 и не покрыт pilot; прежде соответствующего сценария требуется настройка динамического адреса.

D001 repair1 fresh independent Code QA94316 запущен на final a5ea3f74… после focused RED/GREEN/final PG и lint. Полная C/Functional приёмка не повышена.

### Автономный FQA: independent Code QA завершён

56357 terminal0, вердикт NOT CLEAN: EN selectors, signal handlers снимаются до cleanup, ownership/consumers volumes и networks не проверяются (особенно partial startup без контейнеров), начальные browser errors не входят в asserted collection. Все четыре переданы владельцу; старые файлы/manifest и failed pilot сохранены, canonical paths освобождены для исправления. Новый прогон ждёт исправлений и focused gates. Independent cleanup verification первого failed pilot подтверждена напрямую через Docker project labels: resources отсутствуют.

### Основной C: новые доказательства и граница композиции

Saved-key actual Bot PG42711 прошёл command/assignment: SavedPlan наблюдался до domain effect, ключ исходного update совпал с canonical receipt, replay не создал нового receipt/model call; native62457 также0. C4 late-admission actual Bot/Sobek RED19226 обнаружил committed parent claim после retirement; identical-test GREEN64569 на coherent fed812d2 прошёл3.27s, cleanup подтверждён владельцем.

D001 repair1 fresh Code QA94316 CLEAN; developer готовит отдельный preview D001 поверх frozen C4 policy-compile2. Root остаётся единственным владельцем итоговой композиции. Root preflight показывает, что delta90 относительно C4c недостаточна: текущий C-working ещё не содержал C4c, поэтому53 paths расходятся. Запрошен полный cumulative C4c+d delta с проверяемой исходной базой, включая перемещения/удаления. Это обязательная интеграция, не новое продуктовое требование; принятие C не повышено.

FQA revision2 исправляет четыре замечания;12 lifecycle tests и scoped lint/format/syntax passed по developer evidence. Новый независимый QA запущен, второй live pilot ещё не выполнен.

### Очерёдность UX по уточнению Даниила

Сначала завершить Go-вариант и подготовить первый production-релиз с текущим UX. Сквозной UX-аудит и улучшения перенесены после первого выпуска, не являются его gate и не останавливают основной трек. Надёжность доставки, согласованные гарантии порядка, права и обязательные проверки остаются в своём scope. Это изменение очередности, не разрешение на production deployment.

### Общая композиция C: первый фактический merge preview

В qa.local/architecture-stage-c-composition-preview создана копия1575-файлового C-working. Хеши C4b baseline и C4d frozen incoming проверены перед переносом. Автоматически:87 exact copies,12 exact deletions,6 трёхсторонних слияний для ручной сверки,10 текстовых конфликтов,3 structural merges;21 чужой owned path сохранён. Исходный C-working после подготовки неизменён.

Root разрешил C3/C5 конфликты: host carriers используют account.LanguageOperationKey/workflow.Action; retained C3 assessment constructor квалифицирован host-типом, дубль projection и старый Bot script store удалены только из preview. C2 разработчику выделены7 точных регистрационных файлов в preview. Host compile выявил отсутствующие финальные C2 OperationWitness/RegistrationOperationRead contracts; запрошена их точная зависимость, совместимые aliases не вводятся. Общая копия ещё не компилируется и не является принятой.

Исторический PublicToolSelection test без ослабления assertions прошёл на актуальном C2 (68406,8.755s); старый C4 snapshot использует прежний callback. Требуется повтор на общем typed составе, прежний failing gate сохранён.

### Общая композиция C: host compile и D001

После подключения20 canonical C2 contracts host-only compile в preview прошёл0 (host-compile-002.log). D00112paths перенесены по exact baseline/source hash checks; canonical C4 choice loader не заменён. d001-preflight.json и d001-merge.json сохраняют binding. Полный Bot/API compile ждёт окончания C2 consumer/proof слияния. Пробный D001 client test пока не запускается: старый pass_operations_internal_test.go ссылается на удалённый Host.PassOperations; его canonical replacement уже входит в согласованный C2 merge. Это известная промежуточная несовместимость, не успешный gate.

C4 privileged diagnostic97198 сохраняет старое failing model-count assertion; durable privacy retirement подтверждено, но видимый пользователю результат требует корректного wire observer. До этого тест не ослабляется и общий C4 PG остаётся непройденным.

## 2026-09-29 — obsolete B6 Docker resources

Removed nine stopped B6 synthetic containers after exact-ID/state verification and confirmation that the C stand has no runtime dependency on them. Removed their unreferenced script IPC volume and Compose network, plus two unused B6 application/evaluator images. No database, host fixture, retained importer resource, shared base image or current stand was removed.

Evidence: `qa.local/synthetic-cleanup-20260929-b6/{before.json,result.json}`; cleanup session23781 exited0 and verified all nine container IDs absent. Other older running stands still require dependency checks; this is not a claim of complete Docker cleanup.
