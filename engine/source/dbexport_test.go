package source

import (
	"strings"
	"testing"
)

func TestDBExportParse(t *testing.T) {
	// Mirrors the real multi-section export: a BOM + preamble, the
	// DialogueEntries marker, two header rows, then entries, then OutgoingLinks.
	data := strings.Join([]string{
		"\ufeffDatabase",
		"Name,Version,Author",
		"DialogueDatabase,,,",
		"",
		"Conversations",
		"ID,Title,Actor",
		"8,U2/Toppo,1",
		"DialogueEntries",
		"entrytag,ConvID,ID,Actor,Conversant,Title,MenuText,DialogueText,IsGroup",
		"Text,Number,Number,Number,Number,Text,Text,Text,Boolean",
		"8_Player_0,8,0,1,5,START,,,False",          // START node -> skip
		`8_Toppo_2,8,2,5,1,,,"Hello, cadet.",False`, // quoted comma
		"8_Player_12,8,12,1,5,,,,False",             // empty text -> skip
		"30_DANI_50,30,50,2,1,,,Menu review complete.,False",
		"5_Mission_Control_9,5,9,6,1,,,Come in.,False", // multi-word speaker
		"OutgoingLinks",
		"5_Toppo_99,5,99,5,1,,,Should be ignored.,False", // after section end
	}, "\n")

	got, err := DBExport{}.Parse(strings.NewReader(data))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	want := []LineItem{
		{ID: "8_Toppo_2", Speaker: "Toppo", Text: "Hello, cadet."},
		{ID: "30_DANI_50", Speaker: "DANI", Text: "Menu review complete."},
		{ID: "5_Mission_Control_9", Speaker: "Mission Control", Text: "Come in."},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].ID != w.ID || got[i].Speaker != w.Speaker || got[i].Text != w.Text {
			t.Errorf("item %d = {ID:%q Speaker:%q Text:%q}, want {ID:%q Speaker:%q Text:%q}",
				i, got[i].ID, got[i].Speaker, got[i].Text, w.ID, w.Speaker, w.Text)
		}
	}
	if got[0].Meta["conversation"] != "8" {
		t.Errorf("item 0 conversation meta = %q, want %q", got[0].Meta["conversation"], "8")
	}
}

func TestDBExportParseNoSection(t *testing.T) {
	_, err := DBExport{}.Parse(strings.NewReader("just,some,csv\nwith,no,section"))
	if err == nil {
		t.Fatal("expected an error when no DialogueEntries section is present")
	}
}

func TestSpeakerFromTag(t *testing.T) {
	cases := map[string]string{
		"8_Toppo_2":           "Toppo",
		"108_Player_4":        "Player",
		"5_Mission_Control_9": "Mission Control",
		"30_DANI_50":          "DANI",
		"noUnderscores":       "",
	}
	for tag, want := range cases {
		if got := speakerFromTag(tag); got != want {
			t.Errorf("speakerFromTag(%q) = %q, want %q", tag, got, want)
		}
	}
}
