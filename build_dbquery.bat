@echo off
chcp 65001 >nul 2>&1
setlocal
cd /d "%~dp0"

echo === dbquery build ===
echo.
set "CGO_ENABLED=0"
go build -ldflags="-s -w" -o dbquery.exe ./tools/dbquery
if %errorlevel% neq 0 (
    echo ERROR
    pause
    exit /b 1
)
echo     OK  =^>  dbquery.exe
echo.
pause