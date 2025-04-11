package models

type Purchase struct {
	ID     int64
	UserID int64 // соответствует полю id_user в БД
	Value  float64
	Type   string // 'Good' или 'Service'
	Name   string
	Status string // 'Ready', 'Process' или 'Wait'
	Time   int64
}
