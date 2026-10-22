package blog

import (
	"encoding/xml"
	"net/http"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/util"
)

// feed is the RSS 2.0 document envelope.
type feed struct {
	XMLName xml.Name    `xml:"rss"`
	Version string      `xml:"version,attr"`
	Channel feedChannel `xml:"channel"`
}

// feedChannel describes the blog and its published entries.
type feedChannel struct {
	Title       string     `xml:"title"`
	Link        string     `xml:"link"`
	Description string     `xml:"description"`
	Items       []feedItem `xml:"item"`
}

// feedItem carries a stable ID link and rendered post content for feed readers.
type feedItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	Published   string `xml:"pubDate"`
	Content     string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
}

// rss builds a feed from committed posts and configured public URLs, never Host.
func (m *module) rss(w http.ResponseWriter, r *http.Request) {
	posts, err := m.posts.List(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	origin, err := util.BuildPublicURL(r, m.app.Config.HTTP, "/", nil)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	channel := feedChannel{Title: m.app.Config.App.Name, Link: origin.String(), Description: "Building and delivering better software."}
	for _, p := range posts {
		link := *origin
		link.Path = "/post/s/" + p.Slug
		guid := *origin
		guid.Path = "/post/id/" + p.ID
		html, err := markdown(p.Markdown)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
		channel.Items = append(channel.Items, feedItem{p.Title, link.String(), guid.String(), p.Summary, p.Published.Format(time.RFC1123Z), string(html)})
	}
	data, err := xml.Marshal(feed{Version: "2.0", Channel: channel})
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	if r.Method != http.MethodHead {
		if _, err := w.Write(append([]byte(xml.Header), data...)); err != nil {
			logger.LogRequestError(r, err)
		}
	}
}
