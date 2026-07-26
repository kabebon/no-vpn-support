package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/gomail.v2"
)

type TicketRequest struct {
	Email      string `json:"email"`
	TgUsername string `json:"tg_username"`
	Message    string `json:"message"`
}

var (
	smtpHost     string
	smtpPort     int
	smtpUser     string
	smtpPass     string
	supportEmail string
	senderName   string
	tgBotToken   string
	tgChatID     string
)

func init() {
	_ = godotenv.Load()

	smtpHost = os.Getenv("SMTP_HOST")
	portStr := os.Getenv("SMTP_PORT")
	smtpPort, _ = strconv.Atoi(portStr)
	smtpUser = os.Getenv("SMTP_USER")
	smtpPass = os.Getenv("SMTP_PASS")
	supportEmail = os.Getenv("SUPPORT_EMAIL")
	senderName = os.Getenv("SENDER_NAME")
	tgBotToken = os.Getenv("TG_BOT_TOKEN")
	tgChatID = os.Getenv("TG_CHAT_ID")

	if senderName == "" {
		senderName = "KabebaVPN Support"
	}
}

func main() {
	if smtpHost == "" || smtpPort == 0 || smtpUser == "" || supportEmail == "" {
		log.Println("ВНИМАНИЕ: Переменные среды для SMTP не заданы! Письма отправляться не будут.")
	} else {
		log.Println("Конфигурация SMTP загружена успешно.")
	}

	if tgBotToken != "" {
		log.Println("Запуск слушателя Telegram-бота для ответов...")
		go telegramBotListener()
	}

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	http.HandleFunc("/api/ticket", handleTicket)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Сервер поддержки запущен на порту %s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func handleTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req TicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSONError(w, "Неверный формат запроса", http.StatusBadRequest)
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	req.Message = strings.TrimSpace(req.Message)
	req.TgUsername = strings.TrimSpace(req.TgUsername)

	if req.Email == "" || req.Message == "" {
		sendJSONError(w, "Email и Проблема обязательны для заполнения", http.StatusBadRequest)
		return
	}

	if !strings.Contains(req.Email, "@") {
		sendJSONError(w, "Введите корректный Email адрес", http.StatusBadRequest)
		return
	}

	if smtpHost != "" {
		go processEmails(req)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Тикет успешно создан",
	})
}

func processEmails(req TicketRequest) {
	dialer := gomail.NewDialer(smtpHost, smtpPort, smtpUser, smtpPass)

	// Письмо Администратору
	mAdmin := gomail.NewMessage()
	mAdmin.SetHeader("From", mAdmin.FormatAddress(smtpUser, senderName))
	mAdmin.SetHeader("To", supportEmail)
	mAdmin.SetHeader("Reply-To", req.Email)
	mAdmin.SetHeader("Subject", "Новое обращение в поддержку от "+req.Email)
	
	tgInfo := "Не указан"
	if req.TgUsername != "" {
		tgInfo = req.TgUsername
	}

	bodyAdmin := fmt.Sprintf(`
		<h2>Новое обращение в службу поддержки</h2>
		<p><strong>Email клиента:</strong> %s</p>
		<p><strong>Telegram:</strong> %s</p>
		<hr>
		<h3>Суть проблемы:</h3>
		<p style="white-space: pre-wrap;">%s</p>
	`, req.Email, tgInfo, req.Message)

	mAdmin.SetBody("text/html", bodyAdmin)
	dialer.DialAndSend(mAdmin)

	// Автоответ Клиенту
	mClient := gomail.NewMessage()
	mClient.SetHeader("From", mClient.FormatAddress(smtpUser, senderName))
	mClient.SetHeader("To", req.Email)
	mClient.SetHeader("Subject", "Ваше обращение принято - "+senderName)
	
	bodyClient := fmt.Sprintf(`
		<h2>Здравствуйте!</h2>
		<p>Мы получили ваше обращение. Наш оператор рассмотрит его и ответит вам в ближайшее время.</p>
		<hr>
		<p><strong>Ваш текст обращения:</strong><br>%s</p>
	`, req.Message)

	mClient.SetBody("text/html", bodyClient)
	dialer.DialAndSend(mClient)

	// Уведомление в Telegram
	if tgBotToken != "" && tgChatID != "" {
		sendTelegramNotification(req)
	}
}

func sendTelegramNotification(req TicketRequest) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tgBotToken)
	tgInfo := "Не указан"
	if req.TgUsername != "" { tgInfo = req.TgUsername }

	text := fmt.Sprintf("🚨 Новый тикет в поддержку\n\nEmail: %s\nTG: %s\n\nПроблема:\n%s", 
		req.Email, tgInfo, req.Message)

	payload := map[string]string{
		"chat_id": tgChatID,
		"text":    text,
	}

	jsonData, _ := json.Marshal(payload)
	http.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
}

func sendJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// -------------------------------------------------------------
// TELEGRAM BOT LISTENER (Long Polling)
// -------------------------------------------------------------

type TgUpdate struct {
	UpdateID int       `json:"update_id"`
	Message  *TgMessage `json:"message"`
}

type TgMessage struct {
	MessageID      int        `json:"message_id"`
	Text           string     `json:"text"`
	Chat           TgChat     `json:"chat"`
	ReplyToMessage *TgMessage `json:"reply_to_message"`
}

type TgChat struct {
	ID int64 `json:"id"`
}

type TgResponse struct {
	Ok     bool       `json:"ok"`
	Result []TgUpdate `json:"result"`
}

func telegramBotListener() {
	offset := 0
	client := &http.Client{Timeout: 35 * time.Second}
	emailRegex := regexp.MustCompile(`Email:\s*([^\s]+)`)

	for {
		url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=30", tgBotToken, offset)
		resp, err := client.Get(url)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		var tgResp TgResponse
		if err := json.NewDecoder(resp.Body).Decode(&tgResp); err == nil && tgResp.Ok {
			for _, update := range tgResp.Result {
				offset = update.UpdateID + 1

				if update.Message == nil || update.Message.ReplyToMessage == nil {
					continue
				}

				// Проверяем, является ли оригинальное сообщение тикетом
				originalText := update.Message.ReplyToMessage.Text
				if !strings.Contains(originalText, "Новый тикет в поддержку") {
					continue
				}

				// Извлекаем Email
				matches := emailRegex.FindStringSubmatch(originalText)
				if len(matches) < 2 {
					sendTelegramMsg(update.Message.Chat.ID, "❌ Ошибка: не удалось найти Email клиента в оригинальном сообщении.")
					continue
				}
				targetEmail := matches[1]
				replyText := update.Message.Text

				// Отправляем письмо клиенту
				err := sendReplyEmail(targetEmail, replyText)
				if err != nil {
					sendTelegramMsg(update.Message.Chat.ID, fmt.Sprintf("❌ Ошибка при отправке на почту %s: %v", targetEmail, err))
				} else {
					sendTelegramMsg(update.Message.Chat.ID, fmt.Sprintf("✅ Успешно! Ваш ответ отправлен клиенту на почту:\n%s", targetEmail))
				}
			}
		}
		resp.Body.Close()
	}
}

func sendReplyEmail(toEmail, replyText string) error {
	dialer := gomail.NewDialer(smtpHost, smtpPort, smtpUser, smtpPass)
	m := gomail.NewMessage()
	m.SetHeader("From", m.FormatAddress(smtpUser, senderName))
	m.SetHeader("To", toEmail)
	m.SetHeader("Subject", "Re: Обращение в поддержку - "+senderName)

	body := fmt.Sprintf(`
		<h2>Ответ от службы поддержки</h2>
		<p style="white-space: pre-wrap; font-size: 16px;">%s</p>
		<hr>
		<p><small>С уважением,<br>%s</small></p>
	`, replyText, senderName)

	m.SetBody("text/html", body)
	return dialer.DialAndSend(m)
}

func sendTelegramMsg(chatID int64, text string) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tgBotToken)
	payload := map[string]string{
		"chat_id": strconv.FormatInt(chatID, 10),
		"text":    text,
	}
	jsonData, _ := json.Marshal(payload)
	http.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
}
