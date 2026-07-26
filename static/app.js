document.addEventListener('DOMContentLoaded', () => {
    const form = document.getElementById('support-form');
    const submitBtn = document.getElementById('submit-btn');
    const alertBox = document.getElementById('form-alert');

    form.addEventListener('submit', async (e) => {
        e.preventDefault();
        
        // Reset alert
        alertBox.className = 'alert';
        alertBox.textContent = '';

        const formData = new FormData(form);
        const data = {
            email: formData.get('email'),
            tg_username: formData.get('tg_username'),
            message: formData.get('message')
        };

        // Basic frontend validation
        if (!data.email || !data.message) {
            showAlert('Заполните обязательные поля (Email и Проблема)', 'error');
            return;
        }

        try {
            submitBtn.disabled = true;
            submitBtn.textContent = 'Отправка...';

            const response = await fetch('/api/ticket', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(data)
            });

            const result = await response.json();

            if (response.ok) {
                showAlert('Ваше обращение успешно отправлено! Мы ответим вам на Email.', 'success');
                form.reset();
            } else {
                showAlert(result.error || 'Произошла ошибка при отправке', 'error');
            }
        } catch (error) {
            showAlert('Ошибка сети. Проверьте подключение к интернету.', 'error');
        } finally {
            submitBtn.disabled = false;
            submitBtn.textContent = 'Отправить заявку';
        }
    });

    function showAlert(message, type) {
        alertBox.textContent = message;
        alertBox.className = `alert ${type}`;
    }
});
