// util.go contains utility functions that are used in the application
package util

import (
	"fmt"
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
