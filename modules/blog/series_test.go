package blog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

// writeSeries prepares shared metadata fixtures without changing post content.
// Production saves series changes only as part of a post save.
func (m *module) writeSeries(ctx context.Context, value Series, ids []string) (*Series, error) {
	update, err := m.authorizeSeriesUpdate(ctx, &seriesUpdate{Series: value, Posts: ids})
	if err != nil {
		return nil, err
	}
	if value.ID == "" && len(ids) == 0 {
		return nil, contentError("Choose at least one post for a new series.")
	}
	store := m.series.(*sqliteRepository)
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	saved, err := store.saveSeries(ctx, tx, update.Series, ids, update.AuthorID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	m.feed.invalidate()
	return saved, nil
}

func seriesPost(t *testing.T, h *harness, title string) Post {
	t.Helper()
	post, err := h.blog.persistPost(t.Context(), Post{Title: title, Markdown: "Original body"})
	if err != nil {
		t.Fatal(err)
	}
	return post
}

func TestSeriesStorageAndVisibility(t *testing.T) {
	h := registeredBrowserHarness(t)
	first, second, third := seriesPost(t, h, "First"), seriesPost(t, h, "Second"), seriesPost(t, h, "Third")
	ctx := adminWritingContext(t.Context())
	value, err := h.blog.writeSeries(ctx, Series{Title: `A <script>series</script>`}, []string{third.ID, first.ID, second.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, post := range []Post{first, second, third} {
		stored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
		if err != nil || !reflect.DeepEqual(post, stored) {
			t.Fatal("series changes modified post content, settings, or version", err)
		}
		history, err := h.blog.history.ListRevisions(t.Context(), "post", post.ID)
		if err != nil || len(history) != 1 {
			t.Fatal("series changes created a post revision", err)
		}
	}
	navigation, err := h.blog.seriesNavigation(t.Context(), first.ID)
	if err != nil || navigation.Position != 2 || navigation.Previous.ID != third.ID || navigation.Next.ID != second.ID {
		t.Fatal("incorrect series position or neighbors", navigation, err)
	}
	body := h.request("GET", "/post/s/first", nil, false).Body.String()
	if !strings.Contains(body, "Part 2 of 3") || !strings.Contains(body, "A &lt;script&gt;series&lt;/script&gt;") || strings.Contains(body, "<script>series") {
		t.Fatal("series navigation missing or unsafe")
	}
	feed := h.request("GET", "/rss", nil, false)
	requireStatus(t, feed, http.StatusOK)
	if !strings.Contains(feed.Body.String(), "Part 2 of 3") || !strings.Contains(feed.Body.String(), "https://blog.example/post/s/third") {
		t.Fatal("feed omitted series navigation or absolute links")
	}
	third.Draft = true
	third, err = h.blog.persistPost(t.Context(), third)
	if err != nil {
		t.Fatal(err)
	}
	navigation, err = h.blog.seriesNavigation(t.Context(), first.ID)
	if err != nil || navigation.Position != 1 || navigation.Previous != nil || len(navigation.Posts) != 2 {
		t.Fatal("draft member remained in reader navigation", err)
	}
	stored, err := h.blog.series.GetSeries(t.Context(), value.ID)
	if err != nil || len(stored.Posts) != 3 || stored.Posts[0].ID != third.ID {
		t.Fatal("unpublishing discarded membership or order", err)
	}
	third.Draft = false
	third.Title, third.Slug = "Renamed third", "renamed-third"
	third, err = h.blog.persistPost(t.Context(), third)
	if err != nil {
		t.Fatal(err)
	}
	navigation, err = h.blog.seriesNavigation(t.Context(), first.ID)
	if err != nil || navigation.Previous.Title != third.Title || navigation.Previous.Slug != third.Slug {
		t.Fatal("republishing or renaming did not update navigation", err)
	}
	if err := h.blog.posts.Delete(t.Context(), third.ID); err != nil {
		t.Fatal(err)
	}
	navigation, err = h.blog.seriesNavigation(t.Context(), first.ID)
	if err != nil || navigation.Position != 1 || navigation.Previous != nil {
		t.Fatal("deleted post remained in series navigation", err)
	}
}

func TestSeriesAtomicityAndAuthorization(t *testing.T) {
	h := newHarness(t)
	first, second := seriesPost(t, h, "First"), seriesPost(t, h, "Second")
	admin := adminWritingContext(t.Context())
	value, err := h.blog.writeSeries(admin, Series{Title: "First series"}, []string{first.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.blog.writeSeries(admin, Series{Title: "Conflict"}, []string{second.ID, first.ID}); !errors.Is(err, errSeriesMembership) {
		t.Fatal("accepted a post in two series", err)
	}
	if current, err := h.blog.series.SeriesForPost(t.Context(), second.ID, false); err != nil || current != nil {
		t.Fatal("failed series save left partial membership", err)
	}
	choices, err := h.blog.series.SearchSeries(t.Context(), "First series", "")
	if err != nil || len(choices) != 1 {
		t.Fatal("failed save left a partial series", err)
	}
	value.Title = "Renamed"
	updated, err := h.blog.writeSeries(admin, *value, []string{second.ID, first.ID})
	if err != nil || updated.Version != value.Version+1 || updated.Posts[0].ID != second.ID {
		t.Fatal("unable to update order", err)
	}
	if _, err := h.blog.writeSeries(admin, *value, []string{first.ID}); !errors.Is(err, errEditConflict) {
		t.Fatal("stale series overwrote newer changes", err)
	}
	for _, invalid := range []seriesContent{{Title: "", Posts: []string{first.ID}}, {Title: "Series", Posts: []string{first.ID, first.ID}}, {Title: "Series", Posts: []string{"bad"}}, {Title: "Series"}} {
		if _, err := h.blog.writeSeries(admin, Series{Title: invalid.Title}, invalid.Posts); err == nil {
			t.Fatal("accepted invalid series", invalid)
		}
	}
	for _, role := range []string{"", "poster", "admin"} {
		actor := &user.User{ID: "other", Role: role}
		ctx := context.WithValue(t.Context(), user.UserContextKey, actor)
		_, err := h.blog.writeSeries(ctx, *updated, []string{first.ID})
		if role == "admin" && err != nil || role != "admin" && !errors.Is(err, errAccessDenied) {
			t.Fatalf("role %q: unexpected series access: %v", role, err)
		}
	}
	poster := &user.User{ID: "poster", Role: "poster"}
	ctx := context.WithValue(t.Context(), user.UserContextKey, poster)
	own, err := h.blog.persistPost(t.Context(), Post{Title: "Own", Markdown: "Body", AuthorID: poster.ID})
	if err != nil {
		t.Fatal(err)
	}
	owned, err := h.blog.writeSeries(ctx, Series{Title: "Own series", AuthorID: "forged"}, []string{own.ID})
	if err != nil || owned.AuthorID != poster.ID {
		t.Fatal("poster could not manage own series or forged ownership", err)
	}
	if _, err := h.blog.writeSeries(ctx, *owned, []string{own.ID, second.ID}); !errors.Is(err, errAccessDenied) {
		t.Fatal("poster changed another author's membership", err)
	}
	if saved, err := h.blog.writeSeries(ctx, *owned, []string{}); err != nil || saved != nil {
		t.Fatal("removing all members did not remove the series", err)
	}
}

func TestSeriesHTTPAndScheduling(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(strconv.FormatBool(hx), func(t *testing.T) {
			h := registeredBrowserHarness(t)
			h.login(t)
			start := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
			advance := publicationClock(h, start)
			first := seriesPost(t, h, "First")
			second, err := h.blog.persistPost(t.Context(), Post{Title: "Second", Markdown: "Body", Published: start.Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			path := "/post/series/" + first.ID
			requireStatus(t, h.request("GET", path+"?collection=new", nil, hx), http.StatusOK)
			content := componentJSON(seriesContent{Title: "Shared series", Posts: []string{first.ID, second.ID}})
			fields := postFields(first, true)
			fields.Set("seriesID", "new")
			fields.Set("seriesVersion", "0")
			fields.Set("series", content)
			w := h.request("POST", "/post/"+first.ID, fields, hx)
			if hx {
				requireStatus(t, w, http.StatusOK)
			} else {
				requireStatus(t, w, http.StatusSeeOther)
			}
			if w.Header().Get("HX-Trigger") != "blog:content-saved" {
				t.Fatal("save did not confirm success")
			}
			value, err := h.blog.series.SeriesForPost(t.Context(), first.ID, false)
			if err != nil || value == nil {
				t.Fatal("series not saved", err)
			}
			fields.Set("seriesID", value.ID)
			fields.Set("seriesVersion", strconv.FormatInt(value.Version, 10))
			fields.Set("series", `{"title":"Keep submitted text","posts":["bad"]}`)
			w = h.request("POST", "/post/"+first.ID, fields, hx)
			requireStatus(t, w, http.StatusUnprocessableEntity)
			if !hx && !strings.Contains(w.Body.String(), "Keep submitted text") {
				t.Fatal("validation discarded submitted content")
			}
			fields.Set("series", content)
			fields.Set("seriesVersion", "999")
			requireStatus(t, h.request("POST", "/post/"+first.ID, fields, hx), http.StatusConflict)
			for _, visible := range []bool{false, true} {
				if visible {
					advance(start.Add(time.Hour))
				}
				for _, path := range []string{"/post/s/first", "/rss"} {
					w := h.request("GET", path, nil, false)
					requireStatus(t, w, http.StatusOK)
					if strings.Contains(w.Body.String(), "Part 1 of 2") != visible {
						t.Fatalf("scheduled membership visibility incorrect in %s", path)
					}
				}
			}
			h.cookies = nil
			if status := h.request("GET", path, nil, hx).Code; status == http.StatusOK {
				t.Fatal("anonymous series management allowed")
			}
		})
	}
}

func TestSeriesConcurrentMembership(t *testing.T) {
	h := newHarness(t)
	post := seriesPost(t, h, "Shared target")
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := h.blog.writeSeries(adminWritingContext(t.Context()), Series{Title: "Concurrent series"}, []string{post.ID})
			errors <- err
		})
	}
	wg.Wait()
	close(errors)
	var successes int
	for err := range errors {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected one successful membership, got %d", successes)
	}
	all, err := h.blog.series.SearchSeries(t.Context(), "Concurrent series", "")
	if err != nil || len(all) != 1 {
		t.Fatal("concurrent conflict left a partial series", err)
	}
	available, err := h.blog.series.AvailableSeriesPosts(t.Context(), "", "")
	if err != nil || slices.ContainsFunc(available, func(candidate Post) bool { return candidate.ID == post.ID }) {
		t.Fatal("picker included a post already in another series", err)
	}
}

func TestSeriesConcurrentPostSaves(t *testing.T) {
	h := newHarness(t)
	first, second := seriesPost(t, h, "First"), seriesPost(t, h, "Second")
	ids := []string{first.ID, second.ID}
	value, err := h.blog.writeSeries(adminWritingContext(t.Context()), Series{Title: "Original series"}, ids)
	if err != nil {
		t.Fatal(err)
	}
	store := h.blog.posts.(*sqliteRepository)
	store.db.SetMaxOpenConns(3)
	value.Title = "Changed series"
	update := &seriesUpdate{Series: *value, Posts: ids}
	results := make(chan error, 8)
	var writers sync.WaitGroup
	for i := range 8 {
		writers.Go(func() {
			post := []Post{first, second}[i%2]
			_, err := store.SaveWithSeries(t.Context(), post, nil, update)
			results <- err
		})
	}
	writers.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, errEditConflict) {
			t.Fatal("a concurrent series save did not return an edit conflict", err)
		}
	}
	if winners != 1 {
		t.Fatal("concurrent series saves must have exactly one winner", winners)
	}
	for _, post := range []Post{first, second} {
		stored, err := store.GetByID(t.Context(), post.ID, true)
		if err != nil || stored != post {
			t.Fatal("a series-only save changed post content or version", stored, err)
		}
	}
}

func TestSeriesCapacityAndPrivateMetadata(t *testing.T) {
	h := usersHarness(t)
	var ids []string
	for i := range maxSeriesPosts {
		post := seriesPost(t, h, fmt.Sprintf("Capacity %d", i))
		ids = append(ids, post.ID)
	}
	value, err := h.blog.writeSeries(adminWritingContext(t.Context()), Series{Title: "Private series metadata"}, ids)
	if err != nil {
		t.Fatal(err)
	}
	choices, err := h.blog.series.SearchSeries(t.Context(), "Private series metadata", "")
	if err != nil || len(choices) != 0 {
		t.Fatal("a full series was offered for another post", err)
	}
	poster := registeredPoster(t, h)
	own, err := h.blog.persistPost(t.Context(), Post{Title: "Own source", Markdown: "Body", AuthorID: poster.ID})
	if err != nil {
		t.Fatal(err)
	}
	path := "/post/series/" + own.ID
	w := h.request("GET", path+"?collection="+value.ID, nil, false)
	requireStatus(t, w, http.StatusForbidden)
	if strings.Contains(w.Body.String(), value.Title) {
		t.Fatal("a guessed collection ID exposed another author's series metadata")
	}
	fields := postFields(own, true)
	fields.Set("title", "Rejected post edit")
	fields.Set("seriesID", value.ID)
	fields.Set("seriesVersion", "1")
	fields.Set("series", componentJSON(seriesContent{Title: "Forged", Posts: []string{own.ID}}))
	w = h.request("POST", "/post/"+own.ID, fields, true)
	requireStatus(t, w, http.StatusForbidden)
	if stored, err := h.blog.posts.GetByID(t.Context(), own.ID, true); err != nil || !reflect.DeepEqual(stored, own) {
		t.Fatal("a forbidden series edit saved the post", err)
	}
}

func TestSeriesJoinAndLeave(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	first, second := seriesPost(t, h, "First"), seriesPost(t, h, "Second")
	value, err := h.blog.writeSeries(adminWritingContext(t.Context()), Series{Title: "Shared series"}, []string{first.ID})
	if err != nil {
		t.Fatal(err)
	}
	path := "/post/series/" + second.ID
	w := h.request("GET", path+"?collection="+value.ID, nil, true)
	requireStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), first.ID) || !strings.Contains(w.Body.String(), second.ID) {
		t.Fatal("joining an existing series did not preserve its members and add the source post")
	}
	for _, ids := range [][]string{{first.ID, second.ID}, {second.ID}, {}} {
		fields := postFields(second, true)
		fields.Set("seriesID", value.ID)
		fields.Set("seriesVersion", strconv.FormatInt(value.Version, 10))
		fields.Set("series", componentJSON(seriesContent{Title: value.Title, Posts: ids}))
		requireStatus(t, h.request("POST", "/post/"+second.ID, fields, true), http.StatusOK)
		value, err = h.blog.series.SeriesForPost(t.Context(), second.ID, false)
		if err != nil || (value == nil) != (len(ids) == 0) {
			t.Fatal("joining or removing membership failed", err)
		}
	}
	if value, err := h.blog.series.SeriesForPost(t.Context(), first.ID, false); err != nil || value != nil {
		t.Fatal("a removed post retained its membership", err)
	}
}

func TestPostAndSeriesSaveAtomic(t *testing.T) {
	for _, failure := range []string{"none", "stale post", "stale series", "missing member", "invalid series", "foreign member"} {
		t.Run(failure, func(t *testing.T) {
			h := registeredBrowserHarness(t)
			first, second := seriesPost(t, h, "First"), seriesPost(t, h, "Second")
			ctx := adminWritingContext(t.Context())
			value, err := h.blog.writeSeries(ctx, Series{Title: "Original series"}, []string{first.ID, second.ID})
			if err != nil {
				t.Fatal(err)
			}
			post := first
			post.Markdown = "Edited post body"
			update := &seriesUpdate{Series: *value, Posts: []string{second.ID, first.ID}}
			update.Series.Title = "Edited series name"
			switch failure {
			case "stale post":
				post.Version++
			case "stale series":
				update.Series.Version++
			case "missing member":
				update.Posts = append(update.Posts, "AAAAAAAAAAAAAAAAAAAAAAAAAA")
			case "invalid series":
				update.Series.Title = ""
			case "foreign member":
				other := seriesPost(t, h, "Other")
				if _, err := h.blog.writeSeries(ctx, Series{Title: "Other series"}, []string{other.ID}); err != nil {
					t.Fatal(err)
				}
				update.Posts = append(update.Posts, other.ID)
			}
			_, err = h.blog.writePostWithSeries(ctx, post, update, 0)
			stored, readErr := h.blog.posts.GetByID(t.Context(), first.ID, true)
			series, seriesErr := h.blog.series.GetSeries(t.Context(), value.ID)
			history, historyErr := h.blog.history.ListRevisions(t.Context(), "post", first.ID)
			if readErr != nil || seriesErr != nil || historyErr != nil {
				t.Fatal(readErr, seriesErr, historyErr)
			}
			if failure == "none" {
				if err != nil || stored.Markdown != post.Markdown || stored.Version != first.Version+1 || series.Title != update.Series.Title || series.Posts[0].ID != second.ID || len(history) != 2 {
					t.Fatal("post and series were not committed together", err)
				}
			} else if err == nil || !reflect.DeepEqual(stored, first) || series.Title != value.Title || series.Version != value.Version || series.Posts[0].ID != first.ID || len(history) != 1 {
				t.Fatal("a rejected combined save changed post content, history, or series metadata", err)
			}
		})
	}
}

func TestNewPostAndSeriesSave(t *testing.T) {
	h := registeredBrowserHarness(t)
	ctx := adminWritingContext(t.Context())
	post, err := h.blog.writePostWithSeries(ctx, Post{Title: "New member", Markdown: "Body"}, &seriesUpdate{Series: Series{Title: "New series"}, Posts: []string{}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	value, err := h.blog.series.SeriesForPost(t.Context(), post.ID, false)
	if err != nil || value == nil || len(value.Posts) != 1 || value.Posts[0].ID != post.ID {
		t.Fatal("a new post was not included in its new series", err)
	}
	next, err := h.blog.writePostWithSeries(ctx, Post{Title: "Joined member", Markdown: "Body"}, &seriesUpdate{Series: *value, Posts: []string{post.ID}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	value, err = h.blog.series.SeriesForPost(t.Context(), next.ID, false)
	if err != nil || len(value.Posts) != 2 || value.Posts[1].ID != next.ID {
		t.Fatal("a new post did not join the existing series", err)
	}
	changed := *value
	changed.Title = "Renamed series"
	saved, err := h.blog.writePostWithSeries(ctx, next, &seriesUpdate{Series: changed, Posts: []string{post.ID, next.ID}}, 0)
	if err != nil || saved.Version != next.Version {
		t.Fatal("series-only edits changed the post version", err)
	}
	stale := *value
	stale.Title = "A rejected series"
	if _, err := h.blog.writePostWithSeries(ctx, Post{Title: "Rejected member", Markdown: "Body"}, &seriesUpdate{Series: stale, Posts: []string{post.ID, next.ID}}, 0); !errors.Is(err, errEditConflict) {
		t.Fatal("a stale series allowed creating a post", err)
	}
	if _, err := h.blog.posts.GetBySlug(t.Context(), "rejected-member", true); !errors.Is(err, errNotFound) {
		t.Fatal("a failed series save left a new post behind", err)
	}
}

func TestRemovedSeriesConflictRetainsPostEdits(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(strconv.FormatBool(hx), func(t *testing.T) {
			h := registeredBrowserHarness(t)
			h.login(t)
			post := seriesPost(t, h, "Original post")
			ctx := adminWritingContext(t.Context())
			value, err := h.blog.writeSeries(ctx, Series{Title: "Removed series"}, []string{post.ID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.blog.writeSeries(ctx, *value, []string{}); err != nil {
				t.Fatal(err)
			}
			fields := postFields(post, true)
			fields.Set("title", "Retained post edit")
			fields.Set("seriesID", value.ID)
			fields.Set("seriesVersion", strconv.FormatInt(value.Version, 10))
			fields.Set("series", componentJSON(seriesContent{Title: "Retained series edit", Posts: []string{post.ID}}))
			w := h.request("POST", "/post/"+post.ID, fields, hx)
			requireStatus(t, w, http.StatusConflict)
			if !strings.Contains(w.Body.String(), "Neither the post nor the series was saved") || !hx && (!strings.Contains(w.Body.String(), "Retained post edit") || !strings.Contains(w.Body.String(), "Retained series edit")) {
				t.Fatal("a removed series discarded submitted edits")
			}
			if stored, err := h.blog.posts.GetByID(t.Context(), post.ID, true); err != nil || !reflect.DeepEqual(stored, post) {
				t.Fatal("a removed series left a saved post edit", err)
			}
		})
	}
}

func TestUnchangedSeriesLeavesSharedDataAlone(t *testing.T) {
	for _, hx := range []bool{false, true} {
		for _, change := range []string{"renamed", "reordered", "joined", "removed", "moved"} {
			t.Run(fmt.Sprintf("%t/%s", hx, change), func(t *testing.T) {
				h := registeredBrowserHarness(t)
				ctx := adminWritingContext(t.Context())
				h.login(t)
				first, second := seriesPost(t, h, "First"), seriesPost(t, h, "Second")
				fields := postFields(first, true)
				fields.Set("title", "Updated writing")
				fields.Set("seriesID", "")
				fields.Set("seriesVersion", "0")
				fields.Set("seriesBaseline", seriesBaseline("", ""))
				if change == "joined" {
					if _, err := h.blog.writeSeries(ctx, Series{Title: "Joined elsewhere"}, []string{second.ID, first.ID}); err != nil {
						t.Fatal(err)
					}
				} else {
					value, err := h.blog.writeSeries(ctx, Series{Title: "Original series"}, []string{first.ID, second.ID})
					if err != nil {
						t.Fatal(err)
					}
					source := componentJSON(seriesSelection(value))
					fields.Set("seriesID", value.ID)
					fields.Set("seriesVersion", strconv.FormatInt(value.Version, 10))
					fields.Set("seriesBaseline", seriesBaseline(value.ID, source))
					// A formatting change or a reverted title edit is still unchanged.
					fields.Set("series", fmt.Sprintf("{\n\"posts\":[%q,%q],\"title\":\" Original series \"\n}", first.ID, second.ID))
					ids := []string{first.ID, second.ID}
					switch change {
					case "renamed":
						value.Title = "Renamed elsewhere"
					case "reordered":
						ids = []string{second.ID, first.ID}
					case "removed":
						ids = []string{}
					case "moved":
						ids = []string{second.ID}
					}
					if _, err := h.blog.writeSeries(ctx, *value, ids); err != nil {
						t.Fatal(err)
					}
					if change == "moved" {
						if _, err := h.blog.writeSeries(ctx, Series{Title: "Moved elsewhere"}, []string{first.ID}); err != nil {
							t.Fatal(err)
						}
					}
				}
				before, err := h.blog.series.SeriesForPost(t.Context(), first.ID, false)
				if err != nil {
					t.Fatal(err)
				}
				w := h.request("POST", "/post/"+first.ID, fields, hx)
				status := http.StatusSeeOther
				if hx {
					status = http.StatusOK
				}
				requireStatus(t, w, status)
				stored, err := h.blog.posts.GetByID(t.Context(), first.ID, true)
				if err != nil || stored.Title != "Updated writing" {
					t.Fatal("an unchanged stale series blocked saving the post", err)
				}
				after, err := h.blog.series.SeriesForPost(t.Context(), first.ID, false)
				if err != nil || (before == nil) != (after == nil) {
					t.Fatal("saving an untouched series changed membership", err)
				}
				if before != nil && (before.ID != after.ID || before.Title != after.Title || before.Version != after.Version || !reflect.DeepEqual(seriesSelection(before), seriesSelection(after))) {
					t.Fatal("saving an untouched series overwrote shared data", before, after)
				}
			})
		}
	}
}

func TestEditedSeriesStillChecksConcurrentChanges(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(strconv.FormatBool(hx), func(t *testing.T) {
			h := registeredBrowserHarness(t)
			h.login(t)
			ctx := adminWritingContext(t.Context())
			post := seriesPost(t, h, "Original writing")
			value, err := h.blog.writeSeries(ctx, Series{Title: "Original series"}, []string{post.ID})
			if err != nil {
				t.Fatal(err)
			}
			fields := postFields(post, true)
			fields.Set("title", "My writing edit")
			fields.Set("seriesID", value.ID)
			fields.Set("seriesVersion", strconv.FormatInt(value.Version, 10))
			fields.Set("seriesBaseline", seriesBaseline(value.ID, componentJSON(seriesSelection(value))))
			fields.Set("series", componentJSON(seriesContent{Title: "My series edit", Posts: []string{post.ID}}))
			value.Title = "Another editor's series edit"
			if _, err := h.blog.writeSeries(ctx, *value, []string{post.ID}); err != nil {
				t.Fatal(err)
			}
			w := h.request("POST", "/post/"+post.ID, fields, hx)
			requireStatus(t, w, http.StatusConflict)
			if !strings.Contains(w.Body.String(), "Click here") || !hx && (!strings.Contains(w.Body.String(), "My series edit") || !strings.Contains(w.Body.String(), `name="seriesBaseline" value="`+fields.Get("seriesBaseline")+`"`)) {
				t.Fatal("conflict did not preserve the original baseline or offer recovery")
			}
			if !strings.Contains(w.Body.String(), "data-series-reload-summary") || !strings.Contains(w.Body.String(), "to replace your unsaved series edits with the current version of the series. Your post edits will be kept.") {
				t.Fatal("the conflict message did not include its recovery action and explanation")
			}
			if strings.Contains(w.Body.String(), "Load current series") || strings.Contains(w.Body.String(), "series-reload-help") {
				t.Fatal("the series editor duplicated conflict recovery")
			}
			if stored, err := h.blog.posts.GetByID(t.Context(), post.ID, true); err != nil || stored.Title != post.Title {
				t.Fatal("a conflicting series saved part of the post", err)
			}
		})
	}
}
