package blog

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const linkFetchTimeout = 5 * time.Second
const maxLinkHTML = 1024 * 1024

// Address checks apply to every connection, including redirects and thumbnails.
// Dialing the checked IP avoids a second DNS lookup. Proxies are never used.
type publicDialer struct {
	lookup func(context.Context, string, string) ([]netip.Addr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
}

func (d publicDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "80" && port != "443" {
		return nil, errors.New("unsupported destination port")
	}
	addresses, err := d.lookup(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, errors.New("destination has no addresses")
	}
	for _, address := range addresses {
		if !publicPreviewAddress(address) {
			return nil, errors.New("destination is not a public address")
		}
	}
	for _, address := range addresses {
		var connection net.Conn
		connection, err = d.dial(ctx, network, net.JoinHostPort(address.Unmap().String(), port))
		if err == nil {
			return connection, nil
		}
	}
	return nil, err
}

func publicPreviewAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLinkLocalUnicast() {
		return false
	}
	// Include special-use ranges that IsGlobalUnicast considers unicast.
	for _, prefix := range [...]string{
		"0.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20",
	} {
		if netip.MustParsePrefix(prefix).Contains(address) {
			return false
		}
	}
	return address.Is4() || netip.MustParsePrefix("2000::/3").Contains(address)
}

func newLinkClient() *http.Client {
	dialer := publicDialer{net.DefaultResolver.LookupNetIP, (&net.Dialer{Timeout: 3 * time.Second}).DialContext}
	return &http.Client{
		Timeout: linkFetchTimeout,
		Transport: &http.Transport{
			DialContext: dialer.DialContext, DisableKeepAlives: true,
			TLSHandshakeTimeout: 2 * time.Second, ResponseHeaderTimeout: 3 * time.Second,
			MaxResponseHeaderBytes: 64 * 1024,
		},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) > 3 {
				return errors.New("too many redirects")
			}
			if _, err := linkURL(request.URL.String()); err != nil {
				return err
			}
			if via[len(via)-1].URL.Scheme == "https" && request.URL.Scheme != "https" {
				return errors.New("insecure redirect")
			}
			return nil
		},
	}
}

func openLinkResponse(ctx context.Context, client *http.Client, raw, accept string) (*http.Response, error) {
	u, err := linkURL(raw)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", "HyperServer-LinkPreview/1.0")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, errors.New("unexpected response")
	}
	return response, nil
}

func fetchLinkBytes(ctx context.Context, client *http.Client, raw, accept string, limit int64) ([]byte, *url.URL, string, error) {
	response, err := openLinkResponse(ctx, client, raw, accept)
	if err != nil {
		return nil, nil, "", err
	}
	defer response.Body.Close()
	if response.ContentLength > limit {
		return nil, nil, "", errors.New("unexpected or oversized response")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, nil, "", errors.New("unable to read response within its limit")
	}
	return data, response.Request.URL, response.Header.Get("Content-Type"), nil
}

// Only the head supplies preview metadata. Stop before a potentially large body.
func fetchLinkHead(ctx context.Context, client *http.Client, raw string) ([]byte, *url.URL, error) {
	response, err := openLinkResponse(ctx, client, raw, "text/html, application/xhtml+xml")
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	kind, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if kind != "text/html" && kind != "application/xhtml+xml" {
		return nil, nil, errors.New("unexpected content type")
	}
	reader := &io.LimitedReader{R: response.Body, N: maxLinkHTML + 1}
	tokens := html.NewTokenizer(reader)
	var head bytes.Buffer
	for {
		kind := tokens.Next()
		if reader.N == 0 {
			return nil, nil, errors.New("metadata exceeds its limit")
		}
		if kind == html.ErrorToken {
			if tokens.Err() != io.EOF {
				return nil, nil, tokens.Err()
			}
			break
		}
		head.Write(tokens.Raw())
		if token := tokens.Token(); kind == html.EndTagToken && token.Data == "head" || kind == html.StartTagToken && token.Data == "body" {
			break
		}
	}
	if !utf8.Valid(head.Bytes()) {
		return nil, nil, contentError("This page uses an unsupported text encoding. Enter the details yourself.")
	}
	return head.Bytes(), response.Request.URL, nil
}

// Metadata is untrusted text. Ignore embed markup, og:url, and document base URLs.
func linkMetadata(data []byte, destination *url.URL) (postLink, string) {
	link := postLink{URL: destination.String()}
	values := make(map[string]string)
	tokens := html.NewTokenizer(bytes.NewReader(data))
	inTitle := false
	var title strings.Builder
parse:
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			break parse
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokens.Token()
			if token.Data == "body" {
				break parse
			}
			if token.Data == "title" {
				inTitle = true
			}
			if token.Data == "meta" {
				var name, value string
				for _, attr := range token.Attr {
					if attr.Key == "property" || attr.Key == "name" {
						name = strings.ToLower(strings.TrimSpace(attr.Val))
					}
					if attr.Key == "content" {
						value = strings.TrimSpace(attr.Val)
					}
				}
				switch name {
				case "og:title", "og:description", "description", "og:site_name", "og:image":
					if values[name] == "" {
						values[name] = value
					}
				}
			}
		case html.TextToken:
			if inTitle {
				title.Write(tokens.Text())
			}
		case html.EndTagToken:
			token := tokens.Token()
			if token.Data == "title" {
				inTitle = false
			}
			if token.Data == "head" {
				break parse
			}
		}
	}
	link.Title = metadataText(values["og:title"], 240)
	if link.Title == "" {
		link.Title = metadataText(title.String(), 240)
	}
	if link.Title == "" {
		link.Title = destination.Hostname()
	}
	link.Description = metadataText(values["og:description"], 1000)
	if link.Description == "" {
		link.Description = metadataText(values["description"], 1000)
	}
	link.SiteName = metadataText(values["og:site_name"], 200)
	return link, values["og:image"]
}

func metadataText(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.ToValidUTF8(value, "")), " ")
	if utf8.RuneCountInString(value) > limit {
		value = string([]rune(value)[:limit])
	}
	return value
}

// One bounded, per-application budget follows accounts rather than IP addresses.
// Expired windows are removed on admission. Active accounts are never evicted.
type linkFetchBudget struct {
	mu      sync.Mutex
	windows map[string]linkFetchWindow
}

type linkFetchWindow struct {
	until time.Time
	count int
}

func (b *linkFetchBudget) allow(id string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key, window := range b.windows {
		if !now.Before(window.until) {
			delete(b.windows, key)
		}
	}
	window, exists := b.windows[id]
	if window.count >= 10 || !exists && len(b.windows) >= 1024 {
		return false
	}
	if b.windows == nil {
		b.windows = make(map[string]linkFetchWindow)
	}
	if !exists {
		window.until = now.Add(time.Minute)
	}
	window.count++
	b.windows[id] = window
	return true
}

func (m *module) fetchLink(w http.ResponseWriter, r *http.Request) {
	m.fetchPreview(w, r, false)
}

func (m *module) fetchVideoDetails(w http.ResponseWriter, r *http.Request) {
	m.fetchPreview(w, r, true)
}

func (m *module) fetchPreview(w http.ResponseWriter, r *http.Request, video bool) {
	account, permission, ok := m.mediaActor(w, r)
	if !ok {
		return
	}
	if !m.linkBudget.allow(account.ID, time.Now()) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, r, http.StatusTooManyRequests, map[string]string{"error": "Preview limit reached. Try again in a minute, or enter the details yourself."})
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, r, http.StatusBadRequest, map[string]string{"error": "Unable to read the URL."})
		return
	}
	destination, err := linkURL(strings.TrimSpace(r.PostForm.Get("url")))
	var source videoSource
	if err == nil && video {
		source, err = videoURL(destination.String())
		if err == nil {
			destination, _ = url.Parse(source.viewingURL())
		}
	}
	if err != nil {
		writeJSON(w, r, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	select {
	case m.linkProcessing <- struct{}{}:
		defer func() { <-m.linkProcessing }()
	default:
		w.Header().Set("Retry-After", "1")
		writeJSON(w, r, http.StatusTooManyRequests, map[string]string{"error": "Other previews are loading. Try again shortly."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), linkFetchTimeout)
	defer cancel()
	data, final, err := fetchLinkHead(ctx, m.linkClient, destination.String())
	if err != nil {
		message := "Couldn't fetch this page. You can enter the details yourself."
		var textError contentError
		if errors.As(err, &textError) {
			message = textError.Error()
		}
		writeJSON(w, r, http.StatusUnprocessableEntity, map[string]string{"error": message})
		return
	}
	link, imageURL := linkMetadata(data, destination)
	warning := ""
	if video {
		resolved, err := videoURL(final.String())
		if err != nil || resolved.Provider != source.Provider || resolved.ID != source.ID || resolved.Hash != source.Hash || link.Title == destination.Hostname() && link.Description == "" {
			writeJSON(w, r, http.StatusUnprocessableEntity, map[string]string{"error": "No video details were available. You can enter them yourself."})
			return
		}
		if link.Title == destination.Hostname() {
			link.Title = ""
		}
		if link.Description == "" {
			warning = "Title fetched. No description was available."
		}
		writeJSON(w, r, http.StatusOK, struct {
			Video   postVideo `json:"video"`
			Warning string    `json:"warning,omitempty"`
		}{postVideo{URL: strings.TrimSpace(r.PostForm.Get("url")), Title: link.Title, Caption: link.Description}, warning})
		return
	}
	if imageURL != "" && r.PostForm.Get("image") != "false" {
		ref, err := url.Parse(imageURL)
		if err == nil {
			link.ImageID, err = m.importLinkImage(ctx, final.ResolveReference(ref).String(), permission)
		}
		if err != nil {
			warning = "The preview image couldn't be imported. The card can be saved without it."
		}
	}
	writeJSON(w, r, http.StatusOK, struct {
		Link    postLink `json:"link"`
		Warning string   `json:"warning,omitempty"`
	}{link, warning})
}

func (m *module) importLinkImage(ctx context.Context, raw string, permission role) (string, error) {
	select {
	case m.imageProcessing <- struct{}{}:
		defer func() { <-m.imageProcessing }()
	default:
		return "", errors.New("another image is being processed")
	}
	data, final, _, err := fetchLinkBytes(ctx, m.linkClient, raw, "image/png, image/jpeg, image/gif", maxImageBytes)
	if err != nil {
		return "", err
	}
	// The processing pipeline checks bytes, decoded dimensions, and image format.
	id, _, err := m.storeImageData(ctx, data, path.Base(final.Path), permission)
	return id, err
}
