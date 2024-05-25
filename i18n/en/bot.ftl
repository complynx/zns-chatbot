auth-authorize = ✔ Authorize
auth-authorized = Request authorized
auth-cancelled = Request cancelled
auth-decline = ❌ Decline
auth-declined = Request declined
auth-request =
    Someone requested authorization for ZNS bot from site {$origin}
    Their IP: {$ip}
    Their User-agent: {$useragent}
start-message =
    Hi, zoukonaut! I will help you create a user image with frame. To do it, just send me your best picture, please.
cancel-command = Cancel
cover-caption-message =
    ❗️If you use this photo in the social networks with profile covers, it goes well with this one.
frame-mover-help-desktop =
    Photo can be moved with a mouse, to turn it hold Shift.
    To scale it, use scrolling.
frame-mover-help-mobile =
    Photo can be dragged with touch. With two fingers you can scale and twist it.
    If in the process of dragging, the window starts closing, try start dragging upwards.
frame-mover-help-unified =
    Photo can be dragged, scaled and turned using mouse or touch.
    To scale and turn with a mouse, use scrolling or hold Shift.
frame-mover-finish-button-text = Submit
undefined-state-error = Something went wrong, try again.
unsupported-message-error = Sorry, I can't understand this message.
max-assistant-messages-reached = You've reached your questions limit for a day, please come back in 24 hours.
avatar-custom-crop = Crop differently
avatar-without-command = No need to send this command anymore, sending the picture right away is better.
something-went-wrong = Something went wrong. Try again.
food-adm-payment-proof-accept-button = ✅ Accept
food-adm-payment-proof-confirmed = Order payment from user {$link} for <i>{$name}</i> is confirmed.
food-adm-payment-proof-received =
    User {$link} sent payment for food order
    Total order sum: {$total} ₽
    order is in the name <i>{$name}</i>. Confirmation required.
    <b>Attention</b>, do not mark proof as rejected, wait a bit and try to find the payment first.
food-adm-payment-proof-reject-button = ❌ Reject
food-adm-payment-proof-rejected = Order payment from user {$link} for <i>{$name}</i> is rejected.
food-back-button = To the beginning.
food-cancel-order-button = Cancel order.
food-command-description = Your space meal at ZNS
food-confirm-payment-button = Pay for the order
food-created-write-for-who = Your order has been saved, write your name, so we can easily identify your meal and give it to you.
food-day-wednesday = Wednesday
food-day-friday = Friday
food-day-saturday = Saturday
food-day-sunday = Sunday
food-edit-order-button = Edit order.
food-meal-lunch = lunch
food-meal-dinner = dinner
food-name-update-cancelled = Recipient name is left unchanged.
food-name-updated = The new recipient name <i>{$name}</i> was saved.
food-new-order-button = Create new
food-no-orders-yet = 
    For the full immersion, zoukonaut has to have a good meal!
    Press the button to choose yourself lunch and or dinner
food-order-button = {$created}|{$name}
food-order-for = Order for <b>{$name}</b>
food-order-message-begin = This is your order, here you can view it or edit.
food-order-new-message-begin = This is a new order, you can open up menu and choose meals.
food-order-saved = Your order has been saved, you can now proceed with the payment transfer.
food-payment-instructions =
    To pay for the meal, you need to make a transaction using SBP to the Sber account bound to phone number
    <code>+79175295923</code>
    Recipient: <i>Ушакова Дарья Евгеньевна/Ushakova Daria Evgenievna</i>.
food-payment-instructions-proof =
    ⚠ When you're done with the transfer, you will need to send me the confirmation —
    with all the data about the transaction: from who, amount, date and time, and other relevant data.
    If you don't have the confirmation now, press cancel.
    You'll be able to come back through the menu.
food-payment-proof-cancelled = Ok, you will have to send proof using menu, when you'll have it.
food-payment-proof-confirmed =
    Administrator confirmed your order payment for <i>{$name}</i>.
    Happy to see you at ZNS.
food-payment-proof-failed = I was waiting for your payment proof and received something else. You will have to send the proof using menu, when you'll have it.
food-payment-proof-forwarded = I sent the proof to our administrators. They will check it and I'll come back with the result.
food-payment-proof-rejected = 
    Administrator rejected payment proof for <i>{$name}</i>.
    You can create another order or try sending another confirmation.
food-rename-button = Rename order
food-select-order = Here you can view your order or create a new one.
food-total = <b>Total:</b> <code>{$total}</code>
food-write-for-who = Please write <b>full name</b> of the person, for who we are creating the order. This will help to identify it at the event.
dow-short =
    { $dow ->
        [0] mo
        [1] tu
        [2] we
        [3] th
        [4] fr
        [5] sa
        *[6] su
    }
dow-long =
    { $dow ->
        [0] monday
        [1] tuesday
        [2] wednesday
        [3] thursday
        [4] friday
        [5] saturday
        *[6] sunday
    }
massage-booking-cancel-button = ❌ Cancel booking
massage-client-about =
    Massage type: {$durationicon} <b>{$price}</b> ₽ / <b>{$duration}</b> minutes.
    Party: <b>{$party}</b>
    Time: <b>{$time}</b>
    Specialist: {$specialist}

    Come <u>on time</u> because there is somebody after you. If you can't come, please cancel in advance.
    Enjoy your cake of peace!
massage-command-description = Book a massage
massage-create-button = 📝 New booking
massage-deleted = Booking successfully deleted. If you need to modify or delete another, press /massage
massage-edit-back-button = ⬅ Back
massage-edit-cancel-button = 🚪 Exit
massage-edit-choose-length = Select desired massage duration:
massage-edit-choose-party-button = {$party}
massage-edit-error-too-many-massages = Only { $max} massages allowed a day for one client.
massage-edit-error-slot-timeout = Cannot book this late, choose another time slot.
massage-edit-error-slot-unavailable = Somebody just reserved this slot, choose another one.
massage-edit-length-button = {$icon} {$price} ₽ / {$minutes} minutes.
massage-edit-next-button = ➡ Next
massage-edit-no-slots-available = No slots available...
massage-edit-page = page {$page} of {$leng}
massage-edit-page-next-button = Next ▶
massage-edit-page-previous-button = ◀ Previous
massage-edit-select-slot =
    Selected massage: {$durationicon} <b>{$price}</b> ₽ / <b>{$duration}</b> minutes.

    Now you can select a party, tick or untick specialists to find available slots in their schedules.
    By default everyone is enabled ✅, but you can exclude some of them ❌ by clicking on their names.
    Then, select the time slot you want.

    {$specialists}<i>{$filtered}{$error}</i>
massage-edit-select-specialists-filtered = Some specialists are not shown because they don't work for massages of this duration.
massage-exit-button = 🚪 Exit
massage-exited = If you need it again, you can always click: /massage
massage-notification-prior-long =
    Hi, zoukonaut!
    I remind you that you have a massage in {$inminutes} minutes, at <b>{$time}</b>:
    Massage type: {$durationicon} <b>{$price}</b> ₽ / <b>{$duration}</b> minutes.
    Specialist: {$specialist}

    Come <u>on time</u> because there is somebody after you. If you can't come, please cancel in advance.
    Enjoy your cake of peace!
massage-notification-prior =
    Hi, zoukonaut!
    I remind you that you have a massage in {$inminutes} minutes, at <b>{$time}</b>:
    Massage type: {$durationicon} <b>{$price}</b> ₽ / <b>{$duration}</b> minutes.
    Specialist: {$specialist}

    Come <u>on time</u> because there is somebody after you. If you can't come, please cancel in advance.
    Enjoy your cake of peace!
massage-notification-toggle = {$pos ->
    [y] 🔔
    *[n] 🔕
}
massage-notifications-edit = You can switch on and off your notifications:
massage-specialist-booking-cancelled =
    Client <i>{$client}</i> <u>cancelled</u> booking:
    Massage type: {$durationicon} <b>{$price}</b> ₽ / <b>{$duration}</b> minutes.
    Party: <b>{$party}</b>
    Time: <b>{$time}</b>

    You can view all the bookings or disable notifications by calling /massage
massage-specialist-clientlist = Here are your clients and their bookings by party:
massage-specialist-clientlist-button = 📃 Client list
massage-specialist-failed-to-reserve = Failed to reserve instant slot, likely someone booked in the meantime, or slot timed out. Try again.
massage-specialist-instantbook = Instant booking:
massage-specialist-instantbook-button = {$icon} {$minutes}+5
massage-specialist-new-booking =
    Client <i>{$client}</i> booked a massage:
    Massage type: {$durationicon} <b>{$price}</b> ₽ / <b>{$duration}</b> minutes.
    Party: <b>{$party}</b>
    Time: <b>{$time}</b>
massage-specialist-no-party-or-slot = No party or slot could have been calculated — error.
massage-specialist-notification-notify-bookings = Booking created or deleted
massage-specialist-notification-notify-next = Upcoming session (5 min before)
massage-specialist-notification-soon =
    You have upcoming massage in {$inminutes} minutes, at <b>{$time}</b>.
    Client <i>{$client}</i>:
    Massage type: {$durationicon} <b>{$price}</b> ₽ / <b>{$duration}</b> minutes.
massage-specialist-notifications-button = 🔔 Notifications
massage-specialist-reserved =
    Successfully reserved:
    Massage type: {$durationicon} <b>{$price}</b> ₽ / <b>{$duration}</b> minutes.
    Time: <b>{$time}</b>
massage-specialist-timetable-button = 📅 Timetable
massage-specialist-to-start-button = ⬅ To start
massage-specialist-view =
    Client: <i>{$client}</i>
    Massage type: {$durationicon} <b>{$price}</b> ₽ / <b>{$duration}</b> minutes.
    Party: <b>{$party}</b>
    Time: <b>{$time}</b>
massage-specialist-view-booking-button = 📝 View booking
massage-start-message = 
    Click "New booking", to book a massage session or select your current session to change or cancel it or to contact the specialist.
    <a href="https://t.me/zouknonstopchannel/182">About massages at ZNS</a>.
    
    Our specialists:
massage-successfully-created = Massage session was successfully booked.
massage-unfinished = Unfinished booking
massage-your-boookings = Your bookings: