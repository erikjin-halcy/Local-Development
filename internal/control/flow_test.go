package control

import "testing"

func TestPromptInput(t *testing.T) {
	rule := "\x1b[38;5;244m" + "──────────────────────────────" + "\n"
	cases := []struct {
		name, capture, want string
		ok                  bool
	}{
		{"tom", "svar\n" + rule + "\x1b[39m❯ \n" + rule + "  ⏵⏵ auto mode on\n", "", true},
		{"text", "svar\n" + rule + "\x1b[39m❯ s\n" + rule + "  ⏵⏵ auto mode on\n", "s", true},
		{"grå förslagstext", rule + "❯ \x1b[2mTry \"fix lint errors\"\x1b[22m\n" + rule, "", true},
		{"flera rader", rule + "❯ första\n  andra\n" + rule, "första\nandra", true},
		{"ingen ruta", "bara text\n$ ls\n", "", false},
	}
	for _, c := range cases {
		got, ok := promptInput(c.capture)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: fick %q,%v; vill ha %q,%v", c.name, got, ok, c.want, c.ok)
		}
	}
}
