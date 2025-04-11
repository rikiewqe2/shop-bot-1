package main

import (
	"log"
	"shop-bot/config"
	"shop-bot/db"
	"shop-bot/handlers"
	"github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	// Инициализация базы данных
	if err := db.InitDB(); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}

	// Загрузка конфигурации
	cfg := config.LoadConfig()

	// Создание клиентского бота
	clientBot, err := tgbotapi.NewBotAPI(cfg.ClientBotToken)
	if err != nil {
		log.Fatal("Failed to create client bot:", err)
	}

	// Создание админского бота
	adminBot, err := tgbotapi.NewBotAPI(cfg.AdminBotToken)
	if err != nil {
		log.Fatal("Failed to create admin bot:", err)
	}

	log.Printf("Client bot authorized as %s", clientBot.Self.UserName)
	log.Printf("Admin bot authorized as %s", adminBot.Self.UserName)

	// Запуск обработчиков
	go handlers.HandleClientBot(clientBot)
	go handlers.HandleAdminBot(adminBot)

	// Бесконечный цикл для удержания программы
	select {}
}