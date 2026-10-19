package main

import (
	"testing"

	"github.com/emersion/go-imap"
)

func TestWhitelistMatching(t *testing.T) {
	original := Whitelist
	t.Cleanup(func() { Whitelist = original })
	for _, tc := range []struct {
		name, entry, address, domain string
		want                         bool
	}{
		{"address", " Friend@Example.com ", "friend@example.com", "example.com", true},
		{"different address", "friend@example.com", "other@example.com", "example.com", false},
		{"domain", "example.com", "friend@example.com", "example.com", true},
		{"exact domain excludes subdomain", "example.com", "friend@mail.example.com", "mail.example.com", false},
		{"suffix apex", "*example.com", "friend@example.com", "example.com", true},
		{"suffix subdomain", "*example.com", "friend@mail.example.com", "mail.example.com", true},
		{"permissive suffix", "*example.com", "friend@otherexample.com", "otherexample.com", true},
		{"suffix excludes trailing domain", "*example.com", "friend@example.com.other.org", "example.com.other.org", false},
		{"dotted suffix excludes apex", "*.example.com", "friend@example.com", "example.com", false},
		{"dotted suffix subdomain", "*.example.com", "friend@mail.example.com", "mail.example.com", true},
		{"blank", " ", "friend@example.com", "example.com", false},
		{"bare wildcard", "*", "friend@example.com", "example.com", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			Whitelist = []string{tc.entry}
			if got := IsWhitelisted(tc.address, tc.domain); got != tc.want {
				t.Fatalf("IsWhitelisted() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildFromEmailAddress(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  *imap.Message
		want bool
	}{
		{"nil message", nil, false},
		{"nil envelope", &imap.Message{}, false},
		{"missing sender", &imap.Message{Envelope: &imap.Envelope{}}, false},
		{"nil sender", &imap.Message{Envelope: &imap.Envelope{From: []*imap.Address{nil}}}, false},
		{"missing local part", &imap.Message{Envelope: &imap.Envelope{From: []*imap.Address{{HostName: "example.com"}}}}, false},
		{"missing domain", &imap.Message{Envelope: &imap.Envelope{From: []*imap.Address{{MailboxName: "friend"}}}}, false},
		{"valid sender with empty subject", &imap.Message{Envelope: &imap.Envelope{From: []*imap.Address{{MailboxName: " Friend ", HostName: " Example.COM "}}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			address, domain, ok := BuildFromEmailAddress(tc.msg)
			if ok != tc.want {
				t.Fatalf("BuildFromEmailAddress() ok = %v, want %v", ok, tc.want)
			}
			if ok && (address != "friend@example.com" || domain != "example.com") {
				t.Fatalf("unexpected normalized sender: %q, %q", address, domain)
			}
		})
	}
}
