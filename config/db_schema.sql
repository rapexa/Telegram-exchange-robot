-- SQL schema and setup for Telegram-exchange-robot

-- 1. Create the database (if not exists)
CREATE DATABASE IF NOT EXISTS telegram_exchange_bot CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- 2. Create the user (if not exists)
CREATE USER IF NOT EXISTS 'telegram_exchange_user'@'localhost' IDENTIFIED BY 'mgstudio884';

-- 3. Grant all privileges on the database to the user
GRANT ALL PRIVILEGES ON telegram_exchange_bot.* TO 'telegram_exchange_user'@'localhost';
FLUSH PRIVILEGES;

-- 4. Use the database
USE telegram_bot;

-- 5. Create the users table
CREATE TABLE IF NOT EXISTS users (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    username VARCHAR(255),
    telegram_id BIGINT UNIQUE,
    full_name VARCHAR(255),
    sheba VARCHAR(32),
    card_number VARCHAR(32),
    registered BOOL DEFAULT FALSE,
    referrer_id BIGINT NULL,
    referral_reward DOUBLE DEFAULT 0,
    FOREIGN KEY (referrer_id) REFERENCES users(id),
    -- Wallet fields
    erc20_address VARCHAR(64),
    erc20_mnemonic VARCHAR(256),
    erc20_priv_key VARCHAR(128),
    bep20_address VARCHAR(64),
    bep20_mnemonic VARCHAR(256),
    bep20_priv_key VARCHAR(128)
);

CREATE TABLE IF NOT EXISTS transactions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT,
    type VARCHAR(16), -- deposit or withdraw
    network VARCHAR(16), -- ERC20 or BEP20
    amount DOUBLE,
    tx_hash VARCHAR(128),
    status VARCHAR(16), -- pending, confirmed, failed
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at DATETIME,
    FOREIGN KEY (user_id) REFERENCES users(id)
); 