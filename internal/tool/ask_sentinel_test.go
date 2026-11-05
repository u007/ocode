package tool

import "testing"

func TestQuestionDismissed(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"exact", QuestionDismissedResult, true},
		{"whitespace padded", "  \n" + QuestionDismissedResult + "\t\n", true},
		{"other text", "The user answered: Staging", false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := QuestionDismissed(tc.content); got != tc.want {
				t.Fatalf("QuestionDismissed(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}
