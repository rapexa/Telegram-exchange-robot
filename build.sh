#!/bin/bash

# Production Build Script for Telegram Exchange Bot

echo "🔨 Building Telegram Exchange Bot for Production..."

# Set build time and version
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')
VERSION="0.0.1"

# Build flags for production (corrected syntax)
BUILD_FLAGS="-ldflags=-s -ldflags=-w -ldflags=-X main.Build=$BUILD_TIME -ldflags=-X main.Version=$VERSION"

# Build the executable
echo "📦 Building executable..."
GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X main.Build=$BUILD_TIME -X main.Version=$VERSION" -o bot .

if [ $? -eq 0 ]; then
    echo "✅ Build successful!"
    echo "📁 Executable: bot"
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