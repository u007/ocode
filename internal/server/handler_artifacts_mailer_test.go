package server

import (
	"errors"
	"testing"
)

func TestSendEmailNoMailerReturnsError(t *testing.T) {
	err := sendEmail(nil, "a@example.com", "s", "b")
	if !errors.Is(err, errNoMailer) {
		t.Fatalf("sendEmail(nil) = %v, want errNoMailer", err)
	}
}

func TestSendEmailUnsupportedTypeReturnsError(t *testing.T) {
	err := sendEmail(struct{}{}, "a@example.com", "s", "b")
	if err == nil {
		t.Fatal("sendEmail(unsupported) = nil, want error")
	}
}

func TestSendEmailFuncMailer(t *testing.T) {
	var gotTo, gotSubject, gotBody string
	m := func(to, subject, body string) error {
		gotTo, gotSubject, gotBody = to, subject, body
		return nil
	}
	if err := sendEmail(m, "a@example.com", "subj", "body"); err != nil {
		t.Fatalf("sendEmail(func) = %v", err)
	}
	if gotTo != "a@example.com" || gotSubject != "subj" || gotBody != "body" {
		t.Fatalf("mailer received %q/%q/%q", gotTo, gotSubject, gotBody)
	}
}

func TestSendEmailPropagatesMailerError(t *testing.T) {
	want := errors.New("smtp down")
	m := func(_, _, _ string) error { return want }
	if err := sendEmail(m, "a@example.com", "s", "b"); !errors.Is(err, want) {
		t.Fatalf("sendEmail(func err) = %v, want %v", err, want)
	}
}
