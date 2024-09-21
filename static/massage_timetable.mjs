function q(q, el=document){
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

function IDQ() {
    return "initData="+encodeURIComponent(Telegram.WebApp.initData)
}
    
function send_error(err) {
    return fetch("error?"+IDQ(), {
        method: "POST",
        body: err
    })
}

let lc = Telegram.WebApp?.initDataUnsafe?.user?.language_code ?? "ru";
if(!(lc in ["ru","en"])) {
    lc = "en";
}
moment.locale(lc);
document.documentElement.lang = lc;
let timetable_div = I("timetable");
let togglers=q(".timetable_togglers")

fetch("./massage_timetable_data?"+ IDQ()).then(r=> r.json()).then(timetable_data=>{
    timetable_data.early_comer_tolerance = moment.duration(timetable_data.early_comer_tolerance, 'seconds');
    console.log(timetable_data);
    let parties = {};
    for(let party of timetable_data.parties){
        party.start = moment(party.start);
        party.end = moment(party.end);
        party.start_early = moment(party.start).subtract(timetable_data.early_comer_tolerance);
        party.end_late = moment(party.end).add(timetable_data.early_comer_tolerance);
        party.duration = party.end.diff(party.start)
        party.duration_x = party.end_late.diff(party.start_early)
        party.id = party.start.date();
        parties[party.start.date()] = party;
        party.toggler = document.createElement("li");
        party.toggler.innerHTML = `<label for="timetable_toggle_${party.id}">${party.start.format("dd")}-${party.end.format("dd")}</label>`;
        togglers.appendChild(party.toggler);
        party.toggler_input=document.createElement("input");
        party.toggler_input.type="radio";
        party.toggler_input.name="timetable_toggle";
        party.toggler_input.id=`timetable_toggle_${party.id}`;
        party.toggler_input.value=`${party.id}`;
        timetable_div.appendChild(party.toggler_input);
        party.div = document.createElement("div");
        party.div.classList.add("timetable");
        party.div.id = `timetable_${party.id}`;
        timetable_div.appendChild(party.div);
        party.name_div = document.createElement("div");
        party.name_div.classList.add("party_name");
        party.name_div.innerText = `${party.start.format("dd")}-${party.end.format("dd")}`;
        party.div.appendChild(party.name_div);
    }
    let in_which_party = (m) => {
        let mm = moment(m);
        for(let p in parties) {
            let party = parties[p]; 
            if(mm.isSameOrBefore(party.end_late)
                && mm.isSameOrAfter(party.start_early))
                return party;
        }
    };
    {
        let now_msk = moment(moment().utc().utcOffset("+03:00").format("YYYY-MM-DDTkk:mm:ss"));
        console.log(now_msk.format(), moment().format());
        let party = in_which_party(now_msk);
        if(party){
            party.now = now_msk;
            I(`timetable_toggle_${party.id}`).checked = true;

            let now_div = document.createElement("DIV");
            now_div.innerText = ` `;
            now_div.classList.add("current-time");
            now_div.style.top = (100. * party.now.diff(party.start_early) / party.duration_x ) + "%";
            party.div.appendChild(now_div);
        }
    }
    function specialist_div(party, specialist) {
        let div = q(`div.specialist[data-id="${specialist.id}"]`, party.div);
        if(!div) {
            div = document.createElement("DIV");
            div.innerHTML=`<div class="name">${specialist.icon} ${specialist.name}</div>`;
            div.classList.add("specialist");
            div.dataset.id=specialist.id;
            party.div.appendChild(div);
        }
        return div;
    }
    for(let specialist_id in timetable_data.specialists) {
        let specialist = timetable_data.specialists[specialist_id];
        specialist.id = parseInt(specialist_id);
        timetable_data.specialists[specialist.id] = specialist;
        if((Telegram.WebApp?.initDataUnsafe?.user?.id ?? -1) == specialist.id) {
            specialist.myself = true;
        }
        for(let wh of specialist.work_hours) {
            wh.start = moment(wh.start);
            wh.end = moment(wh.end);
            let party = in_which_party(wh.start);
            if(!party) {
                console.warn("working hours outside party time??", wh, specialist);
                continue;
            }
            console.log("working hours inside party time", wh, specialist);
            let div = specialist_div(party, specialist);
            if(specialist.myself) {
                div.classList.add("myself");
            }

            wh.div = document.createElement("DIV");
            wh.div.innerText = ` `;
            wh.div.classList.add("wh");
            wh.div.style.top = (100. * wh.start.diff(party.start_early) / party.duration_x ) + "%";
            wh.div.style.height = (100. * wh.end.diff(wh.start) / party.duration_x ) + "%";
            div.appendChild(wh.div);
        }
    }
    for(let massage of timetable_data.massages) {
        massage.start = moment(massage.start);
        massage.end = moment(massage.end);
        let party = in_which_party(massage.start);
        if(!party){
            console.error("massage outside party time", massage);
            continue;
        }
        let specialist = timetable_data.specialists[parseInt(massage.specialist)];
        if(!specialist){
            console.error("massage specialist not found", massage);
            continue;
        }
        let div = specialist_div(party, specialist);
        let massage_div = document.createElement("DIV");
        massage_div.innerText = `${massage.user.name} ${massage.price} ₽ / ${massage.duration}'
${massage.start.format("HH:mm")}
`;
        massage_div.title = `${massage.user.name} ${massage.price} ₽ / ${massage.duration}'`;
        massage_div.classList.add("massage");
        massage_div.classList.add(`massage-id-${massage.id}`);
        massage_div.style.top = (100. * massage.start.diff(party.start_early) / party.duration_x ) + "%";
        massage_div.style.height = (100. * massage.end.diff(massage.start) / party.duration_x ) + "%";
        div.appendChild(massage_div);
    }
}).catch(console.error);


const timetable = document.getElementById('timetable');
const minHeight = timetable.offsetHeight;

// Prevent default browser behavior for pinch-zoom
const preventDefault = (event) => {
    if (event.touches.length > 1) {
        event.preventDefault();
        event.stopPropagation();
    }
};

// Function to handle zoom
const handleZoom = (delta, centerX, centerY) => {
    let currentHeight = timetable.offsetHeight;
    let newHeight = currentHeight + delta;

    if (newHeight < minHeight) {
        newHeight = minHeight;
    }
    if (newHeight >= minHeight * 1.6) {
        timetable.style.setProperty('--font-size', '1em');
    } else {
        timetable.style.setProperty('--font-size', '0.6em');
    }

    // Calculate the scaling factor
    let scale = newHeight / currentHeight;

    // Get the current scroll position
    let scrollLeft = timetable.scrollLeft;
    let scrollTop = timetable.scrollTop;

    // Get the current bounding rectangle
    let rect = timetable.getBoundingClientRect();

    // Calculate the new scroll position
    let newScrollLeft = centerX - rect.left + scrollLeft - (centerX - rect.left) * scale;
    let newScrollTop = centerY - rect.top + scrollTop - (centerY - rect.top) * scale;

    // Apply the new height
    timetable.style.height = `${newHeight}px`;

    // Adjust the scroll position
    timetable.scrollLeft = newScrollLeft;
    timetable.scrollTop = newScrollTop;
};

// Pinch-zoom event listeners
let initialDistance = null;

const pinchZoomStart = (event) => {
    event.preventDefault();
    if (event.touches.length === 2) {
        const touch1 = event.touches[0];
        const touch2 = event.touches[1];

        initialDistance = Math.sqrt(
            Math.pow(touch2.clientX - touch1.clientX, 2) +
            Math.pow(touch2.clientY - touch1.clientY, 2)
        );
    }
};

const pinchZoomMove = (event) => {
    event.preventDefault();
    if (event.touches.length === 2) {
        const touch1 = event.touches[0];
        const touch2 = event.touches[1];

        const distance = Math.sqrt(
            Math.pow(touch2.clientX - touch1.clientX, 2) +
            Math.pow(touch2.clientY - touch1.clientY, 2)
        );

        if (initialDistance) {
            const delta = distance - initialDistance;
            const centerX = (touch1.clientX + touch2.clientX) / 2;
            const centerY = (touch1.clientY + touch2.clientY) / 2;
            handleZoom(delta, centerX, centerY);
            initialDistance = distance;
        }
    }
};

const pinchZoomEnd = (event) => {
    event.preventDefault();
    initialDistance = null;
};

timetable.addEventListener('touchstart', pinchZoomStart, { passive: false });
timetable.addEventListener('touchmove', pinchZoomMove, { passive: false });
timetable.addEventListener('touchend', pinchZoomEnd);

// Ctrl+Scroll event listener
const scrollHandler = (event) => {
    if (event.shiftKey) {
        event.preventDefault();
        const delta = event.deltaY * -5;
        handleZoom(delta, event.clientX, event.clientY);
    }
};

window.addEventListener('wheel', scrollHandler);