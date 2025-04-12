package handlers

import (
	"fmt"
	"log"
	"shop-bot/db"
	"shop-bot/models"
	"strconv"
	"strings"
	"time"

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
		awaitingGoodInput              bool
		awaitingServiceInput           bool
		awaitingGoodEdit               bool
		awaitingServiceEdit            bool
		goodIDToEdit                   int
		serviceIDToEdit                int
		goodIDToDelete                 int
		serviceIDToDelete              int
		awaitingGoodDelete             bool
		awaitingServiceDelete          bool
		awaitingCryptoPayToken         bool
		awaitingSupportAccount         bool
		currentMenu                    string // "main", "catalog", "goods", "services", "settings", "stats"
	}
	state := adminState{currentMenu: "main"}

	for update := range updates {
		if update.Message == nil && update.CallbackQuery == nil {
			continue
		}

		var chatID int64
		var userID int64
		var messageText string

		if update.Message != nil {
			chatID = update.Message.Chat.ID
			userID = update.Message.From.ID
			messageText = strings.TrimSpace(update.Message.Text)
			log.Printf("Received message from user %d: %s (currentMenu: %s)", userID, messageText, state.currentMenu)
		} else if update.CallbackQuery != nil {
			chatID = update.CallbackQuery.Message.Chat.ID
			userID = update.CallbackQuery.From.ID
			messageText = update.CallbackQuery.Data
			log.Printf("Received callback from user %d: %s (currentMenu: %s)", userID, messageText, state.currentMenu)
		}

		// Обработка отмены
		if update.Message != nil && messageText == "Отмена" {
			if state.awaitingUserIDForBalanceCheck != 0 ||
				state.awaitingUserIDForBalanceChange != 0 ||
				state.awaitingBalanceChange ||
				state.awaitingGoodInput ||
				state.awaitingServiceInput ||
				state.awaitingGoodEdit ||
				state.awaitingServiceEdit ||
				state.awaitingGoodDelete ||
				state.awaitingServiceDelete ||
				state.awaitingCryptoPayToken ||
				state.awaitingSupportAccount {
				// Сбрасываем все состояния
				state.awaitingUserIDForBalanceCheck = 0
				state.awaitingUserIDForBalanceChange = 0
				state.awaitingBalanceChange = false
				state.awaitingGoodInput = false
				state.awaitingServiceInput = false
				state.awaitingGoodEdit = false
				state.awaitingServiceEdit = false
				state.goodIDToEdit = 0
				state.serviceIDToEdit = 0
				state.awaitingGoodDelete = false
				state.awaitingServiceDelete = false
				state.awaitingCryptoPayToken = false
				state.awaitingSupportAccount = false

				msg := tgbotapi.NewMessage(chatID, "Действие отменено. Возвращаемся в главное меню:")
				msg.ReplyMarkup = adminMenu()
				state.currentMenu = "main"
				bot.Send(msg)
				continue
			}
		}

		// Обработка callback-запросов
		if update.CallbackQuery != nil {
			callback := update.CallbackQuery
			callbackData := callback.Data

			// Удаляем предыдущее сообщение
			if callback.Message.MessageID != 0 {
				deleteMsg := tgbotapi.NewDeleteMessage(chatID, callback.Message.MessageID)
				bot.Send(deleteMsg)
			}

			var msg tgbotapi.MessageConfig

			switch callbackData {
			case "stats_day", "stats_week", "stats_month", "stats_all":
				var startTime int64
				now := time.Now().Unix()

				switch callbackData {
				case "stats_day":
					startTime = time.Now().AddDate(0, 0, -1).Unix()
				case "stats_week":
					startTime = time.Now().AddDate(0, 0, -7).Unix()
				case "stats_month":
					startTime = time.Now().AddDate(0, -1, 0).Unix()
				case "stats_all":
					startTime = 0 // Все время
				}

				// Получаем платежи и покупки
				payments, err := db.GetPaymentsByTime(startTime, now)
				if err != nil {
					msg = tgbotapi.NewMessage(chatID, "Ошибка получения платежей.")
					bot.Send(msg)
					bot.Send(tgbotapi.NewCallback(callback.ID, ""))
					continue
				}

				purchases, err := db.GetPurchasesByTime(startTime, now)
				if err != nil {
					msg = tgbotapi.NewMessage(chatID, "Ошибка получения покупок.")
					bot.Send(msg)
					bot.Send(tgbotapi.NewCallback(callback.ID, ""))
					continue
				}

				// Подсчитываем суммы
				totalPayments, err := db.GetTotalPaymentsByTime(startTime, now)
				if err != nil {
					totalPayments = 0
				}
				totalPurchases, err := db.GetTotalPurchasesByTime(startTime, now)
				if err != nil {
					totalPurchases = 0
				}

				// Формируем ответ
				response := fmt.Sprintf("Статистика за %s:\n\n", map[string]string{
					"stats_day":   "день",
					"stats_week":  "неделю",
					"stats_month": "месяц",
					"stats_all":   "всё время",
				}[callbackData])

				response += "Платежи:\n"
				if len(payments) == 0 {
					response += "Нет платежей за этот период.\n"
				} else {
					for _, p := range payments {
						timeFormatted := time.Unix(p.Time, 0).Format("2006-01-02 15:04:05")
						response += fmt.Sprintf("ID: %d, Пользователь: %d, Сумма: %.2f %s, Статус: %s, Время: %s\n",
							p.ID, p.UserID, p.Value, p.CryptoType, p.Status, timeFormatted)
					}
				}

				response += "\nПокупки:\n"
				if len(purchases) == 0 {
					response += "Нет покупок за этот период.\n"
				} else {
					for _, p := range purchases {
						timeFormatted := time.Unix(p.Time, 0).Format("2006-01-02 15:04:05")
						response += fmt.Sprintf("ID: %d, Пользователь: %d, Название: %s, Тип: %s, Сумма: %.2f ₽, Статус: %s, Время: %s\n",
							p.ID, p.UserID, p.Name, p.Type, p.Value, p.Status, timeFormatted)
					}
				}

				response += fmt.Sprintf("\nИтого пополнений: %.2f\nИтого покупок: %.2f", totalPayments, totalPurchases)

				msg = tgbotapi.NewMessage(chatID, response)
				msg.ReplyMarkup = statsMenu()
				bot.Send(msg)
				bot.Send(tgbotapi.NewCallback(callback.ID, ""))
				continue

			case "back_to_main":
				state.currentMenu = "main"
				msg = tgbotapi.NewMessage(chatID, "Админ-панель. Выберите действие:")
				msg.ReplyMarkup = adminMenu()
				bot.Send(msg)
				bot.Send(tgbotapi.NewCallback(callback.ID, ""))
				continue
			}

			// Далее идет остальная логика callback (если есть)
		}

		// Обработка текстовых сообщений
		if update.Message == nil {
			continue
		}

		// Обработка ввода данных для редактирования настроек
		if state.awaitingCryptoPayToken {
			var msg tgbotapi.MessageConfig
			newToken := update.Message.Text
			if err := db.SetSetting("CRYPTO_PAY_TOKEN", newToken); err != nil {
				msg = tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка при обновлении CRYPTO_PAY_TOKEN: %v", err))
				bot.Send(msg)
			} else {
				msg = tgbotapi.NewMessage(chatID, "CRYPTO_PAY_TOKEN успешно обновлен!")
				bot.Send(msg)
			}
			state.awaitingCryptoPayToken = false
			state.currentMenu = "settings"
			msg = tgbotapi.NewMessage(chatID, "Настройки:")
			msg.ReplyMarkup = settingsMenu()
			bot.Send(msg)
			continue
		}

		if state.awaitingSupportAccount {
			var msg tgbotapi.MessageConfig
			newAccount := update.Message.Text
			if !strings.HasPrefix(newAccount, "@") {
				msg = tgbotapi.NewMessage(chatID, "Ошибка: аккаунт поддержки должен начинаться с @")
				bot.Send(msg)
				continue
			}
			if err := db.SetSetting("SUPPORT_ACCOUNT", newAccount); err != nil {
				msg = tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка при обновлении SUPPORT_ACCOUNT: %v", err))
				bot.Send(msg)
			} else {
				msg = tgbotapi.NewMessage(chatID, "Аккаунт поддержки успешно обновлен!")
				bot.Send(msg)
			}
			state.awaitingSupportAccount = false
			state.currentMenu = "settings"
			msg = tgbotapi.NewMessage(chatID, "Настройки:")
			msg.ReplyMarkup = settingsMenu()
			bot.Send(msg)
			continue
		}

		// Обработка добавления/редактирования товара или услуги
		if state.awaitingGoodInput || state.awaitingServiceInput || state.awaitingGoodEdit || state.awaitingServiceEdit {
			if !strings.Contains(update.Message.Text, ",") {
				msg := tgbotapi.NewMessage(chatID, "Формат должен быть: название,цена,описание\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				continue
			}

			parts := strings.Split(update.Message.Text, ",")
			if len(parts) != 3 {
				msg := tgbotapi.NewMessage(chatID, "Ошибка: должно быть ровно 3 значения (название,цена,описание).\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				continue
			}

			name := strings.TrimSpace(parts[0])
			value, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if err != nil {
				msg := tgbotapi.NewMessage(chatID, "Ошибка: цена должна быть числом.\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				continue
			}
			descr := strings.TrimSpace(parts[2])

			var msg tgbotapi.MessageConfig

			if state.awaitingGoodInput {
				if err := db.AddGood(models.Good{Name: name, Value: value, Descr: descr}); err != nil {
					log.Println("Failed to add good:", err)
					msg = tgbotapi.NewMessage(chatID, "Ошибка при добавлении товара.")
				} else {
					msg = tgbotapi.NewMessage(chatID, "Товар добавлен!")
				}
				state.awaitingGoodInput = false
				log.Printf("State reset: awaitingGoodInput = %v", state.awaitingGoodInput)
			} else if state.awaitingServiceInput {
				if err := db.AddService(models.Service{Name: name, Value: value, Descr: descr}); err != nil {
					log.Println("Failed to add service:", err)
					msg = tgbotapi.NewMessage(chatID, "Ошибка при добавлении услуги.")
				} else {
					msg = tgbotapi.NewMessage(chatID, "Услуга добавлена!")
				}
				state.awaitingServiceInput = false
				log.Printf("State reset: awaitingServiceInput = %v", state.awaitingServiceInput)
			} else if state.awaitingGoodEdit {
				if err := db.UpdateGood(state.goodIDToEdit, models.Good{ID: state.goodIDToEdit, Name: name, Value: value, Descr: descr}); err != nil {
					log.Println("Failed to edit good:", err)
					msg = tgbotapi.NewMessage(chatID, "Ошибка при редактировании товара.")
				} else {
					msg = tgbotapi.NewMessage(chatID, "Товар отредактирован!")
				}
				state.awaitingGoodEdit = false
				state.goodIDToEdit = 0
				log.Printf("State reset: awaitingGoodEdit = %v", state.awaitingGoodEdit)
			} else if state.awaitingServiceEdit {
				if err := db.UpdateService(state.serviceIDToEdit, models.Service{ID: state.serviceIDToEdit, Name: name, Value: value, Descr: descr}); err != nil {
					log.Println("Failed to edit service:", err)
					msg = tgbotapi.NewMessage(chatID, "Ошибка при редактировании услуги.")
				} else {
					msg = tgbotapi.NewMessage(chatID, "Услуга отредактирована!")
				}
				state.awaitingServiceEdit = false
				state.serviceIDToEdit = 0
				log.Printf("State reset: awaitingServiceEdit = %v", state.awaitingServiceEdit)
			}

			bot.Send(msg)
			state.currentMenu = "main"
			msg = tgbotapi.NewMessage(chatID, "Админ-панель. Выберите действие:")
			msg.ReplyMarkup = adminMenu()
			bot.Send(msg)
			continue
		}

		// Обработка удаления товара или услуги
		if state.awaitingGoodDelete || state.awaitingServiceDelete {
			id, err := strconv.Atoi(update.Message.Text)
			if err != nil {
				msg := tgbotapi.NewMessage(chatID, "Ошибка: введите корректный ID.\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				continue
			}

			var msg tgbotapi.MessageConfig
			if state.awaitingGoodDelete {
				if err := db.DeleteGood(id); err != nil {
					log.Println("Failed to delete good:", err)
					msg = tgbotapi.NewMessage(chatID, "Ошибка при удалении товара.")
				} else {
					msg = tgbotapi.NewMessage(chatID, "Товар удален!")
				}
				state.awaitingGoodDelete = false
				log.Printf("State reset: awaitingGoodDelete = %v", state.awaitingGoodDelete)
			} else if state.awaitingServiceDelete {
				if err := db.DeleteService(id); err != nil {
					log.Println("Failed to delete service:", err)
					msg = tgbotapi.NewMessage(chatID, "Ошибка при удалении услуги.")
				} else {
					msg = tgbotapi.NewMessage(chatID, "Услуга удалена!")
				}
				state.awaitingServiceDelete = false
				log.Printf("State reset: awaitingServiceDelete = %v", state.awaitingServiceDelete)
			}

			bot.Send(msg)
			state.currentMenu = "main"
			msg = tgbotapi.NewMessage(chatID, "Админ-панель. Выберите действие:")
			msg.ReplyMarkup = adminMenu()
			bot.Send(msg)
			continue
		}

		// Обработка проверки и изменения баланса
		if state.awaitingUserIDForBalanceCheck != 0 || state.awaitingUserIDForBalanceChange != 0 {
			id, err := strconv.ParseInt(update.Message.Text, 10, 64)
			if err != nil {
				msg := tgbotapi.NewMessage(chatID, "Ошибка: введите корректный ID пользователя.\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				continue
			}

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
			} else if state.awaitingUserIDForBalanceChange == userID {
				log.Printf("Preparing to change balance for user %d", id)
				msg := tgbotapi.NewMessage(chatID, "Введите новый баланс (например, 100.50):\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				state.awaitingBalanceChange = true
				state.awaitingUserIDForBalanceChange = id
				log.Printf("State updated: awaitingBalanceChange = %v, awaitingUserIDForBalanceChange = %d", state.awaitingBalanceChange, state.awaitingUserIDForBalanceChange)
			}
			continue
		}

		if state.awaitingBalanceChange && state.awaitingUserIDForBalanceChange != 0 {
			log.Printf("Processing balance change for user %d", state.awaitingUserIDForBalanceChange)
			newBalance, err := strconv.ParseFloat(update.Message.Text, 64)
			if err != nil {
				msg := tgbotapi.NewMessage(chatID, "Ошибка: введите корректное число (например, 100.50).\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				continue
			}

			var msg tgbotapi.MessageConfig
			if err := db.UpdateUserBalance(state.awaitingUserIDForBalanceChange, newBalance); err != nil {
				log.Println("Failed to update user balance:", err)
				msg = tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка: %v", err))
			} else {
				msg = tgbotapi.NewMessage(chatID, fmt.Sprintf("Баланс пользователя %d обновлен: %.2f ₽", state.awaitingUserIDForBalanceChange, newBalance))
			}
			bot.Send(msg)

			state.awaitingBalanceChange = false
			state.awaitingUserIDForBalanceChange = 0
			log.Printf("State reset: awaitingBalanceChange = %v, awaitingUserIDForBalanceChange = %d", state.awaitingBalanceChange, state.awaitingUserIDForBalanceChange)
			state.currentMenu = "main"
			msg = tgbotapi.NewMessage(chatID, "Админ-панель. Выберите действие:")
			msg.ReplyMarkup = adminMenu()
			bot.Send(msg)
			continue
		}

		// Обработка кнопок меню
		switch messageText {
		case "/start":
			state.currentMenu = "main"
			msg := tgbotapi.NewMessage(chatID, "Админ-панель. Выберите действие:")
			msg.ReplyMarkup = adminMenu()
			bot.Send(msg)

		case "📊 Статистика":
			if state.currentMenu == "main" {
				state.currentMenu = "stats"
				msg := tgbotapi.NewMessage(chatID, "Выберите период для статистики:")
				msg.ReplyMarkup = statsMenu()
				bot.Send(msg)
			}

		case "👥 Пользователи":
			if state.currentMenu == "main" {
				state.currentMenu = "users"
				msg := tgbotapi.NewMessage(chatID, "Выберите действие:")
				msg.ReplyMarkup = usersMenu()
				bot.Send(msg)
			}

		case "Посмотреть баланс пользователя":
			if state.currentMenu == "users" {
				msg := tgbotapi.NewMessage(chatID, "Введите ID пользователя для проверки баланса:\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				state.awaitingUserIDForBalanceCheck = userID
				log.Printf("State updated: awaitingUserIDForBalanceCheck = %d", state.awaitingUserIDForBalanceCheck)
			}

		case "Изменить баланс пользователя":
			if state.currentMenu == "users" {
				msg := tgbotapi.NewMessage(chatID, "Введите ID пользователя для изменения баланса:\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				state.awaitingUserIDForBalanceChange = userID
				log.Printf("State updated: awaitingUserIDForBalanceChange = %d", state.awaitingUserIDForBalanceChange)
			}

		case "🛒 Каталог":
			if state.currentMenu == "main" {
				state.currentMenu = "catalog"
				msg := tgbotapi.NewMessage(chatID, "Выберите категорию:")
				msg.ReplyMarkup = catalogMenu()
				bot.Send(msg)
			}

		case "Товары":
			if state.currentMenu == "catalog" {
				state.currentMenu = "goods"
				msg := tgbotapi.NewMessage(chatID, "Выберите действие с товарами:")
				msg.ReplyMarkup = goodsMenu()
				bot.Send(msg)
			}

		case "Услуги":
			if state.currentMenu == "catalog" {
				state.currentMenu = "services"
				msg := tgbotapi.NewMessage(chatID, "Выберите действие с услугами:")
				msg.ReplyMarkup = servicesMenu()
				bot.Send(msg)
			}

		case "Добавить товар":
			if state.currentMenu == "goods" {
				msg := tgbotapi.NewMessage(chatID, "Введите данные товара в формате: название,цена,описание\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				state.awaitingGoodInput = true
				log.Printf("State updated: awaitingGoodInput = %v", state.awaitingGoodInput)
			}

		case "Добавить услугу":
			if state.currentMenu == "services" {
				msg := tgbotapi.NewMessage(chatID, "Введите данные услуги в формате: название,цена,описание\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				state.awaitingServiceInput = true
				log.Printf("State updated: awaitingServiceInput = %v", state.awaitingServiceInput)
			}

		case "Убрать товар":
			if state.currentMenu == "goods" {
				msg := tgbotapi.NewMessage(chatID, "Введите ID товара для удаления:\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				state.awaitingGoodDelete = true
				log.Printf("State updated: awaitingGoodDelete = %v", state.awaitingGoodDelete)
			}

		case "Убрать услугу":
			if state.currentMenu == "services" {
				msg := tgbotapi.NewMessage(chatID, "Введите ID услуги для удаления:\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				state.awaitingServiceDelete = true
				log.Printf("State updated: awaitingServiceDelete = %v", state.awaitingServiceDelete)
			}

		case "Редактировать товар":
			if state.currentMenu == "goods" {
				msg := tgbotapi.NewMessage(chatID, "Введите ID товара для редактирования:\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				go func() {
					updates := bot.GetUpdatesChan(u)
					for update := range updates {
						if update.Message == nil {
							continue
						}
						if strings.TrimSpace(update.Message.Text) == "Отмена" {
							msg := tgbotapi.NewMessage(chatID, "Действие отменено. Возвращаемся в главное меню:")
							msg.ReplyMarkup = adminMenu()
							bot.Send(msg)
							state.currentMenu = "main"
							state.awaitingGoodEdit = false
							break
						}
						if id, err := strconv.Atoi(update.Message.Text); err == nil {
							state.goodIDToEdit = id
							state.awaitingGoodEdit = true
							msg := tgbotapi.NewMessage(chatID, "Введите новые данные товара в формате: название,цена,описание\n\nНажмите 'Отмена', чтобы вернуться в меню.")
							bot.Send(msg)
							break
						} else {
							msg := tgbotapi.NewMessage(chatID, "Ошибка: введите корректный ID.\n\nНажмите 'Отмена', чтобы вернуться в меню.")
							bot.Send(msg)
						}
					}
				}()
			}

		case "Редактировать услугу":
			if state.currentMenu == "services" {
				msg := tgbotapi.NewMessage(chatID, "Введите ID услуги для редактирования:\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				go func() {
					updates := bot.GetUpdatesChan(u)
					for update := range updates {
						if update.Message == nil {
							continue
						}
						if strings.TrimSpace(update.Message.Text) == "Отмена" {
							msg := tgbotapi.NewMessage(chatID, "Действие отменено. Возвращаемся в главное меню:")
							msg.ReplyMarkup = adminMenu()
							bot.Send(msg)
							state.currentMenu = "main"
							state.awaitingServiceEdit = false
							break
						}
						if id, err := strconv.Atoi(update.Message.Text); err == nil {
							state.serviceIDToEdit = id
							state.awaitingServiceEdit = true
							msg := tgbotapi.NewMessage(chatID, "Введите новые данные услуги в формате: название,цена,описание\n\nНажмите 'Отмена', чтобы вернуться в меню.")
							bot.Send(msg)
							break
						} else {
							msg := tgbotapi.NewMessage(chatID, "Ошибка: введите корректный ID.\n\nНажмите 'Отмена', чтобы вернуться в меню.")
							bot.Send(msg)
						}
					}
				}()
			}

		case "Просмотр всех товаров":
			if state.currentMenu == "goods" {
				goods, err := db.GetGoods()
				if err != nil {
					msg := tgbotapi.NewMessage(chatID, "Ошибка получения товаров.")
					bot.Send(msg)
					continue
				}
				if len(goods) == 0 {
					msg := tgbotapi.NewMessage(chatID, "Товаров пока нет.")
					bot.Send(msg)
					continue
				}
				response := "Список товаров:\n"
				for _, good := range goods {
					response += fmt.Sprintf("ID: %d, %s - %.2f ₽, Описание: %s\n", good.ID, good.Name, good.Value, good.Descr)
				}
				msg := tgbotapi.NewMessage(chatID, response)
				bot.Send(msg)
			}

		case "Просмотр всех услуг":
			if state.currentMenu == "services" {
				services, err := db.GetServices()
				if err != nil {
					msg := tgbotapi.NewMessage(chatID, "Ошибка получения услуг.")
					bot.Send(msg)
					continue
				}
				if len(services) == 0 {
					msg := tgbotapi.NewMessage(chatID, "Услуг пока нет.")
					bot.Send(msg)
					continue
				}
				response := "Список услуг:\n"
				for _, service := range services {
					response += fmt.Sprintf("ID: %d, %s - %.2f ₽, Описание: %s\n", service.ID, service.Name, service.Value, service.Descr)
				}
				msg := tgbotapi.NewMessage(chatID, response)
				bot.Send(msg)
			}

		case "⚙️ Настройки":
			if state.currentMenu == "main" {
				state.currentMenu = "settings"
				msg := tgbotapi.NewMessage(chatID, "Настройки:")
				msg.ReplyMarkup = settingsMenu()
				bot.Send(msg)
			}

		case "Изменить API_CryptoBot":
			if state.currentMenu == "settings" {
				msg := tgbotapi.NewMessage(chatID, "Введите новый API-токен для CryptoBot:\n\nНажмите 'Отмена', чтобы вернуться в меню.")
				bot.Send(msg)
				state.awaitingCryptoPayToken = true
			}

		case "Изменить аккаунт Поддержки":
			if state.currentMenu == "settings" {
				supportAccount, err := db.GetSetting("SUPPORT_ACCOUNT")
				if err != nil {
					supportAccount = "@SupportBot"
				}
				msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Текущий аккаунт поддержки: %s\nВведите новый аккаунт (например, @SupportBot):\n\nНажмите 'Отмена', чтобы вернуться в меню.", supportAccount))
				bot.Send(msg)
				state.awaitingSupportAccount = true
			}

		case "Назад":
			switch state.currentMenu {
			case "catalog":
				state.currentMenu = "main"
				msg := tgbotapi.NewMessage(chatID, "Админ-панель. Выберите действие:")
				msg.ReplyMarkup = adminMenu()
				bot.Send(msg)
			case "goods", "services":
				state.currentMenu = "catalog"
				msg := tgbotapi.NewMessage(chatID, "Выберите категорию:")
				msg.ReplyMarkup = catalogMenu()
				bot.Send(msg)
			case "settings", "users", "stats":
				state.currentMenu = "main"
				msg := tgbotapi.NewMessage(chatID, "Админ-панель. Выберите действие:")
				msg.ReplyMarkup = adminMenu()
				bot.Send(msg)
			}

		default:
			msg := tgbotapi.NewMessage(chatID, "Пожалуйста, используйте кнопки для навигации.")
			bot.Send(msg)
		}
	}
}

// adminMenu возвращает главное меню админ-панели
func adminMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Статистика"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("👥 Пользователи"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⚙️ Настройки"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🛒 Каталог"),
		),
	)
}

// catalogMenu возвращает меню каталога
func catalogMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Товары"),
			tgbotapi.NewKeyboardButton("Услуги"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Назад"),
		),
	)
}

// goodsMenu возвращает меню действий с товарами
func goodsMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Добавить товар"),
			tgbotapi.NewKeyboardButton("Убрать товар"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Редактировать товар"),
			tgbotapi.NewKeyboardButton("Просмотр всех товаров"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Назад"),
		),
	)
}

// servicesMenu возвращает меню действий с услугами
func servicesMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Добавить услугу"),
			tgbotapi.NewKeyboardButton("Убрать услугу"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Редактировать услугу"),
			tgbotapi.NewKeyboardButton("Просмотр всех услуг"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Назад"),
		),
	)
}

// usersMenu возвращает меню действий с пользователями
func usersMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Посмотреть баланс пользователя"),
			tgbotapi.NewKeyboardButton("Изменить баланс пользователя"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Назад"),
		),
	)
}

// settingsMenu возвращает меню настроек
func settingsMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Изменить API_CryptoBot"),
			tgbotapi.NewKeyboardButton("Изменить аккаунт Поддержки"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Назад"),
		),
	)
}

// statsMenu возвращает меню выбора периода статистики
func statsMenu() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("День", "stats_day"),
			tgbotapi.NewInlineKeyboardButtonData("Неделя", "stats_week"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Месяц", "stats_month"),
			tgbotapi.NewInlineKeyboardButtonData("Все время", "stats_all"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Назад", "back_to_main"),
		),
	)
}
