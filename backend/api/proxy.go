package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

// allowedProxyHosts restricts the proxy to YouTube's CDN only, so this
// endpoint can never be used as an open proxy for arbitrary URLs.
var allowedProxyHostSuffixes = []string{
	".googlevideo.com",
	"googlevideo.com",
}

func isAllowedProxyHost(host string) bool {
	host = strings.ToLower(host)
	for _, suffix := range allowedProxyHostSuffixes {
		if host == suffix || strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// ProxyStreamHandler fetches a googlevideo.com stream URL from the backend
// server itself and relays the bytes to the client.
//
// Why this exists: the URL returned by YouTube's player endpoint is bound to
// the IP address that requested it - i.e. this backend server's IP, NOT the
// browser's IP. If a client's browser (especially over a VPN, or on a
// different network than the server) tries to fetch that URL directly, the
// IP mismatch and/or the VPN/datacenter IP reputation can cause Google to
// reject the request. The browser then reports a generic
// "MEDIA_ELEMENT_ERROR: Format error" because it received an HTML error page
// instead of audio.
//
// By having this server fetch the bytes (using the same IP that requested
// the URL) and stream them back to the client, the client's own network
// path - VPN or not - no longer matters.
func ProxyStreamHandler(c echo.Context) error {
	rawUrl := c.QueryParam("url")
	if rawUrl == "" {
		return c.String(http.StatusBadRequest, "Missing required param: url")
	}

	target, err := url.Parse(rawUrl)
	if err != nil {
		return c.String(http.StatusBadRequest, "Invalid url param")
	}

	if !isAllowedProxyHost(target.Hostname()) {
		return c.String(http.StatusForbidden, "This proxy only relays googlevideo.com URLs")
	}

	req, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Failed to build upstream request")
	}

	// Forward Range headers so seeking / partial playback keeps working.
	if rangeHeader := c.Request().Header.Get("Range"); rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	req.Header.Set("User-Agent", "com.google.android.apps.youtube.music/6.51.53 (Linux; U; Android 14) gzip")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return c.String(http.StatusBadGateway, "Failed to reach upstream: "+err.Error())
	}
	defer resp.Body.Close()

	res := c.Response()

	// Copy through the headers the audio element cares about.
	for _, h := range []string{"Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Cache-Control"} {
		if v := resp.Header.Get(h); v != "" {
			res.Header().Set(h, v)
		}
	}
	if res.Header().Get("Accept-Ranges") == "" {
		res.Header().Set("Accept-Ranges", "bytes")
	}

	status := resp.StatusCode
	if status == 0 {
		status = http.StatusOK
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "audio/mp4"
	}

	return c.Stream(status, contentType, resp.Body)
}
