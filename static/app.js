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

// Загрузка истории
async function loadHistory() {
    try {
        const response = await fetch(`/api/history?client_id=${clientId}`);
        const history = await response.json();
        
        if (history && history.length > 0) {
            document.getElementById('history-section').style.display = 'block';
            const historyList = document.getElementById('history-list');
            historyList.innerHTML = '';
            
            history.forEach(msg => {
                const card = document.createElement('div');
                card.className = 'card';
                card.style.padding = '16px';
                
                const title = document.createElement('h4');
                title.style.marginBottom = '8px';
                
                if (msg.sender === 'client') {
                    title.textContent = 'Вы написали:';
                    title.style.color = '#3b82f6';
                } else {
                    title.textContent = 'Ответ поддержки:';
                    title.style.color = '#10b981';
                }
                
                const text = document.createElement('p');
                text.className = 'text-body';
                text.style.whiteSpace = 'pre-wrap';
                text.textContent = msg.message;
                
                const time = document.createElement('small');
                time.className = 'text-caption text-hollow';
                time.style.display = 'block';
                time.style.marginTop = '8px';
                time.textContent = msg.created_at;
                
                card.appendChild(title);
                card.appendChild(text);
                card.appendChild(time);
                historyList.appendChild(card);
            });
        }
    } catch (e) {
        console.error('Ошибка загрузки истории', e);
    }
}

// Загружаем при открытии
loadHistory();

document.getElementById('support-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    
    const email = document.getElementById('email').value;
    const tg = document.getElementById('tg_username').value;
    const msg = document.getElementById('message').value;
    const submitBtn = document.getElementById('submit-btn');
    const alertBox = document.getElementById('form-alert');
    
    submitBtn.disabled = true;
    submitBtn.textContent = 'Отправка...';
    alertBox.style.display = 'none';
    
    try {
        const response = await fetch('/api/ticket', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                client_id: clientId,
                email: email,
                tg_username: tg,
                message: msg
            })
        });
        
        const data = await response.json();
        
        if (response.ok) {
            alertBox.className = 'alert alert-success';
            alertBox.textContent = 'Заявка успешно отправлена! Ответ придет на вашу почту.';
            alertBox.style.display = 'block';
            document.getElementById('message').value = '';
            
            // Перезагружаем историю
            setTimeout(loadHistory, 1000);
        } else {
            alertBox.className = 'alert alert-error';
            alertBox.textContent = data.error || 'Произошла ошибка при отправке';
            alertBox.style.display = 'block';
        }
    } catch (err) {
        alertBox.className = 'alert alert-error';
        alertBox.textContent = 'Ошибка сети. Попробуйте позже.';
        alertBox.style.display = 'block';
    } finally {
        submitBtn.disabled = false;
        submitBtn.textContent = 'Отправить заявку';
    }
});
