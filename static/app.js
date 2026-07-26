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

// ------------------------------------------------
// Навигация между секциями
// ------------------------------------------------
function showForm(e) {
    if (e) e.preventDefault();
    document.getElementById('form-section').style.display = '';
    document.getElementById('history-section').style.display = 'none';
}

function showHistory(e) {
    if (e) e.preventDefault();
    document.getElementById('form-section').style.display = 'none';
    document.getElementById('history-section').style.display = '';
    loadHistory();
}

// ------------------------------------------------
// Загрузка конфига (cabinet_url)
// ------------------------------------------------
async function loadConfig() {
    try {
        const resp = await fetch('/api/config');
        const cfg = await resp.json();
        if (cfg.cabinet_url) {
            const btn = document.getElementById('cabinet-btn');
            btn.href = cfg.cabinet_url;
            btn.style.opacity = '1';
            btn.style.pointerEvents = 'auto';
        }
    } catch (e) {}
}

// ------------------------------------------------
// История обращений
// ------------------------------------------------
async function loadHistory() {
    try {
        const response = await fetch(`/api/history?client_id=${clientId}`);
        const history = await response.json();
        const historyList = document.getElementById('history-list');
        const emptyBox = document.getElementById('history-empty');

        historyList.innerHTML = '';

        if (!history || history.length === 0) {
            emptyBox.style.display = 'block';
            return;
        }

        emptyBox.style.display = 'none';

        history.forEach(msg => {
            const div = document.createElement('div');
            div.className = `history-msg ${msg.sender}`;

            const senderLabel = msg.sender === 'client' ? 'Вы написали:' : '✅ Ответ поддержки:';
            const timeStr = msg.created_at ? msg.created_at.replace('T', ' ').replace('Z', '') : '';

            div.innerHTML = `
                <div class="history-sender ${msg.sender}">${senderLabel}</div>
                <div style="white-space: pre-wrap; font-size: 14px; line-height: 1.6;">${escapeHtml(msg.message)}</div>
                <div class="history-time">${timeStr}</div>
            `;
            historyList.appendChild(div);
        });

        // Прокручиваем вниз
        historyList.scrollTop = historyList.scrollHeight;
    } catch (e) {
        console.error('Ошибка загрузки истории', e);
    }
}

function escapeHtml(text) {
    return text
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}

// ------------------------------------------------
// Загрузка изображений — мультивыбор + превью
// ------------------------------------------------
let selectedFiles = []; // Массив файлов, которые будем отправлять

document.getElementById('images').addEventListener('change', function() {
    const newFiles = Array.from(this.files);
    newFiles.forEach(file => {
        if (!selectedFiles.find(f => f.name === file.name && f.size === file.size)) {
            selectedFiles.push(file);
        }
    });
    // Сбрасываем input, чтобы можно было добавить ещё раз те же файлы
    this.value = '';
    renderPreviews();
});

function renderPreviews() {
    const grid = document.getElementById('preview-grid');
    const label = document.getElementById('file-label');
    const labelText = document.getElementById('file-label-text');
    grid.innerHTML = '';

    if (selectedFiles.length === 0) {
        label.classList.remove('has-file');
        labelText.textContent = 'Нажмите или перетащите изображения сюда';
        return;
    }

    label.classList.add('has-file');
    labelText.textContent = `Добавить ещё (выбрано: ${selectedFiles.length})`;

    selectedFiles.forEach((file, index) => {
        const item = document.createElement('div');
        item.className = 'preview-item';

        const img = document.createElement('img');
        img.src = URL.createObjectURL(file);
        img.alt = file.name;

        const removeBtn = document.createElement('button');
        removeBtn.className = 'remove-btn';
        removeBtn.type = 'button';
        removeBtn.textContent = '×';
        removeBtn.title = 'Удалить';
        removeBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            selectedFiles.splice(index, 1);
            renderPreviews();
        });

        item.appendChild(img);
        item.appendChild(removeBtn);
        grid.appendChild(item);
    });
}

// ------------------------------------------------
// Отправка формы
// ------------------------------------------------
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
        const formData = new FormData();
        formData.append('client_id', clientId);
        formData.append('email', email);
        formData.append('tg_username', tg);
        formData.append('message', msg);
        // Добавляем все выбранные изображения
        selectedFiles.forEach(file => {
            formData.append('images', file);
        });

        const response = await fetch('/api/ticket', {
            method: 'POST',
            body: formData
        });

        const data = await response.json();

        if (response.ok) {
            alertBox.className = 'alert alert-success';
            alertBox.textContent = '✅ Заявка отправлена! Ответ придёт на вашу почту.';
            alertBox.style.display = 'block';
            document.getElementById('message').value = '';
            // Сбрасываем файлы и превью
            selectedFiles = [];
            renderPreviews();
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

// ------------------------------------------------
// Инициализация
// ------------------------------------------------
loadConfig();
