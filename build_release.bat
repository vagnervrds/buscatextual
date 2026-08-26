@echo off
setlocal

set "ROOT=%~dp0"
cd /d "%ROOT%"

echo ==================================================
echo   Iniciando processo de Build e Release GitHub
echo ==================================================

:: Executa build.bat sem pausar
set NO_PAUSE=1
call "%ROOT%build.bat"
if errorlevel 1 (
    echo [ERRO] Falha durante a execucao do build.bat
    pause
    exit /b 1
)

:: Obtem o numero de build atualizado
for /f %%i in ('powershell -NoProfile -Command "$file='%ROOT%build.json'; $data = Get-Content $file -Raw | ConvertFrom-Json; Write-Output $data.build"') do set BUILD_NUM=%%i

if "%BUILD_NUM%"=="" (
    echo [ERRO] Nao foi possivel identificar o numero do build.
    pause
    exit /b 1
)

echo.
echo ==================================================
echo   Enviando alteracoes para o repositorio GitHub...
echo ==================================================

git add -A
git commit -m "Release Build %BUILD_NUM%"
git push origin master

echo.
echo ==================================================
echo   Criando Release v%BUILD_NUM% no GitHub...
echo ==================================================

:: Verifica se a ferramenta gh (GitHub CLI) esta disponivel
where gh >nul 2>&1
if errorlevel 1 (
    echo [AVISO] GitHub CLI (gh) nao encontrado no PATH.
    echo O commit e push foram concluidos, mas a Release do GitHub nao pode ser criada automaticamente.
    pause
    exit /b 0
)

:: Cria a Release no GitHub e anexa o executavel
gh release create "v%BUILD_NUM%" buscatextual.exe --title "Build %BUILD_NUM%" --notes "Versao/Build %BUILD_NUM% do BuscaTextual" --latest

if errorlevel 1 (
    echo [AVISO] Falha ao criar a release via GitHub CLI. Tentando atualizar release existente...
    gh release upload "v%BUILD_NUM%" buscatextual.exe --clobber
)

echo.
echo ==================================================
echo   Release v%BUILD_NUM% criada e enviada com sucesso!
echo ==================================================
pause
endlocal
