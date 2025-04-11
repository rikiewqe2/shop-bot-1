package config

type Config struct {
	ClientBotToken string
	AdminBotToken  string
	CryptoPayToken string // Добавьте это поле
}

func LoadConfig() Config {
	return Config{
		ClientBotToken: "7632574750:AAF7AzEbCkZuaHYKfSHzeJhhdoxNK3ZJ-jo",
		AdminBotToken:  "8127433801:AAEQyBUhROaiuv9qzMz1w7venwdzciPnfPY",
		CryptoPayToken: "368429:AAJOnB8gkYvfhqHBa4AOqBmBpKa8UbTTd8E", // Замените на ваш токен Crypto Pay API
	}
}
