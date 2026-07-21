package synth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSynthesizeWithTimestamps(t *testing.T) {
	var gotPath, gotFormat, gotKey, gotMethod string
	var gotBody ttsRequestBody

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotFormat = r.URL.Query().Get("output_format")
		gotKey = r.Header.Get("xi-api-key")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		resp := map[string]any{
			"audio_base64": base64.StdEncoding.EncodeToString([]byte("AUDIO")),
			"alignment": map[string]any{
				"characters":                    []string{"H", "i", " ", "y", "o", "u"},
				"character_start_times_seconds": []float64{0.0, 0.1, 0.2, 0.3, 0.4, 0.5},
				"character_end_times_seconds":   []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := &ElevenLabs{APIKey: "secret-key", BaseURL: srv.URL, HTTP: srv.Client()}
	res, err := c.Synthesize(context.Background(), Request{
		VoiceID:        "VOICE123",
		Text:           "Hi you",
		Format:         MP3_44100_192,
		WithTimestamps: true,
	})
	if err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/text-to-speech/VOICE123/with-timestamps" {
		t.Errorf("path = %q", gotPath)
	}
	if gotFormat != string(MP3_44100_192) {
		t.Errorf("output_format = %q, want %q", gotFormat, MP3_44100_192)
	}
	if gotKey != "secret-key" {
		t.Errorf("xi-api-key = %q", gotKey)
	}
	if gotBody.Text != "Hi you" || gotBody.ModelID != defaultModelID {
		t.Errorf("body = %+v, want text=%q model=%q", gotBody, "Hi you", defaultModelID)
	}
	if string(res.Audio) != "AUDIO" {
		t.Errorf("audio = %q, want AUDIO", res.Audio)
	}
	want := []WordTiming{{Word: "Hi", Start: 0.0, End: 0.2}, {Word: "you", Start: 0.3, End: 0.6}}
	if len(res.Words) != len(want) {
		t.Fatalf("words = %+v, want %+v", res.Words, want)
	}
	for i, w := range want {
		if res.Words[i] != w {
			t.Errorf("word %d = %+v, want %+v", i, res.Words[i], w)
		}
	}
}

func TestSynthesizeRawAudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/text-to-speech/V" {
			t.Errorf("path = %q", got)
		}
		_, _ = w.Write([]byte("RAWMP3BYTES"))
	}))
	defer srv.Close()

	c := &ElevenLabs{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	res, err := c.Synthesize(context.Background(), Request{VoiceID: "V", Text: "hi"})
	if err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}
	if string(res.Audio) != "RAWMP3BYTES" {
		t.Errorf("audio = %q", res.Audio)
	}
	if res.Words != nil {
		t.Errorf("words = %+v, want nil for non-timestamp request", res.Words)
	}
}

func TestSynthesizeClientErrorNoRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":{"message":"bad voice","status":"invalid"}}`))
	}))
	defer srv.Close()

	c := &ElevenLabs{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := c.Synthesize(context.Background(), Request{VoiceID: "V", Text: "hi"})
	if err == nil {
		t.Fatal("expected error on 422")
	}
	if calls != 1 {
		t.Errorf("server called %d times, want 1 (4xx must not retry)", calls)
	}
	if isRetryable(err) {
		t.Error("422 should not be retryable")
	}
}

func TestVoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("xi-api-key") == "" {
			t.Error("missing xi-api-key header")
		}
		_, _ = w.Write([]byte(`{"voices":[{"voice_id":"a","name":"Amy","category":"premade"},{"voice_id":"b","name":"Ian"}]}`))
	}))
	defer srv.Close()

	c := &ElevenLabs{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	voices, err := c.Voices(context.Background())
	if err != nil {
		t.Fatalf("Voices error: %v", err)
	}
	// Category is parsed from the response ("premade" for a; absent for b).
	want := []Voice{{ID: "a", Name: "Amy", Category: "premade"}, {ID: "b", Name: "Ian"}}
	if len(voices) != len(want) {
		t.Fatalf("got %d voices, want %d", len(voices), len(want))
	}
	for i, v := range want {
		if voices[i] != v {
			t.Errorf("voice %d = %+v, want %+v", i, voices[i], v)
		}
	}
}

func TestRetryDecisions(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{429, true}, {500, true}, {503, true},
		{400, false}, {401, false}, {404, false}, {422, false},
	}
	for _, tc := range cases {
		e := &apiError{status: tc.status}
		if got := e.retryable(); got != tc.want {
			t.Errorf("status %d retryable = %v, want %v", tc.status, got, tc.want)
		}
	}
	// transport errors (non-apiError) are retryable.
	if !isRetryable(io.ErrUnexpectedEOF) {
		t.Error("transport error should be retryable")
	}
}

func TestWordsFromAlignment(t *testing.T) {
	// Leading/trailing spaces and punctuation attached to words.
	chars := []string{" ", "H", "i", "!", " ", "y", "o", "u"}
	starts := []float64{0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7}
	ends := []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8}
	got := wordsFromAlignment(chars, starts, ends)
	want := []WordTiming{{Word: "Hi!", Start: 0.1, End: 0.4}, {Word: "you", Start: 0.5, End: 0.8}}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("word %d = %+v, want %+v", i, got[i], w)
		}
	}
}
