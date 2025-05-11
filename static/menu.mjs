(async function(){
    
    function findParent(element, query) {
        let parent = element.parentElement;
        while (parent && !parent.matches(query)) {
            parent = parent.parentElement;
        }
        return parent;
    }

    let menu = await fetch("static/menu_2025_1.json").then(r=>r.json());
    for(let day in menu) {
        let meal = "lunch";
        let lunch = menu[day][meal];
        for(let i = 0; i < lunch.length; ++i) {
            let item = lunch[i];
            if(item.skip) continue;

            let container = document.createElement("div");
            container.classList.add("item");
            container.classList.add(item.category);
            let url = item.photo;
            if(url && !url.startsWith("https://")) {
                url = "static/menu_photos/"+url+".jpg";
            }
            let innerHTML = `
                <label>
                    <input type="radio" name="${day}-${meal}-${item.category}" value="${i}" />
                    <span class="name" lang="ru">${item.title_ru}</span>
                    <span class="name" lang="en">${item.title_en}</span>
            `;
            if(url) {
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
            if(url) {
                innerHTML += `
                    <img class="photo" loading="lazy" src="${url}">
                `;
            }
            if(item.ingredients_en && item.ingredients_en.length > 0) {
                innerHTML += `
                    <div class="ingredients">
                        <span lang="ru">Ингредиенты: ${item.ingredients_ru}</span>
                        <span lang="en">Ingredients: ${item.ingredients_en}</span>
                    </div>
                `;
            }
            if(item.weight && item.weight.value > 0) {
                innerHTML += `
                    <div class="weight">${item.weight.value}</div>
                `;
            }
            if(item.nutrition) {
                for(let type of ["calories", "fat", "carbohydrates", "protein"]) {
                    if(type in item.nutrition) {
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
        for(let i = 0; i < dinner.length; ++i) {
            let item = dinner[i];
            if(item.skip) continue;
            if(!item.price) {
                console.warn("Item has no price", i, item);
                continue;
            }

            let container = document.createElement("div");
            container.classList.add("item");
            container.classList.add(item.category);
            let url = item.photo;
            if(url && !url.startsWith("https://")) {
                url = "static/menu_photos/"+url+".jpg";
            }
            let innerHTML = `
                <label>
                    <input type="checkbox" name="${day}-${meal}-${i}" value="${i}" />
                    <span class="name" lang="ru">${item.title_ru}</span>
                    <span class="name" lang="en">${item.title_en}</span>
                    <span class="price">${item.price}</span>
            `;
            if(url) {
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
            if(url) {
                innerHTML += `
                    <img class="photo" loading="lazy" src="${url}">
                `;
            }
            if(item.ingredients_en && item.ingredients_en.length > 0) {
                innerHTML += `
                    <div class="ingredients">
                        <span lang="ru">Ингредиенты: ${item.ingredients_ru}</span>
                        <span lang="en">Ingredients: ${item.ingredients_en}</span>
                    </div>
                `;
            }
            if(item.weight && item.weight.value > 0) {
                innerHTML += `
                    <div class="weight">${item.weight.value}</div>
                `;
            }
            if(item.nutrition) {
                for(let type of ["calories", "fat", "carbohydrates", "protein"]) {
                    if(type in item.nutrition) {
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
        return "initData="+encodeURIComponent(Telegram.WebApp.initData)
    }
    
    function send_error(err) {
        return fetch("error?"+IDQ(), {
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
                console.log("Order is incomplete. Please fill all required lunch items for selected lunches.");
                // Optionally: Telegram.WebApp.showAlert("Please complete your lunch selections.");
                return; // Prevent save if incomplete on manual trigger
            }
        }

        let order_query = "";
        if (window.user_order_id) {
            order_query = "&order=" + window.user_order_id;
        }
        if (autosave) {
            order_query += "&autosave=autosave";
        }

        // Use window.user_order which is populated by calculateTotalSumAndOrder via collectedOrder
        const dataToSend = window.user_order; 
        
        console.log("Saving order:", dataToSend); // For debugging

        const response = await fetch('menu?' + IDQ() + order_query, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(dataToSend) 
        });

        if (!response.ok) {
            const errorBody = await response.text().catch(() => "Could not retrieve error body");
            throw new Error(`Network response was not ok: ${response.status} ${response.statusText}\n${errorBody}`);
        }
        return response;
    }
    if(read_only){
        document.body.classList.add("read-only");
        
        for(let cart_id in carts) {
            let cart = carts[cart_id]
            for(let item_id of cart.items) {
                document.getElementById(`menu-item-${item_id}`).classList.add("in-cart")
            }
        }
    } else if (user_order_id) {
        const autosaveInterval = 60*1000; // every minute
        function autosave() {
            setTimeout(()=>{
                save(true).catch(rej=>{console.warn("autosave error", rej)}).finally(()=>autosave());
            }, autosaveInterval);
        }
        autosave();
    }
    Telegram.WebApp.ready();
    Telegram.WebApp.expand();
    Telegram.WebApp.MainButton.setText(finish_button_text);
    Telegram.WebApp.MainButton.onClick(()=>{
        save().catch(error => {
            console.error('Save error:', error);
            return send_error(error);
        }).finally(()=>{
            Telegram.WebApp.close();
        });
    });
    Telegram.WebApp.MainButton.enable();
    Telegram.WebApp.MainButton.show();

    // Added sum calculator logic
    const LUNCH_NO_SOUP_PRICE = 555; // Price for lunch combo without soup
    const LUNCH_WITH_SOUP_PRICE = 665; // Price for lunch combo with soup
    
    let collectedOrder = {}; // This variable will store the details of the current order

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
        
        // console.log("Current Order:", collectedOrder); // For debugging
        // If you need to update the global `user_order` variable from menu.html:
        if (typeof window.user_order !== 'undefined') {
           // Create a deep copy to avoid direct mutation issues if user_order is complex
           try {
             window.user_order = JSON.parse(JSON.stringify(collectedOrder));
           } catch (e) {
             console.error("Error updating window.user_order:", e);
             // Fallback or simpler assignment if deep copy fails or is not needed
             // window.user_order = collectedOrder;
           }
        }
    }

    // Add event listener to the body to recalculate on any click
    document.body.addEventListener('click', function() {
        // Using requestAnimationFrame can help ensure DOM updates are processed before calculation
        // requestAnimationFrame(calculateTotalSumAndOrder);
        // Direct call as per "passively if clicked"
        calculateTotalSumAndOrder();
    });

    calculateTotalSumAndOrder(); // Already loaded
})().catch(console.error);