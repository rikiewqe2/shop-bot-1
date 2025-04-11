package models

type Service struct {
	ID    int
	Name  string
	Value float64
	Descr string // соответствует полю description в БД
}
