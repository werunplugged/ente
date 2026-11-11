package utils

import (
	"errors"
	"testing"

	"github.com/ente/museum/ente"

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

func TestUPChangeEmailTarget(t *testing.T) {
	viper.Set("unplugged.email-host", "msg.example")
	t.Cleanup(viper.Reset)

	allowed := []struct{ current, requested, want string }{
		// Legacy bare-username account, repaired to the canonical email.
		{"alice", "alice", "alice@msg.example"},
		{"alice", "alice@msg.example", "alice@msg.example"},
		// UP account naming its own email (UpdateEmail then rejects it as taken).
		{"alice@msg.example", "alice", "alice@msg.example"},
		// Non-UP account keeps Ente's behaviour.
		{"alice@gmail.com", "alice@proton.me", "alice@proton.me"},
	}
	for _, tc := range allowed {
		got, err := UPChangeEmailTarget(tc.current, tc.requested)
		if err != nil || got != tc.want {
			t.Errorf("UPChangeEmailTarget(%q, %q) = %q, %v; want %q", tc.current, tc.requested, got, err, tc.want)
		}
	}

	denied := []struct{ current, requested string }{
		{"alice@msg.example", "alice@gmail.com"},
		{"alice@msg.example", "bob"},
		{"alice@msg.example", "bob@msg.example"},
		{"alice", "bob"},
		{"alice", "alice@gmail.com"},
		{"alice@gmail.com", "alice"},
		{"alice@gmail.com", "bob@msg.example"},
	}
	for _, tc := range denied {
		got, err := UPChangeEmailTarget(tc.current, tc.requested)
		if !errors.Is(err, ente.ErrPermissionDenied) {
			t.Errorf("UPChangeEmailTarget(%q, %q) = %q, %v; want ErrPermissionDenied", tc.current, tc.requested, got, err)
		}
	}

	for _, requested := range []string{"", "a b"} {
		if got, err := UPChangeEmailTarget("alice@msg.example", requested); !errors.Is(err, ente.ErrBadRequest) {
			t.Errorf("UPChangeEmailTarget(_, %q) = %q, %v; want ErrBadRequest", requested, got, err)
		}
	}
}
