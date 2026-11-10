// GoAgentic home: every workspace and agent in one view. Click a running agent
// to go to its tab; Start opens an agent in a new tab of its workspace's Space,
// named after it. Type to filter, arrows and Enter to act, Esc to leave.
//
// Data: `goagentic dash --fleet --json`, run where the workspaces live. Where
// that is comes from ~/.config/herdr-app/goagentic.json (all optional):
//   { "machine": "<hc machine name, omit for this computer>",
//     "bin": "<full path to goagentic there>", "root": "<fleet folder>" }
import { View, div, svg } from "gpui-kit";
import { v_flex, h_flex, InputState, Input } from "gpui-base";
import { spaces, terminals, run, exec, readTextFile, appHome, theme, props, closeOverlay, notify } from "herdr";

const FOR_YOU = "#e0a030"; // amber: waiting on you
const OTHERS = "#e3814f"; // orange: P1 waiting on someone else
const PROBLEM = "#e5534b"; // red: blocked or unhealthy
const REFRESH_MS = 20000;

function ago(iso) {
  if (!iso) return "never run";
  const m = Math.max(0, (Date.now() - Date.parse(iso)) / 60000);
  if (m < 60) return `${Math.round(m)} min ago`;
  if (m < 60 * 36) return `${Math.round(m / 60)} h ago`;
  return `${Math.round(m / 1440)} d ago`;
}

// The slim shape the view draws, so the cache stays small.
function slim(fleet) {
  return (fleet.workspaces ?? []).map((w) => ({
    name: w.workspace,
    root: w.root,
    agents: (w.agents ?? []).filter((a) => !a.retired).map((a) => ({
      name: a.name, title: a.title ?? "", live: a.live ?? "", pane: a.pane ?? "",
      forYou: a.for_you ?? 0, waiting: a.waiting ?? 0, blocked: a.blocked ?? 0,
      unwrapped: !!a.unwrapped, health: a.health ?? "ok",
      next: a.next_session ?? "", active: a.last_active ?? "",
    })),
  }));
}

export default class GoagenticHome extends View {
  init(_props, cx) {
    this.fleet = [];
    this.conf = {};
    this.filter = "";
    this.at = 0;
    this.error = "";
    this.starting = "";
    try { this.fleet = JSON.parse(localStorage.getItem("fleet") || "[]"); } catch (_) {}
    this.loaded = this.fleet.length > 0;
    this.input = InputState.new({ placeholder: "Find an agent or workspace…" });
    this.input.on("change", (_e, cx) => { this.filter = this.input.value(); this.at = 0; cx.notify(); });
    this.input.on("submit", (_e, cx) => this.act(this.cards()[this.at], cx));
    this.input.focus();
    cx.timer.after(600, () => this.input.focus());
    this.handle = cx.focus_handle();
    this.refresh(cx);
    cx.timer.every(REFRESH_MS, (cx) => this.refresh(cx));
  }

  refresh(cx) {
    if (this.fetching) return;
    this.fetching = true;
    cx.spawn(async (cx) => {
      try {
        const raw = await readTextFile(appHome() + "/goagentic.json");
        if (raw) this.conf = JSON.parse(raw);
      } catch (_) {}
      try {
        const args = ["dash", "--fleet", "--json"];
        if (this.conf.root) args.push("--root", this.conf.root);
        const r = await exec(this.conf.bin || "goagentic", args, { on: this.conf.machine || null, timeout: 30000 });
        if (r.code !== 0) throw new Error((r.stderr || r.stdout || "goagentic failed").trim().slice(0, 200));
        this.fleet = slim(JSON.parse(r.stdout));
        this.loaded = true;
        this.error = "";
        try { localStorage.setItem("fleet", JSON.stringify(this.fleet)); } catch (_) {}
      } catch (e) {
        this.error = String(e?.message ?? e);
      }
      this.fetching = false;
      cx.notify();
    });
  }

  close() {
    const overlay = props()?.overlay;
    if (overlay) closeOverlay(overlay);
  }

  // Addresses and ids of the workspaces' machine carry "<machine>/" in the app.
  prefix() { return this.conf.machine ? this.conf.machine + "/" : ""; }

  // The agent's terminal in the app, when it runs there ("hc:t6" in the data).
  terminalOf(a) { return a.pane.startsWith("hc:") ? this.prefix() + a.pane.slice(3) : ""; }

  // The app Space a workspace's agents open in: the one a terminal of that
  // workspace is already in, else the Space named after it, else none (new).
  spaceOf(w) {
    const pre = this.prefix();
    for (const t of terminals()) {
      const addr = String(t.address ?? "");
      if (pre ? !addr.startsWith(pre) : addr.includes("/")) continue;
      const cwd = String(t.cwd ?? "");
      const space = (t.labels ?? {})["herdr.space"];
      if (space && (cwd === w.root || cwd.startsWith(w.root + "/"))) return space;
    }
    const named = spaces().find((s) => (s.machine ?? "") === (this.conf.machine ?? "") && s.name === w.name);
    return named ? named.id.slice(pre.length) : "";
  }

  groups() {
    const f = this.filter.trim().toLowerCase();
    return this.fleet
      .map((w) => ({ w, agents: w.agents.filter((a) => !f || (w.name + " " + a.name + " " + a.title).toLowerCase().includes(f)) }))
      .filter((g) => g.agents.length);
  }

  cards() {
    const groups = this.groups();
    const ordered = [...groups.filter((g) => g.agents.length > 2), ...groups.filter((g) => g.agents.length <= 2)];
    return ordered.flatMap((g) => g.agents.map((a) => ({ w: g.w, a })));
  }

  act(card, cx) {
    if (!card || this.starting) return;
    const { w, a } = card;
    const terminal = this.terminalOf(a);
    if (terminal) {
      this.close();
      run("agent.focus", { terminal });
      return;
    }
    if (a.live) {
      notify({ title: `${a.name} runs in the terminal Herdr`, body: "Wrap it there before starting it in the app.", timeout: 5000 });
      return;
    }
    this.starting = w.name + "/" + a.name;
    cx.notify();
    cx.spawn(async (cx) => {
      try {
        const args = ["dash", "--root", w.root, "--start", a.name];
        const space = this.spaceOf(w);
        if (space) args.push("--app-space", space);
        const r = await exec(this.conf.bin || "goagentic", args, { on: this.conf.machine || null, timeout: 60000 });
        if (r.code !== 0) throw new Error((r.stderr || r.stdout || "start failed").trim().slice(0, 200));
        const out = JSON.parse(r.stdout);
        if (out.terminal) {
          await cx.sleep(700);
          run("agent.focus", { terminal: this.prefix() + out.terminal });
          this.close();
        } else {
          notify({ title: a.name, body: out.message ?? "", timeout: 6000 });
        }
      } catch (e) {
        notify({ title: `Could not start ${a.name}`, body: String(e?.message ?? e), level: "error", timeout: 8000 });
      }
      this.starting = "";
      cx.notify();
    });
  }

  glyph(t, a) {
    if (a.live === "working") return svg("mods/workspace-chrome/spinner.svg").size(12).flex_none().text_color(t.working).spin(900);
    const color = a.live === "blocked" ? FOR_YOU : a.live === "done" ? t.done : a.live ? t.foreground : t.dim;
    return div().w(12).flex_none().text_color(color).child(a.live === "idle" ? "○" : a.live ? "●" : "·");
  }

  pill(text, color) {
    return div().px(6).h(18).rounded(9).flex_none().text_size(11).font_semibold().text_color(color).bg(color + "26").child(text);
  }

  card(t, c, on) {
    const { w, a } = c;
    const inApp = !!this.terminalOf(a);
    const starting = this.starting === w.name + "/" + a.name;
    const state = a.live === "blocked" ? "needs you" : a.live === "done" ? "finished" : a.live;
    const where = a.live ? (inApp ? state : `${state} · terminal Herdr`) : ago(a.active);
    return v_flex()
      .id(`gh-${w.name}-${a.name}`)
      .w(268).h(108).flex_none().p(10).gap(4).rounded(9)
      .border_1().border_color(on ? t.accent : t.border).bg(a.live ? t.raised : t.surface)
      .when(inApp, (el) => el.cursor_pointer().hover((s) => s.bg(t.hover)).on_click((_e, cx) => this.act(c, cx)))
      .child(
        h_flex().h(20).flex_none().gap(7).items_center()
          .child(this.glyph(t, a))
          .child(div().flex_1().truncate().font_semibold().text_color(t.foreground).child(a.name))
          .when(a.forYou > 0, (el) => el.child(this.pill(`${a.forYou} for you`, FOR_YOU)))
          .when(a.waiting > 0, (el) => el.child(this.pill(`${a.waiting}`, OTHERS)))
          .when(a.blocked > 0, (el) => el.child(this.pill(`${a.blocked} blocked`, PROBLEM))),
      )
      .child(div().h(17).flex_none().truncate().text_size(11.5).text_color(t.muted).child(a.title || " "))
      .child(div().h(16).flex_none().truncate().text_size(11).text_color(t.dim).child(a.next ? "Next: " + a.next : " "))
      .child(
        h_flex().flex_1().items_end().gap(6).text_size(11)
          .child(div().flex_1().truncate().text_color(a.live === "blocked" ? FOR_YOU : a.live ? t.muted : t.dim).child(where))
          .when(a.unwrapped, (el) => el.child(div().text_color(PROBLEM).child("unwrapped")))
          .when(a.health !== "ok" && !a.unwrapped, (el) => el.child(div().text_color(a.health === "fail" ? PROBLEM : t.dim).child("health")))
          .when(inApp, (el) => el.child(div().text_color(t.accent).child("Go to tab →")))
          .when(!a.live, (el) =>
            el.child(
              div().id(`gh-start-${w.name}-${a.name}`).px(8).h(20).rounded(6).border_1().border_color(t.border)
                .text_color(starting ? t.dim : t.foreground).cursor_pointer().hover((s) => s.bg(t.hover))
                .on_click((_e, cx) => { cx.stop_propagation(); this.act(c, cx); })
                .child(starting ? "starting…" : "Start"),
            )),
      );
  }

  render() {
    const t = theme();
    const groups = this.groups();
    const cards = this.cards();
    this.at = Math.max(0, Math.min(this.at, cards.length - 1));
    const all = this.fleet.flatMap((w) => w.agents);
    const sum = (k) => all.reduce((n, a) => n + (a[k] || 0), 0);
    const live = all.filter((a) => a.live).length;
    let i = 0;

    // Workspaces with several agents get a row of their own; the small ones
    // share rows, so a fleet of one-agent workspaces doesn't cost a row each.
    const section = (g) => {
      const mine = g.agents.reduce((n, a) => n + a.forYou, 0);
      const running = g.agents.filter((a) => a.live).length;
      return v_flex().gap(8).when(g.agents.length <= 2, (el) => el.flex_none())
        .child(
          h_flex().h(20).flex_none().gap(10).items_center()
            .child(div().text_size(13).font_semibold().text_color(t.foreground).child(g.w.name))
            .when(g.agents.length > 2 || running > 0, (el) => el.child(div().text_size(11).text_color(t.dim).child(g.agents.length > 2 ? `${g.agents.length} agents${running ? ` · ${running} live` : ""}` : `${running} live`)))
            .when(mine > 0 && g.agents.length > 1, (el) => el.child(div().text_size(11).text_color(FOR_YOU).child(`${mine} for you`))),
        )
        .child(h_flex().flex_wrap().gap(10).children(g.agents.map((a) => { const c = cards[i]; const on = i === this.at; i++; return this.card(t, c, on); })));
    };
    const big = groups.filter((g) => g.agents.length > 2);
    const small = groups.filter((g) => g.agents.length <= 2);
    const sections = big.map(section);
    if (small.length) sections.push(h_flex().flex_wrap().gap(10).gap_y(14).children(small.map(section)));

    return v_flex()
      .id("goagentic-home")
      .track_focus(this.handle)
      .size_full().min_h(0).p(20).gap(12).bg(t.popover).text_size(13)
      .on_key_down((e, cx) => {
        const k = e.key;
        if (k === "escape") return this.close();
        if (!cards.length) return;
        if (k === "down" || k === "right") this.at = (this.at + 1) % cards.length;
        else if (k === "up" || k === "left") this.at = (this.at + cards.length - 1) % cards.length;
        else return;
        cx.stop_propagation();
        cx.notify();
      })
      .child(
        h_flex().gap(12).items_center().flex_none()
          .child(div().text_size(16).font_semibold().text_color(t.foreground).child("GoAgentic"))
          .child(div().text_size(12).text_color(t.muted).child(`${all.length} agents · ${live} live`))
          .when(sum("forYou") > 0, (el) => el.child(this.pill(`${sum("forYou")} for you`, FOR_YOU)))
          .when(sum("waiting") > 0, (el) => el.child(this.pill(`${sum("waiting")} P1 waiting on others`, OTHERS)))
          .when(sum("blocked") > 0, (el) => el.child(this.pill(`${sum("blocked")} blocked`, PROBLEM)))
          .child(div().flex_1())
          .child(div().text_size(11).text_color(t.dim).child("Enter: go or start · Esc: close")),
      )
      .child(
        h_flex().h(36).flex_none().px(10).gap(8).items_center().rounded(9).border_1().border_color(t.accent).bg(t.background)
          .child(svg("mods/workspace-chrome/search.svg").size(15).text_color(t.dim))
          .child(Input.new(this.input).flex_1().h_full().text_size(12.8).text_color(t.foreground)),
      )
      .when(!!this.error, (el) => el.child(div().flex_none().text_size(12).text_color(PROBLEM).child("Could not read the fleet: " + this.error)))
      .when(!this.loaded && !this.error, (el) => el.child(div().flex_none().text_size(12).text_color(t.dim).child("Reading the fleet…")))
      .child(v_flex().id("gh-list").flex_1().min_h(0).gap(16).overflow_y_scrollbar().children(sections));
  }
}
