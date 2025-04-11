package handlers

import (
	"fmt"
	"log"
	"shop-bot/config"
	"shop-bot/cryptopay"
	"shop-bot/db"
	"shop-bot/models"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Поддерживаемые криптовалюты
var supportedAssets = []string{"TON", "BTC", "ETH", "USDT", "USDC"}

// Переменная для хранения состояния пополнения баланса
type topUpState struct {
	userID      int64
	asset       string
	amountStep  bool
	waitingList map[int64]bool // Map для отслеживания пользователей, ожидающих обновления статуса платежа
}

var topUpStates = make(map[int64]*topUpState)
var cryptoClient *cryptopay.CryptoPayClient

func HandleClientBot(bot *tgbotapi.BotAPI) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	// Инициализация клиента Crypto Pay
	cfg := config.LoadConfig()
	cryptoClient = cryptopay.NewCryptoPayClient(cfg.CryptoPayToken)

	// Запуск горутины для проверки статуса платежей
	go checkPaymentStatus(bot)

	// Переменная для хранения состояния
	type clientState struct {
		lastMessageID int // ID последнего отправленного сообщения
	}
	state := clientState{}

	for update := range updates {
		if update.Message != nil {
			userID := update.Message.From.ID
			chatID := update.Message.Chat.ID

			// Обработка текстовых сообщений
			// Проверяем, ожидаем ли от пользователя ввод суммы для пополнения
			if state, exists := topUpStates[userID]; exists && state.amountStep {
				amount, err := strconv.ParseFloat(update.Message.Text, 64)
				if err != nil || amount <= 0 {
					msg := tgbotapi.NewMessage(chatID, "Пожалуйста, введите корректную сумму (например, 100.50).")
					bot.Send(msg)
					continue
				}

				// Создаем инвойс в Crypto Pay
				invoice, err := createCryptoInvoice(userID, amount, state.asset)
				if err != nil {
					log.Printf("Failed to create invoice: %v", err)
					msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка создания счета: %v", err))
					bot.Send(msg)
					continue
				}

				// Сохраняем информацию о платеже в базе данных
				if err := db.CreatePayment(userID, invoice.InvoiceID, amount, state.asset, "active", "top_up", invoice.CreatedAt.Unix()); err != nil {
					log.Printf("Failed to save payment: %v", err)
				}
				// Отправляем пользователю ссылку на оплату
				responseText := fmt.Sprintf(
					"Создан счет на %.2f %s\n\n"+
						"Счет действителен 30 минут. Оплатите его, перейдя по ссылке ниже.",
					invoice.Amount, invoice.Asset)

				msg := tgbotapi.NewMessage(chatID, responseText)
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonURL("Оплатить", invoice.PayUrl),
					),
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("Проверить платеж", fmt.Sprintf("check_payment_%d", invoice.InvoiceID)),
					),
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
					),
				)
				bot.Send(msg)

				// Добавляем пользователя в список ожидающих проверки
				state.waitingList[userID] = true
				state.amountStep = false

				continue
			}

			nameTag := update.Message.From.UserName

			// Добавляем пользователя в базу
			if err := db.AddUser(models.User{ID: userID, NameTag: nameTag}); err != nil {
				log.Println("Failed to add user:", err)
			}

			switch update.Message.Text {
			case "/start":
				// Удаляем предыдущее сообщение, если оно есть
				if state.lastMessageID != 0 {
					deleteMsg := tgbotapi.NewDeleteMessage(chatID, state.lastMessageID)
					if _, err := bot.Send(deleteMsg); err != nil {
						log.Println("Failed to delete message:", err)
					}
				}

				// Отправляем сообщение с inline-кнопками
				msg := tgbotapi.NewMessage(chatID, "Добро пожаловать в магазин! Выберите опцию:")
				msg.ReplyMarkup = clientMenu()
				sentMsg, err := bot.Send(msg)
				if err != nil {
					log.Println("Failed to send message:", err)
					continue
				}
				// Сохраняем ID отправленного сообщения
				state.lastMessageID = sentMsg.MessageID

			default:
				// Если пользователь отправил что-то, что не является командой
				msg := tgbotapi.NewMessage(chatID, "Пожалуйста, используйте кнопки для взаимодействия.")
				sentMsg, err := bot.Send(msg)
				if err != nil {
					log.Println("Failed to send message:", err)
					continue
				}
				// Сохраняем ID нового сообщения
				state.lastMessageID = sentMsg.MessageID
			}
		}

		// Обработка нажатий на inline-кнопки
		if update.CallbackQuery != nil {
			callback := update.CallbackQuery
			chatID := callback.Message.Chat.ID
			userID := callback.From.ID
			var response string
			var msg tgbotapi.MessageConfig

			// Удаляем предыдущее сообщение (например, меню или список товаров/услуг)
			if callback.Message.MessageID != 0 {
				deleteMsg := tgbotapi.NewDeleteMessage(chatID, callback.Message.MessageID)
				if _, err := bot.Send(deleteMsg); err != nil {
					log.Println("Failed to delete message:", err)
				}
				// Обновляем state.lastMessageID, так как сообщение удалено
				if state.lastMessageID == callback.Message.MessageID {
					state.lastMessageID = 0
				}
			}

			switch callback.Data {
			// Проверяем, связан ли callback с платежом
			case "check_payment_":
				if strings.HasPrefix(callback.Data, "check_payment_") {
					invoiceIDStr := strings.TrimPrefix(callback.Data, "check_payment_")
					invoiceID, err := strconv.ParseInt(invoiceIDStr, 10, 64)
					if err != nil {
						response = "Ошибка: неверный ID платежа."
						msg = tgbotapi.NewMessage(chatID, response)
					} else {
						// Проверяем статус платежа
						status, err := checkCryptoInvoiceStatus(invoiceID, userID)
						if err != nil {
							response = fmt.Sprintf("Ошибка при проверке платежа: %v", err)
						} else if status == "paid" {
							response = "Платеж успешно завершен! Ваш баланс обновлен."
						} else {
							response = "Платеж еще не получен. Пожалуйста, завершите оплату и повторите проверку."
						}
						msg = tgbotapi.NewMessage(chatID, response)
						msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
							tgbotapi.NewInlineKeyboardRow(
								tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
							),
						)
					}
					sentMsg, err := bot.Send(msg)
					if err != nil {
						log.Println("Failed to send message:", err)
					}
					state.lastMessageID = sentMsg.MessageID

					// Подтверждаем обработку callback
					callbackConfig := tgbotapi.NewCallback(callback.ID, "")
					bot.Send(callbackConfig)
					continue
				}

			// Проверяем, связан ли callback с выбором криптовалюты
			case "asset_":
				if strings.HasPrefix(callback.Data, "asset_") {
					asset := strings.TrimPrefix(callback.Data, "asset_")

					// Проверяем, поддерживается ли эта криптовалюта
					assetSupported := false
					for _, supportedAsset := range supportedAssets {
						if asset == supportedAsset {
							assetSupported = true
							break
						}
					}

					if !assetSupported {
						response = "Выбранная криптовалюта не поддерживается."
						msg = tgbotapi.NewMessage(chatID, response)
					} else {
						// Сохраняем выбранную криптовалюту и переходим к следующему шагу
						if _, exists := topUpStates[userID]; !exists {
							topUpStates[userID] = &topUpState{
								userID:      userID,
								waitingList: make(map[int64]bool),
							}
						}
						topUpStates[userID].asset = asset
						topUpStates[userID].amountStep = true

						response = fmt.Sprintf("Вы выбрали %s для пополнения баланса. Введите сумму для пополнения:", asset)
						msg = tgbotapi.NewMessage(chatID, response)
					}

					sentMsg, err := bot.Send(msg)
					if err != nil {
						log.Println("Failed to send message:", err)
					}
					state.lastMessageID = sentMsg.MessageID

					// Подтверждаем обработку callback
					callbackConfig := tgbotapi.NewCallback(callback.ID, "")
					bot.Send(callbackConfig)
					continue
				}

			case "back_to_menu":
				// Сбрасываем состояние пользователя при возврате в меню
				if _, exists := topUpStates[userID]; exists {
					delete(topUpStates, userID)
				}

				// Возвращаемся к главному меню
				response = "Добро пожаловать в магазин! Выберите опцию:"
				msg = tgbotapi.NewMessage(chatID, response)
				msg.ReplyMarkup = clientMenu()

			case "catalog":
				// Показываем inline-кнопки для выбора: Товары или Услуги
				response = "Выберите категорию:"
				msg = tgbotapi.NewMessage(chatID, response)
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("Товары", "show_goods"),
						tgbotapi.NewInlineKeyboardButtonData("Услуги", "show_services"),
					),
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
					),
				)

			case "profile":
				// Получаем баланс пользователя
				balance, err := db.GetUserBalance(userID)
				if err != nil {
					log.Println("Failed to get user balance:", err)
					balance = 0.0
				}

				// Получаем историю покупок
				history, err := db.GetPurchaseHistory(userID)
				if err != nil {
					log.Println("Failed to get purchase history:", err)
					history = []int{}
				}

				// Форматируем профиль
				response = fmt.Sprintf(
					"*Ваш баланс:* %.2f ₽ 💰\n\n"+
						"🆔 ID: %d\n"+
						"🛍️ Количество покупок: %d\n\n"+
						"━━━━━━━━━━━━",
					balance,
					userID,
					len(history),
				)
				msg = tgbotapi.NewMessage(chatID, response)
				msg.ParseMode = "Markdown"
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("💸 Пополнить баланс", "top_up_balance"),
					),
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("🤑🤑 Реферальная система", "referral_system"),
					),
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("🛒 История покупок", "purchase_history"),
					),
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("📜 Активировать купон", "activate_coupon"),
					),
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
					),
				)

			case "info":
				response = "Напишите @SupportBot для помощи."
				msg = tgbotapi.NewMessage(chatID, response)
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
					),
				)

			case "show_goods":
				goods, err := db.GetGoods()
				if err != nil {
					log.Println("Failed to get goods:", err)
					response = "Ошибка при получении товаров."
				} else if len(goods) == 0 {
					response = "Товаров пока нет."
				} else {
					response = "Доступные товары:"
					var buttons [][]tgbotapi.InlineKeyboardButton
					for _, good := range goods {
						buttonText := fmt.Sprintf("%s (%.2f ₽)", good.Name, good.Value)
						buttonData := fmt.Sprintf("good_%d", good.ID)
						buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData(buttonText, buttonData),
						))
					}
					// Добавляем кнопку "Назад"
					buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
					))
					msg = tgbotapi.NewMessage(chatID, response)
					msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)
					sentMsg, err := bot.Send(msg)
					if err != nil {
						log.Println("Failed to send message:", err)
						continue
					}
					// Сохраняем ID нового сообщения
					state.lastMessageID = sentMsg.MessageID
					// Подтверждаем обработку callback
					callbackConfig := tgbotapi.NewCallback(callback.ID, "")
					bot.Send(callbackConfig)
					continue
				}

			case "show_services":
				services, err := db.GetServices()
				if err != nil {
					log.Println("Failed to get services:", err)
					response = "Ошибка при получении услуг."
				} else if len(services) == 0 {
					response = "Услуг пока нет."
				} else {
					response = "Доступные услуги:"
					var buttons [][]tgbotapi.InlineKeyboardButton
					for _, service := range services {
						buttonText := fmt.Sprintf("%s (%.2f ₽)", service.Name, service.Value)
						buttonData := fmt.Sprintf("service_%d", service.ID)
						buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData(buttonText, buttonData),
						))
					}
					// Добавляем кнопку "Назад"
					buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
					))
					msg = tgbotapi.NewMessage(chatID, response)
					msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)
					sentMsg, err := bot.Send(msg)
					if err != nil {
						log.Println("Failed to send message:", err)
						continue
					}
					// Сохраняем ID нового сообщения
					state.lastMessageID = sentMsg.MessageID
					// Подтверждаем обработку callback
					callbackConfig := tgbotapi.NewCallback(callback.ID, "")
					bot.Send(callbackConfig)
					continue
				}

			case "top_up_balance":
				response = "Выберите криптовалюту для пополнения баланса:"

				// Создаем кнопки для выбора криптовалюты
				var rows [][]tgbotapi.InlineKeyboardButton
				for _, asset := range supportedAssets {
					rows = append(rows, tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData(asset, "asset_"+asset),
					))
				}

				// Добавляем кнопку "Назад"
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
				))

				msg = tgbotapi.NewMessage(chatID, response)
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)

			case "referral_system":
				response = "Реферальная система: пригласите друга и получите бонус! 🤑"
				msg = tgbotapi.NewMessage(chatID, response)
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
					),
				)

			case "purchase_history":
				// Получаем историю покупок
				history, err := db.GetPurchaseHistory(userID)
				if err != nil {
					log.Println("Failed to get purchase history:", err)
					response = "Ошибка при получении истории покупок. 🛒"
				} else if len(history) == 0 {
					response = "История покупок: у вас пока нет покупок. 🛒"
				} else {
					response = "История покупок:\n"
					goods, _ := db.GetGoods()
					for _, goodID := range history {
						for _, good := range goods {
							if good.ID == goodID {
								response += fmt.Sprintf("ID: %d, %s - %.2f\n", good.ID, good.Name, good.Value)
							}
						}
					}
				}
				msg = tgbotapi.NewMessage(chatID, response)
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
					),
				)

			case "activate_coupon":
				response = "Введите код купона для активации! 📜"
				msg = tgbotapi.NewMessage(chatID, response)
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
					),
				)
			default:
				// Обработка выбора криптовалюты
				if strings.HasPrefix(callback.Data, "asset_") {
					asset := strings.TrimPrefix(callback.Data, "asset_")

					// Проверяем, поддерживается ли эта криптовалюта
					assetSupported := false
					for _, supportedAsset := range supportedAssets {
						if asset == supportedAsset {
							assetSupported = true
							break
						}
					}

					if !assetSupported {
						response = "Выбранная криптовалюта не поддерживается."
						msg = tgbotapi.NewMessage(chatID, response)
					} else {
						// Сохраняем выбранную криптовалюту и переходим к следующему шагу
						if _, exists := topUpStates[userID]; !exists {
							topUpStates[userID] = &topUpState{
								userID:      userID,
								waitingList: make(map[int64]bool),
							}
						}
						topUpStates[userID].asset = asset
						topUpStates[userID].amountStep = true

						response = fmt.Sprintf("Вы выбрали %s для пополнения баланса. Введите сумму для пополнения:", asset)
						msg = tgbotapi.NewMessage(chatID, response)
					}

					sentMsg, err := bot.Send(msg)
					if err != nil {
						log.Println("Failed to send message:", err)
					}
					state.lastMessageID = sentMsg.MessageID

					// Подтверждаем обработку callback
					callbackConfig := tgbotapi.NewCallback(callback.ID, "")
					bot.Send(callbackConfig)
					continue
				}

				// Обработка нажатия на кнопку товара
				if strings.HasPrefix(callback.Data, "good_") {
					goodIDStr := strings.TrimPrefix(callback.Data, "good_")
					goodID, err := strconv.Atoi(goodIDStr)
					if err != nil {
						response = "Ошибка: неверный ID товара."
					} else {
						goods, err := db.GetGoods()
						if err != nil {
							response = "Ошибка при получении информации о товаре."
						} else {
							for _, good := range goods {
								if good.ID == goodID {
									response = fmt.Sprintf(
										"Товар: %s\nЦена: %.2f ₽\nОписание: %s",
										good.Name, good.Value, good.Descr,
									)
									break
								}
							}
							if response == "" {
								response = "Товар не найден."
							}
						}
					}
					msg = tgbotapi.NewMessage(chatID, response)
					msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
						),
					)
				} else if strings.HasPrefix(callback.Data, "service_") {
					// Обработка нажатия на кнопку услуги
					serviceIDStr := strings.TrimPrefix(callback.Data, "service_")
					serviceID, err := strconv.Atoi(serviceIDStr)
					if err != nil {
						response = "Ошибка: неверный ID услуги."
					} else {
						services, err := db.GetServices()
						if err != nil {
							response = "Ошибка при получении информации об услуге."
						} else {
							for _, service := range services {
								if service.ID == serviceID {
									response = fmt.Sprintf(
										"Услуга: %s\nЦена: %.2f ₽\nОписание: %s",
										service.Name, service.Value, service.Descr,
									)
									break
								}
							}
							if response == "" {
								response = "Услуга не найдена."
							}
						}
					}
					msg = tgbotapi.NewMessage(chatID, response)
					msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
						),
					)
				} else {
					response = "Неизвестное действие."
					msg = tgbotapi.NewMessage(chatID, response)
					msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
						),
					)
				}
			}

			// Отправляем ответ на нажатие кнопки
			sentMsg, err := bot.Send(msg)
			if err != nil {
				log.Println("Failed to send message:", err)
				continue
			}
			// Сохраняем ID нового сообщения
			state.lastMessageID = sentMsg.MessageID

			// Подтверждаем обработку callback
			callbackConfig := tgbotapi.NewCallback(callback.ID, "")
			bot.Send(callbackConfig)
		}
	}
}

func clientMenu() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🛒 Каталог", "catalog"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👤 Профиль", "profile"),
			tgbotapi.NewInlineKeyboardButtonData("ℹ Информация", "info"),
		),
	)
}

// Функция для создания инвойса в Crypto Pay
func createCryptoInvoice(userID int64, amount float64, asset string) (*cryptopay.Invoice, error) {
	params := cryptopay.CreateInvoiceParams{
		Asset:         asset,
		Amount:        amount,
		Description:   fmt.Sprintf("Пополнение баланса пользователя %d", userID),
		Payload:       fmt.Sprintf("user_id:%d", userID),
		AllowComments: true,
		ExpiresIn:     1800,
		PaidBtnName:   "openBot", // Используем допустимое значение
		PaidBtnUrl:    "https://t.me/your_bot_name",
	}

	invoice, err := cryptoClient.CreateInvoice(params)
	if err != nil {
		return nil, err
	}

	return invoice, nil
}

// Функция для проверки статуса инвойса
func checkCryptoInvoiceStatus(invoiceID int64, userID int64) (string, error) {
	invoice, err := cryptoClient.GetInvoice(invoiceID)
	if err != nil {
		return "", err
	}

	// Обновляем статус платежа в базе данных
	if invoice.Status != "active" {
		db.UpdatePaymentStatus(invoiceID, invoice.Status)
	}

	// Если платеж оплачен, но баланс еще не обновлен
	if invoice.Status == "paid" {
		payment, err := db.GetPaymentByInvoiceID(invoiceID)
		if err == nil && payment.Status != "paid" {
			// Получаем текущий баланс
			currentBalance, err := db.GetUserBalance(userID)
			if err == nil {
				// Обновляем баланс пользователя
				newBalance := currentBalance + payment.Amount
				db.UpdateUserBalance(userID, newBalance)

				// Обновляем статус платежа
				db.UpdatePaymentStatus(invoiceID, "paid")
			}
		}
	}

	return invoice.Status, nil
}

// Горутина для периодической проверки статуса платежей
func checkPaymentStatus(bot *tgbotapi.BotAPI) {
	for {
		time.Sleep(30 * time.Second)

		// Проверяем все активные платежи
		for userID, state := range topUpStates {
			if len(state.waitingList) == 0 {
				continue
			}

			// Получаем платежи пользователя
			payments, err := db.GetUserPayments(userID)
			if err != nil {
				log.Printf("Failed to get payments for user %d: %v", userID, err)
				continue
			}

			for _, payment := range payments {
				// Проверяем только активные платежи
				if payment.Status != "active" {
					continue
				}

				// Проверяем статус платежа
				status, err := checkCryptoInvoiceStatus(payment.InvoiceID, userID)
				if err != nil {
					log.Printf("Failed to check payment status: %v", err)
					continue
				}

				// Если платеж был оплачен, отправляем уведомление пользователю
				if status == "paid" {
					msg := tgbotapi.NewMessage(userID, fmt.Sprintf(
						"✅ Ваш платеж на сумму %.2f %s успешно обработан! Баланс обновлен.",
						payment.Amount, payment.Asset))

					_, err := bot.Send(msg)
					if err != nil {
						log.Printf("Failed to send notification: %v", err)
					}

					// Удаляем пользователя из списка ожидающих
					delete(state.waitingList, userID)
				}
			}
		}
	}
}
