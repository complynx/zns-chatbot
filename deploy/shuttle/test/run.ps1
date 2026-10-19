param([ValidateSet('Start','Stop','Status','Smoke')][string]$Action='Status')
$ErrorActionPreference='Stop'
$taskRoot=Resolve-Path (Join-Path $PSScriptRoot '../../..')
Push-Location $taskRoot
try {
    $taskCompose=@('compose','--env-file','.cache/shuttle-live-test/compose.env','-f','deploy/shuttle/test/compose.yaml')
    function Invoke-CheckedDocker([string[]]$Arguments) {
        & docker @Arguments
        if($LASTEXITCODE -ne 0){throw 'Test Docker command failed'}
    }
    switch($Action) {
        Start {
            if(-not (Test-Path config/shuttle.test.env)){throw 'Add the test token to config/shuttle.test.env first'}
            Invoke-CheckedDocker @('build','--target','api','-t','shuttle-test-loom:local','../clx-scripter')
            Invoke-CheckedDocker @('build','-f','deploy/shuttle/Dockerfile','-t','shuttle-test-bot:local','.')
            & python deploy/shuttle/test/prepare.py
            if($LASTEXITCODE -ne 0){throw 'Test IAM preparation failed'}
            Invoke-CheckedDocker ($taskCompose+@('--profile','telegram','run','--rm','--no-deps','telegram-poller','python','/test/poller.py','--check'))
            Invoke-CheckedDocker ($taskCompose+@('--profile','app','run','--rm','shuttle','-config','/etc/shuttle/config.yaml','-migrate'))
            Invoke-CheckedDocker ($taskCompose+@('--profile','app','up','-d'))
            Invoke-CheckedDocker ($taskCompose+@('--profile','telegram','up','-d','telegram-poller'))
        }
        Stop { Invoke-CheckedDocker ($taskCompose+@('--profile','app','--profile','telegram','stop','--timeout','5')) }
        Status { Invoke-CheckedDocker ($taskCompose+@('--profile','app','--profile','telegram','ps','--all')) }
        Smoke {
            & python deploy/shuttle/test/smoke.py
            if($LASTEXITCODE -ne 0){throw 'Real IAM/Loom smoke failed'}
        }
    }
} finally { Pop-Location }

# Written by Codex (gpt-6/Codex desktop)
# on behalf of Daniel Drizhuk
