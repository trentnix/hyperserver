package blog

import (
	"io/fs"
	"net/http"
	"time"

	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/ratelimit"
	"github.com/trentnix/hyperserver/pkg/routing"
)

// Routes binds public reading and explicitly protected publishing endpoints.
func (m *module) Routes(routes *routing.Routes) error {
	pages, err := m.auth.PageHandlers(routes)
	if err != nil {
		return err
	}
	routes.Handle("GET /login", pages.Login)
	pages.Submissions.Handle("POST /login", pages.LoginSubmit)
	routes.Handle("GET /register", pages.Registration)
	pages.Submissions.Handle("POST /register", pages.RegistrationSubmit)

	assets, err := fs.Sub(blogModuleFS, "static")
	if err != nil {
		return err
	}
	routes.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets)))
	routes.HandleFunc("GET /assets/js/htmx-errors.js", htmx.ServeErrorScript)
	if m.localAssets != nil {
		routes.HandleFunc("GET /local-assets/{name}", m.requireSetup(m.localAsset))
	}
	routes.HandleFunc("GET /{$}", m.requireSetup(m.index))
	routes.HandleFunc("GET /posts", m.requireSetup(m.archive))
	searchRoutes, err := routes.WithRateLimit(ratelimit.Policy{Requests: searchRequestsPerMinute, Window: time.Minute, MaxClients: 4096})
	if err != nil {
		return err
	}
	searchRoutes.HandleFunc("GET /search", m.requireSetup(m.search))
	routes.HandleFunc("GET /posts/recent", m.requireSetup(m.recent))
	routes.HandleFunc("GET /post/s/{slug}", m.requireSetup(m.post))
	routes.HandleFunc("GET /post/id/{id}", m.requireSetup(m.post))
	routes.HandleFunc("GET /images/{id}", m.requireSetup(m.image))
	routes.HandleFunc("GET /rss", m.requireSetup(m.rss))
	routes.HandleFunc("GET /sitemap.xml", m.requireSetup(m.sitemap))
	routes.HandleFunc("GET /robots.txt", m.requireSetup(m.robots))
	routes.Handle("GET /history/{kind}/{id}", m.requireAdmin(m.revisions))
	routes.Handle("GET /history/{kind}/{id}/delete/{revision}", m.requireAdmin(m.confirmRevisionDelete))
	routes.HandleWithBodyLimit("POST /history/{kind}/{id}/restore", m.requireAdmin(m.restoreRevision), routing.MiB)
	routes.Handle("POST /history/{kind}/{id}/delete/{revision}", m.requireAdmin(m.deleteRevision))
	routes.Handle("GET /profile", m.requireAdmin(m.profileEditor))
	routes.Handle("GET /profile/history", m.requireAdmin(m.profileHistory))
	routes.Handle("GET /profile/history/delete/{revision}", m.requireAdmin(m.confirmProfileRevisionDelete))
	routes.Handle("POST /profile/history/delete/{revision}", m.requireAdmin(m.deleteProfileRevision))
	routes.Handle("GET /media", m.requireAdmin(m.mediaLibrary))
	routes.HandleWithBodyLimit("POST /media/upload", m.requireAdmin(m.uploadMedia), 10*routing.MiB)
	routes.Handle("GET /media/{id}", m.requireAdmin(m.mediaDetail))
	routes.Handle("POST /media/{id}", m.requireAdmin(m.saveMediaMetadata))
	routes.Handle("GET /media/unused", m.requireAdmin(m.unusedMedia))
	routes.Handle("GET /media/delete-unused", m.requireAdmin(m.confirmUnusedMediaDelete))
	routes.HandleWithBodyLimit("POST /media/delete-unused", m.requireAdmin(m.deleteUnusedMedia), routing.MiB)
	routes.Handle("GET /media/delete/{id}", m.requireAdmin(m.confirmMediaDelete))
	routes.Handle("POST /media/delete/{id}", m.requireAdmin(m.deleteMedia))
	routes.HandleWithBodyLimit("POST /profile", m.requireAdmin(m.saveProfile), routing.MiB)
	routes.HandleWithBodyLimit("POST /profile/restore", m.requireAdmin(m.restoreProfile), routing.MiB)
	routes.HandleFunc("GET /about", m.requireSetup(m.page))
	routes.HandleFunc("GET /page/{slug}", m.requireSetup(m.page))
	routes.Handle("GET /pages", m.requireAdmin(m.pages))
	routes.Handle("GET /pages/create", m.requireAdmin(m.pageEditor))
	routes.Handle("GET /pages/update/{id}", m.requireAdmin(m.pageEditor))
	routes.Handle("GET /pages/delete/{id}", m.requireAdmin(m.confirmPageDelete))
	routes.HandleWithBodyLimit("POST /pages", m.requireAdmin(m.savePage), routing.MiB)
	routes.HandleWithBodyLimit("POST /pages/{id}", m.requireAdmin(m.savePage), routing.MiB)
	routes.Handle("POST /pages/delete/{id}", m.requireAdmin(m.deletePage))
	routes.HandleFunc("GET /projects", m.requireSetup(m.page))
	routes.HandleFunc("GET /project/s/{slug}", m.requireSetup(m.page)) // Retain published project links.
	routes.Handle("GET /post/create", m.requireAdmin(m.editor))
	routes.Handle("GET /post/manage", m.requireAdmin(m.managePosts))
	routes.Handle("GET /post/update/{id}", m.requireAdmin(m.editor))
	routes.Handle("GET /post/delete/{id}", m.requireAdmin(m.confirmDelete))
	routes.HandleWithBodyLimit("POST /post", m.requireAdmin(m.save), routing.MiB)
	routes.HandleWithBodyLimit("POST /post/{id}", m.requireAdmin(m.save), routing.MiB)
	routes.Handle("POST /post/delete/{id}", m.requireAdmin(m.delete))
	routes.HandleWithBodyLimit("POST /post/preview", m.requireAdmin(m.preview), routing.MiB)
	routes.HandleWithBodyLimit("POST /post/images/upload", http.HandlerFunc(m.upload), 10*routing.MiB)
	routes.HandleFunc("GET "+authReturnURL, m.requireSetup(m.continueSetup))
	routes.HandleFunc("/", m.requireSetup(m.app.ContentManager.HandleNotFound))
	return routes.Err()
}
