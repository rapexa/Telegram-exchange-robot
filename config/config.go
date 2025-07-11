package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type MySQLConfig struct {
	User     string
	Password string
	Host     string
	Port     int
	DBName   string
}

func (m MySQLConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true", m.User, m.Password, m.Host, m.Port, m.DBName)
}

type TelegramConfig struct {
	Token string
	Debug bool
}

type Config struct {
	MySQL    MySQLConfig
	Telegram TelegramConfig
}

func LoadConfig() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./config")

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
