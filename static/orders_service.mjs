export function calculateMealService(selectedDishes, serviceItems) {
    const counts = new Map();

    for (const dish of selectedDishes) {
        const count = Number.isFinite(dish.count) ? Math.max(0, dish.count) : 0;
        if (count === 0) continue;

        for (const serviceKey of dish.service || []) {
            const serviceItem = serviceItems[serviceKey];
            if (!serviceItem) continue;

            if (serviceItem.kind === "utensil") {
                counts.set(serviceKey, 1);
            } else {
                counts.set(serviceKey, (counts.get(serviceKey) || 0) + count);
            }
        }
    }

    const items = [];
    let total = 0;
    for (const [serviceKey, serviceItem] of Object.entries(serviceItems)) {
        const count = counts.get(serviceKey) || 0;
        if (count === 0) continue;

        const itemTotal = count * serviceItem.price;
        items.push({
            name: serviceKey,
            count,
            price: serviceItem.price,
            total: itemTotal,
        });
        total += itemTotal;
    }

    return {items, total};
}
