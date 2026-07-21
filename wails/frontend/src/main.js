// UI logic for the MHS Audio Generator.
//
// Wails exposes the bound Go methods as window.go.main.App.* and its event bus as
// window.runtime. There is no HTTP server and no framework here on purpose: the
// engine does the work, this file only collects input and renders results.

const $ = (id) => document.getElementById(id);
const app = () => window.go.main.App;

const state = {
  sourcePath: "",
  voicesPath: "",
  voicesInfo: null, // last loaded/saved voice config, seeds the editor
  outputDir: "",
  cleanupPath: "", // "" = built-in MHS defaults
  maxParallel: 4,
  running: false,
};

// --- helpers ----------------------------------------------------------------

const commas = (n) => (n ?? 0).toLocaleString("en-US");

function showError(err) {
  const el = $("error");
  el.textContent = String(err?.message || err);
  el.classList.remove("hidden");
}
const clearError = () => $("error").classList.add("hidden");

function setStatus(text) {
  $("status").textContent = text || "";
}

// A stat tile. Muted unless the number is the one that matters.
function stat(label, value, tone = "") {
  const toneClass =
    tone === "good" ? "text-emerald-400" : tone === "warn" ? "text-amber-400" : "text-slate-100";
  return `<div class="rounded-md border border-slate-700 bg-slate-800/60 p-3">
      <div class="text-lg font-semibold ${toneClass}">${value}</div>
      <div class="mt-0.5 text-[11px] uppercase tracking-wide text-slate-500">${label}</div>
    </div>`;
}

function list(el, items, empty = "none") {
  el.innerHTML = items.length
    ? items.map((t) => `<li>${t}</li>`).join("")
    : `<li class="text-slate-600">${empty}</li>`;
}

const esc = (s) =>
  String(s).replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c])
  );

// --- startup ----------------------------------------------------------------

async function init() {
  try {
    // Cleanup profile needs no key — show the built-in defaults up front.
    try {
      renderCleanup(await app().DefaultCleanup());
    } catch (e) {
      /* non-fatal */
    }

    const s = await app().GetSettings();
    if (!s.hasKey) {
      $("key-card").classList.remove("hidden");
      $("account").textContent = "no API key";
      return;
    }
    await loadAccount();
  } catch (e) {
    showError(e);
  }
}

// loadAccount drives the parallel slider: its max is whatever the account allows.
async function loadAccount() {
  try {
    const a = await app().GetAccount();
    state.maxParallel = a.maxConcurrency;

    const slider = $("parallel");
    slider.max = String(a.maxConcurrency);
    slider.value = String(a.maxConcurrency); // default to the account's max — speed is the point
    updateParallelLabel();

    const used = Math.round((a.characterCount / a.characterLimit) * 100);
    $("account").textContent =
      `${a.tier} · up to ${a.maxConcurrency} parallel · ${commas(a.characterCount)}/${commas(a.characterLimit)} chars (${used}%)`;
    if (!a.tierKnown) {
      $("account").textContent += " · tier unrecognized";
    }
  } catch (e) {
    $("account").textContent = "account unavailable";
  }
}

function updateParallelLabel() {
  const v = Number($("parallel").value);
  $("parallel-val").textContent = v === state.maxParallel ? `${v} (max)` : String(v);
}

// --- pickers ----------------------------------------------------------------

$("pick-source").onclick = async () => {
  try {
    const p = await app().PickSourceFile();
    if (!p) return;
    state.sourcePath = p;
    $("source-path").textContent = p;
    $("source-path").title = p;
    clearError();
  } catch (e) {
    showError(e);
  }
};

$("pick-voices").onclick = async () => {
  try {
    const p = await app().PickVoicesFile();
    if (!p) return;
    await loadVoices(p);
  } catch (e) {
    showError(e);
  }
};

// loadVoices shows the cast and, crucially, the slot->voice bindings, so a
// miscast character is obvious before an hour of generating.
async function loadVoices(path) {
  let info;
  if (path.toLowerCase().endsWith(".csv")) {
    // Importing writes/merges a voices.json next to the CSV, which is what
    // preserves slot bindings across future imports.
    info = await app().ImportVoicesCSV(path, "");
  } else {
    info = await app().LoadVoices(path);
  }
  renderVoices(info);
  clearError();
}

// renderVoices updates the read-only summary and remembers the config so the
// editor can seed itself.
function renderVoices(info) {
  state.voicesPath = info.path || "";
  state.voicesInfo = info;
  $("voices-path").textContent = info.path || "No file chosen";
  $("voices-path").title = info.path || "";
  $("voices-summary").classList.remove("hidden");

  list(
    $("voices-chars"),
    (info.assignments || []).map((a) => `${esc(a.character)} <span class="text-slate-600">→</span> ${esc(a.voiceName)}`)
  );
  list(
    $("voices-slots"),
    (info.playerSlots || []).map((s) => `Player${s.index} <span class="text-slate-600">→</span> ${esc(s.voiceName)}`)
  );

  const probs = info.problems || [];
  const box = $("voices-problems");
  box.classList.toggle("hidden", probs.length === 0);
  box.innerHTML = probs.map((p) => `<div>${esc(p)}</div>`).join("");
}

$("pick-out").onclick = async () => {
  try {
    const p = await app().PickOutputDir();
    if (!p) return;
    state.outputDir = p;
    $("out-path").textContent = p;
    $("out-path").title = p;
    clearError();
  } catch (e) {
    showError(e);
  }
};

$("parallel").oninput = updateParallelLabel;

// --- key --------------------------------------------------------------------

$("key-save").onclick = async () => {
  const key = $("key-input").value.trim();
  if (!key) return;
  try {
    const path = await app().SaveKey(key);
    $("key-card").classList.add("hidden");
    setStatus(`key saved to ${path}`);
    await loadAccount();
  } catch (e) {
    showError(e);
  }
};

// --- request ----------------------------------------------------------------

function request() {
  return {
    sourcePath: state.sourcePath,
    sourceFormat: $("source-format").value,
    voicesPath: state.voicesPath,
    outputDir: state.outputDir,
    layout: $("layout").value,
    format: $("format").value,
    timestamps: $("timestamps").checked,
    concurrency: Number($("parallel").value),
    force: $("force").checked,
    cleanup: $("cleanup").checked,
    cleanupPath: state.cleanupPath,
    defaultSpeaker: $("default-speaker").value.trim(),
  };
}

// --- cleanup profile --------------------------------------------------------

function renderCleanup(info) {
  if (!info) return;
  state.cleanupPath = info.path || ""; // "" for the built-in defaults
  $("cleanup-name").textContent = info.builtIn ? "built-in MHS defaults" : info.path;
  $("cleanup-name").title = info.path || "";
  $("cleanup-count").textContent = `${info.rules} rules (${info.removeCount} remove, ${info.replaceCount} replace)`;

  const probs = info.problems || [];
  const box = $("cleanup-problems");
  box.classList.toggle("hidden", probs.length === 0);
  box.innerHTML = probs.map((p) => `<div>${esc(p)}</div>`).join("");
}

$("pick-cleanup").onclick = async () => {
  try {
    const info = await app().PickCleanupFile();
    if (info) renderCleanup(info);
  } catch (e) {
    showError(e);
  }
};

$("use-default-cleanup").onclick = async () => {
  try {
    renderCleanup(await app().DefaultCleanup());
  } catch (e) {
    showError(e);
  }
};

$("save-default-cleanup").onclick = async () => {
  try {
    const info = await app().ExportDefaultCleanup(); // writes the file, then selects it
    if (info) {
      renderCleanup(info);
      setStatus(`saved cleanup.json to ${info.path}`);
    }
  } catch (e) {
    showError(e);
  }
};

// --- preview ----------------------------------------------------------------

$("preview").onclick = async () => {
  clearError();
  setStatus("planning…");
  try {
    const p = await app().Preview(request());
    renderPreview(p);
    setStatus("");
  } catch (e) {
    setStatus("");
    showError(e);
  }
};

function renderPreview(p) {
  $("preview-card").classList.remove("hidden");
  $("result-card").classList.add("hidden");

  $("preview-stats").innerHTML = [
    stat("lines", commas(p.lines)),
    stat("files to generate", commas(p.toGenerate), "good"),
    stat("already up to date", commas(p.upToDate)),
    stat("characters", commas(p.characters)),
  ].join("");

  list(
    $("preview-voices"),
    (p.perVoice || []).map(
      (v) => `<span class="inline-block w-40">${esc(v.voice)}</span><span class="text-slate-500">${commas(v.count)}</span>`
    )
  );
  list(
    $("preview-samples"),
    (p.samples || []).map(
      (s) =>
        `<div class="truncate"><span class="text-slate-300">${esc(s.relPath)}</span>
         <span class="text-slate-600">[${esc(s.voice)}]</span></div>
         <div class="truncate text-slate-500">${esc(s.text)}</div>`
    )
  );

  const probs = p.problems || [];
  const box = $("preview-problems");
  box.classList.toggle("hidden", probs.length === 0);
  box.innerHTML = probs.map((x) => `<div>${esc(x)}</div>`).join("");
}

// --- generate ---------------------------------------------------------------

window.runtime.EventsOn("progress", (p) => {
  const pct = p.total ? Math.round((p.done / p.total) * 100) : 0;
  $("bar").style.width = pct + "%";
  $("progress-text").textContent =
    `${commas(p.done)} / ${commas(p.total)} files (${pct}%)` + (p.failed ? ` · ${commas(p.failed)} failed` : "");
});

$("generate").onclick = async () => {
  clearError();
  setRunning(true);
  $("progress-card").classList.remove("hidden");
  $("result-card").classList.add("hidden");
  $("bar").style.width = "0%";
  $("progress-text").textContent = "starting…";

  const started = Date.now();
  try {
    const r = await app().Generate(request());
    renderResult(r, Date.now() - started);
  } catch (e) {
    showError(e);
  } finally {
    setRunning(false);
    $("progress-card").classList.add("hidden");
  }
};

$("cancel").onclick = async () => {
  setStatus("stopping…");
  await app().Cancel();
};

function setRunning(on) {
  state.running = on;
  $("generate").disabled = on;
  $("preview").disabled = on;
  $("cancel").classList.toggle("hidden", !on);
  setStatus(on ? "generating…" : "");
}

function renderResult(r, ms) {
  $("result-card").classList.remove("hidden");
  $("result-stats").innerHTML = [
    stat("files written", commas(r.written), "good"),
    stat("already up to date", commas(r.upToDate)),
    stat("lines skipped", commas(r.skippedLines)),
    stat("failed", commas(r.failed), r.failed ? "warn" : ""),
  ].join("");

  const secs = Math.max(1, Math.round(ms / 1000));
  $("result-note").textContent = r.canceled
    ? "stopped — run again to pick up where it left off"
    : `finished in ${secs}s`;

  const probs = r.problems || [];
  const box = $("result-problems");
  box.classList.toggle("hidden", probs.length === 0);
  box.innerHTML = probs.map((x) => `<div>${esc(x)}</div>`).join("");
}

$("reveal").onclick = async () => {
  try {
    await app().RevealOutput(state.outputDir);
  } catch (e) {
    showError(e);
  }
};

// --- voice editor -----------------------------------------------------------
//
// DOM-driven: rows are the source of truth, scraped on save. A scrape→render
// cycle rebuilds rows whenever the option list changes (add/remove/fetch), so
// selections and typed names survive.

const ve = {
  voiceList: [], // [{id, name}] fetched from the account
  voiceNames: {}, // id -> name, seeded from the config + fetched voices
};

// veVoiceOptions builds <option>s, always including the row's current voice even
// if it isn't in the fetched list (so nothing is silently lost).
function veVoiceOptions(selectedId) {
  let html = `<option value="">— choose voice —</option>`;
  const seen = new Set();
  for (const v of ve.voiceList) {
    seen.add(v.id);
    html += `<option value="${esc(v.id)}"${v.id === selectedId ? " selected" : ""}>${esc(v.name)}</option>`;
  }
  if (selectedId && !seen.has(selectedId)) {
    const nm = ve.voiceNames[selectedId] || selectedId;
    html += `<option value="${esc(selectedId)}" selected>${esc(nm)} (current)</option>`;
  }
  return html;
}

function veCharRow(character, voiceId) {
  return `<div class="ve-row flex items-center gap-2">
      <input class="ve-name input-sm flex-1" placeholder="Character" value="${esc(character || "")}"/>
      <select class="ve-voice input-sm flex-1">${veVoiceOptions(voiceId || "")}</select>
      <button class="ve-del btn" title="Remove">✕</button>
    </div>`;
}

function veSlotRow(n, voiceId) {
  return `<div class="ve-row flex items-center gap-2">
      <span class="ve-slot-label w-16 text-xs text-slate-400">Player${n}</span>
      <select class="ve-voice input-sm flex-1">${veVoiceOptions(voiceId || "")}</select>
      <button class="ve-del btn" title="Remove">✕</button>
    </div>`;
}

// scrape reads the current editor state from the DOM.
function veScrape() {
  const chars = [...$("ve-chars").querySelectorAll(".ve-row")].map((row) => ({
    character: row.querySelector(".ve-name").value.trim(),
    voiceId: row.querySelector(".ve-voice").value,
  }));
  const slots = [...$("ve-slots").querySelectorAll(".ve-row")].map((row) => ({
    voiceId: row.querySelector(".ve-voice").value,
  }));
  return { chars, slots };
}

function veRender(data) {
  $("ve-chars").innerHTML = data.chars.map((c) => veCharRow(c.character, c.voiceId)).join("");
  $("ve-slots").innerHTML = data.slots.map((s, i) => veSlotRow(i + 1, s.voiceId)).join("");
}

function veRenumberSlots() {
  [...$("ve-slots").querySelectorAll(".ve-slot-label")].forEach((el, i) => {
    el.textContent = "Player" + (i + 1);
  });
}

function openVoiceEditor() {
  const info = state.voicesInfo || { assignments: [], playerSlots: [] };
  ve.voiceNames = {};
  const chars = (info.assignments || []).map((a) => {
    if (a.voiceId) ve.voiceNames[a.voiceId] = a.voiceName;
    return { character: a.character, voiceId: a.voiceId };
  });
  const slots = (info.playerSlots || []).map((s) => {
    if (s.voiceId) ve.voiceNames[s.voiceId] = s.voiceName;
    return { voiceId: s.voiceId };
  });
  veRender({ chars, slots });
  veError("");
  $("ve-status").textContent = "";
  $("voice-modal").classList.remove("hidden");
}

function closeVoiceEditor() {
  $("voice-modal").classList.add("hidden");
}

function veError(msg) {
  const el = $("ve-error");
  el.textContent = msg || "";
  el.classList.toggle("hidden", !msg);
}

$("edit-voices").onclick = openVoiceEditor;
$("ve-cancel").onclick = closeVoiceEditor;

$("ve-add-char").onclick = () => {
  const d = veScrape();
  d.chars.push({ character: "", voiceId: "" });
  veRender(d);
};
$("ve-add-slot").onclick = () => {
  const d = veScrape();
  d.slots.push({ voiceId: "" });
  veRender(d);
};

// Delegated remove: rebuild from the scrape minus the clicked row (keeps slot
// numbering correct).
function veDelHandler(container, isSlots) {
  container.addEventListener("click", (e) => {
    const btn = e.target.closest(".ve-del");
    if (!btn) return;
    const rows = [...container.querySelectorAll(".ve-row")];
    const idx = rows.indexOf(btn.closest(".ve-row"));
    const d = veScrape();
    (isSlots ? d.slots : d.chars).splice(idx, 1);
    veRender(d);
  });
}
veDelHandler($("ve-chars"), false);
veDelHandler($("ve-slots"), true);

$("ve-fetch").onclick = async () => {
  $("ve-status").textContent = "fetching…";
  try {
    const voices = await app().FetchVoices();
    ve.voiceList = (voices || []).map((v) => ({ id: v.ID ?? v.id, name: v.Name ?? v.name }));
    for (const v of ve.voiceList) ve.voiceNames[v.id] = v.name;
    veRender(veScrape()); // rebuild dropdowns, preserving current selections
    $("ve-status").textContent = `${ve.voiceList.length} voices loaded`;
  } catch (e) {
    $("ve-status").textContent = "";
    veError(String(e?.message || e));
  }
};

$("ve-save").onclick = async () => {
  const d = veScrape();
  const assignments = d.chars
    .filter((c) => c.character && c.voiceId)
    .map((c) => ({ character: c.character, voiceId: c.voiceId, voiceName: ve.voiceNames[c.voiceId] || c.voiceId }));
  // Player slots keep DOM order; numbering is positional (Player1, Player2, …).
  const slots = d.slots
    .filter((s) => s.voiceId)
    .map((s, i) => ({ index: i + 1, voiceId: s.voiceId, voiceName: ve.voiceNames[s.voiceId] || s.voiceId }));

  const dropped = d.chars.length - assignments.length + (d.slots.length - slots.length);
  try {
    const info = await app().SaveVoices(state.voicesPath, assignments, slots);
    if (!info) return; // save dialog canceled
    renderVoices(info);
    closeVoiceEditor();
    setStatus(dropped ? `voices saved (${dropped} incomplete row(s) skipped)` : "voices saved");
  } catch (e) {
    veError(String(e?.message || e));
  }
};

init();
