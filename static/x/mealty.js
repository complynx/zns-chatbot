
console.log(ZNSBotSite);
let username;
try{
    username = localStorage["zns_username"];
}catch(e){}
if(!username) {
    username = "";
}
document.head.innerHTML += `<style>
    .zns-menu{
        font-weight: normal;
    }
    .zns-menu:not([data-state="login-wait"]) .zns-login-wait,
    .zns-menu:not([data-state="cart-mod"]) .zns-cart-mod,
    .zns-menu:not([data-state="main"]) .zns-main,
    .zns-menu:not([data-state="login"]) .zns-login{
        display: none;
    }
    .zns-menu button {
        font-size: 12px;
        font-weight: 600;
        letter-spacing: 0;
        padding: 10px 4px;
        color: black;
        border: 0;
        white-space: normal;
        text-transform: uppercase;
        border-radius: 2px;
    }
    .zns-login .error {
        color: #C82B1D;
    }
</style>`;
let top = document.getElementById("top-nav");
let zns_container = document.createElement("div");
zns_container.innerHTML = `
<div class="col-md-7 navbar-form zns-login">
    Нужно залогиниться через ЗиНуСю:
    <span class="error"></span>
    <input type="text" placeholder="Имя пользователя ТГ" class="form-control">
    <button class="btn-green">Войти</button>
</div>
<div class="col-md-7 navbar-form zns-login-wait">
    Зайди в телеграм и подтверди вход. Ждём подтверждения...
</div>
<div class="col-md-7 navbar-form zns-cart-mod">
    Наполняем корзину...
</div>
<div class="zns-main">
    <button data-cart=clean>Очистить корзину</button>
    Добавить заказ:
    &nbsp;
    <button data-meal=lunch data-day=wednesday>ср/обед</button>
    <button data-meal=dinner data-day=wednesday>ср/ужин</button>
    &nbsp;
    <button data-meal=dinner data-day=firday>пт/ужин</button>
    &nbsp;
    <button data-meal=lunch data-day=saturday>сб/обед</button>
    <button data-meal=dinner data-day=saturday>сб/ужин</button>
    &nbsp;
    <button data-meal=lunch data-day=sunday>вс/обед</button>
    <button data-meal=dinner data-day=sunday>вс/ужин</button>
    &nbsp;
    <button data-cart=all>все</button>
</div>
`;
zns_container.className = "row no-gutters top-menu zns-menu";
let login_err_span = zns_container.querySelector(".zns-login .error");
top.insertBefore(zns_container, top.children[top.children.length-1]);
let orders;
const BASKET_ADD_TIMEOUT=5000;// 5 sec
let menu;

async function add_to_cart(
    target_holder,
    offer_type,
    id,
    count,
    is_recommended,
    force_remove,
    is_heated,
    additional_guid=null
) {
    let cart_id = offer_type + "_" + id;
    let cart_amount = 1*(cart_items[cart_id]||0);
    return await new Promise((res, rej)=>{
        let finished=false;
        let timeout = setTimeout(()=>{
            if(finished) return;
            rej(new Error("add to cart timeout"));
        }, BASKET_ADD_TIMEOUT);
        document.addEventListener("basket_update", ev=>{
            if(finished) return;
            finished = true;
            clearTimeout(timeout);

            if(1*(cart_items[cart_id]||0) != cart_amount + (1*count)) {
                rej(new Error(`add to cart mismatch: was ${cart_amount}, added ${count}, got ${cart_items[cart_id]}`));
            } else {
                res(ev);
            }
        }, {
            once: true
        });
        cart_add(
            target_holder,
            offer_type,
            id,
            count,
            is_recommended,
            force_remove,
            is_heated,
            additional_guid
        );
    });
}

async function add_order(oid) {
    let out_of_stock = [];
    for(let id in orders[oid].items) {
        let item = document.querySelector(`.catalog-item[data-product_id="${id}"]:not(.out-of-stock)`);
        if(item) {
            await add_to_cart(
                {target:item.querySelector(".meal-card__buttons .add_button_plus")},
                item.querySelector(".meal-card__offer_type_id").innerText,
                id,
                orders[oid].items[id]
            );
        } else {
            out_of_stock.push(id);
        }
    }
    return out_of_stock;
}

function show_out_of_stock(out_of_stock){
    if(out_of_stock.length>0) {
        let oos_dict = {};
        for(let item of out_of_stock) {
            if(!oos_dict[item]) 
                oos_dict[item] = 0;
            oos_dict[item] += 1;
        }
        let texts = [];
        for(let item in oos_dict) {
            let count = oos_dict[item];
            let item_name = menu.items[item].name;
            texts.push(`${item_name}, количество заказов: ${count}`);
        }
        fetch(ZNSBotSite + "food_get_orders?x="+Math.random(), {
            credentials: 'include',
            method: "POST",
            body: JSON.stringify(oos_dict)
        }).then(r=>{
            if(r.status >=400)
                throw new Error("request not successful");
            alert("В заказах есть отсутствующие позиции:\n" + texts.join("\n") + "\nИнформация передана боту.");
        }).catch(err=>{
            alert("В заказах есть отсутствующие позиции:\n" + texts.join("\n") + "\nСкопируй и передай Дане: " + JSON.stringify(oos_dict));
        });
    }
}

zns_container.querySelectorAll(".zns-menu .zns-main button[data-cart]").forEach(btn=>{
    btn.addEventListener("click", ev=>(async function(){
        try{
            zns_container.dataset.state="cart-mod";
            if(btn.dataset.cart == "clean") {
                for(let item of document.querySelectorAll(".basket-body .basket__item.product")) {
                    let remove = item.querySelector(".basket__item-remove");
                    let id = item.querySelector("[data-product-id]").dataset.productId;
                    let cart_id = "1_"+id;
                    let num = cart_items[cart_id];
                    if(num) {
                        await add_to_cart({target:remove}, 1, id, -num, false, true);
                    }
                }
            } else if(btn.dataset.cart == "all") {
                let out_of_stock = [];
                for(let oid in orders) {
                    let out_of_stock_1 = await add_order(oid);
                    out_of_stock = out_of_stock.concat(...out_of_stock_1);
                }
                show_out_of_stock(out_of_stock);
            }
        } catch(e){
            console.error(e);
            alert("При изменении корзины возникли ошибки. Попробуй ещё раз. Если будет повторяться, зови специалиста.");
        } finally{
            zns_container.dataset.state="main";
        }
    })().catch(console.error));
});
zns_container.querySelectorAll(".zns-menu .zns-main button[data-meal]").forEach(btn=>{
    btn.addEventListener("click", ev=>(async function(){
        try{
            zns_container.dataset.state="cart-mod";
            let oid = btn.dataset.day + "_" + btn.dataset.meal;
            let out_of_stock = await add_order(oid);
            show_out_of_stock(out_of_stock);
        } catch(e){
            console.error(e);
            alert("При изменении корзины возникли ошибки. Попробуй ещё раз. Если будет повторяться, зови специалиста.");
        } finally{
            zns_container.dataset.state="main";
        }
    })().catch(console.error));
});

function init(){
    fetch(ZNSBotSite + "food_get_orders?x="+Math.random(), {credentials: 'include'}).then(r=>{
        if(r.status >= 500) {
            login_err_span.innerText = "Что-то пошло не так...";
            zns_container.dataset.state="login";
            throw new Error("request not successful")
        }
        if(r.status >=400 && r.status < 500) {
            login_err_span.innerText = "Авторизация отклонена.";
            zns_container.dataset.state="login";
            throw new Error("request not successful")
        }
        return r.json();
    }).then(r=>{
        zns_container.dataset.state="main";
        orders = r.orders;
        menu = r.menu;
        window.zns_orders = r.orders;
    }).catch(console.error);
}

fetch(ZNSBotSite + "auth?check=1&x="+Math.random(), {credentials: 'include'}).then(r=>r.json()).then(r=>{
    if(r.result != "authorized") {
        zns_container.dataset.state="login";
        let input = zns_container.querySelector(".zns-login input");
        let btn = zns_container.querySelector(".zns-login button");
        input.value = username;
        btn.addEventListener("click", ev=>{
            if(input.value == "") {
                login_err_span.innerText = "Введи имя пользователя!";
                return;
            }
            zns_container.dataset.state="login-wait";
            fetch(
                ZNSBotSite + "auth?username="+encodeURIComponent(input.value)+"&x="+Math.random(),
                {credentials: 'include'}
            ).then(r=>{
                if(r.status == 404) {
                    login_err_span.innerText = "Имя пользователя не найдено.";
                    zns_container.dataset.state="login";
                    return;
                }
                if(r.status >= 500) {
                    login_err_span.innerText = "Что-то пошло не так...";
                    zns_container.dataset.state="login";
                    return;
                }
                if(r.status >=400 && r.status < 500) {
                    login_err_span.innerText = "Авторизация отклонена.";
                    zns_container.dataset.state="login";
                    return;
                }
                try{
                    localStorage["zns_username"]=input.value;
                }catch(e){}
                init();
            });
        });
    } else {
        init();
    }
})
