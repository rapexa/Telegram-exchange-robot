#!/bin/bash

# Production Build Script for Telegram Exchange Bot

echo "🔨 Building Telegram Exchange Bot for Production..."

# Set build time and version
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')
VERSION="1.0.0"

# Build flags for production
BUILD_FLAGS="-ldflags=-s -ldflags=-w -ldflags=-X main.Build=$BUILD_TIME -ldflags=-X main.Version=$VERSION"

# Build the executable
echo "📦 Building executable..."
GOOS=linux GOARCH=amd64 go build $BUILD_FLAGS -o bot main.go

if [ $? -eq 0 ]; then
    echo "✅ Build successful!"
    echo "📁 Executable: telegram-exchange-bot"
    echo "📅 Build Time: $BUILD_TIME"
    echo "🏷️ Version: $VERSION"
    echo ""
    echo "🚀 To run the bot:"
    echo "   ./bot"
    echo ""
    echo "📋 Make sure to:"
    echo "   1. Set up config/config.yaml with your credentials"
    echo "   2. Ensure MySQL is running"
    echo "   3. Check logs/bot.log for runtime logs"
else
    echo "❌ Build failed!"
    exit 1
fi 