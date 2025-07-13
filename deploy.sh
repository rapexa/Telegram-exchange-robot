#!/bin/bash

# Complete Deployment Script for Telegram Exchange Bot

echo "🚀 Complete Deployment Script for Telegram Exchange Bot"
echo "=================================================="

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "❌ Please run as root (use sudo)"
    exit 1
fi

# Check if we're in the right directory
if [ ! -f "main.go" ]; then
    echo "❌ Please run this script from the project root directory"
    exit 1
fi

# Step 1: Build the bot
echo "🔨 Step 1: Building the bot..."
if [ -f "build.sh" ]; then
    chmod +x build.sh
    ./build.sh
    if [ $? -ne 0 ]; then
        echo "❌ Build failed!"
        exit 1
    fi
else
    echo "❌ build.sh not found!"
    exit 1
fi

# Step 2: Check if bot executable exists
if [ ! -f "bot" ]; then
    echo "❌ Bot executable not found after build!"
    exit 1
fi

# Step 3: Check configuration
echo "⚙️ Step 2: Checking configuration..."
if [ ! -f "config/config.yaml" ]; then
    echo "❌ config/config.yaml not found!"
    echo "📝 Please create the configuration file first"
    exit 1
fi

# Step 4: Install Supervisor
echo "📦 Step 3: Installing Supervisor..."
apt update
apt install -y supervisor

# Step 5: Create logs directory
echo "📁 Step 4: Setting up directories..."
mkdir -p logs
chown -R www-data:www-data logs

# Step 6: Set proper permissions
echo "🔐 Step 5: Setting permissions..."
chown -R www-data:www-data .
chmod +x bot
chmod 644 config/config.yaml
chmod 755 logs

# Step 7: Install supervisor configuration
echo "⚙️ Step 6: Installing supervisor configuration..."
if [ -f "supervisor.conf" ]; then
    cp supervisor.conf /etc/supervisor/conf.d/bot.conf
else
    echo "❌ supervisor.conf not found!"
    exit 1
fi

# Step 8: Reload supervisor
echo "🔄 Step 7: Reloading supervisor..."
supervisorctl reread
supervisorctl update

# Step 9: Start the bot
echo "🚀 Step 8: Starting the bot..."
supervisorctl start bot

# Step 10: Check status
echo "📊 Step 9: Checking status..."
sleep 3
supervisorctl status bot

echo ""
echo "✅ Deployment completed successfully!"
echo ""
echo "📋 Quick Commands:"
echo "   supervisorctl status bot    # Check status"
echo "   supervisorctl restart bot   # Restart bot"
echo "   tail -f logs/bot.log                 # View logs"
echo ""
echo "📁 Files created:"
echo "   /etc/supervisor/conf.d/bot.conf"
echo "   logs/bot.log"
echo "   logs/supervisor.log"
echo ""
echo "🔧 Next steps:"
echo "   1. Test your bot with /start"
echo "   2. Monitor logs: tail -f logs/bot.log"
echo "   3. Set up log rotation if needed"
echo "   4. Configure firewall rules" 