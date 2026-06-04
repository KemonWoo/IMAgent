package transport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// ringBuffer holds the last N log lines.
type ringBuffer struct {
	mu   sync.Mutex
	buf  []string
	pos  int
	size int
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{buf: make([]string, size), size: size}
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range strings.Split(strings.TrimSpace(string(p)), "\n") {
		if line == "" {
			continue
		}
		r.buf[r.pos%r.size] = line
		r.pos++
	}
	return len(p), nil
}

func (r *ringBuffer) Lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	start := r.pos
	if start > r.size {
		start = r.pos - r.size
	}
	for i := start; i < r.pos; i++ {
		out = append(out, r.buf[i%r.size])
	}
	return out
}

// PanelStatus is the JSON response for /panel/status.
type PanelStatus struct {
	AgentConnected bool     `json:"agent_connected"`
	APKConnected   bool     `json:"apk_connected"`
	PairCode       string   `json:"pair_code"`
	RecentLogs     string   `json:"recent_logs"`
}

// HandlePanelRoot serves the web panel.
func (r *Relay) HandlePanelRoot(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/" {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(panelHTML)
}

// HandlePanelStatus returns relay status as JSON.
func (r *Relay) HandlePanelStatus(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	agentID, pairCode, apkConnected, _ := r.state.GetState()
	status := PanelStatus{
		AgentConnected: agentID != "",
		APKConnected:   apkConnected,
		PairCode:       pairCode,
		RecentLogs:     strings.Join(r.logs.Lines(), "\n"),
	}
	json.NewEncoder(w).Encode(status)
}

// HandlePanelReset resets the relay pairing.
func (r *Relay) HandlePanelReset(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	r.state.SetNexus("", "")
	r.state.SetAPKConnected(false)
	r.sessions.Reset()
	fmt.Fprint(w, `{"ok":true}`)
}

// panelHTML is the embedded management page.
var panelHTML = []byte(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1.0">
<title>IMAgent Relay</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:system-ui,-apple-system,sans-serif;background:#0f0f23;color:#e0e0e0;min-height:100vh;padding:20px;max-width:460px;margin:0 auto}
h1{font-size:22px;margin:16px 0 4px;color:#8b5cf6;text-align:center}
.subtitle{color:#666;font-size:12px;margin-bottom:20px;text-align:center}
.card{background:#1a1a2e;border-radius:16px;padding:20px;margin-bottom:14px}
.card h2{font-size:15px;color:#aaa;margin-bottom:10px}
.row{display:flex;align-items:center;gap:8px;margin:6px 0}
.dot{width:10px;height:10px;border-radius:50%;flex-shrink:0}
.online{background:#22c55e}.offline{background:#ef4444}
.code-box{background:#0f0f23;border:2px dashed #444;border-radius:12px;padding:16px;text-align:center;margin:10px 0}
.code{font-size:32px;font-weight:700;letter-spacing:8px;color:#8b5cf6;font-family:monospace}
#qr{display:flex;justify-content:center;margin:12px 0}
.btn{display:inline-block;padding:10px 20px;border-radius:20px;font-size:13px;text-decoration:none;font-weight:600;margin:3px;border:none;cursor:pointer;color:#fff}
.btn-p{background:#8b5cf6}.btn-s{background:#333}.btn-d{background:#ef4444}
.actions{display:flex;gap:6px;flex-wrap:wrap;margin-top:10px}
.logs{background:#0f0f23;border-radius:8px;padding:10px;max-height:180px;overflow-y:auto;font-size:11px;font-family:monospace;color:#777;white-space:pre-wrap;word-break:break-all}
</style>
</head>
<body>
<h1>&#x1f916; IMAgent Relay</h1>
<div class="subtitle">I'm Agent. I evolve to serve.</div>

<div class="card">
  <h2>&#x1f4e1; 状态</h2>
  <div class="row"><span class="dot" id="ad"></span> Agent: <b id="as">...</b></div>
  <div class="row"><span class="dot" id="pd"></span> APK: <b id="ps">...</b></div>
</div>

<div class="card" id="pc" style="display:none">
  <h2>&#x1f511; 配对码</h2>
  <div class="code-box"><span class="code" id="co">----</span></div>
  <div id="qr"></div>
  <div style="text-align:center;color:#555;font-size:11px">在 APK 输入此码，或扫码</div>
</div>

<div class="card">
  <h2>&#x1f4f1; 下载</h2>
  <a href="/dl/imagent-v1.apk" class="btn btn-p">&#x2b07; APK</a>
  <span style="color:#555;font-size:11px;margin-left:8px">460MB · 全离线</span>
</div>

<div class="card">
  <h2>&#x2699; 操作</h2>
  <div class="actions">
    <button class="btn btn-s" onclick="rf()">&#x1f504; 刷新</button>
    <button class="btn btn-d" onclick="rs()">&#x1f50c; 重置</button>
  </div>
</div>

<div class="card">
  <h2>&#x1f4cb; 日志</h2>
  <div class="logs" id="lg">...</div>
</div>

<script>
async function rf(){
  try{
    const r=await fetch('/panel/status');
    const j=await r.json();
    document.getElementById('as').textContent=j.agent_connected?'已连接':'离线';
    document.getElementById('ad').className='dot '+(j.agent_connected?'online':'offline');
    document.getElementById('ps').textContent=j.apk_connected?'已连接':'离线';
    document.getElementById('pd').className='dot '+(j.apk_connected?'online':'offline');
    if(j.pair_code){document.getElementById('pc').style.display='';document.getElementById('co').textContent=j.pair_code;
      document.getElementById('qr').innerHTML='<img src="https://api.qrserver.com/v1/create-qr-code/?size=160x160&data='+
        encodeURIComponent(location.origin+'?code='+j.pair_code)+'" width=160 height=160 style="border-radius:8px;background:#fff;padding:6px">';
    }else{document.getElementById('pc').style.display='none'}
    document.getElementById('lg').textContent=j.recent_logs||'暂无';
  }catch(e){}
}
async function rs(){if(confirm('重置？APK 将断开。')){await fetch('/panel/reset',{method:'POST'});rf()}}
rf();setInterval(rf,15000);
</script>
</body>
</html>`)
