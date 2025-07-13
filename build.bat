@echo off
REM Production Build Script for Telegram Exchange Bot (Windows)

echo 🔨 Building Telegram Exchange Bot for Production...

REM Set build time and version
for /f "tokens=2 delims==" %%a in ('wmic OS Get localdatetime /value') do set "dt=%%a"
set "BUILD_TIME=%dt:~0,4%-%dt:~4,2%-%dt:~6,2%_%dt:~8,2%:%dt:~10,2%:%dt:~12,2%"
set "VERSION=0.0.1"

REM Build the executable
echo 📦 Building executable...
go build -ldflags "-s -w -X main.Build=%BUILD_TIME% -X main.Version=%VERSION%" -o bot.exe .

if %ERRORLEVEL% EQU 0 (
    echo ✅ Build successful!
    echo 📁 Executable: bot.exe
    echo 📅 Build Time: %BUILD_TIME%
    echo 🏷️ Version: %VERSION%
    echo.
    echo 🚀 To run the bot:
    echo    bot.exe
    echo.
    echo 📋 Make sure to:
    echo    1. Set up config/config.yaml with your credentials
    echo    2. Ensure MySQL is running
    echo    3. Check logs/bot.log for runtime logs
) else (
    echo ❌ Build failed!
    exit /b 1
) 