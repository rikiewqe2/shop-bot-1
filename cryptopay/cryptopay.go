package cryptopay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	BaseURL = "https://pay.crypt.bot/api"
)

// CryptoPayClient представляет клиента для работы с Crypto Pay API
type CryptoPayClient struct {
	ApiToken string
}

// NewCryptoPayClient создает новый экземпляр клиента Crypto Pay
func NewCryptoPayClient(apiToken string) *CryptoPayClient {
	return &CryptoPayClient{
		ApiToken: apiToken,
	}
}

// CreateInvoiceParams определяет параметры для создания счета
type CreateInvoiceParams struct {
	Asset          string  `json:"asset"`
	Amount         float64 `json:"amount"`
	Description    string  `json:"description,omitempty"`
	HiddenMessage  string  `json:"hidden_message,omitempty"`
	PaidBtnName    string  `json:"paid_btn_name,omitempty"`
	PaidBtnUrl     string  `json:"paid_btn_url,omitempty"`
	Payload        string  `json:"payload,omitempty"`
	AllowComments  bool    `json:"allow_comments,omitempty"`
	AllowAnonymous bool    `json:"allow_anonymous,omitempty"`
	ExpiresIn      int     `json:"expires_in,omitempty"`
}

// Float64String - кастомный тип для разбора строки в float64
type Float64String float64

func (f *Float64String) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = Float64String(val)
	return nil
}

// TimeString - кастомный тип для разбора строки ISO 8601 в time.Time
type TimeString struct {
	time.Time
}

func (t *TimeString) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}

// Invoice представляет структуру счета, возвращаемого API
type Invoice struct {
	InvoiceID       int64         `json:"invoice_id"`
	Status          string        `json:"status"`
	Hash            string        `json:"hash"`
	Asset           string        `json:"asset"`
	Amount          Float64String `json:"amount"`
	PayUrl          string        `json:"pay_url"`
	Description     string        `json:"description"`
	CreatedAt       TimeString    `json:"created_at"`
	AllowComments   bool          `json:"allow_comments"`
	AllowAnonymous  bool          `json:"allow_anonymous"`
	ExpiresAt       TimeString    `json:"expiration_date"`
	PaidAt          TimeString    `json:"paid_at,omitempty"`
	PaidAnonymously bool          `json:"paid_anonymously,omitempty"`
	Comment         string        `json:"comment,omitempty"`
	HiddenMessage   string        `json:"hidden_message"`
	Payload         string        `json:"payload"`
	PaidBtnName     string        `json:"paid_btn_name"`
	PaidBtnUrl      string        `json:"paid_btn_url"`
}

// APIResponse представляет общую структуру ответа от API
type APIResponse struct {
	Ok     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  interface{}     `json:"error,omitempty"`
}

// CreateInvoice создает счет для оплаты
func (c *CryptoPayClient) CreateInvoice(params CreateInvoiceParams) (*Invoice, error) {
	url := fmt.Sprintf("%s/createInvoice", BaseURL)

	payload, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("ошибка при сериализации параметров: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return nil, fmt.Errorf("ошибка при создании запроса: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Crypto-Pay-API-Token", c.ApiToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка при выполнении запроса: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка при чтении ответа: %w", err)
	}

	fmt.Println("Ответ API:", string(body))

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка при разборе ответа API: %w", err)
	}

	if !apiResp.Ok {
		var errorMsg string
		switch errVal := apiResp.Error.(type) {
		case string:
			errorMsg = errVal
		case map[string]interface{}:
			if msg, ok := errVal["message"].(string); ok {
				errorMsg = msg
			} else {
				errorMsg = "Неизвестная ошибка"
			}
		default:
			errorMsg = "Неизвестная ошибка"
		}
		return nil, fmt.Errorf("API вернуло ошибку: %s", errorMsg)
	}

	var invoice Invoice
	if err := json.Unmarshal(apiResp.Result, &invoice); err != nil {
		return nil, fmt.Errorf("ошибка при разборе данных счета: %w", err)
	}

	return &invoice, nil
}

// GetInvoice получает информацию о счете по его ID
func (c *CryptoPayClient) GetInvoice(invoiceID int64) (*Invoice, error) {
	url := fmt.Sprintf("%s/getInvoices?invoice_ids=%d", BaseURL, invoiceID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("ошибка при создании запроса: %w", err)
	}

	req.Header.Set("Crypto-Pay-API-Token", c.ApiToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка при выполнении запроса: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка при чтении ответа: %w", err)
	}

	fmt.Println("Ответ API:", string(body))

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка при разборе ответа API: %w", err)
	}

	if !apiResp.Ok {
		var errorMsg string
		switch errVal := apiResp.Error.(type) {
		case string:
			errorMsg = errVal
		case map[string]interface{}:
			if msg, ok := errVal["message"].(string); ok {
				errorMsg = msg
			} else {
				errorMsg = "Неизвестная ошибка"
			}
		default:
			errorMsg = "Неизвестная ошибка"
		}
		return nil, fmt.Errorf("API вернуло ошибку: %s", errorMsg)
	}

	// Ожидаем одиночный объект Invoice
	var invoice Invoice
	if err := json.Unmarshal(apiResp.Result, &invoice); err != nil {
		return nil, fmt.Errorf("ошибка при разборе данных счета: %w", err)
	}

	return &invoice, nil
}
