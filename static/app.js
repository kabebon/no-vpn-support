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
        const resp = await fetch('/api/config', { cache: 'no-store' });
        const cfg = await resp.json();
        const btn = document.getElementById('cabinet-btn');
        if (cfg && cfg.cabinet_url && cfg.cabinet_url.trim() !== "") {
            btn.href = cfg.cabinet_url.trim();
            btn.style.opacity = '1';
            btn.style.pointerEvents = 'auto';
            btn.title = 'Перейти в личный кабинет';
        } else {
            // Если в .env не указано, делаем кнопку активной, но выводим предупреждение при клике
            btn.style.opacity = '0.6';
            btn.style.pointerEvents = 'auto';
            btn.onclick = (e) => {
                if (btn.getAttribute('href') === '#' || !btn.getAttribute('href')) {
                    e.preventDefault();
                    alert('Ссылка на кабинет ещё не настроена администратором (параметр CABINET_URL в .env)');
                }
            };
        }
    } catch (e) {
        console.error('Ошибка загрузки конфига:', e);
    }
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
// Загрузка изображений — мультивыбор + превью + Drag&Drop
// ------------------------------------------------
let selectedFiles = []; // Массив файлов для отправки

const fileInput = document.getElementById('images');
const fileLabel = document.getElementById('file-label');

function addFiles(files) {
    const newImgs = Array.from(files).filter(file => file.type.startsWith('image/'));
    for (let file of newImgs) {
        if (!selectedFiles.find(f => f.name === file.name && f.size === file.size)) {
            if (selectedFiles.length >= 5) {
                alert('⚠️ Можно прикрепить максимум 5 скриншотов.');
                break;
            }
            selectedFiles.push(file);
        }
    }
    renderPreviews();
}

fileInput.addEventListener('change', function() {
    if (this.files && this.files.length > 0) {
        addFiles(this.files);
    }
});

// Drag & Drop
['dragenter', 'dragover'].forEach(eventName => {
    fileLabel.addEventListener(eventName, (e) => {
        e.preventDefault();
        e.stopPropagation();
        fileLabel.style.borderColor = '#10b981';
        fileLabel.style.background = 'rgba(16,185,129,0.08)';
    }, false);
});

['dragleave', 'drop'].forEach(eventName => {
    fileLabel.addEventListener(eventName, (e) => {
        e.preventDefault();
        e.stopPropagation();
        fileLabel.style.borderColor = '';
        fileLabel.style.background = '';
    }, false);
});

fileLabel.addEventListener('drop', (e) => {
    const dt = e.dataTransfer;
    if (dt && dt.files && dt.files.length > 0) {
        addFiles(dt.files);
    }
});

function renderPreviews() {
    const grid = document.getElementById('preview-grid');
    const labelText = document.getElementById('file-label-text');
    grid.innerHTML = '';

    if (selectedFiles.length === 0) {
        fileLabel.classList.remove('has-file');
        labelText.textContent = 'Нажмите или перетащите изображения сюда';
        return;
    }

    fileLabel.classList.add('has-file');
    if (selectedFiles.length >= 5) {
        labelText.textContent = `Достигнут лимит (5 из 5 скриншотов)`;
    } else {
        labelText.textContent = `Добавить ещё (выбрано: ${selectedFiles.length} из 5)`;
    }

    selectedFiles.forEach((file, index) => {
        const item = document.createElement('div');
        item.className = 'preview-item';

        const img = document.createElement('img');
        
        const reader = new FileReader();
        reader.onload = (e) => {
            img.src = e.target.result;
        };
        reader.readAsDataURL(file);

        const removeBtn = document.createElement('button');
        removeBtn.className = 'remove-btn';
        removeBtn.type = 'button';
        removeBtn.textContent = '×';
        removeBtn.title = 'Удалить изображение';
        removeBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            e.preventDefault();
            selectedFiles.splice(index, 1);
            renderPreviews();
        });

        item.appendChild(img);
        item.appendChild(removeBtn);
        grid.appendChild(item);
    });
}

// ------------------------------------------------
// Селектор способа регистрации (Telegram / Email)
// ------------------------------------------------
function initRegistrationSelector() {
    const radios = document.querySelectorAll('input[name="reg_type"]');
    const fieldTg = document.getElementById('field-telegram');
    const fieldEmail = document.getElementById('field-email-reg');
    const cardTg = document.getElementById('card-reg-tg');
    const cardEmail = document.getElementById('card-reg-email');
    const copyBtn = document.getElementById('copy-email-btn');

    function updateView(selectedVal) {
        if (selectedVal === 'telegram') {
            if (fieldTg) fieldTg.style.display = 'block';
            if (fieldEmail) fieldEmail.style.display = 'none';
            if (cardTg) {
                cardTg.style.background = 'rgba(59, 130, 246, 0.08)';
                cardTg.style.borderColor = '#3b82f6';
                cardTg.querySelector('span').style.color = '#1d4ed8';
            }
            if (cardEmail) {
                cardEmail.style.background = 'rgba(0, 0, 0, 0.02)';
                cardEmail.style.borderColor = 'rgba(0, 0, 0, 0.1)';
                cardEmail.querySelector('span').style.color = '#555';
            }
        } else {
            if (fieldTg) fieldTg.style.display = 'none';
            if (fieldEmail) fieldEmail.style.display = 'block';
            if (cardEmail) {
                cardEmail.style.background = 'rgba(59, 130, 246, 0.08)';
                cardEmail.style.borderColor = '#3b82f6';
                cardEmail.querySelector('span').style.color = '#1d4ed8';
            }
            if (cardTg) {
                cardTg.style.background = 'rgba(0, 0, 0, 0.02)';
                cardTg.style.borderColor = 'rgba(0, 0, 0, 0.1)';
                cardTg.querySelector('span').style.color = '#555';
            }
        }
    }

    radios.forEach(radio => {
        radio.addEventListener('change', (e) => {
            updateView(e.target.value);
            saveCurrentForm();
        });
    });

    if (copyBtn) {
        copyBtn.addEventListener('click', () => {
            const mainEmail = document.getElementById('email').value;
            const regEmailInput = document.getElementById('reg_email');
            if (mainEmail && regEmailInput) {
                regEmailInput.value = mainEmail;
                saveCurrentForm();
            } else {
                alert('Сначала укажите ваш основной Email для получения ответа выше 👆');
            }
        });
    }

    const checked = document.querySelector('input[name="reg_type"]:checked');
    if (checked) updateView(checked.value);
}

// ------------------------------------------------
// Отправка формы
// ------------------------------------------------
document.getElementById('support-form').addEventListener('submit', async (e) => {
    e.preventDefault();

    const email = document.getElementById('email').value;
    const msg = document.getElementById('message').value;
    const submitBtn = document.getElementById('submit-btn');
    const alertBox = document.getElementById('form-alert');

    // Определяем данные аккаунта в зависимости от выбранного способа регистрации
    const regTypeEl = document.querySelector('input[name="reg_type"]:checked');
    const regType = regTypeEl ? regTypeEl.value : 'telegram';
    let accountInfo = '';

    if (regType === 'telegram') {
        const tgVal = document.getElementById('tg_username').value.trim();
        accountInfo = tgVal ? `[Регистрация в Telegram]: ${tgVal}` : '[Telegram: не указан / скрыт]';
    } else {
        const regEmailVal = document.getElementById('reg_email').value.trim();
        accountInfo = regEmailVal ? `[Регистрация по Email]: ${regEmailVal}` : `[Регистрация по Email]: ${email}`;
    }

    submitBtn.disabled = true;
    submitBtn.textContent = 'Отправка...';
    alertBox.style.display = 'none';

    try {
        const formData = new FormData();
        formData.append('client_id', clientId);
        formData.append('email', email);
        formData.append('tg_username', accountInfo);
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
            clearSavedMessage(); // Очищаем сохраненное сообщение после успешной отправки
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
// Автосохранение введенных данных
// ------------------------------------------------
const STORAGE_FORM_KEY = 'kabeba_support_form_save';

function saveCurrentForm() {
    try {
        const checkedReg = document.querySelector('input[name="reg_type"]:checked');
        const currentData = {
            email: document.getElementById('email') ? document.getElementById('email').value : '',
            tg_username: document.getElementById('tg_username') ? document.getElementById('tg_username').value : '',
            reg_email: document.getElementById('reg_email') ? document.getElementById('reg_email').value : '',
            message: document.getElementById('message') ? document.getElementById('message').value : '',
            reg_type: checkedReg ? checkedReg.value : 'telegram'
        };
        localStorage.setItem(STORAGE_FORM_KEY, JSON.stringify(currentData));
    } catch (e) {}
}

function setupAutoSave() {
    const fields = ['email', 'tg_username', 'reg_email', 'message'];
    try {
        const saved = JSON.parse(localStorage.getItem(STORAGE_FORM_KEY) || '{}');
        fields.forEach(id => {
            const el = document.getElementById(id);
            if (el && saved[id] !== undefined) {
                el.value = saved[id];
            }
        });
        if (saved.reg_type) {
            const radio = document.querySelector(`input[name="reg_type"][value="${saved.reg_type}"]`);
            if (radio) radio.checked = true;
        }
    } catch (e) {}

    fields.forEach(id => {
        const el = document.getElementById(id);
        if (el) {
            el.addEventListener('input', saveCurrentForm);
        }
    });
}

function clearSavedMessage() {
    try {
        const saved = JSON.parse(localStorage.getItem(STORAGE_FORM_KEY) || '{}');
        delete saved.message; // Оставляем контактные данные и выбор типа регистрации
        localStorage.setItem(STORAGE_FORM_KEY, JSON.stringify(saved));
    } catch (e) {}
}

// ------------------------------------------------
// Инициализация
// ------------------------------------------------
loadConfig();
setupAutoSave();
initRegistrationSelector();
