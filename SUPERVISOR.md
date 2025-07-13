# 🚀 Supervisor Setup Guide for Telegram Exchange Bot

## 📋 Overview

This guide covers setting up the Telegram Exchange Bot with Supervisor for production deployment on Linux servers.

## 🔧 Prerequisites

- Linux server (Ubuntu/Debian recommended)
- Root access or sudo privileges
- Go 1.19+ installed
- MySQL server running
- Bot token from @BotFather

## 📦 Installation Steps

### 1. Build the Bot
```bash
# Navigate to project directory
cd /var/www/Telegram-exchange-robot

# Build for production
./build.sh
```

### 2. Configure the Bot
```bash
# Edit configuration file
nano config/config.yaml
```

**Example config:**
```yaml
mysql:
  user: "your_db_user"
  password: "your_secure_password"
  host: "localhost"
  port: 3306
  dbname: "telegram_bot"

telegram:
  token: "YOUR_BOT_TOKEN_HERE"
  debug: false
```

### 3. Install Supervisor
```bash
# Make installation script executable
chmod +x install-supervisor.sh

# Run installation script
sudo ./install-supervisor.sh
```

## 🎯 Supervisor Configuration Details

### Configuration File: `supervisor.conf`
```ini
[program:bot]
command=/var/www/Telegram-exchange-robot/bot
directory=/var/www/Telegram-exchange-robot
user=www-data
autostart=true
autorestart=true
startretries=3
redirect_stderr=true
stdout_logfile=/var/www/Telegram-exchange-robot/logs/supervisor.log
stdout_logfile_maxbytes=50MB
stdout_logfile_backups=10
environment=HOME="/var/www/Telegram-exchange-robot"
stopsignal=TERM
stopwaitsecs=10
```

### Key Settings Explained:
- **`command`**: Path to the bot executable
- **`directory`**: Working directory for the bot
- **`user`**: User account to run the bot (www-data for web servers)
- **`autostart`**: Start automatically when supervisor starts
- **`autorestart`**: Restart automatically if it crashes
- **`startretries`**: Number of restart attempts
- **`stdout_logfile`**: Supervisor's log file for the bot
- **`stopsignal`**: Signal to stop the bot gracefully

## 🛠️ Management Commands

### Check Bot Status
```bash
supervisorctl status bot
```

### Start the Bot
```bash
supervisorctl start bot
```

### Stop the Bot
```bash
supervisorctl stop bot
```

### Restart the Bot
```bash
supervisorctl restart bot
```

### View All Supervisor Processes
```bash
supervisorctl status
```

### Reload Configuration
```bash
supervisorctl reread
supervisorctl update
```

## 📊 Monitoring and Logs

### Bot Application Logs
```bash
# Real-time bot logs
tail -f /var/www/Telegram-exchange-robot/logs/bot.log

# Last 100 lines
tail -n 100 /var/www/Telegram-exchange-robot/logs/bot.log

# Search for errors
grep "\[ERROR\]" /var/www/Telegram-exchange-robot/logs/bot.log
```

### Supervisor Logs
```bash
# Real-time supervisor logs
tail -f /var/www/Telegram-exchange-robot/logs/supervisor.log

# Supervisor main log
tail -f /var/log/supervisor/supervisord.log
```

### System Logs
```bash
# Check system service status
systemctl status supervisor

# View system logs
journalctl -u supervisor -f
```

## 🔒 Security Best Practices

### File Permissions
```bash
# Set proper ownership
sudo chown -R www-data:www-data /var/www/Telegram-exchange-robot

# Set proper permissions
sudo chmod 755 /var/www/Telegram-exchange-robot
sudo chmod 755 /var/www/Telegram-exchange-robot/bot
sudo chmod 644 /var/www/Telegram-exchange-robot/config/config.yaml
sudo chmod 755 /var/www/Telegram-exchange-robot/logs
```

### Database Security
```bash
# Create dedicated database user
mysql -u root -p
```

```sql
CREATE DATABASE telegram_bot;
CREATE USER 'bot_user'@'localhost' IDENTIFIED BY 'strong_password';
GRANT ALL PRIVILEGES ON telegram_bot.* TO 'bot_user'@'localhost';
FLUSH PRIVILEGES;
```

### Firewall Configuration
```bash
# Allow only necessary ports
sudo ufw allow 22    # SSH
sudo ufw allow 80    # HTTP (if needed)
sudo ufw allow 443   # HTTPS (if needed)
sudo ufw enable
```

## 🔧 Troubleshooting

### Bot Not Starting
```bash
# Check supervisor status
supervisorctl status bot

# Check supervisor logs
tail -n 50 /var/www/Telegram-exchange-robot/logs/supervisor.log

# Check bot logs
tail -n 50 /var/www/Telegram-exchange-robot/logs/bot.log

# Check file permissions
ls -la /var/www/Telegram-exchange-robot/bot
```

### Permission Issues
```bash
# Fix ownership
sudo chown -R www-data:www-data /var/www/Telegram-exchange-robot

# Fix permissions
sudo chmod +x /var/www/Telegram-exchange-robot/bot
```

### Database Connection Issues
```bash
# Test database connection
mysql -u your_user -p your_database

# Check MySQL status
sudo systemctl status mysql
```

### Memory Issues
```bash
# Check memory usage
free -h

# Check bot memory usage
ps aux | grep bot
```

## 🔄 Updates and Maintenance

### Updating the Bot
```bash
# Stop the bot
supervisorctl stop bot

# Backup current version
cp /var/www/Telegram-exchange-robot/bot /var/www/Telegram-exchange-robot/bot.backup

# Build new version
./build.sh

# Start the bot
supervisorctl start bot

# Check status
supervisorctl status bot
```

### Log Rotation
```bash
# Create logrotate configuration
sudo nano /etc/logrotate.d/bot
```

```conf
/var/www/Telegram-exchange-robot/logs/*.log {
    daily
    missingok
    rotate 30
    compress
    delaycompress
    notifempty
    create 644 www-data www-data
    postrotate
        supervisorctl restart bot
    endscript
}
```

## 📈 Performance Monitoring

### Resource Monitoring
```bash
# Monitor CPU and memory
htop

# Monitor disk usage
df -h

# Monitor network connections
netstat -tulpn | grep bot
```

### Bot Performance Metrics
```bash
# Count successful registrations
grep "Registration completed successfully" /var/www/Telegram-exchange-robot/logs/bot.log | wc -l

# Count errors in last hour
grep "$(date '+%Y/%m/%d %H')" /var/www/Telegram-exchange-robot/logs/bot.log | grep "\[ERROR\]" | wc -l

# Monitor response times
grep "User ID:" /var/www/Telegram-exchange-robot/logs/bot.log | tail -n 10
```

## 🆘 Emergency Procedures

### Emergency Stop
```bash
# Stop bot immediately
supervisorctl stop bot

# Kill process if needed
pkill -f bot
```

### Emergency Restart
```bash
# Restart supervisor
sudo systemctl restart supervisor

# Restart bot
supervisorctl restart bot
```

### Rollback to Previous Version
```bash
# Stop bot
supervisorctl stop bot

# Restore backup
cp /var/www/Telegram-exchange-robot/bot.backup /var/www/Telegram-exchange-robot/bot

# Start bot
supervisorctl start bot
```

## 📞 Support

For issues with Supervisor setup:
1. Check supervisor status: `supervisorctl status`
2. Check logs: `tail -f /var/www/Telegram-exchange-robot/logs/bot.log`
3. Check system resources: `htop`, `df -h`
4. Verify configuration: `cat /etc/supervisor/conf.d/bot.conf` 