package phone

import "testing"

func TestNormalize(t *testing.T) {
	t.Run("cleans separators, spaces and a leading zero", func(t *testing.T) {
		cc, num, msg := Normalize(" +880 ", "01712-345 678")
		if msg != "" || cc != "+880" || num != "1712345678" {
			t.Errorf("got (%q, %q, %q)", cc, num, msg)
		}
	})

	t.Run("brackets are stripped too", func(t *testing.T) {
		if _, num, msg := Normalize("+1", "(415) 555-2671"); msg != "" || num != "4155552671" {
			t.Errorf("got (%q, %q)", num, msg)
		}
	})

	t.Run("only one leading zero is removed", func(t *testing.T) {
		if _, num, _ := Normalize("+880", "0012345678"); num != "012345678" {
			t.Errorf("got %q", num)
		}
	})

	bad := map[string][2]string{
		"missing country":   {"", "1712345678"},
		"country without +": {"880", "1712345678"},
		"country too long":  {"+88012", "1712345678"},
		"letters in number": {"+880", "017abc45678"},
		"too short":         {"+880", "12345"},
		"too long":          {"+880", "1234567890123456"},
		"empty number":      {"+880", ""},
	}
	for name, in := range bad {
		t.Run(name, func(t *testing.T) {
			if _, _, msg := Normalize(in[0], in[1]); msg == "" {
				t.Errorf("expected an error for %v", in)
			}
		})
	}
}
