package models

type Good struct {
	ID    int
	Name  string
	Value float64
	Descr string // соответствует полю description в БД
}
