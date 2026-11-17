package blog

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

var redditPostPath = regexp.MustCompile(`^/r/[a-zA-Z0-9_]{1,50}/comments/[a-z0-9]{2,16}(?:/[\p{L}\p{M}\p{N}_-]{1,300}(?:/[a-z0-9]{2,16})?)?$`)
var instagramPostPath = regexp.MustCompile(`^/(p|reel|tv)/[a-zA-Z0-9_-]{5,64}$`)
var linkedInPostPath = regexp.MustCompile(`^/posts/[a-zA-Z0-9_-]+-activity-([1-9][0-9]{0,29})-[a-zA-Z0-9_-]+$`)
var linkedInUpdatePath = regexp.MustCompile(`^/feed/update/urn:li:(activity|share|ugcPost):([1-9][0-9]{0,29})$`)
var tiktokPostPath = regexp.MustCompile(`^/@[a-zA-Z0-9._]{1,24}/(video|photo)/[1-9][0-9]{0,29}$`)
var mastodonPostPath = regexp.MustCompile(`^/@[a-zA-Z0-9_]{1,64}/[1-9][0-9]{0,29}$`)
var mastodonStatusPath = regexp.MustCompile(`^/users/([a-zA-Z0-9_]{1,64})/statuses/([1-9][0-9]{0,29})$`)
var publicDNSSuffix = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func additionalSocialPostURL(provider socialProvider, u *url.URL) (string, error) {
	path := strings.TrimSuffix(u.Path, "/")
	host := strings.ToLower(u.Hostname())
	switch provider.Kind {
	case "reddit-post":
		if (host == "reddit.com" || host == "www.reddit.com" || host == "old.reddit.com" || host == "np.reddit.com") && redditPostPath.MatchString(path) && !strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
			return (&url.URL{Scheme: "https", Host: "www.reddit.com", Path: path + "/"}).String(), nil
		}
	case "instagram-post":
		if (host == "instagram.com" || host == "www.instagram.com") && instagramPostPath.MatchString(path) {
			return "https://www.instagram.com" + path + "/", nil
		}
	case "linkedin-post":
		if host == "linkedin.com" || host == "www.linkedin.com" {
			if linkedInUpdatePath.MatchString(path) {
				return "https://www.linkedin.com" + path, nil
			}
			if match := linkedInPostPath.FindStringSubmatch(path); match != nil {
				return "https://www.linkedin.com/feed/update/urn:li:activity:" + match[1], nil
			}
		}
	case "tiktok-post":
		if (host == "tiktok.com" || host == "www.tiktok.com" || host == "m.tiktok.com") && tiktokPostPath.MatchString(path) {
			return "https://www.tiktok.com" + path, nil
		}
	case "mastodon-post":
		return mastodonPostURL(u)
	}
	return "", contentError("Enter a full public " + provider.Name + " post URL.")
}

// Mastodon runs on independent servers. Accept public DNS names and the native
// status URL shapes, without fetching an arbitrary server during editing or Save.
func mastodonPostURL(u *url.URL) (string, error) {
	invalid := contentError("Enter a full public Mastodon post URL.")
	host := strings.ToLower(u.Hostname())
	if u.Scheme != "https" || u.RawPath != "" || !blueskyHandle.MatchString(host) || len(host) > 253 || net.ParseIP(host) != nil || !publicDNSSuffix.MatchString(host[strings.LastIndex(host, ".")+1:]) {
		return "", invalid
	}
	for _, suffix := range []string{"localhost", "local", "internal", "test", "invalid", "example", "onion", "home", "arpa"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return "", invalid
		}
	}
	path := strings.TrimSuffix(u.Path, "/")
	if match := mastodonStatusPath.FindStringSubmatch(path); match != nil {
		path = "/@" + match[1] + "/" + match[2]
	}
	if !mastodonPostPath.MatchString(path) {
		return "", invalid
	}
	return "https://" + host + path, nil
}
