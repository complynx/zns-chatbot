
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

function add_order(oid) {
    let out_of_stock = [];
    for(let id in orders[oid].items) {
        let item = document.querySelector(`.catalog-item[data-product_id="${id}"]:not(.out-of-stock)`);
        if(item) {
            cart_add(
                {target:item.querySelector(".meal-card__buttons .add_button_plus")},
                item.querySelector(".meal-card__offer_type_id").innerText,
                id,
                orders[oid].items[id]
            );
        } else {
            out_of_stock.push(id);
        }
    }
    console.warn(out_of_stock);
}

zns_container.querySelectorAll(".zns-menu .zns-main button[data-cart]").forEach(btn=>{
    btn.addEventListener("click", ev=>{
        if(btn.dataset.cart == "clean") {
            for(let item of document.querySelectorAll(".basket-body .basket__item.product .basket__item-remove")) {
                item.onclick({target:item});
            }
        } else if(btn.dataset.cart == "all") {
            for(let oid in orders) {
                add_order(oid);
            }
        }
    });
});
zns_container.querySelectorAll(".zns-menu .zns-main button[data-meal]").forEach(btn=>{
    btn.addEventListener("click", ev=>{
        let oid = btn.dataset.day + "_" + btn.dataset.meal;
        add_order(oid);
    });
});

function init(){
    fetch(ZNSBotSite + "food_get_orders", {credentials: 'include'}).then(r=>{
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
    }).catch(console.error);
}

fetch(ZNSBotSite + "auth?check=1", {credentials: 'include'}).then(r=>r.json()).then(r=>{
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
            fetch(ZNSBotSite + "auth?username="+encodeURIComponent(input.value), {credentials: 'include'}).then(r=>{
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
