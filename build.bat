@echo off
setlocal

set "ROOT=%~dp0"
cd /d "%ROOT%"

if not exist ".gocache" mkdir ".gocache"
if not exist ".gomodcache" mkdir ".gomodcache"

set "GOCACHE=%ROOT%.gocache"
set "GOMODCACHE=%ROOT%.gomodcache"

echo Gerando buscatextual.exe...
go build -o buscatextual.exe .
if errorlevel 1 (
    echo Falha ao gerar o executavel.
    exit /b 1
)

echo Executavel criado em "%ROOT%buscatextual.exe"
endlocal
