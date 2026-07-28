package synth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.elevenlabs.io"
	defaultModelID = "eleven_multilingual_v2" // highest quality; returns timestamps
	defaultFormat  = MP3_44100_128
	maxRetries     = 3 // retries for transient (5xx/transport) failures
	defaultTimeout = 120 * time.Second

	// maxRateLimitRetries is larger than maxRetries: 429s mean "the account's
	// concurrency cap is busy right now", which resolves on its own.
	maxRateLimitRetries = 8

	// maxBackoff caps the exponential so a long queue never sleeps absurdly long.
	maxBackoff = 32 * time.Second
)

// ElevenLabs is a minimal REST client for the ElevenLabs API.
type ElevenLabs struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client
}

// NewElevenLabs builds a client with sane defaults. The API key must come from
// the environment or the OS keychain — never hardcoded (see prior-apps for the
// anti-pattern this replaces).
func NewElevenLabs(apiKey string) *ElevenLabs {
	return &ElevenLabs{
		APIKey:  apiKey,
		BaseURL: defaultBaseURL,
		HTTP:    &http.Client{Timeout: defaultTimeout},
	}
}

// Synthesize calls POST /v1/text-to-speech/{voice_id} — or the /with-timestamps
// variant when req.WithTimestamps is set — and returns audio plus word timings
// derived from the character alignment. Transient failures (429, 5xx, transport
// errors) are retried with exponential backoff, honoring any Retry-After header
// and ctx cancellation; client errors (4xx) fail fast.
func (c *ElevenLabs) Synthesize(ctx context.Context, req Request) (*Result, error) {
	if req.VoiceID == "" {
		return nil, errors.New("synth: empty voice ID")
	}
	if req.ModelID == "" {
		req.ModelID = defaultModelID
	}
	if req.Format == "" {
		req.Format = defaultFormat
	}

	// Rate-limit rejections get their own, larger budget: hitting the account's
	// concurrency cap is expected (the key is shared across the team) and always
	// clears once in-flight requests finish, so we wait it out rather than fail.
	var transientTries, rateLimitTries int
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		res, err := c.synthesizeOnce(ctx, req)
		if err == nil {
			return res, nil
		}
		if !isRetryable(err) {
			return nil, err
		}
		if isRateLimit(err) {
			rateLimitTries++
			if rateLimitTries > maxRateLimitRetries {
				return nil, err
			}
		} else {
			transientTries++
			if transientTries > maxRetries {
				return nil, err
			}
		}
		if werr := waitBackoff(ctx, transientTries+rateLimitTries, retryAfterOf(err)); werr != nil {
			return nil, werr
		}
	}
}

func (c *ElevenLabs) synthesizeOnce(ctx context.Context, req Request) (*Result, error) {
	body, err := json.Marshal(newTTSRequestBody(req))
	if err != nil {
		return nil, fmt.Errorf("synth: encoding request: %w", err)
	}

	endpoint := c.BaseURL + "/v1/text-to-speech/" + url.PathEscape(req.VoiceID)
	if req.WithTimestamps {
		endpoint += "/with-timestamps"
	}
	endpoint += "?output_format=" + url.QueryEscape(string(req.Format))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("xi-api-key", c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, err // transport error — retryable
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseAPIError(resp.StatusCode, data, retryAfterHeader(resp))
	}

	if !req.WithTimestamps {
		return &Result{Audio: data}, nil
	}

	var tr ttsTimestampResponse
	if err := json.Unmarshal(data, &tr); err != nil {
		return nil, fmt.Errorf("synth: decoding timestamp response: %w", err)
	}
	audio, err := base64.StdEncoding.DecodeString(tr.AudioBase64)
	if err != nil {
		return nil, fmt.Errorf("synth: decoding audio_base64: %w", err)
	}
	return &Result{
		Audio: audio,
		Words: wordsFromAlignment(
			tr.Alignment.Characters,
			tr.Alignment.CharacterStartTimesSeconds,
			tr.Alignment.CharacterEndTimesSeconds,
		),
	}, nil
}

// Subscription calls GET /v1/user/subscription. The response carries no
// concurrency limit — only the tier — so callers pair Tier with MaxConcurrency.
func (c *ElevenLabs) Subscription(ctx context.Context) (*Subscription, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/user/subscription", nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("xi-api-key", c.APIKey)

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseAPIError(resp.StatusCode, data, 0)
	}

	var sr struct {
		Tier           string `json:"tier"`
		CharacterCount int    `json:"character_count"`
		CharacterLimit int    `json:"character_limit"`
	}
	if err := json.Unmarshal(data, &sr); err != nil {
		return nil, fmt.Errorf("synth: decoding subscription: %w", err)
	}
	return &Subscription{
		Tier:           sr.Tier,
		CharacterCount: sr.CharacterCount,
		CharacterLimit: sr.CharacterLimit,
	}, nil
}

// Voices calls GET /v1/voices so the UI can present a picker instead of
// requiring raw voice IDs.
func (c *ElevenLabs) Voices(ctx context.Context) ([]Voice, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/voices", nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("xi-api-key", c.APIKey)

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseAPIError(resp.StatusCode, data, 0)
	}

	var vr struct {
		Voices []struct {
			VoiceID  string `json:"voice_id"`
			Name     string `json:"name"`
			Category string `json:"category"`
		} `json:"voices"`
	}
	if err := json.Unmarshal(data, &vr); err != nil {
		return nil, fmt.Errorf("synth: decoding voices: %w", err)
	}
	out := make([]Voice, 0, len(vr.Voices))
	for _, v := range vr.Voices {
		out = append(out, Voice{ID: v.VoiceID, Name: v.Name, Category: v.Category})
	}
	return out, nil
}

// PronunciationRule is one alias to publish: say To when the text reads From.
type PronunciationRule struct {
	From string
	To   string
}

// Pronunciation-dictionary management. These are calls on the concrete client,
// deliberately not part of the Client interface — the job runner never
// publishes. Dictionaries are account-level and versioned with NO delete
// endpoint, so callers must reuse one dictionary per project: create once,
// then update it in place (each update mints a new version of the same
// dictionary; verified live 2026-07-27).

type pronRuleBody struct {
	StringToReplace string `json:"string_to_replace"`
	Type            string `json:"type"`
	Alias           string `json:"alias"`
}

type pronDictResponse struct {
	ID        string `json:"id"`
	VersionID string `json:"version_id"`
}

func pronRuleBodies(rules []PronunciationRule) []pronRuleBody {
	rb := make([]pronRuleBody, 0, len(rules))
	for _, r := range rules {
		rb = append(rb, pronRuleBody{StringToReplace: r.From, Type: "alias", Alias: r.To})
	}
	return rb
}

// CreatePronunciationDictionary uploads alias rules as a brand-new dictionary
// and returns its locator.
func (c *ElevenLabs) CreatePronunciationDictionary(ctx context.Context, name string, rules []PronunciationRule) (DictionaryLocator, error) {
	return c.pronCall(ctx, "/v1/pronunciation-dictionaries/add-from-rules", struct {
		Name  string         `json:"name"`
		Rules []pronRuleBody `json:"rules"`
	}{Name: name, Rules: pronRuleBodies(rules)})
}

// AddPronunciationRules adds or overwrites rules on an existing dictionary,
// returning the locator of the new version it mints.
func (c *ElevenLabs) AddPronunciationRules(ctx context.Context, dictID string, rules []PronunciationRule) (DictionaryLocator, error) {
	return c.pronCall(ctx, "/v1/pronunciation-dictionaries/"+url.PathEscape(dictID)+"/add-rules", struct {
		Rules []pronRuleBody `json:"rules"`
	}{Rules: pronRuleBodies(rules)})
}

// RemovePronunciationRules removes rules (by their written form) from an
// existing dictionary, returning the locator of the new version it mints.
func (c *ElevenLabs) RemovePronunciationRules(ctx context.Context, dictID string, ruleStrings []string) (DictionaryLocator, error) {
	return c.pronCall(ctx, "/v1/pronunciation-dictionaries/"+url.PathEscape(dictID)+"/remove-rules", struct {
		RuleStrings []string `json:"rule_strings"`
	}{RuleStrings: ruleStrings})
}

func (c *ElevenLabs) pronCall(ctx context.Context, path string, reqBody any) (DictionaryLocator, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return DictionaryLocator{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return DictionaryLocator{}, err
	}
	httpReq.Header.Set("xi-api-key", c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return DictionaryLocator{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return DictionaryLocator{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return DictionaryLocator{}, parseAPIError(resp.StatusCode, data, 0)
	}
	var pr pronDictResponse
	if err := json.Unmarshal(data, &pr); err != nil {
		return DictionaryLocator{}, fmt.Errorf("synth: decoding dictionary response: %w", err)
	}
	if pr.ID == "" || pr.VersionID == "" {
		return DictionaryLocator{}, fmt.Errorf("synth: dictionary response missing id/version")
	}
	return DictionaryLocator{ID: pr.ID, VersionID: pr.VersionID}, nil
}

// --- request/response bodies -------------------------------------------------

type ttsRequestBody struct {
	Text          string             `json:"text"`
	ModelID       string             `json:"model_id"`
	VoiceSettings *voiceSettingsBody `json:"voice_settings,omitempty"`

	PronunciationDictionaryLocators []dictLocatorBody `json:"pronunciation_dictionary_locators,omitempty"`
}

type dictLocatorBody struct {
	PronunciationDictionaryID string `json:"pronunciation_dictionary_id"`
	VersionID                 string `json:"version_id"`
}

// voiceSettingsBody sends only the knobs that are actually set; omitted fields
// keep the voice's own defaults on the ElevenLabs side.
type voiceSettingsBody struct {
	Stability       *float64 `json:"stability,omitempty"`
	SimilarityBoost *float64 `json:"similarity_boost,omitempty"`
	Style           *float64 `json:"style,omitempty"`
	UseSpeakerBoost *bool    `json:"use_speaker_boost,omitempty"`
	Speed           *float64 `json:"speed,omitempty"`
}

func newTTSRequestBody(req Request) ttsRequestBody {
	b := ttsRequestBody{Text: req.Text, ModelID: req.ModelID}
	for _, l := range req.DictionaryLocators {
		b.PronunciationDictionaryLocators = append(b.PronunciationDictionaryLocators,
			dictLocatorBody{PronunciationDictionaryID: l.ID, VersionID: l.VersionID})
	}
	if s := req.VoiceSettings; s != nil {
		b.VoiceSettings = &voiceSettingsBody{
			Stability:       s.Stability,
			SimilarityBoost: s.SimilarityBoost,
			Style:           s.Style,
			UseSpeakerBoost: s.UseSpeakerBoost,
			Speed:           s.Speed,
		}
	}
	return b
}

type ttsTimestampResponse struct {
	AudioBase64 string `json:"audio_base64"`
	Alignment   struct {
		Characters                 []string  `json:"characters"`
		CharacterStartTimesSeconds []float64 `json:"character_start_times_seconds"`
		CharacterEndTimesSeconds   []float64 `json:"character_end_times_seconds"`
	} `json:"alignment"`
}

// wordsFromAlignment groups runs of non-whitespace characters into words. Each
// word's Start is the start time of its first character and End the end time of
// its last, so punctuation attached to a word (e.g. "cadet.") is kept with it —
// matching a consumer that tokenizes on whitespace.
func wordsFromAlignment(chars []string, starts, ends []float64) []WordTiming {
	var (
		words []WordTiming
		cur   strings.Builder
		start float64
		end   float64
		open  bool
	)
	flush := func() {
		if open {
			words = append(words, WordTiming{Word: cur.String(), Start: start, End: end})
			cur.Reset()
			open = false
		}
	}
	for i, ch := range chars {
		if strings.TrimSpace(ch) == "" {
			flush()
			continue
		}
		if !open {
			open = true
			if i < len(starts) {
				start = starts[i]
			}
		}
		cur.WriteString(ch)
		if i < len(ends) {
			end = ends[i]
		}
	}
	flush()
	return words
}

// --- errors & retry ----------------------------------------------------------

// apiError is a non-2xx response from the API.
type apiError struct {
	status     int
	message    string
	retryAfter time.Duration
}

func (e *apiError) Error() string {
	if e.message == "" {
		return fmt.Sprintf("synth: API status %d", e.status)
	}
	return fmt.Sprintf("synth: API status %d: %s", e.status, e.message)
}

// retryable reports whether the status warrants a retry (rate limit or server error).
func (e *apiError) retryable() bool { return e.status == http.StatusTooManyRequests || e.status >= 500 }

// isRateLimit reports whether err is a 429 — the account's concurrency cap being
// saturated, as opposed to a transient server fault.
func isRateLimit(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.status == http.StatusTooManyRequests
}

func parseAPIError(status int, body []byte, retryAfter time.Duration) *apiError {
	msg := strings.TrimSpace(string(body))
	// ElevenLabs errors are usually {"detail": "..."} or {"detail": {"message": "..."}}.
	var env struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(body, &env) == nil && len(env.Detail) > 0 {
		var s string
		if json.Unmarshal(env.Detail, &s) == nil {
			msg = s
		} else {
			var d struct {
				Message string `json:"message"`
				Status  string `json:"status"`
			}
			if json.Unmarshal(env.Detail, &d) == nil && d.Message != "" {
				msg = d.Message
			}
		}
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return &apiError{status: status, message: msg, retryAfter: retryAfter}
}

// isRetryable reports whether err (an API error or a transport error) should be
// retried. Transport errors are assumed transient; ctx cancellation is handled
// separately in the retry loop.
func isRetryable(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.retryable()
	}
	return true
}

func retryAfterOf(err error) time.Duration {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.retryAfter
	}
	return 0
}

func retryAfterHeader(resp *http.Response) time.Duration {
	v := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// waitBackoff sleeps before retry n (1-based): Retry-After if provided, else
// exponential 2^n seconds capped at maxBackoff. Jitter is always added so that
// workers rejected together don't retry in lockstep and re-collide. It returns
// early if ctx is canceled.
func waitBackoff(ctx context.Context, n int, retryAfter time.Duration) error {
	d := retryAfter
	if d <= 0 {
		d = time.Duration(int64(1)<<uint(min(n, 5))) * time.Second // 2s…32s
		if d > maxBackoff {
			d = maxBackoff
		}
	}
	d += rand.N(500 * time.Millisecond)
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Compile-time assertion that *ElevenLabs satisfies Client.
var _ Client = (*ElevenLabs)(nil)
