package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/gomail.v2"
)

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
	smtpFrom     string
	supportEmail string
	senderName   string
	tgBotToken   string
	tgChatID     string
	cabinetURL   string
	db           *sql.DB

	// Ограничение частоты запросов (Rate Limiter)
	rateLimitMap = make(map[string]time.Time)
	rateLimitMu  sync.Mutex
)

func init() {
	_ = godotenv.Load()

	smtpHost = os.Getenv("SMTP_HOST")
	portStr := os.Getenv("SMTP_PORT")
	smtpPort, _ = strconv.Atoi(portStr)
	smtpUser = os.Getenv("SMTP_USER")
	smtpPass = os.Getenv("SMTP_PASS")
	smtpFrom = os.Getenv("SMTP_FROM")
	if smtpFrom == "" {
		smtpFrom = smtpUser
	}
	supportEmail = os.Getenv("SUPPORT_EMAIL")
	senderName = os.Getenv("SENDER_NAME")
	tgBotToken = os.Getenv("TG_BOT_TOKEN")
	tgChatID = os.Getenv("TG_CHAT_ID")
	cabinetURL = os.Getenv("CABINET_URL")

	if senderName == "" {
		senderName = "Поддержка KabebaVPN"
	}

	initDB()
}

func initDB() {
	os.MkdirAll("./data", os.ModePerm)
	os.MkdirAll("./data/uploads", os.ModePerm)
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
	db.Exec("CREATE INDEX IF NOT EXISTS idx_client_id ON tickets(client_id);")
}

func main() {
	if smtpHost == "" || smtpPort == 0 || smtpUser == "" || supportEmail == "" {
		log.Println("ВНИМАНИЕ: Переменные среды для SMTP не заданы!")
	} else {
		log.Println("Конфигурация SMTP загружена успешно.")
	}

	if tgBotToken != "" {
		log.Println("Запуск слушателя Telegram-бота...")
		go telegramBotListener()
	}

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	http.HandleFunc("/api/ticket", handleTicket)
	http.HandleFunc("/api/history", handleHistory)
	http.HandleFunc("/api/config", handleConfig)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Сервер поддержки запущен на порту %s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	json.NewEncoder(w).Encode(map[string]string{
		"cabinet_url": strings.TrimSpace(cabinetURL),
	})
}

func checkRateLimit(ip string) bool {
	rateLimitMu.Lock()
	defer rateLimitMu.Unlock()
	lastSeen, exists := rateLimitMap[ip]
	if exists && time.Since(lastSeen) < 15*time.Second {
		return false
	}
	rateLimitMap[ip] = time.Now()
	return true
}

func handleTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Защита от DoS и спам-флуда
	clientIP := r.Header.Get("X-Forwarded-For")
	if clientIP == "" {
		clientIP = strings.Split(r.RemoteAddr, ":")[0]
	}
	if !checkRateLimit(clientIP) {
		sendJSONError(w, "Слишком частое создание обращений. Подождите 15 секунд.", http.StatusTooManyRequests)
		return
	}

	// Универсальный парсер: поддерживает multipart/form-data, application/x-www-form-urlencoded и JSON
	var clientID, email, tgUsername, message string

	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		// JSON (старый клиент)
		var jsonReq struct {
			ClientID   string `json:"client_id"`
			Email      string `json:"email"`
			TgUsername string `json:"tg_username"`
			Message    string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&jsonReq); err != nil {
			sendJSONError(w, "Неверный формат запроса", http.StatusBadRequest)
			return
		}
		clientID = strings.TrimSpace(jsonReq.ClientID)
		email = strings.TrimSpace(jsonReq.Email)
		tgUsername = strings.TrimSpace(jsonReq.TgUsername)
		message = strings.TrimSpace(jsonReq.Message)
	} else {
		// multipart/form-data или urlencoded
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			r.ParseForm()
		}
		clientID = strings.TrimSpace(r.FormValue("client_id"))
		email = strings.TrimSpace(r.FormValue("email"))
		tgUsername = strings.TrimSpace(r.FormValue("tg_username"))
		message = strings.TrimSpace(r.FormValue("message"))
	}

	if email == "" || message == "" {
		log.Printf("Пустые поля: email=%q message=%q content-type=%q", email, message, contentType)
		sendJSONError(w, "Email и Проблема обязательны для заполнения", http.StatusBadRequest)
		return
	}
	if !strings.Contains(email, "@") {
		sendJSONError(w, "Введите корректный Email адрес", http.StatusBadRequest)
		return
	}

	// Обработка загружаемых изображений (максимум 5, защита от Directory Traversal и исполняемых файлов)
	var imagePaths []string
	if r.MultipartForm != nil && r.MultipartForm.File != nil {
		fileHeaders := r.MultipartForm.File["images"]
		if len(fileHeaders) == 0 {
			fileHeaders = r.MultipartForm.File["images[]"]
		}
		if len(fileHeaders) == 0 {
			fileHeaders = r.MultipartForm.File["image"]
		}

		validExts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true}
		count := 0

		for _, fileHeader := range fileHeaders {
			if count >= 5 {
				log.Println("Превышен лимит (5 изображений), остальные отброшены")
				break
			}
			cleanName := filepath.Base(fileHeader.Filename)
			ext := strings.ToLower(filepath.Ext(cleanName))
			if !validExts[ext] {
				log.Printf("⚠️ Отклонен файл с неразрешенным расширением: %s", cleanName)
				continue
			}

			file, openErr := fileHeader.Open()
			if openErr != nil {
				log.Printf("Ошибка открытия файла из формы: %v", openErr)
				continue
			}
			savePath := fmt.Sprintf("./data/uploads/%d_%d%s", time.Now().UnixNano(), count, ext)
			dst, createErr := os.Create(savePath)
			if createErr == nil {
				io.Copy(dst, file)
				dst.Close()
				imagePaths = append(imagePaths, savePath)
				count++
				log.Printf("Сохранено изображение: %s", savePath)
			} else {
				log.Printf("Ошибка сохранения файла на диск: %v", createErr)
			}
			file.Close()
		}
	}

	// Сохраняем тикет в БД
	if clientID != "" {
		saveMessage(clientID, email, "client", message)
	}

	if smtpHost != "" {
		go processEmails(email, tgUsername, message, imagePaths)
	}
	if tgBotToken != "" && tgChatID != "" {
		go sendTelegramNotification(clientID, email, tgUsername, message, imagePaths)
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

func processEmails(email, tgUsername, message string, imagePaths []string) {
	dialer := gomail.NewDialer(smtpHost, smtpPort, smtpUser, smtpPass)
	dialer.SSL = true

	tgInfo := "Не указан"
	if tgUsername != "" {
		tgInfo = tgUsername
	}

	// Письмо Администратору
	mAdmin := gomail.NewMessage()
	mAdmin.SetHeader("From", mAdmin.FormatAddress(smtpFrom, senderName))
	mAdmin.SetHeader("To", supportEmail)
	mAdmin.SetHeader("Reply-To", email)
	mAdmin.SetHeader("Subject", "Новое обращение в поддержку от "+email)

	safeEmail := html.EscapeString(email)
	safeTgInfo := html.EscapeString(tgInfo)
	safeMessage := html.EscapeString(message)

	bodyAdmin := fmt.Sprintf(`
		<h2>Новое обращение в службу поддержки</h2>
		<p><strong>Email клиента:</strong> %s</p>
		<p><strong>Telegram:</strong> %s</p>
		<hr>
		<h3>Суть проблемы:</h3>
		<p style="white-space: pre-wrap;">%s</p>
	`, safeEmail, safeTgInfo, safeMessage)

	mAdmin.SetBody("text/html", bodyAdmin)
	for _, p := range imagePaths {
		mAdmin.Attach(p)
	}

	if err := dialer.DialAndSend(mAdmin); err != nil {
		log.Printf("Ошибка отправки админу: %v\n", err)
	}

	// Автоответ Клиенту
	mClient := gomail.NewMessage()
	mClient.SetHeader("From", mClient.FormatAddress(smtpFrom, senderName))
	mClient.SetHeader("To", email)
	mClient.SetHeader("Subject", "Ваше обращение принято - "+senderName)

	bodyClient := fmt.Sprintf(`
		<h2>Здравствуйте!</h2>
		<p>Мы получили ваше обращение. Наш оператор рассмотрит его и ответит вам в ближайшее время.</p>
		<hr>
		<p><strong>Ваш текст обращения:</strong><br>%s</p>
		<hr>
		<p><small>С уважением, <strong>Поддержка KabebaVPN</strong></small></p>
	`, safeMessage)

	mClient.SetBody("text/html", bodyClient)
	if err := dialer.DialAndSend(mClient); err != nil {
		log.Printf("Ошибка отправки клиенту: %v\n", err)
	}
}

func sendTelegramNotification(clientID, email, tgUsername, message string, imagePaths []string) {
	tgInfo := "Не указан"
	if tgUsername != "" {
		tgInfo = tgUsername
	}
	text := fmt.Sprintf("🚨 Новый тикет в поддержку\n\nID: %s\nEmail: %s\nTG: %s\n\nПроблема:\n%s",
		clientID, email, tgInfo, message)

	if len(imagePaths) > 0 {
		// Первое фото отправляем с подписью
		sendTelegramPhoto(tgChatID, text, imagePaths[0])
		// Остальные без подписи
		for _, p := range imagePaths[1:] {
			sendTelegramPhoto(tgChatID, "", p)
		}
	} else {
		apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tgBotToken)
		payload := map[string]string{"chat_id": tgChatID, "text": text}
		jsonData, _ := json.Marshal(payload)
		http.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	}
}

func sendTelegramPhoto(chatID, caption, filePath string) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", tgBotToken)

	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("Не удалось открыть файл для TG: %v", err)
		return
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writer.WriteField("chat_id", chatID)
	writer.WriteField("caption", caption)
	part, _ := writer.CreateFormFile("photo", filePath)
	io.Copy(part, file)
	writer.Close()

	req, _ := http.NewRequest("POST", apiURL, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	client := &http.Client{Timeout: 30 * time.Second}
	client.Do(req)
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
	emailRegex := regexp.MustCompile(`Email:\s*([^\s\n]+)`)
	idRegex := regexp.MustCompile(`ID:\s*([^\s\n]+)`)

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

				// 🚨 ЗАЩИТА: проверяем, что отвечает именно авторизованный администратор (TG_CHAT_ID)
				if strconv.FormatInt(update.Message.Chat.ID, 10) != tgChatID {
					log.Printf("🚨 Предупреждение о безопасности: попытка ответа из чужого чата (ID: %d)", update.Message.Chat.ID)
					continue
				}

				originalText := update.Message.ReplyToMessage.Text
				if !strings.Contains(originalText, "Новый тикет в поддержку") {
					continue
				}

				matches := emailRegex.FindStringSubmatch(originalText)
				if len(matches) < 2 {
					sendTelegramMsg(update.Message.Chat.ID, "❌ Ошибка: не удалось найти Email клиента.")
					continue
				}
				targetEmail := matches[1]
				replyText := update.Message.Text

				var clientID string
				idMatches := idRegex.FindStringSubmatch(originalText)
				if len(idMatches) >= 2 {
					clientID = idMatches[1]
				}

				if clientID != "" {
					saveMessage(clientID, targetEmail, "support", replyText)
				}

				err := sendReplyEmail(targetEmail, replyText)
				if err != nil {
					sendTelegramMsg(update.Message.Chat.ID, fmt.Sprintf("❌ Ошибка при отправке на почту %s: %v", targetEmail, err))
				} else {
					sendTelegramMsg(update.Message.Chat.ID, fmt.Sprintf("✅ Ответ сохранён в историю и отправлен клиенту:\n%s", targetEmail))
				}
			}
		}
		resp.Body.Close()
	}
}

func sendReplyEmail(toEmail, replyText string) error {
	dialer := gomail.NewDialer(smtpHost, smtpPort, smtpUser, smtpPass)
	dialer.SSL = true

	m := gomail.NewMessage()
	m.SetHeader("From", m.FormatAddress(smtpFrom, senderName))
	m.SetHeader("To", toEmail)
	m.SetHeader("Subject", "Re: Обращение в поддержку - "+senderName)

	safeReply := html.EscapeString(replyText)
	body := fmt.Sprintf(`
		<h2>Ответ от службы поддержки</h2>
		<p style="white-space: pre-wrap; font-size: 16px;">%s</p>
		<hr>
		<p><small>С уважением,<br><strong>Поддержка KabebaVPN</strong></small></p>
	`, safeReply)

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
