package handlers

import (
	"fmt"
	"log"
	"shop-bot/db"
	"shop-bot/models"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func HandleAdminBot(bot *tgbotapi.BotAPI) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	// Переменная для хранения состояния
	type adminState struct {
		awaitingUserIDForBalanceCheck  int64
		awaitingUserIDForBalanceChange int64
		awaitingBalanceChange          bool
		awaitingGoodInput              bool // Ожидание ввода данных для товара
		awaitingServiceInput           bool // Ожидание ввода данных для услуги
	}
	state := adminState{}

	for update := range updates {
		if update.Message == nil {
			continue
		}

		chatID := update.Message.Chat.ID
		userID := update.Message.From.ID

		log.Printf("Received message from user %d: %s", userID, update.Message.Text)

		switch update.Message.Text {
		case "/start":
			msg := tgbotapi.NewMessage(chatID, "Админ-панель. Выберите действие:")
			msg.ReplyMarkup = adminMenu()
			bot.Send(msg)

		case "Добавить товар":
			msg := tgbotapi.NewMessage(chatID, "Введите данные товара в формате: название,цена,описание")
			bot.Send(msg)
			state.awaitingGoodInput = true
			log.Printf("State updated: awaitingGoodInput = %v", state.awaitingGoodInput)

		case "Добавить услугу":
			msg := tgbotapi.NewMessage(chatID, "Введите данные услуги в формате: название,цена,описание")
			bot.Send(msg)
			state.awaitingServiceInput = true
			log.Printf("State updated: awaitingServiceInput = %v", state.awaitingServiceInput)

		case "Статистика":
			var userCount int
			db.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount)
			response := fmt.Sprintf("Пользователей: %d", userCount)
			msg := tgbotapi.NewMessage(chatID, response)
			bot.Send(msg)

		case "Посмотреть баланс пользователя":
			msg := tgbotapi.NewMessage(chatID, "Введите ID пользователя для проверки баланса:")
			bot.Send(msg)
			state.awaitingUserIDForBalanceCheck = userID
			log.Printf("State updated: awaitingUserIDForBalanceCheck = %d", state.awaitingUserIDForBalanceCheck)

		case "Изменить баланс пользователя":
			msg := tgbotapi.NewMessage(chatID, "Введите ID пользователя для изменения баланса:")
			bot.Send(msg)
			state.awaitingUserIDForBalanceChange = userID
			log.Printf("State updated: awaitingUserIDForBalanceChange = %d", state.awaitingUserIDForBalanceChange)

		default:
			// Обработка добавления товара или услуги
			if strings.Contains(update.Message.Text, ",") {
				parts := strings.Split(update.Message.Text, ",")
				if len(parts) == 3 {
					name := parts[0]
					value, err := strconv.ParseFloat(parts[1], 64)
					if err != nil {
						msg := tgbotapi.NewMessage(chatID, "Ошибка: цена должна быть числом.")
						bot.Send(msg)
						continue
					}
					descr := parts[2]

					// Добавление товара
					if state.awaitingGoodInput {
						if err := db.AddGood(models.Good{Name: name, Value: value, Descr: descr}); err != nil {
							log.Println("Failed to add good:", err)
							msg := tgbotapi.NewMessage(chatID, "Ошибка при добавлении товара.")
							bot.Send(msg)
							continue
						}
						msg := tgbotapi.NewMessage(chatID, "Товар добавлен!")
						bot.Send(msg)
						state.awaitingGoodInput = false
						log.Printf("State reset: awaitingGoodInput = %v", state.awaitingGoodInput)

						// Добавление услуги
					} else if state.awaitingServiceInput {
						if err := db.AddService(models.Service{Name: name, Value: value, Descr: descr}); err != nil {
							log.Println("Failed to add service:", err)
							msg := tgbotapi.NewMessage(chatID, "Ошибка при добавлении услуги.")
							bot.Send(msg)
							continue
						}
						msg := tgbotapi.NewMessage(chatID, "Услуга добавлена!")
						bot.Send(msg)
						state.awaitingServiceInput = false
						log.Printf("State reset: awaitingServiceInput = %v", state.awaitingServiceInput)
					}
				}
			} else if id, err := strconv.ParseInt(update.Message.Text, 10, 64); err == nil {
				// Обработка проверки и изменения баланса
				log.Printf("Processing ID %d for user %d", id, userID)

				// Проверка баланса
				if state.awaitingUserIDForBalanceCheck == userID {
					log.Printf("Checking balance for user %d", id)
					balance, err := db.GetUserBalance(id)
					var msg tgbotapi.MessageConfig
					if err != nil {
						log.Println("Failed to get user balance:", err)
						msg = tgbotapi.NewMessage(chatID, "Ошибка: не удалось получить баланс.")
					} else {
						msg = tgbotapi.NewMessage(chatID, fmt.Sprintf("Баланс пользователя %d: %.2f ₽", id, balance))
					}
					bot.Send(msg)
					state.awaitingUserIDForBalanceCheck = 0
					log.Printf("State reset: awaitingUserIDForBalanceCheck = %d", state.awaitingUserIDForBalanceCheck)

					// Подготовка к изменению баланса
				} else if state.awaitingUserIDForBalanceChange == userID {
					log.Printf("Preparing to change balance for user %d", id)
					msg := tgbotapi.NewMessage(chatID, "Введите новый баланс (например, 100.50):")
					bot.Send(msg)
					state.awaitingBalanceChange = true
					state.awaitingUserIDForBalanceChange = id
					log.Printf("State updated: awaitingBalanceChange = %v, awaitingUserIDForBalanceChange = %d", state.awaitingBalanceChange, state.awaitingUserIDForBalanceChange)
				}

			} else if state.awaitingBalanceChange && state.awaitingUserIDForBalanceChange != 0 {
				log.Printf("Processing balance change for user %d", state.awaitingUserIDForBalanceChange)
				// Обработка ввода нового баланса
				newBalance, err := strconv.ParseFloat(update.Message.Text, 64)
				if err != nil {
					msg := tgbotapi.NewMessage(chatID, "Ошибка: введите корректное число (например, 100.50).")
					bot.Send(msg)
					continue
				}

				// Обновляем баланс
				var msg tgbotapi.MessageConfig
				if err := db.UpdateUserBalance(state.awaitingUserIDForBalanceChange, newBalance); err != nil {
					log.Println("Failed to update user balance:", err)
					msg = tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка: %v", err))
				} else {
					msg = tgbotapi.NewMessage(chatID, fmt.Sprintf("Баланс пользователя %d обновлен: %.2f ₽", state.awaitingUserIDForBalanceChange, newBalance))
				}
				bot.Send(msg)

				// Сбрасываем состояние
				state.awaitingBalanceChange = false
				state.awaitingUserIDForBalanceChange = 0
				log.Printf("State reset: awaitingBalanceChange = %v, awaitingUserIDForBalanceChange = %d", state.awaitingBalanceChange, state.awaitingUserIDForBalanceChange)
			}
		}
	}
}

func adminMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Добавить товар"),
			tgbotapi.NewKeyboardButton("Добавить услугу"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Статистика"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Посмотреть баланс пользователя"),
			tgbotapi.NewKeyboardButton("Изменить баланс пользователя"),
		),
	)
}
