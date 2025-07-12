# 🚀 Production Deployment Guide

## 📋 Overview

This guide covers deploying the Telegram Exchange Bot in production with proper logging and monitoring.

## 🔨 Building for Production

### Linux/macOS
```bash
chmod +x build.sh
./build.sh
```

### Windows
```cmd
build.bat
```

## 📁 Production Structure

```
telegram-exchange-robot/
├── telegram-exchange-bot.exe    # Production executable
├── config/
│   └── config.yaml             # Configuration file
├── logs/
│   └── bot.log                 # Application logs
└── README.md
```

## ⚙️ Configuration

### config/config.yaml
```yaml
mysql:
  user: "your_db_user"
  password: "your_db_password"
  host: "localhost"
  port: 3306
  dbname: "telegram_bot"

telegram:
  token: "YOUR_BOT_TOKEN"
  debug: false  # Set to false in production
```

## 📊 Logging System

### Log Levels
- **[INFO]** - General information and successful operations
- **[ERROR]** - Errors and failures
- **[DEBUG]** - Detailed debugging information

### Log Format
```
2025/07/12 23:41:29 [INFO] 🚀 Telegram Exchange Bot Starting...
2025/07/12 23:41:29 [INFO] 📦 Version: 1.0.0
2025/07/12 23:41:29 [INFO] 🔨 Build: 2025-07-12_23:41:29
```

### Log File Location
- **File:** `logs/bot.log`
- **Rotation:** Manual (consider using logrotate)
- **Permissions:** 0666

## 🚀 Running in Production

### Direct Execution
```bash
./telegram-exchange-bot
```

### Using Systemd (Linux)
```bash
# Create service file
sudo nano /etc/systemd/system/telegram-bot.service
```

```ini
[Unit]
Description=Telegram Exchange Bot
After=network.target mysql.service

[Service]
Type=simple
User=botuser
WorkingDirectory=/path/to/telegram-exchange-robot
ExecStart=/path/to/telegram-exchange-robot/telegram-exchange-bot
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

```bash
# Enable and start service
sudo systemctl enable telegram-bot
sudo systemctl start telegram-bot
sudo systemctl status telegram-bot
```

### Using PM2 (Node.js Process Manager)
```bash
# Install PM2
npm install -g pm2

# Create ecosystem file
pm2 ecosystem
```

```javascript
module.exports = {
  apps: [{
    name: 'telegram-bot',
    script: './telegram-exchange-bot',
    cwd: '/path/to/telegram-exchange-robot',
    instances: 1,
    autorestart: true,
    watch: false,
    max_memory_restart: '1G',
    env: {
      NODE_ENV: 'production'
    }
  }]
}
```

```bash
# Start with PM2
pm2 start ecosystem.config.js
pm2 save
pm2 startup
```

## 📈 Monitoring

### Key Metrics to Monitor
1. **Bot Response Time** - Check logs for slow operations
2. **Database Connections** - Monitor MySQL connection pool
3. **Memory Usage** - Watch for memory leaks
4. **Error Rate** - Count [ERROR] log entries
5. **User Registration Rate** - Track new user signups

### Log Analysis Commands
```bash
# Count errors in last hour
grep "$(date '+%Y/%m/%d %H')" logs/bot.log | grep "\[ERROR\]" | wc -l

# Monitor real-time logs
tail -f logs/bot.log

# Search for specific user activity
grep "User ID: 123456789" logs/bot.log

# Check registration success rate
grep "Registration completed successfully" logs/bot.log | wc -l
```

## 🔒 Security Best Practices

1. **Database Security**
   - Use strong passwords
   - Limit database user permissions
   - Enable SSL connections

2. **Bot Token Security**
   - Keep token in secure config file
   - Use environment variables if possible
   - Rotate tokens regularly

3. **File Permissions**
   ```bash
   chmod 600 config/config.yaml
   chmod 755 telegram-exchange-bot
   chmod 755 logs/
   ```

4. **Network Security**
   - Use firewall rules
   - Limit database access to bot server only
   - Use VPN if needed

## 🛠️ Troubleshooting

### Common Issues

1. **Bot Not Responding**
   ```bash
   # Check if bot is running
   ps aux | grep telegram-exchange-bot
   
   # Check logs for errors
   tail -n 50 logs/bot.log
   ```

2. **Database Connection Issues**
   ```bash
   # Test database connection
   mysql -u your_user -p your_database
   
   # Check MySQL status
   sudo systemctl status mysql
   ```

3. **Permission Issues**
   ```bash
   # Fix log file permissions
   sudo chown botuser:botuser logs/bot.log
   chmod 666 logs/bot.log
   ```

### Log Analysis
```bash
# Find all errors
grep "\[ERROR\]" logs/bot.log

# Find user registration issues
grep "registration" logs/bot.log | grep -i error

# Monitor real-time activity
tail -f logs/bot.log | grep "User ID:"
```

## 📞 Support

For production issues:
1. Check logs first: `tail -n 100 logs/bot.log`
2. Verify configuration: `cat config/config.yaml`
3. Test database connection
4. Check system resources: `htop`, `df -h`

## 🔄 Updates

When updating the bot:
1. Stop the current instance
2. Backup the database
3. Replace the executable
4. Update configuration if needed
5. Restart the service
6. Monitor logs for any issues 