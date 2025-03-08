// util.go contains utility functions that are used in the application
package util

import (
	"fmt"
	"html/template"
	"os"
)

// GetWorkingDirectory will retrieve the current working directory
func GetWorkingDirectory() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", NewErrFailedToGetWorkingDirectory(nil)
	}

	return wd, nil
}

// SetWorkingDirectory will set the application's working directory to the specified path
func SetWorkingDirectory(wd string) error {
	if wd == "" {
		return NewErrWorkingDirectoryNotSpecified(fmt.Errorf("SetWorkingDirectory"))
	}

	if err := os.Chdir(wd); err != nil {
		return NewErrFailedToSetWorkingDirectory(err)
	}

	return nil
}

// LoadHTMLFromFile takes the file at the specified filePath and returns a template.HTML object with
// its contents
func LoadHTMLFromFile(filePath string) (template.HTML, error) {
	htmlBytes, err := os.ReadFile(filePath)
	if err != nil {
		return "", NewErrLoadingTemplate(err, filePath)
	}

	return template.HTML(htmlBytes), nil
}
