// Drives a headless Chrome through the approver page over the DevTools
// protocol: pair with the code, wait for a prompt, open its details, approve.
// Prints one JSON line with what the page showed. No dependencies: Node's own
// WebSocket and child_process are enough.
//
// usage: node browser.mjs <chrome> <pairing url>
//
// FYLANE_SCAN=<file.mjpeg> opens the page with no code and reads it through
// Chrome's fake camera, which plays that file: the home-screen path.
import { spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const [chrome, pairURL] = process.argv.slice(2);
const scanFile = process.env.FYLANE_SCAN;
const profile = mkdtempSync(join(tmpdir(), "fylane-approver-"));
const camera = scanFile ? [
  "--use-fake-ui-for-media-stream", "--use-fake-device-for-media-stream",
  `--use-file-for-fake-video-capture=${resolve(scanFile)}`,
] : [];
const proc = spawn(chrome, [
  "--headless=new", "--remote-debugging-port=0", `--user-data-dir=${profile}`,
  "--no-first-run", "--no-default-browser-check", "--disable-gpu", ...camera, "about:blank",
]);
const wsURL = await new Promise((resolve, reject) => {
  let err = "";
  proc.stderr.on("data", (d) => {
    err += d;
    const m = err.match(/DevTools listening on (ws:\/\/\S+)/);
    if (m) resolve(m[1]);
  });
  proc.on("exit", () => reject(new Error("chrome exited: " + err)));
});

const ws = new WebSocket(wsURL);
await new Promise((r) => (ws.onopen = r));
let seq = 0;
const waiting = new Map();
ws.onmessage = (ev) => {
  const msg = JSON.parse(ev.data);
  if (msg.id && waiting.has(msg.id)) {
    const { resolve, reject } = waiting.get(msg.id);
    waiting.delete(msg.id);
    msg.error ? reject(new Error(msg.error.message)) : resolve(msg.result);
  }
};
const send = (method, params = {}, sessionId) =>
  new Promise((resolve, reject) => {
    const id = ++seq;
    waiting.set(id, { resolve, reject });
    ws.send(JSON.stringify({ id, method, params, sessionId }));
  });

const { targetId } = await send("Target.createTarget", { url: pairURL });
const { sessionId } = await send("Target.attachToTarget", { targetId, flatten: true });
const evaluate = async (expression) => {
  const r = await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true }, sessionId);
  if (r.exceptionDetails) throw new Error(r.exceptionDetails.text + " " + JSON.stringify(r.exceptionDetails.exception));
  return r.result.value;
};
const until = async (expression, ms = 8000) => {
  const end = Date.now() + ms;
  while (Date.now() < end) {
    if (await evaluate(expression)) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error("timed out waiting for " + expression);
};

const out = {};
try {
  if (scanFile) {
    await until(`document.getElementById("nocode") && !document.getElementById("nocode").hidden`);
    // The button has to be there to press: a hidden one still takes a
    // click() from here, which is not what a thumb can do.
    if (await evaluate(`document.getElementById("scan").hidden || document.getElementById("scan").offsetParent === null`)) throw new Error("the scan button is not shown");
    await evaluate(`document.getElementById("scan").click(); true`);
    // The first frame can decode before this poll sees the camera view:
    // either the camera view or the pairing form that follows it counts.
    await until(`!document.getElementById("camera").hidden || !document.getElementById("pair").hidden`, 15000);
    out.scanned = true;
  }
  await until(`document.getElementById("pair") && !document.getElementById("pair").hidden`, scanFile ? 20000 : 8000);
  await evaluate(`document.getElementById("name").value = "Pixel 9"; document.getElementById("pgo").click(); true`);
  if (process.env.FYLANE_PAIR_ONLY) {
    // Against a live Companion with nothing waiting: pairing alone is the check.
    await until(`!document.getElementById("idle").hidden`, 15000);
    out.device = await evaluate(`document.getElementById("iname").textContent`);
    out.until = await evaluate(`document.getElementById("iuntil").textContent`);
    out.ok = true;
    throw { done: true };
  }
  await until(`!document.getElementById("prompt").hidden`, 15000);
  out.title = await evaluate(`document.getElementById("ptitle").textContent`);
  out.command = await evaluate(`document.getElementById("pcmd").textContent`);
  out.detailHiddenBefore = await evaluate(`document.getElementById("pdetail").hidden`);
  await evaluate(`document.getElementById("details").click(); true`);
  out.changes = await evaluate(`document.getElementById("dchanges").textContent`);
  out.detailHiddenAfter = await evaluate(`document.getElementById("pdetail").hidden`);
  out.approveLabel = await evaluate(`document.getElementById("approve").textContent`);
  await evaluate(`window.__log = []; const f = window.fetch; window.fetch = (u, o) => f(u, o).then((r) => { window.__log.push(u + " " + r.status); return r; }, (e) => { window.__log.push(u + " " + e); throw e; }); true`);
  await evaluate(`document.getElementById("approve").click(); true`);
  await until(`!document.getElementById("idle").hidden`, 10000);
  out.device = await evaluate(`document.getElementById("iname").textContent`);
  // The notifications row fills in once the worker is ready and the
  // computer answered: it must not stay blank, and with push available and
  // nothing subscribed the button is offered.
  await until(`document.getElementById("ipush").textContent !== ""`, 10000);
  out.push = await evaluate(`document.getElementById("ipush").textContent`);
  out.pushButton = await evaluate(`!document.getElementById("pushrow").hidden && document.getElementById("pushon").offsetParent !== null`);
  // An outline button outside a row collapses to its text height in the
  // column layout; the rendered height says whether it got its padding.
  out.pushButtonHeight = await evaluate(`document.getElementById("pushon").getBoundingClientRect().height`);
  out.ok = true;
} catch (e) {
  if (!(e && e.done)) out.error = String(e);
  try { out.log = await evaluate(`JSON.stringify(window.__log || null) + " aerr=" + document.getElementById("aerr").textContent + " btn=" + document.getElementById("approve").textContent`); } catch {}
  try { out.state = await evaluate(`["unsupported","nocode","camera","pair","idle","prompt","gone"].filter(id => !document.getElementById(id).hidden).join(",") + " " + (document.getElementById("perr").textContent || "") + (document.getElementById("nerr").textContent || "") + (document.getElementById("cerr").textContent || "")`); } catch {}
}
console.log(JSON.stringify(out));
ws.close();
// The profile is removed only once Chrome has let go of it.
const exited = new Promise((r) => proc.on("exit", r));
proc.kill();
await exited;
rmSync(profile, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
