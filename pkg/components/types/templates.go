// Package types defines template paths shared by rendering components and services.
package types

import "io/fs"

// TemplatePath identifies one template in an explicit filesystem. Path must be a
// valid fs path, relative to FS. Use embed.FS, os.DirFS, or another fs.FS implementation.
type TemplatePath struct {
	FS   fs.FS
	Path string
}
