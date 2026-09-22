package contact

import "testing"

func TestNormalize(t *testing.T) {
	ok := CreateContactRequest{Name: "  Ayesha Rahman ", Relation: " Mother ", Phone: "01712-345 678", CountryCode: " +880 "}

	t.Run("cleans valid input", func(t *testing.T) {
		got, msg := normalize(ok)
		if msg != "" {
			t.Fatalf("unexpected error: %s", msg)
		}
		want := CreateContactRequest{Name: "Ayesha Rahman", Relation: "Mother", Phone: "1712345678", CountryCode: "+880"}
		if got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("relation is optional", func(t *testing.T) {
		r := ok
		r.Relation = ""
		if _, msg := normalize(r); msg != "" {
			t.Errorf("unexpected error: %s", msg)
		}
	})

	bad := map[string]func(*CreateContactRequest){
		"blank name":        func(r *CreateContactRequest) { r.Name = "   " },
		"name too long":     func(r *CreateContactRequest) { r.Name = string(make([]rune, 61)) + "x" },
		"relation too long": func(r *CreateContactRequest) { r.Relation = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" },
		"missing country":   func(r *CreateContactRequest) { r.CountryCode = "" },
		"country without +": func(r *CreateContactRequest) { r.CountryCode = "880" },
		"country too long":  func(r *CreateContactRequest) { r.CountryCode = "+88012" },
		"phone has letters": func(r *CreateContactRequest) { r.Phone = "017abc45678" },
		"phone too short":   func(r *CreateContactRequest) { r.Phone = "12345" },
		"phone too long":    func(r *CreateContactRequest) { r.Phone = "1234567890123456" },
		"phone empty":       func(r *CreateContactRequest) { r.Phone = "" },
	}
	for name, mutate := range bad {
		t.Run(name, func(t *testing.T) {
			r := ok
			mutate(&r)
			if _, msg := normalize(r); msg == "" {
				t.Errorf("expected a validation error for %q", name)
			}
		})
	}
}
