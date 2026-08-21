import {calculateMealService} from "./orders_service.mjs";

const EXTRA_PRICES = {
    preparty: 35,
    excursion_minsk: 30,
    shuttle: 65,
    excursion_grodno: 25,
    excursion_grodno_overview: 25,
    excursion_grodno_gorodnitsa: 25,
};
const BYN_TO_RUB = 30;

let menuData = null;
let activeContentIcon = null;
let orderSubmissionPending = false;
let legacyGrodnoExcursion = false;

function currentLanguage() {
    return document.documentElement.lang.toLowerCase().startsWith("en") ? "en" : "ru";
}

function closeContentCaption() {
    if (!activeContentIcon) return;
    activeContentIcon.classList.remove("active");
    activeContentIcon.setAttribute("aria-expanded", "false");
    const caption = activeContentIcon.closest(".name-text")?.querySelector(".content-caption");
    if (caption) caption.hidden = true;
    activeContentIcon = null;
}

function toggleContentCaption(icon, caption, label, emoji) {
    const shouldClose = activeContentIcon === icon;
    closeContentCaption();
    if (shouldClose) return;

    caption.textContent = `${emoji} ${label}`;
    caption.hidden = false;
    icon.classList.add("active");
    icon.setAttribute("aria-expanded", "true");
    activeContentIcon = icon;
}

function appendLocalizedText(container, ru, en) {
    const ruText = document.createElement("span");
    ruText.lang = "ru";
    ruText.textContent = ru || "";
    container.appendChild(ruText);

    const enText = document.createElement("span");
    enText.lang = "en";
    enText.textContent = en || "";
    container.appendChild(enText);
}

function createDish(dishKey, dish) {
    const language = currentLanguage();
    const dishDiv = document.createElement("div");
    dishDiv.dataset.name = dishKey;
    dishDiv.dataset.service = JSON.stringify(dish.service || []);
    dishDiv.classList.add("dish");

    const nameDiv = document.createElement("div");
    nameDiv.classList.add("name");

    if (dish.image) {
        const img = document.createElement("img");
        img.classList.add("dish-thumb");
        img.loading = "lazy";
        img.alt = dish[`name_${language}`] || dish.name_en || dish.name_ru || "";
        img.src = `static/orders_photos/${dish.image}`;
        nameDiv.appendChild(img);
        nameDiv.addEventListener("click", () => toggleFullImage(img.src, img.alt));
    }

    const nameText = document.createElement("div");
    nameText.classList.add("name-text");
    appendLocalizedText(nameText, dish.name_ru, dish.name_en);

    if (dish.ingredients_ru || dish.ingredients_en) {
        const ingredients = document.createElement("div");
        ingredients.classList.add("ingredients");
        appendLocalizedText(
            ingredients,
            dish.ingredients_ru ? `Состав: ${dish.ingredients_ru}` : "",
            dish.ingredients_en ? `Ingredients: ${dish.ingredients_en}` : "",
        );
        nameText.appendChild(ingredients);
    }

    const contents = document.createElement("div");
    contents.classList.add("contents");
    const contentCaption = document.createElement("div");
    contentCaption.classList.add("content-caption");
    contentCaption.hidden = true;
    contentCaption.setAttribute("aria-live", "polite");
    for (const contentKey of dish.contents || []) {
        const content = menuData.content_icons[contentKey];
        if (!content) continue;
        const icon = document.createElement("span");
        icon.classList.add("content-icon");
        icon.textContent = content.icon;
        const label = content[language] || content.en || content.ru;
        icon.title = label;
        icon.tabIndex = 0;
        icon.setAttribute("role", "button");
        icon.setAttribute("aria-label", label);
        icon.setAttribute("aria-expanded", "false");
        icon.addEventListener("click", event => {
            event.stopPropagation();
            toggleContentCaption(icon, contentCaption, label, content.icon);
        });
        icon.addEventListener("keydown", event => {
            if (event.key !== "Enter" && event.key !== " ") return;
            event.preventDefault();
            event.stopPropagation();
            toggleContentCaption(icon, contentCaption, label, content.icon);
        });
        contents.appendChild(icon);
    }
    nameText.appendChild(contents);
    nameText.appendChild(contentCaption);
    nameDiv.appendChild(nameText);
    dishDiv.appendChild(nameDiv);

    const priceDiv = document.createElement("div");
    priceDiv.classList.add("price");
    priceDiv.textContent = dish.price;
    dishDiv.appendChild(priceDiv);

    const counterDiv = document.createElement("div");
    counterDiv.classList.add("counter");
    counterDiv.textContent = "0";
    dishDiv.appendChild(counterDiv);

    if (!read_only) {
        const addButton = document.createElement("button");
        addButton.classList.add("add");
        addButton.type = "button";
        addButton.textContent = "+";
        addButton.addEventListener("click", () => {
            counterDiv.textContent = String(Number(counterDiv.textContent) + 1);
            refreshOrderPreview();
        });
        dishDiv.appendChild(addButton);

        const removeButton = document.createElement("button");
        removeButton.classList.add("remove");
        removeButton.type = "button";
        removeButton.textContent = "−";
        removeButton.addEventListener("click", () => {
            counterDiv.textContent = String(Math.max(0, Number(counterDiv.textContent) - 1));
            refreshOrderPreview();
        });
        dishDiv.appendChild(removeButton);

        const clearButton = document.createElement("button");
        clearButton.classList.add("clear");
        clearButton.type = "button";
        clearButton.textContent = "🗑";
        clearButton.addEventListener("click", () => {
            counterDiv.textContent = "0";
            refreshOrderPreview();
        });
        dishDiv.appendChild(clearButton);
    }

    const outputDiv = document.createElement("div");
    outputDiv.classList.add("output");
    outputDiv.textContent = dish.output || "";
    dishDiv.appendChild(outputDiv);

    return dishDiv;
}

function createServiceSummary() {
    const summary = document.createElement("div");
    summary.classList.add("service-summary");
    summary.hidden = true;

    const title = document.createElement("div");
    title.classList.add("service-title");
    appendLocalizedText(title, "Автоматически к этому приёму пищи", "Added automatically to this meal");
    summary.appendChild(title);

    const items = document.createElement("ul");
    items.classList.add("service-items");
    summary.appendChild(items);
    return summary;
}

function fillAllSections() {
    for (const section of document.querySelectorAll("section.meal")) {
        const day = [...section.classList].find(className => menuData.choices[className]);
        const mealtime = [...section.classList].find(className => ["lunch", "dinner"].includes(className));
        const choices = menuData.choices[day]?.[mealtime];
        if (!choices) continue;

        section.querySelectorAll(".dish-group, .service-summary").forEach(element => element.remove());

        for (const [dishType, dishKeys] of Object.entries(choices)) {
            const group = document.createElement("div");
            group.classList.add("dish-group", dishType);

            const typeName = document.createElement("div");
            typeName.classList.add("dish-type");
            const label = menuData.category_labels[dishType] || {};
            appendLocalizedText(typeName, label.ru || dishType, label.en || dishType);
            group.appendChild(typeName);

            const dishes = document.createElement("div");
            dishes.classList.add("dishes");
            for (const dishKey of dishKeys) {
                const dish = menuData.dishes[dishKey];
                if (dish) dishes.appendChild(createDish(dishKey, dish));
            }
            group.appendChild(dishes);
            section.appendChild(group);
        }

        section.appendChild(createServiceSummary());
    }
}

function collectOrdersWithExtras() {
    const orders = {
        total: 0,
        days: {},
        extras: {total: 0},
        customer: "",
    };

    orders.customer_first_name = document.querySelector('.for-who input[name="for_who_first_name"]').value.trim();
    orders.customer_last_name = document.querySelector('.for-who input[name="for_who_last_name"]').value.trim();
    orders.customer_patronymus = document.querySelector('.for-who input[name="for_who_patronymus"]').value.trim();
    orders.customer = `${orders.customer_first_name} ${orders.customer_patronymus ? `${orders.customer_patronymus} ` : ""}${orders.customer_last_name}`;

    for (const meal of document.querySelectorAll(".meal")) {
        const day = [...meal.classList].find(className => ["friday", "saturday", "sunday"].includes(className));
        const mealtime = [...meal.classList].find(className => ["lunch", "dinner"].includes(className));
        if (!orders.days[day]) orders.days[day] = {total: 0, mealtimes: {}};

        const mealOrder = {total: 0, dishes: []};
        const selectedForService = [];
        for (const dish of meal.querySelectorAll(".dishes > .dish")) {
            const count = Number.parseInt(dish.querySelector(".counter").textContent.trim(), 10);
            if (count <= 0) continue;

            const price = Number.parseFloat(dish.querySelector(".price").textContent.trim());
            const dishInfo = {
                name: dish.dataset.name,
                count,
                price,
                total: count * price,
            };
            mealOrder.dishes.push(dishInfo);
            mealOrder.total += dishInfo.total;
            selectedForService.push({
                count,
                service: JSON.parse(dish.dataset.service || "[]"),
            });
        }

        mealOrder.service = calculateMealService(selectedForService, menuData.service_items);
        mealOrder.total += mealOrder.service.total;
        orders.days[day].mealtimes[mealtime] = mealOrder;
        orders.days[day].total += mealOrder.total;
        orders.total += mealOrder.total;
    }

    const extras = [
        ["preparty", "preparty"],
        ["excursion_minsk", "excursion_minsk"],
        ["shuttle_bus", "shuttle"],
    ];
    for (const [inputName, orderKey] of extras) {
        if (document.querySelector(`.excursions input[name="${inputName}"]`).checked) {
            const price = EXTRA_PRICES[orderKey];
            orders.extras[orderKey] = price;
            orders.extras.total += price;
            orders.total += price;
        }
    }

    const grodnoToggle = document.querySelector('.excursions input[name="excursion_grodno"]');
    const grodnoVariant = document.querySelector('.excursions input[name="grodno_excursion_variant"]:checked');
    if (grodnoToggle.checked && grodnoVariant) {
        const orderKey = grodnoVariant.value;
        const price = EXTRA_PRICES[orderKey];
        orders.extras[orderKey] = price;
        orders.extras.total += price;
        orders.total += price;
    } else if (grodnoToggle.checked && legacyGrodnoExcursion) {
        const price = EXTRA_PRICES.excursion_grodno;
        orders.extras.excursion_grodno = price;
        orders.extras.total += price;
        orders.total += price;
    }

    return orders;
}

function formatPrice(value) {
    return Number(value.toFixed(2)).toString();
}

function renderServiceSummaries(orders) {
    for (const meal of document.querySelectorAll(".meal")) {
        const day = [...meal.classList].find(className => ["friday", "saturday", "sunday"].includes(className));
        const mealtime = [...meal.classList].find(className => ["lunch", "dinner"].includes(className));
        const service = orders.days[day]?.mealtimes[mealtime]?.service;
        const summary = meal.querySelector(".service-summary");
        const list = summary.querySelector(".service-items");
        list.replaceChildren();

        if (!service || service.items.length === 0) {
            summary.hidden = true;
            continue;
        }

        for (const item of service.items) {
            const definition = menuData.service_items[item.name];
            const row = document.createElement("li");
            const name = document.createElement("span");
            name.textContent = `${definition.icon} `;
            appendLocalizedText(name, definition.name_ru, definition.name_en);
            row.appendChild(name);

            const amount = document.createElement("span");
            amount.textContent = `×${item.count} · ${formatPrice(item.total)} BYN`;
            row.appendChild(amount);
            list.appendChild(row);
        }
        summary.hidden = false;
    }
}

function refreshOrderPreview() {
    if (!menuData) return;
    const orders = collectOrdersWithExtras();
    renderServiceSummaries(orders);
    const total = currencyCeil(orders.total);
    const totalRub = currencyCeil(orders.total * BYN_TO_RUB);
    document.getElementById("total-sum").textContent = `${total} BYN ${totalRub} RUB`;
}

function fillInOrders(orders) {
    if (!orders || typeof orders !== "object") return;

    const fields = ["first_name", "last_name", "patronymus"];
    for (const field of fields) {
        const value = orders[`customer_${field}`];
        if (typeof value === "string") {
            document.querySelector(`.for-who input[name="for_who_${field}"]`).value = value.trim();
        }
    }

    for (const [day, dayOrder] of Object.entries(orders.days || {})) {
        for (const [mealtime, mealOrder] of Object.entries(dayOrder.mealtimes || {})) {
            const meal = document.querySelector(`.${day}.${mealtime}.meal`);
            if (!meal) continue;
            for (const dish of mealOrder.dishes || []) {
                const dishElement = [...meal.querySelectorAll(".dishes > .dish")]
                    .find(element => element.dataset.name === dish.name);
                if (dishElement) dishElement.querySelector(".counter").textContent = dish.count;
            }
        }
    }

    const extras = orders.extras || {};
    document.querySelector('.excursions input[name="preparty"]').checked = "preparty" in extras;
    document.querySelector('.excursions input[name="excursion_minsk"]').checked = "excursion_minsk" in extras;
    document.querySelector('.excursions input[name="shuttle_bus"]').checked = "shuttle" in extras;
    const grodnoServices = ["excursion_grodno_overview", "excursion_grodno_gorodnitsa"];
    const grodnoToggle = document.querySelector('.excursions input[name="excursion_grodno"]');
    legacyGrodnoExcursion = "excursion_grodno" in extras;
    grodnoToggle.checked = legacyGrodnoExcursion || grodnoServices.some(service => service in extras);
    for (const service of grodnoServices) {
        document.querySelector(`.excursions input[name="grodno_excursion_variant"][value="${service}"]`).checked = service in extras;
    }
    syncGrodnoExcursionChoice();
}

function setReadOnly() {
    if (!read_only) return;
    document.querySelectorAll(".for-who input, .excursions input").forEach(input => {
        input.disabled = true;
    });
}

const sections = document.querySelectorAll("body > section");
let currentIndex = 0;

function updateSections() {
    sections.forEach((section, index) => section.classList.toggle("active", index === currentIndex));
    Telegram.WebApp.MainButton.setText(currentIndex === sections.length - 1 ? finish_button_text : next_button_text);
}

let nameValidity;
try {
    const pattern = "^\\s*\\p{Uppercase_Letter}\\p{Lowercase_Letter}+\\s*$";
    const namePattern = /^\s*\p{Uppercase_Letter}\p{Lowercase_Letter}+\s*$/u;
    nameValidity = (element, error) => {
        element.setCustomValidity(namePattern.test(element.value) ? "" : error);
    };
    document.querySelector('.for-who input[name="for_who_first_name"]').setAttribute("pattern", pattern);
    document.querySelector('.for-who input[name="for_who_last_name"]').setAttribute("pattern", pattern);
} catch (error) {
    sendError(error);
    const pattern = "^\\s*[A-ZА-ЯЁ][a-zа-яё]+\\s*$";
    const namePattern = /^\s*[A-ZА-ЯЁ][a-zа-яё]+\s*$/u;
    nameValidity = (element, message) => {
        element.setCustomValidity(namePattern.test(element.value) ? "" : message);
    };
    document.querySelector('.for-who input[name="for_who_first_name"]').setAttribute("pattern", pattern);
    document.querySelector('.for-who input[name="for_who_last_name"]').setAttribute("pattern", pattern);
}

function validateSection(index) {
    nameValidity(document.querySelector('.for-who input[name="for_who_first_name"]'), validity_error_first_name);
    nameValidity(document.querySelector('.for-who input[name="for_who_last_name"]'), validity_error_last_name);
    for (const input of sections[index].querySelectorAll("input")) {
        if (!input.checkValidity()) {
            input.reportValidity();
            return false;
        }
    }
    return true;
}

function currencyCeil(sum) {
    return Math.ceil(sum * 100) / 100;
}

function initDataQuery() {
    return `initData=${encodeURIComponent(Telegram.WebApp.initData)}`;
}

function sendError(error) {
    return fetch(`error?${initDataQuery()}`, {
        method: "POST",
        body: String(error),
    });
}

function markShuttleUnavailable() {
    const shuttleInput = document.querySelector('.excursions input[name="shuttle_bus"]');
    shuttleInput.checked = false;
    shuttleInput.disabled = true;
    const label = shuttleInput.closest("label");
    label.classList.add("unavailable");
    label.querySelector(".shuttle-sold-out").hidden = false;
    refreshOrderPreview();
}

function syncGrodnoExcursionChoice() {
    const toggle = document.querySelector('.excursions input[name="excursion_grodno"]');
    const options = document.querySelector(".grodno-excursion-options");
    const radios = options.querySelectorAll('input[name="grodno_excursion_variant"]');
    options.hidden = !toggle.checked;
    for (const radio of radios) radio.required = toggle.checked;
    if (!toggle.checked) {
        for (const radio of radios) radio.checked = false;
    }
}

function setGrodnoExcursionAvailability() {
    const services = ["excursion_grodno_overview", "excursion_grodno_gorodnitsa"];
    for (const service of services) {
        if (grodno_excursion_availability[service] !== false) continue;
        const label = document.querySelector(`[data-excursion-service="${service}"]`);
        const radio = label.querySelector('input[name="grodno_excursion_variant"]');
        radio.disabled = true;
        label.classList.add("unavailable");
        label.querySelector(".excursion-sold-out").hidden = false;
    }
    const toggle = document.querySelector('.excursions input[name="excursion_grodno"]');
    const hasAvailableVariant = services.some(service => grodno_excursion_availability[service] !== false);
    if (!hasAvailableVariant && !toggle.checked) toggle.disabled = true;
}

function markGrodnoExcursionFull(service) {
    grodno_excursion_availability[service] = false;
    const radio = document.querySelector(`input[name="grodno_excursion_variant"][value="${service}"]`);
    radio.checked = false;
    setGrodnoExcursionAvailability();
    const hasAvailableVariant = [...document.querySelectorAll('input[name="grodno_excursion_variant"]')]
        .some(input => !input.disabled);
    if (!hasAvailableVariant) {
        document.querySelector('input[name="excursion_grodno"]').checked = false;
        syncGrodnoExcursionChoice();
    }
    refreshOrderPreview();
    currentIndex = [...sections].indexOf(document.querySelector("section.excursions"));
    updateSections();
}

function showUserAlert(message) {
    if (typeof Telegram.WebApp.showAlert === "function") {
        Telegram.WebApp.showAlert(message);
    } else {
        window.alert(message);
    }
}

async function submitOrder() {
    if (orderSubmissionPending) return;
    orderSubmissionPending = true;
    Telegram.WebApp.MainButton.disable();
    Telegram.WebApp.MainButton.showProgress?.();
    try {
        const orders = collectOrdersWithExtras();
        const response = await fetch(`orders?${initDataQuery()}&order_id=${user_order_id}`, {
            method: "POST",
            headers: {"Content-Type": "application/json"},
            body: JSON.stringify(orders),
        });
        if (response.status === 409) {
            const result = await response.json().catch(() => ({}));
            if (result.error === "shuttle_full") {
                markShuttleUnavailable();
                showUserAlert(shuttle_full_error);
                return;
            }
            if (result.error === "capacity_full" && result.service in grodno_excursion_full_errors) {
                markGrodnoExcursionFull(result.service);
                showUserAlert(grodno_excursion_full_errors[result.service]);
                return;
            }
        }
        if (!response.ok) {
            throw new Error(`Network response was not ok: ${response.status} ${response.statusText}`);
        }
        Telegram.WebApp.close();
    } catch (error) {
        sendError(error);
    } finally {
        orderSubmissionPending = false;
        Telegram.WebApp.MainButton.hideProgress?.();
        Telegram.WebApp.MainButton.enable();
    }
}

function mainButtonClick() {
    if (currentIndex === sections.length - 1) {
        if (read_only) {
            Telegram.WebApp.close();
            return;
        }
        submitOrder();
        return;
    }

    if (currentIndex < sections.length - 1 && validateSection(currentIndex)) {
        currentIndex += 1;
        updateSections();
    }
}

function backButtonClick() {
    if (currentIndex > 0) {
        currentIndex -= 1;
        updateSections();
    } else {
        Telegram.WebApp.close();
    }
}

function toggleFullImage(src, alt = "") {
    const existing = document.getElementById("image-overlay");
    if (existing) {
        const sameImage = existing.dataset.src === src;
        existing.remove();
        if (sameImage) return;
    }

    const overlay = document.createElement("div");
    overlay.id = "image-overlay";
    overlay.dataset.src = src;
    const img = document.createElement("img");
    img.src = src;
    img.alt = alt;
    overlay.appendChild(img);
    overlay.addEventListener("click", () => overlay.remove());
    document.body.appendChild(overlay);
}

Telegram.WebApp.ready();
Telegram.WebApp.expand();
Telegram.WebApp.MainButton.onClick(mainButtonClick);
Telegram.WebApp.MainButton.enable();
Telegram.WebApp.MainButton.show();
Telegram.WebApp.BackButton.onClick(backButtonClick);
Telegram.WebApp.BackButton.show();
window.mainButtonClick = mainButtonClick;
window.backButtonClick = backButtonClick;
document.body.addEventListener("change", event => {
    if (event.target.matches('input[name="excursion_grodno"]')) {
        if (!event.target.checked) legacyGrodnoExcursion = false;
        syncGrodnoExcursionChoice();
    }
    if (event.target.matches('input[name="grodno_excursion_variant"]')) {
        legacyGrodnoExcursion = false;
    }
    refreshOrderPreview();
});
document.body.addEventListener("click", closeContentCaption);
updateSections();
setReadOnly();

fetch("static/menu_belarus.json")
    .then(response => {
        if (!response.ok) throw new Error(`Menu response was not ok: ${response.status}`);
        return response.json();
    })
    .then(menu => {
        menuData = menu;
        fillAllSections();
        fillInOrders(user_order);
        setGrodnoExcursionAvailability();
        if (!shuttle_available) markShuttleUnavailable();
        refreshOrderPreview();
    })
    .catch(sendError);
