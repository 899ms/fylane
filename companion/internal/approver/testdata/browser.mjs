// Drives a headless Chrome through the approver page over the DevTools
// protocol: pair with the code, wait for a prompt, open its details, approve.
// Prints one JSON line with what the page showed. No dependencies: Node's own
// WebSocket and child_process are enough.
//
// usage: node browser.mjs <chrome> <pairing url>
import { spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const [chrome, pairURL] = process.argv.slice(2);
const profile = mkdtempSync(join(tmpdir(), "fylane-approver-"));
const proc = spawn(chrome, [
  "--headless=new", "--remote-debugging-port=0", `--user-data-dir=${profile}`,
  "--no-first-run", "--no-default-browser-check", "--disable-gpu", "about:blank",
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
  await until(`document.getElementById("pair") && !document.getElementById("pair").hidden`);
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
  out.ok = true;
} catch (e) {
  if (!(e && e.done)) out.error = String(e);
  try { out.log = await evaluate(`JSON.stringify(window.__log || null) + " aerr=" + document.getElementById("aerr").textContent + " btn=" + document.getElementById("approve").textContent`); } catch {}
  try { out.state = await evaluate(`["unsupported","nocode","pair","idle","prompt","gone"].filter(id => !document.getElementById(id).hidden).join(",") + " " + (document.getElementById("perr").textContent || "")`); } catch {}
}
console.log(JSON.stringify(out));
ws.close();
// The profile is removed only once Chrome has let go of it.
const exited = new Promise((r) => proc.on("exit", r));
proc.kill();
await exited;
rmSync(profile, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
