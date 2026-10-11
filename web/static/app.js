import { sounds } from "./sounds.js";

// ---------------------------------------------------------------------
// Setup
// ---------------------------------------------------------------------

const $ = (id) => document.getElementById(id);

const REGION_LABELS = {
  world: "World",
  indonesia: "Indonesia",
  asia: "Asia",
  europe: "Europe",
  "north-america": "North America",
  "south-america": "South America",
  africa: "Africa",
  oceania: "Oceania",
};

const TILE_URL = "https://tile.openstreetmap.org/{z}/{x}/{y}.png";
const TILE_ATTRIBUTION = '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors';

const countryNames = (() => {
  try { return new Intl.DisplayNames(["en"], { type: "region" }); } catch { return null; }
})();

const state = {
  config: null,
  settings: { region: "world", rounds: 5, timeLimitSec: 0 },
  gameId: null,
  round: null,        // current RoundView from the API
  lastResult: null,   // last GuessResult
  guess: null,        // L.LatLng of the placed pin
  submitting: false,
  timer: null,
  viewer: null,
  maps: {},
};

// ---------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------

async function api(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
  return data;
}

function show(screenId) {
  document.querySelectorAll(".screen").forEach((s) => s.classList.toggle("active", s.id === screenId));
}

let toastTimer;
function toast(msg, ms = 5000) {
  const el = $("toast");
  el.textContent = msg;
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (el.hidden = true), ms);
}

function fmtNum(n) {
  return Math.round(n).toLocaleString("en-US");
}

function fmtDistance(km) {
  if (km < 1) return `${Math.round(km * 1000)} m`;
  if (km < 10) return `${km.toFixed(1)} km`;
  return `${fmtNum(km)} km`;
}

function placeName(r) {
  const country = r.country && countryNames ? countryNames.of(r.country) : r.country;
  if (r.near && country) return `Near ${r.near}, ${country}`;
  return country || "";
}

function countUp(el, to, ms = 700) {
  const start = performance.now();
  const step = () => {
    const t = Math.min(1, Math.max(0, (performance.now() - start) / ms));
    const eased = 1 - Math.pow(1 - t, 3);
    el.textContent = fmtNum(to * eased);
    if (t < 1) requestAnimationFrame(step);
  };
  requestAnimationFrame(step);
}

function pinIcon(kind, label) {
  const labelHtml = label ? `<span class="pin-label">${label}</span>` : "";
  return L.divIcon({
    className: "pin-drop",
    html: `<div class="pin ${kind}"></div>${labelHtml}`,
    iconSize: [22, 22],
    iconAnchor: [11, 26],
  });
}

function makeMap(id, opts = {}) {
  const map = L.map(id, { worldCopyJump: true, minZoom: 1, ...opts }).setView([20, 0], 1);
  L.tileLayer(TILE_URL, { maxZoom: 19, attribution: TILE_ATTRIBUTION }).addTo(map);
  // Keep tiles in sync whenever the container changes size.
  new ResizeObserver(() => map.invalidateSize()).observe(map.getContainer());
  return map;
}

// Shift `to` by ±360° so a line from `from` takes the short way around.
function nearestWrap(from, to) {
  let lng = to.lng;
  while (lng - from.lng > 180) lng -= 360;
  while (lng - from.lng < -180) lng += 360;
  return L.latLng(to.lat, lng);
}

// ---------------------------------------------------------------------
// Start screen
// ---------------------------------------------------------------------

async function loadConfig() {
  try {
    state.config = await api("GET", "/api/config");
  } catch (e) {
    showWarning(`Can't reach the game server: ${e.message}`);
    $("btn-start").disabled = true;
    return;
  }
  renderRegions();

  if (!state.config.mapillaryToken) {
    showWarning(
      "No Mapillary token found. Create a free one at " +
      '<a href="https://www.mapillary.com/dashboard/developers" target="_blank" rel="noopener">mapillary.com/dashboard/developers</a>, ' +
      "put it in <code>.env</code> as <code>MAPILLARY_TOKEN=...</code>, then restart the server."
    );
    $("btn-start").disabled = true;
  }
}

function showWarning(html) {
  const el = $("setup-warning");
  el.innerHTML = html;
  el.hidden = false;
}

function renderRegions() {
  const wrap = $("region-chips");
  wrap.innerHTML = "";
  for (const r of state.config.regions) {
    const btn = document.createElement("button");
    btn.className = "chip";
    btn.dataset.value = r.id;
    btn.innerHTML = REGION_LABELS[r.id] || r.id;
    if (r.poolSize > 0) btn.innerHTML += `<span class="count">${r.poolSize}</span>`;
    btn.title = r.poolSize > 0
      ? `${r.poolSize} saved locations`
      : "No saved locations yet: they'll be found live (a little slower)";
    btn.disabled = r.poolSize === 0 && !r.liveSearch;
    if (r.id === state.settings.region) btn.classList.add("selected");
    wrap.appendChild(btn);
  }
}

function wireChips(groupId, key, parse = (v) => v) {
  $(groupId).addEventListener("click", (e) => {
    const chip = e.target.closest(".chip");
    if (!chip || chip.disabled) return;
    sounds.click();
    $(groupId).querySelectorAll(".chip").forEach((c) => c.classList.toggle("selected", c === chip));
    state.settings[key] = parse(chip.dataset.value);
  });
}

// ---------------------------------------------------------------------
// Game flow
// ---------------------------------------------------------------------

async function startGame() {
  sounds.unlock();
  sounds.click();
  show("screen-loading");
  $("loading-text").textContent = "Scouting locations…";
  const slow = setTimeout(() => {
    $("loading-text").textContent = "Searching Mapillary for good spots. First games in a new region take a few seconds…";
  }, 2500);

  try {
    const g = await api("POST", "/api/games", state.settings);
    state.gameId = g.id;
    await loadRound(0);
  } catch (e) {
    show("screen-start");
    toast(`Couldn't start a game: ${e.message}`, 9000);
  } finally {
    clearTimeout(slow);
  }
}

async function loadRound(n) {
  const round = await api("GET", `/api/games/${state.gameId}/rounds/${n}`);
  state.round = round;
  state.guess = null;
  state.submitting = false;

  $("hud-round").textContent = `${round.index + 1} / ${round.total}`;
  $("hud-score").textContent = fmtNum(round.score);
  $("viewer-error").hidden = true;

  resetGuessMap();
  show("screen-round");
  await showImage(round.imageId);
  startTimer(round.timeLimitSec);
}

function ensureViewer() {
  if (state.viewer) return state.viewer;
  state.viewer = new mapillary.Viewer({
    accessToken: state.config.mapillaryToken,
    container: "viewer",
    component: { cover: false },
  });
  window.addEventListener("resize", () => state.viewer.resize());
  return state.viewer;
}

async function showImage(imageId) {
  const viewer = ensureViewer();
  try {
    await viewer.moveTo(imageId);
  } catch (e) {
    // Images get deleted or hidden on Mapillary from time to time.
    if (state.round && state.round.imageId === imageId) {
      $("viewer-error").hidden = false;
    }
  }
}

function resetGuessMap() {
  if (!state.maps.guess) {
    const map = makeMap("guess-map");
    map.on("click", (e) => placePin(e.latlng));
    state.maps.guess = map;
  }
  const map = state.maps.guess;
  if (state.maps.guessMarker) {
    state.maps.guessMarker.remove();
    state.maps.guessMarker = null;
  }
  map.setView([20, 0], 1);
  const btn = $("btn-guess");
  btn.disabled = true;
  btn.textContent = "Place your pin on the map";
}

function placePin(latlng) {
  if (state.submitting) return;
  state.guess = latlng;
  if (state.maps.guessMarker) {
    state.maps.guessMarker.setLatLng(latlng);
  } else {
    state.maps.guessMarker = L.marker(latlng, { icon: pinIcon("guess") }).addTo(state.maps.guess);
  }
  sounds.pin();
  const btn = $("btn-guess");
  btn.disabled = false;
  btn.textContent = "Guess";
}

async function submitGuess({ timedOut = false } = {}) {
  if (state.submitting || !state.round) return;
  if (!state.guess && !timedOut) return;
  state.submitting = true;
  stopTimer();
  $("btn-guess").disabled = true;

  // If the timer ran out but a pin is down, it still counts.
  const body = state.guess ? { lat: state.guess.lat, lng: state.guess.lng } : {};
  try {
    const res = await api("POST", `/api/games/${state.gameId}/rounds/${state.round.index}/guess`, body);
    state.lastResult = res;
    if (res.guess) sounds.whoosh();
    showResult(res);
  } catch (e) {
    state.submitting = false;
    $("btn-guess").disabled = !state.guess;
    toast(`Guess failed: ${e.message}`);
  }
}

function showResult(res) {
  show("screen-result");

  if (!state.maps.result) {
    state.maps.result = makeMap("result-map", { zoomControl: false });
    L.control.zoom({ position: "topright" }).addTo(state.maps.result);
    state.maps.resultLayer = L.layerGroup().addTo(state.maps.result);
  }
  const map = state.maps.result;
  const layer = state.maps.resultLayer;
  layer.clearLayers();
  map.invalidateSize();

  const pad = { paddingTopLeft: [50, 50], paddingBottomRight: [50, 300] };
  if (res.guess) {
    const g = L.latLng(res.guess.lat, res.guess.lng);
    const a = nearestWrap(g, L.latLng(res.actual.lat, res.actual.lng));
    L.polyline([g, a], { color: "#101a1f", weight: 3, dashArray: "6 8", opacity: 0.8 }).addTo(layer);
    L.marker(g, { icon: pinIcon("guess") }).addTo(layer);
    L.marker(a, { icon: pinIcon("truth") }).addTo(layer);
    map.fitBounds(L.latLngBounds([g, a]), { ...pad, maxZoom: 16 });
  } else {
    const a = L.latLng(res.actual.lat, res.actual.lng);
    L.marker(a, { icon: pinIcon("truth") }).addTo(layer);
    map.setView(a, 5);
  }

  $("result-eyebrow").textContent = `Round ${res.index + 1} of ${state.round.total}`;
  $("result-detail").innerHTML = res.timedOut && !res.guess
    ? "Time's up. No pin, no points."
    : `Your guess was <strong>${fmtDistance(res.distanceKm)}</strong> away.`;
  $("result-place").textContent = placeName(res);
  $("btn-next").textContent = res.gameOver ? "See final score" : "Next round";

  $("result-points").textContent = "0";
  $("result-bar").style.width = "0";
  // Let the whoosh land before the score reveal.
  setTimeout(() => {
    countUp($("result-points"), res.score);
    $("result-bar").style.width = `${(res.score / 5000) * 100}%`;
    if (res.score >= 5000) sounds.perfect();
    else if (res.guess) sounds.score(res.score);
  }, res.guess ? 450 : 50);
}

async function nextRound() {
  if (!state.lastResult) return;
  sounds.click();
  const res = state.lastResult;
  state.lastResult = null;
  try {
    if (res.gameOver) await showSummary();
    else await loadRound(res.index + 1);
  } catch (e) {
    state.lastResult = res;
    toast(e.message);
  }
}

async function showSummary() {
  const sum = await api("GET", `/api/games/${state.gameId}/summary`);
  show("screen-summary");

  if (!state.maps.summary) {
    state.maps.summary = makeMap("summary-map", { zoomControl: false });
    L.control.zoom({ position: "topright" }).addTo(state.maps.summary);
    state.maps.summaryLayer = L.layerGroup().addTo(state.maps.summary);
  }
  const map = state.maps.summary;
  const layer = state.maps.summaryLayer;
  layer.clearLayers();
  map.invalidateSize();

  const all = [];
  const list = $("summary-rounds");
  list.innerHTML = "";

  sum.rounds.forEach((r, i) => {
    const pts = [];
    let a = L.latLng(r.actual.lat, r.actual.lng);
    if (r.guess) {
      const g = L.latLng(r.guess.lat, r.guess.lng);
      a = nearestWrap(g, a);
      L.polyline([g, a], { color: "#101a1f", weight: 2, dashArray: "5 7", opacity: 0.7 }).addTo(layer);
      L.marker(g, { icon: pinIcon("guess") }).addTo(layer);
      pts.push(g);
    }
    L.marker(a, { icon: pinIcon("truth", i + 1) }).addTo(layer);
    pts.push(a);
    all.push(...pts);

    const li = document.createElement("li");
    li.innerHTML = `
      <span class="n">${i + 1}</span>
      <span class="where"></span>
      <span class="pts">${fmtNum(r.score)}</span>`;
    li.querySelector(".where").textContent =
      (placeName(r) || "Unknown place") + (r.guess ? ` · ${fmtDistance(r.distanceKm)}` : " · timed out");
    li.addEventListener("click", () => {
      sounds.click();
      map.flyToBounds(L.latLngBounds(pts), { paddingTopLeft: [50, 50], paddingBottomRight: [50, 420], maxZoom: 14 });
    });
    list.appendChild(li);
  });

  if (all.length) {
    map.fitBounds(L.latLngBounds(all), { paddingTopLeft: [50, 50], paddingBottomRight: [50, 420], maxZoom: 12 });
  }

  const ratio = sum.totalScore / sum.maxScore;
  $("summary-max").textContent = ` / ${fmtNum(sum.maxScore)}`;
  $("summary-verdict").textContent = verdict(ratio);
  countUp($("summary-total"), sum.totalScore, 1200);
  sounds.fanfare(ratio);
}

function verdict(ratio) {
  if (ratio >= 0.9) return "Are you a satellite? Ridiculous.";
  if (ratio >= 0.75) return "Seriously well travelled.";
  if (ratio >= 0.5) return "Solid. You know your way around.";
  if (ratio >= 0.25) return "Some good reads, some wild guesses.";
  return "The world is big. Go again.";
}

// ---------------------------------------------------------------------
// Timer
// ---------------------------------------------------------------------

function startTimer(seconds) {
  stopTimer();
  const cell = $("hud-timer-cell");
  cell.hidden = !seconds;
  cell.classList.remove("low");
  if (!seconds) return;

  const deadline = performance.now() + seconds * 1000;
  let lastShown = -1;
  const render = () => {
    const left = Math.max(0, Math.ceil((deadline - performance.now()) / 1000));
    if (left !== lastShown) {
      lastShown = left;
      $("hud-timer").textContent = `${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")}`;
      cell.classList.toggle("low", left <= 10);
      if (left <= 10 && left > 0) sounds.tick(left <= 5);
    }
    if (left === 0) {
      stopTimer();
      sounds.timeUp();
      submitGuess({ timedOut: true });
    }
  };
  render();
  state.timer = setInterval(render, 200);
}

function stopTimer() {
  if (state.timer) clearInterval(state.timer);
  state.timer = null;
}

// ---------------------------------------------------------------------
// Controls
// ---------------------------------------------------------------------

function renderMute() {
  $("btn-mute").innerHTML = sounds.muted
    ? '<svg viewBox="0 0 24 24" width="20" height="20"><path d="M4 9v6h4l5 5V4L8 9H4zm12.6 3 2.7-2.7-1.4-1.4-2.7 2.7-2.7-2.7-1.4 1.4 2.7 2.7-2.7 2.7 1.4 1.4 2.7-2.7 2.7 2.7 1.4-1.4z" fill="currentColor"/></svg>'
    : '<svg viewBox="0 0 24 24" width="20" height="20"><path d="M4 9v6h4l5 5V4L8 9H4zm12 3a4 4 0 0 0-2-3.5v7A4 4 0 0 0 16 12zm-2-8v2a6 6 0 0 1 0 12v2a8 8 0 0 0 0-16z" fill="currentColor"/></svg>';
  $("btn-mute").setAttribute("aria-pressed", String(sounds.muted));
}

function toggleMute() {
  sounds.unlock();
  sounds.toggleMute();
  renderMute();
  sounds.click();
}

function returnToStart() {
  if (!state.round) return;
  sounds.click();
  $("viewer-error").hidden = true;
  showImage(state.round.imageId);
}

function activeScreen() {
  return document.querySelector(".screen.active")?.id;
}

document.addEventListener("keydown", (e) => {
  if (e.target.closest("input, textarea")) return;
  const screen = activeScreen();
  const key = e.key.toLowerCase();

  if (key === "m") return toggleMute();
  if (screen === "screen-round") {
    if (key === " " || key === "enter") {
      if (state.guess) {
        e.preventDefault();
        submitGuess();
      }
    } else if (key === "r") {
      returnToStart();
    }
  } else if (screen === "screen-result" && (key === " " || key === "enter")) {
    e.preventDefault();
    nextRound();
  }
});

wireChips("region-chips", "region");
wireChips("rounds-chips", "rounds", Number);
wireChips("timer-chips", "timeLimitSec", Number);

$("btn-start").addEventListener("click", startGame);
$("btn-guess").addEventListener("click", () => submitGuess());
$("btn-next").addEventListener("click", nextRound);
$("btn-again").addEventListener("click", startGame);
$("btn-menu").addEventListener("click", async () => {
  sounds.click();
  show("screen-start");
  await loadConfig();
});
$("btn-mute").addEventListener("click", toggleMute);
$("btn-return").addEventListener("click", returnToStart);
$("btn-skip").addEventListener("click", () => {
  $("viewer-error").hidden = true;
  state.guess = null; // a skipped round scores zero even if a pin is down
  submitGuess({ timedOut: true });
});
$("btn-pin-map").addEventListener("click", () => {
  const panel = $("guess-panel");
  const on = panel.classList.toggle("expanded");
  $("btn-pin-map").setAttribute("aria-pressed", String(on));
  sounds.click();
});

renderMute();
loadConfig();
