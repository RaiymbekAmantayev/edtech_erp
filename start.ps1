# Локальный запуск EMP: API (:8080), worker и фронтенд (:5173).
# Первый запуск:  .\start.ps1 -Setup   (создаст роли и БД в PostgreSQL, применит миграции, загрузит демо-данные)
# Пересоздать демо-данные: .\start.ps1 -Reset
param([switch]$Setup, [switch]$Reset)

$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$go = Join-Path $env:USERPROFILE 'sdk\go\bin\go.exe'
if (-not (Test-Path $go)) { $go = 'go' }

Push-Location (Join-Path $root 'backend')
try {
    if ($Reset) { & $go run ./cmd/migrate -reset -seed }
    elseif ($Setup) { & $go run ./cmd/migrate -setup -seed }
    else { & $go run ./cmd/migrate }   # только новые миграции
    & $go build -o bin/api.exe ./cmd/api
    & $go build -o bin/worker.exe ./cmd/worker
} finally { Pop-Location }

if (-not (Test-Path (Join-Path $root 'frontend\node_modules'))) {
    Push-Location (Join-Path $root 'frontend'); npm install; Pop-Location
}

$api = Start-Process -PassThru -WorkingDirectory (Join-Path $root 'backend') -FilePath (Join-Path $root 'backend\bin\api.exe') -RedirectStandardOutput (Join-Path $root 'api.log') -RedirectStandardError (Join-Path $root 'api.err.log') -WindowStyle Hidden
$worker = Start-Process -PassThru -WorkingDirectory (Join-Path $root 'backend') -FilePath (Join-Path $root 'backend\bin\worker.exe') -RedirectStandardOutput (Join-Path $root 'worker.log') -RedirectStandardError (Join-Path $root 'worker.err.log') -WindowStyle Hidden
Write-Host "API: http://localhost:8080 (pid $($api.Id)), worker pid $($worker.Id)"
Write-Host "Откройте http://localhost:5173 — вход: aigerim@bilimplus.kz / Demo12345!"
Write-Host "Остановка фронтенда: Ctrl+C. API и worker: Stop-Process -Id $($api.Id),$($worker.Id)"

Push-Location (Join-Path $root 'frontend')
try { npx vite --port 5173 }
finally {
    Pop-Location
    Stop-Process -Id $api.Id, $worker.Id -ErrorAction SilentlyContinue
}
