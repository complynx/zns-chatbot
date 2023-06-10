function $(q, el=document){
    return el.querySelector(q);
}
function $$(q, el=document){
    return el.querySelectorAll(q);
}
function I(id, doc=document){
    return doc.getElementById(id);
}

Telegram.WebApp.ready();
Telegram.WebApp.expand();
Telegram.WebApp.MainButton.setText("Закрыть");
Telegram.WebApp.MainButton.show();
Telegram.WebApp.MainButton.onClick(()=>{
    Telegram.WebApp.close();
});
Telegram.WebApp.MainButton.enable();
Telegram.WebApp.MainButton.show();
Telegram.WebApp.BackButton.onClick(()=>{
    Telegram.WebApp.close();
});


let workload_div = I("workload");
moment.locale("ru");
let parties = {
    4: {
        start: moment("2023-06-09 18:00:00"),
        end: moment("2023-06-10 07:00:00"),
    },
    5: {
        start: moment("2023-06-10 13:00:00"),
        end: moment("2023-06-11 07:00:00"),
    },
    6: {
        start: moment("2023-06-11 13:00:00"),
        end: moment("2023-06-12 07:00:00"),
    }
};
for(let p in parties) {
    let party = parties[p];
    party.id = p;
    party.massages=[];
    party.working_hours=[];
    party.div = I(`workload_${p}`);
}

let in_which_party = (m) => {
    let mm = moment(m);
    for(let p in parties) {
        let party = parties[p]; 
        if(mm.isSameOrBefore(party.end) && mm.isSameOrAfter(party.start))
            return party;
    }
};

fetch("./massage_system").then(r=> r.json()).then(massage_system=>{
    console.log(massage_system);
    let i = 0;
    for(let type of massage_system.massage_types) {
        type.duration = moment.duration(type.duration, 'seconds');
        type.index=i++;
    }
    for(let masseur_id in massage_system.masseurs) {
        massage_system.masseurs[masseur_id].id = masseur_id;
    }
    for(let massage_id in massage_system.massages) {
        let massage = massage_system.massages[massage_id];
        massage.massage_type = massage_system.massage_types[massage.massage_type_index];
        massage.start=moment(massage.start);
        massage.end=massage.start.clone().add(massage.massage_type.duration);
        massage.id = massage_id;
        massage.masseur = massage_system.masseurs[massage.masseur_id];
        if(!massage.masseur){
            console.error("undefined massage masseur", massage);
            continue;
        }
        let party = in_which_party(massage.start);
        if(!party) {
            console.error("undefined massage party", massage);
            continue;
        }
        massage.party = party;
        party.massages.push(massage);
    }
    for(let wh of massage_system.working_hours) {
        wh.start=moment(wh.start);
        wh.end=moment(wh.end);
        wh.masseur = massage_system.masseurs[wh.masseur_id];
        if(!wh.masseur){
            console.error("undefined wh masseur", wh);
            continue;
        }
        let party = in_which_party(wh.start);
        if(!party) {
            console.error("undefined wh party", wh);
            continue;
        }
        wh.party = party;
        party.working_hours.push(wh);
    }
    {
        let party = in_which_party(moment());
        if(party){
            party.now = moment();
        }
    }

    for(let p in parties) {
        let party = parties[p]; 
        let party_duration = party.end.diff(party.start);
        for(let wh of party.working_hours){
            let masseur = wh.masseur;
            let masseur_div = $(`div.masseur.masseur-id-${masseur.id}`, party.div);
            if(!masseur_div) {
                masseur_div = document.createElement("DIV");
                masseur_div.innerHTML=`<div class="masseur_name">${masseur.icon} ${masseur.name}</div>`;
                masseur_div.classList.add("masseur");
                masseur_div.classList.add(`masseur-id-${masseur.id}`);
                party.div.appendChild(masseur_div);
            }
            
            let wh_div = document.createElement("DIV");
            wh_div.innerText = ` `;
            wh_div.classList.add("wh");
            wh_div.style.top = (100. * wh.start.diff(party.start) / party_duration ) + "%";
            wh_div.style.height = (100. * wh.end.diff(wh.start) / party_duration ) + "%";
            masseur_div.appendChild(wh_div);
        }
        for(let massage of party.massages){
            let masseur = massage.masseur;
            let masseur_div = $(`div.masseur.masseur-id-${masseur.id}`, party.div);
            
            let massage_div = document.createElement("DIV");
            massage_div.innerText = `${massage.client_name} ${massage.massage_type.name}`;
            massage_div.title = `${massage.client_name} ${massage.massage_type.name}`;
            massage_div.classList.add("massage");
            massage_div.classList.add(`massage-id-${massage.id}`);
            massage_div.style.top = (100. * massage.start.diff(party.start) / party_duration ) + "%";
            massage_div.style.height = (100. * massage.end.diff(massage.start) / party_duration ) + "%";
            masseur_div.appendChild(massage_div);
        }
        if(party.now) {
            let now_div = document.createElement("DIV");
            now_div.innerText = ` `;
            now_div.classList.add("current-time");
            now_div.style.top = (100. * party.now.diff(party.start) / party_duration ) + "%";
            party.div.appendChild(now_div);
        }
    }
}).catch(console.error);
