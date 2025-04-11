package models

type User struct {
	ID      int64 // переименовано в id_user в БД
	Balance float64
	History int // теперь это число покупок, а не строка
}
