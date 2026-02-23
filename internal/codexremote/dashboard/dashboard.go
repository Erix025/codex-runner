package dashboard

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"

	"codex-runner/internal/codexremote/config"
	"codex-runner/internal/codexremote/machcheck"
	"codex-runner/internal/codexremote/machineup"
	"codex-runner/internal/shared/jsonutil"
)

type Server struct {
	cfg config.Config

	mu     sync.Mutex
	cache  map[string]machcheck.Status
	expiry time.Duration
}

func New(cfg config.Config) *Server {
	return &Server{
		cfg:    cfg,
		cache:  make(map[string]machcheck.Status),
		expiry: 10 * time.Second,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /api/machines", s.handleMachines)
	mux.HandleFunc("POST /api/machines/{name}/check", s.handleMachineCheck)
	mux.HandleFunc("POST /api/machines/{name}/up", s.handleMachineUp)
	return mux
}

var indexTmpl = template.Must(template.New("index").Parse(`
<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width,initial-scale=1" />
    <title>codex-remote dashboard</title>
    <style>
      body { font-family: ui-sans-serif, system-ui, -apple-system; margin: 24px; }
      table { border-collapse: collapse; width: 100%; }
      th, td { padding: 10px 8px; border-bottom: 1px solid #eee; text-align: left; }
      .pill { display: inline-block; padding: 2px 10px; border-radius: 999px; font-size: 12px; }
      .ok { background: #e8fff0; color: #126a33; }
      .bad { background: #fff0f0; color: #8a1f1f; }
      .unk { background: #f3f4f6; color: #374151; }
      button { padding: 6px 10px; margin-right: 6px; }
      code { background: #f6f8fa; padding: 2px 6px; border-radius: 6px; }
    </style>
  </head>
  <body>
    <h2>Machines</h2>
	    <p>Checks: SSH reachable or direct <code>addr</code>, plus daemon <code>/health</code>.</p>
    <table>
      <thead>
        <tr><th>Name</th><th>SSH</th><th>Daemon</th><th>Latency</th><th>Last Check</th><th>Actions</th></tr>
      </thead>
      <tbody id="rows"></tbody>
    </table>
    <script>
      function pill(ok, textOk, textBad, textUnk) {
        if (ok === true) return '<span class="pill ok">' + textOk + '</span>';
        if (ok === false) return '<span class="pill bad">' + textBad + '</span>';
        return '<span class="pill unk">' + textUnk + '</span>';
      }
      async function refresh() {
        const res = await fetch('/api/machines');
        const data = await res.json();
        const rows = document.getElementById('rows');
        rows.innerHTML = '';
        for (const st of data.machines) {
          const tr = document.createElement('tr');
          const nameEsc = String(st.name).replace(/"/g, '&quot;');
          tr.innerHTML =
            '<td><b>' + nameEsc + '</b></td>' +
            '<td>' + pill(st.ssh_ok, "OK", "DOWN", "N/A") + '</td>' +
            '<td>' + pill(st.daemon_ok, "OK", "DOWN", "N/A") + '</td>' +
            '<td>' + st.latency_ms + ' ms</td>' +
            '<td>' + st.checked_at + '</td>' +
            '<td>' +
              '<button onclick="check(\\'' + nameEsc + '\\')">Check</button>' +
              '<button onclick="up(\\'' + nameEsc + '\\')">Up</button>' +
              '<span style="color:#888">' + (st.error || '') + '</span>' +
            '</td>';
          rows.appendChild(tr);
        }
      }
      async function check(name) {
        await fetch('/api/machines/' + encodeURIComponent(name) + '/check', { method: 'POST' });
        await refresh();
      }
      async function up(name) {
        await fetch('/api/machines/' + encodeURIComponent(name) + '/up', { method: 'POST' });
        await refresh();
      }
      refresh();
      setInterval(refresh, 10000);
    </script>
  </body>
</html>
`))

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	_ = indexTmpl.Execute(w, nil)
}

func (s *Server) handleMachines(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()

	out := make([]machcheck.Status, 0, len(s.cfg.Machines))
	for _, m := range s.cfg.Machines {
		out = append(out, s.getCachedOrCheck(ctx, m))
	}
	_ = jsonutil.WriteJSON(w, map[string]any{"machines": out})
}

func (s *Server) handleMachineCheck(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	m, ok := s.cfg.FindMachine(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	st := machcheck.Check(ctx, *m)
	s.mu.Lock()
	s.cache[name] = st
	s.mu.Unlock()
	_ = jsonutil.WriteJSON(w, st)
}

func (s *Server) handleMachineUp(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	m, ok := s.cfg.FindMachine(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if strings.TrimSpace(m.SSH) == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = jsonutil.WriteJSON(w, map[string]any{"error": "machine.ssh is required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	up := machineup.Start(ctx, *m)
	if !up.OK {
		w.WriteHeader(http.StatusBadGateway)
	}
	_ = json.NewEncoder(w).Encode(up)
}

func (s *Server) getCachedOrCheck(ctx context.Context, m config.Machine) machcheck.Status {
	s.mu.Lock()
	prev, ok := s.cache[m.Name]
	s.mu.Unlock()
	if ok {
		t, err := time.Parse(time.RFC3339Nano, prev.CheckedAt)
		if err == nil && time.Since(t) < s.expiry {
			return prev
		}
	}
	st := machcheck.Check(ctx, m)
	s.mu.Lock()
	s.cache[m.Name] = st
	s.mu.Unlock()
	return st
}
