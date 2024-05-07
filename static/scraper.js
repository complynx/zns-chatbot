// scrape
(()=>{
    let categories = {};
    let items = {};
    let chefs_reverse = {};
    let chefs = {};

    function findParent(element, className) {
        let parent = element.parentElement;
        while (parent && !parent.classList.contains(className)) {
            parent = parent.parentElement;
        }
        return parent;
    }
    for(let category of document.querySelectorAll(".category-wrapper")) {
        let catId = parseInt(category.dataset.category);
        categories[catId] = {
            id: catId,
            name: category.dataset.categoryName,
            name_ru: category.querySelector(".menu-category-title").innerText,
        };
    }
    
    for(let item of document.querySelectorAll(`.category-wrapper:not([data-category="0"]) .catalog-item:not(.out-of-stock)`)) {
        let category = findParent(item, "category-wrapper").dataset.category;
        let itemId = parseInt(item.dataset.product_id);
        let chef = {
            name: item.querySelector(".hidden .meal-card__chef-name").innerText,
            profession: item.querySelector(".hidden .meal-card__chef-profession").innerText,
            photo: item.querySelector(".hidden .meal-card__chef-photo img").dataset.fancyboxSrc,
        };
        let chef_rev = `${chef.name}|${chef.profession}|${chef.photo}`;
        let chef_id;
        if(chef_rev in chefs_reverse) {
            chef_id = chefs_reverse[chef_rev];
        } else {
            chef_id = chef.id = Object.keys(chefs).length;
            chefs[chef.id] = chef;
            chefs_reverse[chef_rev] = chef.id;
        }
        items[itemId] = {
            seller_product_id: parseInt(item.dataset.sellerProduct_id),
            id: itemId,
            offer_type_id: item.querySelector(".meal-card__offer_type_id").innerText,
            priority: parseInt(item.dataset.priority),
            category: parseInt(category),
            name: item.querySelector(".meal-card__name").innerText,
            name_sub: item.querySelector(".meal-card__name-note").innerText,
            image: item.querySelector("img[data-src]").dataset.src,
            price: parseInt(item.querySelector(".hidden .meal-card__price").innerText),
            image_big: item.querySelector(".hidden .meal-card__photo img").dataset.fancyboxSrc,
            chef: chef_id,
            description: item.querySelector(".meal-card__description").innerText,
            weight: parseInt(item.querySelector(".meal-card__weight").innerText),
            weight_unit: item.querySelector(".meal-card__weight_unit").innerText,
            calories: parseInt(item.querySelector(".meal-card__calories").innerText),
            calories_per_portion: parseInt(item.querySelector(".meal-card__calories__portion").innerText),
            shelf_life: item.querySelector(".meal-card__shelf_life").innerText,
            ingridients: item.querySelector(".meal-card__products").innerText,
            nutritional_value: {
                fats: parseFloat(item.querySelector(".meal-card__fats").innerText.replace(",",".")),
                carbohydrates: parseFloat(item.querySelector(".meal-card__carbohydrates").innerText.replace(",",".")),
                proteins: parseFloat(item.querySelector(".meal-card__proteins").innerText.replace(",",".")),
            }
        };
    }
    return {items:items, categories: categories, chefs: chefs};
})()

// add to cart
((select)=>{
    for(let item of document.querySelectorAll(".basket-body .basket__item.product .basket__item-remove")) {
        item.onclick({target:item});
    }

    let out_of_stock = [];
    for(let id in select) {
        let obj = select[id];
        let item = document.querySelector(`.catalog-item[data-product_id="${id}"]:not(.out-of-stock)`);
        if(item) {
            cart_add({target:item.querySelector(".meal-card__buttons .add_button_plus")},obj.offer_type_id,id,obj.amount);
        } else {
            out_of_stock.push(id);
        }
    }
    console.warn(out_of_stock);
})({
    6:{offer_type_id:"1",amount:8},
    82:{offer_type_id:"1",amount:35},
    263:{offer_type_id:"1",amount:24},
    123123:{offer_type_id:"1",amount:3},
})