package blog

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// Provider identity is derived from the URL, never from author-supplied JSON.
type socialProvider struct {
	Kind, Field, Name, Example string
}

func blueskyProvider() socialProvider {
	return socialProvider{"bluesky-post", "blueskyPost", "Bluesky", "https://bsky.app/profile/username.bsky.social/post/…"}
}

func threadsProvider() socialProvider {
	return socialProvider{"threads-post", "threadsPost", "Threads", "https://www.threads.com/@username/post/…"}
}

func facebookProvider() socialProvider {
	return socialProvider{"facebook-post", "facebookPost", "Facebook", "https://www.facebook.com/username/posts/…"}
}

func xProvider() socialProvider {
	return socialProvider{"x-post", "xPost", "X", "https://x.com/username/status/…"}
}

func socialPostDefinition() componentDefinition {
	return componentDefinition{
		Kind: "social-post", Field: "socialPost", Payload: "socialPost",
		Name: "Social media", Summary: "Public posts, photos, and videos",
		EmptyMessage: "Enter a public post URL or remove the social media component.",
		TemplateFile: "templates/components/social-post.html", Template: "blog.socialPostEditor", Icon: "blog.socialPostIcon", Script: "social-post-editor.js",
		Parse: func(source string) (componentContent, error) { return parseSocialPost(source) }, Renderer: socialPostRenderer{},
		EditorData: (socialProvider{"social-post", "socialPost", "social", "https://…"}).editorData,
	}
}

// Keep the original envelope keys for existing writing, history, and recovered
// drafts. Every editor now accepts the same URLs, regardless of its stored kind.
func legacySocialPostDefinition(provider socialProvider) componentDefinition {
	definition := socialPostDefinition()
	definition.Kind, definition.Field, definition.Payload = provider.Kind, provider.Field, provider.Field
	definition.AliasFor = "social-post"
	provider.Name, provider.Example = "social", "https://…"
	definition.EditorData = provider.editorData
	return definition
}

func socialPostProvider(raw string) (socialProvider, error) {
	u, err := linkURL(raw)
	if err != nil {
		return socialProvider{}, err
	}
	var provider socialProvider
	switch strings.ToLower(u.Hostname()) {
	case "x.com", "www.x.com", "mobile.x.com", "twitter.com", "www.twitter.com", "mobile.twitter.com":
		provider = xProvider()
	case "bsky.app":
		provider = blueskyProvider()
	case "threads.net", "www.threads.net", "threads.com", "www.threads.com":
		provider = threadsProvider()
	case "facebook.com", "www.facebook.com", "m.facebook.com", "mbasic.facebook.com":
		provider = facebookProvider()
	case "reddit.com", "www.reddit.com", "old.reddit.com", "np.reddit.com":
		provider = socialProvider{Kind: "reddit-post", Name: "Reddit"}
	case "instagram.com", "www.instagram.com":
		provider = socialProvider{Kind: "instagram-post", Name: "Instagram"}
	case "linkedin.com", "www.linkedin.com":
		provider = socialProvider{Kind: "linkedin-post", Name: "LinkedIn"}
	case "tiktok.com", "www.tiktok.com", "m.tiktok.com":
		provider = socialProvider{Kind: "tiktok-post", Name: "TikTok"}
	default:
		if _, err := mastodonPostURL(u); err != nil {
			return socialProvider{}, contentError("Enter a public post URL from a supported social media platform.")
		}
		provider = socialProvider{Kind: "mastodon-post", Name: "Mastodon"}
	}
	_, err = provider.postURL(raw)
	return provider, err
}

func (provider socialProvider) editorData(source string, noLibrary bool) any {
	return struct {
		componentEditorData
		socialProvider
	}{componentEditorData{source, noLibrary}, provider}
}

type postSocialPost struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	ImageID     string `json:"imageID,omitempty"`
	provider    socialProvider
}

var blueskyHandle = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$`)
var blueskyDID = regexp.MustCompile(`^did:plc:[a-z2-7]{24}$`)
var blueskyRecord = regexp.MustCompile(`^[a-zA-Z0-9._~:-]{1,512}$`)
var threadsUser = regexp.MustCompile(`^@[a-zA-Z0-9._]{1,30}$`)
var threadsPostID = regexp.MustCompile(`^[a-zA-Z0-9_-]{5,64}$`)
var facebookUser = regexp.MustCompile(`^[a-zA-Z0-9.]{1,100}$`)
var facebookPostID = regexp.MustCompile(`^([1-9][0-9]{0,29}|pfbid[a-zA-Z0-9]{1,200})$`)

// Return a canonical public viewing URL. Never accept embed HTML, redirect links,
// provider plugin URLs, or arbitrary provider-hosted pages as posts.
func (provider socialProvider) postURL(raw string) (string, error) {
	invalid := contentError("Enter a full public " + provider.Name + " post URL.")
	u, err := linkURL(raw)
	if err != nil {
		return "", err
	}
	if u.RawPath != "" && provider.Kind != "reddit-post" {
		return "", invalid
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	host := strings.ToLower(u.Hostname())
	switch provider.Kind {
	case "x-post":
		id, err := xPostURL(raw)
		if err != nil {
			return "", err
		}
		return "https://x.com/i/web/status/" + id, nil
	case "bluesky-post":
		if host != "bsky.app" || len(parts) != 4 || parts[0] != "profile" || parts[2] != "post" ||
			!(len(parts[1]) <= 253 && blueskyHandle.MatchString(parts[1]) || blueskyDID.MatchString(parts[1])) ||
			!blueskyRecord.MatchString(parts[3]) || parts[3] == "." || parts[3] == ".." {
			return "", invalid
		}
		return "https://bsky.app/profile/" + parts[1] + "/post/" + parts[3], nil
	case "threads-post":
		if host != "threads.net" && host != "www.threads.net" && host != "threads.com" && host != "www.threads.com" {
			return "", invalid
		}
		if len(parts) == 3 && threadsUser.MatchString(parts[0]) && parts[1] == "post" && threadsPostID.MatchString(parts[2]) ||
			len(parts) == 2 && parts[0] == "t" && threadsPostID.MatchString(parts[1]) {
			return "https://www.threads.com/" + strings.Join(parts, "/"), nil
		}
	case "facebook-post":
		if host != "facebook.com" && host != "www.facebook.com" && host != "m.facebook.com" && host != "mbasic.facebook.com" {
			return "", invalid
		}
		if len(parts) == 3 && facebookUser.MatchString(parts[0]) && parts[1] == "posts" && facebookPostID.MatchString(parts[2]) {
			return "https://www.facebook.com/" + strings.Join(parts, "/"), nil
		}
		if len(parts) == 1 && (parts[0] == "permalink.php" || parts[0] == "story.php") {
			query, err := url.ParseQuery(u.RawQuery)
			if err == nil && len(query["story_fbid"]) == 1 && len(query["id"]) == 1 && facebookPostID.MatchString(query.Get("story_fbid")) && xPostID.MatchString(query.Get("id")) {
				return "https://www.facebook.com/permalink.php?" + url.Values{"story_fbid": {query.Get("story_fbid")}, "id": {query.Get("id")}}.Encode(), nil
			}
		}
	default:
		return additionalSocialPostURL(provider, u)
	}
	return "", invalid
}

func parseSocialPost(source string) (*postSocialPost, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var value *postSocialPost
	if err := decodeComponentJSON(source, &value); err != nil || value == nil || !utf8.ValidString(source) {
		return nil, contentError("Unable to read the social post. Check its source before saving.")
	}
	provider, err := socialPostProvider(value.URL)
	if err != nil {
		return nil, err
	}
	if !utf8.ValidString(value.Description) || utf8.RuneCountInString(value.Description) > 1000 {
		return nil, contentError("Keep the " + provider.Name + " post description at most 1,000 characters.")
	}
	if value.ImageID != "" && !validImageID(value.ImageID) {
		return nil, contentError("Choose an uploaded image for the " + provider.Name + " post fallback.")
	}
	value.provider = provider
	return value, nil
}

func (value *postSocialPost) Empty() bool { return value == nil }
func (value *postSocialPost) EditorSource() string {
	if value.Empty() {
		return ""
	}
	return componentJSON(value)
}
func (value *postSocialPost) SearchText() string { return "\n" + value.Description }
func (value *postSocialPost) Media() []componentMedia {
	if value.ImageID == "" {
		return nil
	}
	return []componentMedia{{ID: value.ImageID}}
}
func (value *postSocialPost) Node() ast.Node {
	if value.provider.Kind == "x-post" {
		return &xPostNode{Post: *value}
	}
	return &socialPostNode{Post: *value}
}

type socialPostNode struct {
	ast.BaseBlock
	Post postSocialPost
}

var socialPostKind = ast.NewNodeKind("SocialPost")

func (*socialPostNode) Kind() ast.NodeKind              { return socialPostKind }
func (n *socialPostNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type socialPostRenderer struct{}

func (socialPostRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	(xPostRenderer{}).RegisterFuncs(register)
	register.Register(socialPostKind, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		post := node.(*socialPostNode).Post
		provider := post.provider
		viewURL, _ := provider.postURL(post.URL)
		fmt.Fprintf(writer, `<section class="social-post-component" data-social-provider="%s" aria-label="%s post">`, provider.Kind, provider.Name)
		if post.Description != "" {
			fmt.Fprintf(writer, `<p class="social-post-description">%s</p>`, html.EscapeString(post.Description))
		}
		writer.WriteString(`<div class="social-post-media">`)
		if post.ImageID != "" {
			fmt.Fprintf(writer, `<a class="social-post-fallback" data-social-post-fallback href="%s" target="_blank" rel="noopener noreferrer" hx-boost="false"><img src="/images/%s?variant=display" alt="Screenshot of the %s post" loading="lazy" decoding="async"></a>`, html.EscapeString(viewURL), post.ImageID, provider.Name)
		}
		fmt.Fprintf(writer, `<div data-reader-only class="social-post-embed" data-social-post="%s"></div></div><div class="social-post-actions"><div data-reader-only><button type="button" class="button-primary" data-social-post-load hidden>Load %s post</button></div><a href="%s" target="_blank" rel="noopener noreferrer" hx-boost="false">Open on %s</a></div>`, html.EscapeString(viewURL), provider.Name, html.EscapeString(viewURL), provider.Name)
		writer.WriteString(`<div data-reader-only><p class="social-post-status" data-social-post-status role="status"></p></div></section>`)
		return ast.WalkSkipChildren, nil
	})
}
