package api

import (
	"crypto/sha256"
	"encoding/hex"
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
	// buildETag identifies this build. The frontend is embedded at compile
	// time, so every asset in a given binary changes together or not at all —
	// one tag for the lot is exact, not an approximation.
	buildETag string
}

func newStaticHandler(root fs.FS) *staticHandler {
	return &staticHandler{
		assets:    http.FileServer(http.FS(root)),
		root:      root,
		buildETag: buildETag(root),
	}
}

// buildETag hashes the embedded frontend. Deterministic across restarts of the
// same binary, so a redeploy invalidates caches and a restart does not.
func buildETag(root fs.FS) string {
	h := sha256.New()
	_ = fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(root, p)
		if err != nil {
			return nil
		}
		h.Write([]byte(p))
		h.Write(b)
		return nil
	})
	return `"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`
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

			// Revalidate every asset against the build id.
			//
			// A plain max-age let a browser keep JavaScript for five minutes
			// after a deploy while fetching the new HTML immediately — a
			// half-updated page that looks like a bug and cannot be diagnosed,
			// because the server is serving the fix and the browser is not
			// running it. no-cache does not mean "do not store": the browser
			// still caches, but asks first, and gets a 304 when nothing moved.
			// One conditional request per asset is worth never debugging a
			// phantom.
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("ETag", h.buildETag)
			if match := r.Header.Get("If-None-Match"); match == h.buildETag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
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
