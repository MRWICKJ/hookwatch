import { useState, useEffect, useCallback, useRef } from "react";

interface CapturedRequest {
  id: string;
  received_at: string;
  method: string;
  path: string;
  remote_addr: string;
  headers: Record<string, string[]>;
  query_params: Record<string, string[]>;
  body: any;
  size_bytes: number;
}

type MethodFilter = "ALL" | "GET" | "POST" | "PUT" | "DELETE" | "PATCH";

const WS_URL = `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/ws`;
const API_BASE = "/api";

function methodColor(method: string): string {
  switch (method) {
    case "POST":   return "bg-emerald-500/10 text-emerald-400 border-emerald-500/20";
    case "GET":    return "bg-sky-500/10 text-sky-400 border-sky-500/20";
    case "PUT":    return "bg-amber-500/10 text-amber-400 border-amber-500/20";
    case "DELETE": return "bg-rose-500/10 text-rose-400 border-rose-500/20";
    case "PATCH":  return "bg-violet-500/10 text-violet-400 border-violet-500/20";
    default:       return "bg-zinc-500/10 text-zinc-400 border-zinc-500/20";
  }
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
}

function buildCurl(req: CapturedRequest): string {
  let curl = `curl -X ${req.method} 'http://localhost:8877${req.path}'`;
  for (const [key, vals] of Object.entries(req.headers)) {
    if (key.toLowerCase() === "host" || key.toLowerCase() === "content-length") continue;
    for (const v of vals) {
      curl += ` \\\n  -H '${key}: ${v}'`;
    }
  }
  if (req.body && typeof req.body === "string") {
    curl += ` \\\n  -d '${req.body.replace(/'/g, "\\'")}'`;
  } else if (req.body && typeof req.body === "object") {
    curl += ` \\\n  -d '${JSON.stringify(req.body).replace(/'/g, "\\'")}'`;
  }
  return curl;
}

const METHODS: MethodFilter[] = ["ALL", "GET", "POST", "PUT", "DELETE", "PATCH"];

export default function App() {
  const [requests, setRequests] = useState<CapturedRequest[]>([]);
  const [selected, setSelected] = useState<CapturedRequest | null>(null);
  const [connected, setConnected] = useState(false);
  const [search, setSearch] = useState("");
  const [methodFilter, setMethodFilter] = useState<MethodFilter>("ALL");
  const [paused, setPaused] = useState(false);
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectRef = useRef<number>(0);

  const updateRequestsRef = useRef<(incoming: CapturedRequest) => void>(() => {});
  const updateRequests = useCallback((incoming: CapturedRequest) => {
    if (paused) return;
    setRequests(prev => {
      const next = [incoming, ...prev];
      return next.length > 1000 ? next.slice(0, 1000) : next;
    });
    setSelected(current => current || incoming);
  }, [paused]);
  updateRequestsRef.current = updateRequests;

  useEffect(() => {
    let cancelled = false;
    let ws: WebSocket | null = null;

    function connect() {
      if (cancelled) return;
      ws = new WebSocket(WS_URL);
      wsRef.current = ws;

      ws.onopen = () => {
        if (cancelled) { ws?.close(); return; }
        setConnected(true);
        reconnectRef.current = 0;
      };

      ws.onclose = () => {
        if (cancelled) return;
        setConnected(false);
        const delay = Math.min(1000 * Math.pow(2, reconnectRef.current), 30000);
        reconnectRef.current++;
        setTimeout(connect, delay);
      };

      ws.onmessage = (event) => {
        if (cancelled) return;
        try {
          const incoming: CapturedRequest = JSON.parse(event.data);
          updateRequestsRef.current(incoming);
        } catch { /* ignore parse errors */ }
      };
    }

    fetch(`${API_BASE}/requests`)
      .then(res => res.json())
      .then((data: CapturedRequest[]) => {
        if (cancelled) return;
        setRequests(data.reverse());
        if (data.length > 0) setSelected(data[data.length - 1]);
      })
      .catch(() => {});

    connect();
    return () => {
      cancelled = true;
      ws?.close();
    };
  }, []);

  const clearRequests = useCallback(async () => {
    await fetch(`${API_BASE}/clear`, { method: "POST" });
    setRequests([]);
    setSelected(null);
  }, []);

  const copyCurl = useCallback(async (req: CapturedRequest) => {
    const curl = buildCurl(req);
    await navigator.clipboard.writeText(curl);
    setCopiedId(req.id);
    setTimeout(() => setCopiedId(null), 2000);
  }, []);

  const filtered = requests.filter(req => {
    if (methodFilter !== "ALL" && req.method !== methodFilter) return false;
    if (search && !req.path.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

  return (
    <div className="flex flex-col h-screen bg-[#09090b] text-zinc-100 antialiased">
      <header className="flex items-center justify-between px-5 py-3 border-b border-zinc-800/60 bg-[#09090b] shrink-0">
        <div className="flex items-center gap-3">
          <h1 className="text-lg font-bold tracking-tight text-zinc-200">HookWatch</h1>
          <span className="text-[10px] font-mono text-zinc-600 bg-zinc-900 px-2 py-0.5 rounded">v1.0</span>
        </div>
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-1.5">
            <span className={`w-2 h-2 rounded-full ${connected ? "bg-emerald-500" : "bg-rose-500"}`} />
            <span className="text-xs font-mono text-zinc-500 min-w-[80px]">{connected ? "Connected" : "Reconnecting..."}</span>
          </div>
          <span className="text-xs font-mono text-zinc-600">{requests.length} captured</span>
        </div>
      </header>

      <div className="flex flex-1 overflow-hidden">
        <aside className="w-2/5 border-r border-zinc-800/60 bg-[#0c0c0e] flex flex-col min-w-0">
          <div className="p-3 border-b border-zinc-800/40 space-y-2">
            <div className="flex items-center gap-2">
              <input
                type="text"
                placeholder="Search paths..."
                value={search}
                onChange={e => setSearch(e.target.value)}
                className="flex-1 bg-zinc-900 border border-zinc-800 rounded px-2.5 py-1.5 text-xs font-mono text-zinc-300 placeholder-zinc-600 outline-none focus:border-zinc-600 transition-colors"
              />
              <button
                onClick={() => setPaused(p => !p)}
                className={`px-2.5 py-1.5 rounded text-xs font-mono transition-colors ${paused ? "bg-amber-500/10 text-amber-400 border border-amber-500/20" : "bg-zinc-900 text-zinc-500 border border-zinc-800 hover:border-zinc-600"}`}
                title={paused ? "Resume live stream" : "Pause live stream"}
              >
                {paused ? "▶" : "⏸"}
              </button>
              <button
                onClick={clearRequests}
                className="px-2.5 py-1.5 rounded text-xs font-mono text-zinc-500 bg-zinc-900 border border-zinc-800 hover:border-zinc-600 hover:text-zinc-400 transition-colors"
              >
                ✕
              </button>
            </div>
            <div className="flex gap-1">
              {METHODS.map(m => (
                <button
                  key={m}
                  onClick={() => setMethodFilter(m)}
                  className={`px-2 py-0.5 rounded text-[10px] font-mono transition-colors ${
                    methodFilter === m
                      ? "bg-zinc-200 text-zinc-900 font-semibold"
                      : "text-zinc-500 hover:text-zinc-300 bg-zinc-900/50"
                  }`}
                >
                  {m === "ALL" ? `ALL (${requests.length})` : `${m}`}
                </button>
              ))}
            </div>
          </div>

          <div className="flex-1 overflow-y-auto">
            {filtered.length === 0 ? (
              <div className="flex flex-col items-center justify-center h-full text-center p-8 text-zinc-600">
                <div className="w-8 h-8 rounded-full border border-dashed border-zinc-700 animate-spin mb-4" />
                <p className="text-sm text-zinc-500">Waiting for requests...</p>
                <p className="text-xs mt-1 font-mono">Send traffic to <span className="text-zinc-400">localhost:8877</span></p>
              </div>
            ) : (
              filtered.map((req) => (
                <button
                  key={req.id}
                  onClick={() => setSelected(req)}
                  className={`w-full text-left px-4 py-3 transition-colors flex flex-col gap-1.5 border-b border-zinc-800/20 ${
                    selected?.id === req.id ? "bg-zinc-900/80 border-l-[3px] border-l-zinc-300" : "hover:bg-zinc-900/30 border-l-[3px] border-l-transparent"
                  }`}
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2 min-w-0">
                      <span className={`px-1.5 py-0.5 text-[10px] font-bold font-mono rounded border ${methodColor(req.method)}`}>
                        {req.method}
                      </span>
                      <span className="font-mono text-xs text-zinc-300 truncate">{req.path}</span>
                    </div>
                    <span className="font-mono text-[10px] text-zinc-600 shrink-0">
                      {new Date(req.received_at).toLocaleTimeString()}
                    </span>
                  </div>
                  <div className="flex items-center justify-between text-[10px] font-mono text-zinc-600">
                    <span className="truncate">{req.id.slice(0, 8)}</span>
                    <span>{formatBytes(req.size_bytes)}</span>
                  </div>
                </button>
              ))
            )}
            <div ref={bottomRef} />
          </div>
        </aside>

        <main className="flex-1 bg-[#09090b] overflow-y-auto p-6">
          {selected ? (
            <div className="max-w-3xl space-y-6">
              <div className="bg-zinc-900/30 border border-zinc-800/60 rounded-xl p-5">
                <div className="flex items-start justify-between">
                  <div>
                    <span className="text-[10px] font-mono text-zinc-600 uppercase tracking-wider">Route</span>
                    <div className="flex items-center gap-2 mt-1">
                      <span className={`px-2 py-0.5 text-xs font-bold font-mono rounded border ${methodColor(selected.method)}`}>
                        {selected.method}
                      </span>
                      <span className="font-mono text-base text-zinc-200">{selected.path}</span>
                    </div>
                  </div>
                  <button
                    onClick={() => copyCurl(selected)}
                    className="shrink-0 px-3 py-1.5 rounded text-xs font-mono bg-zinc-900 border border-zinc-800 text-zinc-400 hover:text-zinc-200 hover:border-zinc-600 transition-colors"
                  >
                    {copiedId === selected.id ? "Copied!" : "Copy as cURL"}
                  </button>
                </div>

                <div className="grid grid-cols-3 gap-4 mt-4 text-xs font-mono">
                  <div>
                    <span className="text-zinc-600 block text-[10px] uppercase tracking-wider">ID</span>
                    <span className="text-zinc-400 mt-0.5 block select-all text-[11px]">{selected.id}</span>
                  </div>
                  <div>
                    <span className="text-zinc-600 block text-[10px] uppercase tracking-wider">Timestamp</span>
                    <span className="text-zinc-400 mt-0.5 block text-[11px]">{new Date(selected.received_at).toISOString()}</span>
                  </div>
                  <div>
                    <span className="text-zinc-600 block text-[10px] uppercase tracking-wider">Size</span>
                    <span className="text-zinc-400 mt-0.5 block text-[11px]">{formatBytes(selected.size_bytes)}</span>
                  </div>
                  <div>
                    <span className="text-zinc-600 block text-[10px] uppercase tracking-wider">Remote</span>
                    <span className="text-zinc-400 mt-0.5 block text-[11px] font-mono">{selected.remote_addr}</span>
                  </div>
                  <div>
                    <span className="text-zinc-600 block text-[10px] uppercase tracking-wider">Query Params</span>
                    <span className="text-zinc-400 mt-0.5 block text-[11px]">{Object.keys(selected.query_params).length}</span>
                  </div>
                  <div>
                    <span className="text-zinc-600 block text-[10px] uppercase tracking-wider">Headers</span>
                    <span className="text-zinc-400 mt-0.5 block text-[11px]">{Object.keys(selected.headers).length}</span>
                  </div>
                </div>
              </div>

              {Object.keys(selected.query_params).length > 0 && (
                <section>
                  <h3 className="text-[10px] font-mono tracking-widest text-zinc-600 uppercase mb-2">Query Parameters</h3>
                  <div className="bg-zinc-950 border border-zinc-800/60 rounded-xl overflow-hidden">
                    <table className="w-full text-left font-mono text-xs border-collapse">
                      <thead>
                        <tr className="bg-zinc-900/40 border-b border-zinc-800/60 text-zinc-600 text-[10px] uppercase tracking-wider">
                          <th className="p-3 w-1/3 font-medium">Key</th>
                          <th className="p-3 font-medium">Value</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-zinc-800/40">
                        {Object.entries(selected.query_params).map(([key, vals]) => (
                          <tr key={key} className="hover:bg-zinc-900/20">
                            <td className="p-3 text-zinc-400 font-semibold select-all">{key}</td>
                            <td className="p-3 text-zinc-300 select-all">{vals.join(", ")}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </section>
              )}

              <section>
                <h3 className="text-[10px] font-mono tracking-widest text-zinc-600 uppercase mb-2">Headers</h3>
                <div className="bg-zinc-950 border border-zinc-800/60 rounded-xl overflow-hidden">
                  <table className="w-full text-left font-mono text-xs border-collapse">
                    <thead>
                      <tr className="bg-zinc-900/40 border-b border-zinc-800/60 text-zinc-600 text-[10px] uppercase tracking-wider">
                        <th className="p-3 w-1/3 font-medium">Header</th>
                        <th className="p-3 font-medium">Value</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-zinc-800/40">
                      {Object.entries(selected.headers).map(([key, vals]) => (
                        <tr key={key} className="hover:bg-zinc-900/20">
                          <td className="p-3 text-zinc-400 font-semibold select-all text-[11px]">{key}</td>
                          <td className="p-3 text-zinc-300 select-all break-all text-[11px]">{vals.join(", ")}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </section>

              <section>
                <h3 className="text-[10px] font-mono tracking-widest text-zinc-600 uppercase mb-2">Body</h3>
                <div className="bg-zinc-950 border border-zinc-800/60 rounded-xl overflow-hidden">
                  <pre className="p-4 text-xs font-mono text-zinc-300 leading-relaxed select-all overflow-x-auto">
                    {typeof selected.body === "string"
                      ? selected.body
                      : JSON.stringify(selected.body, null, 2)}
                  </pre>
                </div>
              </section>
            </div>
          ) : (
            <div className="flex flex-col items-center justify-center h-full text-zinc-600">
              <svg className="w-12 h-12 mb-4 text-zinc-700" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1} d="M13 10V3L4 14h7v7l9-11h-7z" />
              </svg>
              <p className="text-sm font-mono">Select a request to inspect</p>
              <p className="text-xs mt-1 text-zinc-700">Or send one with <code className="text-zinc-500">curl -X POST localhost:8877</code></p>
            </div>
          )}
        </main>
      </div>
    </div>
  );
}
