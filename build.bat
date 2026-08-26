@echo off
setlocal

set "ROOT=%~dp0"
cd /d "%ROOT%"

if not exist ".gocache" mkdir ".gocache"
if not exist ".gomodcache" mkdir ".gomodcache"

set "GOCACHE=%ROOT%.gocache"
set "GOMODCACHE=%ROOT%.gomodcache"

echo Atualizando contador de build...
for /f %%i in ('powershell -NoProfile -Command "$file='%ROOT%build.json'; if (-not (Test-Path $file)) { [System.IO.File]::WriteAllText($file, '{\"build\": 0}', (New-Object System.Text.UTF8Encoding($false))) }; $data = Get-Content $file -Raw | ConvertFrom-Json; $data.build++; [System.IO.File]::WriteAllText($file, ($data | ConvertTo-Json -Depth 2), (New-Object System.Text.UTF8Encoding($false))); Write-Output $data.build"') do set BUILD_NUM=%%i

echo Build incrementado para: %BUILD_NUM%
echo Configurando icone...
go-winres simply --icon icon.ico --arch amd64

echo Fechando instancias ativas do buscatextual.exe...
taskkill /f /im buscatextual.exe >nul 2>&1

echo Gerando buscatextual.exe...
go build -ldflags "-X main.BuildVersion=%BUILD_NUM%" -o buscatextual.exe .
if errorlevel 1 (
    echo Falha ao gerar o executavel.
    if "%NO_PAUSE%"=="" pause
    exit /b 1
)

echo Executavel criado em "%ROOT%buscatextual.exe"
if "%NO_PAUSE%"=="" pause
endlocal & set "BUILD_NUM=%BUILD_NUM%"
