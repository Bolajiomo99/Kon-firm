package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// staticHandler serves the embedded frontend.
//
// Page routes are read from the FS directly rather than delegated to
// http.FileServer. FileServer canonicalises "/index.html" by 301-ing to "./",
// so routing "/" through it answers the homepage with a redirect instead of
// the page.
type staticHandler struct {
	assets http.Handler
	root   fs.FS
}

func newStaticHandler(root fs.FS) *staticHandler {
	return &staticHandler{assets: http.FileServer(http.FS(root)), root: root}
}

// pages maps clean URLs to their files, so /pos works as well as /pos.html.
var pages = map[string]string{
	"/":                 "index.html",
	"/index.html":       "index.html",
	"/pos":              "pos.html",
	"/admin":            "admin.html",
	"/login":            "login.html",
	"/signup":           "signup.html",
	"/orders":           "orders.html",
	"/payment/callback": "callback.html",
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if file, ok := pages[r.URL.Path]; ok {
		h.writeFile(w, r, file, http.StatusOK)
		return
	}

	// Anything else is an asset. Confirm it exists before delegating, so a
	// miss renders our 404 page rather than Go's bare "404 page not found".
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name != "" && name != "." {
		if f, err := h.root.Open(name); err == nil {
			_ = f.Close()
			// Modest caching: a redeploy must not serve yesterday's JavaScript.
			w.Header().Set("Cache-Control", "public, max-age=300")
			h.assets.ServeHTTP(w, r)
			return
		}
	}

	h.writeFile(w, r, "404.html", http.StatusNotFound)
}

func (h *staticHandler) writeFile(w http.ResponseWriter, r *http.Request, name string, status int) {
	b, err := fs.ReadFile(h.root, name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// HTML is never cached: it is the entry point, and a stale index.html
	// pointing at replaced assets is how a deploy half-breaks.
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)

	if r.Method != http.MethodHead {
		_, _ = w.Write(b)
	}
}
