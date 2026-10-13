package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func validQuestionSpec(now time.Time) QuestionSpec {
	return QuestionSpec{
		ExternalID: "  ask_1  ",
		Title:      "  Need three details  ",
		ExpiresAt:  now.Add(15 * time.Minute).Format(time.RFC3339),
		Items: []QuestionItem{
			{ID: "ship_date", Header: "Fecha", Prompt: "¿Qué día embarcamos?", Options: []QuestionOption{{Label: " Lun 15 sep ", Description: "AA 2231"}, {Label: "Mar 16 sep"}}, AllowOther: true},
			{ID: "boxes", Header: "Cajas", Prompt: "¿Cuántas cajas?"},
			{ID: "extras", Header: "Extras", Prompt: "¿Algo más?", MultiSelect: true, Options: []QuestionOption{{Label: "Capuchón"}, {Label: "UPC"}}},
		},
	}
}

func TestNormalizeQuestionSpecTrimsAndDefaults(t *testing.T) {
	now := time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC)
	spec, err := NormalizeQuestionSpec(validQuestionSpec(now))
	if err != nil {
		t.Fatal(err)
	}
	if spec.ExternalID != "ask_1" || spec.Title != "Need three details" || spec.AllowSkip == nil || !*spec.AllowSkip {
		t.Fatalf("unexpected normalized spec: %#v", spec)
	}
	if spec.ExpiresAt != "2026-09-12T15:15:00Z" || spec.Items[0].Options[0].Label != "Lun 15 sep" {
		t.Fatalf("unexpected normalized fields: %#v", spec)
	}
}

func TestNormalizeQuestionSpecRejectsInvalidQuestions(t *testing.T) {
	now := time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC)
	for name, mutate := range map[string]func(*QuestionSpec){
		"no items":            func(spec *QuestionSpec) { spec.Items = nil },
		"too many items":      func(spec *QuestionSpec) { spec.Items = append(spec.Items, spec.Items[1], spec.Items[1], spec.Items[1]) },
		"bad id":              func(spec *QuestionSpec) { spec.Items[0].ID = "Ship-Date" },
		"repeated id":         func(spec *QuestionSpec) { spec.Items[1].ID = "ship_date" },
		"empty header":        func(spec *QuestionSpec) { spec.Items[0].Header = " " },
		"long header":         func(spec *QuestionSpec) { spec.Items[0].Header = strings.Repeat("h", MaxQuestionHeaderLength+1) },
		"empty prompt":        func(spec *QuestionSpec) { spec.Items[0].Prompt = "" },
		"unsafe url":          func(spec *QuestionSpec) { spec.Items[0].URL = "javascript:alert(1)" },
		"repeated option":     func(spec *QuestionSpec) { spec.Items[0].Options[1].Label = "lun 15 SEP" },
		"single multi-select": func(spec *QuestionSpec) { spec.Items[2].Options = spec.Items[2].Options[:1] },
		"past deadline":       func(spec *QuestionSpec) { spec.ExpiresAt = now.Add(-time.Minute).Format(time.RFC3339) },
		"deadline too far":    func(spec *QuestionSpec) { spec.ExpiresAt = now.Add(8 * 24 * time.Hour).Format(time.RFC3339) },
		"bad deadline":        func(spec *QuestionSpec) { spec.ExpiresAt = "tomorrow" },
		"empty responder":     func(spec *QuestionSpec) { spec.ResponderUserIDs = []string{" "} },
	} {
		t.Run(name, func(t *testing.T) {
			spec := validQuestionSpec(now)
			mutate(&spec)
			normalized, err := NormalizeQuestionSpec(spec)
			if err == nil {
				err = ValidateQuestionLifetime(normalized, now)
			}
			if !errors.Is(err, ErrInvalidQuestion) {
				t.Fatalf("expected invalid question, got %v", err)
			}
		})
	}
}

func TestNormalizeQuestionAnswers(t *testing.T) {
	now := time.Now()
	spec, err := NormalizeQuestionSpec(validQuestionSpec(now))
	if err != nil {
		t.Fatal(err)
	}
	got, err := NormalizeQuestionAnswers(spec.Items, map[string][]string{
		"ship_date": {"  Lun 15 sep "},
		"boxes":     {" 120 "},
		"extras":    {"UPC", "Capuchón", "UPC"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"ship_date": {"Lun 15 sep"}, "boxes": {"120"}, "extras": {"UPC", "Capuchón"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("answers = %#v, want %#v", got, want)
	}
	if _, err := NormalizeQuestionAnswers(spec.Items, map[string][]string{"ship_date": {"Jue 18 sep"}, "boxes": {"1"}, "extras": {"UPC"}}); err != nil {
		t.Fatalf("free text for a question that allows other: %v", err)
	}
	withOther := append([]QuestionItem(nil), spec.Items...)
	withOther[2].AllowOther = true
	if _, err := NormalizeQuestionAnswers(withOther, map[string][]string{"ship_date": {"Mar 16 sep"}, "boxes": {"1"}, "extras": {"UPC", "Comida", "Flores"}}); !errors.Is(err, ErrInvalidQuestion) {
		t.Fatalf("expected one free-text answer per question, got %v", err)
	}
	for name, answers := range map[string]map[string][]string{
		"missing question":  {"ship_date": {"Mar 16 sep"}, "boxes": {"1"}},
		"unknown question":  {"ship_date": {"Mar 16 sep"}, "boxes": {"1"}, "extras": {"UPC"}, "color": {"red"}},
		"two single-select": {"ship_date": {"Lun 15 sep", "Mar 16 sep"}, "boxes": {"1"}, "extras": {"UPC"}},
		"unlisted option":   {"ship_date": {"Mar 16 sep"}, "boxes": {"1"}, "extras": {"Comida"}},
		"empty value":       {"ship_date": {"Mar 16 sep"}, "boxes": {" "}, "extras": {"UPC"}},
		"long free text":    {"ship_date": {"Mar 16 sep"}, "boxes": {strings.Repeat("x", MaxQuestionFreeTextLength+1)}, "extras": {"UPC"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeQuestionAnswers(spec.Items, answers); !errors.Is(err, ErrInvalidQuestion) {
				t.Fatalf("expected invalid answer, got %v", err)
			}
		})
	}
}

func TestQuestionStatusRules(t *testing.T) {
	now := time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC)
	deadline := now.Format(time.RFC3339Nano)
	if EffectiveQuestionStatus(QuestionStatusOpen, deadline, now) != QuestionStatusExpired {
		t.Fatal("an open question at its deadline must read as expired")
	}
	if EffectiveQuestionStatus(QuestionStatusOpen, now.Add(time.Second).Format(time.RFC3339Nano), now) != QuestionStatusOpen {
		t.Fatal("an open question before its deadline must stay open")
	}
	if EffectiveQuestionStatus(QuestionStatusSubmitted, deadline, now) != QuestionStatusSubmitted {
		t.Fatal("a submitted question keeps its status after the deadline")
	}
	for _, tc := range []struct {
		from, to string
		allowed  bool
	}{
		{QuestionStatusOpen, QuestionStatusAnswered, true},
		{QuestionStatusOpen, QuestionStatusOpen, false},
		{QuestionStatusSubmitted, QuestionStatusOpen, true},
		{QuestionStatusSubmitted, QuestionStatusExpired, false},
		{QuestionStatusAnswered, QuestionStatusFailed, false},
	} {
		if QuestionResolutionAllowed(tc.from, tc.to) != tc.allowed {
			t.Fatalf("%s -> %s allowed = %t", tc.from, tc.to, !tc.allowed)
		}
	}
	if !IsTerminalQuestionStatus(QuestionStatusFailed) || IsTerminalQuestionStatus(QuestionStatusSubmitted) {
		t.Fatal("unexpected terminal status classification")
	}
}
