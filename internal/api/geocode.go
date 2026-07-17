package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// Reverse geocoding, proxied through this server.
//
// Two reasons it is a proxy rather than a fetch from the browser:
//
//  1. Our CSP is `connect-src 'self'`. A browser call to a geocoder would be
//     blocked, and relaxing the policy to allow it would widen the one thing
//     keeping third parties out of the page.
//  2. Nominatim's usage policy requires an identifying User-Agent and caps
//     traffic at roughly one request per second. A browser cannot be trusted
//     to honour either; a single server-side gate can.
//
// OpenStreetMap rather than Google Maps: no API key, no billing account, no
// SDK to load. The trade is coarser data — good enough to name the street and
// the LGA, which is what a dispatch rider needs on top of coordinates.
const (
	nominatimURL   = "https://nominatim.openstreetmap.org/reverse"
	geocodeTimeout = 6 * time.Second
)

// geocodeLimiter enforces Nominatim's ~1 req/sec policy across all callers.
// Exceeding it gets an IP banned, which would break the feature for everyone.
type geocodeLimiter struct {
	mu   sync.Mutex
	last time.Time
}

func (l *geocodeLimiter) wait(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	const minGap = 1100 * time.Millisecond
	if gap := time.Since(l.last); gap < minGap {
		select {
		case <-time.After(minGap - gap):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	l.last = time.Now()
	return nil
}

var limiter = &geocodeLimiter{}

type nominatimResponse struct {
	DisplayName string `json:"display_name"`
	Address     struct {
		HouseNumber   string `json:"house_number"`
		Road          string `json:"road"`
		Neighbourhood string `json:"neighbourhood"`
		Suburb        string `json:"suburb"`
		CityDistrict  string `json:"city_district"`
		City          string `json:"city"`
		Town          string `json:"town"`
		Village       string `json:"village"`
		County        string `json:"county"`
		State         string `json:"state"`
		Country       string `json:"country"`
	} `json:"address"`
}

type geocodeResult struct {
	Address string `json:"address"`
	City    string `json:"city"`
	State   string `json:"state"`
	Full    string `json:"full"`
}

// normaliseState maps what OpenStreetMap calls a state to the values our
// delivery pricing recognises. OSM says "Lagos State"; the free-delivery rule
// checks for "Lagos", and a near-miss here silently charges a Lagos customer
// for nationwide delivery.
func normaliseState(s string) string {
	s = trimSpace(s)
	for _, suffix := range []string{" State", " state"} {
		if len(s) > len(suffix) && s[len(s)-len(suffix):] == suffix {
			s = s[:len(s)-len(suffix)]
		}
	}
	if s == "Federal Capital Territory" || s == "Abuja" {
		return "FCT"
	}
	return s
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func (s *Server) handleReverseGeocode(w http.ResponseWriter, r *http.Request) {
	lat, errLat := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lng, errLng := strconv.ParseFloat(r.URL.Query().Get("lng"), 64)
	if errLat != nil || errLng != nil {
		writeError(w, http.StatusBadRequest, "lat and lng are required")
		return
	}
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		writeError(w, http.StatusBadRequest, "coordinates out of range")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), geocodeTimeout)
	defer cancel()

	if err := limiter.wait(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "busy — please type your address")
		return
	}

	q := url.Values{}
	q.Set("format", "jsonv2")
	q.Set("lat", strconv.FormatFloat(lat, 'f', 6, 64))
	q.Set("lon", strconv.FormatFloat(lng, 'f', 6, 64))
	q.Set("zoom", "18") // building/street level
	q.Set("addressdetails", "1")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nominatimURL+"?"+q.Encode(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not look up that location")
		return
	}
	// Nominatim's policy requires a real identifying User-Agent. Sending a
	// default one is how a project gets blocked.
	req.Header.Set("User-Agent", "Kon-firm/1.0 (https://konfirm.onrender.com)")
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Timeout: geocodeTimeout}).Do(req)
	if err != nil {
		s.log.Warn("reverse geocode failed", "err", err)
		writeError(w, http.StatusBadGateway, "could not look up that location — please type your address")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.log.Warn("reverse geocode non-200", "status", resp.StatusCode)
		writeError(w, http.StatusBadGateway, "could not look up that location — please type your address")
		return
	}

	var n nominatimResponse
	if err := json.NewDecoder(resp.Body).Decode(&n); err != nil {
		writeError(w, http.StatusBadGateway, "could not read that location")
		return
	}

	// Build a street line the way a Nigerian address is written.
	street := trimSpace(n.Address.HouseNumber + " " + n.Address.Road)
	if street == "" {
		street = firstNonEmpty(n.Address.Neighbourhood, n.Address.Suburb)
	}

	city := firstNonEmpty(n.Address.CityDistrict, n.Address.Suburb, n.Address.City,
		n.Address.Town, n.Address.Village, n.Address.County)

	writeJSON(w, http.StatusOK, geocodeResult{
		Address: street,
		City:    city,
		State:   normaliseState(n.Address.State),
		Full:    n.DisplayName,
	})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if trimSpace(v) != "" {
			return trimSpace(v)
		}
	}
	return ""
}
