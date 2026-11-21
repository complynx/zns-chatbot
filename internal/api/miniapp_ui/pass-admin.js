"use strict";

window.createZinusyaPassAdmin = env => {
  const host = document.getElementById("pass-admin");
  let eventID = "", language = "en", serial = 0, busy = false, locked = false;
  let page, selected, preview;
  const t = key => window.passCopy(language, "ops." + key);
  const node = (tag, text) => {
    const element = document.createElement(tag);
    if (text !== undefined) element.textContent = text;
    return element;
  };
  const endpoint = action => "./pass-admin/events/" + encodeURIComponent(eventID) + "/" + action;
  function control(parent, label, action) {
    const button = node("button", label);
    button.type = "button";
    button.addEventListener("click", action);
    parent.append(button);
    return button;
  }
  function registrationLabel(row) {
    const telegram = "Telegram " + row.telegram_id;
    const identity = row.display_name ? row.display_name + " · " + telegram : telegram;
    return identity + " · " + t(row.role);
  }
  function display(minor, row) {
    const scale = row.currency_scale;
    const digits = String(minor).padStart(scale + 1, "0");
    return (scale ? digits.slice(0,-scale) + "." + digits.slice(-scale) : digits);
  }
  function minorValue(value, scale) {
    const parts = /^(\d+)(?:[.,](\d+))?$/.exec(value.trim());
    if (!parts || (parts[2] || "").length > scale) return NaN;
    return Number(parts[1] + (parts[2] || "").padEnd(scale, "0"));
  }
  async function action(callback, managePage = true) {
    if (busy) return;
    const id = serial;
    busy = true; host.inert = true;
    if (managePage) env.setBusy(true);
    try { await callback(id); }
    catch (err) {
      if (serial === id) {
        if (err.status === 401 || err.status === 403) { reset(""); env.denied?.(); }
        else host.querySelector("[role=status]").textContent = t("error");
      }
    } finally {
      if (serial === id) { busy = false; host.inert = locked; }
      if (managePage) env.setBusy(false);
    }
  }
  async function load(id) {
    const response = await env.request(endpoint("registrations"), "GET");
    if (id !== serial) return;
    if (response.event_id !== eventID || !Array.isArray(response.rows)) throw new Error();
    page = response;
    selected = page.rows.find(r => r.registration_id === selected?.registration_id);
    preview = undefined; render();
  }
  function reset(event) {
    serial++; busy = false; host.inert = locked; page = selected = preview = undefined; eventID = event;
    render();
  }
  function download(route, filename) {
    return action(async id => {
      const blob = await env.readBlob(endpoint(route));
      if (serial !== id) return;
      const link = node("a");
      const url = URL.createObjectURL(blob);
      link.href = url; link.download = filename;
      try {
        host.append(link); link.click();
      } finally {
        link.remove(); setTimeout(() => URL.revokeObjectURL(url), 1000);
      }
    });
  }
  function render() {
    host.replaceChildren(); host.hidden = !eventID;
    if (!eventID) return;
    host.append(node("h2", t("title")));
    const status = node("p"); status.setAttribute("role", "status"); host.append(status);
    control(host, t("reload"), () => action(load));
    control(host, t("export"), () => download("export.xlsx", "passes-" + eventID + ".xlsx"));
    if (!page) return;
    if (page.identity_export_available === true) {
      control(host, t("exportIdentity"), () => download("identity.zip", "identity-" + eventID + ".zip"));
    }
    if (!page.rows.length) {status.textContent = t("noRows"); return;}
    const select = node("select");
    const placeholder = node("option",t("selectRegistration")); placeholder.value = ""; select.append(placeholder);
    for (const row of page.rows) {
      const option = node("option", registrationLabel(row));
      option.value = row.registration_id; select.append(option);
    }
    select.value = selected?.registration_id || "";
    select.addEventListener("change", () => {
      selected = page.rows.find(r => r.registration_id === select.value); preview = undefined; render();
    });
    const label = node("label",t("selectRegistration")); label.append(select); host.append(label);
    if (!selected) return;
    const r = selected;
    host.append(node("p", registrationLabel(r)), node("p", t(r.state + "State")));
    if (r.pass_id) host.append(node("p",t("confirmedAmount") + " " + display(r.confirmed_minor,r) + " " + r.currency));
    if (r.partner_registration_id) {
      const partner = page.rows.find(row => row.registration_id === r.partner_registration_id);
      host.append(node("p", t("partner") + " " + (partner?.display_name || partner?.telegram_id || r.partner_registration_id)));
    }
    const refund = r.state === "returned";
    if (r.pending_proof) {host.append(node("p",t("reviewProof"))); return;}
    if (refund) {
      if (r.pass_id) host.append(node("p",t("refundDue") + " " + display(r.refund_due_minor - r.refunded_minor,r) + " " + r.currency));
      if (r.refunded_at) {host.append(node("p",t("refundedAmount") + " " + display(r.refunded_minor,r) + " " + r.currency)); return;}
      if (!r.refund_due_minor) return;
    }
    if (preview) {
      const body = preview;
      host.append(node("p",t(refund ? "refundNote" : "returnNote")),node("p",body.reason));
      if (r.pass_id) host.append(node("p",t("refundDue") + " " + display(body.refund_minor,r) + " " + r.currency));
      control(host,t("confirm"),() => action(async id => {
        await env.request(endpoint(refund ? "refund" : "return"),"POST",body);
        if (serial !== id) return;
        await load(id);
        if (serial === id) host.querySelector("[role=status]").textContent = t(refund ? "refunded" : "returned");
      }));
      return;
    }
    const reason = node("input"); reason.maxLength = 500;
    const reasonLabel = node("label",t(refund ? "refundReason" : "returnReason"));
    reasonLabel.append(reason); host.append(reasonLabel);
    const amount = node("input");
    amount.inputMode = "decimal"; amount.value = display(refund ? r.refund_due_minor : r.confirmed_minor,r);
    if (!refund && r.confirmed_minor > 0) {
      const amountLabel = node("label",t("refundDue") + " · " + r.currency);
      amountLabel.append(amount); host.append(amountLabel);
    }
    control(host,t(refund ? "reviewRefund" : "reviewReturn"),() => {
      const minor = minorValue(amount.value,r.currency_scale);
      if (!reason.value.trim()) {status.textContent = t("reasonRequired");return;}
      if (!Number.isSafeInteger(minor) || minor < 0 || minor > r.confirmed_minor) {status.textContent=t("error");return;}
      preview = {operation_id:crypto.randomUUID(),registration_id:r.registration_id,expected_version:r.version,
        confirmed_minor:r.confirmed_minor,refund_minor:minor,reason:reason.value.trim(),confirmed:true};
      render();
    });
  }
  return {
    select(value, lang) {language = lang; reset(value); if (value) return action(load, false);},
    setBusy(value) {locked = value; host.inert = busy || locked;},
    clear() {reset("");},
    language(value) {language = value; render();}
  };
};
