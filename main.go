package main

import (
	"bytes"
	"database/sql"
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
	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/gomail.v2"
)

type TicketRequest struct {
	ClientID   string `json:"client_id"`
	Email      string `json:"email"`
	TgUsername string `json:"tg_username"`
	Message    string `json:"message"`
}

type TicketMessage struct {
	Sender    string `json:"sender"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
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
	db           *sql.DB
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

	initDB()
}

func initDB() {
	os.MkdirAll("./data", os.ModePerm)
	var err error
	db, err = sql.Open("sqlite3", "./data/tickets.db")
	if err != nil {
		log.Fatalf("Ошибка открытия БД: %v", err)
	}

	query := `
	CREATE TABLE IF NOT EXISTS tickets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		client_id TEXT,
		email TEXT,
		sender TEXT,
		message TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.Exec(query); err != nil {
		log.Fatalf("Ошибка создания таблицы: %v", err)
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
	http.HandleFunc("/api/history", handleHistory)

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
	req.ClientID = strings.TrimSpace(req.ClientID)

	if req.Email == "" || req.Message == "" {
		sendJSONError(w, "Email и Проблема обязательны для заполнения", http.StatusBadRequest)
		return
	}
	if !strings.Contains(req.Email, "@") {
		sendJSONError(w, "Введите корректный Email адрес", http.StatusBadRequest)
		return
	}

	// Сохраняем тикет в БД (история)
	if req.ClientID != "" {
		saveMessage(req.ClientID, req.Email, "client", req.Message)
	}

	if smtpHost != "" {
		go processEmails(req)
	}
	if tgBotToken != "" && tgChatID != "" {
		go sendTelegramNotification(req)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Тикет успешно создан",
	})
}

func saveMessage(clientID, email, sender, message string) {
	_, err := db.Exec("INSERT INTO tickets (client_id, email, sender, message) VALUES (?, ?, ?, ?)",
		clientID, email, sender, message)
	if err != nil {
		log.Printf("Ошибка сохранения сообщения: %v", err)
	}
}

func handleHistory(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	if clientID == "" {
		sendJSONError(w, "client_id required", http.StatusBadRequest)
		return
	}

	rows, err := db.Query("SELECT sender, message, created_at FROM tickets WHERE client_id = ? ORDER BY id ASC", clientID)
	if err != nil {
		sendJSONError(w, "Ошибка БД", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var history []TicketMessage
	for rows.Next() {
		var msg TicketMessage
		if err := rows.Scan(&msg.Sender, &msg.Message, &msg.CreatedAt); err == nil {
			history = append(history, msg)
		}
	}

	if history == nil {
		history = []TicketMessage{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

func processEmails(req TicketRequest) {
	dialer := gomail.NewDialer(smtpHost, smtpPort, smtpUser, smtpPass)
	dialer.SSL = true

	fromEmail := os.Getenv("SMTP_FROM")
	if fromEmail == "" {
		fromEmail = smtpUser
	}

	// Письмо Администратору
	mAdmin := gomail.NewMessage()
	mAdmin.SetHeader("From", mAdmin.FormatAddress(fromEmail, senderName))
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
	if err := dialer.DialAndSend(mAdmin); err != nil {
		log.Printf("Ошибка отправки админу: %v\n", err)
	}

	// Автоответ Клиенту
	mClient := gomail.NewMessage()
	mClient.SetHeader("From", mClient.FormatAddress(fromEmail, senderName))
	mClient.SetHeader("To", req.Email)
	mClient.SetHeader("Subject", "Ваше обращение принято - "+senderName)
	
	bodyClient := fmt.Sprintf(`
		<h2>Здравствуйте!</h2>
		<p>Мы получили ваше обращение. Наш оператор рассмотрит его и ответит вам в ближайшее время.</p>
		<hr>
		<p><strong>Ваш текст обращения:</strong><br>%s</p>
	`, req.Message)

	mClient.SetBody("text/html", bodyClient)
	if err := dialer.DialAndSend(mClient); err != nil {
		log.Printf("Ошибка отправки клиенту: %v\n", err)
	}
}

func sendTelegramNotification(req TicketRequest) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tgBotToken)
	tgInfo := "Не указан"
	if req.TgUsername != "" { tgInfo = req.TgUsername }

	// Добавляем ID в текст, чтобы потом извлечь его при ответе
	text := fmt.Sprintf("🚨 Новый тикет в поддержку\n\nID: %s\nEmail: %s\nTG: %s\n\nПроблема:\n%s", 
		req.ClientID, req.Email, tgInfo, req.Message)

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
	idRegex := regexp.MustCompile(`ID:\s*([^\s]+)`)

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

				// Извлекаем Client ID (для сохранения в историю)
				var clientID string
				idMatches := idRegex.FindStringSubmatch(originalText)
				if len(idMatches) >= 2 {
					clientID = idMatches[1]
				}

				// Сохраняем ответ в базу
				if clientID != "" {
					saveMessage(clientID, targetEmail, "support", replyText)
				}

				// Отправляем письмо клиенту
				err := sendReplyEmail(targetEmail, replyText)
				if err != nil {
					sendTelegramMsg(update.Message.Chat.ID, fmt.Sprintf("❌ Ошибка при отправке на почту %s: %v", targetEmail, err))
				} else {
					sendTelegramMsg(update.Message.Chat.ID, fmt.Sprintf("✅ Успешно! Ваш ответ сохранен в историю и отправлен клиенту на почту:\n%s", targetEmail))
				}
			}
		}
		resp.Body.Close()
	}
}

func sendReplyEmail(toEmail, replyText string) error {
	dialer := gomail.NewDialer(smtpHost, smtpPort, smtpUser, smtpPass)
	dialer.SSL = true

	fromEmail := os.Getenv("SMTP_FROM")
	if fromEmail == "" {
		fromEmail = smtpUser
	}

	m := gomail.NewMessage()
	m.SetHeader("From", m.FormatAddress(fromEmail, senderName))
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
