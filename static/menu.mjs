(async function () {

    function findParent(element, query) {
        let parent = element.parentElement;
        while (parent && !parent.matches(query)) {
            parent = parent.parentElement;
        }
        return parent;
    }

    let menu = await fetch("static/menu_2025_1.json").then(r => r.json());
    for (let day in menu) {
        let meal = "lunch";
        let lunch = menu[day][meal];
        for (let i = 0; i < lunch.length; ++i) {
            let item = lunch[i];
            if (item.skip) continue;

            let container = document.createElement("div");
            container.classList.add("item");
            container.classList.add(item.category);
            let url = item.photo;
            if (url && !url.startsWith("https://")) {
                url = "static/menu_photos/" + url + ".jpg";
            }
            let innerHTML = `
                <label>
                    <input type="radio" name="${day}-${meal}-${item.category}" value="${i}" />
                    <span class="name" lang="ru">${item.title_ru}</span>
                    <span class="name" lang="en">${item.title_en}</span>
            `;
            if (url) {
                innerHTML += `
                    <img class="small_photo" loading="lazy" src="${url}">
                `;
            }
            innerHTML += `
                </label>
                <button class="description-button">i</button>
                <div class="description">
                    <span class="name" lang="ru">${item.title_ru}</span>
                    <span class="name" lang="en">${item.title_en}</span>
            `;
            if (url) {
                innerHTML += `
                    <img class="photo" loading="lazy" src="${url}">
                `;
            }
            if (item.ingredients_en && item.ingredients_en.length > 0) {
                innerHTML += `
                    <div class="ingredients">
                        <span lang="ru">Ингредиенты: ${item.ingredients_ru}</span>
                        <span lang="en">Ingredients: ${item.ingredients_en}</span>
                    </div>
                `;
            }
            if (item.weight && item.weight.value > 0) {
                innerHTML += `
                    <div class="weight">${item.weight.value}</div>
                `;
            }
            if (item.nutrition) {
                for (let type of ["calories", "fat", "carbohydrates", "protein"]) {
                    if (type in item.nutrition) {
                        innerHTML += `
                            <div class="nutrition ${type}">${item.nutrition[type]}</div>
                        `;
                    }
                }
            }
            innerHTML += `
                </div>
            `;
            container.innerHTML = innerHTML;
            document.querySelector(`section.${day}.${meal} .combo-class.${item.category} .choices`).appendChild(container);
        }
        meal = "dinner";
        let dinner = menu[day][meal];
        for (let i = 0; i < dinner.length; ++i) {
            let item = dinner[i];
            if (item.skip) continue;
            if (!item.price) {
                console.warn("Item has no price", i, item);
                continue;
            }

            let container = document.createElement("div");
            container.classList.add("item");
            container.classList.add(item.category);
            let url = item.photo;
            if (url && !url.startsWith("https://")) {
                url = "static/menu_photos/" + url + ".jpg";
            }
            let innerHTML = `
                <label>
                    <input type="checkbox" name="${day}-${meal}-${i}" value="${i}" />
                    <span class="name" lang="ru">${item.title_ru}</span>
                    <span class="name" lang="en">${item.title_en}</span>
                    <span class="price">${item.price}</span>
            `;
            if (url) {
                innerHTML += `
                    <img class="small_photo" loading="lazy" src="${url}">
                `;
            }
            innerHTML += `
                </label>
                <button class="description-button">i</button>
                <div class="description">
                    <span class="name" lang="ru">${item.title_ru}</span>
                    <span class="name" lang="en">${item.title_en}</span>
            `;
            if (url) {
                innerHTML += `
                    <img class="photo" loading="lazy" src="${url}">
                `;
            }
            if (item.ingredients_en && item.ingredients_en.length > 0) {
                innerHTML += `
                    <div class="ingredients">
                        <span lang="ru">Ингредиенты: ${item.ingredients_ru}</span>
                        <span lang="en">Ingredients: ${item.ingredients_en}</span>
                    </div>
                `;
            }
            if (item.weight && item.weight.value > 0) {
                innerHTML += `
                    <div class="weight">${item.weight.value}</div>
                `;
            }
            if (item.nutrition) {
                for (let type of ["calories", "fat", "carbohydrates", "protein"]) {
                    if (type in item.nutrition) {
                        innerHTML += `
                            <div class="nutrition ${type}">${item.nutrition[type]}</div>
                        `;
                    }
                }
            }
            innerHTML += `
                </div>
            `;
            container.innerHTML = innerHTML;
            document.querySelector(`section.${day}.${meal} .meals`).appendChild(container);
        }
    }

    function IDQ() {
        return "initData=" + encodeURIComponent(Telegram.WebApp.initData)
    }

    function send_error(err) {
        return fetch("error?" + IDQ(), {
            method: "POST",
            body: err
        })
    }

    async function save(autosave = false) { // Added default for autosave
        if (read_only && !autosave) { // Allow autosave to proceed if it was somehow called in read_only, but block manual save.
            console.log("Manual save is disabled in read-only mode.");
            return;
        }
        // If read_only is true, the autosave setup itself in the IIFE already prevents calling save(true).
        // So, if(read_only) return; is also fine if we assume autosave is never initiated.

        // Ensure window.user_order is up-to-date before any save logic, for both manual and auto saves.
        calculateTotalSumAndOrder();

        let orderIsIncomplete = false;
        if (!autosave) { // Validation and highlighting only for manual saves (button press)
            document.querySelectorAll('.combo-class.unfilled').forEach(el => el.classList.remove('unfilled')); // Clear previous highlights

            const days = Object.keys(menu);
            const LUNCH_SUB_CATEGORIES = ["main", "side", "salad"];

            days.forEach(day => {
                const lunchSection = document.querySelector(`section.${day}.lunch`);
                if (!lunchSection || !menu[day] || !menu[day].lunch) return;

                const noLunchRadio = lunchSection.querySelector(`input[name="${day}-lunch-soup"][value="no-lunch"]:checked`);

                if (noLunchRadio) {
                    // "No lunch" is selected, remove any unfilled class from this day's lunch categories
                    lunchSection.querySelectorAll('.combo-class.soup, .combo-class.main, .combo-class.side, .combo-class.salad')
                        .forEach(el => el.classList.remove('unfilled'));
                    return; // Skip validation for this day's lunch
                }

                // "No lunch" is NOT selected. Validate soup, main, side, salad.

                // 1. Validate soup choice (includes "no-soup" or a specific soup)
                const soupChoiceMade = lunchSection.querySelector(`input[name="${day}-lunch-soup"]:checked`);
                const soupComboEl = lunchSection.querySelector('.combo-class.soup');
                if (!soupChoiceMade) { // No selection in the soup radio group
                    if (soupComboEl) soupComboEl.classList.add('unfilled');
                    orderIsIncomplete = true;
                } else {
                    if (soupComboEl) soupComboEl.classList.remove('unfilled');
                }

                // 2. Validate main, side, salad choices
                LUNCH_SUB_CATEGORIES.forEach(category => {
                    const categoryEl = lunchSection.querySelector(`.combo-class.${category}`);
                    const itemSelected = lunchSection.querySelector(`.combo-class.${category} .choices input[name="${day}-lunch-${category}"]:checked`);
                    if (!itemSelected) {
                        if (categoryEl) categoryEl.classList.add('unfilled');
                        orderIsIncomplete = true;
                    } else {
                        if (categoryEl) categoryEl.classList.remove('unfilled');
                    }
                });
            });

            if (orderIsIncomplete) {
                const message = (typeof alert_lunch_incomplete_text !== 'undefined' && alert_lunch_incomplete_text)
                    ? alert_lunch_incomplete_text
                    : "Order is incomplete. Please fill all required lunch items for selected lunches.";
                await showCustomAlert(message);
                return "user_validation_failed";
            }

            let someDinnerIsEmpty = false;
            if (collectedOrder) {
                for (const day in menu) { // Iterate through days defined in the master menu
                    // Check if dinner is offered for this day in the menu
                    if (menu[day] && menu[day].dinner && menu[day].dinner.length > 0) {
                        // Check if the order for this day has an empty dinner array
                        if (collectedOrder[day] && collectedOrder[day].dinner && collectedOrder[day].dinner.length === 0) {
                            someDinnerIsEmpty = true;
                            break; // Found one, no need to check further
                        }
                    }
                }
            }

            if (someDinnerIsEmpty) {
                const dinnerConfirmText = (typeof confirm_dinner_empty_text !== 'undefined' && confirm_dinner_empty_text)
                    ? confirm_dinner_empty_text
                    : "You have not selected any items for dinner on one or more days. Proceed anyway?";
                const proceedWithEmptyDinner = await showCustomConfirm(dinnerConfirmText);

                if (!proceedWithEmptyDinner) {
                    return "user_validation_failed"; // MODIFIED HERE
                }
            }
        }

        let order_query = "";
        if (window.user_order_id) {
            order_query = "&order=" + window.user_order_id;
        }
        if (autosave) {
            order_query += "&autosave=autosave";
        }

        // Add orig_msg_id and orig_chat_id if they are available
        if (typeof orig_msg_id !== 'undefined' && orig_msg_id !== null) {
            order_query += `&orig_msg_id=${orig_msg_id}`;
        }
        if (typeof orig_chat_id !== 'undefined' && orig_chat_id !== null) {
            order_query += `&orig_chat_id=${orig_chat_id}`;
        }

        console.log("Saving order:", collectedOrder); // For debugging

        const response = await fetch('menu?' + IDQ() + order_query, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(collectedOrder)
        });

        if (!response.ok) {
            const errorBody = await response.text().catch(() => "Could not retrieve error body");
            throw new Error(`Network response was not ok: ${response.status} ${response.statusText}\n${errorBody}`);
        }
        return response;
    }

    // ALWAYS try to pre-fill from user_order if it exists
    if (typeof user_order !== 'undefined' && user_order !== null) {
        // FIRST PASS: Check appropriate inputs based on user_order
        for (const day in user_order) {
            if (user_order[day].lunch) { // Check if lunch object exists for the day
                const lunchDetails = user_order[day].lunch;
                const lunchItems = lunchDetails.items; // items might be null/undefined
                const lunchType = lunchDetails.type;

                if (lunchType === 'no-lunch') {
                    const noLunchRadio = document.querySelector(`input[name="${day}-lunch-soup"][value="no-lunch"]`);
                    if (noLunchRadio) noLunchRadio.checked = true;
                } else {
                    // Lunch is selected, determine soup status
                    if (lunchType === 'no-soup') {
                        const noSoupRadio = document.querySelector(`input[name="${day}-lunch-soup"][value="no-soup"]`);
                        if (noSoupRadio) noSoupRadio.checked = true;
                    } else if (lunchItems && lunchItems.soup_index !== undefined && lunchItems.soup_index !== null) {
                        // Assumes lunchType is 'with-soup' or similar if a specific soup is chosen
                        const soupRadio = document.querySelector(`input[name="${day}-lunch-soup"][value="${lunchItems.soup_index}"]`);
                        if (soupRadio) soupRadio.checked = true;
                    }

                    // Handle main, side, salad if lunchItems are present
                    if (lunchItems) {
                        ['main', 'side', 'salad'].forEach(category => {
                            if (lunchItems[`${category}_index`] !== undefined && lunchItems[`${category}_index`] !== null) {
                                const itemRadio = document.querySelector(`input[name="${day}-lunch-${category}"][value="${lunchItems[`${category}_index`]}"]`);
                                if (itemRadio) itemRadio.checked = true;
                            }
                        });
                    }
                }
            }
            if (user_order[day].dinner) {
                user_order[day].dinner.forEach(itemIndex => {
                    const dinnerCheckbox = document.querySelector(`input[name="${day}-dinner-${itemIndex}"]`);
                    if (dinnerCheckbox) dinnerCheckbox.checked = true;
                });
            }
        }
    }

    if (read_only) {
        document.body.classList.add("read-only");

        // Pre-filling is now done above.
        // We still need to ensure user_order was present for the hiding logic to make sense.
        if (typeof user_order !== 'undefined' && user_order !== null) {
            // SECOND PASS: Hide unselected items based on user_order
            for (const day in user_order) {
                const dayData = user_order[day];
                const lunchSection = document.querySelector(`section.${day}.lunch`);
                const dinnerSection = document.querySelector(`section.${day}.dinner`);

                if (lunchSection && dayData.lunch) {
                    const lunchOrderDetails = dayData.lunch;
                    const lunchType = lunchOrderDetails.type;
                    const lunchItems = lunchOrderDetails.items; // Might be {} or contain selections

                    const noLunchItemContainer = lunchSection.querySelector(`.meals .item.no-lunch`);

                    if (lunchType === 'no-lunch') {
                        // Hide all .combo-class divs (soup, main, side, salad groups)
                        lunchSection.querySelectorAll(`.meals .combo-class`).forEach(el => el.style.display = 'none');
                        // The "no-lunch" item itself remains visible.
                    } else {
                        // A specific lunch was chosen (with or without soup), so hide the "no-lunch" option
                        if (noLunchItemContainer) noLunchItemContainer.style.display = 'none';

                        const soupComboClass = lunchSection.querySelector(`.combo-class.soup`);
                        if (soupComboClass) {
                            const noSoupItemContainer = soupComboClass.querySelector(`.item.no-soup`);
                            if (lunchType === 'no-soup') {
                                // "No-soup" was chosen for lunch. Hide all specific soup items.
                                soupComboClass.querySelectorAll(`.choices .item`).forEach(itemEl => itemEl.style.display = 'none');
                                // The "no-soup" item itself remains visible.
                            } else if (lunchType === 'with-soup') {
                                // A specific soup was chosen. Hide the "no-soup" option.
                                if (noSoupItemContainer) noSoupItemContainer.style.display = 'none';
                                // Hide unselected specific soups
                                const selectedSoupIndex = lunchItems?.soup_index;
                                soupComboClass.querySelectorAll(`.choices .item`).forEach(itemEl => {
                                    const radio = itemEl.querySelector('input[type="radio"]');
                                    if (!radio || radio.value !== selectedSoupIndex) {
                                        itemEl.style.display = 'none';
                                    }
                                });
                            } else {
                                // Unclear or incomplete lunchType regarding soup, hide both no-soup and specific soup options if not explicitly chosen
                                if (noSoupItemContainer) noSoupItemContainer.style.display = 'none';
                                soupComboClass.querySelectorAll(`.choices .item`).forEach(itemEl => itemEl.style.display = 'none');
                            }
                        }

                        // Handle Main, Side, Salad visibility - these are expected if lunchType is 'no-soup' or 'with-soup'
                        if (lunchType === 'no-soup' || lunchType === 'with-soup') {
                            ['main', 'side', 'salad'].forEach(category => {
                                const categoryChoicesContainer = lunchSection.querySelector(`.combo-class.${category} .choices`);
                                if (categoryChoicesContainer) {
                                    const selectedItemIndex = lunchItems ? lunchItems[`${category}_index`] : undefined;
                                    categoryChoicesContainer.querySelectorAll('.item').forEach(itemEl => {
                                        const radio = itemEl.querySelector('input[type="radio"]');
                                        if (!radio || radio.value !== selectedItemIndex) {
                                            itemEl.style.display = 'none';
                                        }
                                    });
                                }
                            });
                        } else {
                            // If lunchType is not 'no-soup' or 'with-soup' (e.g. some error or different type), hide main/side/salad groups
                            ['main', 'side', 'salad'].forEach(category => {
                                const categoryGroup = lunchSection.querySelector(`.combo-class.${category}`);
                                if (categoryGroup) categoryGroup.style.display = 'none';
                            });
                        }
                    }
                } else if (lunchSection) { // No lunch data for this day in user_order, hide all lunch options
                    lunchSection.querySelectorAll(`.meals .item, .meals .combo-class`).forEach(el => el.style.display = 'none');
                }


                if (dinnerSection && dayData.dinner) {
                    const selectedDinnerIndices = (dayData.dinner || []).map(val => String(val)); // map to string for comparison
                    dinnerSection.querySelectorAll(`.meals .item`).forEach(itemEl => {
                        const checkbox = itemEl.querySelector('input[type="checkbox"]');
                        if (!checkbox || !selectedDinnerIndices.includes(checkbox.value)) {
                            itemEl.style.display = 'none';
                        }
                    });
                } else if (dinnerSection) { // No dinner data for this day, hide all dinner items
                    dinnerSection.querySelectorAll(`.meals .item`).forEach(el => el.style.display = 'none');
                }
            }

            // Disable all inputs and hide description buttons in read-only mode
            document.querySelectorAll('section.meal input[type="radio"], section.meal input[type="checkbox"]')
                .forEach(input => input.disabled = true);
            document.querySelectorAll('section.meal button.description-button')
                .forEach(button => button.style.display = 'none');
        }
    } else if (user_order_id) {
        const autosaveInterval = 60 * 1000; // every minute
        function autosave() {
            setTimeout(() => {
                save(true).catch(rej => { console.warn("autosave error", rej) }).finally(() => autosave());
            }, autosaveInterval);
        }
        autosave();
    }
    Telegram.WebApp.ready();
    Telegram.WebApp.expand();
    Telegram.WebApp.MainButton.setText(finish_button_text);
    window.save = save;
    Telegram.WebApp.MainButton.onClick(() => {
        let shouldClose = true;
        save().then(saveResult => {
            if (saveResult === "user_validation_failed") {
                shouldClose = false;
            }
        }).catch(error => send_error(error)).finally(() => {
            if (shouldClose) {
                Telegram.WebApp.close();
            }
        });
    });
    Telegram.WebApp.MainButton.enable();
    Telegram.WebApp.MainButton.show();

    // Added sum calculator logic
    const LUNCH_NO_SOUP_PRICE = 555; // Price for lunch combo without soup
    const LUNCH_WITH_SOUP_PRICE = 665; // Price for lunch combo with soup

    let collectedOrder = {}; // This variable will store the details of the current order


    const customDialogOverlay = document.getElementById('custom-dialog-overlay');
    const customAlertDialog = document.getElementById('custom-alert-dialog');
    const customAlertMessage = document.getElementById('custom-alert-message');
    const customAlertOkButton = document.getElementById('custom-alert-ok-button');

    const customConfirmDialog = document.getElementById('custom-confirm-dialog');
    const customConfirmMessage = document.getElementById('custom-confirm-message');
    const customConfirmOkButton = document.getElementById('custom-confirm-ok-button');
    const customConfirmCancelButton = document.getElementById('custom-confirm-cancel-button');

    async function showCustomAlert(message) {
        return new Promise((resolve) => {
            customAlertMessage.textContent = message;
            customDialogOverlay.classList.remove('hidden');
            customAlertDialog.classList.remove('hidden');

            const listener = () => {
                customDialogOverlay.classList.add('hidden');
                customAlertDialog.classList.add('hidden');
                customAlertOkButton.removeEventListener('click', listener);
                resolve();
            };
            customAlertOkButton.addEventListener('click', listener);
        });
    }

    async function showCustomConfirm(message) {
        return new Promise((resolve) => {
            customConfirmMessage.textContent = message;
            customDialogOverlay.classList.remove('hidden');
            customConfirmDialog.classList.remove('hidden');

            const okListener = () => {
                customDialogOverlay.classList.add('hidden');
                customConfirmDialog.classList.add('hidden');
                customConfirmOkButton.removeEventListener('click', okListener);
                customConfirmCancelButton.removeEventListener('click', cancelListener);
                resolve(true);
            };

            const cancelListener = () => {
                customDialogOverlay.classList.add('hidden');
                customConfirmDialog.classList.add('hidden');
                customConfirmOkButton.removeEventListener('click', okListener);
                customConfirmCancelButton.removeEventListener('click', cancelListener);
                resolve(false);
            };

            customConfirmOkButton.addEventListener('click', okListener);
            customConfirmCancelButton.addEventListener('click', cancelListener);
        });
    }




    function calculateTotalSumAndOrder() {
        let totalSum = 0;
        collectedOrder = {}; // Reset for each calculation

        if (typeof menu === 'undefined' || menu === null) {
            console.error("Menu data is not available for calculation.");
            return;
        }

        const days = Object.keys(menu); // Get days from the menu data itself

        days.forEach(day => {
            if (!menu[day]) return;

            collectedOrder[day] = {
                lunch: null,
                dinner: []
            };

            const lunchSection = document.querySelector(`section.${day}.lunch`);
            if (lunchSection) {
                let lunchOrderDetails = { type: null, items: {} };
                const selectedLunchOptionRadio = lunchSection.querySelector(`input[name="${day}-lunch-soup"]:checked`);
                const soupComboEl = lunchSection.querySelector(`.combo-class.soup`);

                if (selectedLunchOptionRadio) {
                    if (soupComboEl) soupComboEl.classList.remove('unfilled'); // A choice in this group was made

                    const choiceValue = selectedLunchOptionRadio.value;
                    if (choiceValue === "no-lunch") {
                        lunchOrderDetails.type = "no-lunch";
                        // Remove 'unfilled' from all lunch categories for this day
                        lunchSection.querySelectorAll('.combo-class.main, .combo-class.side, .combo-class.salad')
                            .forEach(el => el.classList.remove('unfilled'));
                        // Soup combo element already handled above
                    } else if (choiceValue === "no-soup") {
                        totalSum += LUNCH_NO_SOUP_PRICE;
                        lunchOrderDetails.type = "no-soup";
                    } else { // A specific soup is selected
                        totalSum += LUNCH_WITH_SOUP_PRICE;
                        lunchOrderDetails.type = "with-soup";
                        lunchOrderDetails.items.soup_index = choiceValue;
                    }
                }
                // If no selectedLunchOptionRadio, save() will handle adding 'unfilled' to soupComboEl on button press.

                if (lunchOrderDetails.type && lunchOrderDetails.type !== "no-lunch") {
                    const LUNCH_SUB_CATEGORIES = ["main", "side", "salad"];
                    LUNCH_SUB_CATEGORIES.forEach(category => {
                        const selectedRadio = lunchSection.querySelector(`.combo-class.${category} .choices input[name="${day}-lunch-${category}"]:checked`);
                        const comboEl = lunchSection.querySelector(`.combo-class.${category}`);
                        if (selectedRadio) {
                            if (comboEl) comboEl.classList.remove('unfilled');
                            lunchOrderDetails.items[category + '_index'] = selectedRadio.value;
                        }
                        // If !selectedRadio, save() will handle adding 'unfilled' on button press.
                    });
                }
                collectedOrder[day].lunch = lunchOrderDetails;
            }

            // --- DINNER ---
            const dinnerSection = document.querySelector(`section.${day}.dinner`);
            if (dinnerSection && menu[day].dinner) {
                // Assuming dinner items are checkboxes like: <input type="checkbox" name="${day}-dinner-item-${index}" value="${index}">
                const selectedDinnerInputs = dinnerSection.querySelectorAll(`.meals .item input[type="checkbox"]:checked`);

                selectedDinnerInputs.forEach(input => {
                    const itemIndexStr = input.value;
                    // Ensure the value is a valid index for menu[day].dinner
                    if (itemIndexStr && menu[day].dinner[parseInt(itemIndexStr)]) {
                        const itemIndex = parseInt(itemIndexStr);
                        const menuItem = menu[day].dinner[itemIndex];
                        if (menuItem && typeof menuItem.price === 'number') {
                            totalSum += menuItem.price;
                        }
                        collectedOrder[day].dinner.push(itemIndex);
                    }
                });
            }
        });

        const totalSumElement = document.querySelector('#total-sum .value');
        if (totalSumElement) {
            totalSumElement.textContent = totalSum.toFixed(0); // Display sum, adjust formatting as needed
        }

        return collectedOrder;
    }

    // Add event listener to the body to recalculate on any click
    document.body.addEventListener('click', function () {
        // Using requestAnimationFrame can help ensure DOM updates are processed before calculation
        // requestAnimationFrame(calculateTotalSumAndOrder);
        // Direct call as per "passively if clicked"
        calculateTotalSumAndOrder();
    });

    calculateTotalSumAndOrder(); // Already loaded
})().catch(console.error);