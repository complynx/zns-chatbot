(async function () {

    function findParent(element, query) {
        let parent = element.parentElement;
        while (parent && !parent.matches(query)) {
            parent = parent.parentElement;
        }
        return parent;
    }

    // Helper function to uncheck inputs in a hidden section
    function uncheckInputsInSection(sectionElement) {
        if (!sectionElement) return;
        sectionElement.querySelectorAll('input[type="radio"], input[type="checkbox"]').forEach(input => {
            if (input.checked) {
                input.checked = false;
                // Consider dispatching a 'change' event if other listeners depend on it,
                // though our main recalculateTotalSumAndOrder is triggered by body click or explicit calls.
            }
        });
    }

    function createMenuItemHTML(item, day, mealContext, index, category = null) { // category is item.category
        let url = item.photo;
        if (url && !url.startsWith("https://")) {
            url = "static/menu_photos/" + url + ".jpg";
        }

        let inputType;
        let nameAttribute;
        let valueAttribute = index; // Value is always the item's index in its original menu array
        let displayPrice = false;

        if (mealContext === "lunch-individual") { // New: For individual lunch item selection
            inputType = "checkbox";
            nameAttribute = `${day}-lunch-individual-${index}`; // e.g., friday-lunch-individual-0
            if (item.price) displayPrice = true;
        } else if (mealContext === "lunch") { // For components of a combo lunch
            inputType = "radio";
            if (category === "soup") { // Special handling for actual soup choice within combo
                nameAttribute = `${day}-lunch-actual-soup`; // e.g., friday-lunch-actual-soup
            } else { // main, side, salad components of a combo
                nameAttribute = `${day}-lunch-${category}`; // e.g., friday-lunch-main
            }
            // Price is not displayed for individual combo components; price is for the combo itself
        } else if (mealContext === "dinner") { // Original dinner logic
            inputType = "checkbox";
            nameAttribute = `${day}-dinner-${index}`; // e.g., friday-dinner-0
            if (item.price) displayPrice = true;
        } else {
            console.warn("Unknown meal context in createMenuItemHTML:", mealContext, "for item", item);
            // Fallback, though ideally, this should not be reached with controlled calls
            inputType = "checkbox";
            nameAttribute = `${day}-${mealContext}-${index}`;
        }

        let innerHTML = `
            <label>
                <input type="${inputType}" name="${nameAttribute}" value="${valueAttribute}" data-item-index="${index}" />
                <span class="name" lang="ru">${item.title_ru}</span>
                <span class="name" lang="en">${item.title_en}</span>
        `;
        if (displayPrice && typeof item.price === 'number') { // Display price if applicable
            innerHTML += `
                    <span class="price">${item.price}</span>
            `;
        }
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
        return innerHTML;
    }

    let menu = await fetch("static/menu_2025_1.json").then(r => r.json());
    for (let day in menu) {
        let meal = "lunch";
        let lunchItemsFromMenu = menu[day][meal];
        const lunchSection = document.querySelector(`section.${day}.lunch`);
        let hasPricedIndividualLunchItems = false;

        if (lunchSection && lunchItemsFromMenu) {
            const individualChoicesContainer = lunchSection.querySelector('.individual-items-selection-container .choices.individual-lunch-choices'); // Assumed HTML
            const comboSoupChoicesContainer = lunchSection.querySelector('.combo-details-container .actual-soup-choices'); // Assumed HTML

            for (let i = 0; i < lunchItemsFromMenu.length; ++i) {
                let item = lunchItemsFromMenu[i];
                if (item.skip) continue;

                // Populate "Individual Items" if item has a price
                if (typeof item.price === 'number' && individualChoicesContainer) {
                    hasPricedIndividualLunchItems = true;
                    let container = document.createElement("div");
                    container.classList.add("item");
                    if (item.category) container.classList.add(item.category); // Add category for potential styling
                    container.innerHTML = createMenuItemHTML(item, day, "lunch-individual", i, item.category);
                    individualChoicesContainer.appendChild(container);
                }

                // Populate "Combo" choices
                if (item.category) {
                    if (item.category === 'soup' && comboSoupChoicesContainer) {
                        let container = document.createElement("div");
                        container.classList.add("item");
                        container.innerHTML = createMenuItemHTML(item, day, "lunch", i, "soup");
                        comboSoupChoicesContainer.appendChild(container);
                    } else if (['main', 'side', 'salad'].includes(item.category)) {
                        const comboCategoryChoicesContainer = lunchSection.querySelector(`.combo-details-container .combo-class.${item.category} .choices`); // Assumed HTML
                        if (comboCategoryChoicesContainer) {
                            let container = document.createElement("div");
                            container.classList.add("item");
                            // container.classList.add(item.category); // Already handled by querySelector path
                            container.innerHTML = createMenuItemHTML(item, day, "lunch", i, item.category);
                            comboCategoryChoicesContainer.appendChild(container);
                        }
                    }
                }
            }
            // Show/hide the "Individual Items" radio button *option* based on availability
            const individualItemsModeOption = lunchSection.querySelector('.individual-items-mode-option'); // Assumed HTML
            if (individualItemsModeOption) {
                individualItemsModeOption.style.display = hasPricedIndividualLunchItems ? '' : 'none';
            }
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
            container.innerHTML = createMenuItemHTML(item, day, meal, i);
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

        let orderIsIncompleteForCombos = false;
        let someSelectionsAreEmpty = false; // For individual lunch or dinner

        if (!autosave) { // Validation and highlighting only for manual saves (button press)
            document.querySelectorAll('.combo-class.unfilled, .individual-items-selection-container.unfilled').forEach(el => el.classList.remove('unfilled')); // Clear previous highlights

            const days = Object.keys(menu);
            const LUNCH_COMBO_SUB_CATEGORIES = ["main", "side", "salad"]; // Renamed for clarity

            days.forEach(day => {
                if (!menu[day] || !menu[day].lunch) return;

                const lunchOrderDetails = collectedOrder[day] ? collectedOrder[day].lunch : null;
                const lunchSection = document.querySelector(`section.${day}.lunch`);
                if (!lunchSection || !lunchOrderDetails) return;

                // Validation for COMBO lunches
                if (lunchOrderDetails.type === "combo-no-soup" || lunchOrderDetails.type === "combo-with-soup") {
                    // 1. Validate soup choice if it's a "combo-with-soup"
                    if (lunchOrderDetails.type === "combo-with-soup" && lunchOrderDetails.items.soup_index === undefined) {
                        const soupComboEl = lunchSection.querySelector('.combo-details-container .actual-soup-choices'); // Target the actual choices
                        if (soupComboEl) soupComboEl.classList.add('unfilled'); // Or a more general combo container
                        orderIsIncompleteForCombos = true;
                    } else {
                        const soupComboEl = lunchSection.querySelector('.combo-details-container .actual-soup-choices');
                        if (soupComboEl) soupComboEl.classList.remove('unfilled');
                    }

                    // 2. Validate main, side, salad choices for combos
                    LUNCH_COMBO_SUB_CATEGORIES.forEach(category => {
                        const categoryEl = lunchSection.querySelector(`.combo-details-container .combo-class.${category}`);
                        if (lunchOrderDetails.items[`${category}_index`] === undefined) {
                            if (categoryEl) categoryEl.classList.add('unfilled');
                            orderIsIncompleteForCombos = true;
                        } else {
                            if (categoryEl) categoryEl.classList.remove('unfilled');
                        }
                    });
                } else if (lunchOrderDetails.type === "no-lunch") {
                    // Clear unfilled from combo sections if "no lunch" is chosen for this day
                    lunchSection.querySelectorAll('.combo-details-container .combo-class, .combo-details-container .actual-soup-choices')
                        .forEach(el => el.classList.remove('unfilled'));
                }

                // Check for empty "Individual Items" lunch selection
                if (lunchOrderDetails.type === "individual-items" && (!lunchOrderDetails.items || lunchOrderDetails.items.length === 0)) {
                    someSelectionsAreEmpty = true;
                    // Optionally, highlight the individual items section
                    const individualContainer = lunchSection.querySelector('.individual-items-selection-container');
                    if (individualContainer) individualContainer.classList.add('unfilled'); // Or some other visual cue
                } else {
                    const individualContainer = lunchSection.querySelector('.individual-items-selection-container');
                    if (individualContainer) individualContainer.classList.remove('unfilled');
                }
            });

            if (orderIsIncompleteForCombos) {
                const message = (typeof alert_lunch_incomplete_text !== 'undefined' && alert_lunch_incomplete_text)
                    ? alert_lunch_incomplete_text
                    : "Order is incomplete. Please fill all required items for selected combo lunches.";
                await showCustomAlert(message);
                return "user_validation_failed";
            }

            // Check for empty dinner selections
            if (collectedOrder) {
                for (const day in menu) {
                    if (menu[day] && menu[day].dinner && menu[day].dinner.length > 0) {
                        if (collectedOrder[day] && collectedOrder[day].dinner && collectedOrder[day].dinner.length === 0) {
                            // Check if user actually intended to have dinner, i.e. didn't uncheck everything deliberately
                            // This check might be too aggressive if user can deselect all dinners.
                            // For now, if dinner is offered and order has empty dinner array for it, flag for confirm.
                            someSelectionsAreEmpty = true;
                            break;
                        }
                    }
                }
            }

            if (someSelectionsAreEmpty) {
                const DEFAULT_CONFIRM_EMPTY_TEXT = "You have selected 'Individual Items' for lunch or opted for dinner on one or more days, but haven't chosen any specific items. Proceed anyway?";
                const confirmationMessage = (typeof confirm_dinner_empty_text !== 'undefined' && confirm_dinner_empty_text)
                    ? confirm_dinner_empty_text
                    : DEFAULT_CONFIRM_EMPTY_TEXT;
                const proceedWithEmpty = await showCustomConfirm(confirmationMessage);

                if (!proceedWithEmpty) {
                    return "user_validation_failed";
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
            if (user_order[day] && user_order[day].lunch) {
                const lunchDetails = user_order[day].lunch;
                const lunchMode = lunchDetails.type; // e.g., "no-lunch", "individual-items", "combo-no-soup", "combo-with-soup"
                const lunchItems = lunchDetails.items; // object for combos, array for individual-items

                const modeRadio = document.querySelector(`input[name="${day}-lunch-mode"][value="${lunchMode}"]`);
                if (modeRadio) modeRadio.checked = true;

                if (lunchMode === "individual-items" && lunchItems && Array.isArray(lunchItems)) {
                    lunchItems.forEach(itemIndex => {
                        const itemCheckbox = document.querySelector(`input[name="${day}-lunch-individual-${itemIndex}"]`);
                        if (itemCheckbox) itemCheckbox.checked = true;
                    });
                } else if (lunchMode === "combo-no-soup" || lunchMode === "combo-with-soup") {
                    if (lunchItems) {
                        if (lunchMode === "combo-with-soup" && lunchItems.soup_index !== undefined && lunchItems.soup_index !== null) {
                            const soupRadio = document.querySelector(`input[name="${day}-lunch-actual-soup"][value="${lunchItems.soup_index}"]`);
                            if (soupRadio) soupRadio.checked = true;
                        }
                        ['main', 'side', 'salad'].forEach(category => {
                            if (lunchItems[`${category}_index`] !== undefined && lunchItems[`${category}_index`] !== null) {
                                const itemRadio = document.querySelector(`input[name="${day}-lunch-${category}"][value="${lunchItems[`${category}_index`]}"]`);
                                if (itemRadio) itemRadio.checked = true;
                            }
                        });
                    }
                }
                // "no-lunch" is handled by checking the modeRadio.
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

        // Pre-filling is done above.
        // We need user_order and menu to be available for the hiding logic.
        if (typeof user_order !== 'undefined' && user_order !== null && typeof menu !== 'undefined' && menu !== null) {
            const allDaysFromMenu = Object.keys(menu);

            allDaysFromMenu.forEach(day => {
                const dayData = user_order[day]; // User's order for this specific day
                const lunchSection = document.querySelector(`section.${day}.lunch`);
                const dinnerSection = document.querySelector(`section.${day}.dinner`);

                // --- LUNCH SECTION VISIBILITY (READ-ONLY) ---
                if (lunchSection && dayData && dayData.lunch) {
                    const lunchDetails = dayData.lunch;
                    const lunchMode = lunchDetails.type;
                    const lunchItems = lunchDetails.items;

                    const individualItemsContainer = lunchSection.querySelector('.individual-items-selection-container');
                    const comboDetailsContainer = lunchSection.querySelector('.combo-details-container');
                    const noLunchDisplayItem = lunchSection.querySelector('.no-lunch-display'); // Assumed HTML for displaying "No Lunch" text

                    if (individualItemsContainer) individualItemsContainer.style.display = 'none';
                    if (comboDetailsContainer) comboDetailsContainer.style.display = 'none';
                    if (noLunchDisplayItem) noLunchDisplayItem.style.display = 'none';

                    if (lunchMode === "no-lunch") {
                        lunchSection.style.display = ''; // Show the day's lunch section to indicate "No lunch"
                        if (noLunchDisplayItem) noLunchDisplayItem.style.display = ''; // Show "No lunch" text
                        // Hide combo and individual sections explicitly
                        if (comboDetailsContainer) comboDetailsContainer.style.display = 'none';
                        if (individualItemsContainer) individualItemsContainer.style.display = 'none';

                    } else if (lunchMode === "individual-items") {
                        lunchSection.style.display = '';
                        if (individualItemsContainer) individualItemsContainer.style.display = '';
                        if (comboDetailsContainer) comboDetailsContainer.style.display = 'none';

                        if (lunchItems && Array.isArray(lunchItems) && individualItemsContainer) {
                            const selectedIndices = lunchItems.map(val => String(val));
                            individualItemsContainer.querySelectorAll(`.choices.individual-lunch-choices .item`).forEach(itemEl => {
                                const checkbox = itemEl.querySelector('input[type="checkbox"]');
                                if (checkbox && selectedIndices.includes(String(checkbox.value))) {
                                    itemEl.style.display = '';
                                } else {
                                    itemEl.style.display = 'none';
                                }
                            });
                        }
                    } else if (lunchMode === "combo-no-soup" || lunchMode === "combo-with-soup") {
                        lunchSection.style.display = '';
                        if (comboDetailsContainer) comboDetailsContainer.style.display = '';
                        if (individualItemsContainer) individualItemsContainer.style.display = 'none';

                        // Soup visibility within combo
                        const actualSoupChoices = comboDetailsContainer.querySelector('.actual-soup-choices');
                        const noSoupInComboDisplay = comboDetailsContainer.querySelector('.no-soup-in-combo-display'); // Assumed HTML

                        if (actualSoupChoices) actualSoupChoices.style.display = 'none';
                        if (noSoupInComboDisplay) noSoupInComboDisplay.style.display = 'none';

                        if (lunchMode === "combo-with-soup") {
                            if (actualSoupChoices) actualSoupChoices.style.display = '';
                            if (lunchItems && lunchItems.soup_index !== undefined) {
                                const selectedSoupIndexStr = String(lunchItems.soup_index);
                                actualSoupChoices.querySelectorAll('.item').forEach(itemEl => {
                                    const radio = itemEl.querySelector('input[type="radio"]');
                                    if (radio && String(radio.value) === selectedSoupIndexStr) {
                                        itemEl.style.display = '';
                                    } else {
                                        itemEl.style.display = 'none';
                                    }
                                });
                            }
                        } else { // combo-no-soup
                            if (noSoupInComboDisplay) noSoupInComboDisplay.style.display = '';
                        }

                        // Main, Side, Salad visibility within combo
                        ['main', 'side', 'salad'].forEach(category => {
                            const categoryChoicesContainer = comboDetailsContainer.querySelector(`.combo-class.${category} .choices`);
                            if (categoryChoicesContainer) {
                                if (lunchItems && lunchItems[`${category}_index`] !== undefined) {
                                    const selectedItemIndexStr = String(lunchItems[`${category}_index`]);
                                    categoryChoicesContainer.querySelectorAll('.item').forEach(itemEl => {
                                        const radio = itemEl.querySelector('input[type="radio"]');
                                        if (radio && String(radio.value) === selectedItemIndexStr) {
                                            itemEl.style.display = '';
                                        } else {
                                            itemEl.style.display = 'none';
                                        }
                                    });
                                } else { // Whole category not selected, hide all its items
                                    categoryChoicesContainer.querySelectorAll('.item').forEach(itemEl => itemEl.style.display = 'none');
                                }
                            }
                        });
                    } else { // No valid lunch order for this day, hide the entire lunch section for this day
                        lunchSection.style.display = 'none';
                    }
                } else if (lunchSection) { // No dayOrderData or no lunch in order for this day
                    lunchSection.style.display = 'none';
                }

                // --- DINNER SECTION VISIBILITY (READ-ONLY) ---
                if (dinnerSection) {
                    // Condition for dinner section to be VISIBLE:
                    // 1. Order for the day exists.
                    // 2. Dinner details for the day exist.
                    // 3. Dinner array is not empty.
                    if (dayData && dayData.dinner && dayData.dinner.length > 0) {
                        dinnerSection.style.display = ''; // Show section

                        const selectedDinnerIndices = dayData.dinner.map(val => String(val));
                        dinnerSection.querySelectorAll(`.meals .item`).forEach(itemEl => {
                            const checkbox = itemEl.querySelector('input[type="checkbox"]');
                            if (checkbox && selectedDinnerIndices.includes(String(checkbox.value))) {
                                itemEl.style.display = ''; // Show selected dinner item
                            } else {
                                itemEl.style.display = 'none'; // Hide other dinner items
                            }
                        });
                    } else {
                        // Dinner section should be hidden (no order, or empty dinner array)
                        dinnerSection.style.display = 'none';
                    }
                }
            });

        } else {
            // user_order or menu is not available, hide all meal sections
            document.querySelectorAll('section.meal').forEach(section => {
                section.style.display = 'none';
            });
        }

        // Disable all inputs and hide description buttons in read-only mode
        document.querySelectorAll('section.meal input[type="radio"], section.meal input[type="checkbox"]')
            .forEach(input => input.disabled = true);
        document.querySelectorAll('section.meal button.description-button')
            .forEach(button => button.style.display = 'none');
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
            document.querySelector('#total-sum .value').textContent = 'Error';
            return;
        }

        const days = Object.keys(menu);

        days.forEach(day => {
            if (!menu[day]) {
                collectedOrder[day] = { lunch: null, dinner: [] }; // Still init day in order
                return;
            }

            collectedOrder[day] = {
                lunch: null, // Will be populated based on mode
                dinner: []
            };

            const lunchSection = document.querySelector(`section.${day}.lunch`);
            if (lunchSection) {
                let currentLunchOrderDetails = { type: null, items: null }; // items: {} for combo, [] for individual
                const selectedLunchModeRadio = lunchSection.querySelector(`input[name="${day}-lunch-mode"]:checked`);

                const individualItemsContainer = lunchSection.querySelector('.individual-items-selection-container');
                const comboDetailsContainer = lunchSection.querySelector('.combo-details-container');
                const actualSoupChoicesContainer = comboDetailsContainer ? comboDetailsContainer.querySelector('.actual-soup-choices') : null;
                // const noSoupInComboDisplay = comboDetailsContainer ? comboDetailsContainer.querySelector('.no-soup-in-combo-display') : null; // For display only

                // Default visibility: hide both specific sections first
                if (!read_only) { // Only manipulate display if not in read_only mode
                    if (individualItemsContainer) individualItemsContainer.style.display = 'none';
                    if (comboDetailsContainer) comboDetailsContainer.style.display = 'none';
                    if (actualSoupChoicesContainer) actualSoupChoicesContainer.style.display = 'none';
                    // if (noSoupInComboDisplay) noSoupInComboDisplay.style.display = 'none';
                }

                if (selectedLunchModeRadio) {
                    const mode = selectedLunchModeRadio.value;
                    currentLunchOrderDetails.type = mode;

                    if (mode === "no-lunch") {
                        currentLunchOrderDetails.items = {}; // Or null
                        if (!read_only) {
                            uncheckInputsInSection(individualItemsContainer);
                            uncheckInputsInSection(comboDetailsContainer); // Clears all combo radios
                        }
                    } else if (mode === "individual-items") {
                        currentLunchOrderDetails.items = [];
                        if (!read_only && individualItemsContainer) individualItemsContainer.style.display = '';
                        if (!read_only) uncheckInputsInSection(comboDetailsContainer);

                        const selectedIndividualItems = lunchSection.querySelectorAll(`.individual-items-selection-container .choices.individual-lunch-choices input[type="checkbox"]:checked`);
                        selectedIndividualItems.forEach(input => {
                            const itemIndex = parseInt(input.value);
                            if (!isNaN(itemIndex) && menu[day].lunch && menu[day].lunch[itemIndex]) {
                                const menuItem = menu[day].lunch[itemIndex];
                                if (menuItem && typeof menuItem.price === 'number') {
                                    totalSum += menuItem.price;
                                }
                                currentLunchOrderDetails.items.push(itemIndex);
                            }
                        });
                    } else if (mode === "combo-no-soup" || mode === "combo-with-soup") {
                        currentLunchOrderDetails.items = {}; // For main, side, salad, soup_index
                        if (!read_only && comboDetailsContainer) comboDetailsContainer.style.display = '';
                        if (!read_only) uncheckInputsInSection(individualItemsContainer);

                        if (mode === "combo-no-soup") {
                            totalSum += LUNCH_NO_SOUP_PRICE;
                            // if (!read_only && noSoupInComboDisplay) noSoupInComboDisplay.style.display = '';
                            if (!read_only && actualSoupChoicesContainer) {
                                actualSoupChoicesContainer.style.display = 'none';
                                uncheckInputsInSection(actualSoupChoicesContainer);
                            }
                        } else { // combo-with-soup
                            totalSum += LUNCH_WITH_SOUP_PRICE;
                            if (!read_only && actualSoupChoicesContainer) actualSoupChoicesContainer.style.display = '';
                            // if (!read_only && noSoupInComboDisplay) noSoupInComboDisplay.style.display = 'none';

                            const selectedSoupRadio = lunchSection.querySelector(`input[name="${day}-lunch-actual-soup"]:checked`);
                            if (selectedSoupRadio) {
                                currentLunchOrderDetails.items.soup_index = selectedSoupRadio.value;
                            }
                        }

                        const LUNCH_COMBO_SUB_CATEGORIES = ["main", "side", "salad"];
                        LUNCH_COMBO_SUB_CATEGORIES.forEach(category => {
                            const selectedRadio = lunchSection.querySelector(`.combo-details-container .combo-class.${category} .choices input[name="${day}-lunch-${category}"]:checked`);
                            if (selectedRadio) {
                                currentLunchOrderDetails.items[category + '_index'] = selectedRadio.value;
                            }
                        });
                    }
                }
                collectedOrder[day].lunch = currentLunchOrderDetails;
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
    document.body.addEventListener('click', function (event) {
        // Recalculate unless the click was on a description button or inside a description popup.
        if (!event.target.closest('.description-button') && !event.target.closest('.description.active')) {
            // Using requestAnimationFrame can help ensure DOM updates are processed before calculation
            // requestAnimationFrame(calculateTotalSumAndOrder);
            // Direct call as per "passively if clicked"
            calculateTotalSumAndOrder();
        }
    });

    calculateTotalSumAndOrder(); // Initial calculation on load
    // Pre-fill must happen BEFORE the first calculateTotalSumAndOrder if it's to influence the first view,
    // or call calculate again after prefill.
    // The current structure pre-fills, then adds event listeners, then calls calculate. This is okay.
})().catch(err => {
    console.error("Top level error in menu.mjs:", err);
    // Try to send error to backend if possible, even if Telegram object might not be ready.
    fetch("error?initData=" + encodeURIComponent(Telegram.WebApp.initData || "undefined"), {
        method: "POST",
        body: "Top level menu.mjs error: " + (err.message ? err.message : String(err)) + (err.stack ? "\\n" + err.stack : "")
    }).catch(e => console.error("Failed to send top level error", e));
});