package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"bats/internal/types"

	"google.golang.org/protobuf/proto"
)

// ─── Data Models ────────────────────────────────────────────────────────────

type NodeHealth struct {
	ID       string `json:"id"`
	Port     string `json:"port"`
	Alive    bool   `json:"alive"`
	View     uint64 `json:"view"`
	IsLeader bool   `json:"is_leader"`
	Latency  string `json:"latency"`
}

type DashboardState struct {
	Nodes          map[string]*NodeHealth `json:"nodes"`
	ConsensusLog   []string              `json:"consensus_log"`
	BlockedCounter map[string]int        `json:"blocked_counter"`
	WALEntries     []string              `json:"wal_entries"`
	Uptime         string                `json:"uptime"`
}

var (
	state     DashboardState
	stateMu   sync.RWMutex
	peerList  []string
	tlsClient *http.Client
	startTime = time.Now()
)

func initClient() {
	caCert, err := os.ReadFile("certs/ca.crt")
	pool := x509.NewCertPool()
	if err == nil {
		pool.AppendCertsFromPEM(caCert)
	}

	tlsClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:            pool,
				InsecureSkipVerify: true,
			},
			ForceAttemptHTTP2:   true,
			MaxIdleConnsPerHost: 4,
		},
		Timeout: 1 * time.Second,
	}
}

func main() {
	peersEnv := os.Getenv("PEERS")
	if peersEnv == "" {
		peerList = []string{"localhost:8001", "localhost:8002", "localhost:8003", "localhost:8004"}
	} else {
		peerList = strings.Split(peersEnv, ",")
	}

	state = DashboardState{
		Nodes:          make(map[string]*NodeHealth),
		BlockedCounter: map[string]int{"sql_injection": 0, "shell_exec": 0, "file_access": 0, "other": 0},
	}

	initClient()
	go pollLoop()

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveHTML)
	mux.HandleFunc("/api/state", handleState)
	mux.HandleFunc("/api/validate", handleProxyValidate)

	port := "9000"
	if p := os.Getenv("DASHBOARD_PORT"); p != "" {
		port = p
	}
	fmt.Printf("[BATS-DASHBOARD] Live at http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// ─── Polling ────────────────────────────────────────────────────────────────

func pollLoop() {
	for {
		for _, addr := range peerList {
			go pollNode(addr)
		}
		time.Sleep(1 * time.Second)
	}
}

func pollNode(addr string) {
	start := time.Now()
	resp, err := tlsClient.Get("https://" + addr + "/status")
	lat := time.Since(start)

	stateMu.Lock()
	defer stateMu.Unlock()

	if err != nil {
		if state.Nodes[addr] == nil {
			state.Nodes[addr] = &NodeHealth{Port: addr}
		}
		state.Nodes[addr].Alive = false
		state.Nodes[addr].Latency = "timeout"
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var s types.NodeStatus
	if err := proto.Unmarshal(body, &s); err != nil {
		return
	}

	state.Nodes[addr] = &NodeHealth{
		ID:       s.Id,
		Port:     addr,
		Alive:    true,
		View:     s.View,
		IsLeader: s.IsLeader,
		Latency:  lat.Round(time.Microsecond).String(),
	}
}

// ─── API ────────────────────────────────────────────────────────────────────

func handleState(w http.ResponseWriter, r *http.Request) {
	stateMu.RLock()
	defer stateMu.RUnlock()

	state.Uptime = time.Since(startTime).Round(time.Second).String()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(state)
}

func handleProxyValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}

	body, _ := io.ReadAll(r.Body)
	leader := ""
	for _, addr := range peerList {
		leader = addr
		break
	}

	req, _ := http.NewRequest("POST", "https://"+leader+"/validate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BATS-Nonce", fmt.Sprintf("dash-%d", time.Now().UnixNano()))
	req.Header.Set("X-BATS-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))

	start := time.Now()
	resp, err := tlsClient.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	stateMu.Lock()
	// Track consensus events
	approved, _ := result["approved"].(bool)
	fastPath, _ := result["fast_path"].(bool)
	var action struct{ Action string `json:"action"` }
	json.Unmarshal(body, &action)

	entry := fmt.Sprintf("[%s] %s → ", time.Now().Format("15:04:05"), action.Action)
	if approved && fastPath {
		entry += fmt.Sprintf("FAST_PATH (%v)", elapsed)
	} else if approved {
		entry += fmt.Sprintf("COMMITTED (%v)", elapsed)
	} else {
		entry += fmt.Sprintf("BLOCKED (%v)", elapsed)
		// Categorize block type
		al := strings.ToLower(action.Action)
		switch {
		case strings.Contains(al, "drop") || strings.Contains(al, "delete") || strings.Contains(al, "truncate"):
			state.BlockedCounter["sql_injection"]++
		case strings.Contains(al, "rm ") || strings.Contains(al, "exec") || strings.Contains(al, "bash"):
			state.BlockedCounter["shell_exec"]++
		case strings.Contains(al, "shadow") || strings.Contains(al, "passwd") || strings.Contains(al, "/etc/"):
			state.BlockedCounter["file_access"]++
		default:
			state.BlockedCounter["other"]++
		}
	}

	state.ConsensusLog = append(state.ConsensusLog, entry)
	if len(state.ConsensusLog) > 50 {
		state.ConsensusLog = state.ConsensusLog[len(state.ConsensusLog)-50:]
	}
	stateMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// ─── Embedded HTML Dashboard ────────────────────────────────────────────────

func serveHTML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, dashboardHTML)
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>BATS Control Plane</title>
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;700&family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
<style>
*{margin:0;padding:0;box-sizing:border-box}
:root{
  --bg:#0a0e17;--surface:#111827;--border:#1e293b;
  --text:#e2e8f0;--muted:#64748b;--accent:#3b82f6;
  --green:#10b981;--red:#ef4444;--amber:#f59e0b;--purple:#8b5cf6;
}
body{background:var(--bg);color:var(--text);font-family:'Inter',sans-serif;min-height:100vh}
.header{padding:20px 32px;border-bottom:1px solid var(--border);display:flex;align-items:center;gap:16px;background:var(--surface)}
.header h1{font-size:20px;font-weight:700;letter-spacing:-0.5px}
.header .badge{background:var(--accent);color:#fff;font-size:11px;padding:3px 10px;border-radius:20px;font-weight:600}
.header .uptime{margin-left:auto;color:var(--muted);font-family:'JetBrains Mono',monospace;font-size:13px}
.grid{display:grid;grid-template-columns:1fr 1fr;gap:16px;padding:24px 32px}
.card{background:var(--surface);border:1px solid var(--border);border-radius:12px;padding:20px;min-height:200px}
.card h2{font-size:14px;font-weight:600;color:var(--muted);text-transform:uppercase;letter-spacing:1px;margin-bottom:16px;display:flex;align-items:center;gap:8px}
.card h2 .dot{width:8px;height:8px;border-radius:50%;background:var(--accent);display:inline-block}

/* Nodes */
.nodes-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px}
.node-card{background:var(--bg);border:1px solid var(--border);border-radius:8px;padding:14px;transition:border-color 0.3s}
.node-card.alive{border-color:var(--green)}
.node-card.dead{border-color:var(--red)}
.node-card .id{font-family:'JetBrains Mono',monospace;font-weight:700;font-size:15px}
.node-card .meta{color:var(--muted);font-size:12px;margin-top:6px;font-family:'JetBrains Mono',monospace}
.node-card .leader-badge{background:var(--amber);color:#000;font-size:10px;padding:2px 8px;border-radius:10px;font-weight:700;margin-left:8px}
.node-card .status-dot{width:10px;height:10px;border-radius:50%;display:inline-block;margin-right:6px}
.node-card .status-dot.alive{background:var(--green);box-shadow:0 0 8px var(--green)}
.node-card .status-dot.dead{background:var(--red);box-shadow:0 0 8px var(--red)}

/* Log */
.log{font-family:'JetBrains Mono',monospace;font-size:12px;max-height:280px;overflow-y:auto;line-height:1.8}
.log .entry{padding:2px 0;border-bottom:1px solid rgba(255,255,255,0.03)}
.log .entry.fast{color:var(--green)}
.log .entry.commit{color:var(--accent)}
.log .entry.block{color:var(--red)}

/* Blocked */
.blocked-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px}
.blocked-item{background:var(--bg);border-radius:8px;padding:14px;text-align:center}
.blocked-item .count{font-size:28px;font-weight:700;font-family:'JetBrains Mono',monospace}
.blocked-item .label{font-size:11px;color:var(--muted);margin-top:4px;text-transform:uppercase}
.blocked-item.sql .count{color:var(--red)}
.blocked-item.shell .count{color:var(--amber)}
.blocked-item.file .count{color:var(--purple)}
.blocked-item.other .count{color:var(--muted)}

/* Test panel */
.test-panel{grid-column:1/3;display:flex;gap:12px;align-items:center;padding:0}
.test-panel input{flex:1;background:var(--bg);border:1px solid var(--border);border-radius:8px;padding:12px 16px;color:var(--text);font-family:'JetBrains Mono',monospace;font-size:13px}
.test-panel input:focus{outline:none;border-color:var(--accent)}
.test-panel button{background:var(--accent);color:#fff;border:none;padding:12px 24px;border-radius:8px;font-weight:600;cursor:pointer;font-size:13px;white-space:nowrap;transition:background 0.2s}
.test-panel button:hover{background:#2563eb}
.test-panel .result{font-family:'JetBrains Mono',monospace;font-size:12px;color:var(--muted);min-width:200px}

/* Scrollbar */
::-webkit-scrollbar{width:4px}
::-webkit-scrollbar-track{background:transparent}
::-webkit-scrollbar-thumb{background:var(--border);border-radius:4px}

@media(max-width:900px){.grid{grid-template-columns:1fr}.test-panel{grid-column:1}}
</style>
</head>
<body>
<div class="header">
  <h1>BATS Control Plane</h1>
  <span class="badge">v3.1</span>
  <span class="uptime" id="uptime">--</span>
</div>

<div class="grid">
  <!-- Nodes -->
  <div class="card">
    <h2><span class="dot"></span>Cluster Nodes</h2>
    <div class="nodes-grid" id="nodes"></div>
  </div>

  <!-- Blocked -->
  <div class="card">
    <h2><span class="dot" style="background:var(--red)"></span>Blocked Actions</h2>
    <div class="blocked-grid" id="blocked">
      <div class="blocked-item sql"><div class="count" id="b-sql">0</div><div class="label">SQL Injection</div></div>
      <div class="blocked-item shell"><div class="count" id="b-shell">0</div><div class="label">Shell Exec</div></div>
      <div class="blocked-item file"><div class="count" id="b-file">0</div><div class="label">File Access</div></div>
      <div class="blocked-item other"><div class="count" id="b-other">0</div><div class="label">Other</div></div>
    </div>
  </div>

  <!-- Consensus Log -->
  <div class="card" style="grid-column:1/3">
    <h2><span class="dot" style="background:var(--green)"></span>Consensus Log (live)</h2>
    <div class="log" id="log"></div>
  </div>

  <!-- Test Panel -->
  <div class="card test-panel">
    <input id="action-input" type="text" placeholder='Try: "read user profile" or "DROP TABLE users"' />
    <button onclick="sendAction()">Validate</button>
    <div class="result" id="result"></div>
  </div>
</div>

<script>
function poll(){
  fetch('/api/state').then(r=>r.json()).then(d=>{
    // Uptime
    document.getElementById('uptime').textContent='Uptime: '+d.uptime;

    // Nodes
    const nc=document.getElementById('nodes');
    nc.innerHTML='';
    const sorted=Object.values(d.nodes).sort((a,b)=>a.port.localeCompare(b.port));
    sorted.forEach(n=>{
      const el=document.createElement('div');
      el.className='node-card '+(n.alive?'alive':'dead');
      el.innerHTML='<div class="id"><span class="status-dot '+(n.alive?'alive':'dead')+'"></span>'
        +n.id+(n.is_leader?'<span class="leader-badge">LEADER</span>':'')
        +'</div><div class="meta">'+n.port+' · View '+n.view+' · '+n.latency+'</div>';
      nc.appendChild(el);
    });

    // Blocked
    document.getElementById('b-sql').textContent=d.blocked_counter.sql_injection||0;
    document.getElementById('b-shell').textContent=d.blocked_counter.shell_exec||0;
    document.getElementById('b-file').textContent=d.blocked_counter.file_access||0;
    document.getElementById('b-other').textContent=d.blocked_counter.other||0;

    // Log
    const lg=document.getElementById('log');
    lg.innerHTML='';
    (d.consensus_log||[]).slice().reverse().forEach(e=>{
      const div=document.createElement('div');
      let cls='entry';
      if(e.includes('FAST_PATH'))cls+=' fast';
      else if(e.includes('COMMITTED'))cls+=' commit';
      else if(e.includes('BLOCKED'))cls+=' block';
      div.className=cls;
      div.textContent=e;
      lg.appendChild(div);
    });
  }).catch(()=>{});
}

function sendAction(){
  const inp=document.getElementById('action-input');
  const res=document.getElementById('result');
  if(!inp.value)return;
  res.textContent='Sending...';
  fetch('/api/validate',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:inp.value})})
    .then(r=>r.json())
    .then(d=>{
      if(d.approved)res.innerHTML='<span style="color:var(--green)">✓ APPROVED</span> '+(d.fast_path?'(fast)':'(pbft)');
      else res.innerHTML='<span style="color:var(--red)">✕ BLOCKED</span> '+(d.reason||'');
      poll();
    })
    .catch(e=>res.textContent='Error: '+e);
}

document.getElementById('action-input').addEventListener('keydown',e=>{if(e.key==='Enter')sendAction()});
setInterval(poll,1000);
poll();
</script>
</body>
</html>`
