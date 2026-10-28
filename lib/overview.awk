# overview.awk — renderer for bin/jolitz-overview.
#
# Input files (tab-separated, prepared by the script):
#   hosts.tsv      host os ip distro lastconn tags port mode
#                  rules todos lastchange product version
#                  owner services
#   todos.tsv      host text
#   changes.tsv    date time host who headline rollback
#                  flags detail (lines joined by \037)
#   aliases.tsv    alias canonical
#   eol.tsv        F product fetched | R product cycle eol
#   audit-cur.tsv  date host key value (newest snapshot)
#   audit-prev.tsv date host key value (the one before)
#   guard.tsv      time mode reason subject
#   lint.txt       WARN lines of jolitz-memory-lint
#   links.tsv      from to label ("- Link:" in network.md)
#
# Variables: mode (html|text), today, now, days, tag, out,
# mem, jq, json (0|1), prom (0|1). Plain POSIX awk: it
# runs under the BWK awk of macOS and the mawk of Debian
# and Ubuntu.
#
# Every string from memory passes esc() on its way into
# HTML and jesc() on its way into JSON or a Prometheus
# label.

function base(p) { sub(/.*\//, "", p); return p }

function esc(s) {
  gsub(/&/, "\\&amp;", s); gsub(/</, "\\&lt;", s)
  gsub(/>/, "\\&gt;", s); gsub(/"/, "\\&quot;", s)
  return s
}

# JSON string body: backslash, double quote and control
# characters escaped. Built character by character; the
# awks disagree on backslashes in a gsub replacement.
function jesc(s,   i, n, c, o) {
  o = ""; n = length(s)
  for (i = 1; i <= n; i++) {
    c = substr(s, i, 1)
    if (c == "\\") o = o "\\\\"
    else if (c == "\"") o = o "\\\""
    else if (c in ctl) o = o ctl[c]
    else o = o c
  }
  return o
}
function jstr(s) { return "\"" jesc(s) "\"" }

# Days since 1970-01-01 for a YYYY-MM-DD date, -1 if the
# string is no such date.
function dnum(s,   y, m, d, era, yoe, doy, doe) {
  if (s !~ /^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]/) return -1
  y = substr(s, 1, 4) + 0; m = substr(s, 6, 2) + 0
  d = substr(s, 9, 2) + 0
  if (m <= 2) y--
  era = int(y / 400); yoe = y - era * 400
  doy = int((153 * (m > 2 ? m - 3 : m + 9) + 2) / 5) + d - 1
  doe = yoe * 365 + int(yoe / 4) - int(yoe / 100) + doy
  return era * 146097 + doe - 719468
}

function dstr(z,   era, doe, yoe, y, doy, mp, d, m) {
  z += 719468; era = int(z / 146097); doe = z - era * 146097
  yoe = int((doe - int(doe / 1460) + int(doe / 36524) \
    - int(doe / 146096)) / 365)
  y = yoe + era * 400
  doy = doe - (365 * yoe + int(yoe / 4) - int(yoe / 100))
  mp = int((5 * doy + 2) / 153)
  d = doy - int((153 * mp + 2) / 5) + 1
  m = mp < 10 ? mp + 3 : mp - 9
  if (m <= 2) y++
  return sprintf("%04d-%02d-%02d", y, m, d)
}

# Sets E_STATE (ended|soon|ok|open|unlisted|nodata|""),
# E_TEXT and E_DAYS (days left, 0 once ended, -1 when
# unknown) for host number n.
function eol(n,   p, v, c, mm, k) {
  p = hf[n, 12]; v = hf[n, 13]; E_STATE = ""; E_TEXT = ""; E_DAYS = -1
  if (p == "") return
  if (!(p in eolprod)) {
    E_STATE = "nodata"; E_TEXT = "no data (run with --eol)"
    return
  }
  c[1] = v
  mm = v; if (match(v, /^[0-9]+\.[0-9]+/)) mm = substr(v, 1, RLENGTH)
  c[2] = mm
  c[3] = v; sub(/\..*/, "", c[3])
  for (k = 1; k <= 3; k++)
    if (c[k] != "" && ((p SUBSEP c[k]) in eolrow)) break
  if (k > 3) {
    E_STATE = "unlisted"
    E_TEXT = "release " v " not listed for " p
    return
  }
  if (eolrow[p, c[k]] == "") {
    E_STATE = "open"; E_TEXT = "no end date announced"
    return
  }
  k = dnum(eolrow[p, c[k]]) - T
  E_DAYS = k < 0 ? 0 : k
  if (k < 0) {
    E_STATE = "ended"; E_TEXT = "ended " dstr(T + k)
  } else if (k <= 180) {
    E_STATE = "soon"; E_TEXT = "ends " dstr(T + k) " (" k " days)"
  } else {
    E_STATE = "ok"; E_TEXT = "until " dstr(T + k)
  }
}

# Age class of the last connection: "" | old | stale.
function age(n,   a) {
  a = dnum(hf[n, 5])
  if (a < 0) return ""
  a = T - a
  if (a >= 90) return "stale"
  if (a >= 30) return "old"
  return ""
}
function agetext(n,   a) {
  a = dnum(hf[n, 5])
  if (a < 0) return hf[n, 5] == "" ? "unknown" : hf[n, 5]
  a = T - a
  return hf[n, 5] " (" (a == 0 ? "today" : a == 1 ? "1 day" : a " days") ")"
}
# Days since the last connection, -1 when unknown.
function agedays(n,   a) {
  a = dnum(hf[n, 5])
  return a < 0 ? -1 : T - a
}

function pill(cls, text) {
  return "<span class=\"pill " cls "\">" esc(text) "</span>"
}
function eolcell(n) {
  eol(n)
  if (E_STATE == "") return "<span class=\"muted\">not tracked</span>"
  if (E_STATE == "ended") return pill("bad", E_TEXT)
  if (E_STATE == "soon") return pill("warn", E_TEXT)
  if (E_STATE == "ok" || E_STATE == "open") return pill("ok", E_TEXT)
  return "<span class=\"muted\">" esc(E_TEXT) "</span>"
}
function taglist(t,   a, i, n, s) {
  n = split(t, a, " "); s = ""
  for (i = 1; i <= n; i++) s = s "<span class=\"tag\">" esc(a[i]) "</span>"
  return s
}

FNR == 1 { file = base(FILENAME) }

file == "hosts.tsv" {
  H++
  for (i = 1; i <= 15; i++) hf[H, i] = $i
  hidx[$1] = H
  next
}
file == "todos.tsv" { TD++; td[TD, 1] = $1; td[TD, 2] = $2; next }
file == "changes.tsv" {
  C++
  for (i = 1; i <= 8; i++) ch[C, i] = $i
  next
}
file == "aliases.tsv" {
  al[$2] = (al[$2] == "" ? $1 : al[$2] " " $1); next
}
file == "eol.tsv" {
  if ($1 == "F") { eolprod[$2] = $3; EP++; eplist[EP] = $2 }
  else eolrow[$2, $3] = $4
  next
}
file == "audit-cur.tsv" {
  adate = $1
  if (tag != "" && !($2 in hidx)) next
  if (!(($2) in ahost)) { ahost[$2] = 1; AH++; ahl[AH] = $2 }
  if (!(($3) in akey)) { akey[$3] = 1; AK++; akl[AK] = $3 }
  av[$2, $3] = $4
  next
}
file == "audit-prev.tsv" { pdate = $1; pv[$2, $3] = $4; next }
file == "guard.tsv" {
  g = dnum(substr($1, 1, 10))
  if (g >= 0 && T - g <= 30) {
    r = $3
    if (!(r in gcount)) { GR++; grl[GR] = r }
    gcount[r]++
    if (substr($1, 1, 10) > glast[r]) glast[r] = substr($1, 1, 10)
    GN++
  }
  next
}
file == "lint.txt" {
  l = $0; sub(/^WARN +/, "", l)
  lh = l; sub(/:.*/, "", lh)
  # With --tag, warnings about other servers stay out.
  if (tag != "" && lh != "memory" && !(lh in hidx)) next
  L++; lint[L] = $0
  next
}
file == "links.tsv" {
  # With --tag, a link needs one end inside the selection.
  if (tag != "" && !($1 in hidx) && !($2 in hidx)) next
  LK++; lk[LK, 1] = $1; lk[LK, 2] = $2; lk[LK, 3] = $3
  if (!($1 in nodeidx)) { NN++; node[NN] = $1; nodeidx[$1] = NN }
  if (!($2 in nodeidx)) { NN++; node[NN] = $2; nodeidx[$2] = NN }
  next
}

BEGIN {
  T = dnum(today)
  for (i = 1; i < 32; i++) ctl[sprintf("%c", i)] = sprintf("\\u%04x", i)
  ctl["\t"] = "\\t"; ctl["\n"] = "\\n"; ctl["\r"] = "\\r"
}

# --- shared computations ------------------------------------
function compute(   i, k, h, n, seen, v, cnt, tv) {
  # sort hosts by name (insertion sort, the list is short)
  for (i = 1; i <= H; i++) ord[i] = i
  for (i = 2; i <= H; i++) {
    k = ord[i]
    for (n = i - 1; n >= 1 && hf[ord[n], 1] > hf[k, 1]; n--)
      ord[n + 1] = ord[n]
    ord[n + 1] = k
  }
  for (i = 1; i <= H; i++) {
    if (age(i) != "") STALE++
    eol(i)
    if (E_STATE == "ended" || E_STATE == "soon") EOLW++
    if (hf[i, 6] != "") {
      n = split(hf[i, 6], tv, " ")
      for (k = 1; k <= n; k++)
        if (!(tv[k] in tagseen)) { tagseen[tv[k]] = 1; TG++; tgl[TG] = tv[k] }
    }
  }
  for (i = 1; i <= C; i++)
    if (dnum(ch[i, 1]) >= 0 && T - dnum(ch[i, 1]) <= days + 0) RECENT++
  # audit: rows whose readable values disagree
  for (k = 1; k <= AK; k++) {
    split("", seen); cnt = 0
    for (h = 1; h <= AH; h++) {
      if (!((ahl[h] SUBSEP akl[k]) in av)) continue
      v = av[ahl[h], akl[k]]
      if (v ~ /^unknown/) continue
      if (!(v in seen)) { seen[v] = 1; cnt++ }
    }
    drift[k] = cnt > 1
    if (drift[k]) DRIFT++
  }
  for (h = 1; h <= AH; h++)
    for (k = 1; k <= AK; k++)
      if (((ahl[h] SUBSEP akl[k]) in pv) && ((ahl[h] SUBSEP akl[k]) in av) \
          && pv[ahl[h], akl[k]] != av[ahl[h], akl[k]]) {
        AC++; acl[AC] = ahl[h] SUBSEP akl[k]
      }
  # Sort the tag list for a stable filter bar.
  for (i = 2; i <= TG; i++) {
    v = tgl[i]
    for (n = i - 1; n >= 1 && tgl[n] > v; n--) tgl[n + 1] = tgl[n]
    tgl[n + 1] = v
  }
  services()
  for (i = 1; i <= H; i++) if (hf[i, 14] != "") OWN++
}

# "- Services: nginx 1.26, postgresql 17, docker" per host
# -> svl[1..SV] sorted names, svhas[name, host], svver[name,
# host] and svn[host] names with svv[host, k] versions.
function services(   i, k, n, a, v, name, ver, j) {
  for (i = 1; i <= H; i++) {
    svn[i] = 0
    if (hf[i, 15] == "") continue
    n = split(hf[i, 15], a, ",")
    for (k = 1; k <= n; k++) {
      v = a[k]; sub(/^[ \t]+/, "", v); sub(/[ \t]+$/, "", v)
      if (v == "") continue
      name = v; ver = ""
      if (index(v, " ") > 0) {
        name = substr(v, 1, index(v, " ") - 1)
        ver = substr(v, index(v, " ") + 1); sub(/^[ \t]+/, "", ver)
      }
      svn[i]++; svname[i, svn[i]] = name; svv[i, svn[i]] = ver
      svhas[name, i] = 1; svver[name, i] = ver
      if (!(name in svseen)) { svseen[name] = 1; SV++; svl[SV] = name }
    }
  }
  for (i = 2; i <= SV; i++) {
    v = svl[i]
    for (j = i - 1; j >= 1 && svl[j] > v; j--) svl[j + 1] = svl[j]
    svl[j + 1] = v
  }
}

function hosttags(h) { return (h in hidx) ? hf[hidx[h], 6] : "" }

# --- text ---------------------------------------------------
function text(   i, n, k, h, any) {
  print "jolitz fleet summary, " today (tag != "" ? ", tag " tag : "")
  print "Hosts: " H + 0 ", open to-dos: " TD + 0 ", changes in " days \
    " days: " RECENT + 0
  print ""
  print "Changes, last " days " days"
  any = 0
  for (i = 1; i <= C; i++) {
    if (dnum(ch[i, 1]) < 0 || T - dnum(ch[i, 1]) > days + 0) continue
    print "  " ch[i, 1] " " ch[i, 3] ": " ch[i, 5]; any = 1
  }
  if (!any) print "  none"
  print ""
  print "Open to-dos"
  for (i = 1; i <= TD; i++) print "  " td[i, 1] ": " td[i, 2]
  if (!TD) print "  none"
  print ""
  print "Not connected for 30 days or more"
  any = 0
  for (k = 1; k <= H; k++) {
    i = ord[k]
    if (age(i) != "") { print "  " hf[i, 1] ": last " agetext(i); any = 1 }
  }
  if (!any) print "  none"
  print ""
  if (OWN) {
    print "Owners"
    for (k = 1; k <= H; k++) {
      i = ord[k]
      if (hf[i, 14] != "") print "  " hf[i, 1] ": " hf[i, 14]
    }
    print ""
  }
  print "End of life"
  any = 0
  for (k = 1; k <= H; k++) {
    i = ord[k]; eol(i)
    if (E_STATE == "ended" || E_STATE == "soon") {
      print "  " hf[i, 1] ": " hf[i, 2] ", " E_TEXT; any = 1
    }
  }
  if (!any) print "  none within 180 days"
  for (k = 1; k <= H; k++) {
    eol(ord[k])
    if (E_STATE == "nodata") {
      print "  some systems have no EOL data yet: run with --eol"
      break
    }
  }
  if (AK) {
    print ""
    print "Fleet audit " adate ": " DRIFT + 0 " settings differ" \
      (pdate != "" ? ", " AC + 0 " changed since " pdate : "")
    for (i = 1; i <= AC; i++) {
      split(acl[i], n, SUBSEP)
      print "  " n[1] " " n[2] ": " pv[n[1], n[2]] " -> " av[n[1], n[2]]
    }
  }
  if (GN) {
    print ""
    print "Guard denials, last 30 days: " GN
    for (i = 1; i <= GR; i++)
      print "  " gcount[grl[i]] "x " substr(grl[i], 1, 100)
  }
  print ""
  print "Memory lint: " L + 0 " warnings"
  for (i = 1; i <= L; i++) print "  " lint[i]
  if (EP) {
    printf "EOL dates: endoflife.date, fetched"
    for (i = 1; i <= EP; i++)
      printf "%s %s %s", (i > 1 ? "," : ""), eplist[i], eolprod[eplist[i]]
    print ""
  }
}

# --- html ---------------------------------------------------
function css() {
  return "<style>\n" \
":root{--bg:#f7f7f5;--card:#fff;--fg:#1d1d1b;--muted:#6b6b66;" \
"--line:#e3e2dd;--accent:#2f5d8a;--ok:#2e7d4f;--ok-bg:#e6f2ea;" \
"--warn:#8a5a00;--warn-bg:#fbf0d9;--bad:#a12a2a;--bad-bg:#f9e3e1;" \
"--code:#f0efea}\n" \
"@media (prefers-color-scheme:dark){:root:not([data-theme=light]){" \
"--bg:#141413;--card:#1e1e1c;--fg:#ecebe6;--muted:#9c9b94;" \
"--line:#33322f;--accent:#8ab4e0;--ok:#7fcf9c;--ok-bg:#1d3326;" \
"--warn:#f0c674;--warn-bg:#3a2f15;--bad:#f28b82;--bad-bg:#3d1f1d;" \
"--code:#262624}}\n" \
"*{box-sizing:border-box}body{margin:0;background:var(--bg);" \
"color:var(--fg);font:15px/1.5 -apple-system,BlinkMacSystemFont," \
"'Segoe UI',Roboto,sans-serif}main{max-width:1180px;margin:0 auto;" \
"padding:24px 16px 64px}h1{font-size:24px;margin:0 0 4px}" \
"h2{font-size:17px;margin:32px 0 10px}a{color:var(--accent)}" \
".muted{color:var(--muted)}.sub{color:var(--muted);font-size:13px}" \
".kpis{display:grid;grid-template-columns:repeat(auto-fit," \
"minmax(140px,1fr));gap:10px;margin:20px 0}.kpi{background:var(--card);" \
"border:1px solid var(--line);border-radius:8px;padding:12px 14px}" \
".kpi b{display:block;font-size:24px;font-variant-numeric:tabular-nums}" \
".kpi span{color:var(--muted);font-size:13px}" \
".box{background:var(--card);border:1px solid var(--line);" \
"border-radius:8px;overflow-x:auto}table{border-collapse:collapse;" \
"width:100%}th,td{text-align:left;padding:8px 12px;" \
"border-bottom:1px solid var(--line);vertical-align:top}" \
"th{font-size:12px;text-transform:uppercase;letter-spacing:.04em;" \
"color:var(--muted);font-weight:600}tbody th{text-transform:none;" \
"letter-spacing:0;font:500 13px ui-monospace,Menlo,monospace;" \
"color:var(--fg);white-space:nowrap}tr:last-child td,tr:last-child th{" \
"border-bottom:0}.facts th{font:600 13px/1.5 inherit;font-family:inherit;" \
"color:var(--muted);width:11em}" \
".pill{display:inline-block;padding:1px 8px;border-radius:999px;" \
"font-size:12px;white-space:nowrap}.ok{background:var(--ok-bg);" \
"color:var(--ok)}.warn{background:var(--warn-bg);color:var(--warn)}" \
".bad{background:var(--bad-bg);color:var(--bad)}.tag{display:inline-block;" \
"border:1px solid var(--line);border-radius:4px;padding:0 6px;" \
"margin:0 4px 2px 0;font-size:12px}ul.list{margin:0;padding:0;" \
"list-style:none}ul.list li{padding:8px 12px;border-bottom:1px solid " \
"var(--line)}ul.list li:last-child{border-bottom:0}" \
"details summary{cursor:pointer}pre{background:var(--code);" \
"padding:10px 12px;border-radius:6px;overflow-x:auto;font-size:13px;" \
"white-space:pre-wrap;word-break:break-word}" \
".filters{display:flex;flex-wrap:wrap;gap:6px;margin:8px 0 0}" \
".filters button{font:inherit;font-size:13px;border:1px solid var(--line);" \
"background:var(--card);color:var(--fg);border-radius:999px;" \
"padding:2px 12px;cursor:pointer}.filters button[aria-pressed=true]{" \
"background:var(--accent);color:var(--bg);border-color:var(--accent)}" \
"td.diff{font-weight:600}tr.drift th:first-child::before{content:'! '}" \
"code{font-size:13px}.matrix td{text-align:center;white-space:nowrap}" \
".matrix th.svc{font:500 13px ui-monospace,Menlo,monospace;" \
"text-transform:none;letter-spacing:0;color:var(--fg)}" \
"svg.chart{display:block;width:100%;height:auto;padding:8px}" \
"svg.chart text{fill:var(--muted);font-size:12px}" \
"svg.chart text.host,svg.chart text.node{fill:var(--fg);font-size:13px}" \
"svg.chart .lane{stroke:var(--line)}svg.chart .tick{stroke:var(--line)}" \
"svg.chart .dot{fill:var(--accent)}svg.chart .flag{fill:var(--warn)}" \
"svg.chart .edge{stroke:var(--muted);stroke-width:1.2}" \
"svg.chart text.lbl{paint-order:stroke;stroke:var(--card);" \
"stroke-width:4px;stroke-linejoin:round}" \
"svg.chart .nd{fill:var(--card);stroke:var(--accent);stroke-width:2}" \
"svg.chart a text{fill:var(--accent)}" \
"@media print{.filters,script{display:none}" \
"body{background:#fff}.box{border-color:#ccc}}\n</style>"
}

function head(f, title) {
  print "<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">" > f
  print "<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">" > f
  print "<title>" esc(title) "</title>" > f
  print css() > f
  print "</head>\n<body>\n<main>" > f
}

function foot(f,   i) {
  printf "<p class=\"sub\" style=\"margin-top:40px\">Built %s by " \
    "<code>bin/jolitz-overview</code> from <code>memory/</code>. " \
    "No server was contacted.", esc(now) > f
  if (EP) {
    printf " End-of-life dates from endoflife.date, fetched" > f
    for (i = 1; i <= EP; i++)
      printf "%s %s %s", (i > 1 ? "," : ""), esc(eplist[i]), \
        esc(eolprod[eplist[i]]) > f
    printf "." > f
  }
  print "</p>\n</main>" > f
}

function change_item(f, i, link,   d, n, k) {
  printf "<li data-tags=\"%s\"><details><summary><b>%s %s</b> ", \
    esc(hosttags(ch[i, 3])), esc(ch[i, 1]), esc(ch[i, 2]) > f
  if (link) printf "<a href=\"hosts/%s.html\">%s</a> ", \
    esc(ch[i, 3]), esc(ch[i, 3]) > f
  if (ch[i, 4] != "") printf "<span class=\"muted\">[%s]</span> ", \
    esc(ch[i, 4]) > f
  printf "%s</summary>", esc(ch[i, 5]) > f
  if (ch[i, 8] != "") {
    n = split(ch[i, 8], d, "\037")
    printf "<pre>" > f
    for (k = 1; k <= n; k++) printf "%s%s", (k > 1 ? "\n" : ""), esc(d[k]) > f
    printf "</pre>" > f
  } else printf "<p class=\"sub\">No detail recorded.</p>" > f
  print "</details></li>" > f
}

function index_page(f,   i, k, h, n, cls, any, v, r, pa) {
  head(f, "Fleet overview")
  print "<h1>Fleet overview</h1>" > f
  printf "<p class=\"sub\">%s%s</p>\n", esc(now), \
    (tag != "" ? " &middot; tag " esc(tag) : "") > f
  print "<div class=\"kpis\">" > f
  printf "<div class=\"kpi\"><b>%d</b><span>servers</span></div>\n", H > f
  printf "<div class=\"kpi\"><b>%d</b><span>not seen for 30+ days</span></div>\n", STALE > f
  printf "<div class=\"kpi\"><b>%d</b><span>open to-dos</span></div>\n", TD > f
  printf "<div class=\"kpi\"><b>%d</b><span>changes, %d days</span></div>\n", RECENT, days > f
  printf "<div class=\"kpi\"><b>%d</b><span>EOL within 180 days</span></div>\n", EOLW > f
  printf "<div class=\"kpi\"><b>%d</b><span>memory warnings</span></div>\n", L > f
  print "</div>" > f

  if (TG) {
    print "<div class=\"filters\" role=\"group\" aria-label=\"Filter by tag\">" > f
    print "<button type=\"button\" data-filter=\"*\" aria-pressed=\"true\">all</button>" > f
    for (i = 1; i <= TG; i++)
      printf "<button type=\"button\" data-filter=\"%s\" aria-pressed=\"false\">%s</button>\n", \
        esc(tgl[i]), esc(tgl[i]) > f
    print "</div>" > f
  }

  # Attention: what needs a decision.
  any = 0
  for (k = 1; k <= H; k++) {
    i = ord[k]; eol(i)
    if (E_STATE == "ended" || E_STATE == "soon" || age(i) == "stale") any = 1
  }
  if (any || DRIFT || AC || L) {
    print "<h2>Needs attention</h2>\n<div class=\"box\"><ul class=\"list\">" > f
    for (k = 1; k <= H; k++) {
      i = ord[k]; eol(i)
      if (E_STATE == "ended" || E_STATE == "soon")
        printf "<li data-tags=\"%s\">%s <a href=\"hosts/%s.html\">%s</a>: %s, support %s</li>\n", \
          esc(hf[i, 6]), pill(E_STATE == "ended" ? "bad" : "warn", "EOL"), \
          esc(hf[i, 1]), esc(hf[i, 1]), esc(hf[i, 2]), esc(E_TEXT) > f
      if (age(i) == "stale")
        printf "<li data-tags=\"%s\">%s <a href=\"hosts/%s.html\">%s</a>: last connected %s; the memory may be out of date</li>\n", \
          esc(hf[i, 6]), pill("warn", "stale"), esc(hf[i, 1]), \
          esc(hf[i, 1]), esc(agetext(i)) > f
    }
    if (DRIFT)
      printf "<li>%s Fleet audit %s: %d settings differ between servers (<a href=\"#audit\">below</a>)</li>\n", \
        pill("warn", "drift"), esc(adate), DRIFT > f
    for (i = 1; i <= AC; i++) {
      split(acl[i], pa, SUBSEP)
      printf "<li data-tags=\"%s\">%s <a href=\"hosts/%s.html\">%s</a>: <code>%s</code> changed from <code>%s</code> to <code>%s</code> between the audits of %s and %s</li>\n", \
        esc(hosttags(pa[1])), pill("warn", "changed"), esc(pa[1]), esc(pa[1]), \
        esc(pa[2]), esc(pv[pa[1], pa[2]]), esc(av[pa[1], pa[2]]), esc(pdate), \
        esc(adate) > f
    }
    for (i = 1; i <= L; i++) {
      r = lint[i]; sub(/^WARN +/, "", r)
      printf "<li>%s %s</li>\n", pill("warn", "memory"), esc(r) > f
    }
    print "</ul></div>" > f
  }

  print "<h2>Servers</h2>\n<div class=\"box\"><table>" > f
  print "<thead><tr><th>Server</th><th>Owner</th><th>Tags</th><th>OS</th><th>End of life</th><th>IP</th><th>Last connected</th><th>To-dos</th><th>Last change</th></tr></thead><tbody>" > f
  for (k = 1; k <= H; k++) {
    i = ord[k]; cls = age(i)
    printf "<tr data-tags=\"%s\"><td><a href=\"hosts/%s.html\">%s</a>", \
      esc(hf[i, 6]), esc(hf[i, 1]), esc(hf[i, 1]) > f
    if (al[hf[i, 1]] != "") printf "<br><span class=\"sub\">also %s</span>", \
      esc(al[hf[i, 1]]) > f
    if (hf[i, 9] == 1) printf "<br><span class=\"sub\">own rules.md</span>" > f
    printf "</td><td>%s</td><td>%s</td><td>%s", esc(hf[i, 14]), \
      taglist(hf[i, 6]), esc(hf[i, 2]) > f
    if (hf[i, 8] != "") printf " <span class=\"sub\">(%s)</span>", esc(hf[i, 8]) > f
    printf "</td><td>%s</td><td>%s", eolcell(i), esc(hf[i, 3]) > f
    if (hf[i, 7] != "") printf "<br><span class=\"sub\">ssh port %s</span>", esc(hf[i, 7]) > f
    printf "</td><td>%s</td>", \
      (cls == "" ? esc(agetext(i)) : pill(cls == "stale" ? "bad" : "warn", agetext(i))) > f
    printf "<td>%s</td><td>%s</td></tr>\n", \
      (hf[i, 10] > 0 ? hf[i, 10] : "<span class=\"muted\">0</span>"), \
      esc(hf[i, 11]) > f
  }
  if (!H) print "<tr><td colspan=\"9\" class=\"muted\">No servers in memory/servers/ yet.</td></tr>" > f
  print "</tbody></table></div>" > f

  if (SV) {
    print "<h2>Services</h2>" > f
    print "<p class=\"sub\">From <code>- Services:</code> in memory.md. A cell shows the version, a dot a service without one.</p>" > f
    print "<div class=\"box\"><table class=\"matrix\"><thead><tr><th>Service</th>" > f
    for (k = 1; k <= H; k++)
      printf "<th><a href=\"hosts/%s.html\">%s</a></th>", esc(hf[ord[k], 1]), \
        esc(hf[ord[k], 1]) > f
    print "</tr></thead><tbody>" > f
    for (i = 1; i <= SV; i++) {
      printf "<tr><th class=\"svc\">%s</th>", esc(svl[i]) > f
      for (k = 1; k <= H; k++) {
        n = ord[k]
        if (!((svl[i] SUBSEP n) in svhas)) printf "<td></td>" > f
        else if (svver[svl[i], n] == "") printf "<td>&bull;</td>" > f
        else printf "<td>%s</td>", esc(svver[svl[i], n]) > f
      }
      print "</tr>" > f
    }
    print "</tbody></table></div>" > f
  }

  if (LK) network(f)

  print "<h2>Open to-dos</h2>\n<div class=\"box\"><ul class=\"list\">" > f
  for (i = 1; i <= TD; i++)
    printf "<li data-tags=\"%s\"><a href=\"hosts/%s.html\">%s</a>: %s</li>\n", \
      esc(hosttags(td[i, 1])), esc(td[i, 1]), esc(td[i, 1]), esc(td[i, 2]) > f
  if (!TD) print "<li class=\"muted\">Nothing open.</li>" > f
  print "</ul></div>" > f

  printf "<h2>Changes, last %d days</h2>\n", days > f
  print "<p class=\"sub\"><a href=\"timeline.html\">Timeline</a>: the same window, one lane per server.</p>" > f
  print "<div class=\"box\"><ul class=\"list\">" > f
  any = 0
  for (i = 1; i <= C; i++) {
    if (dnum(ch[i, 1]) < 0 || T - dnum(ch[i, 1]) > days + 0) continue
    change_item(f, i, 1); any = 1
  }
  if (!any) print "<li class=\"muted\">No changes recorded in this window.</li>" > f
  print "</ul></div>" > f

  if (AK) {
    printf "<h2 id=\"audit\">Fleet audit %s</h2>\n", esc(adate) > f
    printf "<p class=\"sub\">%d settings differ. Values that need root are not counted.%s</p>\n", \
      DRIFT, (pdate != "" ? " Previous audit: " esc(pdate) "." : "") > f
    print "<div class=\"box\"><table><thead><tr><th>Setting</th>" > f
    for (h = 1; h <= AH; h++) printf "<th>%s</th>", esc(ahl[h]) > f
    print "</tr></thead><tbody>" > f
    for (r = 0; r <= 1; r++)
      for (k = 1; k <= AK; k++) {
        if (drift[k] != (r == 0)) continue
        printf "<tr%s><th>%s</th>", (drift[k] ? " class=\"drift\"" : ""), \
          esc(akl[k]) > f
        for (h = 1; h <= AH; h++) {
          v = ((ahl[h] SUBSEP akl[k]) in av) ? av[ahl[h], akl[k]] : ""
          if (v ~ /^unknown\(needs-root\)/) v = "needs root"
          n = (((ahl[h] SUBSEP akl[k]) in pv) && pv[ahl[h], akl[k]] != av[ahl[h], akl[k]])
          printf "<td%s>%s%s</td>", (drift[k] ? " class=\"diff\"" : ""), \
            esc(v), (n ? " " pill("warn", "was " pv[ahl[h], akl[k]]) : "") > f
        }
        print "</tr>" > f
      }
    print "</tbody></table></div>" > f
  }

  if (GN) {
    print "<h2>Guard denials, last 30 days</h2>" > f
    print "<p class=\"sub\">From <code>~/.cache/jolitz/guard.log</code>. A denial that blocks real work is a false positive worth reporting; the commands stay in the log, not on this page.</p>" > f
    print "<div class=\"box\"><table><thead><tr><th>Count</th><th>Last</th><th>Reason</th></tr></thead><tbody>" > f
    for (i = 1; i <= GR; i++)
      printf "<tr><td>%d</td><td>%s</td><td>%s</td></tr>\n", gcount[grl[i]], \
        esc(glast[grl[i]]), esc(grl[i]) > f
    print "</tbody></table></div>" > f
  }

  foot(f)
  if (TG) {
    print "<script>" > f
    print "document.querySelectorAll('[data-filter]').forEach(function(b){b.addEventListener('click',function(){var t=b.getAttribute('data-filter');document.querySelectorAll('[data-filter]').forEach(function(x){x.setAttribute('aria-pressed',x===b?'true':'false')});document.querySelectorAll('[data-tags]').forEach(function(el){el.hidden=t!=='*'&&(' '+el.getAttribute('data-tags')+' ').indexOf(' '+t+' ')<0})})})" > f
    print "</script>" > f
  }
  print "</body>\n</html>" > f
  close(f)
}

function rawfile(f, path,   line, any) {
  any = 0
  while ((getline line < path) > 0) {
    if (!any) printf "<pre>" > f
    printf "%s%s", (any ? "\n" : ""), esc(line) > f
    any = 1
  }
  close(path)
  if (any) print "</pre>" > f
  return any
}

function host_page(n,   f, h, i, any, k, v) {
  h = hf[n, 1]
  f = out "/hosts/" h ".html"
  head(f, h)
  print "<p class=\"sub\"><a href=\"../index.html\">Fleet overview</a></p>" > f
  printf "<h1>%s</h1>\n", esc(h) > f
  print "<p class=\"sub\">Handover sheet: what runs here, what changed and why, what is open. Built from memory/, not from the live server.</p>" > f
  print "<h2>Facts</h2>\n<div class=\"box\"><table class=\"facts\"><tbody>" > f
  printf "<tr><th>OS</th><td>%s</td></tr>\n", esc(hf[n, 2]) > f
  printf "<tr><th>End of life</th><td>%s</td></tr>\n", eolcell(n) > f
  printf "<tr><th>IP</th><td>%s</td></tr>\n", esc(hf[n, 3]) > f
  if (hf[n, 7] != "") printf "<tr><th>SSH port</th><td>%s</td></tr>\n", esc(hf[n, 7]) > f
  if (hf[n, 8] != "") printf "<tr><th>Mode</th><td>%s</td></tr>\n", esc(hf[n, 8]) > f
  printf "<tr><th>Last connected</th><td>%s</td></tr>\n", esc(agetext(n)) > f
  if (hf[n, 6] != "") printf "<tr><th>Tags</th><td>%s</td></tr>\n", taglist(hf[n, 6]) > f
  if (hf[n, 14] != "") printf "<tr><th>Owner</th><td>%s</td></tr>\n", esc(hf[n, 14]) > f
  if (hf[n, 15] != "") printf "<tr><th>Services</th><td>%s</td></tr>\n", esc(hf[n, 15]) > f
  if (al[h] != "") printf "<tr><th>Also known as</th><td>%s</td></tr>\n", esc(al[h]) > f
  print "</tbody></table></div>" > f

  print "<h2>Open to-dos</h2>\n<div class=\"box\"><ul class=\"list\">" > f
  any = 0
  for (i = 1; i <= TD; i++)
    if (td[i, 1] == h) { printf "<li>%s</li>\n", esc(td[i, 2]) > f; any = 1 }
  if (!any) print "<li class=\"muted\">Nothing open.</li>" > f
  print "</ul></div>" > f

  # Flags are constraints a colleague must know.
  any = 0
  for (i = 1; i <= C; i++)
    if (ch[i, 3] == h && ch[i, 7] != "") {
      if (!any) print "<h2>Standing constraints</h2>\n<div class=\"box\"><ul class=\"list\">" > f
      printf "<li>%s <span class=\"sub\">(since %s)</span></li>\n", \
        esc(ch[i, 7]), esc(ch[i, 1]) > f
      any = 1
    }
  if (any) print "</ul></div>" > f

  if (hf[n, 9] == 1) {
    print "<h2>Own rules (rules.md)</h2>" > f
    rawfile(f, mem "/servers/" h "/rules.md")
  }

  print "<h2>Changes</h2>\n<div class=\"box\"><ul class=\"list\">" > f
  any = 0
  for (i = 1; i <= C; i++)
    if (ch[i, 3] == h) { change_item(f, i, 0); any = 1 }
  if (!any) print "<li class=\"muted\">No changelog yet.</li>" > f
  print "</ul></div>" > f

  any = 0
  for (k = 1; k <= AK; k++)
    if ((h SUBSEP akl[k]) in av) {
      if (!any) {
        printf "<h2>Fleet audit %s</h2>\n<div class=\"box\"><table><tbody>\n", esc(adate) > f
        any = 1
      }
      v = av[h, akl[k]]
      printf "<tr><th>%s</th><td>%s%s</td></tr>\n", esc(akl[k]), esc(v), \
        (drift[k] ? " " pill("warn", "differs in the fleet") : "") > f
    }
  if (any) print "</tbody></table></div>" > f

  print "<h2>memory.md</h2>" > f
  if (!rawfile(f, mem "/servers/" h "/memory.md"))
    print "<p class=\"muted\">No memory.md.</p>" > f
  foot(f)
  print "</body>\n</html>" > f
  close(f)
}

# --- network: hosts on a circle, labelled lines ----------------
function hostlink(name,   s) {
  s = esc(name)
  if (name in hidx) return "<a href=\"hosts/" s ".html\">" s "</a>"
  return s
}
function network(f,   i, cx, cy, r, w, h, a, pi, nx, ny, x1, y1, x2, y2, \
    anchor, dx, dy) {
  pi = atan2(0, -1)
  w = 760; h = NN <= 2 ? 220 : 160 + 44 * NN
  if (h > 640) h = 640
  cx = w / 2; cy = h / 2; r = (h - 70) / 2
  print "<h2>Network</h2>" > f
  print "<p class=\"sub\">From <code>- Link:</code> lines in <code>memory/network.md</code>. Arrows point from the side that opens the connection.</p>" > f
  print "<div class=\"box\">" > f
  printf "<svg class=\"chart\" viewBox=\"0 0 %d %d\" role=\"img\" aria-label=\"Network links\">\n", \
    w, h > f
  print "<defs><marker id=\"arrow\" viewBox=\"0 0 10 10\" refX=\"10\" refY=\"5\" markerWidth=\"7\" markerHeight=\"7\" orient=\"auto\"><path d=\"M0 0 L10 5 L0 10 z\" style=\"fill:var(--muted)\"/></marker></defs>" > f
  for (i = 1; i <= NN; i++) {
    a = 2 * pi * (i - 1) / NN - pi / 2
    nx[i] = cx + r * cos(a); ny[i] = cy + r * sin(a)
  }
  for (i = 1; i <= LK; i++) {
    if (lk[i, 1] == lk[i, 2]) continue
    x1 = nx[nodeidx[lk[i, 1]]]; y1 = ny[nodeidx[lk[i, 1]]]
    x2 = nx[nodeidx[lk[i, 2]]]; y2 = ny[nodeidx[lk[i, 2]]]
    # stop short of the node circle (radius 8) on both ends
    dx = x2 - x1; dy = y2 - y1; a = sqrt(dx * dx + dy * dy)
    if (a < 1) continue
    printf "<line class=\"edge\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" marker-end=\"url(#arrow)\"><title>%s -&gt; %s%s</title></line>\n", \
      x1 + dx * 10 / a, y1 + dy * 10 / a, x2 - dx * 12 / a, y2 - dy * 12 / a, \
      esc(lk[i, 1]), esc(lk[i, 2]), (lk[i, 3] != "" ? " (" esc(lk[i, 3]) ")" : "") > f
    # The label sits a third of the way from the opening
    # side, so two edges through the centre do not share it.
    if (lk[i, 3] != "")
      printf "<text class=\"lbl\" x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\">%s</text>\n", \
        x1 + dx * 0.3, y1 + dy * 0.3 - 5, esc(lk[i, 3]) > f
  }
  for (i = 1; i <= NN; i++) {
    a = 2 * pi * (i - 1) / NN - pi / 2
    anchor = cos(a) > 0.3 ? "start" : cos(a) < -0.3 ? "end" : "middle"
    printf "<circle class=\"nd\" cx=\"%.1f\" cy=\"%.1f\" r=\"8\"/>\n", nx[i], ny[i] > f
    if (node[i] in hidx) printf "<a href=\"hosts/%s.html\">", esc(node[i]) > f
    printf "<text class=\"node\" x=\"%.1f\" y=\"%.1f\" text-anchor=\"%s\">%s</text>", \
      nx[i] + 14 * cos(a), ny[i] + 14 * sin(a) + (sin(a) > 0.3 ? 12 : sin(a) < -0.3 ? -4 : 4), \
      anchor, esc(node[i]) > f
    if (node[i] in hidx) printf "</a>" > f
    print "" > f
  }
  print "</svg>" > f
  print "<ul class=\"list\">" > f
  for (i = 1; i <= LK; i++)
    printf "<li>%s &rarr; %s%s</li>\n", hostlink(lk[i, 1]), hostlink(lk[i, 2]), \
      (lk[i, 3] != "" ? " <span class=\"muted\">" esc(lk[i, 3]) "</span>" : "") > f
  print "</ul></div>" > f
}

# --- timeline: one lane per host, one mark per change ----------
function timeline_page(f,   i, k, n, w, h, left, right, top, lane, x, y, \
    start, d, any, step, t) {
  w = 960; left = 210; right = 20; top = 30; lane = 30
  h = top + lane * (H > 0 ? H : 1) + 20
  start = T - days
  head(f, "Timeline")
  print "<p class=\"sub\"><a href=\"index.html\">Fleet overview</a></p>" > f
  printf "<h1>Timeline, last %d days</h1>\n", days > f
  printf "<p class=\"sub\">%s%s &middot; %d changes. A dot is a changelog entry, a square an entry with <code>Flags:</code>. Hover for the headline.</p>\n", \
    esc(now), (tag != "" ? " &middot; tag " esc(tag) : ""), RECENT > f
  print "<div class=\"box\">" > f
  printf "<svg class=\"chart\" viewBox=\"0 0 %d %d\" role=\"img\" aria-label=\"Changes per server over time\">\n", \
    w, h > f
  # date ticks: start, three in between, today
  for (t = 0; t <= 4; t++) {
    x = left + (w - left - right) * t / 4
    d = start + int(days * t / 4)
    printf "<line class=\"tick\" x1=\"%.1f\" y1=\"%d\" x2=\"%.1f\" y2=\"%d\"/>\n", \
      x, top - 6, x, h - 20 > f
    printf "<text x=\"%.1f\" y=\"%d\" text-anchor=\"%s\">%s</text>\n", x, top - 12, \
      (t == 0 ? "start" : t == 4 ? "end" : "middle"), dstr(d) > f
  }
  for (k = 1; k <= H; k++) {
    n = ord[k]; y = top + lane * (k - 1) + lane / 2
    printf "<line class=\"lane\" x1=\"%d\" y1=\"%.1f\" x2=\"%d\" y2=\"%.1f\"/>\n", \
      left, y, w - right, y > f
    printf "<a href=\"hosts/%s.html\"><text class=\"host\" x=\"%d\" y=\"%.1f\" text-anchor=\"end\">%s</text></a>\n", \
      esc(hf[n, 1]), left - 10, y + 4, esc(hf[n, 1]) > f
    for (i = 1; i <= C; i++) {
      if (ch[i, 3] != hf[n, 1]) continue
      d = dnum(ch[i, 1])
      if (d < 0 || d < start) continue
      if (d > T) d = T
      x = left + (w - left - right) * (d - start) / (days > 0 ? days : 1)
      if (ch[i, 7] != "")
        printf "<rect class=\"flag\" x=\"%.1f\" y=\"%.1f\" width=\"10\" height=\"10\"><title>%s %s: %s</title></rect>\n", \
          x - 5, y - 5, esc(ch[i, 1]), esc(ch[i, 3]), esc(ch[i, 5]) > f
      else
        printf "<circle class=\"dot\" cx=\"%.1f\" cy=\"%.1f\" r=\"5\"><title>%s %s: %s</title></circle>\n", \
          x, y, esc(ch[i, 1]), esc(ch[i, 3]), esc(ch[i, 5]) > f
    }
  }
  if (!H) printf "<text x=\"%d\" y=\"%d\">No servers in memory/servers/ yet.</text>\n", \
    left, top + lane / 2 > f
  print "</svg></div>" > f
  print "<h2>Changes</h2>\n<div class=\"box\"><ul class=\"list\">" > f
  any = 0
  for (i = 1; i <= C; i++) {
    if (dnum(ch[i, 1]) < 0 || T - dnum(ch[i, 1]) > days + 0) continue
    change_item(f, i, 1); any = 1
  }
  if (!any) print "<li class=\"muted\">No changes recorded in this window.</li>" > f
  print "</ul></div>" > f
  foot(f)
  print "</body>\n</html>" > f
  close(f)
}

# --- json ---------------------------------------------------
function jlist(s, sep,   a, n, i, o) {
  n = split(s, a, sep); o = "["
  for (i = 1; i <= n; i++) if (a[i] != "") o = o (o == "[" ? "" : ", ") jstr(a[i])
  return o "]"
}
function json_out(f,   i, k, n, h, d, m, v, any, c) {
  printf "{\n  \"generated\": %s,\n  \"today\": %s,\n  \"days\": %d,\n  \"tag\": %s,\n", \
    jstr(now), jstr(today), days, jstr(tag) > f
  print "  \"hosts\": [" > f
  for (k = 1; k <= H; k++) {
    i = ord[k]; eol(i)
    printf "    {\"name\": %s, \"os\": %s, \"ip\": %s, \"distro_family\": %s,\n", \
      jstr(hf[i, 1]), jstr(hf[i, 2]), jstr(hf[i, 3]), jstr(hf[i, 4]) > f
    printf "     \"last_connected\": %s, \"last_connected_days\": %d,\n", \
      jstr(hf[i, 5]), agedays(i) > f
    printf "     \"tags\": %s, \"ssh_port\": %s, \"mode\": %s, \"rules\": %s,\n", \
      jlist(hf[i, 6], " "), jstr(hf[i, 7]), jstr(hf[i, 8]), (hf[i, 9] == 1 ? "true" : "false") > f
    printf "     \"todos\": %d, \"last_change\": %s, \"product\": %s, \"version\": %s,\n", \
      hf[i, 10], jstr(hf[i, 11]), jstr(hf[i, 12]), jstr(hf[i, 13]) > f
    printf "     \"owner\": %s, \"aliases\": %s,\n", jstr(hf[i, 14]), jlist(al[hf[i, 1]], " ") > f
    printf "     \"services\": [" > f
    for (n = 1; n <= svn[i]; n++)
      printf "%s{\"name\": %s, \"version\": %s}", (n > 1 ? ", " : ""), \
        jstr(svname[i, n]), jstr(svv[i, n]) > f
    print "]," > f
    printf "     \"eol\": {\"state\": %s, \"text\": %s, \"days\": %d},\n", \
      jstr(E_STATE), jstr(E_TEXT), E_DAYS > f
    printf "     \"audit\": {" > f
    any = 0
    for (n = 1; n <= AK; n++)
      if ((hf[i, 1] SUBSEP akl[n]) in av) {
        printf "%s%s: %s", (any ? ", " : ""), jstr(akl[n]), jstr(av[hf[i, 1], akl[n]]) > f
        any = 1
      }
    printf "}}%s\n", (k < H ? "," : "") > f
  }
  print "  ]," > f
  print "  \"todos\": [" > f
  for (i = 1; i <= TD; i++)
    printf "    {\"host\": %s, \"text\": %s}%s\n", jstr(td[i, 1]), jstr(td[i, 2]), \
      (i < TD ? "," : "") > f
  print "  ]," > f
  print "  \"changes\": [" > f
  for (i = 1; i <= C; i++) {
    printf "    {\"date\": %s, \"time\": %s, \"host\": %s, \"who\": %s,\n", \
      jstr(ch[i, 1]), jstr(ch[i, 2]), jstr(ch[i, 3]), jstr(ch[i, 4]) > f
    printf "     \"headline\": %s, \"rollback\": %s, \"flags\": %s,\n", \
      jstr(ch[i, 5]), jstr(ch[i, 6]), jstr(ch[i, 7]) > f
    printf "     \"detail\": %s}%s\n", jlist(ch[i, 8], "\037"), (i < C ? "," : "") > f
  }
  print "  ]," > f
  printf "  \"network\": [" > f
  for (i = 1; i <= LK; i++)
    printf "%s\n    {\"from\": %s, \"to\": %s, \"label\": %s}", (i > 1 ? "," : ""), \
      jstr(lk[i, 1]), jstr(lk[i, 2]), jstr(lk[i, 3]) > f
  print "]," > f
  printf "  \"audit\": {\"date\": %s, \"previous\": %s, \"drift\": %d, \"changed\": [", \
    jstr(adate), jstr(pdate), DRIFT > f
  for (i = 1; i <= AC; i++) {
    split(acl[i], c, SUBSEP)
    printf "%s\n    {\"host\": %s, \"key\": %s, \"from\": %s, \"to\": %s}", \
      (i > 1 ? "," : ""), jstr(c[1]), jstr(c[2]), jstr(pv[c[1], c[2]]), \
      jstr(av[c[1], c[2]]) > f
  }
  print "]}," > f
  printf "  \"guard\": {\"denials_30d\": %d, \"reasons\": [", GN > f
  for (i = 1; i <= GR; i++)
    printf "%s\n    {\"reason\": %s, \"count\": %d, \"last\": %s}", (i > 1 ? "," : ""), \
      jstr(grl[i]), gcount[grl[i]], jstr(glast[grl[i]]) > f
  print "]}," > f
  printf "  \"lint\": [" > f
  for (i = 1; i <= L; i++) printf "%s\n    %s", (i > 1 ? "," : ""), jstr(lint[i]) > f
  print "]" > f
  print "}" > f
  close(f)
}

# --- prometheus text format ---------------------------------
function prom_out(f,   i, k) {
  print "# HELP jolitz_fleet_hosts Servers in memory/servers/." > f
  print "# TYPE jolitz_fleet_hosts gauge" > f
  printf "jolitz_fleet_hosts %d\n", H > f
  print "# HELP jolitz_host_eol_days Days until the OS release ends support; 0 once ended, -1 when unknown." > f
  print "# TYPE jolitz_host_eol_days gauge" > f
  for (k = 1; k <= H; k++) {
    i = ord[k]; eol(i)
    printf "jolitz_host_eol_days{host=\"%s\"} %d\n", jesc(hf[i, 1]), E_DAYS > f
  }
  print "# HELP jolitz_host_last_connected_days Days since the last connection; -1 when unknown." > f
  print "# TYPE jolitz_host_last_connected_days gauge" > f
  for (k = 1; k <= H; k++) {
    i = ord[k]
    printf "jolitz_host_last_connected_days{host=\"%s\"} %d\n", jesc(hf[i, 1]), agedays(i) > f
  }
  print "# HELP jolitz_host_todos Open to-do items in todo.md." > f
  print "# TYPE jolitz_host_todos gauge" > f
  for (k = 1; k <= H; k++) {
    i = ord[k]
    printf "jolitz_host_todos{host=\"%s\"} %d\n", jesc(hf[i, 1]), hf[i, 10] > f
  }
  print "# HELP jolitz_guard_denials_30d Guard denials in the last 30 days." > f
  print "# TYPE jolitz_guard_denials_30d gauge" > f
  printf "jolitz_guard_denials_30d %d\n", GN > f
  close(f)
}

END {
  compute()
  if (json) json_out(out "/overview.json")
  if (prom) prom_out(out "/jolitz.prom")
  if (mode == "text") { text(); exit }
  index_page(out "/index.html")
  timeline_page(out "/timeline.html")
  for (k = 1; k <= H; k++) host_page(ord[k])
}
