package agent

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
)

const modelHeader = "X-Zns-Model"
const effortHeader = "X-Zns-Reasoning-Effort"

// These headers belong to the private host-to-model protocol, never user input.
func modelRequestContext(request *http.Request) (context.Context, error) {
	selection := modelsettings.Selection{
		Model:  request.Header.Get(modelHeader),
		Effort: request.Header.Get(effortHeader),
	}
	if !modelsettings.Valid(selection) {
		return nil, errors.New("invalid model selection")
	}
	return modelsettings.WithSelection(request.Context(), selection), nil
}

func codexModelArguments(ctx context.Context, directory, schema string) []string {
	args := codexArguments(directory, schema)
	selection := modelsettings.FromContext(ctx)
	for index, arg := range args {
		if arg == "--model" {
			args[index+1] = selection.Model
			break
		}
	}
	if selection.Effort != "" {
		args = append(args[:len(args)-1], "-c", "model_reasoning_effort="+strconv.Quote(selection.Effort), "-")
	}
	return args
}
