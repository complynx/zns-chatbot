# Shuttle аудит выбора зависимостей

Дата: 12 октября 2026 года. Аудит относится к выбранному candidate-графу, а не
к готовому приложению или production composition. Основной выбор описан в
[shuttle-dependencies.md](../../shuttle-dependencies.md).

## Проверенный результат

- Go 1.27.2: go mod tidy, go mod verify и сборка candidate прошли на Windows/amd64.
- Cross-build Linux/amd64 с CGO=0 прошёл. Это проверка сборки, не Linux runtime QA.
- govulncheck 1.8.0 проверил imports на Windows и Linux. В итоговых JSON нет findings.
  Использованная база имеет db_last_modified 2026-10-08T22:31:09Z.
- go-licenses/v2 2.0.1 с include_tests: 71 license entry для Windows и 72 для Linux.
  Все найденные сторонние licenses — MIT, Apache-2.0, BSD-3-Clause, ISC и MPL-2.0.
- golangci-lint 2.14.0 config verify прошёл. Линт приложения не выполнялся:
  реализация Go-бота ещё не создана.
- 236 модулей разрешённого candidate-графа имеют полные SHA и content sums в
  candidate-lock.json. Это не 236 библиотек в executable: граф включает модули
  неиспользуемых SQL drivers и зависимостей зависимостей.

Первоначальный вариант содержал golang.org/x/net 0.59.0 и дал findings
GO-2026-6603, GO-2026-6610, GO-2026-6611, GO-2026-6612, GO-2026-6617.
Candidate обновлён до 0.60.0, затем сборка и scans повторены. Исходный результат
сохранён в govulncheck-initial-summary.json; summary хранит affected symbol и глубину
trace, а не полный call path. JSON-режим сканера вернул exit 0 и при findings;
итог оценивается по содержимому отчёта.

Резервный go-telegram/bot исключён из основного candidate: выбран узкий transport
на net/http. Его root LICENSE, version и SHA остаются в selection.json как
проверенный резерв, без заявления о включении в приложение.

## Артефакты

- github-evidence.json: repository popularity, activity, archived/disabled flags
  и GitHub license metadata. Metadata не заменяет чтение LICENSE.
- selection.json: выбранные версии, SHA tools/libraries, Go toolchain и хеши
  root LICENSE-файлов. Версии общих компонентов происходят из Loom lock;
  новые pins разрешены через Go proxy VCS origin.
- *.LICENSE: notices закреплённых прямых кандидатов и tools.
  Goose проверен по MIT LICENSE, несмотря на GitHub NOASSERTION;
  YAML LICENSE содержит MIT и Apache notices.
- candidate/go.mod и go.sum: отдельный audit module, не go.mod приложения.
- candidate/imports.go и imports_test.go: planned runtime/test imports для scans.
  Они не реализуют бот и не являются поведенческими тестами.
- candidate-lock.json: 236 module pins, repository, origin source и content sums.
  Для старых proxy info без Origin использованы совпадающие version/SHA из Loom
  либо публичный GitHub tag/pseudo-version lookup. Проверка канонического разрешения
  полного lock обязательна при принятии реального графа приложения.
- licenses.csv и licenses-linux.csv: scanner results с тестовыми imports.
- licenses*.stderr.txt: ограничение scanner для xxhash assembly; не-Go code
  не может быть полностью исследован этим инструментом.
- govulncheck.json.gz и govulncheck-linux.json.gz: полные финальные JSON reports.
- checks.json: результаты, число findings, counts и контрольные суммы reports.

## Повтор проверки

Начальный repository audit выполняет scripts/audit-shuttle-selection.ps1.
Он использует соседний Loom lock. Repository metadata уже сделанного снимка
переиспользуется; для нового снимка нужен новый audit date/directory и свежий fetch.

Из candidate directory с GOWORK=off и Go 1.27.2:

```sh
go mod tidy
go mod verify
go build ./...
govulncheck -json ./...
go-licenses report --include_tests --ignore example.invalid/shuttle-dependency-audit ./...
```

Для Linux-target scans запускается нативный scanner с GOOS=linux, GOARCH=amd64
и CGO_ENABLED=0. На Windows нельзя запускать scanner через go run после установки
GOOS=linux: это создаёт Linux executable. Закреплённые native tools собирались
во временную папку, без изменения системного Go или пользовательского bin.

Candidate imports проверяются заново, когда появляется предметный код: нынешняя
reachability не предсказывает будущие вызовы. Tests, races, реальный PostgreSQL,
Zitadel grants, shutdown и end-to-end tracing принимаются отдельно по ТЗ.

golangci-lint GPL-3.0 — только разрешённый dev/CI tool. Аудит его полного tool graph,
нативных assets, будущих avatar workers и контейнерного OS image здесь не выполнен.
Новые optional tools и XLSX library также не входят в проверенный candidate.

Written by Codex (gpt-6/Codex desktop)
on behalf of Daniel Drizhuk
