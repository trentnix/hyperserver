package deployment

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestUploadTemporaryStorageConfiguration(t *testing.T) {
	data, err := os.ReadFile("../../../deploy/local/compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			ReadOnly bool `yaml:"read_only"`
			Tmpfs    []string
		}
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatal(err)
	}
	app := compose.Services["app"]
	if !app.ReadOnly || !slices.Contains(app.Tmpfs, "/tmp:size=64m,mode=1777") {
		t.Fatal("the read-only blog needs bounded, writable temporary upload storage")
	}
}

// Exercise multipart disk spill through the real proxy and read-only container.
func TestDeploymentUpload(t *testing.T) {
	dir, client := deployment(t)
	command(t, dir, "docker", "compose", "up", "-d", "--wait")
	loginAdministrator(t, client)
	img := image.NewRGBA(image.Rect(0, 0, 640, 640))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	var original bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err := encoder.Encode(&original, img); err != nil {
		t.Fatal(err)
	}
	if original.Len() <= 1<<20 || original.Len() > maxResponseBytes {
		t.Fatalf("fixture must exceed multipart memory threshold: %d bytes", original.Len())
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "large-upload.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(original.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequestWithContext(t.Context(), http.MethodPost, origin+"/media/upload", &body)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("Origin", origin)
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	location := response.Header.Get("Location")
	if response.StatusCode != http.StatusSeeOther || !strings.HasPrefix(location, "/media/") {
		message, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
		t.Fatalf("large upload: %d %s", response.StatusCode, message)
	}
	response, downloaded := request(t, client, "/images/"+strings.TrimPrefix(location, "/media/")+"?variant=original", nil, false, false)
	if response.StatusCode != http.StatusOK || downloaded != original.String() {
		t.Fatal("uploaded original was not preserved")
	}
}
