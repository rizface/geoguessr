import { sounds } from "./sounds.js";
import { makeMap, resetView, makePin, setLines, fitPoints, nearestWrap } from "./maps.js";

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

const countryNames = (() => {
  try { return new Intl.DisplayNames(["en"], { type: "region" }); } catch { return null; }
})();

const state = {
  config: null,
  settings: { region: "world", rounds: 5, timeLimitSec: 0 },
  gameId: null,
  round: null,        // current RoundView from the API
  lastResult: null,   // last GuessResult
  guess: null,        // {lng, lat} of the placed pin
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

// Leave room for the floating score card when framing results.
function resultPadding(cardHeight) {
  const el = document.querySelector(".screen.active");
  const h = el ? el.clientHeight : window.innerHeight;
  const bottom = Math.min(cardHeight + 30, Math.max(60, h - 160));
  return { top: 60, left: 50, right: 60, bottom };
}

function summaryPadding() {
  return resultPadding(document.querySelector(".summary-card").offsetHeight);
}

const ll = (p) => [p.lng, p.lat];

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
    const map = makeMap($("guess-map"));
    map.on("click", (e) => placePin(e.lngLat));
    state.maps.guess = map;
  }
  const map = state.maps.guess;
  if (state.maps.guessMarker) {
    state.maps.guessMarker.remove();
    state.maps.guessMarker = null;
  }
  map.resize();
  resetView(map);
  const btn = $("btn-guess");
  btn.disabled = true;
  btn.textContent = "Place your pin on the map";
}

function placePin(lngLat) {
  if (state.submitting) return;
  state.guess = { lng: lngLat.lng, lat: lngLat.lat };
  if (state.maps.guessMarker) {
    state.maps.guessMarker.setLngLat(ll(state.guess));
  } else {
    state.maps.guessMarker = makePin(state.maps.guess, ll(state.guess), "guess");
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
    state.maps.result = makeMap($("result-map"));
    state.maps.resultPins = [];
  }
  const map = state.maps.result;
  state.maps.resultPins.forEach((m) => m.remove());
  state.maps.resultPins = [];
  map.resize();

  const pad = resultPadding(document.querySelector(".result-card").offsetHeight);
  if (res.guess) {
    const g = res.guess;
    const a = nearestWrap(g, res.actual);
    setLines(map, [[ll(g), ll(a)]]);
    state.maps.resultPins.push(makePin(map, ll(g), "guess"), makePin(map, ll(a), "truth"));
    fitPoints(map, [ll(g), ll(a)], { padding: pad, maxZoom: 16 });
  } else {
    setLines(map, []);
    state.maps.resultPins.push(makePin(map, ll(res.actual), "truth"));
    fitPoints(map, [ll(res.actual)]);
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
    state.maps.summary = makeMap($("summary-map"));
    state.maps.summaryPins = [];
  }
  const map = state.maps.summary;
  state.maps.summaryPins.forEach((m) => m.remove());
  state.maps.summaryPins = [];
  map.resize();

  const all = [];
  const lines = [];
  const list = $("summary-rounds");
  list.innerHTML = "";

  sum.rounds.forEach((r, i) => {
    const pts = [];
    let a = r.actual;
    if (r.guess) {
      a = nearestWrap(r.guess, a);
      lines.push([ll(r.guess), ll(a)]);
      state.maps.summaryPins.push(makePin(map, ll(r.guess), "guess"));
      pts.push(ll(r.guess));
    }
    state.maps.summaryPins.push(makePin(map, ll(a), "truth", i + 1));
    pts.push(ll(a));
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
      fitPoints(map, pts, { padding: summaryPadding(), maxZoom: 14, animate: true });
    });
    list.appendChild(li);
  });

  setLines(map, lines, 2);
  fitPoints(map, all, { padding: summaryPadding(), maxZoom: 12 });

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
