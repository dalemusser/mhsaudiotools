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
  emotionPath: "", // "" = built-in emotion map
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
    // Cleanup + emotion maps need no key — show the built-in defaults up front.
    try {
      renderCleanup(await app().DefaultCleanup());
      renderEmotion(await app().DefaultEmotion());
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

// renderAccount drives the header and the parallel slider: its max is whatever
// the account allows.
function renderAccount(a) {
  state.maxParallel = a.maxConcurrency;

  const slider = $("parallel");
  slider.max = String(a.maxConcurrency);
  slider.value = String(a.maxConcurrency); // default to the account's max — speed is the point
  updateParallelLabel();

  const used = a.characterLimit > 0 ? ` (${Math.round((a.characterCount / a.characterLimit) * 100)}%)` : "";
  $("account").textContent =
    `${a.tier} · up to ${a.maxConcurrency} parallel · ${commas(a.characterCount)}/${commas(a.characterLimit)} chars${used}`;
  if (!a.tierKnown) {
    $("account").textContent += " · tier unrecognized";
  }
}

async function loadAccount() {
  try {
    renderAccount(await app().GetAccount());
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
  $("key-save").disabled = true;
  try {
    const path = await app().SaveKey(key);
    // Prove the key actually works before hiding the card — a mistyped key
    // that only fails at Generate time is a miserable way to find out. The
    // failure could equally be a network blip, so say so; the key IS saved
    // and retrying costs nothing.
    let account;
    try {
      account = await app().GetAccount();
    } catch (e) {
      showError(`key saved to ${path}, but it couldn't be verified with ElevenLabs — ` +
        `a mistyped key or a network problem (${String(e?.message || e)})`);
      return;
    }
    $("key-card").classList.add("hidden");
    setStatus(`key saved to ${path}`);
    renderAccount(account);
  } catch (e) {
    showError(e);
  } finally {
    $("key-save").disabled = false;
  }
};

// The escape hatch for a revoked or mistyped stored key: GetSettings only says
// "a key exists", so without this there'd be no way back to the key card short
// of deleting ~/.elevenlabs_key by hand.
$("key-change").onclick = () => {
  $("key-card").classList.remove("hidden");
  $("key-input").value = "";
  $("key-input").focus();
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
    emotion: $("emotion").checked,
    emotionPath: state.emotionPath,
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

// --- emotion map display ----------------------------------------------------

function renderEmotion(info) {
  if (!info) return;
  state.emotionPath = info.path || "";
  $("emotion-name").textContent = info.builtIn ? "built-in defaults" : info.path;
  $("emotion-name").title = info.path || "";
  $("emotion-count").textContent = `${info.rules} tags, ${info.ignoreCount} ignored`;
}

$("pick-emotion").onclick = async () => {
  try {
    const info = await app().PickEmotionFile();
    if (info) renderEmotion(info);
  } catch (e) {
    showError(e);
  }
};

$("use-default-emotion").onclick = async () => {
  try {
    renderEmotion(await app().DefaultEmotion());
  } catch (e) {
    showError(e);
  }
};

$("save-default-emotion").onclick = async () => {
  try {
    const info = await app().ExportDefaultEmotion();
    if (info) {
      renderEmotion(info);
      setStatus(`saved emotions.json to ${info.path}`);
    }
  } catch (e) {
    showError(e);
  }
};

// --- preview ----------------------------------------------------------------

// previewSeq goes stale when another preview starts or a run begins, so a slow
// Plan can't pop its card over the progress view or wipe fresh results.
let previewSeq = 0;

$("preview").onclick = async () => {
  if (state.running) return;
  clearError();
  const seq = ++previewSeq;
  $("preview").disabled = true;
  setStatus("planning…");
  try {
    const p = await app().Preview(request());
    if (seq !== previewSeq || state.running) return; // superseded — drop it
    renderPreview(p);
    setStatus("");
  } catch (e) {
    if (seq !== previewSeq || state.running) return;
    setStatus("");
    showError(e);
  } finally {
    // Only the newest preview may re-enable: a stale one settling here while a
    // fresh one is still in flight must not reopen the door to a third.
    if (seq === previewSeq && !state.running) $("preview").disabled = false;
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
  previewSeq++; // any preview still in flight is now stale
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
  try {
    await app().Cancel();
  } catch (e) {
    showError(e);
  }
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
  voiceCategory: {}, // id -> "generated"/"professional"/… (for model suggestion)
};

const MODEL_V2 = "eleven_multilingual_v2";
const MODEL_V3 = "eleven_v3";

// veModelSelect builds the per-voice model picker (v2 default).
function veModelSelect(model) {
  const v3 = model === MODEL_V3;
  return `<select class="ve-model input-sm" style="width:58px" title="Model for this voice">
      <option value="${MODEL_V2}"${v3 ? "" : " selected"}>v2</option>
      <option value="${MODEL_V3}"${v3 ? " selected" : ""}>v3</option>
    </select>`;
}

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

function veCharRow(character, voiceId, model) {
  return `<div class="ve-row flex items-center gap-2">
      <input class="ve-name input-sm flex-1" placeholder="Character" value="${esc(character || "")}"/>
      <select class="ve-voice input-sm flex-1">${veVoiceOptions(voiceId || "")}</select>
      ${veModelSelect(model)}
      <button class="ve-play btn" title="Preview voice on its model">▶</button>
      <button class="ve-del btn" title="Remove">✕</button>
    </div>`;
}

function veSlotRow(n, voiceId, model) {
  return `<div class="ve-row flex items-center gap-2">
      <span class="ve-slot-label w-16 text-xs text-slate-400">Player${n}</span>
      <select class="ve-voice input-sm flex-1">${veVoiceOptions(voiceId || "")}</select>
      ${veModelSelect(model)}
      <button class="ve-play btn" title="Preview voice on its model">▶</button>
      <button class="ve-del btn" title="Remove">✕</button>
    </div>`;
}

// Audio preview: synthesize the sample line in a row's voice and play it. Only
// one clip plays at a time.
let vePlayer = null;

async function vePreview(row, btn) {
  const voiceId = row.querySelector(".ve-voice").value;
  if (!voiceId) {
    veError("Pick a voice on this row first.");
    return;
  }
  const model = row.querySelector(".ve-model").value;
  veError("");
  const glyph = btn.textContent;
  btn.textContent = "…";
  btn.disabled = true;
  try {
    const uri = await app().PreviewVoice(voiceId, $("ve-sample").value, model);
    if (vePlayer) vePlayer.pause();
    vePlayer = new Audio(uri);
    await vePlayer.play();
  } catch (e) {
    veError(String(e?.message || e));
  } finally {
    btn.textContent = glyph;
    btn.disabled = false;
  }
}

function vePlayHandler(container) {
  container.addEventListener("click", (e) => {
    const btn = e.target.closest(".ve-play");
    if (!btn) return;
    vePreview(btn.closest(".ve-row"), btn);
  });
}
vePlayHandler($("ve-chars"));
vePlayHandler($("ve-slots"));

// scrape reads the current editor state from the DOM.
function veScrape() {
  const chars = [...$("ve-chars").querySelectorAll(".ve-row")].map((row) => ({
    character: row.querySelector(".ve-name").value.trim(),
    voiceId: row.querySelector(".ve-voice").value,
    model: row.querySelector(".ve-model").value,
  }));
  const slots = [...$("ve-slots").querySelectorAll(".ve-row")].map((row) => ({
    voiceId: row.querySelector(".ve-voice").value,
    model: row.querySelector(".ve-model").value,
  }));
  return { chars, slots };
}

function veRender(data) {
  $("ve-chars").innerHTML = data.chars.map((c) => veCharRow(c.character, c.voiceId, c.model)).join("");
  $("ve-slots").innerHTML = data.slots.map((s, i) => veSlotRow(i + 1, s.voiceId, s.model)).join("");
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
    return { character: a.character, voiceId: a.voiceId, model: a.model };
  });
  const slots = (info.playerSlots || []).map((s) => {
    if (s.voiceId) ve.voiceNames[s.voiceId] = s.voiceName;
    return { voiceId: s.voiceId, model: s.model };
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
    ve.voiceList = (voices || []).map((v) => ({ id: v.ID ?? v.id, name: v.Name ?? v.name, category: v.Category ?? v.category }));
    for (const v of ve.voiceList) {
      ve.voiceNames[v.id] = v.name;
      ve.voiceCategory[v.id] = v.category;
    }
    veRender(veScrape()); // rebuild dropdowns, preserving current selections
    $("ve-status").textContent = `${ve.voiceList.length} voices loaded`;
  } catch (e) {
    $("ve-status").textContent = "";
    veError(String(e?.message || e));
  }
};

// Set each row's model from its voice's type: professional clones -> v2 (review
// by ear), everything else (generated/premade) -> v3.
$("ve-suggest-models").onclick = () => {
  if (Object.keys(ve.voiceCategory).length === 0) {
    veError("Fetch voices first so I can read each voice's type.");
    return;
  }
  veError("");
  const pick = (voiceId) => (ve.voiceCategory[voiceId] === "professional" ? MODEL_V2 : MODEL_V3);
  const d = veScrape();
  d.chars.forEach((c) => { if (c.voiceId && ve.voiceCategory[c.voiceId] !== undefined) c.model = pick(c.voiceId); });
  d.slots.forEach((s) => { if (s.voiceId && ve.voiceCategory[s.voiceId] !== undefined) s.model = pick(s.voiceId); });
  veRender(d);
  $("ve-status").textContent = "models set from voice type — review the v2 (pro) ones by ear";
};

$("ve-save").onclick = async () => {
  const d = veScrape();
  const assignments = d.chars
    .filter((c) => c.character && c.voiceId)
    .map((c) => ({ character: c.character, voiceId: c.voiceId, voiceName: ve.voiceNames[c.voiceId] || c.voiceId, model: c.model }));
  // Player slots keep DOM order; numbering is positional (Player1, Player2, …).
  const slots = d.slots
    .filter((s) => s.voiceId)
    .map((s, i) => ({ index: i + 1, voiceId: s.voiceId, voiceName: ve.voiceNames[s.voiceId] || s.voiceId, model: s.model }));

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

// --- cleanup-rules editor ---------------------------------------------------
//
// Same DOM-driven scrape/render pattern as the voice editor. Rules are an ordered
// list, so rows carry up/down. A test box applies the current rules live, and a
// scan button reuses the engine's Suggest() to propose remove rules.

const ce = { name: "mhs-dialogue" };

const CE_KINDS = ["literal", "regex"];
const CE_OPS = ["remove", "replace"];

function ceSelect(cls, opts, sel) {
  return `<select class="${cls} input-sm">${opts
    .map((o) => `<option value="${o}"${o === sel ? " selected" : ""}>${o}</option>`)
    .join("")}</select>`;
}

function ceRuleRow(r) {
  return `<div class="ce-row flex items-center gap-1">
      ${ceSelect("ce-kind", CE_KINDS, r.kind || "literal")}
      ${ceSelect("ce-op", CE_OPS, r.op || "remove")}
      <input class="ce-from input-sm flex-1 select-text" placeholder="from" value="${esc(r.from || "")}"/>
      <input class="ce-to input-sm flex-1 select-text" placeholder="to (replace)" value="${esc(r.to || "")}"/>
      <input class="ce-note input-sm flex-1 select-text" placeholder="note" value="${esc(r.note || "")}"/>
      <button class="ce-up btn" title="Move up">↑</button>
      <button class="ce-down btn" title="Move down">↓</button>
      <button class="ce-del btn" title="Remove">✕</button>
    </div>`;
}

function ceScrapeRules() {
  return [...$("ce-rules").querySelectorAll(".ce-row")].map((row) => ({
    kind: row.querySelector(".ce-kind").value,
    op: row.querySelector(".ce-op").value,
    from: row.querySelector(".ce-from").value,
    to: row.querySelector(".ce-to").value,
    note: row.querySelector(".ce-note").value,
  }));
}

function ceRender(rules) {
  $("ce-rules").innerHTML = rules.map(ceRuleRow).join("");
}

function ceError(msg) {
  const el = $("ce-error");
  el.textContent = msg || "";
  el.classList.toggle("hidden", !msg);
}

async function openCleanupEditor() {
  ceError("");
  ceTestSeq++; // a test call pending across close→reopen must not render
  $("ce-status").textContent = "";
  $("ce-suggestions").innerHTML = "";
  $("ce-test-out").textContent = "";
  try {
    const dto = await app().CleanupRules(state.cleanupPath);
    ce.name = dto.name || "cleanup";
    $("ce-name").textContent = state.cleanupPath ? state.cleanupPath : "(built-in MHS defaults)";
    ceRender(dto.rules || []);
    $("cleanup-modal").classList.remove("hidden");
  } catch (e) {
    showError(e);
  }
}

function closeCleanupEditor() {
  $("cleanup-modal").classList.add("hidden");
}

$("edit-cleanup").onclick = openCleanupEditor;
$("ce-cancel").onclick = closeCleanupEditor;

$("ce-add").onclick = () => {
  const rules = ceScrapeRules();
  rules.push({ kind: "literal", op: "remove", from: "", to: "", note: "" });
  ceRender(rules);
};

// Delegated row controls: up / down / delete.
$("ce-rules").addEventListener("click", (e) => {
  const btn = e.target.closest(".ce-up, .ce-down, .ce-del");
  if (!btn) return;
  const rules = ceScrapeRules();
  const rows = [...$("ce-rules").querySelectorAll(".ce-row")];
  const i = rows.indexOf(btn.closest(".ce-row"));
  if (btn.classList.contains("ce-del")) {
    rules.splice(i, 1);
  } else if (btn.classList.contains("ce-up") && i > 0) {
    [rules[i - 1], rules[i]] = [rules[i], rules[i - 1]];
  } else if (btn.classList.contains("ce-down") && i < rules.length - 1) {
    [rules[i + 1], rules[i]] = [rules[i], rules[i + 1]];
  }
  ceRender(rules);
});

// Live test: apply the current rules to the sample line. One un-sequenced call
// fires per keystroke, so only the latest result may render — an earlier slow
// call must not overwrite a later one.
let ceTestSeq = 0;
$("ce-test-in").oninput = async () => {
  const sample = $("ce-test-in").value;
  const seq = ++ceTestSeq;
  if (!sample) {
    $("ce-test-out").textContent = "";
    return;
  }
  try {
    const out = await app().TestCleanup(ceScrapeRules(), sample);
    if (seq !== ceTestSeq) return;
    $("ce-test-out").textContent = out;
    $("ce-test-out").classList.remove("text-red-300");
  } catch (e) {
    if (seq !== ceTestSeq) return;
    $("ce-test-out").textContent = String(e?.message || e);
    $("ce-test-out").classList.add("text-red-300");
  }
};

$("ce-scan").onclick = async () => {
  $("ce-status").textContent = "scanning…";
  ceError("");
  try {
    const sugs = await app().ScanForCleanup(state.sourcePath, $("source-format").value, ceScrapeRules());
    renderCleanupSuggestions(sugs || []);
    $("ce-status").textContent = "";
  } catch (e) {
    $("ce-status").textContent = "";
    ceError(String(e?.message || e));
  }
};

function renderCleanupSuggestions(sugs) {
  const box = $("ce-suggestions");
  if (!sugs.length) {
    box.innerHTML = `<div class="text-slate-500">Nothing unhandled found in the chosen source.</div>`;
    return;
  }
  box.innerHTML = sugs
    .map(
      (s, i) => `<div class="flex items-start gap-2 rounded border border-slate-700 p-2">
        <button class="ce-accept btn" data-i="${i}">Add rule</button>
        <div class="min-w-0">
          <div class="text-slate-300">${esc(s.description)} — ${commas(s.count)}×</div>
          <div class="truncate text-slate-500">${esc((s.examples || []).slice(0, 6).join("   "))}</div>
          <div class="text-slate-600">regex: <code>${esc(s.pattern)}</code></div>
        </div>
      </div>`
    )
    .join("");
  // Wire the Add buttons against the current suggestion list.
  box.querySelectorAll(".ce-accept").forEach((btn, idx) => {
    btn.onclick = () => {
      const s = sugs[idx];
      const rules = ceScrapeRules();
      rules.push({ kind: "regex", op: "remove", from: s.pattern, to: "", note: "from scan: " + s.description });
      ceRender(rules);
      btn.closest("div.flex").remove(); // consumed
    };
  });
}

$("ce-save").onclick = async () => {
  ceError("");
  try {
    const info = await app().SaveCleanup(state.cleanupPath, { name: ce.name, rules: ceScrapeRules() });
    if (!info) return; // dialog canceled
    renderCleanup(info);
    closeCleanupEditor();
    setStatus("cleanup rules saved");
  } catch (e) {
    ceError(String(e?.message || e)); // e.g. an invalid regex, reported before writing
  }
};

// --- emotion tag-map editor -------------------------------------------------
//
// Same DOM-driven pattern as the cleanup editor. Rows are direction→tag; the
// ignore list is a textarea (one phrase per line); a test box shows what a line
// becomes on v3.

const em = { name: "mhs-emotion" };

function emRuleRow(r) {
  return `<div class="em-row grid grid-cols-[1fr_1fr_auto] gap-1 items-center">
      <input class="em-phrase input-sm select-text" placeholder="sighs" value="${esc(r.phrase || "")}"/>
      <input class="em-tag input-sm select-text" placeholder="[sighs]" value="${esc(r.tag || "")}"/>
      <button class="em-del btn" title="Remove">✕</button>
    </div>`;
}

function emScrapeRules() {
  return [...$("em-rules").querySelectorAll(".em-row")].map((row) => ({
    phrase: row.querySelector(".em-phrase").value,
    tag: row.querySelector(".em-tag").value,
  }));
}

function emRender(rules) {
  $("em-rules").innerHTML = rules.map(emRuleRow).join("");
}

function emError(msg) {
  const el = $("em-error");
  el.textContent = msg || "";
  el.classList.toggle("hidden", !msg);
}

function emDTO() {
  const ignore = $("em-ignore").value.split("\n").map((s) => s.trim()).filter(Boolean);
  return { name: em.name, rules: emScrapeRules(), ignore };
}

async function openEmotionEditor() {
  emError("");
  emTestSeq++; // a test call pending across close→reopen must not render
  $("em-status").textContent = "";
  $("em-test-out").textContent = "";
  try {
    const dto = await app().EmotionRules(state.emotionPath);
    em.name = dto.name || "emotion";
    $("em-name").textContent = state.emotionPath ? state.emotionPath : "(built-in defaults)";
    emRender(dto.rules || []);
    $("em-ignore").value = (dto.ignore || []).join("\n");
    $("emotion-modal").classList.remove("hidden");
  } catch (e) {
    showError(e);
  }
}

function closeEmotionEditor() {
  $("emotion-modal").classList.add("hidden");
}

$("edit-emotion").onclick = openEmotionEditor;
$("em-cancel").onclick = closeEmotionEditor;

$("em-add").onclick = () => {
  const rules = emScrapeRules();
  rules.push({ phrase: "", tag: "" });
  emRender(rules);
};

$("em-rules").addEventListener("click", (e) => {
  const btn = e.target.closest(".em-del");
  if (!btn) return;
  const rows = [...$("em-rules").querySelectorAll(".em-row")];
  const i = rows.indexOf(btn.closest(".em-row"));
  const rules = emScrapeRules();
  rules.splice(i, 1);
  emRender(rules);
});

let emTestSeq = 0;
$("em-test-in").oninput = async () => {
  const sample = $("em-test-in").value;
  const seq = ++emTestSeq;
  if (!sample) {
    $("em-test-out").textContent = "";
    return;
  }
  try {
    const out = await app().TestEmotion(emDTO(), sample);
    if (seq !== emTestSeq) return;
    $("em-test-out").textContent = out;
  } catch (e) {
    if (seq !== emTestSeq) return;
    $("em-test-out").textContent = String(e?.message || e);
  }
};

$("em-save").onclick = async () => {
  emError("");
  // Incomplete rows are dropped and duplicate phrases collapse last-wins on
  // save (the map is keyed by phrase) — say so, like the voice editor does,
  // instead of letting edits vanish silently.
  const rows = emScrapeRules();
  const kept = rows.filter((r) => r.phrase.trim() && r.tag.trim());
  const seen = new Set();
  let dups = 0;
  for (const r of kept) {
    const k = r.phrase.trim().toLowerCase();
    if (seen.has(k)) dups++;
    seen.add(k);
  }
  const dropped = rows.length - kept.length;
  try {
    const info = await app().SaveEmotion(state.emotionPath, emDTO());
    if (!info) return;
    renderEmotion(info);
    closeEmotionEditor();
    const notes = [];
    if (dropped) notes.push(`${dropped} incomplete row(s) skipped`);
    if (dups) notes.push(`${dups} duplicate phrase(s) collapsed`);
    setStatus(notes.length ? `emotion tags saved (${notes.join(", ")})` : "emotion tags saved");
  } catch (e) {
    emError(String(e?.message || e));
  }
};

init();
