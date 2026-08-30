package main

import "testing"

func TestWebhookSecretMatchesOnlyWhenConfiguredSecretMatchesHeader(t *testing.T) {
	if !webhookSecretMatches("", "") {
		t.Fatal("empty configured secret should allow requests")
	}
	if webhookSecretMatches("expected", "") {
		t.Fatal("missing header matched configured secret")
	}
	if webhookSecretMatches("expected", "wrong") {
		t.Fatal("wrong header matched configured secret")
	}
	if !webhookSecretMatches("expected", "expected") {
		t.Fatal("matching header was rejected")
	}
}
