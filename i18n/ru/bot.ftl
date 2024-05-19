auth-authorize = ✔ Авторизовать
auth-authorized = Запрос авторизован
auth-cancelled = Запрос отменён
auth-decline = ❌ Отклонить
auth-declined = Запрос отклонён
auth-request =
    Кто-то запросил авторизацию для ZNS бота с сайта = {$origin}
    Его IP: {$ip}
    Его юзер-агент: {$useragent}
start-message =
    Привет, зуконавт! Я помогу тебе создать красивую аватарку, для этого пришли мне своё лучшее фото, пожалуйста!
cancel-command = Отмена
cover-caption-message =
    ❗️Рекомендую поставить новую аватарку в Вк вместе с этой обложкой профиля.
frame-mover-help-desktop =
    Фото можно перемещать мышкой, для вращения зажми кнопку Shift.
    Для масштабирования используй прокрутку.
frame-mover-help-mobile =
    Фото можно перемещать одним касанием. Двумя — вращать и масштабировать.
    Если вместо перемещения фото сворачивается окно, то попробуй начать с движения вверх.
frame-mover-help-unified =
    Фото можно перемещать, вращать и масштабировать.
    Для масштабирования или вращения мышкой используй прокрутку и кнопку Shift.
frame-mover-finish-button-text = Готово
undefined-state-error = Что-то пошло не так, попробуй ещё раз!
unsupported-message-error = Прости, я не понимаю этого сообщения.
max-assistant-messages-reached =  К сожалению, на сегодня лимит сообщений исчерпан. Приходи снова через 24 часа)
avatar-custom-crop = Поменять расположение
avatar-without-command = Хочешь еще одну аватарку? Загрузи ещё одно фото, и я сделаю) Кстати, теперь не нужно каждый раз присылать эту команду, просто присылай фотографию. 
something-went-wrong = Что-то пошло не так. Попробуй ещё.
food-adm-payment-proof-accept-button = ✅ Подтверждено
food-adm-payment-proof-confirmed = Питание от пользователя {$link} для зуконавта по имени <i>{$name}</i> подтверждено.
food-adm-payment-proof-received =
    Пользователь {$link} прислал подтверждение оплаты питания
    на сумму: {$total} ₽
    для зуконавта по имени <i>{$name}</i>. Требуется подтверждение.
    <b>Внимание</b>, не стоит помечать отсутствие оплаты раньше времени, лучше сначала удостовериться.
food-adm-payment-proof-reject-button = ❌ Отказ
food-adm-payment-proof-rejected = Питание от пользователя {$link} для зуконавта по имени <i>{$name}</i> было отклонено.
food-back-button = К началу.
food-cancel-order-button = Отменить заказ.
food-command-description = Твой космический пайёк на ZNS.
food-confirm-payment-button = Оплатить заказ
food-created-write-for-who = Твой заказ сохранён. Напиши своё имя, чтобы мы могли идентифицировать твой заказ и выдать его тебе.
food-day-wednesday = Среда
food-day-friday = Пятница
food-day-saturday = Суббота
food-day-sunday = Воскресенье
food-edit-order-button = Изменить заказ.
food-meal-lunch = обед
food-meal-dinner = ужин
food-name-update-cancelled = Имя зуконавта не изменено.
food-name-updated = Новое имя зуконавта <i>{$name}</i> сохранено.
food-new-order-button = Создать новый
food-no-orders-yet = 
    Для полного погружения зуконавту необходимо полноценное питание!
    Жми на кнопку и выбирай себе обед и/или ужин.
food-order-button = {$created}|{$name}
food-order-for = Заказ для зуконавта по имени <b>{$name}</b>
food-order-message-begin = Вот твой заказ. Здесь ты можешь его просмотреть, или изменить.
food-order-saved = Твой заказ сохранён, теперь можно приступить к оплате.
food-payment-instructions =
    Для оплаты, необходимо сделать перевод на Сбер по номеру
    <code>+79175295923</code>
    Получатель: <i>Ушакова Дарья Евгеньевна</i>.
food-payment-instructions-proof =
    ⚠ Когда пройдет оплата заказа отправь сюда <u><b>квитанцию (чек)</b></u> об оплате, а не просто скрин. Его проверит наш администратор.
    Если пока не готов(а) прислать подтверждение, жми на кнопку отмены.
    Сюда можно будет снова вернуться.
food-payment-proof-cancelled = Ок. Теперь, придётся зайти в меню, чтобы снова отправить подтверждение оплаты.
food-payment-proof-confirmed =
    Администратор подтвердил заказ на питание для зуконавта по имени <i>{$name}</i>.
    Ждём тебя на ZNS.
food-payment-proof-failed = Я ожидала подтверждение оплаты, а это не похоже на него. Чтобы снова отправить подтверждение оплаты, придётся зайти в меню.
food-payment-proof-rejected = 
    Админинстратор отменил заказ на питание для зуконавта по имени <i>{$name}</i>.
    Можно заказать заново, или попробовать прислать другое подтверждение.
food-payment-proof-forwarded = Я переслала подтверждение администраторам. Как только они проверят, я вернусь с результатом.
food-rename-button = Переименовать
food-select-order =  Пожалуйста, ознакомься со своим заказом, или создай новый.
food-total = <b>Итого:</b> <code>{$total}</code>
food-write-for-who = Укажи полные <b>имя</b> и <b>фамилию</b> зуконавта, для которого будет этот заказ.
dow-short =
    {$dow ->
        [0] пн
        [1] вт
        [2] ср
        [3] чт
        [4] пт
        [5] сб
        *[6] вс
    }
dow-long =
    {$dow ->
        [0] понедельник
        [1] вторник
        [2] среда
        [3] четверг
        [4] пятница
        [5] суббота
        *[6] воскресенье
    }
massage-create-button = 📝 Записаться
massage-edit-back-button = ⬅ Назад
massage-edit-cancel-button = ❌ Выйти
massage-edit-choose-day = Выбери вечеринку:
massage-edit-choose-length =
    Выбрана вечеринка: {$party}
    Выбери желаемую длину массажа:
massage-edit-choose-party-button = {$party}
massage-edit-length-button = {$icon} {$price} ₽ / {$minutes} минут.
massage-edit-next-button = ➡ Дальше
massage-edit-select-specialists =
    Выбрана вечеринка: {$party}
    Выбран массаж: {$durationicon} <b>{$price}</b> ₽ / <b>{$duration}</b> минут.

    Здесь можно отфильтровать массажистов, чтобы найти свободные слоты в их расписании.
    По умолчанию, выбраны все ✅, но можно исключить некоторых ❌, нажав на их имя.
    Когда закончишь, жми <b>{massage-edit-next-button}</b>

    {$specialists}

    {$filtered}
massage-edit-select-specialists-filtered = Некоторые массажисты были убраны из списка, так как они не работают с массажами данной продолжительности.
massage-exit-button = ❌ Выйти
massage-exited = Если ещё понадобится, можно всегда вызвать команду вновь: /massage
massage-specialist-clientlist-button = 📃 Список клиентов
massage-specialist-instantbook = Моментальная бронь:
massage-specialist-instantbook-button = {$icon} {$minutes}+5
massage-specialist-notifications-button = 🔔 Напоминания
massage-specialist-timetable-button = 📅 Расписание
massage-start-message =
    Кликни "Записаться", чтобы попасть на приём к массажисту или выбери свою текущую запись из списка, чтобы внести изменения, отменить или связаться со специалистом.
    <a href="https://t.me/zouknonstopchannel/182">Пост о массаже на ZNS</a>.
    
    Наши специалисты:
massage-unfinished = Незавершённое бронирование
massage-your-boookings = Твои бронирования: