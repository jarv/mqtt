import ReconnectingWebSocket from "reconnecting-websocket";

// --- State ---
let devices = {};

// --- Tile math ---
// Returns the floating-point tile coordinate (not floored) for a lat/lon.
function latLonToTileF(lat, lon, zoom) {
  const n = Math.pow(2, zoom);
  const xf = ((lon + 180) / 360) * n;
  const latRad = (lat * Math.PI) / 180;
  const yf =
    ((1 - Math.log(Math.tan(latRad) + 1 / Math.cos(latRad)) / Math.PI) / 2) * n;
  return { xf, yf };
}

// Build a 256×256 map element centred exactly on lat/lon.
// We fetch a 3×3 grid of tiles, draw them onto a canvas, then clip out
// the 256×256 region centred on the device position.
function buildMapEl(lat, lon) {
  const zoom = 14;
  const { xf, yf } = latLonToTileF(lat, lon, zoom);

  // Centre tile
  const cx = Math.floor(xf);
  const cy = Math.floor(yf);

  // Sub-tile pixel offset of the device within the centre tile (0–255)
  const subPx = (xf - cx) * 256;
  const subPy = (yf - cy) * 256;

  // The 3×3 canvas is 768×768. The centre tile starts at (256, 256).
  // Device position on the full canvas:
  const devCanvasX = 256 + subPx;
  const devCanvasY = 256 + subPy;

  // We want to show a 256×256 window centred on (devCanvasX, devCanvasY).
  // clipX/clipY = top-left of that window on the canvas.
  const clipX = devCanvasX - 128;
  const clipY = devCanvasY - 128;

  const SIZE = 256;
  const wrapper = document.createElement("div");
  wrapper.style.cssText = `position:relative; width:${SIZE}px; height:${SIZE}px; flex-shrink:0; overflow:hidden; background:#e8e8e0;`;

  const canvas = document.createElement("canvas");
  canvas.width = SIZE;
  canvas.height = SIZE;
  canvas.style.cssText = "display:block; position:absolute; top:0; left:0;";
  wrapper.appendChild(canvas);

  // Dot always at exact centre of the 256×256 view
  const dot = document.createElement("div");
  dot.className = "tile-marker";
  dot.style.left = "128px";
  dot.style.top = "128px";
  wrapper.appendChild(dot);

  // Load all 9 tiles and draw onto canvas once all are ready
  const ctx = canvas.getContext("2d");
  const subs = ["a", "b", "c"];
  const imgs = [];

  for (let dy = -1; dy <= 1; dy++) {
    for (let dx = -1; dx <= 1; dx++) {
      const tx = cx + dx;
      const ty = cy + dy;
      const sub = subs[Math.abs(tx + ty) % 3];
      const url = `https://${sub}.basemaps.cartocdn.com/light_all/${zoom}/${tx}/${ty}.png`;
      const img = new Image();
      img.crossOrigin = "anonymous";
      // Canvas position of this tile's top-left corner
      const canvasX = (dx + 1) * 256 - clipX;
      const canvasY = (dy + 1) * 256 - clipY;
      imgs.push({ img, canvasX, canvasY });
      img.onload = () => {
        ctx.drawImage(img, canvasX, canvasY, 256, 256);
        // Re-raise dot above canvas on every draw
        wrapper.appendChild(dot);
      };
      img.onerror = () => {};
      img.src = url;
    }
  }

  // Draw any already-cached tiles immediately
  imgs.forEach(({ img, canvasX, canvasY }) => {
    if (img.complete && img.naturalWidth)
      ctx.drawImage(img, canvasX, canvasY, 256, 256);
  });

  return wrapper;
}

function buildNoLocationEl() {
  const el = document.createElement("div");
  el.style.cssText =
    "width:256px; height:256px; flex-shrink:0; display:flex; align-items:center; justify-content:center;";
  el.innerHTML = `<span style="font-size:12px; color:var(--color-site-muted)">No GPS fix</span>`;
  return el;
}

// --- Formatters ---
function formatBatteryEl(mv) {
  const wrap = document.createElement("div");
  wrap.style.cssText = "display:flex; align-items:center; gap:8px;";

  if (!mv) {
    wrap.innerHTML = `<span style="color:var(--color-site-muted)">N/A</span>`;
    return wrap;
  }

  const pct = Math.max(
    0,
    Math.min(100, Math.round(((mv - 3300) / (4200 - 3300)) * 100)),
  );
  const track = document.createElement("div");
  track.className = "bat-bar-track";
  track.style.cssText = "flex:1; min-width:60px;";
  const fill = document.createElement("div");
  fill.className = "bat-bar-fill";
  fill.style.width = pct + "%";
  track.appendChild(fill);

  const label = document.createElement("span");
  label.className = "card-value";
  label.style.cssText =
    "font-size:12px; min-width:32px; text-align:right; color:var(--color-site-text)";
  label.textContent = pct + "%";

  wrap.appendChild(track);
  wrap.appendChild(label);
  return wrap;
}

function formatCoord(val, pos, neg) {
  if (!val) return "—";
  return `${Math.abs(val).toFixed(5)}° ${val >= 0 ? pos : neg}`;
}

function formatLastSeen(ts) {
  if (!ts) return "never";
  const diffMs = Date.now() - new Date(ts).getTime();
  if (diffMs < 60000) return `${Math.round(diffMs / 1000)}s ago`;
  if (diffMs < 3600000) return `${Math.round(diffMs / 60000)}m ago`;
  return new Date(ts).toLocaleTimeString();
}

// --- Build info rows as a table-like structure ---
function buildInfoContent(device) {
  const frag = document.createDocumentFragment();

  // Header row: device ID + status
  const header = document.createElement("div");
  header.style.cssText =
    "display:flex; justify-content:space-between; align-items:center; margin-bottom:10px;";

  const title = document.createElement("div");
  title.className = "card-heading";
  title.style.cssText =
    "font-weight:700; font-size:15px; color:var(--color-site-text);";
  title.textContent = device.id;

  const status = document.createElement("div");
  status.className = device.online ? "status-online" : "status-offline";
  status.style.cssText = `font-size:12px; font-weight:600; color:${device.online ? "var(--color-site-online)" : "var(--color-site-offline)"};`;
  status.textContent = device.online ? "● Online" : "○ Offline";

  header.appendChild(title);
  header.appendChild(status);
  frag.appendChild(header);

  // Divider
  const hr = document.createElement("div");
  hr.className = "card-divider";
  hr.style.cssText =
    "border-top:1px solid var(--color-site-border); margin-bottom:10px;";
  frag.appendChild(hr);

  // Data rows
  const rows = [
    ["Lat", formatCoord(device.lat, "N", "S")],
    ["Lon", formatCoord(device.lon, "E", "W")],
    ["Alt", device.alt ? device.alt.toFixed(1) + " m" : "—"],
    ["Speed", device.speed ? device.speed.toFixed(1) + " km/h" : "0.0 km/h"],
    [
      "Sats",
      `${device.sats || 0}  /  HDOP ${device.hdop ? device.hdop.toFixed(1) : "—"}`,
    ],
    ["RSSI", device.rssi ? device.rssi.toFixed(1) + " dBm" : "—"],
    ["SNR", device.snr ? device.snr.toFixed(1) : "—"],
  ];

  rows.forEach(([label, value]) => {
    const row = document.createElement("div");
    row.style.cssText =
      "display:flex; justify-content:space-between; align-items:baseline; font-size:13px; margin-bottom:4px;";

    const lEl = document.createElement("span");
    lEl.className = "card-label";
    lEl.style.cssText = "color:var(--color-site-muted); min-width:48px;";
    lEl.textContent = label;

    const vEl = document.createElement("span");
    vEl.className = "card-value";
    vEl.style.cssText = "color:var(--color-site-text); text-align:right;";
    vEl.textContent = value;

    row.appendChild(lEl);
    row.appendChild(vEl);
    frag.appendChild(row);
  });

  // Battery row (special — has a bar)
  const batRow = document.createElement("div");
  batRow.style.cssText =
    "display:flex; justify-content:space-between; align-items:center; font-size:13px; margin-bottom:4px; gap:8px;";
  const batLabel = document.createElement("span");
  batLabel.className = "card-label";
  batLabel.style.cssText =
    "color:var(--color-site-muted); min-width:48px; flex-shrink:0;";
  batLabel.textContent = "Battery";
  batRow.appendChild(batLabel);
  batRow.appendChild(formatBatteryEl(device.battery_mv));
  frag.appendChild(batRow);

  // Last seen
  const seen = document.createElement("div");
  seen.className = "seen-text";
  seen.style.cssText =
    "margin-top:10px; font-size:11px; color:var(--color-site-muted);";
  seen.textContent = `Last seen: ${formatLastSeen(device.last_seen)}`;
  frag.appendChild(seen);

  return frag;
}

// --- Create device row ---
function createDeviceRow(device) {
  const row = document.createElement("div");
  row.dataset.deviceId = device.id;
  row.className = "card";
  row.style.cssText = `
    display: inline-flex;
    align-items: stretch;
    border: 1px solid var(--color-site-border);
    background-color: var(--color-site-surface);
    border-radius: 6px;
    overflow: hidden;
    box-shadow: 0 1px 4px rgba(0,0,0,0.06);
  `;

  // Info panel — fixed width matching the map column
  const info = document.createElement("div");
  info.className = "device-info";
  info.style.cssText =
    "width:256px; flex-shrink:0; padding:16px; box-sizing:border-box;";
  info.appendChild(buildInfoContent(device));
  row.appendChild(info);

  // Divider
  const divider = document.createElement("div");
  divider.className = "card-map-divider";
  divider.style.cssText =
    "width:1px; background-color:var(--color-site-border); flex-shrink:0;";
  row.appendChild(divider);

  // Map cell — centres the 256×256 map element both axes
  const mapCell = document.createElement("div");
  mapCell.className = "device-map";
  mapCell.style.cssText =
    "display:flex; align-items:center; justify-content:center; width:256px; flex-shrink:0;";
  const mapEl =
    device.lat && device.lon
      ? buildMapEl(device.lat, device.lon)
      : buildNoLocationEl();
  mapCell.appendChild(mapEl);
  row.appendChild(mapCell);

  document.getElementById("device-list").appendChild(row);
  return row;
}

function updateDeviceRow(row, device) {
  const info = row.querySelector(".device-info");
  if (info) {
    info.innerHTML = "";
    info.appendChild(buildInfoContent(device));
  }

  const mapCell = row.querySelector(".device-map");
  if (mapCell) {
    const newMap =
      device.lat && device.lon
        ? buildMapEl(device.lat, device.lon)
        : buildNoLocationEl();
    mapCell.innerHTML = "";
    mapCell.appendChild(newMap);
  }
}

function renderDevices() {
  const list = document.getElementById("device-list");
  const noDevices = document.getElementById("no-devices");
  const deviceList = Object.values(devices);

  if (deviceList.length === 0) {
    list.classList.add("hidden");
    noDevices.classList.remove("hidden");
    return;
  }

  list.classList.remove("hidden");
  noDevices.classList.add("hidden");

  const seen = new Set();
  deviceList.forEach((device) => {
    seen.add(device.id);
    const existing = list.querySelector(`[data-device-id="${device.id}"]`);
    if (!existing) createDeviceRow(device);
    else updateDeviceRow(existing, device);
  });

  list.querySelectorAll("[data-device-id]").forEach((el) => {
    if (!seen.has(el.dataset.deviceId)) el.remove();
  });
}

// --- WebSocket ---
function connectWebSocket() {
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  const ws = new ReconnectingWebSocket(`${proto}//${window.location.host}/ws`);
  const statusEl = document.getElementById("ws-status");

  ws.addEventListener("open", () => {
    statusEl.textContent = "● Connected";
    statusEl.style.color = "var(--color-site-online, #28a745)";
  });
  ws.addEventListener("close", () => {
    statusEl.textContent = "Reconnecting...";
    statusEl.style.color = "var(--color-site-muted)";
  });
  ws.addEventListener("error", () => {
    statusEl.textContent = "Connection error";
    statusEl.style.color = "#dc3545";
  });
  ws.addEventListener("message", (event) => {
    try {
      const msg = JSON.parse(event.data);
      if (msg.type === "devices") {
        devices = {};
        (msg.data || []).forEach((d) => {
          devices[d.id] = d;
        });
        renderDevices();
      }
    } catch (e) {
      console.error("WS parse error", e);
    }
  });
}

// --- Clock ---
function startClock() {
  const el = document.getElementById("clock");
  const tick = () => {
    el.textContent = new Date().toUTCString().replace(" GMT", " UTC");
  };
  tick();
  setInterval(tick, 1000);
}

document.addEventListener("DOMContentLoaded", () => {
  connectWebSocket();
  startClock();
});
