package blog

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/util"
)

// editForm keeps submitted fields alongside server-owned identity and publication
// time so failed submissions can redisplay the editor without losing the user's work.
type editForm struct {
	form.Form
	Post
	Action string
}

// Bind reads editable fields from the parsed form body, never from URL parameters.
// For edits, ID and PublishedAt come from storage, but Version must come from the form.
func (f *editForm) Bind(r *http.Request) error {
	f.Slug = strings.TrimSpace(r.PostForm.Get("slug"))
	f.Title = strings.TrimSpace(r.PostForm.Get("title"))
	f.Summary = strings.TrimSpace(r.PostForm.Get("summary"))
	f.Markdown = r.PostForm.Get("markdown")
	if f.Slug == "" {
		f.Slug = Slug(f.Title)
	}
	if f.ID != "" {
		// An invalid submission must not inherit the latest stored version and
		// silently turn a stale edit into an overwrite on resubmission.
		f.Version = 0
		version, err := strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
		if err != nil || version < 1 {
			return form.FieldErrors{"Version": {"Invalid post version. Reload the editor."}}
		}
		f.Version = version
	}
	return nil
}

// Validate reports field errors before the save operation assigns new-post metadata.
// Repository validation checks the complete Post before persistence.
func (f *editForm) Validate() form.FieldErrors {
	fields := make(form.FieldErrors)
	if f.Title == "" || utf8.RuneCountInString(f.Title) > 200 || !utf8.ValidString(f.Title) {
		fields["Title"] = []string{"Enter a title of at most 200 characters using valid UTF-8."}
	}
	if f.Slug == "" || len(f.Slug) > 160 || Slug(f.Slug) != f.Slug {
		fields["Slug"] = []string{"Use lowercase letters, numbers, and single hyphens, up to 160 characters."}
	}
	if utf8.RuneCountInString(f.Summary) > 1000 || !utf8.ValidString(f.Summary) {
		fields["Summary"] = []string{"Use valid UTF-8 and keep the summary within 1,000 characters."}
	}
	if strings.TrimSpace(f.Markdown) == "" || len(f.Markdown) > MaxContentBytes || !utf8.ValidString(f.Markdown) {
		fields["Markdown"] = []string{"Enter valid UTF-8 Markdown, up to 256 KiB."}
	}
	return fields
}

func (m *module) createForm(w http.ResponseWriter, r *http.Request) {
	m.render(w, r, "edit", "New post", &editForm{Action: "/post"})
}

func (m *module) editForm(w http.ResponseWriter, r *http.Request) {
	p, err := m.options.Posts.GetByID(r.Context(), r.PathValue("id"))
	if err != nil {
		m.failure(w, r, err)
		return
	}
	m.render(w, r, "edit", "Edit post", &editForm{Post: p, Action: "/post/update/" + p.ID})
}

// save owns post creation metadata and preserves submitted values after a conflict.
// The repository must check the submitted version atomically when editing.
func (m *module) save(w http.ResponseWriter, r *http.Request, editing bool) {
	f := &editForm{Action: "/post"}
	if editing {
		p, err := m.options.Posts.GetByID(r.Context(), r.PathValue("id"))
		if err != nil {
			m.failure(w, r, err)
			return
		}
		f.Post = p
		f.Action = "/post/update/" + p.ID
	}
	page := m.views["edit"].Content(r, f)
	page.Title = "Edit post"
	if err := form.ParseAndValidateWithOptions(r, f, form.ParseOptions{MaxFieldBytes: MaxContentBytes}); err != nil {
		form.HandleFormError(w, r, page, f, "Unable to read the post", err)
		return
	}

	var err error
	if editing {
		err = m.options.Posts.Update(r.Context(), f.Post)
	} else {
		f.ID, f.PublishedAt, f.Version = uuid.NewString(), time.Now().UTC(), 1
		err = m.options.Posts.Create(r.Context(), f.Post)
	}
	if err != nil {
		if errors.Is(err, ErrSlugExists) || errors.Is(err, ErrConflict) {
			f.AddErrorMessage(err.Error())
			m.render(w, r, "edit", "Edit post", f)
		} else {
			m.failure(w, r, err)
		}
		return
	}
	util.RedirectToURL(w, r, postURL(f.Post))
}

func (m *module) create(w http.ResponseWriter, r *http.Request) { m.save(w, r, false) }
func (m *module) update(w http.ResponseWriter, r *http.Request) { m.save(w, r, true) }

func (m *module) deleteForm(w http.ResponseWriter, r *http.Request) {
	p, err := m.options.Posts.GetByID(r.Context(), r.PathValue("id"))
	if err != nil {
		m.failure(w, r, err)
		return
	}
	m.render(w, r, "delete", "Delete post", p)
}

// delete uses the version shown on the confirmation page so a concurrent edit
// cannot be discarded by an older delete request.
func (m *module) delete(w http.ResponseWriter, r *http.Request) {
	if err := form.Parse(r); err != nil {
		http.Error(w, "Invalid delete request", err.StatusCode)
		return
	}
	version, err := strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
	if err != nil || version < 1 {
		http.Error(w, "Invalid post version", http.StatusBadRequest)
		return
	}
	if err := m.options.Posts.Delete(r.Context(), r.PathValue("id"), version); err != nil {
		m.failure(w, r, err)
		return
	}
	util.RedirectToURL(w, r, "/")
}
