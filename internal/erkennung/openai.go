package erkennung

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

const rawLimit = 64 * 1024

// OpenAIOptions configures the OpenAI-compatible chat client.
type OpenAIOptions struct {
	BaseURL         string
	APIKey          string
	Model           string
	Format          string
	ReasoningEffort string
	Timeout         time.Duration
	HTTPClient      *http.Client
}

// OpenAI calls POST {base}/chat/completions through openai-go.
type OpenAI struct {
	client    openai.Client
	model     string
	format    string
	reasoning string
	problem   atomic.Value
}

// NewOpenAI builds a client. An empty API key omits the Authorization header
// so local servers such as Ollama accept the request.
func NewOpenAI(opt OpenAIOptions) *OpenAI {
	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	// The SDK resolves "chat/completions" against the base URL. Without a
	// trailing slash, ".../v1" is treated as a file and the path is dropped.
	base := strings.TrimRight(strings.TrimSpace(opt.BaseURL), "/") + "/"
	req := []option.RequestOption{
		option.WithBaseURL(base),
		option.WithMaxRetries(0),
		option.WithRequestTimeout(timeout),
	}
	if strings.TrimSpace(opt.APIKey) != "" {
		req = append(req, option.WithAPIKey(opt.APIKey))
	}
	if opt.HTTPClient != nil {
		req = append(req, option.WithHTTPClient(opt.HTTPClient))
	}
	format := opt.Format
	if format == "" {
		format = "auto"
	}
	return &OpenAI{
		client:    openai.NewClient(req...),
		model:     opt.Model,
		format:    format,
		reasoning: strings.TrimSpace(opt.ReasoningEffort),
	}
}

// Problem is the last authentication failure, or empty after a successful call.
func (c *OpenAI) Problem() string {
	if c == nil {
		return ""
	}
	v := c.problem.Load()
	if v == nil {
		return ""
	}
	text, _ := v.(string)
	return text
}

// Extract sends the JPEG and parses the structured response.
// img must already be the normalised picture without EXIF.
func (c *OpenAI) Extract(ctx context.Context, img []byte, _ string) (Ergebnis, Meta, error) {
	start := time.Now()
	mode := c.format
	if mode == "auto" || mode == "" {
		mode = "json_schema"
	}
	content, model, err := c.complete(ctx, img, mode)
	if err != nil && (c.format == "auto" || c.format == "") && formatRejected(err) {
		content, model, err = c.complete(ctx, img, "json_object")
	}
	meta := Meta{Modell: model, DauerMS: time.Since(start).Milliseconds(), Roh: clip([]byte(content))}
	if meta.Modell == "" {
		meta.Modell = c.model
	}
	if err != nil {
		return Ergebnis{}, meta, c.asCall(err)
	}
	if strings.TrimSpace(content) == "" {
		return Ergebnis{}, meta, &CallError{Kind: KindSchema, Text: "Antwort ungültig"}
	}
	parsed, perr := Parse(content)
	if perr != nil {
		return Ergebnis{}, meta, &CallError{Kind: KindSchema, Text: "Antwort ungültig"}
	}
	c.problem.Store("")
	return parsed, meta, nil
}

func (c *OpenAI) complete(ctx context.Context, img []byte, mode string) (string, string, error) {
	format := objectFormat()
	promptMode := "json_object"
	if mode == "json_schema" {
		schema, err := schemaFormat()
		if err != nil {
			return "", "", err
		}
		format = schema
		promptMode = "json_schema"
	}
	dataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(img)
	params := openai.ChatCompletionNewParams{
		Model: shared.ChatModel(c.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{
				openai.TextContentPart(promptText(promptMode)),
				openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: dataURL}),
			}),
		},
		ResponseFormat: format,
	}
	if c.reasoning != "" {
		params.ReasoningEffort = shared.ReasoningEffort(c.reasoning)
	}
	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", "", err
	}
	model := resp.Model
	if len(resp.Choices) == 0 {
		return "", model, errors.New("leere Antwort")
	}
	return resp.Choices[0].Message.Content, model, nil
}

func schemaFormat() (openai.ChatCompletionNewParamsResponseFormatUnion, error) {
	schema, err := schemaValue()
	if err != nil {
		return openai.ChatCompletionNewParamsResponseFormatUnion{}, err
	}
	return openai.ChatCompletionNewParamsResponseFormatUnion{
		OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
			JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
				Name:   "kassenbeleg",
				Strict: openai.Bool(true),
				Schema: schema,
			},
		},
	}, nil
}

func objectFormat() openai.ChatCompletionNewParamsResponseFormatUnion {
	return openai.ChatCompletionNewParamsResponseFormatUnion{
		OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
	}
}

func (c *OpenAI) asCall(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			c.problem.Store("API-Key ungültig")
			return &CallError{Kind: KindAuth, Text: "API-Key ungültig"}
		case http.StatusTooManyRequests:
			return &CallError{Kind: KindTransient, Text: "Dienst vorübergehend nicht erreichbar"}
		case http.StatusBadRequest:
			return &CallError{Kind: KindPermanent, Text: "Anfrage abgelehnt"}
		default:
			if apiErr.StatusCode >= 500 {
				return &CallError{Kind: KindTransient, Text: "Dienst vorübergehend nicht erreichbar"}
			}
			return &CallError{Kind: KindPermanent, Text: "Anfrage abgelehnt"}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &CallError{Kind: KindTransient, Text: "Zeitüberschreitung"}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return &CallError{Kind: KindTransient, Text: "Zeitüberschreitung"}
		}
		return &CallError{Kind: KindTransient, Text: "Dienst vorübergehend nicht erreichbar"}
	}
	if strings.Contains(strings.ToLower(err.Error()), "leere antwort") {
		return &CallError{Kind: KindSchema, Text: "Antwort ungültig"}
	}
	return &CallError{Kind: KindTransient, Text: "Dienst vorübergehend nicht erreichbar"}
}

func formatRejected(err error) bool {
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		return false
	}
	blob := strings.ToLower(apiErr.Message + " " + apiErr.RawJSON())
	return strings.Contains(blob, "response_format")
}

func clip(b []byte) []byte {
	if len(b) <= rawLimit {
		return b
	}
	out := make([]byte, rawLimit)
	copy(out, b)
	return out
}
