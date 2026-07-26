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
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/gomail.v2"
)

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

// WebSocket clients
var (
	clients   = make(map[string]*websocket.Conn)
	clientsMu sync.Mutex
	upgrader  = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
)

type WSMessage struct {
	Type       string `json:"type"`       // "history", "message", "error"
	Sender     string `json:"sender"`     // "client" или "support"
	Message    string `json:"message"`
	CreatedAt  string `json:"created_at"`
	Email      string `json:"email,omitempty"`
	TgUsername string `json:"tg_username,omitempty"`
}

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
	db, err = sql.Open("sqlite3", "./data/chat.db")
	if err != nil {
		log.Fatalf("Ошибка открытия БД: %v", err)
	}

	createTableQuery := `
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		client_id TEXT NOT NULL,
		sender TEXT NOT NULL,
		message TEXT NOT NULL,
		email TEXT,
		tg_username TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.Exec(createTableQuery); err != nil {
		log.Fatalf("Ошибка создания таблицы: %v", err)
	}
}

func main() {
	if tgBotToken != "" {
		go telegramBotListener()
	}

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	http.HandleFunc("/api/ws", handleWebSocket)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Real-time Сервер запущен на порту %s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	if clientID == "" {
		http.Error(w, "client_id is required", http.StatusBadRequest)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WS Upgrade error: %v", err)
		return
	}
	defer ws.Close()

	clientsMu.Lock()
	clients[clientID] = ws
	clientsMu.Unlock()

	defer func() {
		clientsMu.Lock()
		delete(clients, clientID)
		clientsMu.Unlock()
	}()

	// При подключении отправляем историю
	sendHistory(clientID, ws)

	// Чтение сообщений от клиента
	for {
		var msg WSMessage
		err := ws.ReadJSON(&msg)
		if err != nil {
			break
		}

		if msg.Type == "message" && strings.TrimSpace(msg.Message) != "" {
			saveMessage(clientID, "client", msg.Message, msg.Email, msg.TgUsername)
			
			// Отправляем уведомления (TG/Email)
			go sendAdminNotifications(clientID, msg)
		}
	}
}

func sendHistory(clientID string, ws *websocket.Conn) {
	rows, err := db.Query("SELECT sender, message, created_at FROM messages WHERE client_id = ? ORDER BY id ASC", clientID)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var msg WSMessage
		msg.Type = "history"
		if err := rows.Scan(&msg.Sender, &msg.Message, &msg.CreatedAt); err == nil {
			ws.WriteJSON(msg)
		}
	}
}

func saveMessage(clientID, sender, message, email, tg string) {
	_, err := db.Exec("INSERT INTO messages (client_id, sender, message, email, tg_username) VALUES (?, ?, ?, ?, ?)",
		clientID, sender, message, email, tg)
	if err != nil {
		log.Printf("DB Insert error: %v", err)
	}
}

func sendAdminNotifications(clientID string, msg WSMessage) {
	// Telegram
	if tgBotToken != "" && tgChatID != "" {
		apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tgBotToken)
		tgInfo := msg.TgUsername
		if tgInfo == "" {
			tgInfo = "Не указан"
		}
		emailInfo := msg.Email
		if emailInfo == "" {
			emailInfo = "Не указан"
		}

		text := fmt.Sprintf("🚨 Новое сообщение в чате!\n\nID: %s\nEmail: %s\nTG: %s\n\nТекст:\n%s", 
			clientID, emailInfo, tgInfo, msg.Message)

		payload := map[string]string{
			"chat_id": tgChatID,
			"text":    text,
		}
		jsonData, _ := json.Marshal(payload)
		http.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	}

	// Email Fallback
	if smtpHost != "" && msg.Email != "" && strings.Contains(msg.Email, "@") {
		dialer := gomail.NewDialer(smtpHost, smtpPort, smtpUser, smtpPass)
		m := gomail.NewMessage()
		m.SetHeader("From", m.FormatAddress(smtpUser, senderName))
		m.SetHeader("To", supportEmail)
		m.SetHeader("Reply-To", msg.Email)
		m.SetHeader("Subject", "Новое обращение в чат от "+msg.Email)
		
		body := fmt.Sprintf("ID: %s<br>Email: %s<br>Текст: %s", clientID, msg.Email, msg.Message)
		m.SetBody("text/html", body)
		dialer.DialAndSend(m)
	}
}

// -------------------------------------------------------------
// TELEGRAM BOT LISTENER (Long Polling)
// -------------------------------------------------------------

type TgUpdate struct {
	UpdateID int        `json:"update_id"`
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
	idRegex := regexp.MustCompile(`ID:\s*([^\s]+)`)
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

				originalText := update.Message.ReplyToMessage.Text
				if !strings.Contains(originalText, "Новое сообщение в чате!") && !strings.Contains(originalText, "Новый тикет") {
					continue
				}

				// Извлекаем Client ID
				matches := idRegex.FindStringSubmatch(originalText)
				if len(matches) < 2 {
					sendTelegramMsg(update.Message.Chat.ID, "❌ Ошибка: не удалось найти ID клиента в сообщении.")
					continue
				}
				clientID := matches[1]
				replyText := update.Message.Text

				// Сохраняем в БД
				saveMessage(clientID, "support", replyText, "", "")

				// Пробуем отправить по WebSocket
				clientsMu.Lock()
				ws, online := clients[clientID]
				clientsMu.Unlock()

				wsSent := false
				if online {
					err := ws.WriteJSON(WSMessage{
						Type:      "message",
						Sender:    "support",
						Message:   replyText,
						CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
					})
					if err == nil {
						wsSent = true
					}
				}

				// Если клиент не онлайн, шлем на Email
				emailSent := false
				emailMatches := emailRegex.FindStringSubmatch(originalText)
				if len(emailMatches) >= 2 && emailMatches[1] != "Не" {
					err := sendReplyEmail(emailMatches[1], replyText)
					if err == nil {
						emailSent = true
					}
				}

				// Уведомляем админа
				if wsSent && emailSent {
					sendTelegramMsg(update.Message.Chat.ID, "✅ Ответ доставлен прямо в чат и продублирован на Email!")
				} else if wsSent {
					sendTelegramMsg(update.Message.Chat.ID, "✅ Ответ доставлен прямо в чат (онлайн).")
				} else if emailSent {
					sendTelegramMsg(update.Message.Chat.ID, "✅ Ответ отправлен на Email (клиент оффлайн).")
				} else {
					sendTelegramMsg(update.Message.Chat.ID, "⚠️ Ответ сохранен в базе, но клиент оффлайн и Email недоступен.")
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
	m.SetHeader("Subject", "Ответ от службы поддержки - "+senderName)

	body := fmt.Sprintf(`
		<h2>Здравствуйте!</h2>
		<p style="white-space: pre-wrap; font-size: 16px;">%s</p>
		<hr>
		<p><small>Вы можете продолжить диалог на нашем портале поддержки.</small></p>
	`, replyText)

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
