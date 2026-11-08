package logging

import (
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/redact"
	"strings"
	"testing"
)

func TestGitContentMinimization(t *testing.T) {
	message := "private commit prose"
	input := Record{Argv: []string{"commit", "-m", message}, Operations: []model.Operation{{Texts: &model.Texts{Body: &model.Text{Value: message}}, Parameters: map[string]any{"message": message}}}}
	got := redactRecord(redact.New(nil), input)
	if strings.Contains(got.Argv[2], message) || got.Operations[0].Texts.Body.Value != contentSummary(message) || got.Operations[0].Parameters["message"] != contentSummary(message) {
		t.Fatalf("content leaked: %+v", got)
	}
	if input.Argv[2] != message || input.Operations[0].Texts.Body.Value != message || input.Operations[0].Parameters["message"] != message {
		t.Fatal("logging mutated hook/execution input")
	}
}
