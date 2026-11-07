"use strict";

(() => {
  const status = document.getElementById("status");
  const name = document.getElementById("name");
  const retry = document.getElementById("retry");
  const accountSection = document.getElementById("account");
  const accountTitle = document.getElementById("account-title");
  const accountStatus = document.getElementById("account-status");
  const accountEvents = document.getElementById("account-events");
  const accountRetry = document.getElementById("account-retry");
  const accountFirst = document.getElementById("account-first");
  const accountNext = document.getElementById("account-next");
  const storageKey = "zinusya-miniapp-v1";
  // Telegram appends initialization parameters inside the requested fragment.
  const launchTarget = (window.location?.hash || "").slice(1).split("?")[0];
  const words = {
    en: { connecting: "Connecting…", connected: "Connected", reopen: "Close ZiNuSya and open it again from Telegram.",
      unavailable: "Could not connect. Try again.", outside: "Open this page from the ZiNuSya Telegram bot.", retry: "Retry connection" },
    ru: { connecting: "Подключаюсь…", connected: "Подключено", reopen: "Закрой ЗиНуСю и открой её заново из Telegram.",
      unavailable: "Не удалось подключиться. Попробуй ещё раз.", outside: "Открой эту страницу из Telegram-бота ЗиНуСя.", retry: "Повторить подключение" }
  };
  const accountWords = {
    en: { title: "My registrations", loading: "Loading…", empty: "No registrations.", updated: "Updated", refresh: "Refresh",
      first: "First page", next: "Next page", unavailable: "Could not load registrations. Retry this page.",
      disabled: "Registration module unavailable. Pass and payment details are hidden.",
      leader: "Leader", follower: "Follower", waitlisted: "Waiting list", assigned: "Pass assigned", cancelled: "Cancelled",
      price: "Pass price", confirmed: "Confirmed paid", remaining: "Remaining", deposit: "Required deposit",
      pending: "Proofs awaiting human confirmation", due: "Evidence deadline", final: "Final payment deadline",
      paid: "Paid — confirmed", depositPaid: "Deposit confirmed", unpaid: "Payment not fully confirmed",
      overdue: "The deadline has passed. The pass remains assigned; contact an event ambassador." },
    ru: { title: "Мои регистрации", loading: "Загружаю…", empty: "Регистраций пока нет.", updated: "Обновлено", refresh: "Обновить",
      first: "Первая страница", next: "Следующая страница", unavailable: "Не удалось загрузить регистрации. Повтори эту страницу.",
      disabled: "Модуль регистрации недоступен. Данные пасса и оплаты скрыты.",
      leader: "Лидер", follower: "Фолловер", waitlisted: "Очередь ожидания", assigned: "Пасс назначен", cancelled: "Отменено",
      price: "Цена пасса", confirmed: "Подтверждено оплачено", remaining: "Осталось", deposit: "Минимальная предоплата",
      pending: "Подтверждения, ожидающие проверки человеком", due: "Срок отправки подтверждения", final: "Срок доплаты",
      paid: "Оплата подтверждена полностью", depositPaid: "Предоплата подтверждена", unpaid: "Оплата подтверждена не полностью",
      overdue: "Срок прошёл. Пасс остаётся назначенным; свяжись с амбассадором эвента." }
  };
  const defaultLanguage = /^(ru|be|uk)(-|$)/i.test(navigator.language) ? "ru" : "en";
  let language = defaultLanguage;
  let session = {};
  let busy = false;
  let connecting = false;
  let profileVersion;
  const profileSection = document.getElementById("profile");
  const profileStatus = document.getElementById("profile-status");
  const profileFirstName = document.getElementById("profile-first-name");
  const profileLastName = document.getElementById("profile-last-name");
  const profileLanguage = document.getElementById("profile-language");
  const profileSaveName = document.getElementById("profile-save-name");
  const profileSaveLanguage = document.getElementById("profile-save-language");
  const profileRefresh = document.getElementById("profile-refresh");
  const profileWords = {
    en: { title: "My profile", firstName: "First name", lastName: "Last name", language: "Bot language", auto: "Automatic", saveName: "Save name", saveLanguage: "Save language", refresh: "Refresh profile", saved: "Profile saved.", loading: "Loading profile…", unavailable: "Could not load profile. Refresh to retry.", retry: "Could not confirm the change. Press Save again to retry the same change.", conflict: "The profile changed elsewhere. Refresh and review it before saving.", invalid: "Check the name or language and try again." },
    ru: { title: "Мой профиль", firstName: "Имя", lastName: "Фамилия", language: "Язык бота", auto: "Автоматически", saveName: "Сохранить имя", saveLanguage: "Сохранить язык", refresh: "Обновить профиль", saved: "Профиль сохранён.", loading: "Загружаю профиль…", unavailable: "Не удалось прочитать профиль. Нажми «Обновить профиль».", retry: "Не удалось подтвердить изменение. Нажми «Сохранить» ещё раз для повтора того же изменения.", conflict: "Профиль изменён в другом месте. Обнови его и проверь перед сохранением.", invalid: "Проверь имя или язык и попробуй снова." }
  };
  const detailsSection = document.getElementById("details");
  const detailsStatus = document.getElementById("details-status");
  const detailsName = document.getElementById("details-name");
  const detailsPassport = document.getElementById("details-passport");
  const detailsSaveName = document.getElementById("details-save-name");
  const detailsSavePassport = document.getElementById("details-save-passport");
  const detailsRefresh = document.getElementById("details-refresh");
  const detailsWords = {
    en: { title: "Registration details", note: "Saved values are hidden. Filling these fields does not register you for an event.",
      name: "Legal full name", passport: "Passport number", saveName: "Save legal name", savePassport: "Save passport number",
      refresh: "Check saved status", present: "Saved; value hidden", missing: "Not filled in", loading: "Checking saved status…",
      saved: "Saved.", recovered: "The earlier request was saved. Current status is shown below.",
      retry: "Result unknown. Press Save again to retry the same request, or check its saved status.",
      reenter: "The earlier request has no receipt yet. Re-enter its original value to retry that same request.",
      conflict: "Details changed elsewhere. The earlier request has no receipt and can no longer apply at its old version. Check the current status before saving again.",
      unavailable: "Could not check saved status. Try again.", disabled: "Registration details are unavailable. The earlier request has not been discarded.",
      storage: "This form needs browser session storage to recover a request after reload. Nothing was submitted.",
      invalid: "Check the value. The earlier request is still tracked; check saved status before retrying.",
      blocked: "The earlier request cannot be recovered from this page. Nothing new will be submitted." },
    ru: { title: "Данные для регистрации", note: "Сохранённые значения скрыты. Заполнение этих полей не регистрирует тебя на эвент.",
      name: "ФИО по документу", passport: "Номер паспорта", saveName: "Сохранить ФИО", savePassport: "Сохранить номер паспорта",
      refresh: "Проверить сохранение", present: "Сохранено; значение скрыто", missing: "Не заполнено", loading: "Проверяю сохранение…",
      saved: "Сохранено.", recovered: "Предыдущий запрос сохранён. Ниже показано текущее состояние.",
      retry: "Результат неизвестен. Нажми «Сохранить» ещё раз для повтора того же запроса или проверь сохранение.",
      reenter: "Квитанции предыдущего запроса пока нет. Введи его исходное значение заново для повтора того же запроса.",
      conflict: "Данные изменены в другом месте. У предыдущего запроса нет квитанции, и он уже не может примениться со старой версией. Проверь текущее состояние перед новым сохранением.",
      unavailable: "Не удалось проверить сохранение. Попробуй ещё раз.", disabled: "Данные для регистрации недоступны. Предыдущий запрос не сброшен.",
      storage: "Форме нужно хранилище сессии браузера, чтобы восстановить запрос после перезагрузки. Ничего не отправлено.",
      invalid: "Проверь значение. Предыдущий запрос ещё отслеживается; проверь сохранение перед повтором.",
      blocked: "Не удалось восстановить предыдущий запрос на этой странице. Новый запрос не будет отправлен." }
  };
  let detailsOwner;
  let detailsCurrent;
  let detailsPhase = "unavailable";
  // The private body lives only in memory. Session storage holds recovery IDs.
  let detailsBody;
  let accountAfter;
  let accountNextCursor;
  const knowledgeUI = window.createZinusyaKnowledge?.({
 suggestionID: launchTarget === "knowledge" ? (window.location?.hash || "").split(/[?&#]/).find(part => part.startsWith("suggestion="))?.slice("suggestion=".length) : undefined,
    request: (path, method, body) => request(path, method, body, session.token),
    setBusy, denied: detailsDenied
  });
  const passPaymentsUI = window.createZinusyaPassPayments?.({
    request: (path, method, body) => request(path, method, body, session.token),
    setBusy, denied: detailsDenied, readProof
  });
  const orderProofUI = window.createZinusyaOrderProof?.({
    request: (path, method, body) => request(path, method, body, session.token),
    setBusy, denied: detailsDenied, readProof
  });
  const pairsUI = window.createZinusyaPairs?.({
    request: (path, method, body) => request(path, method, body, session.token), setBusy, denied: detailsDenied
  });
  const avatarsUI = window.createZinusyaAvatars?.({
    request: (path, method, body) => request(path, method, body, session.token),
    readBlob: readProof, upload: uploadAvatar, setBusy, denied: detailsDenied
  });
  const cropUI = launchTarget === "avatar-crop" ? window.createZinusyaAvatarCrop?.({
    draftID: (window.location?.hash || "").split(/[?&#]/).find(part => part.startsWith("draft="))?.slice(6),
    request: (path, method, body) => request(path, method, body, session.token),
    readBlob: readProof, setBusy, denied: detailsDenied
  }) : undefined;
  const mealsUI = window.createZinusyaMeals?.({
    continuation: launchTarget === "meals" ? {
      event_id: (window.location?.hash || "").split(/[?&#]/).find(part => part.startsWith("event="))?.slice(6),
      operation_id: (window.location?.hash || "").split(/[?&#]/).find(part => part.startsWith("operation="))?.slice(10)
    } : undefined,
    openProof: orderProofUI ? (order, locale) => orderProofUI.open(order, locale) : undefined,
    request: (path, method, body) => request(path, method, body, session.token), setBusy, denied: detailsDenied
  });
  const ordersUI = window.createZinusyaOrders?.({
    continuation: launchTarget === "orders" ? {
      event_id: (window.location?.hash || "").split(/[?&#]/).find(part => part.startsWith("event="))?.slice(6),
      operation_id: (window.location?.hash || "").split(/[?&#]/).find(part => part.startsWith("operation="))?.slice(10)
    } : undefined,
    openProof: orderProofUI ? (order, locale) => orderProofUI.open(order, locale) : undefined,
    request: (path, method, body) => request(path, method, body, session.token),
    setBusy,
    denied: detailsDenied
  });
  const paymentReviewsUI = window.createZinusyaPaymentReviews?.({
    request: (path, method, body) => request(path, method, body, session.token),
    setBusy,
    denied: detailsDenied,
    readProof
  });
  const orderReviewsUI = window.createZinusyaOrderReviews?.({
    request: (path, method, body) => request(path, method, body, session.token),
    setBusy, denied: detailsDenied, readProof
  });
  const massageUI = window.createZinusyaMassage?.({
    request: (path, method, body) => request(path, method, body, session.token),
    setBusy, denied: detailsDenied
  });
  const massageStaffUI = window.createZinusyaMassageStaff?.({
    request: (path, method, body) => request(path, method, body, session.token),
    prepareOwnTime: (context, args) => massageUI?.prepareInstant(context, args),
    openOwnBookings: context => massageUI?.openOwnEvent(context),
    setBusy, denied: detailsDenied
  });
  const massageAdminUI = window.createZinusyaMassageAdmin?.({
    request: (path, method, body) => request(path, method, body, session.token),
    setBusy, denied: detailsDenied
  });
  const pairAssignmentsUI = window.createZinusyaPairAssignments?.({
    request: (path, method, body) => request(path, method, body, session.token),
    setBusy, denied: detailsDenied
  });
  const assignmentsUI = window.createZinusyaAssignments?.({
    request: (path, method, body) => request(path, method, body, session.token),
    setBusy, denied: detailsDenied
  });
  const avatarAdminUI = window.createZinusyaAvatarAdmin?.({
    request: (path, method, body) => request(path, method, body, session.token),
    setBusy, denied: detailsDenied
  });
  async function readProof(path) {
    const response = await fetch(path, { headers: { Authorization: "ZinusyaSession " + session.token }, credentials: "omit", cache: "no-store" });
    if (!response.ok) {
      if (response.status === 401) { setBusy(false); detailsDenied(); }
      throw { status: response.status };
    }
    return response.blob();
  }
  async function uploadAvatar(file, operationID) {
    const response = await fetch("./avatars/drafts/upload", { method: "POST", body: file,
      headers: { Authorization: "ZinusyaSession " + session.token, "Content-Type": "application/octet-stream", "X-Zinusya-Operation-Id": operationID },
      credentials: "omit", cache: "no-store" });
    if (!response.ok) throw { status: response.status };
    return response.json();
  }
  try { session = JSON.parse(sessionStorage.getItem(storageKey) || "{}"); } catch { session = {}; }
  if (!session || typeof session !== "object" || Array.isArray(session)) session = {};
  if (typeof session.token !== "string" || !/^[A-Za-z0-9_-]{43}$/.test(session.token)) delete session.token;
  if (typeof session.nonce !== "string" || !/^[A-Za-z0-9_-]{43}$/.test(session.nonce)) delete session.nonce;

  if (session.detailsChange) {
    const c = session.detailsChange;
    session.detailsChange = validDetailsChange(c)
      ? { owner: c.owner, operation_id: c.operation_id, expected_version: c.expected_version, field: c.field }
      : { invalid: true };
  }

  function save() {
    try { sessionStorage.setItem(storageKey, JSON.stringify(session)); return true; } catch { return false; }
  }

  function setBusy(value) {
    // One module finishing startup must not unlock the remaining initialization.
    value = value || connecting;
    busy = value;
    for (const button of [retry, accountRetry, accountFirst, accountNext]) button.disabled = value;
    const pending = session.profileChange;
    profileFirstName.disabled = profileLastName.disabled = profileLanguage.disabled = value || !!pending;
    profileSaveName.disabled = value || profileVersion === undefined || (!!pending && pending.kind !== "name");
    profileSaveLanguage.disabled = value || profileVersion === undefined || (!!pending && pending.kind !== "language");
    profileRefresh.disabled = value;
    detailsControls(value);
    passPaymentsUI?.setLocked(value);
    knowledgeUI?.setBusy(value);
    orderProofUI?.setBusy(value);
    ordersUI?.setBusy(value);
    mealsUI?.setBusy(value);
    pairsUI?.setBusy(value);
    avatarsUI?.setBusy(value);
    cropUI?.setBusy(value);
    paymentReviewsUI?.setBusy(value);
    orderReviewsUI?.setBusy(value);
    massageUI?.setBusy(value);
    massageStaffUI?.setBusy(value);
    massageAdminUI?.setBusy(value);
    avatarAdminUI?.setBusy(value);
    assignmentsUI?.setBusy(value);
    pairAssignmentsUI?.setBusy(value);
  }

  function clearAccount() {
    knowledgeUI?.clear();
    passPaymentsUI?.clear();
    orderProofUI?.clear();
    ordersUI?.clear();
    mealsUI?.clear();
    pairsUI?.clear();
    avatarsUI?.clear();
    cropUI?.clear();
    paymentReviewsUI?.clear();
    orderReviewsUI?.clear();
    massageUI?.clear();
    massageStaffUI?.clear();
    massageAdminUI?.clear();
    avatarAdminUI?.clear();
    assignmentsUI?.clear();
    pairAssignmentsUI?.clear();
    clearDetails();
    profileSection.hidden = true;
    profileFirstName.value = profileLastName.value = profileLanguage.value = "";
    profileVersion = undefined;
    accountEvents.replaceChildren();
    accountSection.hidden = true;
    accountAfter = accountNextCursor = undefined;
  }

  function show(key, canRetry = false) {
    document.documentElement.lang = language;
    status.textContent = words[language][key];
    retry.textContent = words[language].retry;
    retry.hidden = !canRetry;
  }

  function nonce() {
    const bytes = crypto.getRandomValues(new Uint8Array(32));
    return btoa(String.fromCharCode(...bytes)).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
  }

  async function request(path, method, body, token) {
    const headers = { "Content-Type": "application/json" };
    if (token) headers.Authorization = "ZinusyaSession " + token;
    let response;
    try {
      response = await fetch(path, { method, headers, credentials: "omit", cache: "no-store",
        body: body === undefined ? undefined : JSON.stringify(body) });
    } catch {
      throw { transient: true };
    }
    if (!response.ok) throw { status: response.status };
    return response.status === 204 ? null : response.json();
  }

  async function connect() {
    if (busy) return;
    connecting = true;
    setBusy(true);
    clearAccount();
    name.hidden = true;
    show("connecting");
    try {
      const initData = window.Telegram?.WebApp?.initData;
      if (!initData) { show("outside"); return; }
      const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(initData));
      const launch = Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, "0")).join("");
      if (session.launch !== launch) {
        session = { launch, nonce: nonce(), detailsChange: session.detailsChange };
        save();
      }
      let account;
      if (session.token) {
        try {
          account = await request("./me", "GET", undefined, session.token);
        } catch (error) {
          if (error.status !== 401) throw error;
          delete session.token;
          save();
        }
      }
      if (!account) {
        if (!session.nonce) { session.nonce = nonce(); save(); }
        account = await request("./session/bootstrap", "POST", { init_data: initData, bootstrap_nonce: session.nonce });
        if (typeof account.session_token !== "string" || !/^[A-Za-z0-9_-]{43}$/.test(account.session_token)) throw new Error("Invalid reply");
        session.token = account.session_token;
        save();
      }
      detailsOwner = account.user_id;
      if (session.detailsChange && validDetailsChange(session.detailsChange) && session.detailsChange.owner !== detailsOwner) {
        delete session.detailsChange; save();
      }
      if (account.bot_language === "en" || account.bot_language === "ru") language = account.bot_language;
      name.textContent = typeof account.full_name === "string" ? account.full_name : "";
      name.hidden = !name.textContent;
      show("connected");
      if (cropUI && session.token) {
        await cropUI.start(account.user_id, language);
        return;
      }
      await readProfile();
      await readAccount();
      if (session.token) await readDetails();
      if (session.token) await passPaymentsUI?.start(account.user_id, language);
      if (session.token) await orderProofUI?.start(account.user_id, language);
      if (session.token) await ordersUI?.start(account.user_id, language);
      if (session.token) await mealsUI?.start(account.user_id, language);
      if (session.token) await pairsUI?.start(account.user_id, language);
      if (session.token) await avatarsUI?.start(account.user_id, language);
      if (session.token) await paymentReviewsUI?.start(account.user_id, language);
      if (session.token) await orderReviewsUI?.start(account.user_id, language);
      if (session.token) await massageUI?.start(account.user_id, language);
      if (session.token) await massageStaffUI?.start(account.user_id, language);
      if (session.token) await massageAdminUI?.start(account.user_id, language);
      if (session.token) await avatarAdminUI?.start(account.user_id, language);
      if (session.token) await assignmentsUI?.start(account.user_id, language);
      if (session.token) await pairAssignmentsUI?.start(account.user_id, language);
      if (session.token) await knowledgeUI?.start(account.user_id, language);
      // Restore the launch position after all modules reveal their content.
      if (session.token && ["knowledge", "pass-payments", "pairs", "avatars", "orders", "meals", "massage", "payment-review", "pass-assignments", "pair-assignments"].includes(launchTarget)) {
        const section = document.getElementById(launchTarget);
        if (section && !section.hidden) section.scrollIntoView({ block: "start" });
      }
    } catch (error) {
      if (error.transient || error.status === 408 || error.status === 429 || error.status >= 500) {
        show("unavailable", true);
      } else if (error.status) {
        detailsDenied();
      } else {
        show("unavailable");
      }
    } finally {
      connecting = false;
      setBusy(false);
    }
  }

  function profileLabels() {
    const w = profileWords[language];
    document.getElementById("profile-title").textContent = w.title;
    document.getElementById("profile-first-name-label").textContent = w.firstName;
    document.getElementById("profile-last-name-label").textContent = w.lastName;
    document.getElementById("profile-language-label").textContent = w.language;
    profileLanguage.options[0].textContent = w.auto;
    profileSaveName.textContent = w.saveName;
    profileSaveLanguage.textContent = w.saveLanguage;
    profileRefresh.textContent = w.refresh;
  }

  async function readProfile() {
    profileLabels();
    detailsLabels();
    profileSection.hidden = false;
    profileStatus.textContent = profileWords[language].loading;
    try {
      const p = await request("./profile", "GET", undefined, session.token);
      if (!Number.isSafeInteger(p.version) || p.version < 0) throw new Error("Invalid profile");
      profileVersion = p.version;
      language = p.bot_language === "en" || p.bot_language === "ru" ? p.bot_language : defaultLanguage;
      document.documentElement.lang = language;
      knowledgeUI?.setLanguage(language);
      passPaymentsUI?.setLanguage(language);
      orderProofUI?.setLanguage(language);
      ordersUI?.setLanguage(language);
      mealsUI?.setLanguage(language);
      pairsUI?.setLanguage(language);
      avatarsUI?.setLanguage(language);
      paymentReviewsUI?.setLanguage(language);
      orderReviewsUI?.setLanguage(language);
      massageUI?.setLanguage(language);
      massageStaffUI?.setLanguage(language);
      massageAdminUI?.setLanguage(language);
      avatarAdminUI?.setLanguage(language);
      assignmentsUI?.setLanguage(language);
      pairAssignmentsUI?.setLanguage(language);
      profileLabels();
      detailsLabels();
      name.textContent = p.full_name;
      name.hidden = !name.textContent;
      show("connected");
      profileFirstName.value = p.first_name || "";
      profileLastName.value = p.last_name || "";
      profileLanguage.value = p.bot_language;
      if (session.profileChange) {
        const pending = session.profileChange;
        if ((pending.kind !== "name" && pending.kind !== "language") || !pending.body || typeof pending.body !== "object") {
          delete session.profileChange; save();
        } else {
          if (pending.kind === "name") {
            profileFirstName.value = pending.body.first_name || "";
            profileLastName.value = pending.body.last_name || "";
          }
          else profileLanguage.value = pending.body.bot_language;
        }
      }
      profileStatus.textContent = session.profileChange ? profileWords[language].retry : "";
      return true;
    } catch (error) {
      if (error.status === 401) throw error;
      profileVersion = undefined;
      profileStatus.textContent = profileWords[language].unavailable;
      return false;
    }
  }

  async function saveProfile(kind) {
    if (busy || profileVersion === undefined) return;
    if (session.profileChange && session.profileChange.kind !== kind) return;
    if (!session.profileChange) {
      const body = { operation_id: crypto.randomUUID(), expected_version: profileVersion };
      if (kind === "name") {
        body.first_name = profileFirstName.value;
        body.last_name = profileLastName.value;
      }
      else body.bot_language = profileLanguage.value;
      session.profileChange = { kind, body };
      save();
    }
    setBusy(true);
    try {
      const pending = session.profileChange;
      await request("./profile/" + pending.kind, "POST", pending.body, session.token);
      delete session.profileChange; save();
      if (await readProfile()) profileStatus.textContent = profileWords[language].saved;
    } catch (error) {
      if (error.status === 400 || error.status === 409) {
        delete session.profileChange; save();
        if (error.status === 409) profileVersion = undefined;
        profileStatus.textContent = profileWords[language][error.status === 409 ? "conflict" : "invalid"];
      } else if (error.status === 401) {
        detailsDenied();
      } else profileStatus.textContent = profileWords[language].retry;
    } finally { setBusy(false); }
  }

  document.getElementById("profile-form").addEventListener("submit", event => { event.preventDefault(); void saveProfile("name"); });
  profileSaveLanguage.addEventListener("click", () => { void saveProfile("language"); });
  profileRefresh.addEventListener("click", async () => {
    if (busy) return;
    setBusy(true);
    try { await readProfile(); } catch { detailsDenied(); }
    finally { setBusy(false); }
  });

  function validDetailsChange(c) {
    const id = value => typeof value === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value)
      && value !== "00000000-0000-0000-0000-000000000000";
    return c && id(c.owner) && id(c.operation_id) && Number.isSafeInteger(c.expected_version) && c.expected_version >= 0
      && (c.field === "legal-name" || c.field === "passport-number");
  }

  function detailsSnapshot(value) {
    if (!value || !Number.isSafeInteger(value.version) || value.version < 0
      || typeof value.legal_name_present !== "boolean" || typeof value.passport_number_present !== "boolean") throw new Error("Invalid details status");
    return { version: value.version, legal_name_present: value.legal_name_present, passport_number_present: value.passport_number_present };
  }

  function detailsLabels() {
    const w = detailsWords[language];
    document.getElementById("details-title").textContent = w.title;
    document.getElementById("details-note").textContent = w.note;
    document.getElementById("details-name-label").textContent = w.name;
    document.getElementById("details-passport-label").textContent = w.passport;
    detailsSaveName.textContent = w.saveName;
    detailsSavePassport.textContent = w.savePassport;
    detailsRefresh.textContent = w.refresh;
    document.getElementById("details-name-presence").textContent = detailsCurrent ? w[detailsCurrent.legal_name_present ? "present" : "missing"] : "";
    document.getElementById("details-passport-presence").textContent = detailsCurrent ? w[detailsCurrent.passport_number_present ? "present" : "missing"] : "";
    detailsStatus.textContent = w[detailsPhase] || "";
  }

  function detailsControls(value) {
    const pending = session.detailsChange;
    const ready = detailsCurrent && (detailsPhase === "ready" || detailsPhase === "saved" || detailsPhase === "recovered" || detailsPhase === "conflict" || detailsPhase === "storage");
    const retryable = validDetailsChange(pending) && detailsPhase !== "disabled" && detailsPhase !== "blocked"
      && (detailsBody || detailsPhase === "reenter");
    for (const [field, input, button] of [["legal-name", detailsName, detailsSaveName], ["passport-number", detailsPassport, detailsSavePassport]]) {
      const enabled = !value && !!session.token && (pending ? retryable && pending.field === field : ready);
      input.disabled = !enabled || !!detailsBody;
      button.disabled = !enabled;
    }
    detailsRefresh.disabled = value;
  }

  function clearDetails() {
    detailsName.value = detailsPassport.value = "";
    detailsBody = detailsCurrent = undefined;
    detailsPhase = "unavailable";
    detailsSection.hidden = true;
  }

  function clearDetailsInput(field) {
    if (field === "legal-name") detailsName.value = "";
    if (field === "passport-number") detailsPassport.value = "";
  }

  function forgetDetailsChange() {
    clearDetailsInput(session.detailsChange?.field);
    delete session.detailsChange;
    detailsBody = undefined;
    save();
  }

  function detailsDenied() {
    delete session.token;
    delete session.profileChange;
    save();
    clearAccount();
    name.textContent = "";
    name.hidden = true;
    show("reopen");
  }

  async function readDetails() {
    detailsPhase = "loading";
    detailsLabels();
    try {
      detailsCurrent = detailsSnapshot(await request("./registration-details", "GET", undefined, session.token));
      detailsSection.hidden = false;
      const pending = session.detailsChange;
      if (!pending) detailsPhase = "ready";
      else if (!validDetailsChange(pending) || pending.owner !== detailsOwner) detailsPhase = "blocked";
      else {
        let recovered = false;
        try {
          detailsSnapshot(await request("./registration-details/operations/" + pending.operation_id, "GET", undefined, session.token));
          recovered = true;
        } catch (error) { if (error.status !== 404) throw error; }
        if (recovered) {
          forgetDetailsChange();
          detailsCurrent = detailsSnapshot(await request("./registration-details", "GET", undefined, session.token));
          detailsPhase = "recovered";
        } else if (detailsCurrent.version > pending.expected_version) {
          // Missing receipt after a newer monotonic version means the old CAS cannot apply.
          forgetDetailsChange();
          detailsPhase = "conflict";
        } else detailsPhase = detailsCurrent.version === pending.expected_version ? (detailsBody ? "retry" : "reenter") : "blocked";
      }
    } catch (error) {
      if (error.status === 401) { detailsDenied(); return; }
      detailsCurrent = undefined;
      detailsPhase = error.status === 404 ? "disabled" : "unavailable";
      detailsSection.hidden = error.status === 404 && !session.detailsChange;
    }
    detailsLabels();
  }

  async function saveDetails(field) {
    if (busy || !session.token) return;
    detailsControls(false);
    const button = field === "legal-name" ? detailsSaveName : detailsSavePassport;
    if (button.disabled) return;
    const input = field === "legal-name" ? detailsName : detailsPassport;
    if (!session.detailsChange) {
      const pending = { owner: detailsOwner, operation_id: crypto.randomUUID(), expected_version: detailsCurrent.version, field };
      if (!validDetailsChange(pending)) { detailsPhase = "blocked"; detailsLabels(); detailsControls(false); return; }
      session.detailsChange = pending;
      if (!save()) {
        delete session.detailsChange;
        detailsPhase = "storage"; detailsLabels(); detailsControls(false); return;
      }
    }
    const pending = session.detailsChange;
    if (!detailsBody) detailsBody = { operation_id: pending.operation_id, expected_version: pending.expected_version, value: input.value };
    setBusy(true);
    try {
      detailsSnapshot(await request("./registration-details/" + pending.field, "POST", detailsBody, session.token));
      forgetDetailsChange();
      await readDetails();
      if (detailsPhase === "ready") detailsPhase = "saved";
    } catch (error) {
      if (error.status === 401) { detailsDenied(); return; }
      if (error.status === 400 || error.status === 409) {
        detailsBody = undefined;
        clearDetailsInput(pending.field);
        await readDetails();
        if (detailsPhase === "reenter" && error.status === 400) detailsPhase = "invalid";
      } else detailsPhase = "retry";
    } finally { detailsLabels(); setBusy(false); }
  }

  document.getElementById("details-name-form").addEventListener("submit", event => { event.preventDefault(); return saveDetails("legal-name"); });
  document.getElementById("details-passport-form").addEventListener("submit", event => { event.preventDefault(); return saveDetails("passport-number"); });
  detailsRefresh.addEventListener("click", async () => {
    if (busy || !session.token) return;
    setBusy(true);
    try { await readDetails(); } finally { setBusy(false); }
  });

  function textElement(tag, text) {
    const element = document.createElement(tag);
    element.textContent = text;
    return element;
  }

  function money(amount, scale, currency) {
    if (!Number.isSafeInteger(amount) || amount < 0 || !Number.isInteger(scale) || scale < 0 || scale > 4 || !/^[A-Z]{3}$/.test(currency)) {
      throw new Error("Invalid amount");
    }
    const digits = String(amount).padStart(scale + 1, "0");
    return (scale ? digits.slice(0, -scale) + (language === "ru" ? "," : ".") + digits.slice(-scale) : digits) + " " + currency;
  }

  function renderEvent(item) {
    const w = accountWords[language];
    const article = document.createElement("article");
    article.append(textElement("h2", item.event_name));
    const date = instant => new Intl.DateTimeFormat(language, { timeZone: item.timezone,
      year: "numeric", month: "short", day: "numeric", hour: "numeric", minute: "2-digit", second: "2-digit",
      timeZoneName: "shortOffset" }).format(new Date(instant));
    article.append(textElement("p", date(item.starts_at) + " — " + date(item.ends_at) + " · " + item.timezone));
    article.append(textElement("p", (w[item.registration.role] || item.registration.role) + " · " + (w[item.registration.state] || item.registration.state)));
    if (!item.registration_enabled) {
      article.append(textElement("p", w.disabled));
    } else if (item.pass) {
      const pass = item.pass;
      const balance = pass.balance;
      if (passPaymentsUI && balance?.pass_id) {
        const payments = textElement("button", language === "ru" ? "Подтверждения оплаты пасса" : "Pass payment evidence");
        payments.type = "button";
        payments.addEventListener("click", () => passPaymentsUI.show(item.event_id, balance.pass_id));
        article.append(payments);
      }
      const format = amount => money(amount, balance.currency_scale, balance.currency);
      const details = document.createElement("dl");
      for (const [label, value] of [[w.price, format(balance.price_minor)], [w.confirmed, format(balance.confirmed_minor)],
        [w.remaining, format(balance.remaining_minor)], [w.deposit, format(balance.deposit_required_minor)]]) {
        details.append(textElement("dt", label), textElement("dd", value));
      }
      article.append(details, textElement("p", balance.fully_paid ? w.paid : balance.deposit_satisfied ? w.depositPaid : w.unpaid));
      if (pass.pending_evidence > 0) article.append(textElement("p", w.pending + ": " + pass.pending_evidence));
      if (!balance.fully_paid) {
        article.append(textElement("p", w.due + ": " + date(pass.due_at) + " · " + item.timezone));
        if (pass.final_due_at !== pass.due_at) article.append(textElement("p", w.final + ": " + date(pass.final_due_at) + " · " + item.timezone));
        if (pass.overdue) article.append(textElement("p", w.overdue));
      }
    }
    return article;
  }

  async function readAccount(after) {
    accountAfter = after;
    const w = accountWords[language];
    accountTitle.textContent = w.title;
    accountRetry.textContent = w.refresh;
    accountFirst.textContent = w.first;
    accountNext.textContent = w.next;
    accountSection.hidden = false;
    accountEvents.replaceChildren();
    accountFirst.hidden = !after;
    accountNext.hidden = true;
    accountStatus.textContent = w.loading;
    try {
      const page = await request("./account" + (after ? "?after=" + encodeURIComponent(after) : ""), "GET", undefined, session.token);
      if (!Array.isArray(page.events) || page.events.length > 20) throw new Error("Invalid account page");
      const rendered = page.events.map(renderEvent);
      accountEvents.replaceChildren(...rendered);
      accountStatus.textContent = page.events.length ? w.updated + ": " + new Date(page.observed_at).toISOString() : w.empty;
      accountNextCursor = page.has_more ? page.next : undefined;
      accountNext.hidden = !accountNextCursor;
    } catch (error) {
      if (error.status === 401) {
        detailsDenied();
      } else {
        accountStatus.textContent = w.unavailable;
      }
    }
  }

  async function accountPage(after) {
    if (busy || !session.token) return;
    setBusy(true);
    try { await readAccount(after); } finally { setBusy(false); }
  }

  retry.addEventListener("click", connect);
  accountRetry.addEventListener("click", () => accountPage(accountAfter));
  accountFirst.addEventListener("click", () => accountPage());
  accountNext.addEventListener("click", () => accountPage(accountNextCursor));
  window.Telegram?.WebApp?.ready();
  connect();
})();

