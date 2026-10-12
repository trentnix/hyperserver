package form

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/pkg/components/content"
)

// These forms deliberately do not embed Form or provide template/message helpers.
type validationOnlyForm struct {
	value     string
	fields    FieldErrors
	validated bool
}

func (f *validationOnlyForm) Validate() FieldErrors {
	if f.value == "" {
		return FieldErrors{"Value": {RequiredMessage}}
	}
	return nil
}

func (f *validationOnlyForm) SetFieldErrors(fields FieldErrors) { f.fields = fields }
func (f *validationOnlyForm) SetValidated(validated bool)       { f.validated = validated }

type bindingValidationForm struct {
	validationOnlyForm
	bindErr error
}

func (f *bindingValidationForm) Bind(r *http.Request) error {
	f.value = r.PostForm.Get("value")
	return f.bindErr
}

type renderingOnlyForm struct{ Message string }

func (f *renderingOnlyForm) AddErrorMessage(message string) { f.Message = message }

// A named map implements the contract without being a struct or pointer.
type mapForm map[string]any

func (f mapForm) Bind(r *http.Request) error {
	f["value"] = r.PostForm.Get("value")
	return nil
}

func (f mapForm) Validate() FieldErrors {
	if f["value"] == nil || f["value"] == "" {
		return FieldErrors{"Value": {RequiredMessage}}
	}
	return nil
}

func (f mapForm) SetFieldErrors(fields FieldErrors) { f["errors"] = fields }
func (f mapForm) SetValidated(validated bool)       { f["validated"] = validated }

var (
	_ ValidationForm = (*validationOnlyForm)(nil)
	_ FormComponent  = (*bindingValidationForm)(nil)
	_ ErrorForm      = (*renderingOnlyForm)(nil)
	_ FormComponent  = mapForm(nil)
	// Keep validation's public signature typed, not merely its implementation.
	_ func(ValidationForm) error = Validate
)

func TestValidationDoesNotRequireBindingOrMessages(t *testing.T) {
	f := &validationOnlyForm{}
	if err := Validate(f); err != nil {
		t.Fatal(err)
	}
	if !f.validated || !reflect.DeepEqual(f.fields, FieldErrors{"Value": {RequiredMessage}}) {
		t.Fatalf("validation results: %+v", f)
	}
	f.value = "corrected"
	if err := Validate(f); err != nil || len(f.fields) != 0 {
		t.Fatalf("corrected validation: fields=%v, err=%v", f.fields, err)
	}
}

func TestParseAndValidateMinimalContract(t *testing.T) {
	failure := errors.New("binding failed")
	for _, tc := range []struct {
		name, body string
		bindErr    error
		wantError  bool
		wantFields FieldErrors
	}{
		{name: "valid", body: "value=present"},
		{name: "invalid", body: "value=", wantFields: FieldErrors{"Value": {RequiredMessage}}},
		{name: "parse failure", body: "value=%zz", wantError: true},
		{name: "binding failure", body: "value=present", bindErr: failure, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &bindingValidationForm{bindErr: tc.bindErr}
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			message, err := ParseAndValidate(r, f)
			if (err != nil) != tc.wantError || (message != "") != tc.wantError {
				t.Fatalf("result = %q, %v", message, err)
			}
			if tc.bindErr != nil && !errors.Is(err, tc.bindErr) {
				t.Fatalf("lost binding error: %v", err)
			}
			if f.validated == tc.wantError || !reflect.DeepEqual(f.fields, tc.wantFields) {
				t.Fatalf("validation results: %+v", f)
			}
		})
	}
}

func TestParseAndValidateRejectsNilBeforeBinding(t *testing.T) {
	var typedNil *bindingValidationForm
	for _, f := range []FormComponent{nil, typedNil, mapForm(nil)} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("value=present"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		message, err := ParseAndValidate(r, f)
		var invalid *ErrInvalidForm
		if message == "" || !errors.As(err, &invalid) {
			t.Fatalf("invalid form result = %q, %v", message, err)
		}
	}
}

func TestMapForm(t *testing.T) {
	var nilMap mapForm
	var invalid *ErrInvalidForm
	if err := Validate(nilMap); !errors.As(err, &invalid) {
		t.Fatalf("nil map validation = %v, want ErrInvalidForm", err)
	}

	f := make(mapForm)
	if err := Validate(f); err != nil {
		t.Fatal(err)
	}
	if f["validated"] != true || !reflect.DeepEqual(f["errors"], FieldErrors{"Value": {RequiredMessage}}) {
		t.Fatalf("map validation = %v", f)
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("value=corrected"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if message, err := ParseAndValidate(r, f); err != nil || message != "" {
		t.Fatalf("map form parsing = %q, %v", message, err)
	}
	if f["value"] != "corrected" || f["validated"] != true || len(f["errors"].(FieldErrors)) != 0 {
		t.Fatalf("map form did not record corrected input: %v", f)
	}
}

func TestValueFormWrapper(t *testing.T) {
	f := struct{ *bindingValidationForm }{&bindingValidationForm{}}
	if err := Validate(f); err != nil {
		t.Fatal(err)
	}
	if !f.validated || !reflect.DeepEqual(f.fields, FieldErrors{"Value": {RequiredMessage}}) {
		t.Fatalf("value wrapper did not record validation: %+v", f)
	}

	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("value=corrected"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if message, err := ParseAndValidate(r, f); err != nil || message != "" {
		t.Fatalf("value wrapper parsing = %q, %v", message, err)
	}
	if f.value != "corrected" || !f.validated || len(f.fields) != 0 {
		t.Fatalf("value wrapper did not record corrected input: %+v", f)
	}
}

func TestFormErrorDoesNotRequireBindingOrValidation(t *testing.T) {
	for _, hx := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		if hx {
			r.Header.Set("HX-Request", "true")
		}
		c := content.NewContent(r)
		c.AddContent(fstest.MapFS{"error.html": {Data: []byte(`<p>{{.Data.Message}}</p>`)}}, "error.html")
		f := &renderingOnlyForm{}
		w := httptest.NewRecorder()
		HandleFormError(w, r, c, f, "<invalid>", nil)
		if w.Code != http.StatusOK || w.Body.String() != "<p>&lt;invalid&gt;</p>" || c.Data != f {
			t.Fatalf("rendering-only form: status=%d, body=%q, data=%T", w.Code, w.Body.String(), c.Data)
		}
	}
}
