let $ = (q, el=document)=> el.querySelector(q);
let $$ = (q, el=document)=> el.querySelectorAll(q);
let I = (id, doc=document)=> doc.getElementById(id);

let workload_div = I("workload");

fetch("./massage_system").then(r=> r.json()).then(massage_system=>{
    
}).catch(console.error);
