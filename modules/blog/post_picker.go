package blog

// postPickerData configures the ordered picker shared by series and related content.
type postPickerData struct {
	ID, Name, Source, TitleLabel string
	MaxPosts                     int
	OptionalTitle, IncludePages  bool
}
