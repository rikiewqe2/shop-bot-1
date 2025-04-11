package db

import (
	"database/sql"
	"fmt"
	"shop-bot/models"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

func InitDB() error {
	var err error
	DB, err = sql.Open("sqlite3", "./shop.db")
	if err != nil {
		return err
	}

	// Создание таблиц с новой структурой
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id_user INTEGER PRIMARY KEY,
			balance REAL DEFAULT 0.0,
			history INTEGER DEFAULT 0
		);
		
		CREATE TABLE IF NOT EXISTS goods (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT,
			value REAL,
			description TEXT
		);
		
		CREATE TABLE IF NOT EXISTS services (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT,
			value REAL,
			description TEXT
		);
		
		CREATE TABLE IF NOT EXISTS buy (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			id_user INTEGER,
			value REAL,
			type TEXT CHECK(type IN ('Good', 'Service')),
			name TEXT,
			status TEXT CHECK(status IN ('Ready', 'Process', 'Wait')),
			time INTEGER,
			FOREIGN KEY (id_user) REFERENCES users(id_user)
		);
		
		CREATE TABLE IF NOT EXISTS payments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			id_user INTEGER,
			type_crypto TEXT,
			value REAL,
			status TEXT CHECK(status IN ('Ready', 'Wait')),
			time INTEGER,
			invoice_id INTEGER,
			FOREIGN KEY (id_user) REFERENCES users(id_user)
		);
	`)

	return err
}

// AddUser добавляет нового пользователя или игнорирует, если он уже существует
func AddUser(user models.User) error {
	_, err := DB.Exec("INSERT OR IGNORE INTO users (id_user, balance, history) VALUES (?, ?, ?)",
		user.ID, user.Balance, 0)
	if err != nil {
		fmt.Println("Error adding user:", err)
	}
	return err
}

// AddGood добавляет новый товар
func AddGood(good models.Good) error {
	_, err := DB.Exec("INSERT INTO goods (name, value, description) VALUES (?, ?, ?)",
		good.Name, good.Value, good.Descr)
	if err != nil {
		fmt.Println("Error adding good:", err)
	}
	return err
}

// AddService добавляет новую услугу
func AddService(service models.Service) error {
	_, err := DB.Exec("INSERT INTO services (name, value, description) VALUES (?, ?, ?)",
		service.Name, service.Value, service.Descr)
	if err != nil {
		fmt.Println("Error adding service:", err)
	}
	return err
}

// GetGoods возвращает список всех товаров
func GetGoods() ([]models.Good, error) {
	rows, err := DB.Query("SELECT id, name, value, description FROM goods")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var goods []models.Good
	for rows.Next() {
		var g models.Good
		if err := rows.Scan(&g.ID, &g.Name, &g.Value, &g.Descr); err != nil {
			return nil, err
		}
		goods = append(goods, g)
	}
	return goods, nil
}

// GetServices возвращает список всех услуг
func GetServices() ([]models.Service, error) {
	rows, err := DB.Query("SELECT id, name, value, description FROM services")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []models.Service
	for rows.Next() {
		var s models.Service
		if err := rows.Scan(&s.ID, &s.Name, &s.Value, &s.Descr); err != nil {
			return nil, err
		}
		services = append(services, s)
	}
	return services, nil
}

// GetUserBalance получает баланс пользователя
func GetUserBalance(userID int64) (float64, error) {
	var balance float64
	err := DB.QueryRow("SELECT balance FROM users WHERE id_user = ?", userID).Scan(&balance)
	if err == sql.ErrNoRows {
		// Пользователь не найден, добавляем его
		_, err := DB.Exec("INSERT INTO users (id_user, balance, history) VALUES (?, ?, ?)",
			userID, 0.0, 0)
		if err != nil {
			fmt.Println("Error adding user in GetUserBalance:", err)
			return 0, err
		}
		return 0.0, nil
	}
	if err != nil {
		fmt.Println("Error getting user balance:", err)
		return 0, err
	}
	return balance, nil
}

// UpdateUserBalance обновляет баланс пользователя
func UpdateUserBalance(userID int64, newBalance float64) error {
	if newBalance < 0 {
		return fmt.Errorf("баланс не может быть отрицательным")
	}
	result, err := DB.Exec("UPDATE users SET balance = ? WHERE id_user = ?", newBalance, userID)
	if err != nil {
		fmt.Println("Error updating user balance:", err)
		return err
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		// Пользователь не найден, добавляем его
		_, err := DB.Exec("INSERT INTO users (id_user, balance, history) VALUES (?, ?, ?)",
			userID, newBalance, 0)
		if err != nil {
			fmt.Println("Error adding user in UpdateUserBalance:", err)
			return fmt.Errorf("ошибка при добавлении пользователя: %v", err)
		}
	}
	return nil
}

// GetPurchaseHistory возвращает количество покупок пользователя
func GetPurchaseHistory(userID int64) (int, error) {
	var count int
	err := DB.QueryRow("SELECT history FROM users WHERE id_user = ?", userID).Scan(&count)
	if err == sql.ErrNoRows {
		// Пользователь не найден, добавляем его
		_, err := DB.Exec("INSERT INTO users (id_user, balance, history) VALUES (?, ?, ?)",
			userID, 0.0, 0)
		if err != nil {
			fmt.Println("Error adding user in GetPurchaseHistory:", err)
			return 0, err
		}
		return 0, nil
	}
	if err != nil {
		fmt.Println("Error getting purchase history:", err)
		return 0, err
	}

	return count, nil
}

// IncrementPurchaseCount увеличивает счетчик покупок пользователя
func IncrementPurchaseCount(userID int64) error {
	var count int
	err := DB.QueryRow("SELECT history FROM users WHERE id_user = ?", userID).Scan(&count)
	if err == sql.ErrNoRows {
		// Пользователь не найден, добавляем его
		_, err := DB.Exec("INSERT INTO users (id_user, balance, history) VALUES (?, ?, ?)",
			userID, 0.0, 1)
		if err != nil {
			fmt.Println("Error adding user in IncrementPurchaseCount:", err)
			return err
		}
		return nil
	} else if err != nil {
		fmt.Println("Error getting history count:", err)
		return err
	}

	// Увеличиваем счетчик покупок
	_, err = DB.Exec("UPDATE users SET history = ? WHERE id_user = ?", count+1, userID)
	if err != nil {
		fmt.Println("Error updating purchase count:", err)
		return err
	}

	return nil
}

// AddPurchase добавляет запись о покупке
func AddPurchase(userID int64, value float64, purchaseType, name, status string) error {
	currentTime := time.Now().Unix()
	_, err := DB.Exec("INSERT INTO buy (id_user, value, type, name, status, time) VALUES (?, ?, ?, ?, ?, ?)",
		userID, value, purchaseType, name, status, currentTime)
	if err != nil {
		fmt.Println("Error adding purchase:", err)
		return err
	}

	// Увеличиваем счетчик покупок пользователя
	return IncrementPurchaseCount(userID)
}

// GetUserPurchases возвращает историю покупок пользователя
func GetUserPurchases(userID int64) ([]models.Purchase, error) {
	rows, err := DB.Query(`
		SELECT id, id_user, value, type, name, status, time 
		FROM buy 
		WHERE id_user = ? 
		ORDER BY time DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var purchases []models.Purchase
	for rows.Next() {
		var p models.Purchase
		if err := rows.Scan(&p.ID, &p.UserID, &p.Value, &p.Type, &p.Name, &p.Status, &p.Time); err != nil {
			return nil, err
		}
		purchases = append(purchases, p)
	}
	return purchases, nil
}

// UpdatePurchaseStatus обновляет статус покупки
func UpdatePurchaseStatus(purchaseID int, status string) error {
	_, err := DB.Exec("UPDATE buy SET status = ? WHERE id = ?", status, purchaseID)
	if err != nil {
		fmt.Println("Error updating purchase status:", err)
		return err
	}
	return nil
}

// AddPayment добавляет информацию о платеже
func AddPayment(userID int64, cryptoType string, value float64, status string, invoiceID int64) (int64, error) {
	currentTime := time.Now().Unix()
	result, err := DB.Exec("INSERT INTO payments (id_user, type_crypto, value, status, time, invoice_id) VALUES (?, ?, ?, ?, ?, ?)",
		userID, cryptoType, value, status, currentTime, invoiceID)
	if err != nil {
		fmt.Println("Error adding payment:", err)
		return 0, err
	}

	// Получаем ID созданной записи
	paymentID, err := result.LastInsertId()
	if err != nil {
		fmt.Println("Error getting last insert ID:", err)
		return 0, err
	}

	return paymentID, nil
}

// UpdatePaymentStatus обновляет статус платежа
func UpdatePaymentStatus(paymentID int64, status string) error {
	_, err := DB.Exec("UPDATE payments SET status = ? WHERE id = ?", status, paymentID)
	if err != nil {
		fmt.Println("Error updating payment status:", err)
		return err
	}
	return nil
}

// GetPaymentByID возвращает информацию о платеже по его ID
func GetPaymentByID(paymentID int64) (*models.Payment, error) {
	payment := &models.Payment{}
	err := DB.QueryRow(`
		SELECT id, id_user, type_crypto, value, status, time, invoice_id 
		FROM payments 
		WHERE id = ?`, paymentID).
		Scan(&payment.ID, &payment.UserID, &payment.CryptoType, &payment.Value, &payment.Status, &payment.Time, &payment.InvoiceID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("payment not found")
		}
		fmt.Println("Error getting payment:", err)
		return nil, err
	}
	return payment, nil
}

// GetUserPayments возвращает историю платежей пользователя
func GetUserPayments(userID int64) ([]models.Payment, error) {
	rows, err := DB.Query(`
		SELECT id, id_user, type_crypto, value, status, time, invoice_id 
		FROM payments 
		WHERE id_user = ? 
		ORDER BY time DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var payments []models.Payment
	for rows.Next() {
		var p models.Payment
		if err := rows.Scan(&p.ID, &p.UserID, &p.CryptoType, &p.Value, &p.Status, &p.Time, &p.InvoiceID); err != nil {
			return nil, err
		}
		payments = append(payments, p)
	}
	return payments, nil
}
