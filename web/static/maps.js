// Thin wrapper around MapLibre GL so the game code stays simple.
//
// MapLibre draws vector tiles with WebGL: roads and labels are redrawn
// at any zoom level, so zooming is continuous instead of swapping image
// tiles level by level like a raster (Leaflet/PNG) map does.

const STYLE_URL = "https://tiles.openfreemap.org/styles/liberty";
const WORLD = { center: [0, 20], zoom: 0.8 };
const LINE_COLOR = "#101a1f";

export function makeMap(container) {
  const map = new maplibregl.Map({
    container,
    style: STYLE_URL,
    ...WORLD,
    minZoom: 0,
    maxZoom: 18,
    // A guessing map has no use for tilting or rotating.
    dragRotate: false,
    pitchWithRotate: false,
    touchPitch: false,
    attributionControl: { compact: true },
  });
  map.touchZoomRotate.disableRotation();
  map.keyboard.disableRotation();
  map.addControl(new maplibregl.NavigationControl({ showCompass: false }), "top-right");

  // Resolves once the style is ready for sources and layers. If the tile
  // server is unreachable it never resolves; markers still work.
  map.ready = new Promise((resolve) => {
    if (map.isStyleLoaded()) resolve();
    else map.once("load", resolve);
  });

  // Keep the canvas in sync with its container (e.g. the guess panel
  // growing on hover), at most once per frame.
  let queued = false;
  new ResizeObserver(() => {
    if (queued) return;
    queued = true;
    requestAnimationFrame(() => {
      queued = false;
      map.resize();
    });
  }).observe(container);

  return map;
}

export function resetView(map) {
  map.jumpTo(WORLD);
}

// A teardrop pin. The drop animation lives on an inner element because
// MapLibre positions the marker's root element with a CSS transform.
export function makePin(map, lngLat, kind, label) {
  const el = document.createElement("div");
  el.className = "marker";
  el.innerHTML = `<div class="marker-drop"><div class="pin ${kind}"></div>${
    label != null ? `<span class="pin-label">${label}</span>` : ""
  }</div>`;
  return new maplibregl.Marker({ element: el, anchor: "bottom" }).setLngLat(lngLat).addTo(map);
}

// Draw dashed lines; each line is [[lng, lat], [lng, lat]]. Replaces
// whatever this map drew before.
export async function setLines(map, lines, width = 3) {
  await map.ready;
  const data = {
    type: "FeatureCollection",
    features: lines.map((coords) => ({
      type: "Feature",
      properties: {},
      geometry: { type: "LineString", coordinates: coords },
    })),
  };
  const src = map.getSource("guess-lines");
  if (src) {
    src.setData(data);
    return;
  }
  map.addSource("guess-lines", { type: "geojson", data });
  map.addLayer({
    id: "guess-lines",
    type: "line",
    source: "guess-lines",
    layout: { "line-cap": "round" },
    paint: {
      "line-color": LINE_COLOR,
      "line-width": width,
      "line-opacity": 0.8,
      "line-dasharray": [1.5, 2],
    },
  });
}

// Fit the view around points ([lng, lat] pairs).
export function fitPoints(map, points, { padding, maxZoom = 16, animate = false } = {}) {
  if (!points.length) return;
  if (points.length === 1) {
    map.jumpTo({ center: points[0], zoom: Math.min(maxZoom, 5) });
    return;
  }
  const bounds = new maplibregl.LngLatBounds(points[0], points[0]);
  for (const p of points.slice(1)) bounds.extend(p);
  map.fitBounds(bounds, { padding, maxZoom, animate, duration: animate ? 900 : 0 });
}

// Shift `to` by ±360° so a line from `from` takes the short way around
// the globe. MapLibre happily draws longitudes outside ±180.
export function nearestWrap(from, to) {
  let lng = to.lng;
  while (lng - from.lng > 180) lng -= 360;
  while (lng - from.lng < -180) lng += 360;
  return { lng, lat: to.lat };
}
