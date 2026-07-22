package dashboard

// dashboardTemplate is the full HTML dashboard with Go text/template placeholders.
// The five JS constants (ALERT_DATA, SITE_DATA, TL_LABELS, TL_DATA, INCIDENTS) are
// injected as raw JSON by the Go program — no JS changes needed to add new sites or alerts.
const DashboardTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>DSX SRE — NICo Incident Dashboard</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
  <style>
    @import url('https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700;800&family=JetBrains+Mono:wght@400;500&display=swap');
    body { font-family: 'Inter', system-ui, sans-serif; background: #060910; color: #e2e8f0; }
    .gradient-text { background: linear-gradient(135deg, #76b900 0%, #00c4ff 60%, #a78bfa 100%); -webkit-background-clip: text; -webkit-text-fill-color: transparent; background-clip: text; }
    .card { background: rgba(255,255,255,0.035); border: 1px solid rgba(255,255,255,0.07); border-radius: 12px; }
    .kpi-card { background: linear-gradient(135deg, rgba(255,255,255,0.04) 0%, rgba(255,255,255,0.02) 100%); border: 1px solid rgba(255,255,255,0.08); border-radius: 14px; transition: border-color 0.2s; }
    .kpi-card:hover { border-color: rgba(118,185,0,0.3); }
    .site-pill { display: inline-flex; align-items: center; font-size: 11px; font-weight: 600; padding: 2px 8px; border-radius: 99px; font-family: 'JetBrains Mono', monospace; }
    .badge { font-size: 10px; font-weight: 700; padding: 2px 7px; border-radius: 4px; text-transform: uppercase; letter-spacing: 0.05em; }
    .urgency-high { background: rgba(239,68,68,0.15); color: #fca5a5; border: 1px solid rgba(239,68,68,0.25); }
    .urgency-low  { background: rgba(251,191,36,0.12); color: #fde68a; border: 1px solid rgba(251,191,36,0.2); }
    .status-open  { background: rgba(239,68,68,0.12); color: #f87171; border: 1px solid rgba(239,68,68,0.2); }
    .status-ack   { background: rgba(251,191,36,0.12); color: #fbbf24; border: 1px solid rgba(251,191,36,0.2); }
    .status-res   { background: rgba(118,185,0,0.12); color: #84cc16; border: 1px solid rgba(118,185,0,0.2); }
    table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
    thead th { background: rgba(255,255,255,0.04); padding: 10px 14px; text-align: left; font-weight: 600; font-size: 11px; text-transform: uppercase; letter-spacing: 0.06em; color: #94a3b8; border-bottom: 1px solid rgba(255,255,255,0.07); white-space: nowrap; }
    tbody tr { border-bottom: 1px solid rgba(255,255,255,0.04); transition: background 0.1s; }
    tbody tr:hover { background: rgba(255,255,255,0.03); }
    tbody td { padding: 9px 14px; vertical-align: middle; }
    .mono { font-family: 'JetBrains Mono', monospace; }
    .active-dot { width: 6px; height: 6px; border-radius: 50%; background: #ef4444; display: inline-block; animation: pulse-dot 1.4s infinite; }
    @keyframes pulse-dot { 0%,100%{opacity:1} 50%{opacity:.3} }
    input[type=text] { background: rgba(255,255,255,0.05); border: 1px solid rgba(255,255,255,0.1); color: #e2e8f0; border-radius: 8px; padding: 7px 14px; font-size: 13px; outline: none; }
    input[type=text]:focus { border-color: rgba(118,185,0,0.4); }
    select { background: rgba(20,20,30,0.95); border: 1px solid rgba(255,255,255,0.1); color: #e2e8f0; border-radius: 8px; padding: 7px 10px; font-size: 13px; outline: none; cursor: pointer; }
    ::-webkit-scrollbar { width: 6px; height: 6px; }
    ::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.12); border-radius: 3px; }
  </style>
</head>
<body>
<div class="min-h-screen px-6 py-5 max-w-[1600px] mx-auto">

  <header class="flex items-center justify-between mb-8 gap-4 flex-wrap">
    <div class="flex items-center gap-4">
      <div class="w-10 h-10 rounded-xl flex items-center justify-center font-black" style="background:linear-gradient(135deg,#76b900,#00c4ff)">
        <span style="-webkit-text-fill-color:#000;font-size:9px;letter-spacing:-.5px;font-weight:900">DSX</span>
      </div>
      <div>
        <h1 class="text-xl font-bold tracking-tight gradient-text">NICo Incident Dashboard</h1>
        <p class="text-xs text-slate-500 mt-0.5">DSX SRE &middot; Team {{.TeamID}} &middot; {{.WindowStart}} &ndash; {{.WindowEnd}} &middot; Scoped to <code class="mono text-slate-400">forge_site</code> alerts</p>
      </div>
    </div>
    <div class="flex items-center gap-3 text-xs text-slate-500">
      <span id="activeBadge" class="flex items-center gap-1.5"></span>
      <span class="text-slate-600">&middot;</span>
      <span>Fetched {{.FetchedAt}}</span>
      {{if gt (len .Siblings) 1}}
      <span class="text-slate-600">&middot;</span>
      <select onchange="if(this.value)window.location.href=this.value" title="Switch window">
        {{range .Siblings}}<option value="{{.Filename}}"{{if eq .Filename $.CurrentFile}} selected{{end}}>{{.Label}}</option>
        {{end}}
      </select>
      {{end}}
      <a href="https://nvidia.pagerduty.com/teams/{{.TeamID}}/services" target="_blank"
         class="ml-2 px-3 py-1.5 rounded-lg text-xs font-semibold border border-slate-700 text-slate-300 hover:border-lime-600 hover:text-lime-400 transition-colors">
        Open PagerDuty &rarr;
      </a>
    </div>
  </header>

  <div class="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3 mb-6">
    <div class="kpi-card p-4">
      <div class="text-xs text-slate-500 font-semibold uppercase tracking-widest mb-1.5">Total</div>
      <div class="text-3xl font-black text-white">{{.TotalIncidents}}</div>
      <div class="text-xs text-slate-600 mt-1">all incidents</div>
    </div>
    <div class="kpi-card p-4">
      <div class="text-xs text-slate-500 font-semibold uppercase tracking-widest mb-1.5">NICo Alerts</div>
      <div class="text-3xl font-black" style="color:#76b900">{{.ForgeCount}}</div>
      <div class="text-xs text-slate-600 mt-1">forge_site label</div>
    </div>
    <div class="kpi-card p-4">
      <div class="text-xs text-slate-500 font-semibold uppercase tracking-widest mb-1.5">Active Now</div>
      <div class="text-3xl font-black text-red-400">{{.ActiveCount}}</div>
      <div class="text-xs text-slate-600 mt-1">triggered/ack'd</div>
    </div>
    <div class="kpi-card p-4">
      <div class="text-xs text-slate-500 font-semibold uppercase tracking-widest mb-1.5">Sites</div>
      <div class="text-3xl font-black text-sky-400">{{.SiteCount}}</div>
      <div class="text-xs text-slate-600 mt-1">NICo sites</div>
    </div>
    <div class="kpi-card p-4">
      <div class="text-xs text-slate-500 font-semibold uppercase tracking-widest mb-1.5">Alert Types</div>
      <div class="text-3xl font-black text-violet-400">{{.AlertTypeCount}}</div>
      <div class="text-xs text-slate-600 mt-1">unique alertnames</div>
    </div>
    <div class="kpi-card p-4">
      <div class="text-xs text-slate-500 font-semibold uppercase tracking-widest mb-1.5">Peak Day</div>
      <div class="text-3xl font-black text-amber-400">{{.PeakCount}}</div>
      <div class="text-xs text-slate-600 mt-1">{{.PeakDay}}</div>
    </div>
  </div>

  <div class="grid grid-cols-1 lg:grid-cols-3 gap-4 mb-4">
    <div class="lg:col-span-2 card p-5">
      <h2 class="text-sm font-semibold text-slate-300 mb-4">Incidents by Alert Type <span class="text-slate-500 font-normal text-xs">— top 12</span></h2>
      <div style="position:relative;height:280px"><canvas id="alertChart"></canvas></div>
    </div>
    <div class="card p-5">
      <h2 class="text-sm font-semibold text-slate-300 mb-4">Distribution by Site</h2>
      <div style="position:relative;height:240px"><canvas id="siteChart"></canvas></div>
    </div>
  </div>

  <div class="card p-5 mb-4">
    <h2 class="text-sm font-semibold text-slate-300 mb-4">Incident Timeline <span class="text-slate-500 font-normal text-xs">— NICo alerts per day</span></h2>
    <div style="position:relative;height:180px"><canvas id="timelineChart"></canvas></div>
  </div>

  <div class="card p-5 mb-4">
    <h2 class="text-sm font-semibold text-slate-300 mb-4">Alert Type Breakdown <span class="text-slate-500 font-normal text-xs">— grouped by alertname label</span></h2>
    <div class="overflow-x-auto">
      <table>
        <thead><tr>
          <th>Alert Name</th><th>Count</th><th>Sites</th><th>Active</th><th>Category</th><th>% of Total</th>
        </tr></thead>
        <tbody id="alertTable"></tbody>
      </table>
    </div>
  </div>

  <div class="card p-5">
    <div class="flex flex-wrap items-center justify-between gap-3 mb-4">
      <h2 class="text-sm font-semibold text-slate-300">Incident Log</h2>
      <div class="flex gap-2 flex-wrap">
        <input type="text" id="searchInput" placeholder="Filter by alertname, site, status…" style="width:250px">
        <select id="siteFilter" onchange="PAGE=0;renderTable()"><option value="">All Sites</option></select>
        <select id="statusFilter" onchange="PAGE=0;renderTable()">
          <option value="">All Statuses</option>
          <option value="triggered">Triggered</option>
          <option value="acknowledged">Acknowledged</option>
          <option value="resolved">Resolved</option>
        </select>
      </div>
    </div>
    <div class="overflow-x-auto">
      <table>
        <thead><tr>
          <th>#</th><th>Alert Name</th><th>Site</th><th>Service</th><th>Status</th><th>Urgency</th><th>Date</th><th>TTR</th><th></th>
        </tr></thead>
        <tbody id="incidentTable"></tbody>
      </table>
      <div id="tablePager" class="flex items-center justify-between mt-3 text-xs text-slate-500 px-1"></div>
    </div>
  </div>

  <footer class="text-center mt-8 text-xs text-slate-700 pb-4">
    DSX SRE &middot; PagerDuty team {{.TeamID}} &middot; Generated {{.FetchedAt}} &middot;
    <a href="https://nvidia.pagerduty.com/analytics/insights/" class="hover:text-slate-400">PD Analytics</a>
  </footer>
</div>

<script>
// ── Data injected by Go ───────────────────────────────────────────────────────
const ALERT_DATA = {{.AlertsJSON}};
const SITE_DATA  = {{.SiteDataJSON}};
const TL_LABELS  = {{.TLLabelsJSON}};
const TL_DATA    = {{.TLCountsJSON}};
const INCIDENTS  = {{.IncidentsJSON}};

// ── Site colour palette ───────────────────────────────────────────────────────
const SITE_COLORS = {
  az:'#38bdf8', pdxlab:'#a78bfa', ytl:'#84cc16',
  pdx:'#fbbf24', tlv:'#fb923c', hfa:'#f472b6', tpe:'#2dd4bf',
};
function siteColor(s) { return SITE_COLORS[s] || '#64748b'; }

// ── Build site filter dropdown from data ──────────────────────────────────────
(function() {
  const sel = document.getElementById('siteFilter');
  Object.keys(SITE_DATA).sort((a,b) => SITE_DATA[b]-SITE_DATA[a]).forEach(s => {
    const o = document.createElement('option');
    o.value = s; o.textContent = s;
    sel.appendChild(o);
  });
})();

// ── Active badge ──────────────────────────────────────────────────────────────
(function() {
  const active = INCIDENTS.filter(i => i.status === 'triggered' || i.status === 'acknowledged').length;
  const el = document.getElementById('activeBadge');
  if (active > 0) {
    el.innerHTML = '<span style="width:6px;height:6px;border-radius:50%;background:#ef4444;display:inline-block;animation:pulse-dot 1.4s infinite"></span>'
                 + '<span style="color:#f87171;font-weight:600">' + active + ' active</span>';
  }
})();

// ── Chart defaults ────────────────────────────────────────────────────────────
Chart.defaults.color = '#94a3b8';
Chart.defaults.font  = {family:"'Inter',system-ui,sans-serif", size:11};

// Alert frequency bar
(function(){
  const top = ALERT_DATA.slice(0, 12);
  const catC = {'Hardware':'#38bdf8','NICo Health':'#84cc16','Carbide':'#a78bfa','Kubernetes':'#fb923c','Other':'#64748b'};
  new Chart(document.getElementById('alertChart').getContext('2d'), {
    type: 'bar',
    data: {
      labels: top.map(a => a.name),
      datasets: [{
        data: top.map(a => a.count),
        backgroundColor: top.map(a => (catC[a.cat]||'#64748b') + '44'),
        borderColor:     top.map(a =>  catC[a.cat]||'#64748b'),
        borderWidth: 1.5, borderRadius: 4,
      }]
    },
    options: {
      indexAxis: 'y', responsive: true, maintainAspectRatio: false,
      plugins: { legend: {display:false}, tooltip: {callbacks:{label:c=>' ' + c.raw + ' incidents'}} },
      scales: {
        x: { grid:{color:'rgba(255,255,255,0.04)'}, ticks:{color:'#64748b'} },
        y: { grid:{display:false}, ticks:{color:'#94a3b8', font:{size:10.5,family:"'JetBrains Mono',monospace"}} }
      }
    }
  });
})();

// Site doughnut
(function(){
  const sites = Object.keys(SITE_DATA);
  new Chart(document.getElementById('siteChart').getContext('2d'), {
    type: 'doughnut',
    data: {
      labels: sites,
      datasets: [{
        data: sites.map(s => SITE_DATA[s]),
        backgroundColor: sites.map(s => siteColor(s) + '88'),
        borderColor:     sites.map(s => siteColor(s)),
        borderWidth: 1.5, hoverOffset: 6,
      }]
    },
    options: {
      responsive: true, maintainAspectRatio: false, cutout: '65%',
      plugins: {
        legend: {position:'right', labels:{boxWidth:10, boxHeight:10, padding:12}},
        tooltip: {callbacks:{label:c=>' ' + c.label + ': ' + c.raw + ' incidents'}}
      }
    }
  });
})();

// Timeline
(function(){
  new Chart(document.getElementById('timelineChart').getContext('2d'), {
    type: 'line',
    data: {
      labels: TL_LABELS,
      datasets: [{
        data: TL_DATA,
        borderColor: '#76b900', backgroundColor: 'rgba(118,185,0,0.08)',
        tension: 0.35, fill: true,
        pointRadius: 3, pointBackgroundColor: '#76b900', borderWidth: 2,
      }]
    },
    options: {
      responsive: true, maintainAspectRatio: false,
      plugins: {legend:{display:false}, tooltip:{callbacks:{label:c=>' ' + c.raw + ' incidents'}}},
      scales: {
        x: {grid:{color:'rgba(255,255,255,0.04)'}, ticks:{color:'#64748b', maxRotation:0}},
        y: {grid:{color:'rgba(255,255,255,0.04)'}, ticks:{color:'#64748b'}, beginAtZero:true}
      }
    }
  });
})();

// Alert breakdown table
(function(){
  const tot = ALERT_DATA.reduce((s,a) => s + a.count, 0);
  const catC = {'Hardware':'#38bdf8','NICo Health':'#84cc16','Carbide':'#a78bfa','Kubernetes':'#fb923c','Other':'#64748b'};
  document.getElementById('alertTable').innerHTML = ALERT_DATA.map(a => {
    const pct  = Math.round(a.count / tot * 100);
    const col  = catC[a.cat] || '#64748b';
    const siteHtml = a.sites.map(s =>
      '<span class="site-pill" style="background:'+siteColor(s)+'22;color:'+siteColor(s)+';border:1px solid '+siteColor(s)+'44">'+s+'</span>'
    ).join(' ');
    const activeDot = a.active > 0
      ? '<span class="active-dot mr-1.5"></span><span style="color:#f87171;font-weight:600">'+a.active+'</span>'
      : '<span style="color:#334155">—</span>';
    return '<tr>'
      + '<td class="mono text-slate-200 font-medium">'+a.name+'</td>'
      + '<td class="text-white font-bold text-base">'+a.count+'</td>'
      + '<td><div class="flex flex-wrap gap-1 py-1">'+siteHtml+'</div></td>'
      + '<td>'+activeDot+'</td>'
      + '<td><span class="badge" style="background:'+col+'22;color:'+col+';border:1px solid '+col+'44">'+a.cat+'</span></td>'
      + '<td><div class="flex items-center gap-2">'
      +   '<div style="width:80px;height:6px;border-radius:3px;background:rgba(255,255,255,0.06);overflow:hidden">'
      +     '<div style="height:100%;width:'+pct+'%;background:linear-gradient(90deg,#76b900,#00c4ff);border-radius:3px"></div>'
      +   '</div>'
      +   '<span style="color:#64748b;font-size:11px">'+pct+'%</span>'
      + '</div></td>'
      + '</tr>';
  }).join('');
})();

// Incident log
let PAGE = 0;
const PAGE_SIZE = 25;

function fmtTTR(s) {
  if (s == null) return '—';
  if (s < 3600)  return Math.round(s/60)+'m';
  if (s < 86400) return Math.round(s/3600)+'h';
  return Math.round(s/86400)+'d';
}

function getFiltered() {
  const q      = document.getElementById('searchInput').value.toLowerCase();
  const site   = document.getElementById('siteFilter').value;
  const status = document.getElementById('statusFilter').value;
  return INCIDENTS.filter(i => {
    const hit = !q || i.alert.toLowerCase().includes(q) || i.site.includes(q)
              || i.status.includes(q) || i.num.includes(q) || i.svc.includes(q);
    return hit && (!site || i.site === site) && (!status || i.status === status);
  });
}

function renderTable() {
  const filtered = getFiltered();
  const page     = filtered.slice(PAGE * PAGE_SIZE, (PAGE+1) * PAGE_SIZE);
  const sc = {triggered:'status-open', acknowledged:'status-ack', resolved:'status-res'};

  document.getElementById('incidentTable').innerHTML = page.map(i => {
    const col = siteColor(i.site);
    return '<tr>'
      + '<td class="mono text-slate-500 text-xs">#'+i.num+'</td>'
      + '<td class="mono text-slate-200 text-xs font-medium" style="max-width:280px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">'+i.alert+'</td>'
      + '<td><span class="site-pill" style="background:'+col+'22;color:'+col+';border:1px solid '+col+'44">'+i.site+'</span></td>'
      + '<td class="text-slate-500 text-xs">'+i.svc+'</td>'
      + '<td><span class="badge '+(sc[i.status]||'')+'">'+i.status+'</span></td>'
      + '<td><span class="badge '+(i.urg==='high'?'urgency-high':'urgency-low')+'">'+i.urg+'</span></td>'
      + '<td class="text-slate-400 text-xs mono">'+i.date+'</td>'
      + '<td class="text-slate-400 text-xs mono">'+fmtTTR(i.ttr)+'</td>'
      + '<td><a href="https://nvidia.pagerduty.com/incidents/'+i.num+'" target="_blank" style="color:#334155;font-size:12px" onmouseover="this.style.color=\'#84cc16\'" onmouseout="this.style.color=\'#334155\'">&#8599;</a></td>'
      + '</tr>';
  }).join('');

  const s = PAGE*PAGE_SIZE+1, e = Math.min((PAGE+1)*PAGE_SIZE, filtered.length);
  document.getElementById('tablePager').innerHTML =
    '<span>'+filtered.length+' incidents &middot; showing '+s+'&ndash;'+e+'</span>'
    + '<div style="display:flex;gap:8px">'
    + (PAGE > 0 ? '<button onclick="changePage(-1)" style="padding:4px 12px;border:1px solid #334155;border-radius:6px;color:#94a3b8;background:none;cursor:pointer">&#8592; Prev</button>' : '')
    + (e < filtered.length ? '<button onclick="changePage(1)" style="padding:4px 12px;border:1px solid #334155;border-radius:6px;color:#94a3b8;background:none;cursor:pointer">Next &#8594;</button>' : '')
    + '</div>';
}

function changePage(d) { PAGE = Math.max(0, PAGE+d); renderTable(); }
document.getElementById('searchInput').addEventListener('input', () => { PAGE=0; renderTable(); });
renderTable();
</script>
</body>
</html>`
