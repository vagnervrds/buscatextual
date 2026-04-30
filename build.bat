@echo off
setlocal

set "ROOT=%~dp0"
cd /d "%ROOT%"

if not exist ".gocache" mkdir ".gocache"
if not exist ".gomodcache" mkdir ".gomodcache"

set "GOCACHE=%ROOT%.gocache"
set "GOMODCACHE=%ROOT%.gomodcache"

echo Atualizando contador de build...
for /f %%i in ('powershell -NoProfile -Command "$file='%ROOT%build.json'; if (-not (Test-Path $file)) { '{\"build\": 0}' | Out-File $file -Encoding utf8 }; $data = Get-Content $file -Raw | ConvertFrom-Json; $data.build++; $data | ConvertTo-Json -Depth 2 | Out-File $file -Encoding utf8; Write-Output $data.build"') do set BUILD_NUM=%%i

echo Build incrementado para: %BUILD_NUM%
echo Configurando icone...
go-winres simply --icon icon.ico --arch amd64

echo Gerando buscatextual.exe...
go build -ldflags "-X main.BuildVersion=%BUILD_NUM%" -o buscatextual.exe .
if errorlevel 1 (
    echo Falha ao gerar o executavel.
    exit /b 1
)

echo Executavel criado em "%ROOT%buscatextual.exe"
endlocal
