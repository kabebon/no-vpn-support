function generateUUID() {
    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
        var r = Math.random() * 16 | 0, v = c == 'x' ? r : (r & 0x3 | 0x8);
        return v.toString(16);
    });
}

function getClientID() {
    let id = localStorage.getItem('support_client_id');
    if (!id) {
        id = generateUUID();
        localStorage.setItem('support_client_id', id);
    }
    return id;
}

const clientId = getClientID();
const chatMessages = document.getElementById('chat-messages');
const chatForm = document.getElementById('chat-form');
const msgInput = document.getElementById('message');
const emailInput = document.getElementById('email');
const statusIndicator = document.getElementById('status-indicator');

// Загружаем сохраненный Email, если есть
if (localStorage.getItem('support_client_email')) {
    emailInput.value = localStorage.getItem('support_client_email');
    emailInput.style.display = 'none'; // Скрываем, если уже введен
}

let ws;

function connectWS() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = new WebSocket(`${protocol}//${window.location.host}/api/ws?client_id=${clientId}`);

    ws.onopen = () => {
        statusIndicator.innerHTML = '🟢 Онлайн';
        statusIndicator.style.color = '#10b981';
    };

    ws.onmessage = (event) => {
        const data = JSON.parse(event.data);
        if (data.type === 'history' || data.type === 'message') {
            appendMessage(data.sender, data.message, data.created_at || new Date().toLocaleTimeString());
        }
    };

    ws.onclose = () => {
        statusIndicator.innerHTML = '🔴 Переподключение...';
        statusIndicator.style.color = '#ef4444';
        setTimeout(connectWS, 3000);
    };
    
    ws.onerror = (err) => {
        console.error("WS Error", err);
    };
}

function appendMessage(sender, text, time) {
    const div = document.createElement('div');
    div.className = `msg ${sender}`;
    
    // Преобразуем переносы строк в <br>
    const formattedText = text.replace(/\n/g, '<br>');
    
    div.innerHTML = `
        <div>${formattedText}</div>
        <span class="msg-time">${time}</span>
    `;
    
    chatMessages.appendChild(div);
    chatMessages.scrollTop = chatMessages.scrollHeight;
}

chatForm.addEventListener('submit', (e) => {
    e.preventDefault();
    
    const text = msgInput.value.trim();
    const email = emailInput.value.trim();
    
    if (!text) return;
    if (!email) {
        alert("Пожалуйста, укажите Email. Туда придет ответ, если вы закроете этот сайт.");
        emailInput.focus();
        return;
    }
    
    // Сохраняем email и скрываем поле
    localStorage.setItem('support_client_email', email);
    emailInput.style.display = 'none';

    // Отправляем на сервер
    if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({
            type: 'message',
            message: text,
            email: email,
            created_at: new Date().toLocaleTimeString()
        }));
        
        appendMessage('client', text, new Date().toLocaleTimeString());
        msgInput.value = '';
    } else {
        alert("Нет подключения к серверу. Попробуйте позже.");
    }
});

// Инициализация
connectWS();
