# Read public repository metadata and licenses for the selected baseline.
param([string]$LoomRoot = (Join-Path $PSScriptRoot '../../clx-scripter'))
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$output = Join-Path $root '.cache/shuttle-work/dependency-audit'
New-Item -ItemType Directory -Path $output -Force | Out-Null
$headers = @{ 'User-Agent' = 'Shuttle-dependency-audit' }
$loomLockPath = Join-Path $LoomRoot 'tools/dependencies.lock.json'
$loomLock = Get-Content -LiteralPath $loomLockPath -Raw | ConvertFrom-Json
$selectedNames = @(
    'github.com/jackc/pgx/v5', 'github.com/pressly/goose/v3',
    'github.com/danielgtaylor/huma/v2', 'github.com/riverqueue/river',
    'github.com/riverqueue/river/riverdriver/riverpgxv5',
    'github.com/go-jose/go-jose/v4', 'github.com/google/uuid',
    'github.com/stretchr/testify', 'go.yaml.in/yaml/v3',
    'golang.org/x/text', 'golang.org/x/sync'
)
$selected = @($loomLock.modules | Where-Object { $_.module -in $selectedNames })
$selected += @($loomLock.tools)
foreach ($entry in $selected) {
    $entry | Add-Member -NotePropertyName pin_source -NotePropertyValue 'Loom lock' -Force
}
foreach ($candidate in @(
    @{ repo = 'go-telegram/bot'; module = 'github.com/go-telegram/bot'; version = 'v1.27.0' },
    @{ repo = 'yuin/goldmark'; module = 'github.com/yuin/goldmark/v2'; version = 'v2.1.6' },
    @{ repo = 'nicksnyder/go-i18n'; module = 'github.com/nicksnyder/go-i18n/v2'; version = 'v2.6.1' },
    @{ repo = 'open-telemetry/opentelemetry-go'; module = 'go.opentelemetry.io/otel'; version = 'v1.47.0' },
    @{ repo = 'open-telemetry/opentelemetry-go'; module = 'go.opentelemetry.io/otel/sdk'; version = 'v1.47.0' },
    @{ repo = 'open-telemetry/opentelemetry-go'; module = 'go.opentelemetry.io/otel/sdk/metric'; version = 'v1.47.0' },
    @{ repo = 'open-telemetry/opentelemetry-go'; module = 'go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp'; version = 'v1.47.0' },
    @{ repo = 'open-telemetry/opentelemetry-go'; module = 'go.opentelemetry.io/otel/exporters/prometheus'; version = 'v0.69.0' },
    @{ repo = 'open-telemetry/opentelemetry-go-contrib'; module = 'go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp'; version = 'v0.72.0' },
    @{ repo = 'prometheus/client_golang'; module = 'github.com/prometheus/client_golang'; version = 'v1.25.0' },
    @{ repo = 'uber-go/goleak'; module = 'go.uber.org/goleak'; version = 'v1.3.0' },
    @{ repo = 'golang/net'; module = 'golang.org/x/net'; version = 'v0.60.0' }
)) {
    $resolved = Invoke-RestMethod -Uri "https://proxy.golang.org/$($candidate.module)/@v/$($candidate.version).info" -Headers $headers -TimeoutSec 20
    if ($resolved.Origin.Hash -notmatch '^[0-9a-f]{40}$') { throw "Missing full VCS SHA: $($candidate.module)" }
    $selected += [pscustomobject]@{
        module = $candidate.module; version = $resolved.Version; commit = $resolved.Origin.Hash
        repository = "https://github.com/$($candidate.repo)"; pin_source = 'Go proxy VCS origin'
    }
}
$evidencePath = Join-Path $output 'github-evidence.json'
$evidence = @()
if (Test-Path -LiteralPath $evidencePath) { $evidence = @(Get-Content -LiteralPath $evidencePath -Raw | ConvertFrom-Json) }
$repositories = @($selected | ForEach-Object {
    $_.repository.Replace('https://github.com/', '').Replace('https://go.googlesource.com/', 'golang/')
} | Sort-Object -Unique)
$repositories += @('PaulSonOfLars/gotgbot', 'prometheus/client_golang', 'uber-go/goleak',
    'golang/sys', 'golang/time', 'xuri/excelize', 'gitleaks/gitleaks', 'aquasecurity/trivy')
foreach ($repo in $repositories | Sort-Object -Unique) {
    if ($repo -in $evidence.repo) { continue }
    $meta = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo" -Headers $headers -TimeoutSec 20
    $evidence += [pscustomobject]@{
        repo = $repo; url = $meta.html_url; stars = $meta.stargazers_count
        forks = $meta.forks_count; created_at = $meta.created_at; pushed_at = $meta.pushed_at
        archived = $meta.archived; disabled = $meta.disabled
        license = $meta.license.spdx_id; default_branch = $meta.default_branch
    }
}
$evidence | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $output 'github-evidence.json') -Encoding utf8
$notices = @()
foreach ($entry in $selected) {
    $repo = $entry.repository.Replace('https://github.com/', '').Replace('https://go.googlesource.com/', 'golang/')
    $licensePath = if ($repo -eq 'danielgtaylor/huma') { 'LICENSE.md' } else { 'LICENSE' }
    $url = "https://raw.githubusercontent.com/$repo/$($entry.commit)/$licensePath"
    $body = Invoke-WebRequest -Uri $url -Headers $headers -TimeoutSec 20
    $name = $entry.module.Replace('/', '_') + '.LICENSE'
    $path = Join-Path $output $name
    [System.IO.File]::WriteAllText($path, $body.Content, [System.Text.UTF8Encoding]::new($false))
    $notices += [pscustomobject]@{
        module = $entry.module; version = $entry.version; commit = $entry.commit
        source = $url; file = $name; sha256 = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    }
}
[pscustomobject]@{
    audit_date = '2026-10-12'; captured_at_utc = [DateTime]::UtcNow.ToString('o')
    scope = 'Library/tool selection including reserved Telegram client; resolved audit graph is separate'
    loom_lock_sha256 = (Get-FileHash -LiteralPath $loomLockPath -Algorithm SHA256).Hash.ToLowerInvariant()
    go = $loomLock.go; selected = $selected; license_notices = $notices
} | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $output 'selection.json') -Encoding utf8
$evidence | Select-Object repo, stars, license, archived, pushed_at | Format-Table -AutoSize
