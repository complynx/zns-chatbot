Telegram.WebApp.ready();
Telegram.WebApp.expand();

const ETransform = [[1,0,0],[0,1,0]];

let ph, pw;
let transformationMatrix = ETransform;
let frameTransformationMatrix = ETransform;
const frame_size_fraction = .9;
let W,H, Vmax, Vmin;
let frame_size, f_top, f_left;
let help_div = document.querySelector(".help");
let screen_size_source = document.querySelector(".photo");
let photo_ancor = document.querySelector(".photo .ancor");
let photo = document.querySelector(".photo img");
let frame = document.querySelector(".overlay");
let cancel_btn = document.querySelector(".button button[name=cancel]");
let submit_btn = document.querySelector(".button button[name=done]");
console.log(photo, frame, cancel_btn, submit_btn);

{// DEBUG
window.DEBUG = false;
let debug_countdown = 10;
function remove_from_dom (el) {
    el?.parentElement?.removeChild(el);
}

function init_debug() {
    if(DEBUG) return;

    DEBUG = true;
    document.body.classList.add("debug");
    let dbg_layer = document.querySelector('.debug-layer');
    let uuid_validator = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

    dbg_layer.querySelector('.link').innerText = window.location.href;
    navigator.clipboard.writeText(window.location.href)
    .catch((error) => {
        console.error('Failed to copy text: ', error);
    });
    dbg_layer.querySelector('.remotejs').addEventListener("click", ()=>{
        let input = dbg_layer.querySelector('input[name=remotejs]');
        let rjs_uuid = input.value.trim();
        if(uuid_validator.test(rjs_uuid)){
            remove_from_dom(dbg_layer.querySelector('.remotejs'));
            remove_from_dom(input);
            let s=document.createElement("script");
            s.src="https://remotejs.com/agent/agent.js";
            s.setAttribute("data-consolejs-channel",rjs_uuid);
            document.head.appendChild(s);
        }
    });  
    
    let real_frame_size = 2000;
    function decomposeTransformMatrix(transformationMatrix) {
        const [a, c, e] = transformationMatrix[0];
        const [b, d, f] = transformationMatrix[1];
    
        const scalingX = Math.sqrt(a * a + c * c);
        const scalingY = Math.sqrt(b * b + d * d);

        const rotation = Math.atan2(b, a);

        // const new_a = scalingX * Math.cos(rotation);
        // const new_b = scalingX * Math.sin(rotation);
        // const new_c = -scalingY * Math.sin(rotation);
        // const new_d = scalingY * Math.cos(rotation);
        // const new_e = e * new_a + f * new_c + e;
        // const new_f = e * new_b + f * new_d + f;
        // console.log(e,f,new_e, new_f);

        return {
            scaling: { x: scalingX, y: scalingY },
            rotation: rotation,
            translation: { x: e, y: f }
        };
    }

    function generateCroppedImage() {
        // Create a new canvas for the cropped image
        const croppedCanvas = document.createElement("canvas");
        croppedCanvas.width = real_frame_size;
        croppedCanvas.height = real_frame_size;
        const ctx = croppedCanvas.getContext("2d");
    
        // Save the current transformation matrix and set it to identity
        ctx.save();
        ctx.setTransform(1, 0, 0, 1, 0, 0);
    
        // Apply the transforms and draw the photo on the canvas
        ctx.translate(real_frame_size / 2, real_frame_size / 2);
    
        // Calculate the necessary transforms
        // const [a, c, e] = transformationMatrix[0];
        // const [b, d, f] = transformationMatrix[1];
        // ctx.transform(a, b, c, d, e, f);
    
        const decomposition = decomposeTransformMatrix(transformationMatrix);
    
        const { scaling, rotation, translation } = decomposition;
        ctx.translate(translation.x, translation.y);
        ctx.scale(scaling.x, scaling.y);
        ctx.rotate(rotation);
    
        ctx.drawImage(photo, -pw / 2, -ph / 2, pw, ph);
    
        // Restore the original transformation matrix
        ctx.restore();
    
        // Get the data URL of the cropped image
        const croppedDataURL = croppedCanvas.toDataURL("image/png");
    
        return croppedDataURL;
    }
    dbg_layer.querySelector("button.canvas").addEventListener("click", () => {
        console.log(transformationMatrix);
        const [a, c, e] = transformationMatrix[0];
        const [b, d, f] = transformationMatrix[1];
        const croppedImage = new Image();
        croppedImage.src = generateCroppedImage();
        // croppedImage.src = `./crop_frame?id=${photo_id}&a=${a}&b=${b}&c=${c}&d=${d}&e=${e}&f=${f}`;
        croppedImage.style.position="fixed";
        croppedImage.style.left = f_left + "px";
        croppedImage.style.top = f_top + "px";
        croppedImage.style.height = croppedImage.style.width = frame_size + "px";
        croppedImage.style.zIndex=4;
        document.body.appendChild(croppedImage);
        croppedImage.addEventListener("click", ()=>{
            remove_from_dom(croppedImage);
        }, false);
    });
}
document.addEventListener('contextmenu', function(event) {
    if (event.altKey && !DEBUG) {
        if(--debug_countdown <= 0) {
            init_debug();
        } 
        event.preventDefault();
    }
});
document.body.addEventListener('touchstart', function(event) {
    if(!DEBUG && e.touches.length === 5) {
        if(--debug_countdown <= 0) {
            init_debug();
        }
    }
});
}


if(Telegram.WebApp.platform === "tdesktop") {
    help_div.innerText = "Фото можно перемещать мышкой, для вращения зажмите кнопку Shift. Для масштабирования используйте прокрутку.";
} else {
    help_div.innerText = "Фото можно перемещать одним касанием. Двумя — вращать и масштабировать.\n"+
    "Если вместо перемещения фото сворачивается окно, попробуйте начать с движения вверх.";
}

/**
 * The `M` function is a matrix multiplication function that takes
 * two matrices `A` and `B` as input and returns their product as
 * a new matrix. The function uses the standard formula for matrix
 * multiplication, where each element of the resulting matrix is
 * the sum of the products of the corresponding elements of the
 * input matrices. The resulting matrix has dimensions `2x3`, which
 * is the standard size for transformation matrices in 2D graphics.
 */
function M(A, B) {
    return [
      [
        A[0][0] * B[0][0] + A[0][1] * B[1][0],
        A[0][0] * B[0][1] + A[0][1] * B[1][1],
        A[0][0] * B[0][2] + A[0][1] * B[1][2] + A[0][2],
      ],
      [
        A[1][0] * B[0][0] + A[1][1] * B[1][0],
        A[1][0] * B[0][1] + A[1][1] * B[1][1],
        A[1][0] * B[0][2] + A[1][1] * B[1][2] + A[1][2],
      ]
    ];
}

function MM(...matrices) {
    if (matrices.length < 1) return ETransform;
    if(matrices.length == 1) return matrices[0];
  
    let result = matrices[0];
  
    for (let i = 1; i < matrices.length; i++)
        result = M(result, matrices[i]);
  
    return result;
}

function createCSSTransform(transformationMatrix) {
    const [a, c, e, b, d, f] = [
        transformationMatrix[0][0],
        transformationMatrix[1][0],
        transformationMatrix[0][1],
        transformationMatrix[1][1],
        transformationMatrix[0][2],
        transformationMatrix[1][2],
    ];
  
    return `matrix(${a}, ${c}, ${e}, ${b}, ${d}, ${f})`;
}

function translate2matrix(x,y) {
    return [
        [1,0,x],
        [0,1,y]
    ];
}


function movePhoto(deltaX, deltaY) {
    let translationMatrix = translate2matrix(deltaX / frame_size * real_frame_size, deltaY / frame_size * real_frame_size);
    
    transformationMatrix = M(translationMatrix, transformationMatrix);

    recalculate_photo();
}

let transformationMatrixPreRotate;

function rotatePhotoStart() {
    transformationMatrixPreRotate = transformationMatrix;
}

function ajustCursorClientPosition(cursorX,cursorY) {
    const photoCenterX = parseFloat(photo_ancor.style.left) + parseFloat(photo.style.left) + pw / 2;
    const photoCenterY = parseFloat(photo_ancor.style.top) + parseFloat(photo.style.top) + ph / 2;

    const adjustedMouseClientX = cursorX - photoCenterX;
    const adjustedMouseClientY = cursorY - photoCenterY;
    
    return [adjustedMouseClientX, adjustedMouseClientY];
}

function rotatePhoto(rotationAtan2, mouseClientX, mouseClientY) {
    const [adjustedMouseClientX, adjustedMouseClientY] = ajustCursorClientPosition(mouseClientX, mouseClientY);

    let rotationMatrix = MM(
        translate2matrix(adjustedMouseClientX, adjustedMouseClientY),
        [
            [Math.cos(rotationAtan2), -Math.sin(rotationAtan2), 0],
            [Math.sin(rotationAtan2), Math.cos(rotationAtan2), 0]
        ],
        translate2matrix(-adjustedMouseClientX, -adjustedMouseClientY)
    );

    transformationMatrix = M(rotationMatrix, transformationMatrixPreRotate);

    recalculate_photo();
}

function scalePhoto(scaleFactor, mouseClientX, mouseClientY) {
    const [adjustedMouseClientX, adjustedMouseClientY] = ajustCursorClientPosition(mouseClientX, mouseClientY);

    let scaleMatrix = MM(
        translate2matrix(adjustedMouseClientX, adjustedMouseClientY),
        [
            [scaleFactor, 0, 0],
            [0, scaleFactor, 0]
        ],
        translate2matrix(-adjustedMouseClientX, -adjustedMouseClientY)
    );

    transformationMatrix = M(transformationMatrix, scaleMatrix);

    recalculate_photo();
}

function recalculate_photo() {
    if(ph === 0 && photo.naturalWidth === 0) return;
    if(ph !== photo.naturalHeight || pw !== photo.naturalWidth) {
        ph = photo.naturalHeight;
        pw = photo.naturalWidth;
        photo.style.top = (-ph/2) + 'px';
        photo.style.left = (-pw/2) + 'px';

        const smallerSide = Math.min(pw, ph);
        const F = real_frame_size / smallerSide;

        // Create the transformation matrix
        transformationMatrix = [
            [F, 0, 0],
            [0, F, 0]
        ];
    }

    photo.style.transform = createCSSTransform(MM(
        frameTransformationMatrix,
        transformationMatrix
    ));
}
function recalculate_all() {
    W = screen_size_source.clientWidth;
    H = screen_size_source.clientHeight;
    Vmin = Math.min(W,H);
    Vmax = Math.max(W,H);
    frame_size = frame_size_fraction*Vmin;
    f_top = ((H - frame_size)/2);
    f_left = ((W - frame_size)/2);
    const F =  frame_size / real_frame_size;
    
    frameTransformationMatrix = [
        [F, 0, 0],
        [0, F, 0]
    ];

    // frame.style.height = frame.style.width = frame_size + "px";

    // frame.style.left = f_left + "px";
    // frame.style.top = f_top + "px";
    photo_ancor.style.left = (f_left + frame_size/2) + "px";
    photo_ancor.style.top = (f_top + frame_size/2) + "px";

    recalculate_photo();
}

recalculate_all();



let isMouseDown = false, initialX, initialY, initialAngle;

let initialTouch1, initialTouch2, initialTouchDistance, initialTouchAngle;

function onTouchStart(e) {
    e.preventDefault();
    if (e.touches.length === 1) {
        initialX = e.touches[0].clientX;
        initialY = e.touches[0].clientY;
        isMouseDown = true;
    } else if (e.touches.length === 2) {
        isMouseDown = false;
        const [adjustedTouch1ClientX, adjustedTouch1ClientY] = ajustCursorClientPosition(e.touches[0].clientX, e.touches[0].clientY);
        const [adjustedTouch2ClientX, adjustedTouch2ClientY] = ajustCursorClientPosition(e.touches[1].clientX, e.touches[1].clientY);

        initialTouch1 = { x: adjustedTouch1ClientX, y: adjustedTouch1ClientY };
        initialTouch2 = { x: adjustedTouch2ClientX, y: adjustedTouch2ClientY };

        initialTouchDistance = Math.hypot(initialTouch2.x - initialTouch1.x, initialTouch2.y - initialTouch1.y);
        initialTouchAngle = Math.atan2(initialTouch2.y - initialTouch1.y, initialTouch2.x - initialTouch1.x);
    }
}

function onTouchMove(e) {
    e.preventDefault();
    if (e.touches.length === 1 && isMouseDown) {
        let deltaX = e.touches[0].clientX - initialX;
        let deltaY = e.touches[0].clientY - initialY;
        movePhoto(deltaX, deltaY);
        initialX = e.touches[0].clientX;
        initialY = e.touches[0].clientY;
    } else if (e.touches.length === 2) {
        const [adjustedTouch1ClientX, adjustedTouch1ClientY] = ajustCursorClientPosition(e.touches[0].clientX, e.touches[0].clientY);
        const [adjustedTouch2ClientX, adjustedTouch2ClientY] = ajustCursorClientPosition(e.touches[1].clientX, e.touches[1].clientY);

        let touch1 = { x: adjustedTouch1ClientX, y: adjustedTouch1ClientY };
        let touch2 = { x: adjustedTouch2ClientX, y: adjustedTouch2ClientY };

        let touchDistance = Math.hypot(touch2.x - touch1.x, touch2.y - touch1.y);
        let scaleFactor = touchDistance / initialTouchDistance;
        let centerX = (touch1.x + touch2.x) / 2;
        let centerY = (touch1.y + touch2.y) / 2;

        let touchAngle = Math.atan2(touch2.y - touch1.y, touch2.x - touch1.x);
        let rotationAngle = touchAngle - initialTouchAngle;

        let transformMatrix = MM(
            translate2matrix(centerX, centerY),
            [
                [scaleFactor * Math.cos(rotationAngle), -scaleFactor * Math.sin(rotationAngle), 0],
                [scaleFactor * Math.sin(rotationAngle), scaleFactor * Math.cos(rotationAngle), 0]
            ],
            translate2matrix(-centerX, -centerY)
        );

        transformationMatrix = M(transformationMatrix, transformMatrix);
        recalculate_photo();

        initialTouchDistance = touchDistance;
        initialTouchAngle = touchAngle;
    }
}

function onTouchEnd(e) {
    e.preventDefault();
    if (e.touches.length === 0) {
        isMouseDown = false;
    }
}

function onMouseDown(e) {
    e.preventDefault();
    initialX = e.clientX;
    initialY = e.clientY;
    rotatePhotoStart();
    isMouseDown = true;
}

function onMouseMove(e) {
    if (!isMouseDown) return;
  
    e.preventDefault();
  
    let deltaX = e.clientX - initialX;
    let deltaY = e.clientY - initialY;
  
    if (e.shiftKey) {
        let rotationAngle = Math.atan2(deltaY, deltaX);
        rotatePhoto(rotationAngle, initialX, initialY);
    } else {
        movePhoto(deltaX, deltaY);
        initialX = e.clientX;
        initialY = e.clientY;
    }
  
}

function onMouseUp(e) {
    e.preventDefault();
    isMouseDown = false;
}

function onMouseWheel(e) {
    e.preventDefault();
    let scaleAddition = 0.1;
    if (e.shiftKey) {
        scaleAddition = 0.005;
    } 
    let scaleFactor = e.deltaY > 0 ? 1-scaleAddition : 1 + scaleAddition;
    scalePhoto(scaleFactor, e.clientX, e.clientY);
}

photo.addEventListener("load", recalculate_photo, {passive:true});
window.addEventListener("resize", recalculate_all, {passive:true});

screen_size_source.addEventListener('mousedown', onMouseDown, false);
screen_size_source.addEventListener('mousemove', onMouseMove, false);
screen_size_source.addEventListener('mouseup', onMouseUp, false);
screen_size_source.addEventListener('mouseleave', onMouseUp, false);
screen_size_source.addEventListener('wheel', onMouseWheel, false);

screen_size_source.addEventListener('touchstart', onTouchStart, false);
screen_size_source.addEventListener('touchmove', onTouchMove, false);
screen_size_source.addEventListener('touchend', onTouchEnd, false);

Telegram.WebApp.MainButton.setText("Готово");
Telegram.WebApp.MainButton.show();
Telegram.WebApp.MainButton.onClick(()=>{
    const [a, c, e] = transformationMatrix[0];
    const [b, d, f] = transformationMatrix[1];
    const data = JSON.stringify({
        id: photo_id,
        a: a,
        b: b,
        c: c,
        d: d,
        e: e,
        f: f
    });

    Telegram.WebApp.sendData(data);
    Telegram.WebApp.close();
});
Telegram.WebApp.MainButton.enable();
Telegram.WebApp.MainButton.show();


Telegram.WebApp.BackButton.onClick(()=>{
    Telegram.WebApp.close();
});
Telegram.WebApp.BackButton.show();
