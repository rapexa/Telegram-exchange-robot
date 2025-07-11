# Telegram Exchange Robot

A secure, decentralized, and user-friendly Telegram bot for handling USDT deposits, fiat withdrawals, KYC onboarding, referral rewards, and admin financial management. Built for seamless interaction and automation within the Telegram ecosystem.

## Features
- User registration with KYC (full name, Sheba, card number)
- USDT deposit and fiat withdrawal menu
- Referral rewards system
- Personal and referral statistics
- Admin support contact
- Automatic database migrations (GORM)

## Requirements
- Go 1.23+
- MySQL 5.7+/8.0+
- Telegram Bot Token

## Installation
1. **Clone the repository:**
   ```sh
   git clone https://github.com/rapexa/Telegram-exchange-robot.git
   cd Telegram-exchange-robot
   ```
2. **Install dependencies:**
   ```sh
   go mod tidy
   ```

## Configuration
Edit `config/config.yaml` with your database and Telegram bot credentials:
```yaml
mysql:
  user: "telegram_exchange_user"
  password: "mgstudio884"
  host: "localhost"
  port: 3306
  dbname: "telegram_exchange_bot"

tg:
  token: "YOUR_TELEGRAM_BOT_TOKEN"
  debug: true
```

## Database Setup
You can let the bot auto-migrate the schema (recommended), or set up the database manually:

### 1. Create Database and User (manual, optional)
Edit and run `config/db_schema.sql` in your MySQL server:
```sql
-- Create database and user
CREATE DATABASE IF NOT EXISTS telegram_exchange_bot CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS 'telegram_exchange_user'@'localhost' IDENTIFIED BY 'mgstudio884';
GRANT ALL PRIVILEGES ON telegram_exchange_bot.* TO 'telegram_exchange_user'@'localhost';
FLUSH PRIVILEGES;
```

### 2. Auto-migration (recommended)
The bot will automatically create the required tables on startup using GORM.

## Usage
1. **Run the bot:**
   ```sh
   go run main.go
   ```
2. **Interact with your bot on Telegram:**
   - `/start` to begin registration
   - Use the main menu for wallet, rewards, stats, and support

## User Registration Flow
- User starts the bot and is prompted for:
  1. Full name
  2. Sheba number
  3. Card number
- After registration, user can access wallet, rewards, and stats menus.

## Project Structure
- `main.go` — Entry point, config loading, DB connection, bot startup
- `config/` — Configuration files and SQL schema
- `models/` — GORM models (currently only `User`)
- `handlers/` — Telegram bot logic and menu handlers

## Dependencies
- [GORM](https://gorm.io/) (MySQL driver)
- [go-telegram-bot-api](https://github.com/go-telegram-bot-api/telegram-bot-api)
- [Viper](https://github.com/spf13/viper) (config)

## License
This project is licensed under the GNU General Public License v3.0. See the [LICENSE](LICENSE) file for details.
