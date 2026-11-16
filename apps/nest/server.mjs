// Nest's own server for the test and production environments: serves the
// built pages and forwards /api, /auth, /ws and /uploads to the fork server,
// so the browser only ever talks to one origin. Node built-ins only.
//
//   NEST_API_ORIGIN=http://<server>:8080 PORT=3000 node server.mjs
//
// The Host header is passed through untouched: the server's /ws origin check
// lets a request through when Origin's host equals Host, and the login cookie
// belongs to the host the browser sees. No X-Forwarded-* headers are added.
import http from "node:http";
import net from "node:net";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const PROXIED = ["/api", "/auth", "/ws", "/uploads"];

/**
 * Whether a request path goes to the fork server: the whole first segment
 * must match. `/ws-team/issues` is a workspace whose slug starts with "ws",
 * and stays with Nest.
 */
export function isProxied(pathname) {
  return PROXIED.some((p) => pathname === p || pathname.startsWith(`${p}/`));
}

/** The file under `root` a request path names, or null when it escapes `root`. */
export function staticFile(root, pathname) {
  let decoded;
  try {
    decoded = decodeURIComponent(pathname);
  } catch {
    return null;
  }
  if (decoded.includes("\0")) return null;
  const full = path.resolve(root, `.${path.posix.normalize(`/${decoded}`)}`);
  return full === root || full.startsWith(`${root}${path.sep}`) ? full : null;
}

const TYPES = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".ico": "image/x-icon",
  ".json": "application/json",
  ".woff2": "font/woff2",
  ".map": "application/json",
};

export function createServer({ apiOrigin, dist }) {
  const api = new URL(apiOrigin);
  const apiPort = Number(api.port || (api.protocol === "https:" ? 443 : 80));
  const indexHtml = path.join(dist, "index.html");

  const server = http.createServer((req, res) => {
    const { pathname } = new URL(req.url ?? "/", "http://nest.invalid");
    if (isProxied(pathname)) return forward(req, res);
    serveStatic(pathname, res, req.method === "HEAD");
  });

  // Streams both ways, nothing buffered. `agent: false` opens a fresh
  // connection per request, so a replaced server container is found again by
  // name without restarting Nest.
  function forward(req, res) {
    const upstream = http.request(
      { host: api.hostname, port: apiPort, method: req.method, path: req.url, headers: req.headers, agent: false },
      (up) => {
        res.writeHead(up.statusCode ?? 502, up.rawHeaders);
        up.pipe(res);
      },
    );
    upstream.on("error", () => {
      if (!res.headersSent) res.writeHead(502, { "Content-Type": "application/json" });
      res.end('{"error":"upstream unavailable"}');
    });
    req.pipe(upstream);
  }

  function serveStatic(pathname, res, headOnly) {
    const file = staticFile(dist, pathname);
    let stat = null;
    try {
      stat = file ? fs.statSync(file) : null;
    } catch {
      stat = null;
    }
    // Anything that is not a built file is a page: the front end's router
    // decides, including sending unknown addresses to the issues page.
    const target = stat?.isFile() ? file : indexHtml;
    const isIndex = target === indexHtml;
    res.writeHead(200, {
      "Content-Type": TYPES[path.extname(target)] ?? "application/octet-stream",
      "Cache-Control": isIndex ? "no-store" : pathname.startsWith("/assets/") ? "public, max-age=31536000, immutable" : "no-cache",
    });
    if (headOnly) return res.end();
    fs.createReadStream(target).on("error", () => res.end()).pipe(res);
  }

  // /ws is an HTTP upgrade: open a TCP connection to the server, replay the
  // upgrade request as received, then splice the two sockets together.
  server.on("upgrade", (req, socket, head) => {
    const { pathname } = new URL(req.url ?? "/", "http://nest.invalid");
    if (!isProxied(pathname)) return socket.destroy();
    const upstream = net.connect(apiPort, api.hostname, () => {
      const lines = [`${req.method} ${req.url} HTTP/${req.httpVersion}`];
      for (let i = 0; i < req.rawHeaders.length; i += 2) lines.push(`${req.rawHeaders[i]}: ${req.rawHeaders[i + 1]}`);
      upstream.write(`${lines.join("\r\n")}\r\n\r\n`);
      if (head.length > 0) upstream.write(head);
      upstream.pipe(socket);
      socket.pipe(upstream);
    });
    const close = () => {
      upstream.destroy();
      socket.destroy();
    };
    upstream.on("error", close);
    socket.on("error", close);
    upstream.on("close", close);
    socket.on("close", close);
  });

  return server;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const apiOrigin = process.env.NEST_API_ORIGIN;
  if (!apiOrigin) {
    console.error("NEST_API_ORIGIN is required, e.g. http://127.0.0.1:8080");
    process.exit(1);
  }
  const dist = path.resolve(process.env.NEST_DIST ?? path.join(path.dirname(fileURLToPath(import.meta.url)), "dist"));
  const port = Number(process.env.PORT ?? 3000);
  createServer({ apiOrigin, dist }).listen(port, process.env.HOSTNAME ?? "0.0.0.0", () => {
    console.log(`nest: listening on ${port}, forwarding to ${apiOrigin}`);
  });
}
