Telegram.WebApp.ready();
Telegram.WebApp.expand();

const ETransform = [[1,0,0],[0,1,0]];

let ph, pw;
let transformationMatrix = ETransform;
let frameTransformationMatrix = ETransform;
const frame_size_fraction = .9;
let W,H, Vmax, Vmin;
let frame_size, f_top, f_left;
let screen_size_source = document.querySelector(".photo");
let photo_ancor = document.querySelector(".photo .ancor");
let photo = document.querySelector(".photo img");
let frame = document.querySelector(".overlay>div:nth-child(1)");
let frame_top = document.querySelector(".overlay>div:nth-child(2)");
let frame_left = document.querySelector(".overlay>div:nth-child(3)");
let frame_right = document.querySelector(".overlay>div:nth-child(4)");
let frame_bottom = document.querySelector(".overlay>div:nth-child(5)");
let cancel_btn = document.querySelector(".button button[name=cancel]");
let submit_btn = document.querySelector(".button button[name=done]");
console.log(photo, frame, cancel_btn, submit_btn);


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

function rotatePhoto(rotationAtan2, mouseClientX, mouseClientY) {
    const photoCenterX = parseFloat(photo_ancor.style.left) + parseFloat(photo.style.left) + pw / 2;
    const photoCenterY = parseFloat(photo_ancor.style.top) + parseFloat(photo.style.top) + ph / 2;

    const adjustedMouseClientX = mouseClientX - photoCenterX;
    const adjustedMouseClientY = mouseClientY - photoCenterY;

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
    const photoCenterX = parseFloat(photo_ancor.style.left) + parseFloat(photo.style.left) + pw / 2;
    const photoCenterY = parseFloat(photo_ancor.style.top) + parseFloat(photo.style.top) + ph / 2;

    const adjustedMouseClientX = mouseClientX - photoCenterX;
    const adjustedMouseClientY = mouseClientY - photoCenterY;

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

    frame_left.style.height = frame_right.style.height = frame.style.height = frame.style.width = frame_size + "px";

    frame.style.left = f_left + "px";
    frame_right.style.top = frame_left.style.top = frame.style.top = f_top + "px";
    frame_left.style.right = frame_right.style.left = (f_left + frame_size) + "px";
    frame_top.style.bottom = frame_bottom.style.top = (f_top + frame_size) + "px";
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
        rotatePhotoStart();
        initialTouch1 = { x: e.touches[0].clientX, y: e.touches[0].clientY };
        initialTouch2 = { x: e.touches[1].clientX, y: e.touches[1].clientY };
        initialTouchDistance = Math.hypot(initialTouch2.x - initialTouch1.x, initialTouch2.y - initialTouch1.y);
        initialTouchAngle = Math.atan2(initialTouch2.y - initialTouch1.y, initialTouch2.x - initialTouch1.x) * (180 / Math.PI);
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
        let touch1 = { x: e.touches[0].clientX, y: e.touches[0].clientY };
        let touch2 = { x: e.touches[1].clientX, y: e.touches[1].clientY };
        let touchDistance = Math.hypot(touch2.x - touch1.x, touch2.y - touch1.y);
        let scaleFactor = touchDistance / initialTouchDistance;
        let centerX = (touch1.x + touch2.x) / 2;
        let centerY = (touch1.y + touch2.y) / 2;
        scalePhoto(scaleFactor, centerX, centerY);
        initialTouchDistance = touchDistance;

        let touchAngle = Math.atan2(touch2.y - touch1.y, touch2.x - touch1.x);
        let rotationAngle = touchAngle - initialTouchAngle;
        rotatePhoto(rotationAngle + initialAngle, centerX, centerY);
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
