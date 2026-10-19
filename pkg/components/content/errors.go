package content

import "errors"

var (
	// ErrNoTemplates indicates no templates are selected for rendering
	ErrNoTemplates = errors.New("no templates specified")
	// ErrParsingTemplates reports a failure parsing template files.
	ErrParsingTemplates = errors.New("unable to parse the specified templates")
	// ErrRenderingTemplates indicates an error rendering the specified templates
	ErrRenderingTemplates = errors.New("unable to render the specified templates")
	// ErrDeliveringResponse means headers were sent and body delivery failed.
	// Log the error without attempting another response. The cause is unwrapped.
	ErrDeliveringResponse = errors.New("unable to deliver the response")
)
