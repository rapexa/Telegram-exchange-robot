#!/bin/bash

# Supervisor Installation Script for Telegram Exchange Bot

echo "🔧 Installing Supervisor for Telegram Exchange Bot..."

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "❌ Please run as root (use sudo)"
    exit 1
fi

# Update package list
echo "📦 Updating package list..."
apt update

# Install Supervisor
echo "📦 Installing Supervisor..."
apt install -y supervisor

# Create logs directory if it doesn't exist
echo "📁 Creating logs directory..."
mkdir -p /var/www/Telegram-exchange-robot/logs
chown -R www-data:www-data /var/www/Telegram-exchange-robot/logs

# Copy supervisor configuration
echo "⚙️ Installing supervisor configuration..."
cp supervisor.conf /etc/supervisor/conf.d/bot.conf

# Make bot executable
echo "🔧 Making bot executable..."
chmod +x /var/www/Telegram-exchange-robot/bot

# Set proper permissions
echo "🔐 Setting proper permissions..."
chown -R www-data:www-data /var/www/Telegram-exchange-robot
chmod 755 /var/www/Telegram-exchange-robot
chmod 644 /var/www/Telegram-exchange-robot/config/config.yaml

# Reload supervisor configuration
echo "🔄 Reloading supervisor configuration..."
supervisorctl reread
supervisorctl update

# Start the bot
echo "🚀 Starting Telegram bot..."
supervisorctl start bot

# Check status
echo "📊 Checking bot status..."
supervisorctl status bot

echo ""
echo "✅ Installation completed!"
echo ""
echo "📋 Useful commands:"
echo "   supervisorctl status bot    # Check bot status"
echo "   supervisorctl restart bot   # Restart bot"
echo "   supervisorctl stop bot      # Stop bot"
echo "   supervisorctl start bot     # Start bot"
echo "   tail -f /var/www/Telegram-exchange-robot/logs/bot.log      # View bot logs"
echo "   tail -f /var/www/Telegram-exchange-robot/logs/supervisor.log # View supervisor logs"
echo ""
echo "🔧 Supervisor web interface (optional):"
echo "   - Edit /etc/supervisor/supervisord.conf"
echo "   - Add: [inet_http_server] section"
echo "   - Restart: systemctl restart supervisor" 