(async function(){
    
    function findParent(element, query) {
        let parent = element.parentElement;
        while (parent && !parent.matches(query)) {
            parent = parent.parentElement;
        }
        return parent;
    }

    let menu = await fetch("static/menu.json").then(r=>r.json());
    let menu_categories = [];
    for(let i in menu.categories) {
        if("position" in menu.categories[i]) {
            menu_categories.push(menu.categories[i]);
        }
    }
    menu_categories.sort((a, b) => a.position - b.position);
    for(let cat of menu_categories) {
        let cat_nav = document.createElement("a");
        let cat_menu = document.createElement("div");
        cat_menu.id = "cat-menu-"+cat.id;
        cat_nav.id = "cat-nav-"+cat.id;
        cat_menu.dataset.id = cat_nav.dataset.id = cat.id;
        cat_nav.title = cat.name_ru;
        cat_nav.innerText = cat.icon;
        cat_menu.innerHTML = `
            <h3><span>${cat.icon}</span>${cat.name_ru}</h3>
        `;
        cat_menu.classList.add("cat-menu");
        cat_nav.classList.add("cat-nav");
        cat_nav.href = "#" + cat_menu.id;
        cat_nav.addEventListener("click", ev=>{
            ev.preventDefault();
            document.getElementById(cat_menu.id).scrollIntoView({
                behavior: 'smooth'
            });
        });
        document.getElementById("menu-nav").appendChild(cat_nav);
        document.getElementById("menu-items").appendChild(cat_menu);
    }
    let menu_items = [];
    for(let i in menu.items) {
        menu_items.push(menu.items[i]);
    }
    menu_items.sort((a, b) => a.position - b.position);
    for(let item of menu_items) {
        let item_el = document.createElement("div");
        item_el.classList.add("item");
        item_el.id = "menu-item-"+item.id;
        item_el.dataset.id = item.id;
        let weight_icon = item.weight_unit == "Вес, г" ? "K" : "L";
        item_el.innerHTML = `
        <img class="image" loading="lazy" src="https://www.mealty.ru${item.image}">
        <img class="image_big" loading="lazy" src="https://www.mealty.ru${item.image_big}">
        <div class="name"><span>${item.name}</span> <span class="sub">${item.name_sub}</span></div>
        <div class="price">${item.price}</div>
        <button class="add icon">N</button>
        <div class="description">${item.description}</div>
        <div class="nutr">
            <div class="nutr-fats">${item.nutritional_value.fats}</div>
            <div class="nutr-carbohydrates">${item.nutritional_value.carbohydrates}</div>
            <div class="nutr-proteins">${item.nutritional_value.proteins}</div>
        </div>
        <div class="weight">
            <div class="weight_icon">${weight_icon}</div>
            <div class="weight_unit">${item.weight_unit}</div>
            <div class="weight">${item.weight}</div>
        </div>
        <div class="calories">
            <div class="calories">${item.calories}</div>
            <div class="calories_per_portion">${item.calories_per_portion}</div>
        </div>
        <label class="ingridients">
        <input type=checkbox>
        <div>${item.ingridients}</div>
        </label>
        `;
        let cat = "true_category" in item ? item.true_category : item.category;
        item_el.querySelector(".add").addEventListener("click", (ev)=>{
            ev.stopPropagation();
            add_item_to_carts(item.id);
        });
        item_el.addEventListener("click", ev=>{
            if(currentState.state !== "wide-view"){
                open_item(item.id);
            }
        });
        document.getElementById("cat-menu-"+cat).appendChild(item_el);
    }

    for(let el of document.querySelectorAll("#carts,#carts button.close")){
        el.addEventListener("click", (ev)=>{
            ev.stopPropagation();
            history.back();
        });
    }
    document.querySelector("#carts .days").addEventListener("click", (ev)=>ev.stopPropagation());
    let carts={};
    let cart_id = (day,meal)=>day+"_"+meal;
    function recalculate_total() {
        let total = 0;
        for(let cart_id in carts) {
            let cart_total = 0;
            let cart = carts[cart_id]
            for(let item_id of cart.items) {
                let item = menu.items[item_id];
                cart_total += item.price;
            }
            cart.total = cart_total;
            total += cart_total;
        }
        let total_info = document.querySelector("#carts .edit");
        if(total>0) {
            total_info.classList.remove("empty");
        } else {
            total_info.classList.add("empty");
        }
        total_info.querySelector(".total").innerText = `${total}`;
    }
    document.getElementById("btn-back").addEventListener("click", ev=>{
        ev.stopPropagation();
        history.back();
    });

    let currentState = {state:"initial"};
    window.addEventListener("popstate", ev=>{
        if(ev.state) {
            switch(ev.state.state){
                case "initial":
                    document.getElementById("carts").classList.remove("open");
                    document.body.classList.remove("carts-editor");
                    document.body.classList.remove("wide-view");
                    break;
                case "wide-view":
                    document.getElementById("carts").classList.remove("open");
                    document.body.classList.remove("carts-editor");
                    document.body.classList.add("wide-view");
                    break;
                case "carts-selector":
                    document.getElementById("carts").classList.add("open");
                    break;
                case "carts-editor":
                    if(document.querySelector("#carts .edit.empty")) {
                        history.back();
                    }
                    document.body.classList.add("carts-editor");
                    recalculate_carts_edit();
                    break;
            }
            if(ev.state.scroll) {
                window.scrollTo(ev.state.scroll.x, ev.state.scroll.y);
            }
            currentState = ev.state;
        } else console.log(ev);
    });

    function open_item(item_id) {
        document.body.classList.add("wide-view");
        pushHistoryState({state:"wide-view"});
        document.getElementById("menu-item-"+item_id).scrollIntoView();
    }
    function open_carts_editor() {
        recalculate_carts_edit();
        document.body.classList.add("carts-editor");
        pushHistoryState({state:"carts-editor"});
    }
    document.querySelector("#carts>.edit").addEventListener("click", ev=>{
        ev.stopPropagation();
        open_carts_editor();
    })
    function pushHistoryState(state){
        currentState.scroll = {
            x:window.scrollX,
            y:window.scrollY
        }
        history.replaceState(currentState,"","");
        history.pushState(state, "", "");
        currentState = state;
    }
    history.replaceState(currentState,"","");
    function remove_from_carts(cart, item_number) {
        carts[cart].items.splice(item_number, 1);
        recalculate_carts_edit_cart(cart);
        recalculate_total();
        if(document.querySelector("#carts .edit.empty")) {
            history.back();
        }
    }
    function recalculate_carts_edit_cart(cart_id) {
        let cart = carts[cart_id];
        let cart_el = document.querySelector(`#cart-contents>[data-day="${cart.day}"]>[data-meal="${cart.meal}"]`);
        let cart_contents = cart_el.querySelector(".contents");
        let cart_sum = cart_el.querySelector(".sum");
        let cart_total = 0;
        cart_contents.innerHTML = "";
        for(let i=0;i<cart.items.length;++i) {
            let item_id = cart.items[i];
            let item = menu.items[item_id];
            cart_total += item.price;

            let cart_item = document.createElement("div");
            cart_item.classList.add("item");
            cart_item.dataset.id = item_id;
            cart_item.dataset.pos = i;
            cart_item.innerHTML = `
            <img class="image" loading="lazy" src="https://www.mealty.ru${item.image}">
            <div class="name"><span>${item.name}</span> <span class="sub">${item.name_sub}</span></div>
            <div class="price">${item.price}</div>
            <button class="del icon">O</button>
            `;
            cart_item.querySelector(".del").addEventListener("click", (ev)=>{
                ev.stopPropagation();
                remove_from_carts(cart_id, i);
            });
            cart_contents.appendChild(cart_item);
        }
        if(cart_total === 0) {
            cart_el.classList.add("empty");
        }else{
            cart_el.classList.remove("empty");
            cart_sum.innerText = cart_total;
        }
    }
    function recalculate_carts_edit(){
        for(let cart_id in carts) {
            recalculate_carts_edit_cart(cart_id);
        }
    }
    function add_item_to_carts(item_id) {
        let carts = document.getElementById("carts");
        carts.dataset.selected = item_id;
        carts.classList.add("open");
        pushHistoryState({state:"carts-selector"});
    }
    function add_item_to_cart(day, meal, item){
        carts[cart_id(day,meal)].items.push(item);
        recalculate_total();
    }
    function select_cart(event){
        event.stopPropagation();
        let cart = event.target;
        let meal = cart.dataset.meal;
        let day = findParent(cart, "[data-day]").dataset.day;
        let carts = document.getElementById("carts")
        let item = carts.dataset.selected;
        add_item_to_cart(day,meal,item);
        history.back();
    }
    for(let cart of document.querySelectorAll("#carts [data-meal]")) {
        let meal = cart.dataset.meal;
        let day = findParent(cart, "[data-day]").dataset.day;
        cart.addEventListener("click", select_cart);
        carts[cart_id(day,meal)] = {
            day: day,
            meal: meal,
            items: [],
        };
    }
    if(window.user_carts) {
        carts = user_carts;
    }
    recalculate_total();

    function IDQ() {
        return "initData="+encodeURIComponent(Telegram.WebApp.initData)
    }
    
    function send_error(err) {
        return fetch("error?"+IDQ(), {
            method: "POST",
            body: err
        })
    }
    
    Telegram.WebApp.ready();
    Telegram.WebApp.expand();
    Telegram.WebApp.MainButton.setText("Сохранить");
    Telegram.WebApp.MainButton.onClick(()=>{
        fetch('menu?'+IDQ(), {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(carts)
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
    });
    Telegram.WebApp.MainButton.enable();
    Telegram.WebApp.MainButton.show();
})().catch(console.error);