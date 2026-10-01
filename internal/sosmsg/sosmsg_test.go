package sosmsg

import "testing"

func TestLooksLikeContactInfo(t *testing.T) {
	yes := []string{
		"call me on 01712345678",
		"my number is 017 1234 5678",
		"+880 1712-345-678",
		"add me on whatsapp",
		"find me @some_handle",
		"Telegram?",
	}
	no := []string{
		"I'm 2 minutes away",
		"Which gate are you at?",
		"I'm near building 12, floor 3",
		"On my way, ETA 5 min",
		"",
	}
	for _, s := range yes {
		if !LooksLikeContactInfo(s) {
			t.Errorf("expected flagged: %q", s)
		}
	}
	for _, s := range no {
		if LooksLikeContactInfo(s) {
			t.Errorf("expected NOT flagged: %q", s)
		}
	}
}
