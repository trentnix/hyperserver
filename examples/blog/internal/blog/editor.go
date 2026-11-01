package blog

// publicationActions names the outcomes available from the saved publication state.
type publicationActions struct {
	Draft, Publish, Schedule string
}

// editorActions distinguishes saving changes from changing publication state.
func editorActions(kind string, published, scheduled bool) publicationActions {
	actions := publicationActions{Draft: "Save draft", Publish: "Publish " + kind, Schedule: "Schedule post"}
	if published || scheduled {
		actions.Draft = "Move to drafts"
	}
	if published {
		actions.Publish = "Save changes"
	}
	if scheduled {
		actions.Schedule = "Save schedule"
	}
	return actions
}

// Label supplies the initial button text before the browser handles selection changes.
func (a publicationActions) Label(publication string) string {
	switch publication {
	case "on":
		return a.Publish
	case "scheduled":
		return a.Schedule
	default:
		return a.Draft
	}
}
