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
frame-realign-message =
    Please align the markers on screen if they are not align, and click submit.
frame-mover-finish-button-text = Submit
orders-adm-payment-cash-requested =
    User {$link} wants to pay for their order in cash to you.
    Total order sum: Br {$total}
    order is in the name <i>{$name}</i>.
    Don't reject the order, wait for the user to contact you first, or ask them yourself.
orders-adm-payment-proof-received =
    User {$link} sent payment for their order
    Total order sum: {$total} ₽
    order is in the name <i>{$name}</i>. Confirmation required.
    <b>Attention</b>, do not mark proof as rejected, wait a bit and try to find the payment first.
orders-admin-belarus = {$name}/{$region}
orders-back-button = Back
orders-closed = If you need to see your orders or change something, you can always use /orders command
orders-close-button = Exit
orders-edit-button = Change order
orders-finish-button-text = Submit
orders-message-list = Your orders are listed here
orders-message-payed-where =
    I received a PDF, which may be a payment proof. Am I right?

orders-message-payment-options =
    You can pay for your order using these methods:

    Russia:
    SBp to <code>+79217826737</code> (Sber, VTB)
    Olga Tesla
    <code>{$rutotal}</code> ₽

    ⚠ <i>After payment, you have to send me a payment proof in <b>PDF</b> format, I don't accept other formats!
    If you have a proof as an image, you can convert it to PDF using this bot:</i> @ImageToPdfRobot

    Belarus:
    Choose one of the contacts below and pay them in person.
    Br <code>{$total}</code>
orders-next-button-text = Next
orders-new-button = Create order
orders-order-pay-button = Payment
orders-order-button = {$created}|{$name}
orders-order-unpaid-button = (unpaid){$created}|{$name}
orders-order-delete-button = Clear order
orders-pay-cancel = Cancel
orders-payment-cash-requested =
    Contact {$link} to arrange a personal meeting and pay.
    Total order sum: Br {$total}
    order is in the name <i>{$name}</i>.
    After cash is collected, the admin will mark your order as payed.
orders-payed-button = It's payment proof
orders-placeholder-first-name=First name
orders-placeholder-last-name=Last name
orders-placeholder-patronymus=Second name(s)
orders-validity-error-first-name=First name should contain one upper case letter and then at least one lowercase
orders-validity-error-last-name=Last name should contain one upper case letter and then at least one lowercase
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
    <b>Amount to pay:</b> <code>{$total}</code> ₽
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
food-remind-about-order =
    I noticed that you have an order that hasn't been paid for yet. If you still plan to have these meals at the marathon, please make the payment and send me the receipt. Otherwise, the order will not be placed and won't be delivered.

    You can pay up until June 4th. If you don't want your order, you can simply cancel it.

    Here is your order:
food-rename-button = Rename order
food-select-order = Here you can view your order or create a new one.
food-total = <b>Total:</b> <code>{$total}</code> ₽
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
    Massage type: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> minutes.
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
massage-edit-length-button = {$icon} Br {$price} / {$minutes} minutes.
massage-edit-next-button = ➡ Next
massage-edit-no-slots-available = No slots available...
massage-edit-page = page {$page} of {$leng}
massage-edit-page-next-button = Next ▶
massage-edit-page-previous-button = ◀ Previous
massage-edit-select-slot =
    Selected massage: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> minutes.

    Now you can select a party, tick or untick specialists to find available slots in their schedules.
    By default everyone is enabled ✅, but you can exclude some of them ❌ by clicking on their names.
    Then, select the time slot you want.

    {$specialists}<i>{$filtered}{$error}</i>
massage-edit-select-specialists-filtered = Some specialists are not shown because they don't work for massages of this duration.
massage-exit-button = 🚪 Exit
massage-exited = If you need it again, you can always click: /massage
massage-notification-additional =
    Hi, zoukonaut!
    You have a massage booking:
    Massage type: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> minutes.
    Party: <b>{$party}</b>
    Time: <b>{$time}</b>
    Specialist: {$specialist}

    Come <u>on time</u> because there is somebody after you. If you can't come, please cancel in advance.
    Enjoy your cake of peace!
massage-notification-prior-long =
    Hi, zoukonaut!
    I remind you that you have a massage in {$inminutes} minutes, at <b>{$time}</b>:
    Massage type: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> minutes.
    Specialist: {$specialist}

    Come <u>on time</u> because there is somebody after you. If you can't come, please cancel in advance.
    Enjoy your cake of peace!
massage-notification-prior =
    Hi, zoukonaut!
    I remind you that you have a massage in {$inminutes} minutes, at <b>{$time}</b>:
    Massage type: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> minutes.
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
    Massage type: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> minutes.
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
    Massage type: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> minutes.
    Party: <b>{$party}</b>
    Time: <b>{$time}</b>
massage-specialist-no-party-or-slot = No party or slot could have been calculated — error.
massage-specialist-notification-notify-bookings = Booking created or deleted
massage-specialist-notification-notify-next = Upcoming session (5 min before)
massage-specialist-notification-soon =
    You have upcoming massage in {$inminutes} minutes, at <b>{$time}</b>.
    Client <i>{$client}</i>:
    Massage type: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> minutes.
massage-specialist-notifications-button = 🔔 Notifications
massage-specialist-reserved =
    Successfully reserved:
    Massage type: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> minutes.
    Time: <b>{$time}</b>
massage-specialist-timetable-button = 📅 Timetable
massage-specialist-to-start-button = ⬅ To start
massage-specialist-view =
    Client: <i>{$client}</i>
    Massage type: {$durationicon} Br <b>{$price}</b> / <b>{$duration}</b> minutes.
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

passes-adm-payment-proof-accept-button = ✅ Accept
passes-adm-payment-proof-reject-button = ❌ Reject
passes-adm-payment-proof-accepted =
    Payment for {$role} pass from user {$link} for <i>{$name}</i> is confirmed.
passes-adm-payment-proof-rejected =
    Payment for {$role} pass from user {$link} for <i>{$name}</i> is confirmed.
passes-adm-payment-proof-received =
    User {$link} sent payment for a {$role} pass.
    Their pass price: {$price} ₽
    order is in the name <i>{$name}</i>. Confirmation required.
    <b>Attention</b>, do not mark proof as rejected, wait a bit and try to find the payment first.
passes-announce-user-registered =
    {$name} applied for a {$role} pass!
passes-button-cancel = ❌ Cancel ⚠️
passes-button-change-name = 🏷 Change name
passes-button-exit = 🚪 Exit
passes-button-pay = 💸 Payment proof
passes-command-description = Register to ZNS or manage your registration
passes-added-to-waitlist =
    Hello, <i>{$name}</i>!
    Unfortunately, all the passes are currently sold out. However, you’ve been added to the waiting list for the next {$role} Zouk Non Stop pass.
    
    You can:
    - Cancel your spot (note that this will permanently remove you from the waitlist)
    - Change the name associated with your spot
    
    Stay tuned — if a pass becomes available, you’ll be notified!
passes-pass-edit-waitlist=
    Hello, <i>{$name}</i>!
    You are currently on the waiting list for a {$role} Zouk Non Stop pass.
    
    You can:
    - Cancel your spot (this will remove you from the waitlist permanently)
    - Change the name associated with your spot
    
    We’ll notify you as soon as a pass becomes available!
passes-pass-assigned =
    Hello, <i>{$name}</i>!
    Congratulations! A {$role} Zouk Non Stop pass has just been assigned to you.
    
    This pass is not officially yours yet. You must complete the payment within 7 days of the assignment.
    The current price is <code>{$price}</code> ₽. Payment can be made via SBP using the phone number <code>+79217826737</code> (Sberbank, VTB), recipient: <b>Olga Tesla</b>.
    
    If you do not pay within this period or fail to send proof of payment, the pass will be offered to someone else.
    Please act quickly to secure your spot!
passes-pass-edit-assigned=
    Hello, <i>{$name}</i>!
    A {$role} Zouk Non Stop pass has been assigned to you.
    
    To claim it, you need to complete the payment within 7 days of the assignment.
    The current price is <code>{$price}</code> ₽. Payment can be made via SBP using the phone number <code>+79217826737</code> (Sberbank, VTB), recipient: <b>Olga Tesla</b>.
    
    You can use this interface to:
    - Edit your name
    - Upload proof of payment
    - Cancel the assignment
    
    Please note that cancellations or missing the payment deadline are irreversible.
passes-pass-edit-payed=
    Hello, <i>{$name}</i>!
    Congratulations! The {$role} Zouk Non Stop pass is now officially yours as you have successfully paid for it.
    You can change the name associated with the pass if necessary.
    If you need to return the pass and request a refund, please contact the administrator.
passes-pass-role-saved =
    You've chosen pass role {$role}.
passes-pass-create-cancel=
    If you want to register for a pass, you can come back using /passes command.
passes-pass-exit=
    If you need to view your pass, provide payment proof or change name, you can get back by calling /passes command again.
passes-pass-cancelled=
    Your pass assignment has been cancelled. If you want to get back again, you can call /passes command any time.
passes-pass-cancel-failed =
    Cancellation failed. Either the pass was marked payed or was already cancelled.
passes-payment-request-callback-message =
    You need to pay <code>{$price}</code> ₽.
    Payment can be made via SBP using the phone number <code>+79217826737</code> (Sberbank, VTB), recipient: <b>Olga Tesla</b>.
    Scroll down to send the proof of payment.
passes-payment-request-waiting-message =
    You need to pay <code>{$price}</code> ₽.
    Payment can be made via SBP using the phone number <code>+79217826737</code> (Sberbank, VTB), recipient: <b>Olga Tesla</b>.
    Please send the proof of payment in PDF format or as an image.
passes-payment-proof-timeout =
    I haven’t received your payment proof in time. Please use the /passes command to try again.
passes-payment-proof-cancelled =
    If you need to resubmit your payment proof or view your pass details, use the /passes command.
passes-payment-proof-accepted =
    Hello, <i>{$name}</i>!
    Congratulations! The {$role} Zouk Non Stop pass is now officially yours, as your payment has been successfully approved by the administrator.
    You can view your pass details by using the /passes command.
passes-payment-proof-rejected =
    Hello, <i>{$name}</i>!
    Unfortunately, your payment proof was rejected by the administrator.
    You can view your pass details or submit another proof by using the /passes command.
    Don’t wait too long — secure your spot before it’s gone!
passes-name-timeout =
    I haven’t received your name in time. Please use the /passes command to try again.
passes-payment-proof-wrong-data =
    I was expecting a PDF file or an image as proof of payment, but this message doesn’t seem to match that.
    Sorry about the confusion.
    To try again, please use the /passes command and submit a PDF file or an image.
passes-payment-proof-forwarded =
    I’ve sent your payment proof to our administrators for verification. They will review it, and I’ll update you with the result soon.
    If you need to view your pass details in the meantime, use the /passes command.
passes-sell-not-started =
    Please be patient; sales have not opened yet.
passes-pass-create-start-message =
    First, I need some information from you.
    Scroll down and enter your full name as it appears in your passport.
passes-legal-name-request-message =
    First, I need some information from you.
    Please enter your full name as it appears in your passport.
    
    <i>By providing your information, you consent to its storage, processing, and use in accordance with our policies.</i>
passes-legal-name-changed-message =
    Your legal name is updated: <b>{$name}</b>.
passes-role-select =
    You can select a role (not related to the assigned passes):
passes-role-change-select =
    You can change your future role (not related to the assigned passes):
passes-pass-role-select =
    To continue, please select a role:
passes-role-button-leader = 🕺 Leader
passes-role-button-follower = 💃 Follower
passes-role-button-cancel = ❌ Cancel and exit
passes-role-saved = Your new role as a {$role} is saved.
passes-role-exit = To change role again, use command /role
passes-payment-deadline-close =
    Warning: Your pass payment deadline is just 1 day away. Act quickly to secure your spot!
    See /passes for more details.
passes-payment-deadline-exceeded =
    Unfortunately, your payment deadline has been exceeded, and the pass is no longer reserved for you.
    If you would like to try again, use the /passes command.

user-is-restricted = Some actions has been disabled.
user-is-none = Maybe run /start first?