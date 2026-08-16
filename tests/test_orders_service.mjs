import assert from "node:assert/strict";
import test from "node:test";

import {calculateMealService} from "../static/orders_service.mjs";

const serviceItems = {
    soup_container: {kind: "container", price: 1.5},
    hot_container: {kind: "container", price: 1.5},
    salad_container: {kind: "container", price: 1.5},
    spoon: {kind: "utensil", price: 0.3},
    fork: {kind: "utensil", price: 0.3},
    knife: {kind: "utensil", price: 0.3},
};

test("reuses one fork for several dishes in the same meal", () => {
    const result = calculateMealService([
        {count: 1, service: ["hot_container", "fork"]},
        {count: 1, service: ["hot_container", "fork"]},
    ], serviceItems);

    assert.deepEqual(result.items, [
        {name: "hot_container", count: 2, price: 1.5, total: 3},
        {name: "fork", count: 1, price: 0.3, total: 0.3},
    ]);
    assert.equal(result.total, 3.3);
});

test("adds containers per portion but utensils once", () => {
    const result = calculateMealService([
        {count: 2, service: ["soup_container", "spoon"]},
        {count: 3, service: ["salad_container", "fork"]},
    ], serviceItems);

    assert.deepEqual(result.items, [
        {name: "soup_container", count: 2, price: 1.5, total: 3},
        {name: "salad_container", count: 3, price: 1.5, total: 4.5},
        {name: "spoon", count: 1, price: 0.3, total: 0.3},
        {name: "fork", count: 1, price: 0.3, total: 0.3},
    ]);
    assert.equal(result.total, 8.1);
});
