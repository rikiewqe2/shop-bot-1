package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Config определяет структуру для хранения конфигурационных данных
type Config struct {
	ClientBotToken string
	AdminBotToken  string
	CryptoPayToken string
	SupportAccount string
}

// LoadConfig загружает конфигурацию из .env файла
func LoadConfig() Config {
	// Загружаем .env файл
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: Error loading .env file: %v. Falling back to environment variables.", err)
	}

	// Получаем значения из переменных окружения
	config := Config{
		ClientBotToken: getEnv("CLIENT_BOT_TOKEN", ""),
		AdminBotToken:  getEnv("ADMIN_BOT_TOKEN", ""),
		CryptoPayToken: getEnv("CRYPTO_PAY_TOKEN", ""),
		SupportAccount: getEnv("SUPPORT_ACCOUNT", "@SupportBot"),
	}

	// Проверяем, что все обязательные переменные установлены
	if config.ClientBotToken == "" {
		log.Fatal("Error: CLIENT_BOT_TOKEN is not set")
	}
	if config.AdminBotToken == "" {
		log.Fatal("Error: ADMIN_BOT_TOKEN is not set")
	}
	if config.CryptoPayToken == "" {
		log.Fatal("Error: CRYPTO_PAY_TOKEN is not set")
	}

	return config
}

// getEnv получает значение переменной окружения или возвращает значение по умолчанию
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
