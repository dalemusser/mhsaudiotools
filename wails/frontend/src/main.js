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
  outputDir: "",
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
  state.voicesPath = info.path;
  $("voices-path").textContent = info.path;
  $("voices-path").title = info.path;
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
  clearError();
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
    defaultSpeaker: $("default-speaker").value.trim(),
  };
}

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

init();
