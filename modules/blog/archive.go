package blog

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type month struct {
	Title string
	Posts []Post
}
type archiveView struct {
	Months         []month
	Previous, Next string
}

// archive serves the full archive or the sidebar's recent-post fragment.
// History restoration must receive the archive, not sidebar-only markup.
func (m *module) archive(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "HX-Target")
	page := 1
	if value := r.URL.Query().Get("page"); value != "" {
		var err error
		page, err = strconv.Atoi(value)
		if err != nil || page < 1 || page > 50000 {
			http.Error(w, "Invalid page number", http.StatusBadRequest)
			return
		}
	}
	// One extra post tells us whether to offer a next page without a count query.
	posts, err := m.options.Posts.List(r.Context(), (page-1)*PageSize, PageSize+1)
	if err != nil {
		m.failure(w, r, err)
		return
	}
	data := archiveView{}
	if page > 1 {
		data.Previous = fmt.Sprintf("/posts?page=%d", page-1)
	}
	if len(posts) > PageSize {
		posts = posts[:PageSize]
		if page < 50000 {
			data.Next = fmt.Sprintf("/posts?page=%d", page+1)
		}
	}
	for _, post := range posts {
		name := post.PublishedAt.Format("January 2006")
		if len(data.Months) == 0 || data.Months[len(data.Months)-1].Title != name {
			data.Months = append(data.Months, month{Title: name})
		}
		group := &data.Months[len(data.Months)-1]
		group.Posts = append(group.Posts, post)
	}
	view := "archive"
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Target") == "posts-list" && r.Header.Get("HX-History-Restore-Request") != "true" {
		view = "recent"
	}
	m.render(w, r, view, "Posts", data)
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Date        string `xml:"pubDate"`
	GUID        string `xml:"guid"`
	Content     string `xml:"content:encoded"`
}

// rss builds a bounded RSS 2.0 feed using the configured public origin.
// Item IDs survive slug changes. Relative Markdown links become absolute so
// feed readers can resolve them outside the site.
func (m *module) rss(w http.ResponseWriter, r *http.Request) {
	posts, err := m.options.Posts.List(r.Context(), 0, MaxListSize)
	if err != nil {
		m.failure(w, r, err)
		return
	}
	feed := struct {
		XMLName   xml.Name `xml:"rss"`
		Version   string   `xml:"version,attr"`
		ContentNS string   `xml:"xmlns:content,attr"`
		Channel   struct {
			Title       string    `xml:"title"`
			Link        string    `xml:"link"`
			Description string    `xml:"description"`
			Items       []rssItem `xml:"item"`
		} `xml:"channel"`
	}{Version: "2.0", ContentNS: "http://purl.org/rss/1.0/modules/content/"}
	feed.Channel.Title, feed.Channel.Link, feed.Channel.Description = m.options.Title, m.options.PublicOrigin, m.options.Description
	for _, p := range posts {
		link := m.options.PublicOrigin + postURL(p)
		base, err := url.Parse(link)
		if err != nil {
			m.failure(w, r, err)
			return
		}
		html, err := renderMarkdown(p.Markdown, base)
		if err != nil {
			m.failure(w, r, err)
			return
		}
		feed.Channel.Items = append(feed.Channel.Items, rssItem{Title: p.Title, Link: link, Description: p.Summary, Date: p.PublishedAt.Format(time.RFC1123Z), GUID: m.options.PublicOrigin + "/post/id/" + p.ID, Content: string(html)})
	}
	output, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		m.failure(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodHead {
		_, _ = w.Write(append([]byte(xml.Header), output...))
	}
}
