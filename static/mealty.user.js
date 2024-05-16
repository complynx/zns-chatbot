// ==UserScript==
// @name			MealtyZNSpatcher
// @version			1.0
// @description		Mealty patcher for Zouk Non Stop
// @author			complynx.net
// @updateURL		https://zouknonstop.com/bot/static/mealty.user.js
// @include			https://www.mealty.ru/*
// ==/UserScript==

scr = unsafeWindow.document.createElement("script");
scr.type = "module";
unsafeWindow.ZNSBotSite="https://zouknonstop.com/bot/";
scr.innerText = `import(ZNSBotSite+"static/x/mealty.js").then(console.log,console.error)`;
unsafeWindow.document.body.appendChild(scr);
