fetch("static/menu_belarus.json").then(r=>r.json()).then(menu=>{
    // Function to fill in all sections by looping through the <section> elements
    function fillAllSections() {
        // Get all sections with the "meal" class
        let sections = document.querySelectorAll('section.meal');

        // Iterate over each section
        sections.forEach(section => {
            // Extract the day and mealType from the section's class list
            let day = [...section.classList].find(cls => menu.choices[cls]);
            let mealType = [...section.classList].find(cls => cls !== 'meal' && cls !== day);

            if (!day || !mealType) return; // Skip if day or mealType is not found

            // Iterate over each dish type in the section (e.g., salads, hot-dishes)
            for (let dishType in menu.choices[day]) {
                let dishContainer = section.querySelector(`.${dishType} .dishes`);
                let dishes = menu.choices[day][dishType];

                // Clear the existing content
                dishContainer.innerHTML = '';

                // Iterate over the dishes and create the HTML structure
                for (let dishKey in dishes) {
                    let dish = dishes[dishKey];

                    // Create the dish container
                    let dishDiv = document.createElement("div");
                    dishDiv.setAttribute("data-name", dishKey);

                    // Add the name
                    let nameDiv = document.createElement("div");
                    nameDiv.classList.add("name");
                    nameDiv.innerHTML = `
                        <span lang="ru">${dish.name_ru}</span>
                        <span lang="en">${dish.name_en}</span>
                    `;
                    dishDiv.appendChild(nameDiv);

                    // Add the output
                    let outputDiv = document.createElement("div");
                    outputDiv.classList.add("output");
                    outputDiv.textContent = dish.output;
                    dishDiv.appendChild(outputDiv);

                    // Add the price
                    let priceDiv = document.createElement("div");
                    priceDiv.classList.add("price");
                    priceDiv.textContent = dish.price;
                    dishDiv.appendChild(priceDiv);

                    // Add the counter
                    let counterDiv = document.createElement("div");
                    counterDiv.classList.add("counter");
                    counterDiv.textContent = 0; // Initialize counter to 0
                    dishDiv.appendChild(counterDiv);

                    if(!read_only) {
                        // Add the buttons and listeners
                        let addButton = document.createElement("button");
                        addButton.classList.add("add");
                        addButton.textContent = '+';
                        dishDiv.appendChild(addButton);

                        let removeButton = document.createElement("button");
                        removeButton.classList.add("remove");
                        removeButton.textContent = '-';
                        dishDiv.appendChild(removeButton);

                        let clearButton = document.createElement("button");
                        clearButton.classList.add("clear");
                        clearButton.textContent = '🗑';
                        dishDiv.appendChild(clearButton);

                        // Add event listeners to the buttons
                        addButton.addEventListener('click', () => {
                            counterDiv.textContent = parseInt(counterDiv.textContent) + 1;
                        });

                        removeButton.addEventListener('click', () => {
                            let count = parseInt(counterDiv.textContent);
                            if (count > 0) {
                                counterDiv.textContent = count - 1;
                            }
                        });

                        clearButton.addEventListener('click', () => {
                            counterDiv.textContent = 0;
                        });
                    }

                    // Append the dish div to the corresponding dish type container
                    dishContainer.appendChild(dishDiv);
                }
            }
        });
    }

    // Run the function to fill all sections
    fillAllSections();
    fillInOrders(user_order);
}).catch(send_error);

function collectOrdersWithExtras() {
    const orders = {
        total: 0,
        days: {},
        extras: {
            total: 0
        },
        customer: ""
    };

    // Collect customer name
    orders.customer_first_name = document.querySelector('.for-who input[name="for_who_first_name"]').value.trim();
    orders.customer_last_name = document.querySelector('.for-who input[name="for_who_last_name"]').value.trim();
    orders.customer_patronymus = document.querySelector('.for-who input[name="for_who_patronymus"]').value.trim();
    orders.customer = orders.customer_first_name + " " +
        (orders.customer_patronymus!==""?orders.customer_patronymus+" ":"") +
        orders.customer_last_name;

    // Select all meal sections
    const meals = document.querySelectorAll('.meal');

    meals.forEach(meal => {
        // Get the day and mealtime in English
        const day = [...meal.classList].find(cls => ['friday', 'saturday', 'sunday'].includes(cls));
        const mealtime = [...meal.classList].find(cls => cls !== 'meal' && cls !== day);

        // Initialize day and mealtime if not already done
        if (!orders.days[day]) {
            orders.days[day] = {
                total: 0,
                mealtimes: {}
            };
        }

        if (!orders.days[day].mealtimes[mealtime]) {
            orders.days[day].mealtimes[mealtime] = {
                total: 0,
                dishes: []
            };
        }

        // Collect all dishes in the current meal
        const dishes = meal.querySelectorAll('.dishes > div');

        dishes.forEach(dish => {
            const name = dish.dataset.name;
            const price = parseFloat(dish.querySelector('.price').textContent.trim());
            const count = parseInt(dish.querySelector('.counter').textContent.trim());

            if (count > 0) {
                const dishInfo = {
                    name: name,
                    count: count,
                    price: price,
                    total: count * price
                };

                // Add the dish to the mealtime's dishes array
                orders.days[day].mealtimes[mealtime].dishes.push(dishInfo);

                // Update the totals
                orders.days[day].mealtimes[mealtime].total += dishInfo.total;
                orders.days[day].total += dishInfo.total;
                orders.total += dishInfo.total;
            }
        });
    });

    // Collect excursion and shuttle data
    const preparty = document.querySelector('.excursions input[name="preparty"]').checked;
    const minskExcursion = document.querySelector('.excursions input[name="excursion_minsk"]').checked;
    const shuttleBus = document.querySelector('.excursions input[name="shuttle_bus"]').checked;
    const grodnoExcursion = document.querySelector('.excursions input[name="excursion_grodno"]').checked;

    if (preparty) {
        orders.extras.preparty = 25;
        orders.extras.total += 25;
        orders.total += 25;
    }

    if (minskExcursion) {
        orders.extras.excursion_minsk = 14;
        orders.extras.total += 14;
        orders.total += 14;
    }

    if (shuttleBus) {
        orders.extras.shuttle = 52;
        orders.extras.total += 52;
        orders.total += 52;
    }

    if (grodnoExcursion) {
        orders.extras.excursion_grodno = 14;
        orders.extras.total += 14;
        orders.total += 14;
    }

    return orders;
}

if(read_only){
    document.querySelector('.for-who input[name="for_who_first_name"]').disabled = true;
    document.querySelector('.for-who input[name="for_who_last_name"]').disabled = true;
    document.querySelector('.for-who input[name="for_who_patronymus"]').disabled = true;
    document.querySelector('.excursions input[name="preparty"]').disabled = true;
    document.querySelector('.excursions input[name="excursion_minsk"]').disabled = true;
    document.querySelector('.excursions input[name="shuttle_bus"]').disabled = true;
    document.querySelector('.excursions input[name="excursion_grodno"]').disabled = true;
}

function fillInOrders(orders) {
    // Validate the orders object
    if (!orders || typeof orders !== 'object') {
        console.error('Invalid orders object:', orders);
        return;
    }

    // Validate customer first name
    if (typeof orders.customer_first_name !== 'string' || orders.customer_first_name.trim() === '') {
        console.error('Invalid customer first name:', orders.customer_first_name);
    } else {
        const customerInput = document.querySelector('.for-who input[name="for_who_first_name"]');
        customerInput.value = orders.customer_first_name;
    }

    // Validate customer last name
    if (typeof orders.customer_last_name !== 'string' || orders.customer_last_name.trim() === '') {
        console.error('Invalid customer last name:', orders.customer_last_name);
    } else {
        const customerInput = document.querySelector('.for-who input[name="for_who_last_name"]');
        customerInput.value = orders.customer_last_name;
    }

    // Validate customer patronymus
    if (typeof orders.customer_patronymus !== 'string') {
        console.error('Invalid customer patronymus:', orders.customer_patronymus);
    } else {
        const customerInput = document.querySelector('.for-who input[name="for_who_patronymus"]');
        customerInput.value = orders.customer_patronymus.trim();
    }

    // Validate days
    if (!orders.days || typeof orders.days !== 'object') {
        console.error('Invalid days structure:', orders.days);
        return;
    }

    Object.keys(orders.days).forEach(day => {
        const mealtimes = orders.days[day].mealtimes;

        // Validate mealtimes
        if (!mealtimes || typeof mealtimes !== 'object') {
            console.error(`Invalid mealtimes for day ${day}:`, mealtimes);
            return;
        }

        Object.keys(mealtimes).forEach(mealtime => {
            const dishes = mealtimes[mealtime].dishes;

            // Validate dishes array
            if (!Array.isArray(dishes)) {
                console.error(`Invalid dishes array for ${day}, ${mealtime}:`, dishes);
                return;
            }

            dishes.forEach(dish => {
                // Validate dish properties
                if (typeof dish !== 'object' || typeof dish.name !== 'string' ||
                    typeof dish.count !== 'number') {
                    console.error(`Invalid dish data for ${day}, ${mealtime}:`, dish);
                    return;
                }

                // Find the corresponding meal section based on the day and mealtime
                const mealSection = document.querySelector(`.${day}.${mealtime}.meal`);

                if (!mealSection) {
                    console.error(`Meal section not found for ${day}, ${mealtime}`);
                    return;
                }

                const dishElement = Array.from(mealSection.querySelectorAll('.dishes > div')).find(el => el.dataset.name === dish.name);

                if (dishElement) {
                    // Update the counter element with the dish count
                    const counterElement = dishElement.querySelector('.counter');
                    counterElement.textContent = dish.count;
                } else {
                    console.error(`Dish element not found for ${day}, ${mealtime}:`, dish);
                }
            });
        });
    });

    // Validate extras
    if (!orders.extras || typeof orders.extras !== 'object') {
        console.error('Invalid extras structure:', orders.extras);
        return;
    }

    const prepartyCheckbox = document.querySelector('.excursions input[name="preparty"]');
    const minskExcursionCheckbox = document.querySelector('.excursions input[name="excursion_minsk"]');
    const shuttleBusCheckbox = document.querySelector('.excursions input[name="shuttle_bus"]');
    const grodnoExcursionCheckbox = document.querySelector('.excursions input[name="excursion_grodno"]');

    if ("preparty" in orders.extras) {
        prepartyCheckbox.checked = true;
    }

    if ("excursion_minsk" in orders.extras) {
        minskExcursionCheckbox.checked = true;
    }

    if ("shuttle" in orders.extras) {
        shuttleBusCheckbox.checked = true;
    }

    if ("excursion_grodno" in orders.extras) {
        grodnoExcursionCheckbox.checked = true;
    }

    let total = currencyCeil(orders.total);
    let total_rub = currencyCeil(orders.total*BYN_TO_RUB);
    
    document.getElementById("total-sum").innerText = total + " BYN "+ total_rub + " RUB";
}

Telegram.WebApp.ready();
Telegram.WebApp.expand();

const sections = document.querySelectorAll('body>section');

let currentIndex = 0;

function updateSections() {
    // Remove .active from all sections
    sections.forEach((section, index) => {
        section.classList.toggle('active', index === currentIndex);
    });

    if(currentIndex === sections.length - 1) {
        Telegram.WebApp.MainButton.setText(finish_button_text);
    } else {
        Telegram.WebApp.MainButton.setText(next_button_text);
    }
}

let name_re=/^\s*\p{Uppercase_Letter}\p{Lowercase_Letter}*\s*$/v;
function name_validity(el, err) {
    if(name_re.test(el.value)) {
        if(!el.checkValidity()) {
            el.setCustomValidity("");
        }
    } else {
        el.setAttribute("pattern", name_re.source);
        el.setCustomValidity(err);
    }
}

function validateSection(index) {
    name_validity(document.querySelector('.for-who input[name="for_who_first_name"]'),validity_error_first_name);
    name_validity(document.querySelector('.for-who input[name="for_who_last_name"]'),validity_error_last_name);
    const inputs = sections[index].querySelectorAll('input');
    for (let input of inputs) {
        if (!input.checkValidity()) {
            input.reportValidity();  // Show the validation message
            return false;
        }
    }
    return true;
}

updateSections();

function currencyCeil(sum) {
    return sum;
}
const BYN_TO_RUB = 30;

document.body.addEventListener("click", ()=>{
    let orders = collectOrdersWithExtras();
    let total = currencyCeil(orders.total);
    let total_rub = currencyCeil(orders.total*BYN_TO_RUB);
    
    document.getElementById("total-sum").innerText = total + " BYN "+ total_rub + " RUB";
}, {passive:true});

function IDQ() {
    return "initData="+encodeURIComponent(Telegram.WebApp.initData)
}

function send_error(err) {
    return fetch("error?"+IDQ(), {
        method: "POST",
        body: err
    })
}

function mainButtonClick() {
    if(currentIndex === sections.length - 1){
        if(read_only){
            Telegram.WebApp.close();
        }
        try{
            let orders = collectOrdersWithExtras();
            fetch('orders?'+IDQ()+`&order_id=${user_order_id}`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(orders)
            }).then(response => {
                if (!response.ok) {
                    throw new Error(`Network response was not ok: ${response.status} ${response.statusText}\n${response.body}`);
                }
            }).catch(error => {
                console.error('Error:', error);
                return send_error(error);
            }).finally(()=>{
                Telegram.WebApp.close();
            });
        }catch(error){
            send_error(error);
        };
        return
    }
    if (currentIndex < sections.length - 1  && validateSection(currentIndex)) {
        currentIndex++;
        updateSections();
    }
}

function backButtonClick(){
    if (currentIndex > 0) {
        currentIndex--;
        updateSections();
    } else {
        Telegram.WebApp.close();
    }
}


Telegram.WebApp.MainButton.show();
Telegram.WebApp.MainButton.onClick(mainButtonClick);

Telegram.WebApp.MainButton.enable();
Telegram.WebApp.MainButton.show();

Telegram.WebApp.BackButton.onClick(backButtonClick);
Telegram.WebApp.BackButton.show();
window.mainButtonClick = mainButtonClick;
window.backButtonClick = backButtonClick;
