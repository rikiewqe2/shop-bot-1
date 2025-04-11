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

type clientState struct {
	userID        int64
	asset         string
	amountStep    bool
	lastMessageID int
	waitingList   map[int64]bool
}

var clientStates = make(map[int64]*clientState)
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

	for update := range updates {
		if update.Message != nil {
			userID := update.Message.From.ID
			chatID := update.Message.Chat.ID

			// Инициализируем состояние пользователя, если его нет
			if _, exists := clientStates[userID]; !exists {
				clientStates[userID] = &clientState{
					userID:      userID,
					waitingList: make(map[int64]bool),
				}
			}
			state := clientStates[userID]

			// Обработка ввода суммы для пополнения
			if state.amountStep {
				amount, err := strconv.ParseFloat(update.Message.Text, 64)
				if err != nil || amount <= 0 {
					msg := tgbotapi.NewMessage(chatID, "Пожалуйста, введите корректную сумму (например, 100.50).")
					bot.Send(msg)
					continue
				}

				// Создаем инвойс
				invoice, paymentID, invoiceID, err := createCryptoInvoice(userID, amount, state.asset)
				if err != nil {
					log.Printf("Failed to create invoice: %v", err)
					msg := tgbotapi.NewMessage(chatID, "Ошибка создания счета.")
					bot.Send(msg)
					continue
				}

				// Отправляем ссылку на оплату
				responseText := fmt.Sprintf(
					"Создан счет на %.2f %s\n\nСчет действителен 30 минут.",
					amount, state.asset)
				msg := tgbotapi.NewMessage(chatID, responseText)
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonURL("Оплатить", invoice.PayUrl),
					),
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("Проверить платеж", fmt.Sprintf("check_payment_%d_%d", invoiceID, paymentID)),
					),
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
					),
				)
				sentMsg, err := bot.Send(msg)
				if err != nil {
					log.Println("Failed to send message:", err)
					continue
				}
				state.lastMessageID = sentMsg.MessageID
				state.waitingList[userID] = true
				state.amountStep = false
				continue
			}

			// Добавляем пользователя в базу
			if err := db.AddUser(models.User{ID: userID}); err != nil {
				log.Printf("Failed to add user %d: %v", userID, err)
			}

			switch update.Message.Text {
			case "/start":
				if state.lastMessageID != 0 {
					deleteMsg := tgbotapi.NewDeleteMessage(chatID, state.lastMessageID)
					bot.Send(deleteMsg)
				}

				msg := tgbotapi.NewMessage(chatID, "Добро пожаловать в магазин! Выберите опцию:")
				msg.ReplyMarkup = clientMenu()
				sentMsg, err := bot.Send(msg)
				if err != nil {
					log.Println("Failed to send message:", err)
					continue
				}
				state.lastMessageID = sentMsg.MessageID

			default:
				msg := tgbotapi.NewMessage(chatID, "Пожалуйста, используйте кнопки.")
				sentMsg, err := bot.Send(msg)
				if err != nil {
					log.Println("Failed to send message:", err)
					continue
				}
				state.lastMessageID = sentMsg.MessageID
			}
		}

		// Обработка callback-запросов
		if update.CallbackQuery != nil {
			callback := update.CallbackQuery
			chatID := callback.Message.Chat.ID
			userID := callback.From.ID

			// Инициализируем состояние, если его нет
			if _, exists := clientStates[userID]; !exists {
				clientStates[userID] = &clientState{
					userID:      userID,
					waitingList: make(map[int64]bool),
				}
			}
			state := clientStates[userID]

			// Удаляем предыдущее сообщение
			if callback.Message.MessageID != 0 {
				deleteMsg := tgbotapi.NewDeleteMessage(chatID, callback.Message.MessageID)
				bot.Send(deleteMsg)
				if state.lastMessageID == callback.Message.MessageID {
					state.lastMessageID = 0
				}
			}

			var response string
			var msg tgbotapi.MessageConfig

			// Проверка статуса платежа
			if strings.HasPrefix(callback.Data, "check_payment_") {
				parts := strings.Split(callback.Data, "_")
				if len(parts) != 4 {
					response = "Ошибка: неверный формат данных."
				} else {
					invoiceID, err1 := strconv.ParseInt(parts[2], 10, 64)
					paymentID, err2 := strconv.ParseInt(parts[3], 10, 64)
					if err1 != nil || err2 != nil {
						response = "Ошибка: неверный ID платежа."
					} else {
						status, err := checkCryptoInvoiceStatus(invoiceID, paymentID, userID)
						if err != nil {
							response = fmt.Sprintf("Ошибка проверки платежа: %v", err)
						} else if status == "Ready" {
							response = "Платеж завершен! Баланс обновлен."
							delete(state.waitingList, userID)
						} else {
							response = "Платеж еще не получен."
						}
					}
				}
				msg = tgbotapi.NewMessage(chatID, response)
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
					),
				)
			} else if strings.HasPrefix(callback.Data, "asset_") {
				// Выбор криптовалюты
				asset := strings.TrimPrefix(callback.Data, "asset_")
				assetSupported := false
				for _, supportedAsset := range supportedAssets {
					if asset == supportedAsset {
						assetSupported = true
						break
					}
				}
				if !assetSupported {
					response = "Криптовалюта не поддерживается."
				} else {
					state.asset = asset
					state.amountStep = true
					response = fmt.Sprintf("Вы выбрали %s. Введите сумму:", asset)
				}
				msg = tgbotapi.NewMessage(chatID, response)
			} else if strings.HasPrefix(callback.Data, "good_") {
				// Покупка товара
				goodIDStr := strings.TrimPrefix(callback.Data, "good_")
				goodID, err := strconv.Atoi(goodIDStr)
				if err != nil {
					response = "Ошибка: неверный ID товара."
				} else {
					goods, err := db.GetGoods()
					if err != nil {
						response = "Ошибка получения товаров."
					} else {
						var good models.Good
						var found bool
						for _, g := range goods {
							if g.ID == goodID {
								good = g
								found = true
								break
							}
						}
						if !found {
							response = "Товар не найден."
						} else {
							balance, err := db.GetUserBalance(userID)
							if err != nil {
								response = "Ошибка проверки баланса."
							} else if balance < good.Value {
								response = "Недостаточно средств на балансе."
							} else {
								// Списываем средства и добавляем покупку
								newBalance := balance - good.Value
								if err := db.UpdateUserBalance(userID, newBalance); err != nil {
									response = "Ошибка обновления баланса."
								} else {
									if err := db.AddPurchase(userID, good.Value, "Good", good.Name, "Process"); err != nil {
										response = "Ошибка записи покупки."
										// Возвращаем баланс, если покупка не записалась
										db.UpdateUserBalance(userID, balance)
									} else {
										response = fmt.Sprintf("Товар '%s' успешно приобретен за %.2f ₽!", good.Name, good.Value)
									}
								}
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
			} else if strings.HasPrefix(callback.Data, "service_") {
				// Покупка услуги
				serviceIDStr := strings.TrimPrefix(callback.Data, "service_")
				serviceID, err := strconv.Atoi(serviceIDStr)
				if err != nil {
					response = "Ошибка: неверный ID услуги."
				} else {
					services, err := db.GetServices()
					if err != nil {
						response = "Ошибка получения услуг."
					} else {
						var service models.Service
						var found bool
						for _, s := range services {
							if s.ID == serviceID {
								service = s
								found = true
								break
							}
						}
						if !found {
							response = "Услуга не найдена."
						} else {
							balance, err := db.GetUserBalance(userID)
							if err != nil {
								response = "Ошибка проверки баланса."
							} else if balance < service.Value {
								response = "Недостаточно средств на балансе."
							} else {
								// Списываем средства и добавляем покупку
								newBalance := balance - service.Value
								if err := db.UpdateUserBalance(userID, newBalance); err != nil {
									response = "Ошибка обновления баланса."
								} else {
									if err := db.AddPurchase(userID, service.Value, "Service", service.Name, "Process"); err != nil {
										response = "Ошибка записи покупки."
										// Возвращаем баланс
										db.UpdateUserBalance(userID, balance)
									} else {
										response = fmt.Sprintf("Услуга '%s' успешно приобретена за %.2f ₽!", service.Name, service.Value)
									}
								}
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
			} else {
				switch callback.Data {
				case "back_to_menu":
					delete(clientStates, userID)
					response = "Добро пожаловать в магазин! Выберите опцию:"
					msg = tgbotapi.NewMessage(chatID, response)
					msg.ReplyMarkup = clientMenu()

				case "catalog":
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
					balance, err := db.GetUserBalance(userID)
					if err != nil {
						log.Printf("Failed to get balance for user %d: %v", userID, err)
						balance = 0.0
					}
					historyCount, err := db.GetPurchaseHistory(userID)
					if err != nil {
						log.Printf("Failed to get history for user %d: %v", userID, err)
						historyCount = 0
					}
					response = fmt.Sprintf(
						"*Ваш баланс:* %.2f ₽ 💰\n\n🆔 ID: %d\n🛍️ Покупок: %d\n\n━━━━━━━━━━━━",
						balance, userID, historyCount)
					msg = tgbotapi.NewMessage(chatID, response)
					msg.ParseMode = "Markdown"
					msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("💸 Пополнить баланс", "top_up_balance"),
						),
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("🛒 История покупок", "purchase_history"),
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
						response = "Ошибка получения товаров."
					} else if len(goods) == 0 {
						response = "Товаров пока нет."
					} else {
						response = "Доступные товары:"
						var buttons [][]tgbotapi.InlineKeyboardButton
						for _, good := range goods {
							buttonText := fmt.Sprintf("%s (%.2f ₽)", good.Name, good.Value)
							buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
								tgbotapi.NewInlineKeyboardButtonData(buttonText, fmt.Sprintf("good_%d", good.ID)),
							))
						}
						buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
						))
						msg = tgbotapi.NewMessage(chatID, response)
						msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)
					}

				case "show_services":
					services, err := db.GetServices()
					if err != nil {
						response = "Ошибка получения услуг."
					} else if len(services) == 0 {
						response = "Услуг пока нет."
					} else {
						response = "Доступные услуги:"
						var buttons [][]tgbotapi.InlineKeyboardButton
						for _, service := range services {
							buttonText := fmt.Sprintf("%s (%.2f ₽)", service.Name, service.Value)
							buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
								tgbotapi.NewInlineKeyboardButtonData(buttonText, fmt.Sprintf("service_%d", service.ID)),
							))
						}
						buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
						))
						msg = tgbotapi.NewMessage(chatID, response)
						msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)
					}

				case "top_up_balance":
					response = "Выберите криптовалюту:"
					var rows [][]tgbotapi.InlineKeyboardButton
					for _, asset := range supportedAssets {
						rows = append(rows, tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData(asset, "asset_"+asset),
						))
					}
					rows = append(rows, tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
					))
					msg = tgbotapi.NewMessage(chatID, response)
					msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)

				case "purchase_history":
					purchases, err := db.GetUserPurchases(userID)
					if err != nil {
						response = "Ошибка получения истории покупок."
					} else if len(purchases) == 0 {
						response = "У вас пока нет покупок."
					} else {
						response = "История покупок:\n"
						for _, purchase := range purchases {
							timeFormatted := time.Unix(purchase.Time, 0).Format("2006-01-02 15:04:05")
							response += fmt.Sprintf(
								"ID: %d, %s (%s) - %.2f ₽, Статус: %s, Время: %s\n",
								purchase.ID, purchase.Name, purchase.Type, purchase.Value, purchase.Status, timeFormatted)
						}
					}
					msg = tgbotapi.NewMessage(chatID, response)
					msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
						),
					)

				default:
					response = "Неизвестное действие."
					msg = tgbotapi.NewMessage(chatID, response)
					msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", "back_to_menu"),
						),
					)
				}
			}

			sentMsg, err := bot.Send(msg)
			if err != nil {
				log.Println("Failed to send message:", err)
			} else {
				state.lastMessageID = sentMsg.MessageID
			}

			bot.Send(tgbotapi.NewCallback(callback.ID, ""))
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

func createCryptoInvoice(userID int64, amount float64, asset string) (*cryptopay.Invoice, int64, int64, error) {
	params := cryptopay.CreateInvoiceParams{
		Asset:         asset,
		Amount:        amount,
		Description:   fmt.Sprintf("Пополнение баланса пользователя %d", userID),
		Payload:       fmt.Sprintf("user_id:%d", userID),
		AllowComments: true,
		ExpiresIn:     1800,
		PaidBtnName:   "openBot",
		PaidBtnUrl:    "https://t.me/your_bot_name",
	}

	invoice, err := cryptoClient.CreateInvoice(params)
	if err != nil {
		return nil, 0, 0, err
	}

	paymentID, err := db.AddPayment(userID, asset, amount, "Wait", invoice.InvoiceID)
	if err != nil {
		return invoice, 0, 0, err
	}

	return invoice, paymentID, invoice.InvoiceID, nil
}

func checkCryptoInvoiceStatus(invoiceID int64, paymentID int64, userID int64) (string, error) {
	invoice, err := cryptoClient.GetInvoice(invoiceID)
	if err != nil {
		return "", err
	}

	if invoice.Status == "paid" {
		payment, err := db.GetPaymentByID(paymentID)
		if err == nil && payment.Status != "Ready" {
			currentBalance, err := db.GetUserBalance(userID)
			if err == nil {
				newBalance := currentBalance + payment.Value
				if err := db.UpdateUserBalance(userID, newBalance); err != nil {
					log.Printf("Failed to update balance for user %d: %v", userID, err)
				}
				if err := db.UpdatePaymentStatus(paymentID, "Ready"); err != nil {
					log.Printf("Failed to update payment status: %v", err)
				}
			}
		}
		return "Ready", nil
	}

	return "Wait", nil
}

func checkPaymentStatus(bot *tgbotapi.BotAPI) {
	for {
		time.Sleep(30 * time.Second)
		for userID, state := range clientStates {
			if len(state.waitingList) == 0 {
				continue
			}
			payments, err := db.GetUserPayments(userID)
			if err != nil {
				log.Printf("Failed to get payments for user %d: %v", userID, err)
				continue
			}
			for _, payment := range payments {
				if payment.Status != "Wait" {
					continue
				}
				status, err := checkCryptoInvoiceStatus(payment.InvoiceID, payment.ID, userID)
				if err != nil {
					log.Printf("Failed to check payment status: %v", err)
					continue
				}
				if status == "Ready" {
					msg := tgbotapi.NewMessage(userID, fmt.Sprintf(
						"✅ Платеж на %.2f %s обработан! Баланс обновлен.",
						payment.Value, payment.CryptoType))
					bot.Send(msg)
					delete(state.waitingList, userID)
				}
			}
		}
	}
}
