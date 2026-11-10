// GoAgentic tab names: a tab whose terminal carries the label
// goagentic.tab=<Name - title> is renamed to it, once, if nobody named the tab.
// The label is set by `/agents:start` (startup step 1) and by `goagentic dash`.
// A tab you rename yourself is never touched; resetting it to "" is respected too.
import { View, div } from "gpui-kit";
import { spaces, terminals, run, props, closeOverlay, notify } from "herdr";

const LABEL = "goagentic.tab";

export default class GoagenticTabs extends View {
  init() {
    this.done = new Set();
    try { this.done = new Set(JSON.parse(localStorage.getItem("done") || "[]")); } catch (_) {}
  }

  sync() {
    const tabs = new Map();
    for (const s of spaces()) for (const t of s.tabs || []) tabs.set(t.id, t);
    let changed = false;
    this.renamed = this.renamed || 0;
    for (const term of terminals()) {
      const labels = term.labels || {};
      const name = labels[LABEL];
      const tab = labels["herdr.tab"];
      if (!name || !tab) continue;
      // Remote terminals and tabs carry a "<machine>/" prefix in the app; labels don't.
      const addr = String(term.address || "");
      const prefix = addr.includes("/") ? addr.slice(0, addr.indexOf("/") + 1) : "";
      const id = tab.includes("/") ? tab : prefix + tab;
      const key = id + "|" + name;
      if (this.done.has(key)) continue;
      const info = tabs.get(id);
      if (!info) continue;
      if (!info.named && info.name !== name) { run("tab.rename", { id, name }); this.renamed++; }
      this.done.add(key);
      changed = true;
    }
    if (changed) {
      try { localStorage.setItem("done", JSON.stringify([...this.done].slice(-500))); } catch (_) {}
    }
  }

  render() {
    try { this.sync(); } catch (e) { console.log("goagentic-tabs: " + e); }
    const overlay = props()?.overlay;
    if (overlay && !this.closed) {
      this.closed = true;
      const n = this.renamed;
      const seen = terminals().filter((t) => (t.labels || {})[LABEL]).length;
      closeOverlay(overlay);
      notify({ title: "Agent tabs", body: `${n} renamed, ${seen} agent terminals labelled`, timeout: 4000 });
    }
    return div().size_full();
  }
}
