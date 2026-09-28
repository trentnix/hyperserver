package form

import "testing"

func TestHasErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*Form)
		want      bool
		formError bool
	}{
		{name: "empty", configure: func(*Form) {}},
		{name: "success", configure: func(f *Form) { f.AddSuccessMessage("Saved") }},
		{name: "information", configure: func(f *Form) { f.AddMessage("Try again") }},
		{name: "empty field errors", configure: func(f *Form) { f.fieldErrors = map[string][]string{"Email": nil} }},
		{name: "field error", configure: func(f *Form) { f.fieldErrors = map[string][]string{"Email": {"Invalid email"}} }, want: true},
		{name: "form error", configure: func(f *Form) { f.AddErrorMessage("Unable to save") }, want: true, formError: true},
		{name: "both", configure: func(f *Form) {
			f.fieldErrors = map[string][]string{"Email": {"Invalid email"}}
			f.AddErrorMessage("Unable to save")
		}, want: true, formError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var f Form
			tc.configure(&f)
			if got := f.HasErrors(); got != tc.want {
				t.Errorf("HasErrors() = %v, want %v", got, tc.want)
			}
			if got := f.HasErrorMessages(); got != tc.formError {
				t.Errorf("HasErrorMessages() = %v, want %v", got, tc.formError)
			}
		})
	}
}
