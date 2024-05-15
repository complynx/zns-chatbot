// ==UserScript==
// @name			MealtyZNSpatcher
// @version			1.0
// @description		Mealty patcher for Zouk Non Stop
// @author			complynx.net
// @license 		MIT
// @homepage		https://zouknonstop.com/bot/static/mealty.user.js
// @encoding		utf-8
// @include			https://www.mealty.ru/*
// ==/UserScript==

let script = document.createElement('script');
script.type = "module";
script.src = "https://zouknonstop.com/bot/static/mealty.mjs"
document.head.appendChild(script);

