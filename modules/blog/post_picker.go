package blog

// postPickerData configures the shared ordered post selector. Its serialized
// value belongs to its consumer, not to the picker or the component catalog.
type postPickerData struct {
	ID, Name, Source, TitleLabel string
	MaxPosts                     int
	OptionalTitle                bool
}
