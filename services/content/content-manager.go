package content

import (
	"sync"
)

type (
	ContentManagerService struct {
		ContentTemplates map[string][]Template
	}
)

var (
	contentManager *ContentManagerService
	once           sync.Once
)

func GetContentManager() *ContentManagerService {
	once.Do(func() {
		contentManager = &ContentManagerService{
			// initialize here
		}
	})

	return contentManager
}

func (c *ContentManagerService) RegisterTemplates(contentType string, templates []Template) {
	c.ContentTemplates[contentType] = templates
}

func (c *ContentManagerService) AddTemplates(contentType string, templates []Template) {
	c.ContentTemplates[contentType] = templates
}
