package alert

import (
	"fmt"
	"html"
)

// renderTrackingPage returns the plain HTML page served at GET /track/{token}
// (see Handler.trackingPage). This package has no other use for html/template
// and no static assets today, so -- like internal/verification, which also
// has no templating -- a single Go string is simplest: one file, no new
// on-disk assets, nothing for go:embed to buy over just inlining the string.
//
// The page itself never talks to the database: it polls
// GET /api/v1/public/alerts/{token} (Handler.publicView) every 5 seconds and
// renders whatever comes back. token is only ever interpolated into a data-*
// attribute via html.EscapeString, never written into script/HTML content
// directly, so nothing here trusts what a caller put in the URL path.
func renderTrackingPage(token string) string {
	return fmt.Sprintf(trackingPageTemplate, html.EscapeString(token))
}

// %s is filled in exactly once, with the escaped share token, and read back
// by the page's own JS from the data-token attribute on <body>.
const trackingPageTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no">
<title>SheShield Live Tracking</title>
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css"
  integrity="sha256-p4NxAoJBhIIN+hmNHrzRCf9tD/miZyoHS5obTRR9BMY=" crossorigin="">
<style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  html, body { height: 100%%; margin: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    background: #111827;
    color: #f9fafb;
    display: flex;
    flex-direction: column;
  }
  header {
    padding: 14px 16px;
    background: #b91c1c;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
  }
  header.resolved { background: #15803d; }
  #title { font-size: 18px; font-weight: 700; margin: 0; }
  #subtitle { font-size: 13px; opacity: 0.9; margin: 2px 0 0; }
  #statusPill {
    font-size: 13px;
    font-weight: 700;
    padding: 4px 10px;
    border-radius: 999px;
    background: rgba(255,255,255,0.2);
    white-space: nowrap;
  }
  #map { flex: 1 1 auto; min-height: 240px; background: #1f2937; }
  #controls {
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    background: #111827;
  }
  button {
    font-size: 18px;
    font-weight: 700;
    padding: 18px 16px;
    border-radius: 14px;
    border: none;
    cursor: pointer;
    width: 100%%;
    -webkit-tap-highlight-color: transparent;
  }
  #alarmBtn {
    background: #dc2626;
    color: white;
    animation: pulse 1.4s infinite;
  }
  #alarmBtn.playing { background: #7f1d1d; animation: none; }
  #alarmBtn:disabled { background: #374151; color: #9ca3af; animation: none; cursor: default; }
  #stopBtn { background: #374151; color: #f9fafb; display: none; }
  #stopBtn.show { display: block; }
  @keyframes pulse {
    0%%   { box-shadow: 0 0 0 0 rgba(220,38,38,0.6); }
    70%%  { box-shadow: 0 0 0 16px rgba(220,38,38,0); }
    100%% { box-shadow: 0 0 0 0 rgba(220,38,38,0); }
  }
  #meta { font-size: 13px; color: #9ca3af; text-align: center; }
  #errorBanner {
    display: none;
    padding: 10px 16px;
    background: #7c2d12;
    color: #fed7aa;
    font-size: 14px;
    text-align: center;
  }
  #errorBanner.show { display: block; }
</style>
</head>
<body data-token="%s">
  <div id="errorBanner">Couldn't reach the server. Retrying…</div>
  <header id="headerBar">
    <div>
      <p id="title">Tracking someone's SOS</p>
      <p id="subtitle">Loading…</p>
    </div>
    <span id="statusPill">Loading</span>
  </header>
  <div id="map"></div>
  <div id="controls">
    <button id="alarmBtn">🔔 Tap to activate alarm</button>
    <button id="stopBtn">Stop alarm</button>
    <p id="meta">Waiting for the first location update…</p>
  </div>

<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"
  integrity="sha256-20nQCchB9co0qIjJZRGuk2/Z9VM+kNiyxNV1lvTlZBo=" crossorigin=""></script>
<script>
(function () {
  "use strict";
  var token = document.body.getAttribute("data-token");
  var pollURL = "/api/v1/public/alerts/" + encodeURIComponent(token);
  var POLL_MS = 5000;

  var titleEl = document.getElementById("title");
  var subtitleEl = document.getElementById("subtitle");
  var statusPill = document.getElementById("statusPill");
  var headerBar = document.getElementById("headerBar");
  var metaEl = document.getElementById("meta");
  var errorBanner = document.getElementById("errorBanner");
  var alarmBtn = document.getElementById("alarmBtn");
  var stopBtn = document.getElementById("stopBtn");

  // ---- Map -------------------------------------------------------------
  var map = L.map("map", { zoomControl: true, attributionControl: true });
  map.setView([23.8103, 90.4125], 12); // Dhaka, replaced by the first real fix
  L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
    maxZoom: 19,
    attribution: "&copy; OpenStreetMap contributors"
  }).addTo(map);

  var marker = null;
  var haveFirstFix = false;

  function updateMarker(lat, lng) {
    var pos = [lat, lng];
    if (!marker) {
      marker = L.marker(pos).addTo(map);
    } else {
      marker.setLatLng(pos);
    }
    if (!haveFirstFix) {
      map.setView(pos, 16);
      haveFirstFix = true;
    } else {
      map.panTo(pos);
    }
  }

  // ---- Siren (Web Audio API, no external audio file) --------------------
  var audioCtx = null;
  var oscillator = null;
  var gainNode = null;
  var sirenTimer = null;
  var playing = false;

  function ensureAudioCtx() {
    if (!audioCtx) {
      var Ctx = window.AudioContext || window.webkitAudioContext;
      audioCtx = new Ctx();
    }
    return audioCtx;
  }

  function startAlarm() {
    if (playing) return;
    try {
      var ctx = ensureAudioCtx();
      if (ctx.state === "suspended") ctx.resume();

      oscillator = ctx.createOscillator();
      gainNode = ctx.createGain();
      oscillator.type = "square";
      gainNode.gain.value = 0.35;
      oscillator.connect(gainNode);
      gainNode.connect(ctx.destination);
      oscillator.frequency.setValueAtTime(800, ctx.currentTime);
      oscillator.start();

      var high = false;
      sirenTimer = setInterval(function () {
        if (!oscillator) return;
        high = !high;
        oscillator.frequency.setValueAtTime(high ? 1000 : 800, ctx.currentTime);
      }, 350);

      playing = true;
      alarmBtn.textContent = "🔔 Alarm playing…";
      alarmBtn.classList.add("playing");
      stopBtn.classList.add("show");
    } catch (e) {
      // Autoplay blocked or Web Audio unsupported -- the button stays
      // available so a tap can still start it.
    }
  }

  function stopAlarm() {
    if (sirenTimer) { clearInterval(sirenTimer); sirenTimer = null; }
    if (oscillator) {
      try { oscillator.stop(); } catch (e) {}
      oscillator.disconnect();
      oscillator = null;
    }
    if (gainNode) { gainNode.disconnect(); gainNode = null; }
    playing = false;
    alarmBtn.textContent = "🔔 Tap to activate alarm";
    alarmBtn.classList.remove("playing");
    stopBtn.classList.remove("show");
  }

  alarmBtn.addEventListener("click", startAlarm);
  stopBtn.addEventListener("click", stopAlarm);

  // Browsers block sound autoplay before any user gesture on the page --
  // this attempt is expected to be silently rejected most of the time, and
  // that's fine, the big button above is the real entry point.
  startAlarm();
  if (audioCtx === null) {
    try { ensureAudioCtx(); } catch (e) {}
  }

  // ---- Polling -----------------------------------------------------------
  var resolved = false;

  function timeAgo(iso) {
    var then = new Date(iso).getTime();
    if (isNaN(then)) return "just now";
    var secs = Math.max(0, Math.round((Date.now() - then) / 1000));
    if (secs < 5) return "just now";
    if (secs < 60) return secs + "s ago";
    var mins = Math.round(secs / 60);
    if (mins < 60) return mins + "m ago";
    var hours = Math.round(mins / 60);
    return hours + "h ago";
  }

  function render(view) {
    var name = view.firstName || "Someone";
    titleEl.textContent = name + "'s live location";

    if (view.status === "resolved") {
      resolved = true;
      headerBar.classList.add("resolved");
      statusPill.textContent = "Marked safe";
      subtitleEl.textContent = name + " has marked themselves safe.";
      stopAlarm();
      alarmBtn.disabled = true;
      alarmBtn.textContent = "✅ Marked safe -- alarm stopped";
    } else {
      headerBar.classList.remove("resolved");
      statusPill.textContent = "Live";
      subtitleEl.textContent = "This location updates automatically.";
    }

    if (typeof view.latitude === "number" && typeof view.longitude === "number") {
      updateMarker(view.latitude, view.longitude);
      var acc = (typeof view.accuracyMeters === "number")
        ? " (±" + Math.round(view.accuracyMeters) + "m)"
        : "";
      metaEl.textContent = "Last updated " + timeAgo(view.updatedAt) + acc;
    } else {
      metaEl.textContent = "Waiting for a location fix…";
    }
  }

  function poll() {
    fetch(pollURL, { cache: "no-store" })
      .then(function (res) {
        if (!res.ok) throw new Error("http " + res.status);
        return res.json();
      })
      .then(function (body) {
        errorBanner.classList.remove("show");
        render(body.data);
      })
      .catch(function () {
        errorBanner.classList.add("show");
      });
  }

  poll();
  setInterval(function () {
    if (!resolved) poll();
  }, POLL_MS);

  // Once resolved, one last poll already happened; nothing else to do --
  // the interval above simply stops issuing new requests.
})();
</script>
</body>
</html>
`
