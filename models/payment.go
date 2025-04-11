package models

type Payment struct {
	ID         int64
	UserID     int64  // соответствует полю id_user в БД
	CryptoType string // соответствует полю type_crypto в БД
	Value      float64
	Status     string // теперь только 'Ready' или 'Wait'
	Time       int64  // соответствует полю time в БД
	InvoiceID  int64  // соответствует полю invoice_id в БД
}
