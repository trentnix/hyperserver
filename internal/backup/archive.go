package backup

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/internal/maintenance"
)

const (
	formatVersion       = 1
	maxFiles            = 100000
	maxBytes      int64 = 64 << 30
	maxManifest         = 16 << 20
)

type entry struct {
	Path   string
	Size   int64
	SHA256 string
}

type manifest struct {
	Format      int
	Created     time.Time
	Resources   []resource
	Directories []string
	Files       []entry
}

// Create archives a stopped blog without replacing an existing backup.
// Configuration is copied unchanged. Environment overrides must be saved separately.
func Create(ctx context.Context, cfg config.Config, destination string) (err error) {
	lock, err := maintenance.Lock(cfg.App.WorkingDirectory)
	if err != nil {
		return err
	}
	defer lock.Close()
	files, err := resources(ctx, cfg)
	if err != nil {
		return err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(destination))
	if err != nil {
		return err
	}
	destination = filepath.Join(parent, filepath.Base(destination))
	for _, file := range files {
		source, err := filepath.EvalSymlinks(file.Source)
		if err != nil {
			return err
		}
		if within(source, destination) {
			return errors.New("backup destination must be outside its source files and directories")
		}
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(destination)
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(destination)
		}
	}()
	stage, err := os.MkdirTemp(parent, ".blog-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	data := filepath.Join(stage, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		return err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(data, filepath.FromSlash(file.Path))
		switch {
		case len(file.Roles) != 0:
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			if err := snapshot(ctx, file.Source, target); err != nil {
				return fmt.Errorf("snapshot %s: %w", file.Path, err)
			}
			if err := os.Chmod(target, 0600); err != nil {
				return err
			}
		case file.Path == "uploads" || file.Path == "assets":
			if err := copyTree(ctx, file.Source, target); err != nil {
				return err
			}
		default:
			if err := copyFile(ctx, file.Source, target); err != nil {
				return err
			}
		}
	}
	archive := filepath.Join(stage, "backup.zip")
	if err := writeArchive(ctx, archive, data, manifest{Format: formatVersion, Created: time.Now().UTC(), Resources: files}); err != nil {
		return err
	}
	if err := verify(ctx, archive, stage); err != nil {
		return fmt.Errorf("verify new backup: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(archive, destination)
}

// Verify checks inventory, checksums, SQLite integrity, and schema versions without restoring
// files or loading archived configuration. Restore only archives you trust.
func Verify(ctx context.Context, archive string) error {
	stage, err := os.MkdirTemp("", "blog-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	return verify(ctx, archive, stage)
}

func writeArchive(ctx context.Context, filename, directory string, m manifest) (err error) {
	out, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	writer := zip.NewWriter(out)
	defer writer.Close()
	var total int64
	err = filepath.WalkDir(directory, func(filename string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if filename == directory {
			return nil
		}
		relative, _ := filepath.Rel(directory, filename)
		name := filepath.ToSlash(relative)
		if d.IsDir() {
			m.Directories = append(m.Directories, name)
			if len(m.Directories)+len(m.Files) > maxFiles {
				return errors.New("too many archive entries")
			}
			header := &zip.FileHeader{Name: name + "/"}
			header.SetMode(os.ModeDir | 0700)
			_, err := writer.CreateHeader(header)
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("backup includes a non-regular file")
		}
		total += info.Size()
		if total > maxBytes || len(m.Files)+len(m.Directories) >= maxFiles {
			return errors.New("backup exceeds 64 GiB or 100,000 files")
		}
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetMode(0600)
		header.SetModTime(info.ModTime())
		target, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		source, err := os.Open(filename)
		if err != nil {
			return err
		}
		hash := sha256.New()
		size, err := io.Copy(io.MultiWriter(target, hash), contextReader{ctx, source})
		source.Close()
		if err != nil {
			return err
		}
		m.Files = append(m.Files, entry{Path: name, Size: size, SHA256: hex.EncodeToString(hash.Sum(nil))})
		return nil
	})
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if len(encoded) > maxManifest {
		return errors.New("backup manifest is too large")
	}
	target, err := writer.Create("manifest.json")
	if err != nil {
		return err
	}
	if _, err := target.Write(encoded); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	return out.Close()
}

// verify streams file checks and uses one temporary file for database integrity checks.
func verify(ctx context.Context, filename, stage string) error {
	reader, err := zip.OpenReader(filename)
	if err != nil {
		return err
	}
	defer reader.Close()
	if len(reader.File) > maxFiles+1 {
		return errors.New("too many archive entries")
	}
	files := make(map[string]*zip.File)
	for _, file := range reader.File {
		name := strings.TrimSuffix(file.Name, "/")
		if !safeName(name) || files[name] != nil ||
			(!file.Mode().IsRegular() && !file.Mode().IsDir()) {
			return errors.New("archive contains an unsafe, duplicate, or non-regular entry")
		}
		files[name] = file
	}
	file := files["manifest.json"]
	if file == nil || file.UncompressedSize64 > maxManifest {
		return errors.New("missing or oversized backup manifest")
	}
	input, err := file.Open()
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx, input}, maxManifest+1))
	input.Close()
	if err != nil {
		return err
	}
	if len(data) > maxManifest {
		return errors.New("oversized backup manifest")
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return errors.New("invalid backup manifest")
	}
	if m.Format != formatVersion {
		return fmt.Errorf("unsupported backup format %d: supported format is %d", m.Format, formatVersion)
	}
	if len(m.Files)+len(m.Directories) != len(files)-1 {
		return errors.New("incomplete backup inventory")
	}
	databaseRoles := make(map[string][]string)
	roles := make(map[string]bool)
	resources := make(map[string]bool)
	for _, resource := range m.Resources {
		file := files[resource.Path]
		if file == nil || resource.Source == "" || resources[resource.Path] {
			return errors.New("invalid resource inventory")
		}
		resources[resource.Path] = true
		for _, role := range resource.Roles {
			if roles[role] || !strings.HasPrefix(resource.Path, "databases/") || !file.Mode().IsRegular() ||
				(role != "blog" && role != "accounts" && role != "sessions") {
				return errors.New("invalid database inventory")
			}
			roles[role] = true
		}
		databaseRoles[resource.Path] = resource.Roles
	}
	if !roles["blog"] || !roles["accounts"] || !roles["sessions"] {
		return errors.New("backup is missing a database role")
	}
	expected := map[string]bool{"manifest.json": true}
	for _, name := range m.Directories {
		file := files[name]
		if !safeName(name) || expected[name] || file == nil || !file.Mode().IsDir() {
			return errors.New("invalid directory inventory")
		}
		expected[name] = true
	}
	var total int64
	for _, item := range m.Files {
		file := files[item.Path]
		if !safeName(item.Path) || expected[item.Path] || file == nil || !file.Mode().IsRegular() ||
			item.Size < 0 || item.Size > maxBytes || file.UncompressedSize64 != uint64(item.Size) {
			return errors.New("invalid file inventory")
		}
		total += item.Size
		if total > maxBytes {
			return errors.New("backup exceeds 64 GiB")
		}
		expected[item.Path] = true
		digest, err := hex.DecodeString(item.SHA256)
		if err != nil || len(digest) != sha256.Size {
			return errors.New("invalid checksum")
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		hash := sha256.New()
		var output io.Writer = hash
		var database *os.File
		// Database entries always have fixed names, independent of the manifest's source paths.
		if strings.HasPrefix(item.Path, "databases/") {
			if len(databaseRoles[item.Path]) == 0 {
				return errors.New("database snapshot is missing its role")
			}
			database, err = os.OpenFile(filepath.Join(stage, "check.db"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
			if err != nil {
				in.Close()
				return err
			}
			output = io.MultiWriter(hash, database)
		}
		size, copyErr := io.Copy(output, contextReader{ctx, io.LimitReader(in, item.Size+1)})
		in.Close()
		if database != nil {
			copyErr = errors.Join(copyErr, database.Close())
		}
		if copyErr != nil {
			return copyErr
		}
		if size != item.Size || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
			return errors.New("backup checksum mismatch")
		}
		if database != nil {
			db, err := openDatabase(database.Name())
			if err != nil {
				return err
			}
			err = checkDatabase(ctx, db)
			if err == nil {
				err = checkCompatibility(ctx, db, databaseRoles[item.Path])
			}
			db.Close()
			if err != nil {
				return fmt.Errorf("verify %s: %w", item.Path, err)
			}
		}
	}
	for _, name := range []string{"config.yaml", "modules/blog/config.yaml", "databases/blog.db", "uploads"} {
		if !expected[name] {
			return fmt.Errorf("backup is missing %s", name)
		}
	}
	for _, resource := range m.Resources {
		if !expected[resource.Path] {
			return errors.New("invalid resource inventory")
		}
	}
	return nil
}

func safeName(name string) bool {
	return name != "." && filepath.IsLocal(name) && path.Clean(name) == name &&
		!strings.ContainsAny(name, "\\\x00:") && !strings.HasSuffix(name, "/")
}
