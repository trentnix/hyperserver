package content

import (
	"sync"
)

type (
	ContentManager struct {
		ContentTemplates map[string][]Template
	}
)

var (
	contentManager *ContentManager
	once           sync.Once
)

func GetContentManager() *ContentManager {
	once.Do(func() {
		contentManager = &ContentManager{
			// initialize here
		}
	})

	return contentManager
}

func (c *ContentManager) RegisterTemplates(contentType string, templates []Template) {
	c.ContentTemplates[contentType] = templates
}

func (c *ContentManager) AddTemplates(contentType string, templates []Template) {
	c.ContentTemplates[contentType] = templates
}
