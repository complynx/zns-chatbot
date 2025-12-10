avatar-no-face = No face found on the image. Please try again with another image: either move away from the phone or move closer, or turn the phone straight.
avatar-no-role = Can't create avatar, because you seem not to be registered to the next ZNS. Please register first using /passes command.
avatar-processing =
    Your photo is processing...
    It may take a while.
avatar-disabled = Creation of userpics temporarily disabled.
avatar-choose-method = Personal portholes are available for the Zukerion crew. The bravest zoukonauts can go into outer space; for this, it will be necessary to wear a spacesuit.
avatar-method-simple = 🪞 Porthole
avatar-method-detailed = 🧑‍🚀 Spacesuit
avatar-cancel-button = ❌ Cancel
avatar-error-generic = Something went wrong. Please try again.
avatar-cancelled-message = Avatar creation cancelled.
avatar-processing-initial = Processing your request...
avatar-error-file-expired-or-missing = Unfortunately, the file was not found or has expired. Please try uploading it again.
avatar-simple-caption = I won't draw you a sheep, but here's what a beauty turned out!
avatar-error-frame-missing = Error: Frame file for simple avatar is missing. Please contact an administrator.
avatar-error-config-role = Error: Configuration for your role is missing or incomplete. Please contact an administrator.

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
    <b>Hello, Zoukonaut!</b> I am <b>ZiNuSya</b>, your digital assistant in the world of convenience and technology.
    Always here to help you register a pass, order hot meals, book a massage,
    create an avatar, and much more — if these features are currently available.
    Want to know what I can do for you right now?
    <b>Click the blue <code>Menu</code> button</b>, and I'll show you all my capabilities!
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
orders-command-description = Order food, transportation and activities.
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
    After cash is collected, the admin will mark your order as paid.
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

# Food Plugin Entries
# Entries are sorted alphabetically, with admin-specific entries (food-adm-) first.

# Admin messages
food-adm-payment-accepted-msg = Payment for order of {$total} RUB from user {$link} accepted.
food-adm-payment-already-processed-or-error = Order already processed or an error occurred.
food-adm-payment-proof-accept-button = ✅ Accept
# food-adm-payment-proof-confirmed = Order payment from user {$link} for <i>{$name}</i> is confirmed. # Retained, but not directly used by current food.py review scope
food-adm-payment-proof-received =
    User {$link} sent payment for their food order.
    Total order sum: {$total} ₽. Confirmation required.
    <b>Attention</b>, do not mark proof as rejected, wait a bit and try to find the payment first.
food-adm-payment-proof-reject-button = ❌ Reject
# food-adm-payment-proof-rejected = Order payment from user {$link} for <i>{$name}</i> is rejected. # Retained, but not directly used by current food.py review scope
food-adm-payment-rejected-msg = Payment for order of {$total} RUB from user {$link} rejected.

# User messages
food-button-create-order = 📝 Create Order
food-button-delete-order = 🗑️ Delete Order
food-button-edit-order = ✏️ Edit Order
food-button-pay = 💸 Pay for Order
food-button-view-order = 👁️ View Order
food-cannot-submit-proof-now = Cannot submit payment proof for this order at this time.
food-command-description = Your cosmic snack on ZNS.
food-no-order = You have no active food order. Create one using the button below.
food-not-authorized-admin = You are not authorized for this action.
food-order-already-paid = This order has already been paid.
food-order-cannot-delete-paid-submitted = This order is already paid or proof has been submitted, and it cannot be deleted.
food-order-deleted-successfully = Order successfully deleted. To create a new one, use the /food command.
food-order-exists-not-complete = Your current order total is {$total} ₽. View or edit your order below.
food-order-exists-payable = Your order (Total: {$total} ₽) is ready for payment. If a previous attempt failed, you can try again.
food-order-is-paid = Your order is paid. Total: {$total} ₽.
food-order-not-complete-for-payment = The order is not complete and cannot be paid yet.
food-order-not-found = Order not found.
food-order-not-found-admin = Order not found. (Admin mode)
food-order-proof-already-submitted = Payment proof for this order has already been submitted.
food-order-proof-submitted = Your payment proof on order with sum of {$total} ₽ has been submitted and is awaiting review.
food-payment-admin-error = Error communicating with the payment administrator. Please try again or contact support.
food-payment-admins-not-configured = The payment system is currently unavailable. Please contact support.
food-payment-proof-accepted = Your payment for the food order with a total of {$total} ₽ has been accepted! You can view your order details using the /food command.
food-payment-proof-cancelled = Payment proof submission cancelled. You can try to pay again or manage your order using the /food command.
food-payment-proof-forwarded = Your payment proof has been sent for review.
food-payment-proof-rejected-retry = Your payment proof for the food order with a total of {$total} ₽ was rejected. You can try to pay again using the /food command, or contact support.
food-payment-proof-timeout = You did not send the payment proof in time. You can try again using the /food command if you still wish to pay.
food-payment-proof-wrong-data =
    I was expecting a PDF file or an image as proof of payment, but this message doesn't seem to match that.
    Sorry about the confusion.
    To try again, please use the /food command and submit a PDF file or an image.

food-payment-method =
    {$phoneSBP ->
        [nosbp] Ambassador {$adminLink} receives payments only in cash or by prior agreement. {$phoneContact ->
            [nophone] Send them a message.
           *[other] Send them a message or call on {$phoneContact}.
        }
        [paypal] Ambassador {$adminLink} accepts payments via PayPal, please contact them for details and pricing including conversion and fees.
        *[sbp] Payment can be made via SBP using the phone number <code>{$phoneSBP}</code> ({$banks}, please don't send to other banks, your money may not arrive!), contact {$adminLink}.
    }

food-payment-request-callback-message =
    Your order total is <code>{$total}</code> ₽.
    {food-payment-method}
    Please prepare your payment proof (screenshot or PDF).

food-payment-request-waiting-message =
    Please send a screenshot or a PDF file <u>here, to the bot,</u> as proof of payment for your order of <code>{$total}</code> ₽.
    {food-payment-method}
food-notification-message-first =
    Hello, zoukonaut!
    Last day to place a food order for the marathon is <b>June 4th</b> (till the end of the day).
    If you want your order to be delivered to the marathon, you need to pay for it and send me the payment confirmation by the end of the day.
    Don't miss out!
food-notification-message-last = {food-notification-message-first}
food-no-order-notification-first = {food-notification-message-first}
food-no-order-notification-last = {food-notification-message-last}
food-not-accepting-orders = Sorry, you can no longer place food orders for the marathon. The deadline was June 4th.

# Activities
activity-adm-payment-proof-received =
    User {$link} sent payment for their selected activities.
    Total activities sum: {$activitiesTotal} ₽. Confirmation required.
    <b>Attention</b>, do not mark proof as rejected, wait a bit and try to find the payment first.
activity-adm-payment-proof-accept-button = ✅ Accept
activity-adm-payment-proof-reject-button = ❌ Reject
activity-adm-payment-accepted-msg =
    Payment for {$link}'s activities with a total of <code>{$activitiesTotal}</code> ₽ is confirmed.
activity-adm-payment-rejected-msg =
    Payment for {$link}'s activities with a total of <code>{$activitiesTotal}</code> ₽ is rejected.

activity-select-message =
    At Zouk Non Stop Picnic, there will be several activities. Some of them require a prior booking, even if you have a full pass and they are included in its cost.
    Mark the ones you want to attend, by toggling the buttons below between ☑️ and ❌.

    Cost:
    🌟<code>2500 ₽</code> - party + practices
    🌟<code>2000 ₽</code> - just party
    🌟<code>2000 ₽</code> - practices without party
    🌟<code> 750 ₽</code> - yoga separately
    🌟<code>1000 ₽</code> - cacao ceremony separately
    🌟<code>1000 ₽</code> - soundhealing separately
    Party and practices are included in the full pass, for others payment at the event.
    Mark where you are going to exactly:
activity-button-submit = ✅ Confirm selection
activity-finished-message =
    Selected activities:
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
    Total price: <code>{$totalPrice}</code> ₽. (only if you don't have a full pass)
    {food-payment-method}
activity-button-pay = 💸 Pay for activities
activity-button-exit = 🚪 Exit
activity-message-exited = If you need to see or change your activities selection, you can always use the /activities command.
activity-payment-request-callback-message =
    You need to pay <code>{$totalPrice}</code> ₽ for the selected activities.
    {food-payment-method}
    Scroll down to send the proof of payment.
activity-payment-request-waiting-message =
    You need to pay <code>{$totalPrice}</code> ₽ for the selected activities.
    {food-payment-method}
    Please send the proof of payment in PDF format or as an image.
activity-payment-proof-timeout =
    I haven't received your payment proof in time. Please use the /activities command to try again.
    Don't worry, your selection hasn't changed.
activity-payment-proof-cancelled =
    If you need to resubmit your payment proof or view your activities selection, use the /activities command.
    Don't worry, your selection hasn't changed.
activity-payment-proof-wrong-data =
    I was expecting a PDF file or an image as proof of payment, but this message doesn't seem to match that.
    Sorry about the confusion.
    To try again, please use the /activities command and submit a PDF file or an image.
activity-payment-proof-forwarded =
    I've sent your payment proof to our administrators for verification. They will review it, and I'll update you with the result soon.
    If you need to view your activities selection in the meantime, use the /activities command.
activity-payment-proof-accepted =
    Your payment for the selected activities with a total of <code>{$activitiesTotal}</code> ₽ has been accepted! You can view your selection using the /activities command.
activity-payment-proof-rejected-retry =
    Your payment proof for the selected activities with a total of <code>{$activitiesTotal}</code> ₽ was rejected. You can try to pay again using the /activities command, or contact support.


# Activity Names
activity-open = Party
activity-yoga = Fitness yoga
activity-cacao = Cacao ceremony
activity-soundhealing = Soundhealing
activity-button-all = Party + practices
activity-button-classes = Only practices
activities-command-description = Select activities

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
massage-price-b = <b>{$price}</b> Byn / <b>{$priceRu}</b> Rub
massage-price = {$price}/{$priceRu}
massage-pd = {$durationicon} <b>{$duration}</b> minutes / 💰 {massage-price-b}
massage-edit-length-button = {$icon}{$minutes}m/💰{massage-price}
massage-booking-cancel-button = ❌ Cancel booking
massage-client-about =
    Massage type: {massage-pd}.
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
massage-edit-next-button = ➡ Next
massage-edit-no-slots-available = No slots available...
massage-edit-page = page {$page} of {$leng}
massage-edit-page-next-button = Next ▶
massage-edit-page-previous-button = ◀ Previous
massage-edit-select-slot =
    Selected massage: {massage-pd}.

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
    Massage type: {massage-pd}.
    Party: <b>{$party}</b>
    Time: <b>{$time}</b>
    Specialist: {$specialist}

    Come <u>on time</u> because there is somebody after you. If you can't come, please cancel in advance.
    Enjoy your cake of peace!
massage-notification-prior-long =
    Hi, zoukonaut!
    I remind you that you have a massage in {$inminutes} minutes, at <b>{$time}</b>:
    Massage type: {massage-pd}.
    Specialist: {$specialist}

    Come <u>on time</u> because there is somebody after you. If you can't come, please cancel in advance.
    Enjoy your cake of peace!
massage-notification-prior =
    Hi, zoukonaut!
    I remind you that you have a massage in {$inminutes} minutes, at <b>{$time}</b>:
    Massage type: {massage-pd}.
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
    Massage type: {massage-pd}.
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
    Massage type: {massage-pd}.
    Party: <b>{$party}</b>
    Time: <b>{$time}</b>
massage-specialist-no-party-or-slot = No party or slot could have been calculated — error.
massage-specialist-notification-notify-bookings = Booking created or deleted
massage-specialist-notification-notify-next = Upcoming session (5 min before)
massage-specialist-notification-soon =
    You have upcoming massage in {$inminutes} minutes, at <b>{$time}</b>.
    Client <i>{$client}</i>:
    Massage type: {massage-pd}.
massage-specialist-notifications-button = 🔔 Notifications
massage-specialist-reserved =
    Successfully reserved:
    Massage type: {massage-pd}.
    Time: <b>{$time}</b>
massage-specialist-timetable-button = 📅 Timetable
massage-specialist-to-start-button = ⬅ To start
massage-specialist-view =
    Client: <i>{$client}</i>
    Massage type: {massage-pd}.
    Party: <b>{$party}</b>
    Time: <b>{$time}</b>
massage-specialist-view-booking-button = 📝 View booking
massage-start-message = 
    Click "New booking", to book a massage session or select your current session to change or cancel it or to contact the specialist.
    <a href="https://t.me/zouknonstopchannel/670">About massages at ZNS</a>.
    
    Our specialists:
massage-successfully-created = Massage session was successfully booked.
massage-unfinished = Unfinished booking
massage-your-boookings = Your bookings:




passes-adm-payment-proof-accept-button = ✅ Accept
passes-adm-payment-proof-reject-button = ❌ Reject
passes-adm-pass-description=
    {$type ->
        [couple] couple pass to {passes-pass-title-long} from user {$link} for <i>{$name}</i> as {$role} and {$coupleLink} (<i>{$coupleName}</i>)
        *[solo] {$role} pass to {passes-pass-title-long} from user {$link} for <i>{$name}</i>
    }
passes-adm-payment-proof-accepted =
    Payment for {passes-adm-pass-description} is confirmed.
passes-adm-payment-proof-rejected =
    Payment for {passes-adm-pass-description} is rejected.
passes-adm-payment-proof-received =
    User {$link} sent payment for a {passes-adm-pass-description}.
    Their pass price: {$price} ₽
    Confirmation required.
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
    Unfortunately, all the passes are currently sold out. However, you've been added to the waiting list for the next {passes-pass-description}.
    
    Your ambassador: {$adminLink}
    
    You can:
    - Cancel your spot (note that this will permanently remove you from the waitlist)
    - Change the name associated with your spot
    
    Stay tuned — if a pass becomes available, you'll be notified!
passes-pass-edit-waitlist=
    Hello, <i>{$name}</i>!
    You are currently on the waiting list for a {passes-pass-description}.
    
    Your ambassador: {$adminLink}
    
    You can:
    - Cancel your spot (this will remove you from the waitlist permanently)
    - Change the name associated with your spot
    
    We'll notify you as soon as a pass becomes available!
passes-pass-edit-waiting-for-couple=
    Hello, <i>{$name}</i>!
    You've selected a couple pass to {passes-pass-title-long} as a {$role} and invited {$coupleLink}.
    They haven't yet answered your invitation.

    Your ambassador: {$adminLink}
    
    You can:
    - Cancel your pass
    - Change it for solo
    - Change the name associated with your spot
passes-payment-method =
    {$phoneSBP ->
        [nosbp] Ambassador {$adminLink} receives payments only in cash or by prior agreement. {$phoneContact ->
            [nophone] Send them a message.
           *[other] Send them a message or call on {$phoneContact}.
        }
        [paypal] Ambassador {$adminLink} accepts payments via PayPal, please contact them for details and pricing including conversion and fees.
        *[sbp] Payment can be made via SBP using the phone number <code>{$phoneSBP}</code> ({$banks}), contact {$adminLink}.
    }
passes-pass-assigned =
    Hello, <i>{$name}</i>!
    Congratulations! A {passes-pass-description} has just been assigned to you.
    
    This pass is not officially yours yet. You must complete the payment within 7 days of the assignment.
    The current price is <code>{$price}</code> ₽. {passes-payment-method}
    
    If you do not pay within this period or fail to send proof of payment, the pass will be offered to someone else.
    Please act quickly to secure your spot!
passes-pass-free-assigned =
    Hello, <i>{$name}</i>!
    Congratulations! The {passes-pass-description} has just been assigned to you.

    Since this is a free pass, you don't need to pay anything.
    You can change the name associated with the pass if necessary.
    If you need to cancel the pass, please contact the administrator.
passes-pass-cancelled-by-other =
    Your couple just cancelled the couple pass to {passes-pass-title-long}. Unfortunately, you have to register again using the command /passes.
passes-pass-description=
    {$type ->
        [couple] couple pass to {passes-pass-title-long} with you as a {$role} and your couple {$coupleLink}
        *[solo] {$role} pass to {passes-pass-title-long}
    }
passes-pass-edit-assigned=
    Hello, <i>{$name}</i>!
    A {passes-pass-description} has been assigned to you.
    
    To claim it, you need to complete the payment within 7 days of the assignment.
    The current price is <code>{$price}</code> ₽. {passes-payment-method}
    
    You can use this interface to:
    - Edit your name
    - Upload proof of payment
    - Cancel the assignment
    
    Please note that cancellations or missing the payment deadline are irreversible.
passes-pass-edit-payed=
    Hello, <i>{$name}</i>!
    Congratulations! The {passes-pass-description} is now officially yours as you have successfully paid for it.
    You can change the name associated with the pass if necessary.
    If you need to return the pass and request a refund, please contact the administrator.
passes-error-couple-not-found =
    Something went wrong and I can't find your couple to proceed with your pass registration.
    Maybe they cancelled it and my message got lost, I don't know.
    Please register again if possible using the command /passes.
passes-payment-admin-button = {$adminEmoji} {$adminName}
passes-payment-admin-desc=
    {$adminEmoji} {$adminLink} {$phoneSBP ->
        [nosbp] — payment: mostly only cash.
        [paypal] — payment: PayPal transfers.
        *[sbp] — paymnent: SBP transfers by phone number.
    }
passes-admin-changed =
    Your ambassador has been changed to {$adminLink}.
    This is related to the pass: {passes-pass-description}.
    Please consult the new ambassador or change them in the /passes interface if needed.
passes-pass-role-saved =
    Now select your preferred ambassador. You can also change them afterwards.
    
    {$adminTexts}
passes-pass-admin-saved =
    Now select pass type:
passes-button-solo = 👤 Solo
passes-button-couple = 👥 Couple
passes-choose-admin =
    Please select the paymet admin for your region or the one you trust the most.
    Do not change the admin if you've already paid, or change back to the one you've paid.
passes-couple-request-edit =
    Please scroll down to select your couple.
passes-couple-request-message =
    For a couple pass, I need your couple's account.
    To send it, simply forward me any message sent by your couple. It can be anything, even a sticker or an emoji, message contents won't be stored or processed.
    Go to the chat with your couple, select any message and forward it to me.
passes-couple-request-timeout =
    I didn't get your couple's message in time. You can try again using /passes command.
passes-couple-request-cancelled =
    Couple pass is cancelled. You can try again using /passes command.
passes-couple-request-wrong-data =
    I asked to give me a repost of your couple's message, but I can't process the message you just sent me.
    If it was really a repost as I asked, then it might be that your couple has a limitation on who can view their profile.
    Please ask them to disable this limitation or to try registering instead of you.
    
    To disable the limitation, go to ≡ → Settings → Privacy and Security → Forwarded messages,
    There either disable the limitation, or add me as an exception.

    You can try again using /passes command.
passes-couple-request-invitee-payed =
    Oops, this person is already registered and has a pass. You can try again using /passes command.
passes-couple-invitation =
    A user <i>{$coupleName}</i>, {$coupleLink} invited you to {passes-pass-title-long}.
    They invited you as a couple for a couple pass, for a role of {$coupleRole ->
        [leader] follower
        *[follower] leader
    }.
    Please accept or decline the invitation:
passes-button-change-admin = 🔄🧑🏼‍💼 Change admin
passes-button-couple-accept = ✅ Accept
passes-button-couple-decline = 🙅 Decline
passes-accept-pass-request-name =
    To accept the invitation, first enter your full name please:
passes-invitation-successfully-accepted =
    The invitation was successfully accepted.
    To view or manage your pass, use command /passes.
passes-invitation-accept-failed =
    Seems like the invitation is no longer valid. Either it has timed out or was cancelled by the inviter.
passes-invitation-successfully-declined =
    The invitation to {passes-pass-title-long} was successfully declined.
passes-invitation-was-accepted =
    A user <i>{$coupleName}</i>, {$couplelink} accepted your invitation for a couple pass to {passes-pass-title-long}.
passes-invitation-was-declined =
    Unfortunately, the user <i>{$coupleName}</i>, {$couplelink} declined your invitation for a couple pass to {passes-pass-title-long}.
    You can choose a solo pass or select another person.
passes-couple-saved =
    I saved your invitation, but your couple is not registered and I can't send them the invitation.
    Ask them to start conversation with me by sending me the /start command, and then accept your invitation by sending me the /passes command and selecting {passes-pass-title-short}.
    The invitation is valid for 2 days.
passes-couple-saved-sent =
    I sent your invitation to your couple.
    Ask them to accept it. If it didn't appear in their messages, they can access it through the /passes command, in {passes-pass-title-short}.
    The invitation is valid for 2 days.
passes-pass-create-cancel=
    If you want to register for a pass, you can come back using /passes command.
passes-pass-exit=
    If you need to view your pass, provide payment proof or change name, you can get back by calling /passes command again.
passes-pass-cancelled=
    Your pass assignment has been cancelled. If you want to get back again, you can call /passes command any time.
passes-pass-cancel-failed =
    Cancellation failed. Either the pass was marked paid or was already cancelled.
passes-payment-request-callback-message =
    You need to pay <code>{$price}</code> ₽.
    {passes-payment-method}
    Scroll down to send the proof of payment.
passes-payment-request-waiting-message =
    You need to pay <code>{$price}</code> ₽.
    {passes-payment-method}
    Please send the proof of payment in PDF format or as an image.
passes-payment-proof-timeout =
    I haven't received your payment proof in time. Please use the /passes command to try again.
    Don't worry, your position in the waitlist hasn't changed.
passes-payment-proof-cancelled =
    If you need to resubmit your payment proof or view your pass details, use the /passes command.
    Don't worry, your position in the waitlist hasn't changed.
passes-payment-proof-accepted =
    Hello, <i>{$name}</i>!
    Congratulations! The {passes-pass-description} is now officially yours, as your payment has been successfully approved by the administrator.
    You can view your pass details by using the /passes command.
passes-payment-proof-rejected =
    Hello, <i>{$name}</i>!
    Unfortunately, your payment proof was rejected by the administrator.
    You can view your pass details or submit another proof by using the /passes command.
    Don't wait too long — secure your spot before it's gone!
passes-name-timeout =
    I haven't received your name in time. Please use the /passes command to try again.
passes-payment-proof-wrong-data =
    I was expecting a PDF file or an image as proof of payment, but this message doesn't seem to match that.
    Sorry about the confusion.
    To try again, please use the /passes command and submit a PDF file or an image.
passes-payment-proof-forwarded =
    I've sent your payment proof to our administrators for verification. They will review it, and I'll update you with the result soon.
    If you need to view your pass details in the meantime, use the /passes command.
passes-select-type-message =
    Select, which Zouk Non Stop marathon we will work with:
passes-select-type-button =
    {passes-pass-country-emoji} {passes-pass-title-short}
passes-sell-not-started =
    Please be patient; sales have not opened yet.
passes-solo-saved =
    Solo pass chosen.
passes-pass-create-start-message =
    First, I need some information from you.
    Scroll down and enter your full name as it appears in your passport.
passes-pass-title-long =
    {$passKey ->
        [pass_2025_2] Zouk Non Stop in Grodno
        [pass_2026_1] Zouk Non Stop in Yekaterinburg
        *[pass20251] Zouk Non Stop in Moscow
    }
passes-pass-title-short =
    {$passKey ->
        [pass_2025_2] ZNS Grodno
        [pass_2026_1] ZNS Yekaterinburg
        *[pass20251] ZNS Moscow
    }
passes-pass-country-emoji =
    {$passKey ->
        [pass_2025_2] 🇧🇾
        *[pass20251] 🇷🇺
    }
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
passes-promopass-select-role =
    You have registered for Zouk Non Stop pass and are in the waitlist, but haven't yet selected a pass type.
    You won't loose your position in the waitlist if you change the pass to couple pass now.
    In 2 days all the passes will be automatically converted to solo passes.
passes-payment-deadline-close =
    Warning: Your pass payment deadline is just 1 day away. Act quickly to secure your spot!
    See /passes for more details.
passes-payment-deadline-exceeded =
    Unfortunately, your payment deadline has been exceeded, and the pass is no longer reserved for you.
    If you would like to try again, use the /passes command.
passes-couple-didnt-answer =
    Unfortunately, I didn't get an answer from your couple in time.
    You can send them an invitation again, or choose another person, or a solo pass.
passes-invitation-timeout =
    Sorry, the invitation I've sent earlier has timed out. You can register again or see your pass in /passes.
passes-passport-request-message=
    Please enter your passport series and number, like <code>AB 1234567</code>. If you have it different, write exactly as it is in your passport.
    It is required to enter the event venue.
passes-passport-changed-message =
    Your passport series and number are updated: <b>{$passportNumber}</b>.
passes-passport-timeout =
    I haven't received your passport number in time. Please use the /passes command to try again.
passes-passport-data-required-beginning-message = 
    Please enter your passport data.
passes-passport-data-required =
    For you to be able to enter the event, I need to collect your passport data.
    This information must be provided as soon as possible and will be transferred
    to the venue security control.
    ⚠️‼️ <b>Deadline tomorrow, <u>June 3rd, 23:59.</u></b> ⚠️‼️

    Please take a moment and review or enter your passport data using a button below.
    Wrong data may result in you being unable to enter the event.
passes-passport-data-button = ⚠️‼️📝 Enter passport data⚠️


user-is-restricted = Some actions have been disabled.
user-is-none = Maybe run /start first?

menu-alert-lunch-incomplete = Order is incomplete. Please fill all required lunch items for selected lunches.
menu-confirm-dinner-empty = You have not selected any items for dinner on one or more days. Proceed anyway?

food-button-exit = 🚪 Exit
food-message-exited = If you need to manage your food order again, you can always use the /food command.
