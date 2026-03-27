avatar-no-face = Лицо на изображении не определено. Попробуй с другим изображением, например отодвинь телефон подальше от лица или поближе, или держи его прямее.
avatar-no-role = Не могу создать аватарку, потому что ты не зарегистрирован на ближайший ZNS. Пожалуйста, зарегистрируйся помощью команды /passes.
avatar-processing =
    Ваше фото обрабатывается...
    Это может занять некоторое время.
avatar-disabled = Создание аватарок временно отключено.
avatar-choose-method = Для экипажа Зукериона доступны персональные иллюминаторы. Самые смелые зуконавты могут выйти в открытый космос, для этого необходимо будет надеть скафандр.
avatar-method-simple = 🪞 Иллюминатор
avatar-method-detailed = 🧑‍🚀 Скафандр
avatar-cancel-button = ❌ Отменить
avatar-error-generic = Что-то пошло не так. Попробуйте еще раз.
avatar-cancelled-message = Создание аватарки отменено.
avatar-processing-initial = Обрабатываю ваш запрос...
avatar-error-file-expired-or-missing = К сожалению, файл не найден или срок его действия истек. Пожалуйста, попробуйте загрузить его снова.
avatar-custom-crop = Поменять расположение
avatar-simple-caption = Добро пожаловать на новый оборот Колеса Сансары!
avatar-error-frame-missing = Ошибка: файл рамки для простого аватара отсутствует. Обратитесь к администратору.
avatar-error-config-role = Ошибка: конфигурация для вашей роли отсутствует или неполна. Обратитесь к администратору.

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
    <b>Привет, Зуконавт!</b> Я — <b>ЗиНуСя</b>, твой цифровой помощник в мире удобства и технологий.
    Всегда на связи, чтобы оформить пасс, заказать горячее питание, записать на массаж,
    создать аватарку и многое другое — если эти функции сейчас доступны.
    Хочешь узнать, что я могу для тебя сделать прямо сейчас?
    <b>Жми на синюю кнопку <code>Меню</code></b>, и я покажу все свои возможности!
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
orders-command-description = Заказ еды, транспорта и активностей.
orders-edit-button = Изменить заказ
orders-finish-button-text = Готово
orders-message-list = Ваши заказы
orders-message-paid-where =
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
orders-paid-button = Это подтверждение оплаты
orders-placeholder-first-name=Имя
orders-placeholder-last-name=Фамилия
orders-placeholder-patronymus=Отчество
orders-validity-error-first-name=Имя должно начинаться с заглавной буквы и содержать как минимум одну строчную
orders-validity-error-last-name=Фамилия должна начинаться с заглавной буквы и содержать как минимум одну строчную
undefined-state-error = Что-то пошло не так, попробуй ещё раз!
unsupported-message-error = Прости, я не понимаю этого сообщения.
max-assistant-messages-reached =  К сожалению, на сегодня лимит сообщений исчерпан. Приходи снова через 24 часа)
avatar-without-command = Хочешь еще одну аватарку? Загрузи ещё одно фото, и я сделаю) Кстати, теперь не нужно каждый раз присылать эту команду, просто присылай фотографию.
something-went-wrong = Что-то пошло не так. Попробуй ещё.

food-adm-payment-proof-accept-button = ✅ Подтверждено
food-adm-payment-proof-confirmed = Заказ от пользователя {$link} для зуконавта по имени <i>{$name}</i> подтверждён.
food-adm-payment-proof-received =
    Пользователь {$link} отправил подтверждение оплаты для своего заказа еды.
    Общая сумма заказа: {$total} ₽. Требуется подтверждение.
    <b>Внимание</b>, не отклоняйте подтверждение сразу, подождите немного и сначала попытайтесь найти платеж.
food-adm-payment-proof-reject-button = ❌ Отказ
food-adm-payment-proof-rejected = Заказ от пользователя {$link} для зуконавта по имени <i>{$name}</i> был отклонён.
food-adm-payment-accepted-msg = Оплата заказа на сумму {$total} ₽ от пользователя {$link} принята.
food-adm-payment-already-processed-or-error = Заказ уже обработан или произошла ошибка.
food-adm-payment-rejected-msg = Оплата заказа на сумму {$total} ₽ от пользователя {$link} отклонена.

food-back-button = К началу.
food-cancel-order-button = Отменить заказ.
food-command-description = Твой космический паёк на ZNS.
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
food-order-unexpected-state = Заказ находится в неожиданном состоянии для проведения оплаты.
food-payment-instructions =
    Для оплаты, необходимо сделать перевод на Сбер по номеру
    <code>+79175295923</code>
    Получатель: <i>Ушакова Дарья Евгеньевна</i>.
    <b>Сумма к оплате:</b> <code>{$total}</code> ₽
food-payment-instructions-proof =
    Сюда можно будет снова вернуться.
food-payment-proof-cancelled = Отправка подтверждения оплаты отменена. Вы можете попробовать снова или управлять своим заказом с помощью команды /food.
food-payment-proof-confirmed =
    Администратор подтвердил заказ для зуконавта по имени <i>{$name}</i>.
    Ждём тебя на ZNS.
food-payment-proof-failed = Я ожидала подтверждение оплаты, а это не похоже на него. Чтобы снова отправить подтверждение оплаты, придётся зайти в меню.
food-payment-proof-rejected = 
    Админинстратор отменил заказ для зуконавта по имени <i>{$name}</i>.
    Можно заказать заново, или попробовать прислать другое подтверждение.
food-payment-admin-desc = Оплатить: {$adminLink}
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
food-button-create-order = 📝 Создать заказ
food-button-delete-order = 🗑️ Удалить заказ
food-button-edit-order = ✏️ Изменить заказ
food-button-pay = 💸 Оплатить заказ
food-button-view-order = 👁️ Посмотреть заказ
food-cannot-submit-proof-now = В данный момент невозможно отправить подтверждение оплаты для этого заказа.
food-no-order = У тебя пока нет активного заказа еды. Создай его с помощью кнопки ниже.
food-not-authorized-admin = У вас нет прав для этого действия.
food-order-already-paid = Этот заказ уже оплачен.
food-order-cannot-delete-paid-submitted = Этот заказ уже оплачен или подтверждение отправлено, и его нельзя удалить.
food-order-deleted-successfully = Заказ успешно удалён. Чтобы создать новый, используйте команду /food.
food-order-exists-not-complete = Сумма твоего текущего заказа {$total} ₽. Посмотри или измени заказ ниже.
food-order-exists-payable = Твой заказ (Сумма: {$total} ₽) готов к оплате. Если предыдущая попытка не удалась, можешь попробовать снова.
food-order-is-paid = Твой заказ оплачен. Сумма: {$total} ₽.
food-order-not-complete-for-payment = Заказ не завершён и пока не может быть оплачен.
food-order-not-found = Заказ не найден.
food-order-not-found-admin = Заказ не найден. (Режим администратора)
food-order-proof-already-submitted = Подтверждение оплаты для этого заказа уже было отправлено.
food-order-proof-submitted = Твоё подтверждение оплаты (Сумма: {$total} ₽) отправлено и ожидает проверки.
food-payment-admin-error = Ошибка связи с администратором платежей. Пожалуйста, попробуй ещё раз или обратись в поддержку.
food-payment-admins-not-configured = Платёжная система в данный момент недоступна. Пожалуйста, обратись в поддержку.
food-payment-proof-accepted = Оплата твоего заказа еды на сумму {$total} ₽ принята! Вы можете посмотреть детали вашего заказа с помощью команды /food.
food-payment-proof-forwarded = Твоё подтверждение оплаты отправлено на проверку.
food-payment-proof-rejected-retry = Твоё подтверждение оплаты для заказа еды на сумму {$total} ₽ было отклонено. Пожалуйста, попробуйте оплатить снова с помощью команды /food или обратитесь в поддержку.
food-payment-proof-timeout = Ты не отправил(а) подтверждение оплаты вовремя. Пожалуйста, попробуйте снова с помощью команды /food, если всё ещё хотите оплатить.
food-payment-proof-wrong-data =
    Я ожидала PDF-файл или изображение в качестве подтверждения оплаты, а получила что-то другое, чего не ожидала.
    Извините за недоразумение.
    Чтобы попробовать снова, используйте команду /food и отправьте PDF файл или картинку/скриншот.

food-payment-method =
    {$phoneSBP ->
        [nosbp] Амбассадор {$adminLink} принимает платежи только наличными или по предварительной договоренности. {$phoneContact ->
            [nophone] Напишите ему сообщение.
           *[other] Напишите ему сообщение или позвоните по номеру {$phoneContact}.
        }
        [paypal] Амбассадор {$adminLink} принимает платежи на PayPal, свяжитесь с ним для получения реквизитов и стоимости с учётом конверсии и комиссии.
        *[sbp] Оплату можно произвести через СБП по номеру телефона <code>{$phoneSBP}</code> ({$banks}, пожалуйста, не отправляйте на другие банки, ваши деньги могут не дойти!), контакт {$adminLink}.
    }

food-payment-request-callback-message =
    Сумма вашего заказа: <code>{$total}</code> ₽.
    {food-payment-method}
    Пожалуйста, подготовьте подтверждение оплаты (скриншот или PDF).

food-payment-request-waiting-message =
    Пожалуйста, отправьте <u>сюда, боту,</u> скриншот или PDF-файл в качестве подтверждения оплаты вашего заказа на сумму <code>{$total}</code> ₽.
    {food-payment-method}

food-payment-rejected = Ваш платеж за заказ на сумму {$total} ₽ был отклонен. Пожалуйста, свяжитесь с {$adminLink} для уточнения деталей.
food-notification-message-first =
    Привет, зуконавт!
    Последний день, когда можно оставить заказ еды на марафон — <b>4 июня</b> (до конца дня).
    Если ты хочешь, чтобы твой заказ был доставлен на марафон, тебе нужно оплатить его и прислать подтверждение оплаты до конца дня.
    Успей!
food-notification-message-last = {food-notification-message-first}
food-no-order-notification-first = {food-notification-message-first}
food-no-order-notification-last = {food-notification-message-last}
food-not-accepting-orders = Извините, вы больше не можете размещать заказы на еду для марафона. Крайний срок был 4 июня.

# Activities

activity-adm-payment-proof-received =
    Пользователь {$link} отправил подтверждение оплаты за свои активности.
    Сумма активностей: {$activitiesTotal} ₽. Требуется подтверждение.
    <b>Внимание</b>, не отмечайте подтверждение как отклонённое, подождите немного и попробуйте сначала найти платёж.
activity-adm-payment-proof-accept-button = ✅ Принять
activity-adm-payment-proof-reject-button = ❌ Отклонить
activity-adm-payment-accepted-msg =
    Подтверждение оплаты за активности {$link} на сумму <code>{$activitiesTotal}</code> ₽ подтверждено.
activity-adm-payment-rejected-msg =
    Подтверждение оплаты за активности {$link} на сумму <code>{$activitiesTotal}</code> ₽ отклонено.

activity-select-message =
    На Пикнике Zouk Non Stop будет несколько практик. На некоторые из них требуется предварительная запись, даже если у тебя фулл-пасс и они входят в его стоимость.
    Отметь, на какие из них ты хочешь записаться, переключая кнопки ниже между ☑️ и ❌.

    Стоимость:
    🌟<code>2500 ₽</code> - вечеринка + практики
    🌟<code>2000 ₽</code> - только вечеринка
    🌟<code>2000 ₽</code> - практики без вечеринки
    🌟<code> 750 ₽</code> - йога отдельно
    🌟<code>1000 ₽</code> - какао церемония отдельно
    🌟<code>1000 ₽</code> - саундхилинг отдельно
    Вечеринка и практики входят в фулл-пасс, для остальных оплата на месте.
    Отметь, куда ты именно собираешься прийти:
activity-button-submit = ✅ Подтвердить выбор
activity-finished-message =
    Выбранные активности:
    {$open ->
        [True] ✅
        *[False] ❌
    } {activity-open}
    {$yoga ->
        [True] ✅
        *[False] ❌
    } {activity-yoga}
    {$cacao ->
        [True] ✅
        *[False] ❌
    } {activity-cacao}
    {$soundhealing ->
        [True] ✅
        *[False] ❌
    } {activity-soundhealing}

    {$needPayment ->
        [true] {activity-finished-message-need-payment}
        *[false]  
    }
activity-finished-message-need-payment = 
    Сумма заказа: <code>{$totalPrice}</code> ₽. (если нет фулл-пасса)
    {food-payment-method}
activity-button-pay = 💸 Оплатить активности
activity-button-exit = 🚪 Выйти
activity-message-exited = Если нужно увидеть или изменить свой выбор активностей, всегда можно использовать команду /activities.
activity-payment-request-callback-message =
    Вам нужно оплатить <code>{$totalPrice}</code> ₽ за выбранные активности.
    {food-payment-method}
    Прокрутите вниз, чтобы отправить подтверждение оплаты.
activity-payment-request-waiting-message =
    Вам нужно оплатить <code>{$totalPrice}</code> ₽ за выбранные активности.
    {food-payment-method}
    Пожалуйста, отправьте подтверждение оплаты в формате PDF или в виде изображения.
activity-payment-proof-timeout =
    Я не получила ваше подтверждение оплаты вовремя. Пожалуйста, используйте команду /activities, чтобы попробовать снова.
    Не волнуйтесь, ваш выбор не изменился.
activity-payment-proof-cancelled =
    Если нужно повторно отправить подтверждение оплаты или просмотреть свой выбор активностей, используйте команду /activities.
    Не волнуйтесь, ваш выбор не изменился.
activity-payment-proof-wrong-data =
    Я ожидала получить PDF файл или изображение в качестве подтверждения оплаты, но это сообщение, похоже, не соответствует этому.
    Извините за путаницу.
    Чтобы попробовать снова, пожалуйста, используйте команду /activities и отправьте PDF файл или изображение.
activity-payment-proof-forwarded =
    Я отправила ваше подтверждение оплаты нашим администраторам для проверки. Они рассмотрят его, и я скоро обновлю вас с результатом.
    Если вам нужно просмотреть свой выбор активностей в это время, используйте команду /activities.
activity-payment-proof-accepted =
    Подтверждение оплаты за выбранные активности на сумму <code>{$activitiesTotal}</code> ₽ подтверждено! Вы можете просмотреть свой выбор, используя команду /activities.
activity-payment-proof-rejected-retry =
    Ваше подтверждение оплаты за выбранные активности на сумму <code>{$activitiesTotal}</code> ₽ было отклонено. Вы можете попробовать оплатить снова, используя команду /activities, или обратиться в поддержку.


# Activity Names
activity-open = Вечеринка
activity-yoga = Фитнес йога
activity-cacao = Какао церемония
activity-soundhealing = Саундхилинг
activity-button-all = Пати + практики
activity-button-classes = Только практики
activities-command-description = Выбор активностей

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
massage-price = {$price}/{$priceRu}
massage-price-b = <b>{$price}</b> Бел.Руб / <b>{$priceRu}</b> Рос.Руб
massage-pd = {$durationicon} <b>{$duration}</b> минут / 💰 {massage-price-b}
massage-edit-length-button = {$icon}{$minutes}м./💰{massage-price}
massage-booking-cancel-button = ❌ Отменить бронирование
massage-client-about =
    Тип массажа: {massage-pd}.
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
massage-edit-next-button = ➡ Дальше
massage-edit-no-slots-available = Свободных слотов нет...
massage-edit-page = страница {$page} из {$leng}
massage-edit-page-next-button = Следующая ▶
massage-edit-page-previous-button = ◀ Предыдущая
massage-edit-select-slot =
    Выбран массаж: {massage-pd}.

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
    Тип массажа: {massage-pd}.
    Вечеринка: <b>{$party}</b>
    Время: <b>{$time}</b>
    Специалист: {$specialist}

    Приходи <u>вовремя</u> ведь после тебя будет кто-то ещё. А если не можешь прийти — лучше заранее отменить.
    Приятного погружения!
massage-notification-prior-long =
    Привет зуконавт!
    Напоминаю, что у тебя есть запись на массаж через {$inminutes} минут, в <b>{$time}</b>:
    Тип массажа: {massage-pd}.
    Специалист: {$specialist}

    Приходи <u>вовремя</u> ведь после тебя будет кто-то ещё. А если не можешь прийти — лучше заранее отменить.
    Приятного погружения!
massage-notification-prior =
    Привет зуконавт!
    Напоминаю, что у тебя есть запись на массаж через {$inminutes} минут, в <b>{$time}</b>:
    Тип массажа: {massage-pd}.
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
    Тип массажа: {massage-pd}.
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
    Тип массажа: {massage-pd}.
    Вечеринка: <b>{$party}</b>
    Время: <b>{$time}</b>
massage-specialist-no-party-or-slot = Не получилось определить вечеринку и слот — ошибка.
massage-specialist-notification-notify-bookings = Создано или удалено бронирование
massage-specialist-notification-notify-next = О грядущей сессии (за 5 минут)
massage-specialist-notification-soon =
    Через {$inminutes} минут, в <b>{$time}</b> следующий массаж.
    Пользователь <i>{$client}</i>:
    Тип массажа: {massage-pd}.
massage-specialist-notifications-button = 🔔 Напоминания
massage-specialist-reserved =
    Успешно зарезервировано:
    Тип массажа: {massage-pd}.
    Время: <b>{$time}</b>
massage-specialist-timetable-button = 📅 Расписание
massage-specialist-to-start-button = ⬅ На старт
massage-specialist-view =
    Пользователь: <i>{$client}</i>
    Тип массажа: {massage-pd}.
    Вечеринка: <b>{$party}</b>
    Время: <b>{$time}</b>
massage-specialist-view-booking-button = 📝 К бронированию
massage-start-message =
    Кликни "Записаться", чтобы попасть на приём к массажисту или выбери свою текущую запись из списка, чтобы внести изменения, отменить или связаться со специалистом.
    <a href="https://t.me/zouknonstopchannel/670">Пост о массаже на ZNS</a>.
    
    Наши специалисты:
massage-successfully-created = Сессия массажа успешно забронирована.
massage-unfinished = Незавершённое бронирование
massage-your-boookings = Твои бронирования:

passes-adm-payment-proof-accept-button = ✅ Подтверждено
passes-adm-payment-proof-reject-button = ❌ Отказ
passes-adm-pass-description =
    {$type ->
        [couple] парный пасс на {$eventTitleLong} для {passes-role} по имени <i>{$name}</i> и второго участника {$coupleLink} (<i>{$coupleName}</i>)
        *[solo] пасс на {$eventTitleLong} для {passes-role} по имени <i>{$name}</i>
    }
passes-adm-payment-proof-accepted =
    Оплата за {passes-adm-pass-description} подтверждена.
passes-adm-payment-proof-rejected =
    Оплата за {passes-adm-pass-description} отклонена.
passes-adm-payment-proof-received =
    Пользователь {$link} отправил оплату за {passes-adm-pass-description}.
    Стоимость пасса: {$price} ₽.
    Требуется подтверждение.
    <b>Внимание</b>, не стоит помечать отсутствие оплаты раньше времени, лучше сначала удостовериться.

passes-announce-user-registered = {$name} подал заявку на пасс {passes-role}!
passes-button-cancel = ❌ Отменить ⚠️
passes-button-change-name = 🏷 Изменить имя
passes-button-exit = 🚪 Выйти
passes-button-pay = 💸 Подтверждение оплаты
passes-command-description = Регистрация на ЗНС и управление ей
passes-pass-description =
    {$type ->
        [couple] Парный пасс на {$eventTitleLong} с вашей ролью {passes-role} и вторым участником {$coupleLink}
        *[solo] Пасс {passes-role} на {$eventTitleLong}
    }
passes-added-to-waitlist =
    Привет, <i>{$name}</i>!
    К сожалению, все пассы на данный момент распроданы.
    Тем не менее, вы добавлены в очередь на {passes-pass-description}. Ожидайте дальнейшего уведомления.
    
    Ваш амбассадор: {$adminLink}

    Вы можете:
    - Отменить своё место (обратите внимание, что это навсегда удалит вас из списка ожидания)
    - Изменить имя, связанное с вашим местом

    Как только пасс станет доступен, вам будет отправлено уведомление!
passes-pass-edit-waitlist=
    Привет, <i>{$name}</i>!
    Вы находитесь в списке ожидания на {passes-pass-description}.
    
    Ваш амбассадор: {$adminLink}

    Вы можете:
    - Отменить своё место (это удалит вас из списка ожидания навсегда)
    - Изменить имя, связанное с вашим местом

    Мы уведомим вас, как только пасс станет доступен!
passes-payment-method =
    {$phoneSBP ->
        [nosbp] Амбассадор {$adminLink} принимает платежи только наличными или по предварительной договоренности. {$phoneContact ->
            [nophone] Напишите ему сообщение.
           *[other] Напишите ему сообщение или позвоните по номеру {$phoneContact}.
        }
        [paypal] Амбассадор {$adminLink} принимает платежи на PayPal, свяжитесь с ним для получения реквизитов и стоимости с учётом конверсии и комиссии.
        *[sbp] Оплату можно произвести через СБП по номеру телефона <code>{$phoneSBP}</code> ({$banks}), контакт {$adminLink}.
    }
passes-pass-assigned =
    Привет, <i>{$name}</i>!
    Поздравляем! {passes-pass-description} только что был назначен вам.

    Этот пасс пока не является официально вашим. Вам необходимо провести оплату в течение 7 дней с момента назначения.
    Текущая цена: <code>{$price}</code> ₽. {passes-payment-method}

    Если вы не оплатите в течение этого периода или не отправите подтверждение оплаты, пасс будет предложен кому-то другому.
    Действуйте быстро, чтобы не потерять место!
passes-pass-free-assigned =
    Привет, <i>{$name}</i>!
    Поздравляем! {passes-pass-description} только что был назначен вам.

    Так как это бесплатный пасс, вам не нужно ничего платить.
    Вы можете изменить имя, связанное с пассом, если это необходимо.
    Если вам нужно отменить пасс, пожалуйста, свяжитесь с администратором.
passes-pass-edit-assigned=
    Привет, <i>{$name}</i>!
    {passes-pass-description} был назначен вам.

    Чтобы забрать его, вам необходимо завершить оплату в течение 7 дней с момента назначения.
    Текущая цена: <code>{$price}</code> ₽. {passes-payment-method}

    Вы можете использовать этот интерфейс для:
    - Изменения вашего имени
    - Загрузки подтверждения оплаты
    - Отмены пасса

    Пожалуйста, обратите внимание, что отмена или пропуск срока оплаты необратимы — пасс уйдёт кому-то ещё.
passes-pass-edit-waiting-for-couple=
    Привет, <i>{$name}</i>!
    Вы выбрали парный пасс в роли {passes-role} на {$eventTitleLong}.
    Ваше приглашение для {$coupleLink} выслано но пока без ответа.
    
    Ваш амбассадор: {$adminLink}
    
    Вы можете использовать этот интерфейс для:
    - Изменения вашего имени
    - Отмены пасса
    - Смены его на соло пасс
passes-pass-edit-paid=
    Привет, <i>{$name}</i>!
    Поздравляем! {passes-pass-description} теперь официально ваш, так как вы успешно оплатили его.
    Вы можете изменить имя, связанное с пассом, если это необходимо.
    Если вам нужно вернуть пасс и запросить возврат, пожалуйста, свяжитесь с администратором.
passes-pass-exit=
    Если вам нужно просмотреть ваш пасс, предоставить подтверждение оплаты или изменить имя, вы можете вернуться, вызвав команду /passes снова.
passes-pass-cancelled=
    Пасс отменён. Если вы хотите вернуть его, вы можете вызвать команду /passes в любое время.
passes-pass-cancel-failed =
    Отмена не удалась. Либо пасс уже был помечен как оплаченный, либо он уже был отменён.
passes-payment-request-callback-message =
    Вам нужно заплатить <code>{$price}</code> ₽.
    {passes-payment-method}
    Прокрутите вниз, чтобы отправить подтверждение оплаты.
passes-payment-request-waiting-message =
    Вам нужно заплатить <code>{$price}</code> ₽.
    {passes-payment-method}
    Пожалуйста, отправьте подтверждение оплаты в формате PDF или как изображение.
passes-payment-proof-timeout =
    Я не получила ваше подтверждение оплаты вовремя. Пожалуйста, используйте команду /passes, чтобы попробовать снова.
    Не переживайте, ваша позиция в списке ожидания не изменилась.
passes-payment-proof-cancelled =
    Если вам нужно повторно отправить подтверждение оплаты или просмотреть данные о вашем пассе, используйте команду /passes.
    Не переживайте, ваша позиция в списке ожидания не изменилась.
passes-payment-proof-accepted =
    Привет, <i>{$name}</i>!
    Поздравляем! {passes-pass-description} теперь официально ваш, так как ваше подтверждение оплаты было успешно подтверждено администратором.
    Вы можете просмотреть данные о вашем пассе, используя команду /passes.
passes-payment-proof-rejected =
    Привет, <i>{$name}</i>!
    К сожалению, ваше подтверждение оплаты было отклонено администратором.
    Вы можете просмотреть данные о вашем пассе или отправить другое подтверждение, используя команду /passes.
    Не ждите слишком долго — получите пасс, пока он ещё на вас!
passes-name-timeout =
    Я не получила ваше имя вовремя. Пожалуйста, используйте команду /passes, чтобы попробовать снова.
passes-payment-proof-wrong-data =
    Я ожидала PDF-файл или изображение в качестве подтверждения оплаты, а получила что-то другое, чего не ожидала.
    Извините за недоразумение.
    Чтобы попробовать снова, используйте команду /passes и отправьте PDF файл или картинку/скриншот.
passes-payment-proof-forwarded =
    Я отправила ваше подтверждение оплаты нашим администраторам для проверки. Они его рассмотрят, и я сообщу вам результат в ближайшее время.
    Если вам нужно просмотреть данные о вашем пассе, используйте команду /passes.
passes-sell-not-started =
    Пожалуйста, подождите, продажи ещё не начались.
passes-pass-create-start-message =
    Сначала мне нужно немного информации от вас.
    Прокрутите вниз и введите ваше полное имя, как указано в паспорте.
passes-pass-create-start-message-continue =
    Сначала мне нужно немного информации от вас.
    Прокрутите вниз, чтобы продолжить.
passes-legal-name-request-message =
    Сначала мне нужно немного информации от вас.
    Пожалуйста, введите ваше полное ФИО, как указано в паспорте.
    
    <i>Отправляя ваши данные, вы в соответствии с требованиями Федерального закона от 27.07.2006 г. № 152-ФЗ «О персональных данных» соглашаетесь на обработку своих персональных данных администрацией.</i>
passes-legal-name-changed-message =
    Ваше полное ФИО обновлено: <b>{$name}</b>.
passes-pass-create-cancel=
    Если снова будет желание зарегистрироваться на пасс, можно вернуться через команду /passes.
passes-payment-admin-button = {$adminEmoji} {$adminName}
passes-payment-admin-desc=
    {$adminEmoji} {$adminLink} {$phoneSBP ->
        [nosbp] — оплата в основном наличкой.
        [paypal] — оплата через PayPal.
        *[sbp] — оплата переводом СБП по номеру телефона.
    }
passes-admin-changed =
    Ваш амбассадор сменился: теперь это {$adminLink}.
    Это касается следующего пасса: {passes-pass-description}
    Обращайтесь к новому амбассадору, либо смените его в ваших пассах: /passes.
passes-pass-role-saved =
    Теперь выберите амбассадора. Потом можно будет сменить его, если что.
    
    {$adminTexts}
passes-pass-admin-saved =
    Теперь выберите тип пасса:
passes-pass-cancelled-by-other =
    Ваша пара только что отменила парный пасс на {$eventTitleLong}. К сожалению, вам придётся зарегистрироваться заново используя команду /passes.
passes-error-couple-not-found =
    Что-то пошло не так и я не могу найти вашу пару чтобы продолжить регистрацию вашего пасса.
    Может ваша пара отклонила предложение, но я потеряла сообщение, не знаю.
    Пожалуйста, зарегистрируйтесь снова, используя команду /passes.
passes-button-solo = 👤 Соло
passes-button-couple = 👥 Парный
passes-button-make-solo = 👤 Сделать сольным
passes-button-make-couple = 👥 Сделать парным
passes-couple-request-message =
    Для парного пасса, мне нужен аккаунт вашей пары.
    Чтобы прислать аккаунт, просто перешлите мне сообщение. Это может быть любое сообщение, даже стикер или эмоджи, содержимое сообщения не имеет значения, не будет обрабатываться или сохраняться.
    Перейдите в чат с вашей парой, выберете сообщение от неё и просто перешлите мне.
passes-couple-request-cancelled =
    Парный пасс отменён. Чтобы попробовать ещё раз, введите команду /passes снова.
passes-couple-request-wrong-data =
    Я попросила вас прислать сообщение от вашей пары, но не могу обработать присланное вами сообщение.
    Если это было пересланное сообщение, то у вашей пары стоит ограничение на видимость аккаунта.
    Попросите вашу пару снять ограничение или попробовать зарегистрироваться вместо вас.

    Чтобы снять ограничение, надо зайти в ≡ → Настройки → Конфиденциальность → Пересылка сообщений,
    Там либо дать всем права, либо добавить меня в исключения.

    Чтобы попробовать ещё раз, введите команду /passes снова.
passes-couple-request-edit =
    Прокрутите вниз чтобы выбрать пару.
passes-promopass-select-role =
    Вы зарегистрированы на пасс Zouk Non Stop и находитесь в списке ожидания, но не выбрали тип пасса.
    Если вы решите поменять тип пасса на парный, то можете это сделать сейчас без потери позиции.
    Через два дня все нетронутые пассы в листе ожидания будут сконвертированы в сольные.
passes-couple-saved =
    Я сохранила ваше приглашение, но похоже ваша пара не зарегистрирована и я не могу отправить приглашение.
    Попросите вашу пару начать разговор со мной, прислав мне команду /start, а затем принять приглашение, отправив команду /passes и выбрав {$eventTitleShort}.
    Приглашение действительно в течение двух дней.
passes-couple-invitation =
    Пользователь <i>{$coupleName}</i>, {$coupleLink} пригласил вас на марафон {$eventTitleLong}.
    Пользователь приглашает разделить с ним парный пасс и поучаствовать в роли {$coupleRole ->
        [leader] партнёрши
        *[follower] партнёра
    }.
    Пожалуйста, примите или отклоните приглашение:
passes-choose-admin =
    Пожалуйста выберите амбассадора вашего региона, или того, которому вы больше всего доверяете.
    Если вы уже оплатили кому-то из них, то выберите его.
passes-button-change-admin = 🔄🧑🏼‍💼 Выбрать амбассадора
passes-button-couple-accept = ✅ Принять
passes-button-couple-decline = 🙅 Отклонить
passes-accept-pass-request-name =
    Чтобы принять приглашение, сначала введите ваше полное ФИО, как в паспорте:
passes-accept-pass-request-continue =
    Чтобы принять приглашение, прокрутите вниз, чтобы продолжить:
passes-invitation-successfully-accepted =
    Приглашение было успешно принято.
    Для просмотра вашего пасса или управления им, используйте команду /passes.
passes-invitation-accept-failed =
    Похоже, приглашение более недействительно. Либо оно истекло, либо было отозвано отправителем.
passes-invitation-successfully-declined =
    Приглашение успешно отклонено.
passes-invitation-was-accepted =
    Пользователь <i>{$coupleName}</i>, {$coupleLink} принял ваше приглашение на парный пасс.
passes-invitation-was-declined =
    К сожалению, пользователь <i>{$coupleName}</i>, {$coupleLink} отклонил ваше приглашение.
    Вы можете выбрать сольный пасс или найти другую пару.
passes-couple-saved-sent =
    Я отправила вашей паре приглашение.
    Попросите вашу пару подтвердить его. Если приглашение не пришло, ваша пара может найти его по команде /passes и выбрать {$eventTitleShort}.
    Приглашение действительно в течение двух дней.
passes-couple-request-invitee-paid =
    Упс, этот человек уже зарегистрирован и получил свой пасс. Чтобы попробовать ещё раз, введите команду /passes снова.
passes-couple-request-timeout =
    Я не дождалась пересланного сообщения от вашей пары. Чтобы попробовать ещё раз, введите команду /passes снова.
passes-select-type-message =
    Выбери, с каким Зук Нон Стоп марафоном мы будем работать сейчас:
passes-select-type-button =
    {$eventCountryEmoji} {$eventTitleShort}
passes-solo-saved =
    Выбран соло пасс.
passes-role =
    {$role ->
        [leader] партнёра
        *[follower] партнёрши
    }
passes-role-select =
    Вы можете выбрать роль (это не связано с уже назначенными пассами):
passes-role-change-select =
    Вы можете изменить свою будущую роль (это не связано с уже назначенными пассами):
passes-pass-role-select =
    Чтобы продолжить, выберите роль:
passes-role-button-leader = 🕺 Партнёр
passes-role-button-follower = 💃 Партнёрша
passes-role-button-cancel = ❌ Отменить и выйти
passes-role-saved =
    Ваша новая роль {passes-role} сохранена.
passes-role-exit = Чтобы изменить роль снова, используйте команду /role
passes-payment-deadline-close =
    Внимание: ваш срок оплаты пасса истекает через 1 день. Действуйте быстро, чтобы сохранить ваше место!
    Подробнее смотрите в /passes.
passes-payment-deadline-exceeded =
    К сожалению, срок оплаты пасса вышел, и он больше не зарезервирован за вами.
    Если вы хотите попробовать снова, используйте команду /passes.
passes-couple-didnt-answer =
    К сожалению, я не получила ответ от вашей пары на приглашение.
    Вы можете попробовать послать приглашение ещё раз, выбрать другого человека или выбрать соло пасс.
passes-invitation-timeout =
    Приглашение, которое я присылала ранее, истекло. Вы можете зарегистрироваться снова или посмотреть ваш пасс по команде /passes.
passes-passport-request-message=
    Пожалуйста, введите серию и номер вашего паспорта, как указано в нём.
    Например, <code>4567 123456</code>. Если у вас отличается, напишите ровно как в паспорте.
    Это необходимо для входа на мероприятие.
passes-passport-changed-message =
    Ваш паспорт успешно обновлён: <b>{$passportNumber}</b>.
passes-passport-timeout =
    Я не получила ваш номер паспорта вовремя. Пожалуйста, используйте команду /passes, чтобы попробовать снова.
passes-passport-data-required-beginning-message =
    Пожалуйста, предоставьте ваши паспортные данные.
passes-passport-data-required =
    Чтобы вам попасть на мероприятие, мне нужно знать данные вашего паспорта.
    Эта информация должна быть предоставлена как можно скорее и будет передана в охранный пункт на входе.
    ⚠️‼️ <b>Дедлайн для предоставления данных — <u>завтра, 3 июня, 23:59.</u></b> ⚠️‼️

    Пожалуйста, потратьте минуту, чтобы проверить или ввести свои паспортные данные, используя кнопку ниже.
    Неверные данные могут привести к тому, что вы не сможете попасть на мероприятие.
passes-passport-data-button = ⚠️‼️📝 Ввести данные паспорта⚠️



user-is-restricted = Часть действий была отключена.
user-is-none = Не удалось получить информацию о пользователе. Пожалуйста, попробуйте команду /start ещё раз.

menu-alert-lunch-incomplete = Заказ неполный. Пожалуйста, выберите все необходимые блюда для выбранных обедов.
menu-confirm-dinner-empty = Вы не выбрали ни одного блюда на ужин в один или несколько дней. Продолжить?

food-button-exit = 🚪 Выйти
food-message-exited = Если тебе снова понадобится управлять заказом еды, ты всегда можешь использовать команду /food.

