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
frame-realign-message =
    Пожалуйста, совместите маркеры на экране как можно точнее, если они расходятся, и нажмите готово.
frame-mover-finish-button-text = Готово
orders-adm-payment-cash-requested =
    Пользователь {$link} хочет оплатить вам за заказ наличными.
    Сумма к оплате: Br {$total}
    заказ на имя <i>{$name}</i>.
    Не отклоняй заказ сразу, а дождись разговора с клиентом, или напиши ему.
orders-adm-payment-proof-received =
    Пользователь {$link} прислал подтверждение оплаты заказа
    на сумму: {$total} ₽
    для зуконавта по имени <i>{$name}</i>. Требуется подтверждение.
    <b>Внимание</b>, не стоит помечать отсутствие оплаты раньше времени, лучше сначала удостовериться.
orders-admin-belarus = {$name}/{$region}
orders-back-button = Назад
orders-closed = Если надо посмотреть свои заказы или что-то изменить, можно снова использовать команду /orders
orders-close-button = Выйти
orders-edit-button = Изменить заказ
orders-finish-button-text = Готово
orders-message-list = Ваши заказы
orders-message-payed-where =
    Я получила PDF, который видимо является подтверждением оплаты. Я права?

orders-message-payment-options =
    Можно оплатить заказ на следующие реквизиты:

    Россия:
    Перевод по номеру <code>+79217826737</code> (Сбер, ВТБ)
    Ольга Тесла
    <code>{$rutotal}</code> ₽

    ⚠ <i>После оплаты, обязательно пришли мне подтверждение в формате <b>PDF</b>, другие форматы не принимаю!
    Если подтверждение в виде изображения, можешь сконвертировать его в PDF с помощью этого бота:</i> @ImageToPdfRobot

    Беларусь:
    Выбери один из контактов ниже и оплати лично:
    Br <code>{$total}</code>
orders-next-button-text = Далее
orders-new-button = Сделать заказ
orders-order-pay-button = Оплата
orders-order-button = {$created}|{$name}
orders-order-unpaid-button = (неоплачен){$created}|{$name}
orders-order-delete-button = Очистить заказ
orders-pay-cancel = Отменить
orders-payment-cash-requested =
    Свяжись с {$link} для личной встречи и оплаты.
    Сумма к оплате: Br {$total}
    заказ на имя <i>{$name}</i>.
    Администратор отметит заказ как оплаченный после получения оплаты.
orders-payed-button = Это подтверждение оплаты
orders-placeholder-first-name=Имя
orders-placeholder-last-name=Фамилия
orders-placeholder-patronymus=Отчество
orders-validity-error-first-name=Имя должно начинаться с заглавной буквы и содержать как минимум одну строчную
orders-validity-error-last-name=Фамилия должна начинаться с заглавной буквы и содержать как минимум одну строчную
undefined-state-error = Что-то пошло не так, попробуй ещё раз!
unsupported-message-error = Прости, я не понимаю этого сообщения.
max-assistant-messages-reached =  К сожалению, на сегодня лимит сообщений исчерпан. Приходи снова через 24 часа)
avatar-custom-crop = Поменять расположение
avatar-without-command = Хочешь еще одну аватарку? Загрузи ещё одно фото, и я сделаю) Кстати, теперь не нужно каждый раз присылать эту команду, просто присылай фотографию. 
something-went-wrong = Что-то пошло не так. Попробуй ещё.
food-adm-payment-proof-accept-button = ✅ Подтверждено
food-adm-payment-proof-confirmed = Заказ от пользователя {$link} для зуконавта по имени <i>{$name}</i> подтверждён.
food-adm-payment-proof-received =
    Пользователь {$link} прислал подтверждение оплаты питания
    на сумму: {$total} ₽
    для зуконавта по имени <i>{$name}</i>. Требуется подтверждение.
    <b>Внимание</b>, не стоит помечать отсутствие оплаты раньше времени, лучше сначала удостовериться.
food-adm-payment-proof-reject-button = ❌ Отказ
food-adm-payment-proof-rejected = Заказ от пользователя {$link} для зуконавта по имени <i>{$name}</i> был отклонён.
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
    <b>Сумма к оплате:</b> <code>{$total}</code> ₽
food-payment-instructions-proof =
    ⚠ Когда пройдет оплата заказа отправь сюда <u><b>квитанцию (чек)</b></u> об оплате, а не просто скрин. Его проверит наш администратор.
    Если пока не готов(а) прислать подтверждение, жми на кнопку отмены.
    Сюда можно будет снова вернуться.
food-payment-proof-cancelled = Ок. Теперь, придётся зайти в меню, чтобы снова отправить подтверждение оплаты.
food-payment-proof-confirmed =
    Администратор подтвердил заказ для зуконавта по имени <i>{$name}</i>.
    Ждём тебя на ZNS.
food-payment-proof-failed = Я ожидала подтверждение оплаты, а это не похоже на него. Чтобы снова отправить подтверждение оплаты, придётся зайти в меню.
food-payment-proof-rejected = 
    Админинстратор отменил заказ для зуконавта по имени <i>{$name}</i>.
    Можно заказать заново, или попробовать прислать другое подтверждение.
food-payment-proof-forwarded = Я переслала подтверждение администраторам. Как только они проверят, я вернусь с результатом.
food-remind-about-order =
    Я заметила, что у тебя есть заказ, который не оплачен.
    Если ты всё ещё хочешь получить этот заказ на марафоне, то тебе следует оплатить его и прислать мне чек.
    Иначе, заказ не будет сформирован и не будет доставлен.
    У тебя есть время вполть до 4го июня.
    Ну или если тебе не нужен этот заказ, можешь его отменить.

    Вот этот заказ:
food-rename-button = Переименовать
food-select-order =  Пожалуйста, ознакомься со своим заказом, или создай новый.
food-total = <b>Итого:</b> <code>{$total}</code> ₽
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
massage-booking-cancel-button = ❌ Отменить бронирование
massage-client-about =
    Тип массажа: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> минут.
    Вечеринка: <b>{$party}</b>
    Время: <b>{$time}</b>
    Специалист: {$specialist}

    Приходи <u>вовремя</u> ведь после тебя будет кто-то ещё. А если не можешь прийти — лучше заранее отменить.
    Приятного погружения!
massage-command-description = Запись на массаж
massage-create-button = 📝 Записаться
massage-deleted = Запись успешно удалена. Если надо изменить другую запись или создать новую: /massage
massage-edit-back-button = ⬅ Назад
massage-edit-cancel-button = 🚪 Выйти
massage-edit-choose-length = Выбери желаемую продолжительность сеанса:
massage-edit-choose-party-button = {$party}
massage-edit-error-too-many-massages = Не более {$max} массажей в день на одного клиента.
massage-edit-error-slot-timeout = Нельзя забронировать так поздно, выбери другой слот.
massage-edit-error-slot-unavailable = Кто-то только что зарезервировал этот слот, выбери другой.
massage-edit-length-button = {$icon} Br {$price} / {$minutes} минут.
massage-edit-next-button = ➡ Дальше
massage-edit-no-slots-available = Свободных слотов нет...
massage-edit-page = страница {$page} из {$leng}
massage-edit-page-next-button = Следующая ▶
massage-edit-page-previous-button = ◀ Предыдущая
massage-edit-select-specialists =
    Выбран массаж: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> минут.

    Здесь можно выбрать вечеринку, отфильтровать массажистов, чтобы найти свободные слоты в их расписании.
    По умолчанию, выбраны все ✅, но можно исключить некоторых ❌, нажав на их имя.
    После этого, выбирай понравившийся тебе слот из свободных.

    {$specialists}<i>{$filtered}{$error}</i>
massage-edit-select-specialists-filtered = Некоторые массажисты были убраны из списка, так как они не работают с массажами данной продолжительности.
massage-exit-button = 🚪 Выйти
massage-exited = Если ещё понадобится, можно всегда вызвать команду вновь: /massage
massage-notification-additional =
    Привет зуконавт!
    Ты записан на массаж:
    Тип массажа: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> минут.
    Вечеринка: <b>{$party}</b>
    Время: <b>{$time}</b>
    Специалист: {$specialist}

    Приходи <u>вовремя</u> ведь после тебя будет кто-то ещё. А если не можешь прийти — лучше заранее отменить.
    Приятного погружения!
massage-notification-prior-long =
    Привет зуконавт!
    Напоминаю, что у тебя есть запись на массаж через {$inminutes} минут, в <b>{$time}</b>:
    Тип массажа: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> минут.
    Специалист: {$specialist}

    Приходи <u>вовремя</u> ведь после тебя будет кто-то ещё. А если не можешь прийти — лучше заранее отменить.
    Приятного погружения!
massage-notification-prior =
    Привет зуконавт!
    Напоминаю, что у тебя есть запись на массаж через {$inminutes} минут, в <b>{$time}</b>:
    Тип массажа: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> минут.
    Специалист: {$specialist}

    Приходи <u>вовремя</u> ведь после тебя будет кто-то ещё. А если не можешь прийти — лучше заранее отменить.
    Приятного погружения!
massage-notification-toggle = {$pos ->
    [y] 🔔
    *[n] 🔕
}
massage-notifications-edit = Здесь можно включить или выключить напоминания:
massage-specialist-booking-cancelled =
    Пользователь <i>{$client}</i> <u>отменил</u> бронирование:
    Тип массажа: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> минут.
    Вечеринка: <b>{$party}</b>
    Время: <b>{$time}</b>

    Посмотреть весь список записавшихся или отключить уведомления можно по команде /massage
massage-specialist-clientlist = Вот список клиентов и их бронирований по вечеринкам:
massage-specialist-clientlist-button = 📃 Список клиентов
massage-specialist-failed-to-reserve = Не получилось зарезервировать моментальную бронь. Либо время слота истекло, либо кто-то зарезервировал массаж. Попробуй ещё раз.
massage-specialist-instantbook = Моментальная бронь:
massage-specialist-instantbook-button = {$icon} {$minutes}+5
massage-specialist-new-booking =
    Пользователь <i>{$client}</i> записался на массаж:
    Тип массажа: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> минут.
    Вечеринка: <b>{$party}</b>
    Время: <b>{$time}</b>
massage-specialist-no-party-or-slot = Не получилось определить вечеринку и слот — ошибка.
massage-specialist-notification-notify-bookings = Создано или удалено бронирование
massage-specialist-notification-notify-next = О грядущей сессии (за 5 минут)
massage-specialist-notification-soon =
    Через {$inminutes} минут, в <b>{$time}</b> следующий массаж.
    Пользователь <i>{$client}</i>:
    Тип массажа: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> минут.
massage-specialist-notifications-button = 🔔 Напоминания
massage-specialist-reserved =
    Успешно зарезервировано:
    Тип массажа: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> минут.
    Время: <b>{$time}</b>
massage-specialist-timetable-button = 📅 Расписание
massage-specialist-to-start-button = ⬅ На старт
massage-specialist-view =
    Пользователь: <i>{$client}</i>
    Тип массажа: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> минут.
    Вечеринка: <b>{$party}</b>
    Время: <b>{$time}</b>
massage-specialist-view-booking-button = 📝 К бронированию
massage-start-message =
    Кликни "Записаться", чтобы попасть на приём к массажисту или выбери свою текущую запись из списка, чтобы внести изменения, отменить или связаться со специалистом.
    <a href="https://t.me/zouknonstopchannel/182">Пост о массаже на ZNS</a>.
    
    Наши специалисты:
massage-successfully-created = Сессия массажа успешно забронирована.
massage-unfinished = Незавершённое бронирование
massage-your-boookings = Твои бронирования:
user-is-restricted = Часть действий была отключена.
