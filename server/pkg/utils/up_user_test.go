package utils

import (
	"testing"

	"github.com/spf13/viper"
)

func TestUPEmailAndUsername(t *testing.T) {
	viper.Set("unplugged.email-host", "msg.example")
	t.Cleanup(viper.Reset)

	valid := map[string]string{
		"alice":     "alice@msg.example",
		"bob.smith": "bob.smith@msg.example",
	}
	for username, want := range valid {
		got, err := UPEmail(username)
		if err != nil || got != want {
			t.Errorf("UPEmail(%q) = %q, %v; want %q", username, got, err, want)
		}
		back, ok := UPUsernameFromEmail(got)
		if !ok || back != username {
			t.Errorf("UPUsernameFromEmail(%q) = %q, %v; want %q", got, back, ok, username)
		}
	}

	for _, username := range []string{"", "alice@msg.example", "victim@gmail.com", "a b", "alice\n"} {
		if got, err := UPEmail(username); err == nil {
			t.Errorf("UPEmail(%q) = %q, want error", username, got)
		}
	}

	for _, email := range []string{"alice@gmail.com", "alice@msg.example.evil", "@msg.example", "a@b@msg.example", "alice"} {
		if got, ok := UPUsernameFromEmail(email); ok {
			t.Errorf("UPUsernameFromEmail(%q) = %q, want not a UP email", email, got)
		}
	}
}
