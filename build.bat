@echo off
setlocal

set "ROOT=%~dp0"
cd /d "%ROOT%"

rem Detecta o executavel do Python
set "PYTHON_CMD="
where python >nul 2>&1
if not errorlevel 1 (
    set "PYTHON_CMD=python"
) else (
    where py >nul 2>&1
    if not errorlevel 1 (
        set "PYTHON_CMD=py"
    )
)

set "OPCAO=%~1"
if not "%OPCAO%"=="" goto :process_choice

:menu
echo ============================================================
echo               BUSCATEXTUAL - BUILD ^& RELEASE
echo ============================================================
echo.
echo  Escolha a operacao desejada:
echo.
echo   [1] Apenas Gerar Build (compilar buscatextual.exe)
echo   [2] Apenas Gerar Release Notes (generate_release_notes.py)
echo   [3] Publicar Release no GitHub (git push + release)
echo   [4] Fazer Ambos (Build + git push + Publicar Release no GitHub)
echo   [0] Cancelar e Sair
echo.
echo ============================================================
set /p "OPCAO=Digite a opcao desejada [1-4, 0]: "

:process_choice
if "%OPCAO%"=="1" goto :opt_build
if "%OPCAO%"=="--build" goto :opt_build
if "%OPCAO%"=="-b" goto :opt_build

if "%OPCAO%"=="2" goto :opt_release_notes
if "%OPCAO%"=="--notes" goto :opt_release_notes
if "%OPCAO%"=="-n" goto :opt_release_notes

if "%OPCAO%"=="3" goto :opt_publish
if "%OPCAO%"=="--publish" goto :opt_publish
if "%OPCAO%"=="-p" goto :opt_publish

if "%OPCAO%"=="4" goto :opt_both
if "%OPCAO%"=="--all" goto :opt_both
if "%OPCAO%"=="-a" goto :opt_both

if "%OPCAO%"=="0" goto :opt_cancel
if /i "%OPCAO%"=="q" goto :opt_cancel
if /i "%OPCAO%"=="sair" goto :opt_cancel

echo.
echo [Aviso] Opcao invalida. Tente novamente.
echo.
set "OPCAO="
goto :menu

rem -------------------------------------------------------------
rem OPCAO 1: Apenas Build
rem -------------------------------------------------------------
:opt_build
echo.
echo ============================================================
echo                 [1/1] EXECUTANDO BUILD
echo ============================================================
goto :exec_build

rem -------------------------------------------------------------
rem OPCAO 2: Apenas Release Notes
rem -------------------------------------------------------------
:opt_release_notes
echo.
echo ============================================================
echo             [1/1] GERANDO NOTAS DE RELEASE
echo ============================================================
if "%PYTHON_CMD%"=="" goto :err_no_python
%PYTHON_CMD% generate_release_notes.py
if errorlevel 1 goto :error_exit
goto :finish

rem -------------------------------------------------------------
rem OPCAO 3: Apenas Publicar Release
rem -------------------------------------------------------------
:opt_publish
echo.
echo ============================================================
echo      [1/1] PUBLICANDO COMMITS E RELEASE NO GITHUB
echo ============================================================
if "%PYTHON_CMD%"=="" goto :err_no_python
%PYTHON_CMD% generate_release_notes.py --publish
if errorlevel 1 goto :error_exit
goto :finish

rem -------------------------------------------------------------
rem OPCAO 4: Fazer Ambos
rem -------------------------------------------------------------
:opt_both
echo.
echo ============================================================
echo                 [1/2] EXECUTANDO BUILD
echo ============================================================
set "DO_AFTER_BUILD=publish"
goto :exec_build

rem -------------------------------------------------------------
rem EXECUCAO DO BUILD
rem -------------------------------------------------------------
:exec_build
if not exist ".gocache" mkdir ".gocache"
if not exist ".gomodcache" mkdir ".gomodcache"

set "GOCACHE=%ROOT%.gocache"
set "GOMODCACHE=%ROOT%.gomodcache"

echo Atualizando contador de build...
for /f %%i in ('powershell -NoProfile -Command "$file='%ROOT%build.json'; if (-not (Test-Path $file)) { [System.IO.File]::WriteAllText($file, '{\"build\": 0}', (New-Object System.Text.UTF8Encoding($false))) }; $data = Get-Content $file -Raw | ConvertFrom-Json; $data.build++; $data | Add-Member -Force -MemberType NoteProperty -Name 'date' -Value (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'); [System.IO.File]::WriteAllText($file, ($data | ConvertTo-Json -Depth 2), (New-Object System.Text.UTF8Encoding($false))); Write-Output $data.build"') do set BUILD_NUM=%%i

echo Build incrementado para: %BUILD_NUM%
echo Configurando icone...
go-winres simply --icon icon.ico --arch amd64

echo Fechando instancias ativas do buscatextual.exe...
taskkill /f /im buscatextual.exe >nul 2>&1

echo Gerando buscatextual.exe...
go build -ldflags "-X main.BuildVersion=%BUILD_NUM%" -o buscatextual.exe .
if errorlevel 1 (
    echo.
    echo [ERRO] Falha ao gerar o executavel buscatextual.exe.
    goto :error_exit
)

echo Executavel criado em "%ROOT%buscatextual.exe"
echo [SUCESSO] Build concluido com sucesso!

if not "%DO_AFTER_BUILD%"=="publish" goto :finish

echo.
echo ============================================================
echo     [2/2] PUBLICANDO COMMITS E RELEASE NO GITHUB
echo ============================================================
if "%PYTHON_CMD%"=="" goto :err_no_python
%PYTHON_CMD% generate_release_notes.py --publish
if errorlevel 1 goto :error_exit
goto :finish

:err_no_python
echo.
echo [ERRO] Python nao foi encontrado no sistema.
echo Certifique-se de instalar o Python para usar o gerador de release notes.
goto :error_exit

:opt_cancel
echo.
echo Operacao cancelada pelo usuario.
goto :finish

:error_exit
echo.
echo [ERRO] A operacao foi interrompida devido a falhas.
if "%NO_PAUSE%"=="" pause
exit /b 1

:finish
echo.
if "%NO_PAUSE%"=="" pause
endlocal
exit /b 0
